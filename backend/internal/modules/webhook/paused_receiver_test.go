package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type pausedWebhookRepository struct {
	webhook       *entities.Webhook
	deliveryCount int
}

func (r *pausedWebhookRepository) Create(context.Context, *entities.Webhook) error { return nil }
func (r *pausedWebhookRepository) Get(context.Context, string) (*entities.Webhook, error) {
	return r.webhook, nil
}
func (r *pausedWebhookRepository) List(context.Context, repositories.WebhookFilter) ([]*entities.Webhook, error) {
	return nil, nil
}
func (r *pausedWebhookRepository) Update(context.Context, *entities.Webhook) error { return nil }
func (r *pausedWebhookRepository) Delete(context.Context, string) error            { return nil }
func (r *pausedWebhookRepository) FindByGitHubRepository(context.Context, repositories.GitHubMatcher) ([]*entities.Webhook, error) {
	return nil, nil
}
func (r *pausedWebhookRepository) RecordDelivery(context.Context, string, *entities.WebhookDeliveryRecord) error {
	r.deliveryCount++
	return nil
}

func TestPausedGitHubWebhookDoesNotProcessDelivery(t *testing.T) {
	const secret = "github-secret"
	body := []byte(`{"repository":{"full_name":"owner/repo"}}`)
	webhook := entities.NewWebhook("webhook-1", "paused", "owner", entities.WebhookTypeGitHub)
	webhook.SetSecret(secret)
	webhook.SetStatus(entities.WebhookStatusPaused)
	repo := &pausedWebhookRepository{webhook: webhook}
	controller := NewWebhookGitHubController(repo, nil, nil)

	h := hmac.New(sha256.New, []byte(secret))
	_, err := h.Write(body)
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/github/webhook-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(h.Sum(nil)))
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath("/hooks/github/:id")
	ctx.SetParamNames("id")
	ctx.SetParamValues("webhook-1")

	require.NoError(t, controller.HandleGitHubWebhook(ctx))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Webhook is paused")
	assert.Zero(t, repo.deliveryCount)
}

func TestPausedCustomWebhookDoesNotProcessInvalidPayload(t *testing.T) {
	const secret = "custom-secret"
	webhook := entities.NewWebhook("webhook-1", "paused", "owner", entities.WebhookTypeCustom)
	webhook.SetSecret(secret)
	webhook.SetSignatureType(entities.WebhookSignatureTypeStatic)
	webhook.SetSignatureHeader("X-Test-Token")
	webhook.SetStatus(entities.WebhookStatusPaused)
	repo := &pausedWebhookRepository{webhook: webhook}
	controller := NewWebhookCustomController(repo, nil, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/custom/webhook-1", bytes.NewBufferString("not-json"))
	req.Header.Set("X-Test-Token", secret)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath("/hooks/custom/:id")
	ctx.SetParamNames("id")
	ctx.SetParamValues("webhook-1")

	require.NoError(t, controller.HandleCustomWebhook(ctx))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Webhook is paused")
	assert.Zero(t, repo.deliveryCount)

	var response map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, "webhook-1", response["webhook_id"])
}
