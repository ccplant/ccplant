package services

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func runtimeStatusOverrideFromRedis(repo portrepos.StatusEventRepository, session entities.Session) {
	ks, ok := session.(*KubernetesSession)
	if !ok {
		return
	}
	// Infrastructure statuses must not be overridden.
	currentStatus := ks.Status()
	switch currentStatus {
	case "creating", "starting", "unhealthy", "stopped", "unknown":
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	redisStatus, updatedAt, err := repo.GetStatus(ctx, ks.ID())
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to read Redis status for session=%s: %v", ks.ID(), err)
		return
	}
	if redisStatus == "" {
		return // No Redis entry; keep Kubernetes-derived status.
	}

	// Only replace with more-specific runtime statuses.
	switch redisStatus {
	case "running", "active", "error", "timeout":
		if redisStatus != currentStatus {
			log.Printf("[K8S_SESSION] Overlaying Redis runtime status session=%s %s→%s",
				ks.ID(), currentStatus, redisStatus)
		}
		ks.SetStatusSilentAt(redisStatus, updatedAt)
	}
}
