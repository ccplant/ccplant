package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

const (
	SessionRouteSecretPrefix = "agentapi-session-route-"
	SessionRouteSecretKey    = "route.json"
	LabelSessionRoute        = "agentapi.proxy/session-route"
	sessionRouteCacheTTL     = 2 * time.Second
)

type routeJSON struct {
	SessionID         string            `json:"session_id"`
	RemoteSessionID   string            `json:"remote_session_id"`
	ManagerID         string            `json:"manager_id,omitempty"`
	HMACSecret        string            `json:"hmac_secret"`
	Transport         string            `json:"transport,omitempty"`
	RuntimeTokenHash  string            `json:"runtime_token_hash,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	UserID            string            `json:"user_id,omitempty"`
	Scope             string            `json:"scope,omitempty"`
	TeamID            string            `json:"team_id,omitempty"`
	Tags              map[string]string `json:"tags,omitempty"`
	StartedAt         time.Time         `json:"started_at,omitempty"`
	InitialMessage    string            `json:"initial_message,omitempty"`
	Status            string            `json:"status,omitempty"`
	StatusUpdatedAt   time.Time         `json:"status_updated_at,omitempty"`
	DeletionRequestID string            `json:"deletion_request_id,omitempty"`
}

// KubernetesSessionRouteRepository implements SessionRouteRepository using Kubernetes Secrets
type KubernetesSessionRouteRepository struct {
	client    kubernetes.Interface
	namespace string
	cacheMu   sync.RWMutex
	cache     map[string]cachedSessionRoute
	loads     singleflight.Group
}

type cachedSessionRoute struct {
	route     *portrepos.SessionRoute
	expiresAt time.Time
}

// NewKubernetesSessionRouteRepository creates a new KubernetesSessionRouteRepository
func NewKubernetesSessionRouteRepository(client kubernetes.Interface, namespace string) *KubernetesSessionRouteRepository {
	return &KubernetesSessionRouteRepository{client: client, namespace: namespace, cache: make(map[string]cachedSessionRoute)}
}

func (r *KubernetesSessionRouteRepository) secretName(sessionID string) string {
	name := SessionRouteSecretPrefix + sessionID
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// Save creates or updates a session route secret
func (r *KubernetesSessionRouteRepository) Save(ctx context.Context, route *portrepos.SessionRoute) error {
	data, err := json.Marshal(&routeJSON{
		SessionID:         route.SessionID,
		RemoteSessionID:   route.RemoteSessionID,
		ManagerID:         route.ManagerID,
		HMACSecret:        route.HMACSecret,
		Transport:         route.Transport,
		RuntimeTokenHash:  route.RuntimeTokenHash,
		Generation:        route.Generation,
		UserID:            route.UserID,
		Scope:             route.Scope,
		TeamID:            route.TeamID,
		Tags:              route.Tags,
		StartedAt:         route.StartedAt,
		InitialMessage:    route.InitialMessage,
		Status:            route.Status,
		StatusUpdatedAt:   route.StatusUpdatedAt,
		DeletionRequestID: route.DeletionRequestID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal route: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.secretName(route.SessionID),
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelSessionRoute: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SessionRouteSecretKey: data,
		},
	}

	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				current, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secret.Name, metav1.GetOptions{})
				if getErr != nil {
					return getErr
				}
				updated := secret.DeepCopy()
				updated.ResourceVersion = current.ResourceVersion
				_, updateErr := r.client.CoreV1().Secrets(r.namespace).Update(ctx, updated, metav1.UpdateOptions{})
				return updateErr
			})
			if err != nil {
				return fmt.Errorf("failed to update session route secret: %w", err)
			}
			r.cacheRoute(route)
			return nil
		}
		return fmt.Errorf("failed to create session route secret: %w", err)
	}
	r.cacheRoute(route)
	return nil
}

// Get retrieves routing information for the given session ID; returns nil, nil if not found
func (r *KubernetesSessionRouteRepository) Get(ctx context.Context, sessionID string) (*portrepos.SessionRoute, error) {
	if route, ok := r.cachedRoute(sessionID); ok {
		return route, nil
	}
	value, err, _ := r.loads.Do(sessionID, func() (interface{}, error) {
		if route, ok := r.cachedRoute(sessionID); ok {
			return route, nil
		}
		return r.load(ctx, sessionID)
	})
	if err != nil || value == nil {
		return nil, err
	}
	return cloneSessionRoute(value.(*portrepos.SessionRoute)), nil
}

func (r *KubernetesSessionRouteRepository) load(ctx context.Context, sessionID string) (*portrepos.SessionRoute, error) {
	secret, err := telemetry.Operation(ctx, "repositories.SessionRoute.GetKubernetes", func(operationCtx context.Context) (*corev1.Secret, error) {
		return r.client.CoreV1().Secrets(r.namespace).Get(operationCtx, r.secretName(sessionID), metav1.GetOptions{})
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get session route secret: %w", err)
	}

	raw, ok := secret.Data[SessionRouteSecretKey]
	if !ok {
		return nil, fmt.Errorf("session route secret missing data key")
	}

	var rj routeJSON
	if err := json.Unmarshal(raw, &rj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal route: %w", err)
	}

	route := &portrepos.SessionRoute{
		SessionID:         rj.SessionID,
		RemoteSessionID:   rj.RemoteSessionID,
		ManagerID:         rj.ManagerID,
		HMACSecret:        rj.HMACSecret,
		Transport:         rj.Transport,
		RuntimeTokenHash:  rj.RuntimeTokenHash,
		Generation:        rj.Generation,
		UserID:            rj.UserID,
		Scope:             rj.Scope,
		TeamID:            rj.TeamID,
		Tags:              rj.Tags,
		StartedAt:         rj.StartedAt,
		InitialMessage:    rj.InitialMessage,
		Status:            rj.Status,
		StatusUpdatedAt:   rj.StatusUpdatedAt,
		DeletionRequestID: rj.DeletionRequestID,
	}
	r.cacheRoute(route)
	return route, nil
}

func (r *KubernetesSessionRouteRepository) cachedRoute(sessionID string) (*portrepos.SessionRoute, bool) {
	r.cacheMu.RLock()
	entry, ok := r.cache[sessionID]
	r.cacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return cloneSessionRoute(entry.route), true
}

func (r *KubernetesSessionRouteRepository) cacheRoute(route *portrepos.SessionRoute) {
	if route == nil {
		return
	}
	r.cacheMu.Lock()
	r.cache[route.SessionID] = cachedSessionRoute{route: cloneSessionRoute(route), expiresAt: time.Now().Add(sessionRouteCacheTTL)}
	r.cacheMu.Unlock()
}

func cloneSessionRoute(route *portrepos.SessionRoute) *portrepos.SessionRoute {
	if route == nil {
		return nil
	}
	clone := *route
	clone.Tags = maps.Clone(route.Tags)
	return &clone
}

// List retrieves all session routes; if userID is non-empty, only routes for that user are returned
func (r *KubernetesSessionRouteRepository) List(ctx context.Context, userID string) ([]*portrepos.SessionRoute, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelSessionRoute + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session route secrets: %w", err)
	}

	routes := make([]*portrepos.SessionRoute, 0, len(secrets.Items))
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		raw, ok := secret.Data[SessionRouteSecretKey]
		if !ok {
			continue
		}
		var rj routeJSON
		if err := json.Unmarshal(raw, &rj); err != nil {
			continue
		}
		if userID != "" && rj.UserID != userID {
			continue
		}
		routes = append(routes, &portrepos.SessionRoute{
			SessionID:         rj.SessionID,
			RemoteSessionID:   rj.RemoteSessionID,
			ManagerID:         rj.ManagerID,
			HMACSecret:        rj.HMACSecret,
			Transport:         rj.Transport,
			RuntimeTokenHash:  rj.RuntimeTokenHash,
			Generation:        rj.Generation,
			UserID:            rj.UserID,
			Scope:             rj.Scope,
			TeamID:            rj.TeamID,
			Tags:              rj.Tags,
			StartedAt:         rj.StartedAt,
			InitialMessage:    rj.InitialMessage,
			Status:            rj.Status,
			StatusUpdatedAt:   rj.StatusUpdatedAt,
			DeletionRequestID: rj.DeletionRequestID,
		})
	}
	return routes, nil
}

// Delete removes the routing information for the given session ID
func (r *KubernetesSessionRouteRepository) Delete(ctx context.Context, sessionID string) error {
	r.cacheMu.Lock()
	delete(r.cache, sessionID)
	r.cacheMu.Unlock()
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, r.secretName(sessionID), metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Idempotent delete
		}
		return fmt.Errorf("failed to delete session route secret: %w", err)
	}
	return nil
}
