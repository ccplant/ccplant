package auth

import "testing"

func TestWorkerControlAPIUsesInternalTokenAuthentication(t *testing.T) {
	if !isInternalTokenEndpoint("/internal/worker/sessions") {
		t.Fatal("worker control API must bypass user authentication middleware")
	}
	if isInternalTokenEndpoint("/internal/workers") {
		t.Fatal("unrelated path must not bypass user authentication middleware")
	}
}

func TestGitHubConnectionCallbackUsesStateAuthentication(t *testing.T) {
	if !isOAuthEndpoint("/auth/github-connections/callback") {
		t.Fatal("GitHub connection callback must bypass bearer authentication and validate its one-time state")
	}
	if isOAuthEndpoint("/auth/github-connections") {
		t.Fatal("authenticated GitHub connection endpoints must not bypass authentication")
	}
}
