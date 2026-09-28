package controllers

import (
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

var secretEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type secretSettingRequest struct {
	Name        string                      `json:"name"`
	Values      map[string]string           `json:"values"`
	Projections []entities.SecretProjection `json:"projections,omitempty"`
	BaseVersion int64                       `json:"base_version,omitempty"`
}

type secretSettingPatchRequest struct {
	Name        *string                      `json:"name,omitempty"`
	Projections *[]entities.SecretProjection `json:"projections,omitempty"`
	BaseVersion int64                        `json:"base_version"`
}

type secretValuesRequest struct {
	Values      map[string]string `json:"values"`
	BaseVersion int64             `json:"base_version"`
}

type secretSettingResponse struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Keys        []string                    `json:"keys"`
	Projections []entities.SecretProjection `json:"projections,omitempty"`
	Version     int64                       `json:"version"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type secretSettingsResponse struct {
	Secrets []secretSettingResponse `json:"secrets"`
}

func (c *SettingsController) ListSecrets(ctx echo.Context) error {
	settings, err := c.authorizedSecretSettings(ctx, false, false)
	if err != nil {
		return err
	}
	result := make([]secretSettingResponse, 0, len(settings.SecretSettings()))
	for _, secret := range settings.SecretSettings() {
		result = append(result, toSecretSettingResponse(secret))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusOK, secretSettingsResponse{Secrets: result})
}

func (c *SettingsController) CreateSecret(ctx echo.Context) error {
	settings, err := c.authorizedSecretSettings(ctx, true, true)
	if err != nil {
		return err
	}
	var req secretSettingRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Values) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "name and at least one value are required")
	}
	if err := validateSecretSetting(req.Values, req.Projections); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	secret := entities.SecretSetting{ID: "sec_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Name: strings.TrimSpace(req.Name), Values: req.Values, Projections: req.Projections, Version: 1, CreatedAt: now, UpdatedAt: now}
	values := append(settings.SecretSettings(), secret)
	settings.SetSecretSettings(values)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save secret")
	}
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusCreated, toSecretSettingResponse(secret))
}

func (c *SettingsController) UpdateSecret(ctx echo.Context) error {
	settings, err := c.authorizedSecretSettings(ctx, true, false)
	if err != nil {
		return err
	}
	var req secretSettingPatchRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	values := settings.SecretSettings()
	index := findSecretSetting(values, ctx.Param("secretId"))
	if index < 0 {
		return echo.NewHTTPError(http.StatusNotFound, "secret not found")
	}
	secret := values[index]
	if req.BaseVersion != secret.Version {
		return echo.NewHTTPError(http.StatusConflict, "secret version conflict")
	}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "name must not be empty")
		}
		secret.Name = strings.TrimSpace(*req.Name)
	}
	if req.Projections != nil {
		if err := validateSecretSetting(secret.Values, *req.Projections); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		secret.Projections = *req.Projections
	}
	secret.Version++
	secret.UpdatedAt = time.Now().UTC()
	values[index] = secret
	settings.SetSecretSettings(values)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update secret")
	}
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusOK, toSecretSettingResponse(secret))
}

func (c *SettingsController) UpdateSecretValues(ctx echo.Context) error {
	settings, err := c.authorizedSecretSettings(ctx, true, false)
	if err != nil {
		return err
	}
	var req secretValuesRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	values := settings.SecretSettings()
	index := findSecretSetting(values, ctx.Param("secretId"))
	if index < 0 {
		return echo.NewHTTPError(http.StatusNotFound, "secret not found")
	}
	secret := values[index]
	if req.BaseVersion != secret.Version {
		return echo.NewHTTPError(http.StatusConflict, "secret version conflict")
	}
	if len(req.Values) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one value is required")
	}
	if err := validateSecretSetting(req.Values, secret.Projections); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	secret.Values = req.Values
	secret.Version++
	secret.UpdatedAt = time.Now().UTC()
	values[index] = secret
	settings.SetSecretSettings(values)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update secret values")
	}
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusOK, toSecretSettingResponse(secret))
}

func (c *SettingsController) DeleteSecret(ctx echo.Context) error {
	settings, err := c.authorizedSecretSettings(ctx, true, false)
	if err != nil {
		return err
	}
	values := settings.SecretSettings()
	index := findSecretSetting(values, ctx.Param("secretId"))
	if index < 0 {
		return echo.NewHTTPError(http.StatusNotFound, "secret not found")
	}
	values = append(values[:index], values[index+1:]...)
	settings.SetSecretSettings(values)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete secret")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SettingsController) authorizedSecretSettings(ctx echo.Context, modify, create bool) (*entities.Settings, error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	name := ctx.Param("name")
	allowed := c.canAccess(user, name)
	if modify {
		allowed = c.canModify(user, name)
	}
	if !allowed {
		return nil, echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		if create {
			return entities.NewSettings(name), nil
		}
		return nil, echo.NewHTTPError(http.StatusNotFound, "settings not found")
	}
	return settings, nil
}

func findSecretSetting(values []entities.SecretSetting, id string) int {
	for i := range values {
		if values[i].ID == id {
			return i
		}
	}
	return -1
}

func toSecretSettingResponse(secret entities.SecretSetting) secretSettingResponse {
	keys := make([]string, 0, len(secret.Values))
	for key := range secret.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return secretSettingResponse{ID: secret.ID, Name: secret.Name, Keys: keys, Projections: secret.Projections, Version: secret.Version, CreatedAt: secret.CreatedAt, UpdatedAt: secret.UpdatedAt}
}

func validateSecretSetting(values map[string]string, projections []entities.SecretProjection) error {
	for key := range values {
		if strings.TrimSpace(key) == "" || len(key) > 128 {
			return echo.NewHTTPError(http.StatusBadRequest, "secret keys must be between 1 and 128 characters")
		}
	}
	seen := make(map[string]bool)
	for _, projection := range projections {
		if _, ok := values[projection.Key]; !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "projection references an unknown key")
		}
		if seen[projection.Key] {
			return echo.NewHTTPError(http.StatusBadRequest, "a key may only have one projection")
		}
		seen[projection.Key] = true
		switch projection.Type {
		case "env":
			if !secretEnvNamePattern.MatchString(projection.EnvName) {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid environment variable name")
			}
		case "file":
			clean := filepath.Clean(projection.Path)
			if !filepath.IsAbs(clean) || strings.HasPrefix(clean, "/proc/") || strings.HasPrefix(clean, "/sys/") || strings.HasPrefix(clean, "/dev/") {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid secret file path")
			}
			if projection.Permissions != "" && projection.Permissions != "0400" && projection.Permissions != "0600" {
				return echo.NewHTTPError(http.StatusBadRequest, "secret file permissions must be 0400 or 0600")
			}
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "projection type must be env or file")
		}
	}
	return nil
}
