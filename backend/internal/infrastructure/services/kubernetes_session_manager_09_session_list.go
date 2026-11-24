package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// ListSessions returns all sessions matching the filter.
// Sessions are read from label-filtered Kubernetes informer stores. The
// initial LIST establishes each store, then WATCH events keep it current.
func (m *KubernetesSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	labelSelector := m.buildLabelSelector(filter)
	ctx := context.Background()
	allSessions := m.fetchSessionsFromK8s(ctx, labelSelector, filter)
	m.hydrateSessionStatusMessages(ctx, allSessions)
	allocationSessions := m.fetchSessionAllocationsFromK8s(ctx, filter)

	return mergeSessionAllocations(m.applySessionListFilters(allSessions, filter), allocationSessions)
}

func (m *KubernetesSessionManager) hydrateSessionStatusMessages(ctx context.Context, sessions []entities.Session) {
	for _, session := range sessions {
		ks, ok := session.(*KubernetesSession)
		if !ok || (ks.Status() != "error" && ks.Status() != "timeout") || ks.StatusMessage() != "" {
			continue
		}
		request, err := m.getProvisionRequest(ctx, ks.ID())
		if err != nil || request.Message == "" {
			continue
		}
		ks.SetStatusMessage(request.Message)
		ks.SetErrorCode(request.ErrorCode)
	}
}

func mergeSessionAllocations(sessions []entities.Session, allocationSessions []entities.Session) []entities.Session {
	if len(allocationSessions) == 0 {
		return sessions
	}
	seen := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		seen[session.ID()] = struct{}{}
	}
	for _, session := range allocationSessions {
		if _, ok := seen[session.ID()]; ok {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions
}

func (m *KubernetesSessionManager) invalidateSessionListCache(reason string) {
	if m.sessionListCacheRepo == nil {
		return
	}
	if err := m.sessionListCacheRepo.InvalidateSessionListCache(context.Background(), m.namespace); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to invalidate session list cache after %s: %v", reason, err)
	}
}

