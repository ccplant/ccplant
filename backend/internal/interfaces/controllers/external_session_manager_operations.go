package controllers

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"
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
	if c.esmControlTunnel == nil || !c.esmControlTunnel.IsConnected(ctx.Request().Context(), manager.ID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}
	req, err := http.NewRequestWithContext(ctx.Request().Context(), method, "http://esm"+path, bytes.NewReader(body))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create operation request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.esmControlTunnel.Do(ctx.Request().Context(), manager.ID, "", "", req)
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
