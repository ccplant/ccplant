package repositories

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (r *KubernetesSessionRouteRepository) cachedList() ([]*portrepos.SessionRoute, bool) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	if r.listCache == nil || time.Now().After(r.listUntil) {
		return nil, false
	}
	return cloneSessionRoutes(r.listCache), true
}

func cloneSessionRoutes(routes []*portrepos.SessionRoute) []*portrepos.SessionRoute {
	clones := make([]*portrepos.SessionRoute, 0, len(routes))
	for _, route := range routes {
		clones = append(clones, cloneSessionRoute(route))
	}
	return clones
}

func filterAndCloneSessionRoutes(routes []*portrepos.SessionRoute, userID string) []*portrepos.SessionRoute {
	filtered := make([]*portrepos.SessionRoute, 0, len(routes))
	for _, route := range routes {
		if userID == "" || route.UserID == userID {
			filtered = append(filtered, cloneSessionRoute(route))
		}
	}
	return filtered
}

// Delete removes the routing information for the given session ID
func (r *KubernetesSessionRouteRepository) Delete(ctx context.Context, sessionID string) error {
	r.cacheMu.Lock()
	delete(r.cache, sessionID)
	r.listCache = nil
	r.listUntil = time.Time{}
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
