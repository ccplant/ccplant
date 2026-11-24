package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func newSessionRunnerToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func verifySessionRunnerToken(expectedHash, token string) bool {
	decoded, err := hex.DecodeString(expectedHash)
	if err != nil || len(decoded) != sha256.Size || token == "" {
		return false
	}
	candidate := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(decoded, candidate[:]) == 1
}

func bearerToken(req *http.Request) string {
	const prefix = "Bearer "
	header := req.Header.Get(echo.HeaderAuthorization)
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

func validatePoolSubject(kind core.SubjectType, id string) error {
	if (kind != core.SubjectUser && kind != core.SubjectTeam) || strings.TrimSpace(id) == "" {
		return errors.New("subject_type must be user or team and subject_id is required")
	}
	return nil
}

func validatePoolBindingSubject(kind core.SubjectType, id string) error {
	if kind == core.SubjectAll {
		if strings.TrimSpace(id) != "" {
			return errors.New("subject_id must be empty when subject_type is all")
		}
		return nil
	}
	return validatePoolSubject(kind, id)
}

func validatePoolBindingRole(kind core.SubjectType, binding *core.Binding) error {
	for _, role := range binding.Roles {
		if role != core.BindingRoleUse && role != core.BindingRoleManage {
			return errors.New("roles may only contain use and manage")
		}
	}
	role := binding.Role
	if role != core.BindingRoleUse && role != core.BindingRoleManage && role != core.BindingRoleManageAndUse {
		return errors.New("at least one of use or manage role is required")
	}
	if kind == core.SubjectAll && role.GrantsManage() {
		return errors.New("all binding cannot have manage role")
	}
	return nil
}

func (c *SessionPoolController) requirePoolManage(ctx echo.Context, pool string) error {
	user := auth.GetUserFromContext(ctx)
	// Admin routes already enforce authentication in middleware. Keeping nil
	// permissive also supports internal controller calls without an HTTP identity.
	if user == nil || user.IsAdmin() {
		return nil
	}
	ok, err := c.canManagePool(ctx.Request().Context(), user, pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "pool manage binding required")
	}
	return nil
}

func (c *SessionPoolController) canManagePool(ctx context.Context, user *entities.User, pool string) (bool, error) {
	if user == nil || user.IsAdmin() {
		return true, nil
	}
	bindings, err := c.store.ListBindings(ctx, pool)
	if err != nil {
		return false, err
	}
	for _, binding := range bindings {
		if !binding.Enabled || !binding.GrantsManage() {
			continue
		}
		if binding.SubjectType == core.SubjectUser && binding.SubjectID == string(user.ID()) {
			return true, nil
		}
		if binding.SubjectType == core.SubjectTeam && user.IsMemberOfTeam(binding.SubjectID) {
			return true, nil
		}
	}
	return false, nil
}

func (c *SessionPoolController) canAccessPool(ctx context.Context, user *entities.User, pool string) (bool, error) {
	if user == nil || user.IsAdmin() {
		return true, nil
	}
	bindings, err := c.store.ListBindings(ctx, pool)
	if err != nil {
		return false, err
	}
	for _, binding := range bindings {
		if bindingAppliesToUser(binding, user) && (binding.SubjectType != core.SubjectAll || binding.Enabled) {
			return true, nil
		}
	}
	return false, nil
}

func bindingAppliesToUser(binding *core.Binding, user *entities.User) bool {
	if binding == nil || user == nil {
		return false
	}
	switch binding.SubjectType {
	case core.SubjectAll:
		return binding.SubjectID == ""
	case core.SubjectUser:
		return binding.SubjectID == string(user.ID())
	case core.SubjectTeam:
		return user.IsMemberOfTeam(binding.SubjectID)
	default:
		return false
	}
}

func poolSubjectForUser(user *entities.User) core.Subject {
	if user.UserType() == entities.UserTypeServiceAccount && user.TeamID() != "" {
		return core.Subject{Type: core.SubjectTeam, ID: user.TeamID()}
	}
	return core.Subject{Type: core.SubjectUser, ID: string(user.ID())}
}

func parseRunnerWait(raw string) time.Duration {
	if raw == "" {
		return 30 * time.Second
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		if seconds, parseErr := strconv.Atoi(raw); parseErr == nil {
			duration = time.Duration(seconds) * time.Second
		}
	}
	if duration < 0 {
		return 0
	}
	if duration > 30*time.Second {
		return 30 * time.Second
	}
	return duration
}

func sessionRunnerStoreError(err error) error {
	switch {
	case errors.Is(err, core.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, core.ErrConflict):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, core.ErrUnauthorized):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
}
