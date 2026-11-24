package services

import (
	"log"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) credentials() {
	ctx := b.ctx
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// Embed managed files from the selected credential owner so that
	// stock pool pods (which have no user-specific volume mounts) can restore files
	// on startup via the provision endpoint payload.
	// Empty CredentialSource preserves the legacy behavior: user-scoped sessions
	// use the session creator and team-scoped sessions receive no credentials.
	credentialOwners := credentialOwnersForRequest(req)
	if req.CredentialSource != "" && req.CredentialSource != "session_user" && req.CredentialSource != "triggered_user" && req.CredentialSource != "github_sender" && req.CredentialSource != "team" && req.CredentialSource != "none" {
		log.Printf("[K8S_SESSION] Warning: unknown credential_source %q for session %s; credentials disabled", req.CredentialSource, session.id)
	}
	for _, credentialOwner := range credentialOwners {
		if credentialOwner == "" {
			continue
		}
		files, filesSecretName := m.loadCredentialFiles(ctx, credentialOwner)
		if len(files) == 0 {
			// Not found or no data is normal. Event-actor sources try the team owner next.
			continue
		}
		settings.Files = files
		settings.CredentialOwner = credentialOwner
		settings.CredentialSyncID = uuid.NewString()
		log.Printf("[K8S_SESSION] Embedded %d managed file(s) for credential owner %s (Secret %s) for session %s",
			len(settings.Files), credentialOwner, filesSecretName, session.id)
		break
	}
}

func (b *sessionSettingsBuilder) userFiles() {
	ctx := b.ctx
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// User-managed files retain their existing user-scope-only behavior; the
	// credential selector above controls authentication files only. The
	// repository is backed by the application KV store so the API and execution
	// plane always read the same source of truth.
	if (req.Scope == entities.ScopeUser || req.Scope == "") && req.UserID != "" && m.userFileRepo != nil {
		userFiles, userFilesErr := m.userFileRepo.List(ctx, req.UserID)
		if userFilesErr != nil {
			log.Printf("[K8S_SESSION] Warning: failed to load user files for %s: %v", req.UserID, userFilesErr)
		} else {
			userManagedFiles := make([]sessionsettings.ManagedFile, 0, len(userFiles))
			for _, file := range userFiles {
				if file == nil || file.Path() == "" {
					continue
				}
				userManagedFiles = append(userManagedFiles, sessionsettings.ManagedFile{
					Path:        file.Path(),
					Content:     file.Content(),
					Permissions: file.Permissions(),
				})
			}
			if len(userManagedFiles) > 0 {
				settings.Files = append(settings.Files, userManagedFiles...)
				log.Printf("[K8S_SESSION] Embedded %d user file(s) from application KV store for session %s",
					len(userManagedFiles), session.id)
			}
		}
	}
}

func (b *sessionSettingsBuilder) projectedFiles() {
	req := b.request
	secretFiles := b.secretFiles
	session := b.session
	settings := b.settings
	if len(secretFiles) > 0 {
		settings.Files = append(settings.Files, secretFiles...)
		log.Printf("[K8S_SESSION] Embedded %d settings secret file(s) for session %s", len(secretFiles), session.id)
	}

	// Profile files are profile-owned and are included even for team sessions.
	// They are embedded directly in the provision payload rather than stored in
	// the user files Secret.
	if len(req.ProfileFiles) > 0 {
		profileFiles := make([]sessionsettings.ManagedFile, len(req.ProfileFiles))
		copy(profileFiles, req.ProfileFiles)
		settings.Files = append(settings.Files, profileFiles...)
		log.Printf("[K8S_SESSION] Embedded %d session profile file(s) for session %s", len(profileFiles), session.id)
	}
	if promptFile := profileSystemPromptFile(req.AgentType, req.ProfileSystemPrompt); promptFile != nil {
		settings.Files = append(settings.Files, *promptFile)
		log.Printf("[K8S_SESSION] Injected session profile system prompt for agent type %s in session %s", req.AgentType, session.id)
	}

	// When CycleMessage is set, write the message into /tmp/check/CYCLE_ENABLED so
	// the provisioner creates it on startup.  The cycle command reads the message
	// directly from this file, so no message argument is needed in the Stop hook.
	// Deleting CYCLE_ENABLED (done by the agent when the goal is met, or when
	// --max-count is reached) disables further cycles without any extra marker file.
	if req.CycleMessage != "" {
		settings.Files = append(settings.Files, sessionsettings.ManagedFile{
			Path:    "/tmp/check/CYCLE_ENABLED",
			Content: req.CycleMessage,
		})
		log.Printf("[K8S_SESSION] Injected CYCLE_ENABLED file (with message) for session %s", session.id)
	}
}
