package services

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) sandbox() {
	ctx := b.ctx
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// Embed sandbox configuration - always enabled. Apply the same defaults here
	// as direct allocation does because external allocations build and forward
	// these settings before the remote manager runs allocateSessionDirect.
	applySandboxDefaults(req)
	effectiveSandbox := m.resolveSandboxParams(ctx, req)
	settings.Sandbox = &sessionsettings.SandboxConfig{
		Enabled:        true,
		PolicyID:       req.Sandbox.PolicyID,
		AllowedDomains: effectiveSandbox.AllowedDomains,
		DeniedDomains:  effectiveSandbox.DeniedDomains,
		CountMode:      effectiveSandbox.CountMode,
	}
	log.Printf("[K8S_SESSION] Network sandbox enabled for session %s (policy: %s, allowed: %v, denied: %v, count_mode=%t)", session.id, req.Sandbox.PolicyID, effectiveSandbox.AllowedDomains, effectiveSandbox.DeniedDomains, effectiveSandbox.CountMode)
}

func (b *sessionSettingsBuilder) dockerAndModels() {
	req := b.request
	session := b.session
	settings := b.settings
	// Embed Docker-in-Docker configuration when enabled.
	if req.Docker != nil && req.Docker.Enabled {
		registries := make([]sessionsettings.RegistryConfig, 0, len(req.Docker.Registries))
		for _, r := range req.Docker.Registries {
			registries = append(registries, sessionsettings.RegistryConfig{
				Server:     r.Server,
				Username:   r.Username,
				Password:   r.Password,
				SecretName: r.SecretName,
				Insecure:   r.Insecure,
			})
		}
		settings.Docker = &sessionsettings.DockerConfig{
			Enabled:    true,
			Registries: registries,
		}
		log.Printf("[K8S_SESSION] DinD enabled for session %s (registries: %d)", session.id, len(registries))
	}
	settings.ParentRuntime = req.ParentRuntime
	applyModelConnections(settings, req)
	applyBedrockCredentialsProfile(settings)
}
