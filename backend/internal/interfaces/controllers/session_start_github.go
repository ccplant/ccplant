package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type sessionStartGitHubRouting struct {
	operation           *sessionStartOperation
	repository          string
	allowedRepositories []string
}

func (o *sessionStartOperation) configureGitHub() error {
	routing := &sessionStartGitHubRouting{operation: o}
	if err := routing.resolveDefaults(); err != nil {
		return err
	}
	useBroker, err := routing.selectBroker()
	if err != nil {
		return err
	}
	if useBroker && o.controller.githubTokenResolver != nil {
		configured, err := routing.configureBroker()
		if configured || err != nil {
			return err
		}
	}
	return routing.configureDirectToken()
}

func (r *sessionStartGitHubRouting) resolveDefaults() error {
	o := r.operation
	repository, connectionID, err := o.controller.githubRoutingDefaults(o.context.Request().Context(), o.request, o.userID, o.authorization)
	if err != nil {
		return sessionProfileResolutionHTTPError(err)
	}
	r.repository = repository
	if connectionID != "" {
		if o.request.Params == nil {
			o.request.Params = &entities.SessionParams{}
		}
		if o.request.Params.ConnectionID == "" {
			o.request.Params.ConnectionID = connectionID
		}
	}
	return nil
}

func (r *sessionStartGitHubRouting) selectBroker() (bool, error) {
	o := r.operation
	useBroker := shouldUseGitHubBroker(o.request, r.repository)
	if !useBroker && (o.request.Scope == entities.ScopeTeam || o.request.Scope == entities.ScopeUser) && r.repository == "" && (o.request.Params == nil || o.request.Params.GithubToken == "") && o.controller.githubTokenResolver != nil {
		allowed, err := o.controller.githubRepositoriesForSession(o.context.Request().Context(), o.request, o.userID, o.authorization)
		if err != nil {
			return false, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		r.allowedRepositories = allowed
		useBroker = len(allowed) > 0
	}
	return useBroker, nil
}

func (r *sessionStartGitHubRouting) configureDirectToken() error {
	o := r.operation
	if o.request.Params != nil && o.request.Params.ConnectionID != "" {
		return o.applyExplicitGitHubToken()
	}
	if (o.request.Params == nil || o.request.Params.GithubToken == "") && repositoryOwner(r.repository) != "" && o.controller.githubTokenResolver != nil {
		return o.applyOrganizationGitHubToken(r.repository)
	}
	return o.applyAuthenticationGitHubToken(true)
}

func (o *sessionStartOperation) applyExplicitGitHubToken() error {
	if o.controller.githubTokenResolver == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "GitHub connection credentials are unavailable")
	}
	token, err := o.controller.githubTokenResolver.ResolveAccessToken(o.context.Request().Context(), o.user, o.request.Params.ConnectionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	o.request.Params.GithubToken = token
	if err := o.controller.applyGitHubConnectionURLs(o.context.Request().Context(), &o.request, o.request.Params.ConnectionID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	o.controller.logSessionTokenRouting(o.sessionID, "explicit", o.request.Params.ConnectionID, token)
	return nil
}

func (o *sessionStartOperation) applyOrganizationGitHubToken(repository string) error {
	token, connectionID, matched, err := o.controller.githubTokenResolver.ResolveAccessTokenForOrganization(o.context.Request().Context(), o.user, repositoryOwner(repository))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if !matched {
		return o.applyAuthenticationGitHubToken(false)
	}
	if o.request.Params == nil {
		o.request.Params = &entities.SessionParams{}
	}
	o.request.Params.GithubToken, o.request.Params.ConnectionID = token, connectionID
	if err := o.controller.applyGitHubConnectionURLs(o.context.Request().Context(), &o.request, connectionID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	o.controller.logSessionTokenRouting(o.sessionID, "organization", connectionID, token)
	return nil
}

func (o *sessionStartOperation) applyAuthenticationGitHubToken(logResolvedConnection bool) error {
	populateGitHubTokenFromAuthHeader(o.context, &o.request)
	if o.request.Params == nil {
		return nil
	}
	if o.request.Params.ConnectionID != "" {
		if err := o.controller.applyGitHubConnectionURLs(o.context.Request().Context(), &o.request, o.request.Params.ConnectionID); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}
	connectionID := ""
	if logResolvedConnection {
		connectionID = o.request.Params.ConnectionID
	}
	o.controller.logSessionTokenRouting(o.sessionID, "authentication", connectionID, o.request.Params.GithubToken)
	return nil
}
