package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestPurgeStockSessionsKeepsAdoptedSessionWithStaleStockWorkloadLabels(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	ctx := context.Background()
	sessionID := "adopted-stock-session"
	name := "agentapi-session-" + sessionID

	adoptedServiceLabels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    sessionID,
		"agentapi.proxy/user-id":       "test-user",
		"agentapi.proxy/scope":         "user",
	}
	staleStockLabels := map[string]string{
		"app.kubernetes.io/name":            "agentapi-session",
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/session-id":         sessionID,
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}

	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-svc",
			Namespace: "test-ns",
			Labels:    adoptedServiceLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create adopted service: %v", err)
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled deployment: %v", err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled pod: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pvc",
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled PVC: %v", err)
	}

	if err := manager.PurgeStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStockSessions failed: %v", err)
	}

	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected adopted service to remain, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled deployment to remain, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled pod to remain, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, name+"-pvc", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled PVC to remain, got err=%v", err)
	}
}

func TestPurgeStockSessionsKeepsAllocatedSessionWithStaleStockServiceLabel(t *testing.T) {
	for _, tc := range []struct {
		name         string
		stockState   string
		secretLabels map[string]string
		parentRunner bool
	}{
		{
			name:       "session settings",
			stockState: "true",
			secretLabels: map[string]string{
				"agentapi.proxy/resource": "session-settings",
			},
		},
		{
			name:       "provision request",
			stockState: "claiming",
			secretLabels: map[string]string{
				"agentapi.proxy/provision-request": "true",
			},
		},
		{
			name:         "parent runner allocation",
			stockState:   "true",
			parentRunner: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := newWorkloadTestManager(t, false)
			ctx := context.Background()
			sessionID := "allocated-stock-session"
			name := "agentapi-session-" + sessionID
			if tc.parentRunner {
				parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if got := r.Header.Get("Authorization"); got != "Bearer manager-token" {
						t.Errorf("Authorization = %q", got)
					}
					_, _ = w.Write([]byte(`{"allocated_runner_ids":["` + sessionID + `"]}`))
				}))
				t.Cleanup(parent.Close)
				manager.ConfigureSessionRunnerPool(parent.URL, "manager-a", "manager-token", "test-pool")
			}
			stockLabels := map[string]string{
				"app.kubernetes.io/name":       "agentapi-session",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"agentapi.proxy/session-id":    sessionID,
				"agentapi.proxy/stock":         tc.stockState,
			}

			if _, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Namespace: "test-ns", Labels: stockLabels},
			}, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Failed to create stale-labeled service: %v", err)
			}
			if _, err := manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: stockLabels},
			}, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Failed to create stale-labeled pod: %v", err)
			}

			if tc.secretLabels != nil {
				secretLabels := map[string]string{"agentapi.proxy/session-id": sessionID}
				for key, value := range tc.secretLabels {
					secretLabels[key] = value
				}
				if _, err := manager.client.CoreV1().Secrets("test-ns").Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: name + "-allocation-proof", Namespace: "test-ns", Labels: secretLabels},
				}, metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create allocation artifact: %v", err)
				}
			}

			if err := manager.PurgeStockSessions(ctx); err != nil {
				t.Fatalf("PurgeStockSessions failed: %v", err)
			}
			if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); err != nil {
				t.Fatalf("Expected allocated service to remain, got err=%v", err)
			}
			if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
				t.Fatalf("Expected allocated pod to remain, got err=%v", err)
			}
		})
	}
}

func TestPurgeStaleStockSessionsUsesEffectiveSessionPodTemplateHash(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	currentHash, err := manager.stockPodTemplateHash(ctx, false)
	if err != nil {
		t.Fatalf("Failed to calculate current stock template hash: %v", err)
	}

	for _, tc := range []struct {
		id   string
		hash string
	}{
		{id: "current-stock", hash: currentHash},
		{id: "stale-stock", hash: "old-hash"},
		{id: "legacy-stock"},
	} {
		name := "agentapi-session-" + tc.id
		labels := map[string]string{
			"app.kubernetes.io/managed-by": "agentapi-proxy",
			"agentapi.proxy/stock":         "true",
			"agentapi.proxy/session-id":    tc.id,
		}
		if tc.hash != "" {
			labels[stockPodTemplateHashLabel] = tc.hash
		}
		_, err = manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Namespace: "test-ns", Labels: labels},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create stock service %s: %v", tc.id, err)
		}
		_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: labels},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create stock deployment %s: %v", tc.id, err)
		}
	}

	if err := manager.PurgeStaleStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStaleStockSessions failed: %v", err)
	}

	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-current-stock-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected current stock service to remain, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, "agentapi-session-current-stock", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected current stock deployment to remain, got err=%v", err)
	}
	for _, id := range []string{"stale-stock", "legacy-stock"} {
		name := "agentapi-session-" + id
		if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
			t.Fatalf("Expected stale service %s to be deleted, got err=%v", id, err)
		}
		if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
			t.Fatalf("Expected stale deployment %s to be deleted, got err=%v", id, err)
		}
	}
}

