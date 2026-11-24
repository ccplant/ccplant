package controllers

import (
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

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