func (m *KubernetesSessionManager) fetchSessionAllocationsFromK8s(ctx context.Context, filter entities.SessionFilter) []entities.Session {
	secrets, err := m.listSessionAllocations(ctx, filter)
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list session allocations: %v", err)
		return nil
	}

	sessions := make([]entities.Session, 0, len(secrets.Items))
	for i := range secrets.Items {
		sec := &secrets.Items[i]
		if sec.DeletionTimestamp != nil {
			continue
		}
		data := sec.Data[sessionAllocationDataKey]
		if len(data) == 0 {
			continue
		}
		var allocation coreallocation.AllocationRequest
		if err := json.Unmarshal(data, &allocation); err != nil {
			log.Printf("[K8S_SESSION] Failed to decode session allocation %s: %v", sec.Name, err)
			continue
		}
		if allocation.SessionID == "" || allocation.Request == nil {
			continue
		}
		scope := allocation.Request.Scope
		if scope == "" {
			scope = entities.ScopeUser
		}
		session := entities.NewProxySessionWithStatus(
			allocation.SessionID,
			allocation.Request.UserID,
			scope,
			allocation.Request.TeamID,
			allocation.Request.Tags,
			allocation.UpdatedAt,
			string(allocation.Status),
		)
		session.SetStatusMessage(allocation.Message)
		if len(m.applySessionListFilters([]entities.Session{session}, filter)) == 0 {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions
}

const sessionAllocationListCacheTTL = 2 * time.Second

func (m *KubernetesSessionManager) listSessionAllocations(ctx context.Context, filter entities.SessionFilter) (*corev1.SecretList, error) {
	if err := m.ensureSessionInformers(ctx); err == nil {
		items := m.filteredAllocationInformerItems(filter)
		secrets := &corev1.SecretList{Items: make([]corev1.Secret, 0, len(items))}
		for _, item := range items {
			secret, ok := item.(*corev1.Secret)
			if ok {
				secrets.Items = append(secrets.Items, *secret.DeepCopy())
			}
		}
		return secrets, nil
	}

	// Startup fallback: retain availability if the informer cannot complete its
	// initial sync. Once synced, all subsequent reads use the local store.
	if secrets, ok := m.cachedSessionAllocations(); ok {
		return secrets, nil
	}
	value, err, _ := m.allocationListLoad.Do("active", func() (interface{}, error) {
		if secrets, ok := m.cachedSessionAllocations(); ok {
			return secrets, nil
		}
		selector := "agentapi.proxy/session-allocation=true,agentapi.proxy/session-allocation-status in (pending,allocating,error)"
		if filter.UserID != "" {
			selector += ",agentapi.proxy/user-id=" + sanitizeLabelValue(filter.UserID)
		}
		if filter.Scope == entities.ScopeTeam {
			selector += ",agentapi.proxy/scope=team"
		}
		if filter.TeamID != "" {
			selector += ",agentapi.proxy/team-id-hash=" + hashTeamID(filter.TeamID)
		}
		for key, value := range filter.Tags {
			selector += ",agentapi.proxy/tag-" + sanitizeLabelKey(key) + "=" + sanitizeLabelValue(value)
		}
		secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, err
		}
		m.allocationListMu.Lock()
		m.allocationListCache = secrets.DeepCopy()
		m.allocationListUntil = time.Now().Add(sessionAllocationListCacheTTL)
		m.allocationListMu.Unlock()
		return secrets.DeepCopy(), nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*corev1.SecretList).DeepCopy(), nil
}

func (m *KubernetesSessionManager) filteredAllocationInformerItems(filter entities.SessionFilter) []interface{} {
	indexer := m.allocationInformer.GetIndexer()
	if filter.UserID != "" {
		if items, err := indexer.ByIndex(sessionUserInformerIndex, sanitizeLabelValue(filter.UserID)); err == nil {
			return items
		}
	}
	if filter.TeamID != "" {
		if items, err := indexer.ByIndex(sessionTeamInformerIndex, hashTeamID(filter.TeamID)); err == nil {
			return items
		}
	}
	if len(filter.TeamIDs) > 0 {
		items := make([]interface{}, 0)
		seen := make(map[string]struct{})
		for _, teamID := range filter.TeamIDs {
			matched, err := indexer.ByIndex(sessionTeamInformerIndex, hashTeamID(teamID))
			if err != nil {
				return m.allocationInformer.GetStore().List()
			}
			for _, item := range matched {
				accessor, err := meta.Accessor(item)
				if err != nil {
					continue
				}
				if _, ok := seen[accessor.GetName()]; ok {
					continue
				}
				seen[accessor.GetName()] = struct{}{}
				items = append(items, item)
			}
		}
		return items
	}
	if len(filter.Tags) > 0 {
		keys := make([]string, 0, len(filter.Tags))
		for key := range filter.Tags {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		indexKey := "agentapi.proxy/tag-" + sanitizeLabelKey(keys[0]) + "=" + sanitizeLabelValue(filter.Tags[keys[0]])
		if items, err := indexer.ByIndex(sessionTagInformerIndex, indexKey); err == nil {
			return items
		}
	}
	return m.allocationInformer.GetStore().List()
}

const sessionInformerSyncTimeout = 15 * time.Second

const (
	sessionIDInformerIndex   = "agentapi.session-id"
	sessionTagInformerIndex  = "agentapi.session-tag"
	sessionUserInformerIndex = "agentapi.user-id"
	sessionTeamInformerIndex = "agentapi.team-id-hash"
)

func sessionIDIndexFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	if id := accessor.GetLabels()["agentapi.proxy/session-id"]; id != "" {
		return []string{id}, nil
	}
	return nil, nil
}

func sessionTagIndexFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	var values []string
	for key, value := range accessor.GetLabels() {
		if strings.HasPrefix(key, "agentapi.proxy/tag-") {
			values = append(values, key+"="+value)
		}
	}
	return values, nil
}

