package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
)

type controllerInitialMessageHistoryRepo struct {
	items   []entities.InitialMessageHistoryItem
	userID  string
	limit   int
	deleted bool
}

func (r *controllerInitialMessageHistoryRepo) List(_ context.Context, userID string, limit int) ([]entities.InitialMessageHistoryItem, error) {
	r.userID, r.limit = userID, limit
	return r.items, nil
}
func (r *controllerInitialMessageHistoryRepo) UpsertAndTrim(context.Context, string, string, int) (entities.InitialMessageHistoryItem, error) {
	return entities.InitialMessageHistoryItem{}, nil
}
func (r *controllerInitialMessageHistoryRepo) DeleteAll(_ context.Context, userID string) error {
	r.userID, r.deleted = userID, true
	return nil
}

func TestInitialMessageHistoryControllerUsesAuthenticatedUser(t *testing.T) {
	repo := &controllerInitialMessageHistoryRepo{items: []entities.InitialMessageHistoryItem{{
		ID: "one", Content: "build it", LastUsedAt: time.Now(),
	}}}
	controller := NewInitialMessageHistoryController(sessionuc.NewInitialMessageHistoryService(repo))
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/users/me/initial-messages?limit=12", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", user)
	if err := controller.List(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || repo.userID != "alice" || repo.limit != 12 {
		t.Fatalf("code=%d user=%q limit=%d", rec.Code, repo.userID, repo.limit)
	}

	req = httptest.NewRequest(http.MethodDelete, "/users/me/initial-messages", nil)
	rec = httptest.NewRecorder()
	ctx = e.NewContext(req, rec)
	ctx.Set("internal_user", user)
	if err := controller.DeleteAll(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent || !repo.deleted || repo.userID != "alice" {
		t.Fatalf("code=%d deleted=%v user=%q", rec.Code, repo.deleted, repo.userID)
	}
}
