package cmd

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type orphanScanner struct {
	ctx       context.Context
	client    kubernetes.Interface
	namespace string
	seen      map[string]struct{}
	orphaned  []orphanedSession
}

func newOrphanScanner(ctx context.Context, client kubernetes.Interface, namespace string) *orphanScanner {
	return &orphanScanner{ctx: ctx, client: client, namespace: namespace, seen: make(map[string]struct{})}
}

func (s *orphanScanner) record(sessionID, source, message string, values ...any) {
	fmt.Printf(message, values...)
	s.seen[sessionID] = struct{}{}
	s.orphaned = append(s.orphaned, orphanedSession{id: sessionID, source: source})
}

func (s *orphanScanner) alreadySeen(sessionID, pass string) bool {
	if _, ok := s.seen[sessionID]; !ok {
		return false
	}
	if orphanedVerbose {
		fmt.Printf("  [SKIP] session %s: already detected in %s\n", sessionID, pass)
	}
	return true
}

func (s *orphanScanner) workloadMissing(sessionID string) (bool, bool) {
	name := fmt.Sprintf("agentapi-session-%s", sessionID)
	exists, err := sessionWorkloadExists(s.ctx, s.client, s.namespace, name)
	if err != nil {
		fmt.Printf("  [WARN] session %s: error checking workload %s: %v\n", sessionID, name, err)
		return false, false
	}
	if exists {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: workload %s found\n", sessionID, name)
		}
		return false, true
	}
	return true, false
}

func (s *orphanScanner) scanServices() error {
	services, err := s.client.CoreV1().Services(s.namespace).List(s.ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=agentapi-session,app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return fmt.Errorf("failed to list session services: %w", err)
	}
	fmt.Printf("Pass 1 (Service scan): found %d session service(s)\n", len(services.Items))
	for _, service := range services.Items {
		sessionID := service.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			if orphanedVerbose {
				fmt.Printf("  [SKIP] Service %s: missing agentapi.proxy/session-id label\n", service.Name)
			}
			continue
		}
		missing, active := s.workloadMissing(sessionID)
		if active {
			continue
		}
		if missing {
			s.record(sessionID, "service", "  [ORPHANED] session %s: workload agentapi-session-%s not found (detected via Service)\n", sessionID, sessionID)
		}
	}
	return nil
}

func (s *orphanScanner) scanSettingsSecrets() error {
	secrets, err := s.client.CoreV1().Secrets(s.namespace).List(s.ctx, metav1.ListOptions{LabelSelector: "agentapi.proxy/resource=session-settings"})
	if err != nil {
		return fmt.Errorf("failed to list session settings secrets: %w", err)
	}
	fmt.Printf("Pass 2 (Settings Secret scan): found %d settings secret(s)\n", len(secrets.Items))
	for _, secret := range secrets.Items {
		sessionID := secret.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			if orphanedVerbose {
				fmt.Printf("  [SKIP] Secret %s: missing agentapi.proxy/session-id label\n", secret.Name)
			}
			continue
		}
		if s.alreadySeen(sessionID, "Pass 1") {
			continue
		}
		missing, active := s.workloadMissing(sessionID)
		if active {
			continue
		}
		if missing {
			s.record(sessionID, "settings-secret", "  [ORPHANED] session %s: workload agentapi-session-%s not found (detected via Settings Secret)\n", sessionID, sessionID)
		}
	}
	return nil
}

func (s *orphanScanner) relatedProvisionResourcesExist(sessionID string) (bool, bool) {
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", sessionID)
	serviceExists, err := serviceExistsForSession(s.ctx, s.client, s.namespace, sessionID)
	if err != nil {
		fmt.Printf("  [WARN] session %s: error checking Service %s: %v\n", sessionID, serviceName, err)
		return false, false
	}
	if serviceExists {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: Service %s found\n", sessionID, serviceName)
		}
		return true, true
	}
	secretName := fmt.Sprintf("agentapi-session-%s-settings", sessionID)
	settingsExist, err := resourceExists(func() error {
		_, getErr := s.client.CoreV1().Secrets(s.namespace).Get(s.ctx, secretName, metav1.GetOptions{})
		return getErr
	})
	if err != nil {
		fmt.Printf("  [WARN] session %s: error checking settings Secret %s: %v\n", sessionID, secretName, err)
		return false, false
	}
	if settingsExist && orphanedVerbose {
		fmt.Printf("  [ACTIVE] session %s: settings Secret %s found\n", sessionID, secretName)
	}
	return settingsExist, true
}

