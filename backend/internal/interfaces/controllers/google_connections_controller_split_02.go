package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GoogleConnectionsController) CompleteLogin(ctx context.Context, stateID, code string) (*GoogleConnectionLoginResult, error) {
	state, connection, claims, err := c.completeAuthorization(ctx, stateID, code, "login")
	if err != nil {
		return nil, err
	}
	if !connection.AllowLogin {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Google login is disabled")
	}
	principal, err := c.resolveLoginPrincipal(ctx, connection, claims)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Google identity could not be resolved").SetInternal(err)
	}
	return &GoogleConnectionLoginResult{PrincipalID: principal.ID, UserID: applicationUserID(principal), Email: claims.Email, Name: claims.Name, AvatarURL: claims.Picture, ConnectionID: state.ConnectionID}, nil
}

func (c *GoogleConnectionsController) ListIdentities(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.principals.loadPrincipalForUser(ctx.Request().Context(), user)
	if apierrors.IsNotFound(err) {
		return ctx.JSON(http.StatusOK, map[string]any{"principal_id": nil, "identities": []any{}})
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load principal").SetInternal(err)
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list Google identities").SetInternal(err)
	}
	result := make([]googleIdentityResponse, 0)
	credential, credentialFound := auth.GetCredentialContext(ctx)
	if !credentialFound {
		credential = nil
	}
	for _, identity := range identities {
		if identity.PrincipalID == principal.ID {
			if record, loadErr := c.loadConnectionRecord(ctx.Request().Context(), identity.ConnectionID); loadErr == nil {
				result = append(result, googleIdentityResponse{googleIdentity: identity, ConnectionName: record.connection.Name, Current: credential != nil && credential.Provider == "google" && credential.ConnectionID == identity.ConnectionID})
			}
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"principal_id": principal.ID, "identities": result})
}

