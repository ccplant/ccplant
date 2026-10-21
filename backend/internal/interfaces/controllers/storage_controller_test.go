package controllers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type storageControllerRepo struct{ created *entities.Storage }

func (r *storageControllerRepo) Create(_ context.Context, storage *entities.Storage) error {
	r.created = storage
	return nil
}
func (r *storageControllerRepo) GetByID(context.Context, string) (*entities.Storage, error) {
	return nil, entities.ErrStorageNotFound{}
}
func (r *storageControllerRepo) List(context.Context, portrepos.StorageFilter) ([]*entities.Storage, error) {
	return nil, nil
}
func (r *storageControllerRepo) Update(context.Context, *entities.Storage) error { return nil }
func (r *storageControllerRepo) Delete(context.Context, string) error            { return nil }

type storageControllerObjects struct{}

func (storageControllerObjects) Put(context.Context, string, string, []byte) error { return nil }
func (storageControllerObjects) Get(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func TestCreateStorageDefaultsScopeToUser(t *testing.T) {
	e := echo.New()
	repo := &storageControllerRepo{}
	controller := NewStorageController(repo, storageControllerObjects{})
	req := httptest.NewRequest(http.MethodPost, "/storages", bytes.NewBufferString(`{"name":"shared","kind":"object_snapshot"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice"))

	require.NoError(t, controller.Create(ctx))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, repo.created)
	require.Equal(t, entities.ScopeUser, repo.created.Scope())
	require.Equal(t, "alice", repo.created.OwnerID())
}