func (s *orphanScanner) inspectProvisionSecret(secret corev1.Secret) {
	sessionID := secret.Labels["agentapi.proxy/session-id"]
	if sessionID == "" {
		if orphanedVerbose {
			fmt.Printf("  [SKIP] Secret %s: missing agentapi.proxy/session-id label\n", secret.Name)
		}
		return
	}
	if s.alreadySeen(sessionID, "earlier pass") {
		return
	}
	if related, checked := s.relatedProvisionResourcesExist(sessionID); !checked || related {
		return
	}
	missing, active := s.workloadMissing(sessionID)
	if active && oldEnoughToPrune(&secret) {
		s.record(sessionID, "provision-request-secret", "  [ORPHANED] session %s: provision request has no Service/settings and is older than %s (detected via Provision Request Secret)\n", sessionID, orphanedMinAge)
		return
	}
	if active {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: workload agentapi-session-%s found and provision request is newer than %s\n", sessionID, sessionID, orphanedMinAge)
		}
		return
	}
	if missing {
		s.record(sessionID, "provision-request-secret", "  [ORPHANED] session %s: provision request remains but Service/settings/workload are gone (detected via Provision Request Secret)\n", sessionID)
	}
}

func (s *orphanScanner) scanProvisionRequests() error {
	secrets, err := s.client.CoreV1().Secrets(s.namespace).List(s.ctx, metav1.ListOptions{LabelSelector: "agentapi.proxy/provision-request=true"})
	if err != nil {
		return fmt.Errorf("failed to list provision request secrets: %w", err)
	}
	fmt.Printf("Pass 3 (Provision Request Secret scan): found %d provision request secret(s)\n", len(secrets.Items))
	for _, secret := range secrets.Items {
		s.inspectProvisionSecret(secret)
	}
	return nil
}

func (s *orphanScanner) inspectDeployment(deployment appsv1.Deployment) {
	sessionID := deployment.Labels["agentapi.proxy/session-id"]
	if sessionID == "" {
		if orphanedVerbose {
			fmt.Printf("  [SKIP] Deployment %s: missing agentapi.proxy/session-id label\n", deployment.Name)
		}
		return
	}
	if s.alreadySeen(sessionID, "earlier pass") {
		return
	}
	exists, err := serviceExistsForSession(s.ctx, s.client, s.namespace, sessionID)
	if err != nil {
		fmt.Printf("  [WARN] session %s: error checking Service: %v\n", sessionID, err)
		return
	}
	if exists {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: Service found for Deployment %s\n", sessionID, deployment.Name)
		}
		return
	}
	if deploymentWorkloadActive(deployment) && !oldEnoughToPrune(&deployment) {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: Deployment %s has no Service but is newer than %s\n", sessionID, deployment.Name, orphanedMinAge)
		}
		return
	}
	s.record(sessionID, "workload", "  [ORPHANED] session %s: Deployment %s has no Service (detected via workload)\n", sessionID, deployment.Name)
}

func (s *orphanScanner) inspectPod(pod corev1.Pod) {
	sessionID := pod.Labels["agentapi.proxy/session-id"]
	if sessionID == "" {
		if orphanedVerbose {
			fmt.Printf("  [SKIP] Pod %s: missing agentapi.proxy/session-id label\n", pod.Name)
		}
		return
	}
	if pod.Name != fmt.Sprintf("agentapi-session-%s", sessionID) {
		if orphanedVerbose {
			fmt.Printf("  [SKIP] Pod %s: not a standalone session Pod\n", pod.Name)
		}
		return
	}
	if s.alreadySeen(sessionID, "earlier pass") {
		return
	}
	exists, err := serviceExistsForSession(s.ctx, s.client, s.namespace, sessionID)
	if err != nil {
		fmt.Printf("  [WARN] session %s: error checking Service: %v\n", sessionID, err)
		return
	}
	if exists {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: Service found for Pod %s\n", sessionID, pod.Name)
		}
		return
	}
	if podWorkloadActive(pod) && !oldEnoughToPrune(&pod) {
		if orphanedVerbose {
			fmt.Printf("  [ACTIVE] session %s: Pod %s has no Service but is newer than %s\n", sessionID, pod.Name, orphanedMinAge)
		}
		return
	}
	s.record(sessionID, "workload", "  [ORPHANED] session %s: Pod %s has no Service (detected via workload)\n", sessionID, pod.Name)
}

func (s *orphanScanner) scanWorkloads() error {
	selector := "app.kubernetes.io/name=agentapi-session,app.kubernetes.io/managed-by=agentapi-proxy"
	deployments, err := s.client.AppsV1().Deployments(s.namespace).List(s.ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("failed to list session deployments: %w", err)
	}
	pods, err := s.client.CoreV1().Pods(s.namespace).List(s.ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("failed to list session pods: %w", err)
	}
	fmt.Printf("Pass 4 (Workload scan): found %d deployment(s), %d pod(s)\n", len(deployments.Items), len(pods.Items))
	for _, deployment := range deployments.Items {
		s.inspectDeployment(deployment)
	}
	for _, pod := range pods.Items {
		s.inspectPod(pod)
	}
	return nil
}

func (s *orphanScanner) scan() ([]orphanedSession, error) {
	passes := []func() error{s.scanServices, s.scanSettingsSecrets, s.scanProvisionRequests, s.scanWorkloads}
	for _, pass := range passes {
		if err := pass(); err != nil {
			return nil, err
		}
	}
	return s.orphaned, nil
}
