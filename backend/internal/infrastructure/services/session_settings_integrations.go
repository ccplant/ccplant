package services

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) slackIntegration() {
	ctx := b.ctx
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// Slack integration: embed SlackParams so the provisioner can launch
	// acp-posts as a subprocess. This enables stock sessions (which have no
	// slack-integration sidecar) to forward agent output to Slack.
	// Use per-bot token secret if provided, fall back to server default.
	if req.SlackParams != nil && req.SlackParams.Channel != "" {
		slackSecretName := req.SlackParams.BotTokenSecretName
		if slackSecretName == "" {
			slackSecretName = m.k8sConfig.SlackBotTokenSecretName
		}
		if slackSecretName != "" {
			botTokenSecretKey := req.SlackParams.BotTokenSecretKey
			if botTokenSecretKey == "" {
				botTokenSecretKey = m.k8sConfig.SlackBotTokenSecretKey
			}
			if botTokenSecretKey == "" {
				botTokenSecretKey = defaultSlackBotTokenSecretKey
			}
			client, namespace := m.client, m.namespace
			if m.slackTokenClient != nil {
				client, namespace = m.slackTokenClient, m.slackTokenNamespace
			}
			secret, err := client.CoreV1().Secrets(namespace).Get(
				ctx,
				slackSecretName,
				metav1.GetOptions{},
			)
			if err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to read Slack bot token secret %s for session %s: %v",
					slackSecretName, session.id, err)
			} else {
				botToken := string(secret.Data[botTokenSecretKey])
				if botToken != "" {
					settings.SlackParams = &sessionsettings.SlackParams{
						Channel:  req.SlackParams.Channel,
						ThreadTS: req.SlackParams.ThreadTS,
						BotToken: botToken,
					}
					log.Printf("[K8S_SESSION] SlackParams embedded in session settings for session %s (channel: %s, secret: %s)",
						session.id, req.SlackParams.Channel, slackSecretName)
				} else {
					log.Printf("[K8S_SESSION] Warning: Slack bot token secret %s key %s is empty for session %s",
						slackSecretName, botTokenSecretKey, session.id)
				}
			}
		}
	}
}

func (b *sessionSettingsBuilder) telemetry() {
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// OtelCollector in-process config: otelcol always runs as a subprocess inside
	// the agentapi container, started by the provisioner after user context is known.
	// This ensures metrics labels are correct even with the stock inventory feature.
	if m.k8sConfig.OtelCollectorEnabled {
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

		scheduleID := "-"
		webhookID := "-"
		if req.Tags != nil {
			if v := req.Tags["schedule_id"]; v != "" {
				scheduleID = v
			}
			if v := req.Tags["webhook_id"]; v != "" {
				webhookID = v
			}
		}
		agentType := req.AgentType
		if agentType == "" {
			agentType = "-"
		}
		teamID := "-"
		if req.Scope == entities.ScopeTeam && req.TeamID != "" {
			teamID = req.TeamID
		}

		settings.OtelCollector = &sessionsettings.OtelCollectorConfig{
			Enabled:        true,
			ScrapeInterval: scrapeInterval,
			ClaudeCodePort: claudeCodePort,
			ExporterPort:   exporterPort,
			SessionID:      session.id,
			UserID:         req.UserID,
			TeamID:         teamID,
			ScheduleID:     scheduleID,
			WebhookID:      webhookID,
			AgentType:      agentType,
		}
		log.Printf("[K8S_SESSION] OtelCollector in-process config embedded for session %s", session.id)
	}
}
