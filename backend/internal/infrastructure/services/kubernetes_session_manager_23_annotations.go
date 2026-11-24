package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (m *KubernetesSessionManager) ensureOtelcolConfigMap(ctx context.Context) error {
	if !m.k8sConfig.OtelCollectorEnabled {
		return nil
	}

	configMapName := "otelcol-config"
	scrapeInterval := "15s"
	if m.k8sConfig.OtelCollectorScrapeInterval != "" {
		scrapeInterval = m.k8sConfig.OtelCollectorScrapeInterval
	}
	claudeCodePort := 9464
	if m.k8sConfig.OtelCollectorClaudeCodePort > 0 {
		claudeCodePort = m.k8sConfig.OtelCollectorClaudeCodePort
	}
	exporterPort := 9090
	if m.k8sConfig.OtelCollectorExporterPort > 0 {
		exporterPort = m.k8sConfig.OtelCollectorExporterPort
	}

	otelConfig := fmt.Sprintf(`receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: 'claude-code'
          scrape_interval: %s
          static_configs:
            - targets: ['localhost:%d']

processors:
  resource:
    attributes:
      - key: user_id
        action: delete
      - key: session_id
        action: delete
  transform:
    error_mode: ignore
    metric_statements:
      - context: datapoint
        statements:
          # Rename claude-code's native labels
          - set(attributes["claude_user_id"], attributes["user_id"]) where attributes["user_id"] != nil
          - set(attributes["claude_session_id"], attributes["session_id"]) where attributes["session_id"] != nil
          - delete_key(attributes, "user_id")
          - delete_key(attributes, "session_id")
          # Remove user_email label to prevent it from being scraped by Prometheus
          - delete_key(attributes, "user_email")
          # Add agentapi labels
          - set(attributes["agentapi_session_id"], "${env:SESSION_ID}")
          - set(attributes["agentapi_user_id"], "${env:USER_ID}")
          - set(attributes["agentapi_team_id"], "${env:TEAM_ID}")
          - set(attributes["agentapi_schedule_id"], "${env:SCHEDULE_ID}")
          - set(attributes["agentapi_webhook_id"], "${env:WEBHOOK_ID}")
          - set(attributes["agentapi_agent_type"], "${env:AGENT_TYPE}")

exporters:
  prometheus:
    endpoint: "0.0.0.0:%d"
    resource_to_telemetry_conversion:
      enabled: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [resource, transform]
      exporters: [prometheus]`, scrapeInterval, claudeCodePort, exporterPort)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "otelcol",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/component":  "telemetry",
			},
		},
		Data: map[string]string{
			"otel-collector-config.yaml": otelConfig,
		},
	}

	// Try to get existing ConfigMap
	existingCM, err := m.client.CoreV1().ConfigMaps(m.namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			_, err = m.client.CoreV1().ConfigMaps(m.namespace).Create(ctx, configMap, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create otelcol ConfigMap: %w", err)
			}
			log.Printf("[K8S_SESSION] Created otelcol ConfigMap: %s", configMapName)
			return nil
		}
		return fmt.Errorf("failed to get otelcol ConfigMap: %w", err)
	}

	// Update existing ConfigMap
	existingCM.Data = configMap.Data
	existingCM.Labels = configMap.Labels
	_, err = m.client.CoreV1().ConfigMaps(m.namespace).Update(ctx, existingCM, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update otelcol ConfigMap: %w", err)
	}
	log.Printf("[K8S_SESSION] Updated otelcol ConfigMap: %s", configMapName)
	return nil
}

