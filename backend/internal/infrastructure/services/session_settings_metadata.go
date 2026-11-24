package services

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) metadata() {
	ctx := b.ctx
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// Session metadata
	scope := string(req.Scope)
	if scope == "" {
		scope = "user"
	}
	settings.Session = sessionsettings.SessionMeta{
		ID:                 session.id,
		UserID:             req.UserID,
		Scope:              scope,
		TeamID:             req.TeamID,
		AgentType:          req.AgentType,
		Teams:              req.Teams,
		ResumeFrom:         req.ResumeFrom,
		PersistenceEnabled: m.config.SessionPersistence.Backend != "",
	}
	if after, enabled, err := m.resolveAutoSuspendPolicy(ctx, session); err == nil {
		settings.Session.AutoSuspendEnabled = &enabled
		settings.Session.AutoSuspendMinutes = int(after / time.Minute)
	}
	settings.UnsyncedFilePaths = append([]string(nil), req.UnsyncedFilePaths...)
	for _, storage := range req.Storages {
		resolved := sessionsettings.StorageAttachment{
			StorageID: storage.StorageID, StoragePath: storage.StoragePath, MountPath: storage.MountPath, ReadOnly: storage.ReadOnly, Managed: storage.Managed,
			Kind: string(storage.Kind), Revision: storage.Revision,
		}
		for _, file := range storage.Files {
			if file.Deleted {
				continue
			}
			resolved.Files = append(resolved.Files, sessionsettings.StorageFile{
				Path: file.Path, Digest: file.Digest, Size: file.Size, Mode: file.Mode, Revision: file.Revision,
			})
		}
		settings.Storages = append(settings.Storages, resolved)
	}
}

func (b *sessionSettingsBuilder) repositoryConfiguration() {
	m := b.manager
	req := b.request
	settings := b.settings
	webhookPayload := b.webhookPayload
	// Repository info
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		settings.Repository = &sessionsettings.RepositoryConfig{
			FullName: req.RepoInfo.FullName,
			CloneDir: "/home/agentapi/workdir/repo",
			Branch:   req.RepoInfo.Branch,
			PR:       req.RepoInfo.PR,
		}
	}

	// Initial message
	settings.InitialMessage = req.InitialMessage

	// Webhook payload
	if len(webhookPayload) > 0 {
		settings.WebhookPayload = string(webhookPayload)
	}

	// GitHub config
	if req.GithubToken != "" {
		settings.Github = &sessionsettings.GithubConfig{
			Token:            req.GithubToken,
			ConfigSecretName: m.k8sConfig.GitHubConfigSecretName,
		}
	} else if m.k8sConfig.GitHubSecretName != "" || m.k8sConfig.GitHubConfigSecretName != "" {
		settings.Github = &sessionsettings.GithubConfig{
			SecretName:       m.k8sConfig.GitHubSecretName,
			ConfigSecretName: m.k8sConfig.GitHubConfigSecretName,
		}
	}
}
