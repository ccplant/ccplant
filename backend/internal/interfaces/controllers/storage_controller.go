package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const maxStorageFileBytes = 64 << 20

type StorageController struct {
	repo    portrepos.StorageRepository
	objects services.StorageObjectStore
}

func NewStorageController(repo portrepos.StorageRepository, objects services.StorageObjectStore) *StorageController {
	return &StorageController{repo: repo, objects: objects}
}

func (c *StorageController) GetName() string { return "StorageController" }

type CreateStorageRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope,omitempty"`
	TeamID      string `json:"team_id,omitempty"`
	Kind        string `json:"kind"`
	Pool        string `json:"pool,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Size        string `json:"size,omitempty"`
}

type UpdateStorageRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Size        *string `json:"size,omitempty"`
}

type StorageResponse struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	OwnerID     string                `json:"owner_id"`
	Scope       string                `json:"scope"`
	TeamID      string                `json:"team_id,omitempty"`
	Kind        entities.StorageKind  `json:"kind"`
	Pool        string                `json:"pool,omitempty"`
	Provider    string                `json:"provider,omitempty"`
	Size        string                `json:"size,omitempty"`
	State       entities.StorageState `json:"state"`
	Revision    int64                 `json:"revision"`
	FileCount   int                   `json:"file_count"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type StorageListResponse struct {
	Storages []*StorageResponse `json:"storages"`
	Total    int                `json:"total"`
}

