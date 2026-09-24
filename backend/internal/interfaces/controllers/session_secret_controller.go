package controllers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	oneTimeSecretPrefix          = "agentapi-ots-"
	oneTimeSecretValueKey        = "value"
	oneTimeSecretSessionID       = "agentapi.proxy/session-id"
	oneTimeSecretExpiresAt       = "agentapi.proxy/expires-at"
	oneTimeSecretConsumedAt      = "agentapi.proxy/consumed-at"
	oneTimeSecretSessionHash     = "agentapi.proxy/session-hash"
	defaultOneTimeSecretLifetime = 10 * time.Minute
	maxOneTimeSecretLifetime     = time.Hour
	maxOneTimeSecretBytes        = 64 << 10
)

type sessionSecretManager interface {
	GetSession(id string) entities.Session
}

type SessionSecretController struct {
	client    kubernetes.Interface
	namespace string
	manager   sessionSecretManager
	routes    portrepos.SessionRouteRepository
	now       func() time.Time
}

func NewSessionSecretController(client kubernetes.Interface, namespace string, manager sessionSecretManager, routes portrepos.SessionRouteRepository) *SessionSecretController {
	return &SessionSecretController{client: client, namespace: namespace, manager: manager, routes: routes, now: time.Now}
}

// Create registers a short-lived value for a running session. The value is
// deliberately omitted from the response and cannot be listed through this API.
func (c *SessionSecretController) Create(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	ownerID, scope, teamID, found, err := c.sessionOwner(ctx, sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to look up session")
	}
	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanAccessResource(ownerID, scope, teamID) {
		return echo.NewHTTPError(http.StatusForbidden, "you don't have permission to access this session")
	}

	var input struct {
		Value            string `json:"value"`
		ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	}
	if err := ctx.Bind(&input); err != nil || input.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "value is required")
	}
	if len(input.Value) > maxOneTimeSecretBytes {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "value exceeds 64 KiB")
	}
	lifetime := defaultOneTimeSecretLifetime
	if input.ExpiresInSeconds != 0 {
		lifetime = time.Duration(input.ExpiresInSeconds) * time.Second
		if lifetime <= 0 || lifetime > maxOneTimeSecretLifetime {
			return echo.NewHTTPError(http.StatusBadRequest, "expires_in_seconds must be between 1 and 3600")
		}
	}

	id := uuid.NewString()
	expiresAt := c.now().UTC().Add(lifetime)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oneTimeSecretName(sessionID, id),
			Namespace: c.namespace,
			Labels: map[string]string{
				"agentapi.proxy/one-time-secret": "true",
				oneTimeSecretSessionHash:         oneTimeSecretSessionLabel(sessionID),
			},
			Annotations: map[string]string{
				oneTimeSecretSessionID: sessionID,
				oneTimeSecretExpiresAt: expiresAt.Format(time.RFC3339Nano),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{oneTimeSecretValueKey: []byte(input.Value)},
	}
	if _, err := c.client.CoreV1().Secrets(c.namespace).Create(ctx.Request().Context(), secret, metav1.CreateOptions{}); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to register secret")
	}
	return ctx.JSON(http.StatusCreated, map[string]interface{}{
		"secret_id":  id,
		"expires_at": expiresAt,
		"local_url":  "http://127.0.0.1:9001/one-time-secrets/" + id,
	})
}

// Consume returns the value exactly once. Clearing it uses Kubernetes resource
// version compare-and-swap, so concurrent callers cannot both succeed.
func (c *SessionSecretController) Consume(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	if !c.authorizeSession(ctx, sessionID) {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	name := oneTimeSecretName(sessionID, ctx.Param("secretId"))
	secrets := c.client.CoreV1().Secrets(c.namespace)
	secret, err := secrets.Get(ctx.Request().Context(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ctx.NoContent(http.StatusNotFound)
	}
	if err != nil {
		return ctx.NoContent(http.StatusServiceUnavailable)
	}
	if secret.Annotations[oneTimeSecretSessionID] != sessionID || secret.Annotations[oneTimeSecretConsumedAt] != "" {
		return ctx.NoContent(http.StatusNotFound)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, secret.Annotations[oneTimeSecretExpiresAt])
	if err != nil || !c.now().Before(expiresAt) {
		secret.Data = nil
		secret.Annotations[oneTimeSecretConsumedAt] = c.now().UTC().Format(time.RFC3339Nano)
		_, _ = secrets.Update(ctx.Request().Context(), secret, metav1.UpdateOptions{})
		return ctx.NoContent(http.StatusGone)
	}
	value, ok := secret.Data[oneTimeSecretValueKey]
	if !ok {
		return ctx.NoContent(http.StatusNotFound)
	}
	secret.Data = nil
	secret.Annotations[oneTimeSecretConsumedAt] = c.now().UTC().Format(time.RFC3339Nano)
	updated, err := secrets.Update(ctx.Request().Context(), secret, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return ctx.NoContent(http.StatusNotFound)
	}
	if err != nil {
		return ctx.NoContent(http.StatusServiceUnavailable)
	}
	_ = secrets.Delete(ctx.Request().Context(), updated.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &updated.UID, ResourceVersion: &updated.ResourceVersion}})
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusOK, map[string]string{"value": string(value)})
}

