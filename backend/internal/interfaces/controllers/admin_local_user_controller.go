package controllers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type LocalUserTokenAuth interface {
	LoadAPIToken(ctx context.Context, token *entities.APIToken) error
}

type AdminLocalUserController struct {
	users  portrepos.LocalUserRepository
	tokens portrepos.APITokenRepository
	auth   LocalUserTokenAuth
}

func NewAdminLocalUserController(users portrepos.LocalUserRepository, tokens portrepos.APITokenRepository, authService LocalUserTokenAuth) *AdminLocalUserController {
	return &AdminLocalUserController{users: users, tokens: tokens, auth: authService}
}

type createLocalUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
}

func (c *AdminLocalUserController) Create(ctx echo.Context) error {
	var req createLocalUserRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	role := entities.Role(req.Role)
	if role == "" {
		role = entities.RoleUser
	}
	caller := auth.GetUserFromContext(ctx)
	user, err := entities.NewLocalUser(req.Username, req.DisplayName, req.Email, role, caller.ID())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.users.Create(ctx.Request().Context(), user); err != nil {
		if errors.Is(err, entities.ErrLocalUserAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "local user already exists")
		}
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to persist local user")
	}
	return ctx.JSON(http.StatusCreated, user)
}

func (c *AdminLocalUserController) Get(ctx echo.Context) error {
	user, err := c.users.GetByID(ctx.Request().Context(), entities.UserID(ctx.Param("id")))
	if errors.Is(err, entities.ErrLocalUserNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "local user not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to read local user")
	}
	return ctx.JSON(http.StatusOK, user)
}

type createLocalUserTokenRequest struct {
	Name      string `json:"name"`
	ExpiresIn string `json:"expires_in"`
}
type createLocalUserTokenResponse struct {
	Token          APITokenMetadata `json:"token"`
	PlaintextToken string           `json:"plaintext_token"`
}

func localUserPermissions(role entities.Role) []entities.Permission {
	p := []entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead, entities.PermissionSessionUpdate, entities.PermissionSessionDelete}
	if role == entities.RoleAdmin {
		p = append(p, entities.PermissionAdmin)
	}
	return p
}

func (c *AdminLocalUserController) CreateToken(ctx echo.Context) error {
	user, err := c.users.GetByID(ctx.Request().Context(), entities.UserID(ctx.Param("id")))
	if errors.Is(err, entities.ErrLocalUserNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "local user not found")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to read local user")
	}
	var req createLocalUserTokenRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return echo.NewHTTPError(http.StatusBadRequest, "name must be 1..64 characters")
	}
	duration := 720 * time.Hour
	if req.ExpiresIn != "" {
		duration, err = time.ParseDuration(req.ExpiresIn)
		if err != nil || duration <= 0 || duration > 8760*time.Hour {
			return echo.NewHTTPError(http.StatusBadRequest, "expires_in must be a positive duration no greater than 8760h")
		}
	}
	id, err := apitoken.GenerateTokenID()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}
	secret, err := apitoken.GenerateSecret()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}
	expires := time.Now().UTC().Add(duration)
	caller := auth.GetUserFromContext(ctx)
	token := entities.NewAPIToken(id, secret, apitoken.DisplayPrefix(secret), name, entities.APITokenScopeUser, user.ID, "", localUserPermissions(user.Role), &expires, caller.ID())
	if err := c.tokens.Create(ctx.Request().Context(), token); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to persist api token")
	}
	if c.auth != nil {
		_ = c.auth.LoadAPIToken(ctx.Request().Context(), token)
	}
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusCreated, createLocalUserTokenResponse{Token: toMetadata(*token), PlaintextToken: secret})
}

func (c *AdminLocalUserController) ListTokens(ctx echo.Context) error {
	id := entities.UserID(ctx.Param("id"))
	if _, err := c.users.GetByID(ctx.Request().Context(), id); errors.Is(err, entities.ErrLocalUserNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "local user not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to read local user")
	}
	tokens, err := c.tokens.ListByOwner(ctx.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to list api tokens")
	}
	items := make([]*APITokenMetadata, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, toMetadataPtr(t))
	}
	return ctx.JSON(http.StatusOK, &APITokenListResponse{Items: items})
}

func (c *AdminLocalUserController) DeleteToken(ctx echo.Context) error {
	id := entities.UserID(ctx.Param("id"))
	token, err := c.tokens.GetByID(ctx.Request().Context(), ctx.Param("tokenId"))
	if err != nil || token.UserID() != id {
		return echo.NewHTTPError(http.StatusNotFound, "api token not found")
	}
	if err := c.tokens.Delete(ctx.Request().Context(), token.ID()); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to delete api token")
	}
	if svc, ok := c.auth.(interface{ RevokeAPIToken(string) }); ok {
		svc.RevokeAPIToken(token.Secret())
	}
	return ctx.NoContent(http.StatusNoContent)
}