type StorageFileResponse struct {
	Path      string    `json:"path"`
	Digest    string    `json:"digest,omitempty"`
	Size      int64     `json:"size,omitempty"`
	Mode      uint32    `json:"mode,omitempty"`
	Deleted   bool      `json:"deleted,omitempty"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StorageFileListResponse struct {
	Revision int64                  `json:"revision"`
	Files    []*StorageFileResponse `json:"files"`
}

func (c *StorageController) Create(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	var request CreateStorageRequest
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if strings.TrimSpace(request.Scope) == "" {
		request.Scope = string(entities.ScopeUser)
	}
	request.Scope, request.TeamID = auth.ResolveUserScope(user, request.Scope, request.TeamID)
	if request.Scope == string(entities.ScopeTeam) && !storageUserInTeam(user, request.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}
	storage := entities.NewStorage(
		uuid.NewString(), request.Name, request.Description, string(user.ID()),
		entities.ResourceScope(request.Scope), request.TeamID, entities.StorageKind(request.Kind),
		request.Pool, request.Provider, request.Size,
	)
	if err := storage.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if storage.Kind() == entities.StorageKindObjectSnapshot && c.objects == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "object storage provider is not configured")
	}
	if err := c.repo.Create(ctx.Request().Context(), storage); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create storage")
	}
	return ctx.JSON(http.StatusCreated, storageResponse(storage))
}

func (c *StorageController) Get(ctx echo.Context) error {
	storage, user, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	_ = user
	return ctx.JSON(http.StatusOK, storageResponse(storage))
}

func (c *StorageController) List(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	scope, teamID := auth.ResolveUserScope(user, ctx.QueryParam("scope"), ctx.QueryParam("team_id"))
	var storages []*entities.Storage
	var err error
	switch scope {
	case string(entities.ScopeUser):
		storages, err = c.repo.List(ctx.Request().Context(), portrepos.StorageFilter{Scope: entities.ScopeUser, OwnerID: string(user.ID())})
	case string(entities.ScopeTeam):
		if !storageUserInTeam(user, teamID) {
			return echo.NewHTTPError(http.StatusForbidden, "Access denied")
		}
		storages, err = c.repo.List(ctx.Request().Context(), portrepos.StorageFilter{Scope: entities.ScopeTeam, TeamID: teamID})
	default:
		var own []*entities.Storage
		own, err = c.repo.List(ctx.Request().Context(), portrepos.StorageFilter{Scope: entities.ScopeUser, OwnerID: string(user.ID())})
		if err == nil {
			teams := storageUserTeamIDs(user)
			var shared []*entities.Storage
			if len(teams) > 0 {
				shared, err = c.repo.List(ctx.Request().Context(), portrepos.StorageFilter{Scope: entities.ScopeTeam, TeamIDs: teams})
			}
			storages = append(own, shared...)
		}
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list storages")
	}
	response := &StorageListResponse{Storages: make([]*StorageResponse, 0, len(storages)), Total: len(storages)}
	for _, storage := range storages {
		response.Storages = append(response.Storages, storageResponse(storage))
	}
	return ctx.JSON(http.StatusOK, response)
}

func (c *StorageController) Update(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	var request UpdateStorageRequest
	if bindErr := ctx.Bind(&request); bindErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if request.Name != nil {
		storage.SetName(strings.TrimSpace(*request.Name))
	}
	if request.Description != nil {
		storage.SetDescription(*request.Description)
	}
	if request.Size != nil {
		nextSize := strings.TrimSpace(*request.Size)
		storage.SetSize(nextSize)
	}
	if validateErr := storage.Validate(); validateErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, validateErr.Error())
	}
	if updateErr := c.repo.Update(ctx.Request().Context(), storage); updateErr != nil {
		if k8serrors.IsConflict(updateErr) {
			return echo.NewHTTPError(http.StatusConflict, "storage was updated concurrently")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update storage")
	}
	return ctx.JSON(http.StatusOK, storageResponse(storage))
}

func (c *StorageController) Delete(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	if deleteErr := c.repo.Delete(ctx.Request().Context(), storage.ID()); deleteErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete storage")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *StorageController) ListFiles(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	includeDeleted := ctx.QueryParam("include_deleted") == "true"
	files := storage.Files()
	paths := make([]string, 0, len(files))
	for filePath, file := range files {
		if !file.Deleted || includeDeleted {
			paths = append(paths, filePath)
		}
	}
	sort.Strings(paths)
	response := StorageFileListResponse{Revision: storage.Revision(), Files: make([]*StorageFileResponse, 0, len(paths))}
	for _, filePath := range paths {
		file := files[filePath]
		response.Files = append(response.Files, storageFileResponse(file))
	}
	return ctx.JSON(http.StatusOK, response)
}

func (c *StorageController) PutFile(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	if storage.Kind() != entities.StorageKindObjectSnapshot {
		return echo.NewHTTPError(http.StatusBadRequest, "file API is available only for object_snapshot storage")
	}
	if c.objects == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "object storage provider is not configured")
	}
	filePath := ctx.Param("*")
	if validateErr := entities.ValidateStoragePath(filePath); validateErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, validateErr.Error())
	}
	limited := io.LimitReader(ctx.Request().Body, maxStorageFileBytes+1)
	content, readErr := io.ReadAll(limited)
	if readErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read file")
	}
	if len(content) > maxStorageFileBytes {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "file exceeds 64 MiB")
	}
	mode := uint32(0o644)
	if rawMode := ctx.QueryParam("mode"); rawMode != "" {
		parsed, parseErr := strconv.ParseUint(rawMode, 8, 32)
		if parseErr != nil || parsed&^0o777 != 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "mode must be an octal permission between 000 and 777")
		}
		mode = uint32(parsed)
	}
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	if putErr := c.objects.Put(ctx.Request().Context(), storage.ID(), digest, content); putErr != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to store file content")
	}
	committed, commitErr := c.commitFile(ctx, storage.ID(), entities.StorageFile{Path: filePath, Digest: digest, Size: int64(len(content)), Mode: mode})
	if commitErr != nil {
		return commitErr
	}
	return ctx.JSON(http.StatusOK, storageFileResponse(committed))
}

func (c *StorageController) GetFile(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	filePath := ctx.Param("*")
	if validateErr := entities.ValidateStoragePath(filePath); validateErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, validateErr.Error())
	}
	file, ok := storage.File(filePath)
	if !ok || file.Deleted {
		return echo.NewHTTPError(http.StatusNotFound, "storage file not found")
	}
	if c.objects == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "object storage provider is not configured")
	}
	body, getErr := c.objects.Get(ctx.Request().Context(), storage.ID(), file.Digest)
	if getErr != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to load file content")
	}
	defer func() { _ = body.Close() }()
	ctx.Response().Header().Set("ETag", `"sha256:`+file.Digest+`"`)
	ctx.Response().Header().Set("X-Storage-Revision", strconv.FormatInt(file.Revision, 10))
	ctx.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
	ctx.Response().WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(ctx.Response().Writer, body)
	return copyErr
}

func (c *StorageController) DeleteFile(ctx echo.Context) error {
	storage, _, err := c.authorizedStorage(ctx)
	if err != nil {
		return err
	}
	if storage.Kind() != entities.StorageKindObjectSnapshot {
		return echo.NewHTTPError(http.StatusBadRequest, "file API is available only for object_snapshot storage")
	}
	filePath := ctx.Param("*")
	if validateErr := entities.ValidateStoragePath(filePath); validateErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, validateErr.Error())
	}
	committed, commitErr := c.commitFile(ctx, storage.ID(), entities.StorageFile{Path: filePath, Deleted: true})
	if commitErr != nil {
		return commitErr
	}
	return ctx.JSON(http.StatusOK, storageFileResponse(committed))
}

func (c *StorageController) commitFile(ctx echo.Context, storageID string, file entities.StorageFile) (entities.StorageFile, error) {
	for attempt := 0; attempt < 8; attempt++ {
		storage, getErr := c.repo.GetByID(ctx.Request().Context(), storageID)
		if getErr != nil {
			return entities.StorageFile{}, echo.NewHTTPError(http.StatusNotFound, "Storage not found")
		}
		user := auth.GetUserFromContext(ctx)
		if user == nil || !storageUserCanAccess(user, storage) {
			return entities.StorageFile{}, echo.NewHTTPError(http.StatusForbidden, "Access denied")
		}
		storage.ApplyFiles([]entities.StorageFile{file})
		updateErr := c.repo.Update(ctx.Request().Context(), storage)
		if updateErr == nil {
			committed, _ := storage.File(file.Path)
			return committed, nil
		}
		if !k8serrors.IsConflict(updateErr) {
			return entities.StorageFile{}, echo.NewHTTPError(http.StatusInternalServerError, "failed to commit file metadata")
		}
	}
	return entities.StorageFile{}, echo.NewHTTPError(http.StatusConflict, "storage is too busy; retry the write")
}

func (c *StorageController) authorizedStorage(ctx echo.Context) (*entities.Storage, *entities.User, error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, nil, echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	storage, err := c.repo.GetByID(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		var notFound entities.ErrStorageNotFound
		if errors.As(err, &notFound) {
			return nil, nil, echo.NewHTTPError(http.StatusNotFound, "Storage not found")
		}
		return nil, nil, echo.NewHTTPError(http.StatusInternalServerError, "Failed to get storage")
	}
	if !storageUserCanAccess(user, storage) {
		return nil, nil, echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}
	return storage, user, nil
}

func storageResponse(storage *entities.Storage) *StorageResponse {
	count := 0
	for _, file := range storage.Files() {
		if !file.Deleted {
			count++
		}
	}
	return &StorageResponse{
		ID: storage.ID(), Name: storage.Name(), Description: storage.Description(),
		OwnerID: storage.OwnerID(), Scope: string(storage.Scope()), TeamID: storage.TeamID(),
		Kind: storage.Kind(), Pool: storage.Pool(), Provider: storage.Provider(), Size: storage.Size(),
		State: storage.State(), Revision: storage.Revision(), FileCount: count,
		CreatedAt: storage.CreatedAt(), UpdatedAt: storage.UpdatedAt(),
	}
}

func storageFileResponse(file entities.StorageFile) *StorageFileResponse {
	return &StorageFileResponse{
		Path: file.Path, Digest: file.Digest, Size: file.Size, Mode: file.Mode,
		Deleted: file.Deleted, Revision: file.Revision, UpdatedAt: file.UpdatedAt,
	}
}

func storageUserCanAccess(user *entities.User, storage *entities.Storage) bool {
	switch storage.Scope() {
	case entities.ScopeUser:
		return storage.OwnerID() == string(user.ID())
	case entities.ScopeTeam:
		return storageUserInTeam(user, storage.TeamID())
	default:
		return false
	}
}

func storageUserInTeam(user *entities.User, teamID string) bool {
	if user == nil || teamID == "" {
		return false
	}
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

func storageUserTeamIDs(user *entities.User) []string {
	if user.UserType() == entities.UserTypeServiceAccount {
		if user.TeamID() == "" {
			return nil
		}
		return []string{user.TeamID()}
	}
	github := user.GitHubInfo()
	if github == nil {
		return nil
	}
	teams := github.Teams()
	result := make([]string, 0, len(teams))
	for _, team := range teams {
		result = append(result, fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug))
	}
	return result
}
