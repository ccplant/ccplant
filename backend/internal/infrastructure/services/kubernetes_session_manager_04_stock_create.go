package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// CreateStockSession creates a pre-warmed stock session (Deployment + Service)
// without creating a provision request. The pod starts agent-provisioner and
// waits for adoption, at which point adoptStockSession creates the request.
// Note: Sandbox (network filter) and scia sidecar are always enabled.
func (m *KubernetesSessionManager) CreateStockSession(ctx context.Context, dind bool) error {
	return m.CreateStockSessionForPool(ctx, "", dind)
}

func (m *KubernetesSessionManager) CreateStockSessionForPool(ctx context.Context, pool string, dind bool) error {
	m.refreshConfig()
	podTemplateHash, err := m.stockPodTemplateHash(ctx, dind)
	if err != nil {
		return fmt.Errorf("failed to resolve stock pod template hash: %w", err)
	}
	id := uuid.New().String()
	deploymentName := fmt.Sprintf("agentapi-session-%s", id)
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)

	_, cancel := context.WithCancel(context.Background())

	// Stock sessions have no owner; the minimal request holds pod capabilities only.
	// Sandbox is always enabled; only DinD is configurable.
	minimalReq := &entities.RunServerRequest{
		Sandbox: stockSandboxParams(),
	}
	if pool != "" && m.runnerParentURL != "" {
		runnerToken, registerErr := m.registerSessionRunner(ctx, id, pool, dind)
		if registerErr != nil {
			cancel()
			return fmt.Errorf("register stock session runner: %w", registerErr)
		}
		minimalReq.Environment = map[string]string{
			"AGENTAPI_SESSION_RUNNER_POOL":  pool,
			"AGENTAPI_SESSION_RUNNER_ID":    id,
			"AGENTAPI_SESSION_RUNNER_TOKEN": runnerToken,
			"PROVISIONER_PROXY_URL":         m.runnerParentURL,
		}
	}
	if dind {
		minimalReq.Docker = &entities.DockerParams{Enabled: true}
	}

	session := NewKubernetesSession(id, minimalReq, deploymentName, serviceName, pvcName,
		m.namespace, m.k8sConfig.BasePort, cancel, nil)
	session.SetIsStock(true)

	// Create the Service first so stock resources can reference it as owner.
	// Keep it out of the available stock pool until the workload is created.
	stockLabels := m.buildLabels(session)
	stockLabels["agentapi.proxy/stock"] = "creating"
	stockLabels[stockPodTemplateHashLabel] = podTemplateHash
	if pool != "" {
		stockLabels["agentapi.proxy/session-pool"] = pool
	}
	if err := m.createServiceWithLabels(ctx, session, stockLabels); err != nil {
		cancel()
		return fmt.Errorf("failed to create stock service: %w", err)
	}
	if minimalReq.Environment["AGENTAPI_SESSION_RUNNER_TOKEN"] != "" {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: runnerCredentialSecretName(id), Namespace: m.namespace, Labels: m.buildLabels(session), OwnerReferences: m.sessionServiceOwnerReferences(ctx, id)},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{"runner-token": minimalReq.Environment["AGENTAPI_SESSION_RUNNER_TOKEN"]},
		}
		if _, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			_ = m.cleanupStockSessionResources(session)
			cancel()
			return fmt.Errorf("create runner credential Secret: %w", err)
		}
	}

	// Create PVC if enabled (required for Deployment volume mounts).
	if m.isPVCEnabled() {
		if err := m.createPVC(ctx, session); err != nil {
			if delErr := m.cleanupStockSessionResources(session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup resources after stock PVC creation failure: %v", delErr)
			}
			cancel()
			return fmt.Errorf("failed to create stock PVC: %w", err)
		}
	}

	if err := m.createSessionWorkload(ctx, session, minimalReq); err != nil {
		if delErr := m.cleanupStockSessionResources(session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after stock workload creation failure: %v", delErr)
		}
		cancel()
		return fmt.Errorf("failed to create stock workload: %w", err)
	}

	// Creating the Kubernetes resources and publishing them as allocatable stock
	// are deliberately separate operations. Pod startup can take longer than an
	// HTTP proxy's request timeout, but the Service's stock=creating label is a
	// durable reservation that can be reconciled or purged after a manager
	// restart. Complete readiness and publication independently of the request.
	go m.publishStockSession(session, cancel, dind)
	return nil
}

