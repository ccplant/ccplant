package cmd

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

func TestParseCodexAuthChallenge(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	result := make(chan codexauth.Challenge, 1)
	errs := make(chan error, 1)
	parseCodexAuthChallenge(strings.NewReader("Open https://auth.openai.com/codex/device\nEnter ABCD-EFGH\n"), result, errs)
	select {
	case challenge := <-result:
		if challenge.UserCode != "ABCD-EFGH" || challenge.VerificationURI != "https://auth.openai.com/codex/device" {
			t.Fatalf("unexpected challenge: %#v", challenge)
		}
	case err := <-errs:
		t.Fatal(err)
	}
	if output := logs.String(); strings.Contains(output, "ABCD-EFGH") || strings.Contains(output, "auth.openai.com") {
		t.Fatalf("diagnostic logs leaked challenge data: %q", output)
	}
}

func TestParseCodexAuthChallengeRejectsOversizedOutput(t *testing.T) {
	result := make(chan codexauth.Challenge, 1)
	errs := make(chan error, 1)
	parseCodexAuthChallenge(strings.NewReader(strings.Repeat("x", 20<<10)), result, errs)
	select {
	case challenge := <-result:
		t.Fatalf("unexpected challenge: %#v", challenge)
	case err := <-errs:
		if err == nil {
			t.Fatal("expected scanner error")
		}
	}
}

func TestPostCodexAuthJSONAcceptsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("unexpected authorization header: %q", got)
		}
		if r.URL.Path != "/cda-test/challenge" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	request := codexauth.WorkloadRequest{AttemptID: "cda-test", CallbackURL: server.URL, Token: "token"}
	if err := postCodexAuthJSON(context.Background(), request, "challenge", codexauth.Challenge{UserCode: "ABCD-EFGH"}); err != nil {
		t.Fatalf("expected callback to succeed: %v", err)
	}
}

// A callback endpoint behind an authentication proxy answers with a redirect to
// a login page. Following that redirect returns HTTP 200 HTML, which previously
// looked like a successful report and left the attempt stuck on "starting".
func TestPostCodexAuthJSONRejectsRedirectToLoginPage(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><title>Sign in</title></html>"))
	}))
	defer login.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, login.URL, http.StatusFound)
	}))
	defer proxy.Close()

	request := codexauth.WorkloadRequest{AttemptID: "cda-test", CallbackURL: proxy.URL, Token: "token"}
	err := postCodexAuthJSON(context.Background(), request, "challenge", codexauth.Challenge{UserCode: "ABCD-EFGH"})
	if err == nil {
		t.Fatal("expected redirect to be rejected")
	}
	if !strings.Contains(err.Error(), "302") {
		t.Fatalf("expected redirect status in error, got %v", err)
	}
}

// Some proxies terminate the redirect themselves and return the login page with
// HTTP 200. HTML content must never be treated as an API acknowledgement.
func TestPostCodexAuthJSONRejectsHTMLSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>Sign in</title></html>"))
	}))
	defer server.Close()

	request := codexauth.WorkloadRequest{AttemptID: "cda-test", CallbackURL: server.URL, Token: "token"}
	err := postCodexAuthJSON(context.Background(), request, "challenge", codexauth.Challenge{UserCode: "ABCD-EFGH"})
	if err == nil {
		t.Fatal("expected HTML response to be rejected")
	}
	if !strings.Contains(err.Error(), "HTML") {
		t.Fatalf("expected HTML error, got %v", err)
	}
}
