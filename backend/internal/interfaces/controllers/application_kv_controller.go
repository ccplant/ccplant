package controllers

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

type ApplicationKVController struct {
	store  kvstore.Store
	tokens []string
}

func NewApplicationKVController(store kvstore.Store, tokens ...string) *ApplicationKVController {
	return &ApplicationKVController{store: store, tokens: tokens}
}

func (c *ApplicationKVController) Handle(ctx echo.Context) error {
	if !c.authorized(ctx.Request().Header.Get(echo.HeaderAuthorization)) {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	switch ctx.Request().Method {
	case http.MethodPost:
		var record kvstore.Record
		if err := ctx.Bind(&record); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		created, err := c.store.Create(ctx.Request().Context(), record)
		return c.recordResult(ctx, created, err)
	case http.MethodPut:
		var record kvstore.Record
		if err := ctx.Bind(&record); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		updated, err := c.store.Update(ctx.Request().Context(), record)
		return c.recordResult(ctx, updated, err)
	case http.MethodDelete:
		version, err := strconv.ParseInt(ctx.QueryParam("version"), 10, 64)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "version is required"})
		}
		err = c.store.Delete(ctx.Request().Context(), kvstore.Kind(ctx.QueryParam("kind")), ctx.QueryParam("namespace"), ctx.QueryParam("key"), version)
		if err != nil {
			return c.storeError(ctx, err)
		}
		return ctx.NoContent(http.StatusNoContent)
	case http.MethodGet:
		if key := ctx.QueryParam("key"); key != "" {
			record, err := c.store.Get(ctx.Request().Context(), kvstore.Kind(ctx.QueryParam("kind")), ctx.QueryParam("namespace"), key)
			return c.recordResult(ctx, record, err)
		}
		records, err := c.store.List(ctx.Request().Context(), kvstore.Query{Kind: kvstore.Kind(ctx.QueryParam("kind")), Namespace: ctx.QueryParam("namespace"), LabelSelector: ctx.QueryParam("label_selector"), KeyPrefix: ctx.QueryParam("key_prefix")})
		if err != nil {
			return c.storeError(ctx, err)
		}
		return ctx.JSON(http.StatusOK, records)
	default:
		return ctx.NoContent(http.StatusMethodNotAllowed)
	}
}

func (c *ApplicationKVController) authorized(header string) bool {
	provided, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return false
	}
	for _, token := range c.tokens {
		if token != "" && len(provided) == len(token) && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (c *ApplicationKVController) recordResult(ctx echo.Context, record kvstore.Record, err error) error {
	if err != nil {
		return c.storeError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, record)
}

func (c *ApplicationKVController) storeError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, kvstore.ErrNotFound):
		return ctx.NoContent(http.StatusNotFound)
	case errors.Is(err, kvstore.ErrConflict):
		return ctx.NoContent(http.StatusConflict)
	default:
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