func (c *GoogleConnectionsController) StartLink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if !c.encryptedStorage {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "account linking requires encrypted storage")
	}
	var req struct {
		ConnectionID string `json:"connection_id"`
		ReturnTo     string `json:"return_to"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := ctx.Bind(&req); err != nil || req.ConnectionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "connection_id is required")
	}
	record, err := c.loadConnectionRecord(ctx.Request().Context(), req.ConnectionID)
	if err != nil || !record.connection.Enabled {
		return echo.NewHTTPError(http.StatusBadRequest, "Google connection is unavailable")
	}
	connection := record.connection
	principal, err := c.principals.getOrCreatePrincipalForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve principal").SetInternal(err)
	}
	callbackURL, err := c.resolveCallbackURL(ctx, req.CallbackURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback_url")
	}
	state, err := c.createState(ctx.Request().Context(), googleStateRequest{connectionID: connection.ID, mode: "link", principalID: principal.ID, returnTo: sanitizeReturnTo(req.ReturnTo), callbackURL: callbackURL})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create OAuth state").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, map[string]string{"authorization_url": googleAuthorizationURL(connection, state)})
}

func (c *GoogleConnectionsController) Callback(ctx echo.Context) error {
	stateID, code := ctx.QueryParam("state"), ctx.QueryParam("code")
	state, connection, claims, err := c.completeAuthorization(ctx.Request().Context(), stateID, code, "link")
	if err != nil {
		return c.redirectResult(ctx, state.ReturnTo, "error", "authentication_failed")
	}
	if connection.ID != state.ConnectionID {
		return c.redirectResult(ctx, state.ReturnTo, "error", "authentication_failed")
	}
	identity := googleIdentity{ID: uuid.NewString(), PrincipalID: state.PrincipalID, ConnectionID: state.ConnectionID, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name, AvatarURL: claims.Picture, HostedDomain: claims.HostedDomain, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created, err := c.linkIdentity(ctx.Request().Context(), identity)
	if errors.Is(err, errGoogleIdentityConflict) {
		return c.redirectResult(ctx, state.ReturnTo, "error", "identity_linked_to_another_principal")
	}
	if err != nil {
		return c.redirectResult(ctx, state.ReturnTo, "error", "identity_link_failed")
	}
	if !created {
		return c.redirectResult(ctx, state.ReturnTo, "success", "already_linked")
	}
	return c.redirectResult(ctx, state.ReturnTo, "success", "linked")
}

func (c *GoogleConnectionsController) Unlink(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principal, err := c.principals.loadPrincipalForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "principal not found")
	}
	identities, err := c.listIdentities(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list identities")
	}
	for _, identity := range identities {
		if identity.ID == ctx.Param("identity_id") && identity.PrincipalID == principal.ID {
			if credential, ok := auth.GetCredentialContext(ctx); ok && credential.Provider == "google" && credential.ConnectionID == identity.ConnectionID {
				return echo.NewHTTPError(http.StatusConflict, "the connection used by the current login cannot be unlinked")
			}
			if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx.Request().Context(), googleIdentitySecretName(identity.ConnectionID, identity.Subject), metav1.DeleteOptions{}); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to unlink identity").SetInternal(err)
			}
			return ctx.NoContent(http.StatusNoContent)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound, "Google identity not found")
}

func (c *GoogleConnectionsController) completeAuthorization(ctx context.Context, stateID, code, mode string) (googleOAuthState, googleConnection, *googleClaims, error) {
	if stateID == "" || code == "" {
		return googleOAuthState{}, googleConnection{}, nil, echo.NewHTTPError(http.StatusBadRequest, "code and state are required")
	}
	state, secret, err := c.loadState(ctx, stateID)
	if err != nil || state.Mode != mode {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusBadRequest, "OAuth state is invalid or expired")
	}
	if err := c.client.CoreV1().Secrets(c.namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusConflict, "OAuth state has already been used")
	}
	record, err := c.loadConnectionRecord(ctx, state.ConnectionID)
	if err != nil || !record.connection.Enabled {
		return state, googleConnection{}, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google connection is unavailable")
	}
	connection := record.connection
	clientSecret, err := c.resolveClientSecret(ctx, connection)
	if err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google connection secret is unavailable")
	}
	config := oauth2.Config{ClientID: connection.OAuthClientID, ClientSecret: clientSecret, RedirectURL: state.CallbackURL, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}, Scopes: []string{"openid", "email", "profile"}}
	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	token, err := config.Exchange(exchangeCtx, code, oauth2.SetAuthURLParam("code_verifier", state.PKCEVerifier))
	if err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google OAuth code exchange failed").SetInternal(err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google did not return an ID token")
	}
	claims, err := c.validateIDToken(ctx, rawIDToken, connection.OAuthClientID)
	if err != nil || claims.Nonce != state.Nonce {
		return state, connection, nil, echo.NewHTTPError(http.StatusUnauthorized, "Google ID token validation failed").SetInternal(err)
	}
	if err := validateGoogleClaims(connection, claims); err != nil {
		return state, connection, nil, echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return state, connection, claims, nil
}

func (c *GoogleConnectionsController) validateGoogleIDToken(ctx context.Context, raw, audience string) (*googleClaims, error) {
	validator, err := idtoken.NewValidator(ctx, option.WithHTTPClient(c.httpClient))
	if err != nil {
		return nil, err
	}
	payload, err := validator.Validate(ctx, raw, audience)
	if err != nil {
		return nil, err
	}
	return &googleClaims{Subject: payload.Subject, Email: claimString(payload.Claims, "email"), EmailVerified: claimBool(payload.Claims, "email_verified"), Name: claimString(payload.Claims, "name"), Picture: claimString(payload.Claims, "picture"), HostedDomain: strings.ToLower(claimString(payload.Claims, "hd")), Nonce: claimString(payload.Claims, "nonce")}, nil
}

func (c *GoogleConnectionsController) resolveLoginPrincipal(ctx context.Context, connection googleConnection, claims *googleClaims) (githubPrincipal, error) {
	var identity googleIdentity
	_, err := c.principals.loadObject(ctx, googleIdentitySecretName(connection.ID, claims.Subject), &identity)
	if apierrors.IsNotFound(err) {
		if !connection.AllowUserCreation {
			return githubPrincipal{}, errors.New("user creation is disabled for this Google connection")
		}
		principal, createErr := c.principals.getOrCreatePrincipal(ctx, "google-connection:"+connection.ID+":"+claims.Subject)
		if createErr != nil {
			return githubPrincipal{}, createErr
		}
		identity = googleIdentity{ID: uuid.NewString(), PrincipalID: principal.ID, ConnectionID: connection.ID, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name, AvatarURL: claims.Picture, HostedDomain: claims.HostedDomain, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, linkErr := c.linkIdentity(ctx, identity)
		return principal, linkErr
	}
	if err != nil {
		return githubPrincipal{}, err
	}
	identity.Email, identity.EmailVerified, identity.Name, identity.AvatarURL, identity.HostedDomain, identity.UpdatedAt = claims.Email, claims.EmailVerified, claims.Name, claims.Picture, claims.HostedDomain, time.Now().UTC()
	if _, err := c.linkIdentity(ctx, identity); err != nil {
		return githubPrincipal{}, err
	}
	principals, err := c.principals.listPrincipals(ctx)
	if err != nil {
		return githubPrincipal{}, err
	}
	for _, principal := range principals {
		if principal.ID == identity.PrincipalID {
			return principal, nil
		}
	}
	return githubPrincipal{}, errors.New("principal for Google identity not found")
}

var errGoogleIdentityConflict = errors.New("google identity belongs to another principal")

func (c *GoogleConnectionsController) linkIdentity(ctx context.Context, identity googleIdentity) (bool, error) {
	name := googleIdentitySecretName(identity.ConnectionID, identity.Subject)
	var existing googleIdentity
	secret, err := c.principals.loadObject(ctx, name, &existing)
	if err == nil {
		if existing.PrincipalID != identity.PrincipalID {
			return false, errGoogleIdentityConflict
		}
		identity.ID, identity.CreatedAt = existing.ID, existing.CreatedAt
		record, marshalErr := json.Marshal(identity)
		if marshalErr != nil {
			return false, marshalErr
		}
		secret.Data["record.json"] = record
		updated, updateErr := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if updateErr == nil && updated == nil {
			updateErr = errors.New("updated Google identity Secret is missing")
		}
		return false, updateErr
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	record, err := json.Marshal(identity)
	if err != nil {
		return false, err
	}
	created, err := c.client.CoreV1().Secrets(c.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{googleIdentityLabel: "true", "agentapi.ccplant.io/connection-id": identity.ConnectionID}}, Data: map[string][]byte{"record.json": record}}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return c.linkIdentity(ctx, identity)
	}
	if err == nil && created == nil {
		err = errors.New("created Google identity Secret is missing")
	}
	return err == nil, err
}

type googleStateRequest struct {
	connectionID, mode, principalID, returnTo, callbackURL string
}

func (c *GoogleConnectionsController) createState(ctx context.Context, request googleStateRequest) (googleOAuthState, error) {
	state := googleOAuthState{ID: uuid.NewString(), ConnectionID: request.connectionID, PrincipalID: request.principalID, ReturnTo: request.returnTo, CallbackURL: request.callbackURL, Mode: request.mode, Nonce: randomURLToken(32), PKCEVerifier: randomURLToken(48), ExpiresAt: time.Now().UTC().Add(googleOAuthStateTTL)}
	err := c.principals.createObject(ctx, googleStateSecretName(state.ID), googleOAuthStateLabel, state, nil)
	return state, err
}

func (c *GoogleConnectionsController) loadState(ctx context.Context, id string) (googleOAuthState, *corev1.Secret, error) {
	var state googleOAuthState
	secret, err := c.principals.loadObject(ctx, googleStateSecretName(id), &state)
	if err != nil || state.ID != id || time.Now().UTC().After(state.ExpiresAt) {
		return state, secret, errors.New("OAuth state is invalid or expired")
	}
	return state, secret, nil
}

func googleAuthorizationURL(connection googleConnection, state googleOAuthState) string {
	hash := sha256.Sum256([]byte(state.PKCEVerifier))
	params := url.Values{"client_id": {connection.OAuthClientID}, "redirect_uri": {state.CallbackURL}, "response_type": {"code"}, "scope": {"openid email profile"}, "state": {state.ID}, "nonce": {state.Nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	if len(connection.HostedDomains) == 1 {
		params.Set("hd", connection.HostedDomains[0])
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func (c *GoogleConnectionsController) resolveCallbackURL(ctx echo.Context, requested string) (string, error) {
	if c.callbackURL != "" {
		return c.callbackURL, nil
	}
	parsed, err := url.Parse(requested)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "/api/v1/auth/google-connections/callback" && parsed.Path != "/api/proxy/auth/google-connections/callback") {
		return "", errors.New("invalid callback URL")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1")) {
		return "", errors.New("callback URL must use HTTPS")
	}
	requestOrigin := ctx.Request().Header.Get("Origin")
	if requestOrigin != "" {
		origin, originErr := url.Parse(requestOrigin)
		if originErr != nil || !strings.EqualFold(origin.Scheme, parsed.Scheme) || !strings.EqualFold(origin.Host, parsed.Host) {
			return "", errors.New("callback URL origin mismatch")
		}
	}
	return parsed.String(), nil
}

func (c *GoogleConnectionsController) redirectResult(ctx echo.Context, returnTo, status, result string) error {
	target, err := url.Parse(sanitizeReturnTo(returnTo))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid redirect target").SetInternal(err)
	}
	query := target.Query()
	query.Set("google_link", status)
	query.Set("google_result", result)
	target.RawQuery = query.Encode()
	return ctx.Redirect(http.StatusFound, target.String())
}

func (c *GoogleConnectionsController) listConnections(ctx context.Context) ([]googleConnection, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: googleConnectionLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]googleConnection, 0, len(secrets.Items))
	for i := range secrets.Items {
		var item googleConnection
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &item) == nil {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (c *GoogleConnectionsController) listIdentities(ctx context.Context) ([]googleIdentity, error) {
	secrets, err := c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: googleIdentityLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]googleIdentity, 0, len(secrets.Items))
	for i := range secrets.Items {
		var item googleIdentity
		if json.Unmarshal(secrets.Items[i].Data["record.json"], &item) == nil {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *GoogleConnectionsController) saveConnection(ctx context.Context, connection googleConnection, clientSecret, rv string) error {
	record, err := json.Marshal(connection)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: googleConnectionSecretName(connection.ID), Namespace: c.namespace, ResourceVersion: rv, Labels: map[string]string{googleConnectionLabel: "true"}}, Data: map[string][]byte{"record.json": record}}
	if connection.SecretSource == "encrypted" && clientSecret != "" {
		secret.Data["client_secret"] = []byte(clientSecret)
	}
	if rv == "" {
		created, createErr := c.client.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{})
		if createErr == nil && created == nil {
			createErr = errors.New("created Google connection Secret is missing")
		}
		err = createErr
	} else {
		updated, updateErr := c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if updateErr == nil && updated == nil {
			updateErr = errors.New("updated Google connection Secret is missing")
		}
		err = updateErr
	}
	return err
}
