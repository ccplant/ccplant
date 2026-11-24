package controllers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

type sessionStartOperation struct {
	controller                         *SessionController
	context                            echo.Context
	dryRun                             bool
	sessionID                          string
	request                            entities.StartRequest
	ttl                                string
	explicitSandbox, explicitDocker    bool
	authorization                      *auth.AuthorizationContext
	user                               *entities.User
	userID, role                       string
	teams                              []string
	template                           *entities.SessionContextTemplate
	reuseResolution, profileResolution map[string]interface{}
	reuseSession                       bool
	input                              []byte
}

func (o *sessionStartOperation) bindInput() error {
	if err := o.context.Bind(&o.request); err != nil {
		log.Printf("Failed to parse request body (using defaults): %v", err)
	}
	o.ttl = o.controller.interactiveSessionTTL
	if claims, ok := o.context.Get("trigger_execution_claims").(executiontoken.ExecutionClaims); ok {
		switch {
		case claims.ScheduleID != "":
			o.ttl = o.controller.scheduleSessionTTL
		case claims.WebhookID != "":
			o.ttl = o.controller.webhookSessionTTL
		default:
			o.ttl = ""
		}
		// Bind worker-triggered identity and ownership to the signed claims.
		o.request.Scope = claims.Scope
		o.request.TeamID = claims.TeamID
		o.request.TriggeredUserID = claims.TriggeredUserID
		if o.request.Tags == nil {
			o.request.Tags = make(map[string]string)
		}
		for key, id := range map[string]string{"slackbot_id": claims.SlackBotID, "schedule_id": claims.ScheduleID, "webhook_id": claims.WebhookID} {
			delete(o.request.Tags, key)
			if id != "" {
				o.request.Tags[key] = id
			}
		}
		credentialSource := ""
		if o.request.Params != nil {
			credentialSource = o.request.Params.CredentialSource
		}
		o.context.SetRequest(o.context.Request().WithContext(withTriggerCredentialPrincipal(o.context.Request().Context(), claims, credentialSource)))
	}
	o.explicitSandbox = o.request.Params != nil && o.request.Params.Sandbox != nil
	o.explicitDocker = o.request.Params != nil && o.request.Params.Docker != nil

	return nil
}

func (o *sessionStartOperation) resolveIdentity() error {
	// Get authorization context from middleware (guaranteed to be non-nil by AuthMiddleware)
	o.authorization = auth.GetAuthorizationContext(o.context)
	o.user = o.authorization.User
	o.userID = string(o.user.ID())

	if len(o.user.Roles()) > 0 {
		o.role = string(o.user.Roles()[0])
	} else {
		o.role = "user"
	}

	// Use pre-resolved team information from authorization context
	o.teams = o.authorization.TeamScope.Teams
	log.Printf("[SESSION_DEBUG] Using authz context for user %s, teams count: %d", o.userID, len(o.teams))

	// Normalize scope: default to "user" if not specified.
	// This prevents downstream failures (e.g. memory dump) that require a valid scope.
	if o.request.Scope == "" {
		o.request.Scope = entities.ScopeUser
	}

	// Service accounts cannot use user scope.
	// Automatically route to the service account's team scope.
	{
		resolvedScope, resolvedTeamID := o.authorization.ResolveScope(string(o.request.Scope), o.request.TeamID)
		if resolvedScope != string(o.request.Scope) || resolvedTeamID != o.request.TeamID {
			log.Printf("[SESSION_DEBUG] Service account %s: routing scope %q → %q (team %q)", o.userID, o.request.Scope, resolvedScope, resolvedTeamID)
		}
		o.request.Scope = entities.ResourceScope(resolvedScope)
		o.request.TeamID = resolvedTeamID
	}
	return nil
}

func (o *sessionStartOperation) authorizeScope() error {
	// Validate team scope authorization
	if o.request.Scope == entities.ScopeTeam {
		if o.request.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required for team scope")
		}

		// Check if user can create in this team
		if !o.authorization.CanCreateInTeam(o.request.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("user is not a member of team %s", o.request.TeamID))
		}
	} else {
		// Personal scope - check if user can create personal resources
		if !o.authorization.PersonalScope.CanCreate {
			return echo.NewHTTPError(http.StatusForbidden, "user does not have permission to create sessions")
		}
	}

	return nil
}

func (o *sessionStartOperation) resolveContext() error {
	var err error
	o.template, err = o.controller.resolveContextTemplate(o.context, &o.request)
	if err != nil {
		return err
	}

	return nil
}
