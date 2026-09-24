package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestVerifyCustomWebhookStaticTokenFailsClosed(t *testing.T) {
	controller := &WebhookCustomController{}
	webhook := entities.NewWebhook("webhook", "test", "owner", entities.WebhookTypeCustom)
	webhook.SetSignatureType(entities.WebhookSignatureTypeStatic)
	webhook.SetSignatureHeader("X-Test-Token")
	webhook.SetSecret("correct")

	for _, header := range []string{"", "wrong"} {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/hooks/custom/webhook", nil)
		if header != "" {
			req.Header.Set("X-Test-Token", header)
		}
		err := controller.verifyWebhookSignature(e.NewContext(req, httptest.NewRecorder()), nil, webhook)
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: error = %#v, want HTTP 401", header, err)
		}
	}
}
