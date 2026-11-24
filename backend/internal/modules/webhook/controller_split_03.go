package webhook

import (
	"fmt"
	"text/template"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/webhook/infra"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *WebhookController) sessionConfigToResponse(sc *entities.WebhookSessionConfig) *SessionConfigResponse {
	resp := &SessionConfigResponse{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
		SessionProfileID:       sc.SessionProfileID(),
	}
	// Exclude GithubToken from response since it is sensitive
	if params := sc.Params(); params != nil {
		resp.Params = &SessionParamsResponse{
			AgentType:        params.AgentType,
			Model:            params.Model,
			Oneshot:          params.Oneshot,
			SessionTTL:       params.SessionTTL,
			AuthProxy:        params.AuthProxy,
			CredentialSource: params.CredentialSource,
		}
	}
	return resp
}

func (c *WebhookController) getWebhookURL(ctx echo.Context, w *entities.Webhook) string {
	baseURL := c.baseURL
	if baseURL == "" {
		scheme := "https"
		if ctx.Request().TLS == nil {
			if proto := ctx.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = proto
			} else {
				scheme = "http"
			}
		}
		host := ctx.Request().Host
		if fwdHost := ctx.Request().Header.Get("X-Forwarded-Host"); fwdHost != "" {
			host = fwdHost
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	switch w.WebhookType() {
	case entities.WebhookTypeGitHub:
		return fmt.Sprintf("%s/hooks/github/%s", baseURL, w.ID())
	default:
		return fmt.Sprintf("%s/hooks/custom/%s", baseURL, w.ID())
	}
}

func (c *WebhookController) userCanAccessWebhook(ctx echo.Context, webhook *entities.Webhook) bool {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return false
	}
	return user.CanAccessResource(
		entities.UserID(webhook.UserID()),
		string(webhook.Scope()),
		webhook.TeamID(),
	)
}

func (c *WebhookController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}

// generateSecret generates a random secret for webhook signature verification
func generateSecret() string {
	return uuid.New().String() + uuid.New().String()
}

// validateWebhookTemplates validates all templates in the webhook request
func (c *WebhookController) validateWebhookTemplates(webhookType entities.WebhookType, req CreateWebhookRequest) error {
	return c.validateTemplates(webhookType, req.SessionConfig, req.Triggers)
}

// validateWebhookTemplatesForUpdate validates all templates in the webhook update request
func (c *WebhookController) validateWebhookTemplatesForUpdate(webhookType entities.WebhookType, req UpdateWebhookRequest) error {
	return c.validateTemplates(webhookType, req.SessionConfig, req.Triggers)
}

// validateTemplates validates all templates in session config and triggers.
func (c *WebhookController) validateTemplates(webhookType entities.WebhookType, sessionConfig *SessionConfigRequest, triggers []TriggerRequest) error {
	if sessionConfig != nil && sessionConfig.InitialMessageTemplate != "" {
		if err := c.validateInitialMessageTemplate(webhookType, sessionConfig.InitialMessageTemplate); err != nil {
			return fmt.Errorf("webhook session_config.initial_message_template: %w", err)
		}
	}

	for i, trigger := range triggers {
		if trigger.Conditions.GoTemplate != "" {
			if err := c.validateGoTemplateCondition(webhookType, trigger.Conditions.GoTemplate); err != nil {
				return fmt.Errorf("trigger[%d] (%s) conditions.go_template: %w", i, trigger.Name, err)
			}
		}

		if trigger.SessionConfig != nil && trigger.SessionConfig.InitialMessageTemplate != "" {
			if err := c.validateInitialMessageTemplate(webhookType, trigger.SessionConfig.InitialMessageTemplate); err != nil {
				return fmt.Errorf("trigger[%d] (%s) session_config.initial_message_template: %w", i, trigger.Name, err)
			}
		}
	}

	return nil
}

// validateGoTemplateCondition validates a GoTemplate condition string
func (c *WebhookController) validateGoTemplateCondition(webhookType entities.WebhookType, tmplStr string) error {
	// Only validate template syntax, not execution with test payload
	// This allows flexible payload structures without predefined schema
	evaluator := infra.NewGoTemplateEvaluator()
	_, err := template.New("condition").Funcs(evaluator.FuncMap()).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse failed: %w", err)
	}

	return nil
}

// validateInitialMessageTemplate validates an initial message template
func (c *WebhookController) validateInitialMessageTemplate(webhookType entities.WebhookType, tmplStr string) error {
	// Only validate template syntax, not execution with test payload
	// This allows flexible payload structures without predefined schema
	_, err := template.New("initial_message").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse failed: %w", err)
	}

	return nil
}

// Repo returns the webhook repository for external access.
func (c *WebhookController) Repo() repositories.WebhookRepository {
	return c.repo
}

// UserCanAccessWebhook checks if the current user can access the webhook.
func (c *WebhookController) UserCanAccessWebhook(ctx echo.Context, webhook *entities.Webhook) bool {
	return c.userCanAccessWebhook(ctx, webhook)
}