func TestPurgeStaleStockSessionsDoesNotReconcilePoolStock(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "pool-runner"

	name := "agentapi-session-" + sessionID
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/stock":         "true",
		"agentapi.proxy/session-id":    sessionID,
		"agentapi.proxy/session-pool":  "test-pool",
		stockPodTemplateHashLabel:      "old-hash",
	}
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Namespace: "test-ns", Labels: labels},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: labels},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.PurgeStaleStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStaleStockSessions failed: %v", err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("allocated pool service was deleted: %v", err)
	}
	if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatalf("pool pod was deleted by local stock reconciliation: %v", err)
	}
}

func TestClaimPoolStockSessionRemovesServiceStockLabel(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "claimed-runner"
	name := "agentapi-session-" + sessionID + "-svc"
	_, err := manager.client.CoreV1().Secrets("test-ns").Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + sessionID + "-runner", Namespace: "test-ns"},
		Data:       map[string][]byte{"runner-token": []byte("runner-token")},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, name, "", "test-ns", 9000, cancel, nil)
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err = manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: map[string]string{
			"app.kubernetes.io/managed-by": "agentapi-proxy",
			"agentapi.proxy/session-id":    sessionID,
			"agentapi.proxy/session-pool":  "test-pool",
			"agentapi.proxy/stock":         "true",
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + sessionID, Namespace: "test-ns", Labels: map[string]string{
			"agentapi.proxy/session-id": sessionID,
			"agentapi.proxy/stock":      "true",
			"agentapi.proxy/user-id":    "",
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	claimedSettings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{ID: "public-session", UserID: "alice", Scope: "user", AgentType: "codex-acp", AutoSuspendEnabled: &enabled, AutoSuspendMinutes: 1},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{
			Enabled: true, Endpoint: "https://parent.example.com", SessionID: "public-session",
			ManagerID: "manager-a", Token: "runtime-token", Generation: 1,
		},
	}
	if err := manager.ClaimPoolStockSession(ctx, sessionID, "wrong-token", claimedSettings); err != ErrInvalidRunnerToken {
		t.Fatalf("wrong-token error = %v", err)
	}
	if err := manager.ClaimPoolStockSession(ctx, sessionID, "runner-token", claimedSettings); err != nil {
		t.Fatal(err)
	}
	svc, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Labels["agentapi.proxy/stock"]; ok {
		t.Fatalf("stock label remains after pool claim: %v", svc.Labels)
	}
	if svc.Labels["agentapi.proxy/session-pool"] != "test-pool" {
		t.Fatalf("pool label changed after claim: %v", svc.Labels)
	}
	if svc.Labels["agentapi.proxy/user-id"] != "alice" || svc.Labels["agentapi.proxy/scope"] != "user" {
		t.Fatalf("service identity labels = %v, want alice/user", svc.Labels)
	}
	if svc.Annotations[sessionSuspendAtAnnotation] == "" {
		t.Fatalf("claimed runner missing suspend timer: %#v", svc.Annotations)
	}
	pod, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, "agentapi-session-"+sessionID, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Labels["agentapi.proxy/user-id"] != "alice" || pod.Labels["agentapi.proxy/scope"] != "user" {
		t.Fatalf("pod identity labels = %v, want alice/user", pod.Labels)
	}
	credential, err := manager.client.CoreV1().Secrets("test-ns").Get(ctx, "agentapi-session-"+sessionID+"-runner", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Labels["agentapi.proxy/user-id"] != "alice" || credential.Labels["agentapi.proxy/scope"] != "user" {
		t.Fatalf("runner credential identity labels = %v, want alice/user", credential.Labels)
	}
	persisted, err := manager.CurrentSessionSettings(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ParentRuntime == nil || persisted.ParentRuntime.SessionID != "public-session" {
		t.Fatalf("parent runtime was not persisted: %#v", persisted.ParentRuntime)
	}
}
