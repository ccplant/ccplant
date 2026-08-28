package controllers

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

func (c *SettingsController) GetExternalSessionManagerOperationalStatus(ctx echo.Context) error {
	return c.proxyExternalSessionManagerOperation(ctx, http.MethodGet, "/internal/esm-management/status", nil)
}

func (c *SettingsController) GetExternalSessionManagerLogs(ctx echo.Context) error {
	tail, err := strconv.Atoi(ctx.QueryParam("tail"))
	if err != nil || tail < 1 || tail > 5000 {
		tail = 200
	}
	path := "/internal/esm-management/logs?" + url.Values{"tail": {strconv.Itoa(tail)}}.Encode()
	return c.proxyExternalSessionManagerOperation(ctx, http.MethodGet, path, nil)
}

func (c *SettingsController) RestartExternalSessionManager(ctx echo.Context) error {
	return c.proxyExternalSessionManagerOperation(ctx, http.MethodPost, "/internal/esm-management/restart", []byte("{}"))
}

func (c *SettingsController) UpgradeExternalSessionManager(ctx echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, 64*1024))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid upgrade request")
	}
	return c.proxyExternalSessionManagerOperation(ctx, http.MethodPost, "/internal/esm-management/upgrade", body)
}

func (c *SettingsController) proxyExternalSessionManagerOperation(ctx echo.Context, method, path string, body []byte) error {
	manager, _, err := c.findAuthorizedESM(ctx, method != http.MethodGet)
	if err != nil {
		return err
	}
	if manager.PublicURL == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager public URL is unavailable")
	}
	target := strings.TrimRight(manager.PublicURL, "/") + path
	req, err := http.NewRequestWithContext(ctx.Request().Context(), method, target, bytes.NewReader(body))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create operation request")
	}
	req.Header.Set("Content-Type", "application/json")
	ts := hmacutil.NowTimestamp()
	message := hmacutil.BuildMessage(method, req.URL.RequestURI(), ts, body)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(manager.HMACSecret), message))
	req.Header.Set(hmacutil.TimestampHeader, ts)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	for key, values := range resp.Header {
		for _, value := range values {
			ctx.Response().Header().Add(key, value)
		}
	}
	ctx.Response().WriteHeader(resp.StatusCode)
	_, err = io.Copy(ctx.Response(), resp.Body)
	return err
}