func (m *KubernetesSessionManager) publishStockSession(session *KubernetesSession, cancel context.CancelFunc, dind bool) {
	// Do not publish the stock session to allocators until every container is
	// ready. In particular, adoption immediately POSTs the sandbox policy to the
	// provisioner. Marking the Service stock=true before its readiness probe has
	// passed lets a concurrent allocation claim it while port 9001 is still
	// closed, causing the allocation (and the newly claimed stock session) to be
	// deleted.
	if err := m.waitForSessionWorkloadReady(context.Background(), session); err != nil {
		if delErr := m.cleanupStockSessionResources(session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after stock workload readiness failure: %v", delErr)
		}
		cancel()
		log.Printf("[K8S_SESSION] Stock session %s did not become ready: %v", session.ID(), err)
		return
	}

	published, err := m.markStockServiceReady(context.Background(), session.ServiceName())
	if err != nil {
		if delErr := m.cleanupStockSessionResources(session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after stock service update failure: %v", delErr)
		}
		cancel()
		log.Printf("[K8S_SESSION] Failed to mark stock session %s ready: %v", session.ID(), err)
		return
	}
	if !published {
		log.Printf("[K8S_SESSION] Stock session %s was allocated before it became ready; leaving its labels unchanged", session.ID())
		return
	}
	log.Printf("[K8S_SESSION] Stock session %s created successfully (dind=%t)",
		session.ID(), dind)
}

func (m *KubernetesSessionManager) markStockServiceReady(ctx context.Context, serviceName string) (bool, error) {
	published := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		published = false
		svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if svc.Labels["agentapi.proxy/stock"] != "creating" {
			return nil
		}
		svc.Labels["agentapi.proxy/stock"] = "true"
		if _, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
			return err
		}
		published = true
		return nil
	})
	return published, err
}

func (m *KubernetesSessionManager) registerSessionRunner(ctx context.Context, runnerID, pool string, dind bool) (string, error) {
	body, err := json.Marshal(map[string]any{
		"runner_id": runnerID,
		"pool":      pool,
		"namespace": m.namespace,
		"capabilities": map[string]string{
			"dind": fmt.Sprintf("%t", dind),
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.runnerParentURL+"/internal/session-runners/register", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.runnerManagerToken)
	req.Header.Set("X-Session-Manager-ID", m.runnerManagerID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("runner registration returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		RunnerToken string `json:"runner_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.RunnerToken == "" {
		return "", fmt.Errorf("runner registration returned no token")
	}
	return result.RunnerToken, nil
}

func runnerCredentialSecretName(sessionID string) string {
	return "agentapi-session-" + sessionID + "-runner"
}

// stockPodTemplateHash builds the effective session Pod template for a stable
// synthetic stock session and hashes it. A stable session ID ensures that
// per-session labels, environment variables, and resource names do not make
// identical templates appear different.
func (m *KubernetesSessionManager) stockPodTemplateHash(ctx context.Context, dind bool) (string, error) {
	const id = "stock-template-hash"
	req := &entities.RunServerRequest{Sandbox: stockSandboxParams()}
	if dind {
		req.Docker = &entities.DockerParams{Enabled: true}
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := NewKubernetesSession(
		id,
		req,
		"agentapi-session-"+id,
		"agentapi-session-"+id+"-svc",
		"agentapi-session-"+id+"-pvc",
		m.namespace,
		m.k8sConfig.BasePort,
		cancel,
		nil,
	)
	session.SetIsStock(true)
	deployment, err := m.buildDeployment(ctx, session, req)
	if err != nil {
		return "", fmt.Errorf("build stock session pod template: %w", err)
	}
	data, err := json.Marshal(deployment.Spec.Template)
	if err != nil {
		return "", fmt.Errorf("marshal stock session pod template: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16]), nil
}
