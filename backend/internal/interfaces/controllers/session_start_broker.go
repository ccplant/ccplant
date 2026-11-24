package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (r *sessionStartGitHubRouting) configureBroker() (bool, error) {
	o := r.operation
	if !o.authorization.CanCreateResource(string(o.request.Scope), o.request.TeamID) {
		return false, echo.NewHTTPError(http.StatusForbidden, "user cannot create sessions in the requested scope")
	}
	lease, connectionID, matched, err := r.resolveBrokerLease()
	if err != nil {
		return false, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if !matched {
		return false, nil
	}
	return true, o.applyBrokerLease(lease, connectionID)
}

func (o *sessionStartOperation) preferredBrokerIdentity() (string, *entities.User) {
	connectionID := ""
	if o.request.Params != nil {
		connectionID = o.request.Params.ConnectionID
	}
	if o.request.Scope != entities.ScopeUser {
		return connectionID, nil
	}
	if connectionID == "" {
		if credential, ok := auth.GetGitHubCredentialFromContext(o.context); ok {
			connectionID = credential.ConnectionID
		}
	}
	return connectionID, o.user
}

func (r *sessionStartGitHubRouting) resolveBrokerLease() (string, string, bool, error) {
	o := r.operation
	preferred, user := o.preferredBrokerIdentity()
	if o.dryRun {
		if r.repository == "" {
			return "<generated-at-apply>", preferred, len(r.allowedRepositories) > 0, nil
		}
		if preferred != "" {
			return "<generated-at-apply>", preferred, true, nil
		}
		_, connectionID, matched, err := o.controller.githubTokenResolver.ResolveAccessTokenForOrganization(o.context.Request().Context(), o.user, repositoryOwner(r.repository))
		return "<generated-at-apply>", connectionID, matched, err
	}
	if r.allowedRepositories == nil {
		allowed, err := o.controller.githubRepositoriesForSession(o.context.Request().Context(), o.request, o.userID, o.authorization)
		if err != nil {
			return "", "", false, err
		}
		r.allowedRepositories = allowed
	}
	return o.controller.githubTokenResolver.IssueBrokerLeaseForRepositories(o.context.Request().Context(), o.sessionID, preferred, repositoryOwner(r.repository), r.repository, r.allowedRepositories, user)
}

func (o *sessionStartOperation) applyBrokerLease(lease, connectionID string) error {
	if o.request.Environment == nil {
		o.request.Environment = make(map[string]string)
	}
	if connectionID != "" {
		if err := o.controller.applyGitHubConnectionURLs(o.context.Request().Context(), &o.request, connectionID); err != nil {
			return o.brokerConfigurationError(err)
		}
	}
	brokerURL, err := githubBrokerURL(o.context, o.sessionID, o.controller.githubBrokerBaseURL)
	if err != nil {
		return o.brokerConfigurationError(err)
	}
	o.request.Environment["AGENTAPI_GITHUB_BROKER_URL"] = brokerURL
	o.request.Environment["AGENTAPI_GITHUB_BROKER_TOKEN"] = lease
	o.request.Environment["AGENTAPI_GITHUB_CONNECTION_ID"] = connectionID
	if o.request.Params != nil {
		o.request.Params.GithubToken = ""
	}
	o.controller.logSessionTokenRouting(o.sessionID, "team-broker", connectionID, "")
	return nil
}

func (o *sessionStartOperation) brokerConfigurationError(err error) error {
	if !o.dryRun {
		o.controller.revokeGitHubBrokerLeases(o.context.Request().Context(), o.sessionID)
	}
	return echo.NewHTTPError(http.StatusBadRequest, err.Error())
}
