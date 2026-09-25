package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"k8s.io/client-go/kubernetes/fake"
)

type oneTimeSecretManagerStub struct {
	session entities.Session
	token   string
}

func (m oneTimeSecretManagerStub) GetSession(id string) entities.Session {
	if m.session != nil && m.session.ID() == id {
		return m.session
	}
	return nil
}

func (m oneTimeSecretManagerStub) ValidateSessionControlToken(id, token string) bool {
	return m.session != nil && id == m.session.ID() && token == m.token
}

type oneTimeSecretRouteRepo struct{ route *portrepos.SessionRoute }

func (r oneTimeSecretRouteRepo) Save(context.Context, *portrepos.SessionRoute) error { return nil }
func (r oneTimeSecretRouteRepo) Get(_ context.Context, id string) (*portrepos.SessionRoute, error) {
	if r.route != nil && r.route.SessionID == id {
		return r.route, nil
	}
	return nil, nil
}
func (r oneTimeSecretRouteRepo) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return nil, nil
}
func (r oneTimeSecretRouteRepo) Delete(context.Context, string) error { return nil }

func TestSessionSecretCreateAndConsumeOnce(t *testing.T) {
	client := fake.NewSimpleClientset()
	session := entities.NewProxySession("session-1", "user-1", entities.ScopeUser, "", nil, time.Now())
	controller := NewSessionSecretController(client, "default", oneTimeSecretManagerStub{session: session, token: "session-token"}, nil)
	fixedNow := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return fixedNow }

	e := echo.New()
	createReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"very-secret","expires_in_seconds":60}`))
	createReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	createRec := httptest.NewRecorder()
	createCtx := e.NewContext(createReq, createRec)
	createCtx.SetParamNames("sessionId")
	createCtx.SetParamValues("session-1")
	createCtx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true, CanCreate: true},
	})
	require.NoError(t, controller.Create(createCtx))
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created struct {
		SecretID string `json:"secret_id"`
		LocalURL string `json:"local_url"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.NotEmpty(t, created.SecretID)
	require.Equal(t, "http://127.0.0.1:9001/one-time-secrets/"+created.SecretID, created.LocalURL)
	require.NotContains(t, createRec.Body.String(), "very-secret")

	consume := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.SetParamNames("sessionId", "secretId")
		ctx.SetParamValues("session-1", created.SecretID)
		require.NoError(t, controller.Consume(ctx))
		return rec
	}
	require.Equal(t, http.StatusUnauthorized, consume("wrong").Code)
	first := consume("session-token")
	require.Equal(t, http.StatusOK, first.Code)
	require.JSONEq(t, `{"value":"very-secret"}`, first.Body.String())
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusNotFound, consume("session-token").Code)

	reauthorizeReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"expires_in_seconds":120}`))
	reauthorizeReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	reauthorizeRec := httptest.NewRecorder()
	reauthorizeCtx := e.NewContext(reauthorizeReq, reauthorizeRec)
	reauthorizeCtx.SetParamNames("sessionId", "secretId")
	reauthorizeCtx.SetParamValues("session-1", created.SecretID)
	reauthorizeCtx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true, CanCreate: true},
	})
	require.NoError(t, controller.Reauthorize(reauthorizeCtx))
	require.Equal(t, http.StatusOK, reauthorizeRec.Code)
	require.NotContains(t, reauthorizeRec.Body.String(), "very-secret")

	second := consume("session-token")
	require.Equal(t, http.StatusOK, second.Code)
	require.JSONEq(t, `{"value":"very-secret"}`, second.Body.String())
	require.Equal(t, http.StatusNotFound, consume("session-token").Code)
}

func TestSessionSecretDirectRuntimeAuthAndExpiry(t *testing.T) {
	tokenHash := sha256.Sum256([]byte("runtime-token"))
	routes := oneTimeSecretRouteRepo{route: &portrepos.SessionRoute{
		SessionID: "session-2", UserID: "user-1", Scope: string(entities.ScopeUser),
		Transport: portrepos.SessionRouteTransportDirectRuntime, RuntimeTokenHash: hex.EncodeToString(tokenHash[:]), Generation: 3,
	}}
	client := fake.NewSimpleClientset()
	controller := NewSessionSecretController(client, "default", oneTimeSecretManagerStub{}, routes)
	fixedNow := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return fixedNow }

	e := echo.New()
	createReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"expired","expires_in_seconds":1}`))
	createReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	createRec := httptest.NewRecorder()
	createCtx := e.NewContext(createReq, createRec)
	createCtx.SetParamNames("sessionId")
	createCtx.SetParamValues("session-2")
	createCtx.Set("authz_context", &auth.AuthorizationContext{User: entities.NewUser("user-1", entities.UserTypeRegular, "user-1"), PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true, CanCreate: true}})
	require.NoError(t, controller.Create(createCtx))
	var created struct {
		SecretID string `json:"secret_id"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	controller.now = func() time.Time { return fixedNow.Add(2 * time.Second) }
	req := httptest.NewRequest(http.MethodGet, "/?generation=3", nil)
	req.Header.Set("Authorization", "Bearer runtime-token")
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId", "secretId")
	ctx.SetParamValues("session-2", created.SecretID)
	require.NoError(t, controller.Consume(ctx))
	require.Equal(t, http.StatusGone, rec.Code)
}

func TestSessionSecretConsumeNextWithoutID(t *testing.T) {
	client := fake.NewSimpleClientset()
	session := entities.NewProxySession("session-next", "user-1", entities.ScopeUser, "", nil, time.Now())
	controller := NewSessionSecretController(client, "default", oneTimeSecretManagerStub{session: session, token: "session-token"}, nil)
	fixedNow := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return fixedNow }

	e := echo.New()
	createReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"next-secret"}`))
	createReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	createCtx := e.NewContext(createReq, httptest.NewRecorder())
	createCtx.SetParamNames("sessionId")
	createCtx.SetParamValues("session-next")
	createCtx.Set("authz_context", &auth.AuthorizationContext{User: entities.NewUser("user-1", entities.UserTypeRegular, "user-1"), PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true, CanCreate: true}})
	require.NoError(t, controller.Create(createCtx))

	consume := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer session-token")
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.SetParamNames("sessionId")
		ctx.SetParamValues("session-next")
		require.NoError(t, controller.ConsumeNext(ctx))
		return rec
	}
	first := consume()
	require.Equal(t, http.StatusOK, first.Code)
	require.JSONEq(t, `{"value":"next-secret"}`, first.Body.String())
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusNotFound, consume().Code)
}