func sessionLabelIndexFunc(label string) cache.IndexFunc {
	return func(obj interface{}) ([]string, error) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		if value := accessor.GetLabels()[label]; value != "" {
			return []string{value}, nil
		}
		return nil, nil
	}
}

func (m *KubernetesSessionManager) ensureSessionInformers(ctx context.Context) error {
	m.sessionInformerOnce.Do(func() {
		resourceFactory := informers.NewSharedInformerFactoryWithOptions(
			m.client,
			0,
			informers.WithNamespace(m.namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "agentapi.proxy/session-id"
			}),
		)
		allocationFactory := informers.NewSharedInformerFactoryWithOptions(
			m.client,
			0,
			informers.WithNamespace(m.namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "agentapi.proxy/session-allocation=true,agentapi.proxy/session-allocation-status in (pending,allocating,error)"
			}),
		)
		m.serviceInformer = resourceFactory.Core().V1().Services().Informer()
		if m.isPVCEnabled() {
			m.workloadInformer = resourceFactory.Apps().V1().Deployments().Informer()
		} else {
			m.workloadInformer = resourceFactory.Core().V1().Pods().Informer()
		}
		m.allocationInformer = allocationFactory.Core().V1().Secrets().Informer()
		for _, informer := range []cache.SharedIndexInformer{m.serviceInformer, m.workloadInformer} {
			if err := informer.AddIndexers(cache.Indexers{
				sessionIDInformerIndex:  sessionIDIndexFunc,
				sessionTagInformerIndex: sessionTagIndexFunc,
			}); err != nil {
				log.Printf("[K8S_SESSION] Failed to add session informer indexes: %v", err)
			}
		}
		if err := m.allocationInformer.AddIndexers(cache.Indexers{
			sessionUserInformerIndex: sessionLabelIndexFunc("agentapi.proxy/user-id"),
			sessionTeamInformerIndex: sessionLabelIndexFunc("agentapi.proxy/team-id-hash"),
			sessionTagInformerIndex:  sessionTagIndexFunc,
		}); err != nil {
			log.Printf("[K8S_SESSION] Failed to add session allocation informer indexes: %v", err)
		}
		resourceFactory.Start(m.sessionInformerCtx.Done())
		allocationFactory.Start(m.sessionInformerCtx.Done())
	})

	waitCtx, cancel := context.WithTimeout(ctx, sessionInformerSyncTimeout)
	defer cancel()
	if !cache.WaitForCacheSync(waitCtx.Done(), m.serviceInformer.HasSynced, m.workloadInformer.HasSynced, m.allocationInformer.HasSynced) {
		return fmt.Errorf("timed out waiting for Kubernetes session informer cache sync")
	}
	return nil
}

func (m *KubernetesSessionManager) cachedSessionAllocations() (*corev1.SecretList, bool) {
	m.allocationListMu.RLock()
	defer m.allocationListMu.RUnlock()
	if m.allocationListCache == nil || time.Now().After(m.allocationListUntil) {
		return nil, false
	}
	return m.allocationListCache.DeepCopy(), true
}

func (m *KubernetesSessionManager) invalidateSessionAllocationListCache() {
	m.allocationListMu.Lock()
	m.allocationListCache = nil
	m.allocationListUntil = time.Time{}
	m.allocationListMu.Unlock()
}

// redisSessionListCacheTTL is the TTL passed to the cache repository.
// Extended from 15s to 60s to reduce Kubernetes API calls while still ensuring
// reasonable freshness for session list queries.
const redisSessionListCacheTTL = 60 * time.Second

// fetchSessionsFromK8s reads the Kubernetes Service and workload informer
// stores and returns all sessions that match the label selector. A direct LIST
// is used only when the initial informer sync cannot complete.
