package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureManagerCredentialsEnrollsAndPersistsSecret(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/session-managers/enroll", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"manager-1","connection_token":"connection-1"}`))
	}))
	defer server.Close()

	client := fake.NewSimpleClientset()
	opts := sessionManagerInstallOptions{upstream: server.URL, registrationToken: "registration-1", namespace: "sessions", release: "manager", instanceID: "sessions/manager", connectionSecret: "manager-parent"}
	result, err := ensureManagerCredentials(context.Background(), client, opts)
	require.NoError(t, err)
	require.Equal(t, "manager-1", result.ManagerID)
	secret, err := client.CoreV1().Secrets("sessions").Get(context.Background(), "manager-parent", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "manager-1", string(secret.Data["manager-id"]))
	require.Equal(t, "connection-1", string(secret.Data["connection-token"]))
	require.Equal(t, "keep", secret.Annotations["helm.sh/resource-policy"])
}

func TestEnsureManagerCredentialsReusesSecretOnUpgrade(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "manager-parent", Namespace: "sessions"}, Data: map[string][]byte{
		"manager-id": []byte("manager-1"), "connection-token": []byte("connection-1"),
	}})
	opts := sessionManagerInstallOptions{namespace: "sessions", connectionSecret: "manager-parent"}
	result, err := ensureManagerCredentials(context.Background(), client, opts)
	require.NoError(t, err)
	require.Equal(t, "manager-1", result.ManagerID)

	opts.registrationToken = "must-not-be-used"
	_, err = ensureManagerCredentials(context.Background(), client, opts)
	require.ErrorContains(t, err, "must not be supplied on upgrade")
}

func TestEnsureManagerCredentialsRequiresRegistrationTokenInitially(t *testing.T) {
	t.Parallel()
	_, err := ensureManagerCredentials(context.Background(), fake.NewSimpleClientset(), sessionManagerInstallOptions{namespace: "sessions", connectionSecret: "manager-parent"})
	require.ErrorContains(t, err, "required for the initial install")
}