// ConsumeNext returns the oldest unconsumed value for the session. Callers do
// not need to know the server-generated secret ID.
func (c *SessionSecretController) ConsumeNext(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	if !c.authorizeSession(ctx, sessionID) {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	secrets := c.client.CoreV1().Secrets(c.namespace)
	for attempts := 0; attempts < 5; attempts++ {
		items, err := secrets.List(ctx.Request().Context(), metav1.ListOptions{LabelSelector: oneTimeSecretSessionHash + "=" + oneTimeSecretSessionLabel(sessionID)})
		if err != nil {
			return ctx.NoContent(http.StatusServiceUnavailable)
		}
		sort.Slice(items.Items, func(i, j int) bool {
			if items.Items[i].CreationTimestamp.Equal(&items.Items[j].CreationTimestamp) {
				return items.Items[i].Name < items.Items[j].Name
			}
			return items.Items[i].CreationTimestamp.Before(&items.Items[j].CreationTimestamp)
		})
		conflicted := false
		for i := range items.Items {
			secret := &items.Items[i]
			if secret.Annotations[oneTimeSecretSessionID] != sessionID || secret.Annotations[oneTimeSecretConsumedAt] != "" {
				continue
			}
			expiresAt, err := time.Parse(time.RFC3339Nano, secret.Annotations[oneTimeSecretExpiresAt])
			if err != nil || !c.now().Before(expiresAt) {
				secret.Data = nil
				secret.Annotations[oneTimeSecretConsumedAt] = c.now().UTC().Format(time.RFC3339Nano)
				_, _ = secrets.Update(ctx.Request().Context(), secret, metav1.UpdateOptions{})
				continue
			}
			value, ok := secret.Data[oneTimeSecretValueKey]
			if !ok {
				continue
			}
			secret.Data = nil
			secret.Annotations[oneTimeSecretConsumedAt] = c.now().UTC().Format(time.RFC3339Nano)
			updated, err := secrets.Update(ctx.Request().Context(), secret, metav1.UpdateOptions{})
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				conflicted = true
				break
			}
			if err != nil {
				return ctx.NoContent(http.StatusServiceUnavailable)
			}
			_ = secrets.Delete(ctx.Request().Context(), updated.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &updated.UID, ResourceVersion: &updated.ResourceVersion}})
			ctx.Response().Header().Set("Cache-Control", "no-store")
			return ctx.JSON(http.StatusOK, map[string]string{"value": string(value)})
		}
		if !conflicted {
			return ctx.NoContent(http.StatusNotFound)
		}
	}
	return ctx.NoContent(http.StatusNotFound)
}

func (c *SessionSecretController) sessionOwner(ctx echo.Context, sessionID string) (string, string, string, bool, error) {
	if c.manager != nil {
		if session := c.manager.GetSession(sessionID); session != nil {
			return session.UserID(), string(session.Scope()), session.TeamID(), true, nil
		}
	}
	if c.routes != nil {
		route, err := c.routes.Get(ctx.Request().Context(), sessionID)
		if err != nil {
			return "", "", "", false, err
		}
		if route != nil {
			return route.UserID, route.Scope, route.TeamID, true, nil
		}
	}
	return "", "", "", false, nil
}

func (c *SessionSecretController) authorizeSession(ctx echo.Context, sessionID string) bool {
	header := ctx.Request().Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if validator, ok := c.manager.(interface{ ValidateSessionControlToken(string, string) bool }); ok && validator.ValidateSessionControlToken(sessionID, token) {
		return c.manager.GetSession(sessionID) != nil
	}
	if c.routes == nil {
		return false
	}
	route, err := c.routes.Get(ctx.Request().Context(), sessionID)
	if err != nil || route == nil || route.Transport != portrepos.SessionRouteTransportDirectRuntime || route.RuntimeTokenHash == "" {
		return false
	}
	generation, err := strconv.ParseInt(ctx.QueryParam("generation"), 10, 64)
	if err != nil || generation != route.Generation {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(route.RuntimeTokenHash)) == 1
}

func oneTimeSecretName(sessionID, id string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + id))
	return oneTimeSecretPrefix + hex.EncodeToString(digest[:])
}

func oneTimeSecretSessionLabel(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(digest[:16])
}