// buildServicePorts builds the service ports for the session
func (m *KubernetesSessionManager) buildServicePorts(_ *KubernetesSession) []corev1.ServicePort {
	var ports []corev1.ServicePort

	// Add metrics port if otelcol is enabled
	if m.k8sConfig.OtelCollectorEnabled {
		exporterPort := 9090
		if m.k8sConfig.OtelCollectorExporterPort > 0 {
			exporterPort = m.k8sConfig.OtelCollectorExporterPort
		}
		ports = append(ports, corev1.ServicePort{
			Name:       "metrics",
			Port:       int32(exporterPort),
			TargetPort: intstr.FromInt(exporterPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return ports
}

const (
	sessionAnnotationPRURL       = "agentapi.proxy/session-annotation-pr-url"
	sessionAnnotationIssueURL    = "agentapi.proxy/session-annotation-issue-url"
	sessionAnnotationDescription = "agentapi.proxy/session-annotation-description"
	sessionAnnotationRunningTask = "agentapi.proxy/session-annotation-running-task"
)

func sessionAnnotationsFromMap(annotations map[string]string) entities.SessionAnnotations {
	if annotations == nil {
		return entities.SessionAnnotations{}
	}
	return entities.SessionAnnotations{
		PRURL:       annotations[sessionAnnotationPRURL],
		IssueURL:    annotations[sessionAnnotationIssueURL],
		Description: annotations[sessionAnnotationDescription],
		RunningTask: annotations[sessionAnnotationRunningTask],
	}
}

func applySessionAnnotationPatch(current entities.SessionAnnotations, patch entities.UpdateSessionAnnotationsRequest) entities.SessionAnnotations {
	if patch.PRURL != nil {
		current.PRURL = *patch.PRURL
	}
	if patch.IssueURL != nil {
		current.IssueURL = *patch.IssueURL
	}
	if patch.Description != nil {
		current.Description = *patch.Description
	}
	if patch.RunningTask != nil {
		current.RunningTask = *patch.RunningTask
	}
	return current
}

func setSessionAnnotationValue(annotations map[string]string, key, value string) {
	if value == "" {
		delete(annotations, key)
		return
	}
	annotations[key] = value
}

// UpdateSessionAnnotations updates user-managed annotations on a session's Service.
func (m *KubernetesSessionManager) UpdateSessionAnnotations(ctx context.Context, sessionID string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error) {
	session := m.GetSession(sessionID)
	if session == nil {
		return entities.SessionAnnotations{}, fmt.Errorf("session not found: %s", sessionID)
	}

	ks, ok := session.(*KubernetesSession)
	if !ok {
		return entities.SessionAnnotations{}, fmt.Errorf("session is not a KubernetesSession")
	}

	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return entities.SessionAnnotations{}, fmt.Errorf("failed to get service: %w", err)
	}
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}

	updated := applySessionAnnotationPatch(sessionAnnotationsFromMap(svc.Annotations), patch)
	setSessionAnnotationValue(svc.Annotations, sessionAnnotationPRURL, updated.PRURL)
	setSessionAnnotationValue(svc.Annotations, sessionAnnotationIssueURL, updated.IssueURL)
	setSessionAnnotationValue(svc.Annotations, sessionAnnotationDescription, updated.Description)
	setSessionAnnotationValue(svc.Annotations, sessionAnnotationRunningTask, updated.RunningTask)

	if _, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		return entities.SessionAnnotations{}, fmt.Errorf("failed to update service annotations: %w", err)
	}

	ks.SetAnnotations(updated)
	if m.sessionListCacheRepo != nil {
		if err := m.sessionListCacheRepo.InvalidateSessionListCache(ctx, m.namespace); err != nil {
			log.Printf("[SESSION] Failed to invalidate session list cache after annotation update for %s: %v", sessionID, err)
		}
	}
	return updated, nil
}

// UpdateServiceAnnotation updates a specific annotation on a session's Service
func (m *KubernetesSessionManager) UpdateServiceAnnotation(ctx context.Context, sessionID, key, value string) error {
	session := m.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	ks, ok := session.(*KubernetesSession)
	if !ok {
		return fmt.Errorf("session is not a KubernetesSession")
	}

	serviceName := ks.ServiceName()

	// Get the current Service
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get service: %w", err)
	}

	// Update the annotation
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}
	svc.Annotations[key] = value

	// Update the Service
	_, err = m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service annotation: %w", err)
	}

	return nil
}
