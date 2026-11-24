package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func BenchmarkGitHubOAuth(b *testing.B) {
	mockServer := newOAuthBenchmarkServer(b)
	defer mockServer.Close()

	githubCfg := &config.GitHubAuthConfig{Enabled: true, BaseURL: mockServer.URL}
	oauthCfg := &config.GitHubOAuthConfig{ClientID: "bench-client", ClientSecret: "bench-secret", BaseURL: mockServer.URL}
	authProvider := NewGitHubAuthProvider(githubCfg)
	oauthProvider := NewGitHubOAuthProvider(oauthCfg, NewGitHubAuthProvider(githubCfg))
	ctx := context.Background()

	b.Run("GenerateAuthURL", func(b *testing.B) {
		var authURL, state string
		for i := 0; i < b.N; i++ {
			var err error
			authURL, state, err = oauthProvider.GenerateAuthURL("http://localhost/callback")
			benchmarkOAuthErr = err
		}
		benchmarkOAuthStrings = authURL + state
	})

	b.Run("ExchangeCode", func(b *testing.B) {
		authURL, state, err := oauthProvider.GenerateAuthURL("http://localhost/callback")
		benchmarkOAuthErr = err
		benchmarkOAuthStrings = authURL
		b.ResetTimer()
		var userContext *UserContext
		for i := 0; i < b.N; i++ {
			userContext, err = oauthProvider.ExchangeCode(ctx, "bench-code", state)
			benchmarkOAuthErr = err
		}
		benchmarkOAuthUser = userContext
	})

	b.Run("Authenticate", func(b *testing.B) {
		var userContext *UserContext
		var err error
		for i := 0; i < b.N; i++ {
			userContext, err = authProvider.Authenticate(ctx, "bench_token")
			benchmarkOAuthErr = err
		}
		benchmarkOAuthUser = userContext
	})
}

func newOAuthBenchmarkServer(b *testing.B) *httptest.Server {
	b.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			writeOAuthBenchmarkResponse(b, w, `{"access_token":"bench_token","token_type":"bearer"}`)
		case "/user":
			writeOAuthBenchmarkResponse(b, w, `{"login":"benchuser","id":123}`)
		case "/user/orgs":
			writeOAuthBenchmarkResponse(b, w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

var benchmarkOAuthStrings string
var benchmarkOAuthUser *UserContext
var benchmarkOAuthErr error

func writeOAuthBenchmarkResponse(b *testing.B, w http.ResponseWriter, body string) {
	b.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		b.Errorf("write benchmark response: %v", err)
	}
}
