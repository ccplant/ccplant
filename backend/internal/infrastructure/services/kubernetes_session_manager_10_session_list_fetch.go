package services

import (
	"context"
	"log"
	"sort"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// In-memory-only filters (status, teamIDs, tags) are NOT applied here so
// callers can reuse the broad informer snapshot across filter variants.
func (m *KubernetesSessionManager) fetchSessionsFromK8s(ctx context.Context, labelSelector string, filter entities.SessionFilter) []entities.Session {
	if err := m.ensureSessionInformers(ctx); err == nil {
		return m.fetchSessionsFromInformer(labelSelector, filter)
	} else {
		log.Printf("[K8S_SESSION] Session informer unavailable, falling back to Kubernetes LIST: %v", err)
	}
	return m.fetchSessionsDirect(ctx, labelSelector, filter)
}

func (m *KubernetesSessionManager) fetchSessionsFromInformer(labelSelector string, filter entities.SessionFilter) []entities.Session {
	selector, err := labels.Parse(labelSelector)
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to parse session label selector %q: %v", labelSelector, err)
		return []entities.Session{}
	}

	serviceItems := m.serviceInformer.GetStore().List()
	// SlackBot reuse always supplies tags. Use the first tag's informer index so
	// lookup cost depends on matching sessions, not every session in the cluster.
	if len(filter.Tags) > 0 {
		keys := make([]string, 0, len(filter.Tags))
		for key := range filter.Tags {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		key := "agentapi.proxy/tag-" + sanitizeLabelKey(keys[0])
		indexed, indexErr := m.serviceInformer.GetIndexer().ByIndex(sessionTagInformerIndex, key+"="+sanitizeLabelValue(filter.Tags[keys[0]]))
		if indexErr == nil {
			serviceItems = indexed
		}
	}
	result := make([]entities.Session, 0)
	for _, item := range serviceItems {
		svc, ok := item.(*corev1.Service)
		if !ok || svc.DeletionTimestamp != nil || !selector.Matches(labels.Set(svc.Labels)) {
			continue
		}
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		userID := svc.Labels["agentapi.proxy/user-id"]
		if filter.UserID != "" && userID != filter.UserID {
			continue
		}
		var deployment *appsv1.Deployment
		var pod *corev1.Pod
		workloads, _ := m.workloadInformer.GetIndexer().ByIndex(sessionIDInformerIndex, sessionID)
		for _, item := range workloads {
			switch workload := item.(type) {
			case *appsv1.Deployment:
				deployment = workload
			case *corev1.Pod:
				pod = workload
			}
		}
		session := m.getOrRestoreSessionWithWorkload(svc.DeepCopy(), deployment, pod)
		if session != nil {
			result = append(result, session)
		}
	}
	return result
}

func (m *KubernetesSessionManager) fetchSessionsDirect(ctx context.Context, labelSelector string, filter entities.SessionFilter) []entities.Session {
	services, err := m.client.CoreV1().Services(m.namespace).List(
		ctx,
		metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list services: %v", err)
		return []entities.Session{}
	}

	deploymentMap := make(map[string]*appsv1.Deployment)
	podMap := make(map[string]*corev1.Pod)
	if m.isPVCEnabled() {
		// Batch fetch deployments once to avoid N+1 API calls.
		deployments, err := m.client.AppsV1().Deployments(m.namespace).List(
			ctx,
			metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Printf("[K8S_SESSION] Failed to list deployments: %v", err)
			// Continue without workload info – sessions will have "stopped" status.
		}
		if deployments != nil {
			for i := range deployments.Items {
				dep := &deployments.Items[i]
				if sid := dep.Labels["agentapi.proxy/session-id"]; sid != "" {
					deploymentMap[sid] = dep
				}
			}
		}
	} else {
		pods, err := m.client.CoreV1().Pods(m.namespace).List(
			ctx,
			metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Printf("[K8S_SESSION] Failed to list pods: %v", err)
		}
		if pods != nil {
			for i := range pods.Items {
				pod := &pods.Items[i]
				if sid := pod.Labels["agentapi.proxy/session-id"]; sid != "" {
					podMap[sid] = pod
				}
			}
		}
	}

	var result []entities.Session
	for i := range services.Items {
		svc := &services.Items[i]
		if svc.DeletionTimestamp != nil {
			continue
		}
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		userID := svc.Labels["agentapi.proxy/user-id"]
		if filter.UserID != "" && userID != filter.UserID {
			continue
		}
		session := m.getOrRestoreSessionWithWorkload(svc, deploymentMap[sessionID], podMap[sessionID])
		if session == nil {
			continue
		}
		result = append(result, session)
	}
	return result
}

// applySessionListFilters applies the in-memory-only parts of filter to
// sessions and returns the matching subset.
func (m *KubernetesSessionManager) applySessionListFilters(sessions []entities.Session, filter entities.SessionFilter) []entities.Session {
	var result []entities.Session
	for _, session := range sessions {
		if filter.Status != "" && session.Status() != filter.Status {
			continue
		}
		if filter.Scope != "" && session.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && session.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && session.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, tid := range filter.TeamIDs {
				if session.TeamID() == tid {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		if len(filter.Tags) > 0 {
			matchAll := true
			sessionTags := session.Tags()
			for k, v := range filter.Tags {
				sv, exists := sessionTags[k]
				if !exists || sanitizeLabelValue(sv) != sanitizeLabelValue(v) {
					matchAll = false
					break
				}
			}
			if !matchAll {
				continue
			}
		}
		result = append(result, session)
	}
	return result
}

// sessionsToCacheDTOs converts a slice of entities.Session to cache DTOs.
// Only fields present on the entities.Session interface are available here;
// extra KubernetesSession fields (deploymentName, serviceName) are captured
// via a type assertion when possible.
func sessionsToCacheDTOs(sessions []entities.Session) []portrepos.CachedSessionDTO {
	dtos := make([]portrepos.CachedSessionDTO, 0, len(sessions))
	for _, s := range sessions {
		dto := portrepos.CachedSessionDTO{
			ID:            s.ID(),
			UserID:        s.UserID(),
			Scope:         string(s.Scope()),
			TeamID:        s.TeamID(),
			Tags:          s.Tags(),
			Status:        s.Status(),
			StartedAt:     s.StartedAt(),
			UpdatedAt:     s.UpdatedAt(),
			LastMessageAt: s.LastMessageAt(),
			Description:   s.Description(),
			Annotations:   sessionAnnotations(s),
		}
		if provider, ok := s.(interface{ StatusMessage() string }); ok {
			dto.StatusMessage = provider.StatusMessage()
		}
		// Capture Kubernetes-specific fields when available.
		if ks, ok := s.(*KubernetesSession); ok {
			dto.ServicePort = ks.servicePort
			dto.Namespace = ks.namespace
			dto.DeploymentName = ks.deploymentName
			dto.ServiceName = ks.serviceName
			if ks.request != nil {
				dto.InitialMessage = ks.request.InitialMessage
			}
			dto.IsStock = ks.isStock
		}
		dtos = append(dtos, dto)
	}
	return dtos
}

type sessionAnnotationsProvider interface {
	Annotations() entities.SessionAnnotations
}

func sessionAnnotations(s entities.Session) entities.SessionAnnotations {
	if annotated, ok := s.(sessionAnnotationsProvider); ok {
		return annotated.Annotations()
	}
	return entities.SessionAnnotations{}
}

// getOrRestoreSessionWithWorkload gets a session from memory or restores it from Service
// using a pre-fetched workload to avoid additional API calls.
func (m *KubernetesSessionManager) getOrRestoreSessionWithWorkload(svc *corev1.Service, deployment *appsv1.Deployment, pod *corev1.Pod) *KubernetesSession {
	// Don't restore if Service is being deleted (same guard as GetSession)
	if svc.DeletionTimestamp != nil {
		return nil
	}

	sessionID := svc.Labels["agentapi.proxy/session-id"]

	// Check if session exists in memory
	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()

	if exists {
		session.SetAnnotations(sessionAnnotationsFromMap(svc.Annotations))
		// If the in-memory session was cached when this Service was still a stock
		// session (user-id was empty at restore time), and the Service now has a
		// real owner, repair the user-id in-place so authorization checks pass.
		// This avoids the need for a proxy restart to recover from the race where
		// another replica restored the session before stock adoption completed.
		if session.UserID() == "" {
			if svcUserID := svc.Labels["agentapi.proxy/user-id"]; svcUserID != "" {
				log.Printf("[K8S_SESSION] Repairing stale user-id for session %s (was empty, now %s)", sessionID, svcUserID)
				session.SetUserID(svcUserID)
			}
		}
		return session
	}

	// Restore session from Service with pre-fetched workload
	restored := m.restoreSessionFromServiceWithWorkload(svc, deployment, pod)
	if restored != nil && m.statusEventRepo != nil {
		runtimeStatusOverrideFromRedis(m.statusEventRepo, restored)
	}
	return restored
}
