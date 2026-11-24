package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureManagerCredentialsReenrollsRejectedSecret(t *testing.T) {
	t.Setenv("INSTALL_TEST_API_KEY", "api-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/runtime-profile"):
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"invalid manager token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session-managers":
			_, _ = w.Write([]byte(`{"session_managers":[{"id":"manager-1","name":"manager","install_pool":"dev","labels":{"namespace":"sessions","release":"manager"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session-managers/manager-1/registration-token":
			_, _ = w.Write([]byte(`{"registration_token":"registration-2"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session-managers/enroll":
			_, _ = w.Write([]byte(`{"id":"manager-1","connection_token":"connection-2"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "manager-parent", Namespace: "sessions"}, Data: map[string][]byte{
		"manager-id": []byte("manager-1"), "connection-token": []byte("connection-1"), "pool": []byte("dev"), "upstream-url": []byte(apiBaseURL(server.URL)),
	}})
	opts := sessionManagerInstallOptions{upstream: server.URL, apiKeyEnv: "INSTALL_TEST_API_KEY", scope: "user", name: "manager", pool: "dev", namespace: "sessions", release: "manager", instanceID: "sessions/manager", connectionSecret: "manager-parent"}
	result, err := ensureManagerCredentials(context.Background(), client, opts)
	require.NoError(t, err)
	require.Equal(t, "connection-2", result.ConnectionToken)
	secret, err := client.CoreV1().Secrets("sessions").Get(context.Background(), "manager-parent", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "connection-2", string(secret.Data["connection-token"]))
}
