package controllers

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (c *SessionController) templateLaunchContext(ctx context.Context, sessionID string, session entities.Session, route *repositories.SessionRoute) (string, string, map[string]string) {
	tags := map[string]string{}
	if session != nil {
		for key, value := range session.Tags() {
			tags[key] = value
		}
	} else if route != nil {
		for key, value := range route.Tags {
			tags[key] = value
		}
	}
	agentType := strings.TrimSpace(tags["agent_type"])
	profileID := strings.TrimSpace(tags["session_profile_id"])
	if store, ok := c.sessionRunnerStore.(sessionConfigurationStore); ok {
		if cfg, err := store.GetConfiguration(ctx, sessionID); err == nil && cfg != nil {
			var source entities.StartRequest
			if json.Unmarshal(cfg.Input, &source) == nil {
				for key, value := range source.Tags {
					if _, exists := tags[key]; !exists {
						tags[key] = value
					}
				}
				if agentType == "" && source.Params != nil {
					agentType = strings.TrimSpace(source.Params.AgentType)
				}
				if profileID == "" {
					profileID = strings.TrimSpace(source.SessionProfileID)
				}
			}
		}
	}
	return agentType, profileID, tags
}

func applyContextTemplateLaunch(req *entities.StartRequest, template *entities.SessionContextTemplate) {
	if req == nil || template == nil {
		return
	}
	if req.Tags == nil {
		req.Tags = map[string]string{}
	}
	// Source launch tags win because repository, branch and selector changes can
	// make the restored workspace or conversation incompatible with its snapshot.
	for key, value := range template.Tags {
		req.Tags[key] = value
	}
	if req.SessionProfileID == "" {
		req.SessionProfileID = template.SessionProfileID
	}
	if req.Params == nil {
		req.Params = &entities.SessionParams{}
	}
	if req.Params.AgentType == "" {
		req.Params.AgentType = template.AgentType
	}
}

func (c *SessionController) recordContextTemplateUse(ctx context.Context, template *entities.SessionContextTemplate) {
	if template == nil {
		return
	}
	now := time.Now().UTC()
	template.LastUsedAt = &now
	template.UseCount++
	if err := c.contextTemplateRepo.Update(ctx, template); err != nil {
		log.Printf("failed to update context template usage for %s: %v", template.ID, err)
	}
}
