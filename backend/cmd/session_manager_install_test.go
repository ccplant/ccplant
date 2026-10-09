package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRequiresLegacySessionManagerLeaseMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("new install", func(t *testing.T) {
		migration, err := requiresLegacySessionManagerLeaseMigration(ctx, fake.NewSimpleClientset(), "sessions", "manager")
		require.NoError(t, err)
		require.False(t, migration)
	})

	t.Run("legacy deployment", func(t *testing.T) {
		client := fake.NewSimpleClientset(sessionManagerDeploymentForLeaseMigrationTest(nil))
		migration, err := requiresLegacySessionManagerLeaseMigration(ctx, client, "sessions", "manager")
		require.NoError(t, err)
		require.True(t, migration)
	})

	t.Run("already migrated", func(t *testing.T) {
		client := fake.NewSimpleClientset(sessionManagerDeploymentForLeaseMigrationTest([]corev1.EnvVar{{
			Name: "AGENTAPI_SESSION_MANAGER_ALLOCATION_LEASE_NAME", Value: "manager",
		}}))
		migration, err := requiresLegacySessionManagerLeaseMigration(ctx, client, "sessions", "manager")
		require.NoError(t, err)
		require.False(t, migration)
	})
}

func TestSessionManagerInstallPersistenceFlags(t *testing.T) {
	t.Parallel()
	command := newSessionManagerInstallCommand()

	require.Equal(t, "false", command.Flags().Lookup("persistence").DefValue)
	require.Equal(t, "", command.Flags().Lookup("storage-class").DefValue)
	require.Equal(t, "10Gi", command.Flags().Lookup("persistence-size").DefValue)
}

func TestSessionManagerInstallRejectsInvalidPersistenceSize(t *testing.T) {
	t.Parallel()
	command := newSessionManagerInstallCommand()
	command.SetArgs([]string{
		"--upstream", "https://ccplant.example.com",
		"--persistence",
		"--persistence-size", "invalid",
	})

	err := command.ExecuteContext(context.Background())
	require.ErrorContains(t, err, "--persistence-size must be a positive Kubernetes resource quantity")
}

func TestSessionManagerInstallValuesIncludesSessionPVC(t *testing.T) {
	t.Parallel()
	opts := sessionManagerInstallOptions{
		upstream:          "https://ccplant.example.com",
		release:           "manager",
		pool:              "builders",
		connectionSecret:  "manager-parent",
		internalSecret:    "manager-internal",
		provisionerSecret: "manager-provisioner",
		persistence:       true,
		storageClass:      "fast",
		persistenceSize:   "20Gi",
	}

	values := sessionManagerInstallValues(opts, &installedManagerCredentials{ManagerID: "manager-1"}, false)
	session := values["session"].(map[string]any)
	pvc := session["pvc"].(map[string]any)
	require.Equal(t, true, pvc["enabled"])
	require.Equal(t, "fast", pvc["storageClass"])
	require.Equal(t, "20Gi", pvc["storageSize"])
}

func TestSessionManagerHelmUpgradeArgsForceServerSideApplyConflicts(t *testing.T) {
	t.Parallel()
	opts := sessionManagerInstallOptions{
		release:         "manager",
		chart:           "oci://example.com/session-manager",
		namespace:       "sessions",
		timeout:         "10m",
		createNamespace: true,
		wait:            true,
		version:         "v1.2.3",
	}

	args := sessionManagerHelmUpgradeArgs(opts, "/tmp/values.yaml", false)

	require.Equal(t, []string{
		"upgrade", "--install", "manager", "oci://example.com/session-manager",
		"--namespace", "sessions",
		"--values", "/tmp/values.yaml",
		"--timeout", "10m",
		"--force-conflicts",
		"--create-namespace",
		"--wait",
		"--version", "v1.2.3",
	}, args)
}

func sessionManagerDeploymentForLeaseMigrationTest(env []corev1.EnvVar) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "sessions"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "session-manager", Env: env,
		}}}}},
	}
}

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
	require.Equal(t, "sessions/manager", string(secret.Data["instance-id"]))
	require.Equal(t, apiBaseURL(server.URL), string(secret.Data["upstream-url"]))
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

}

func TestEnsureManagerCredentialsKeepsManagerIDAcrossRepeatedInstalls(t *testing.T) {
	t.Parallel()
	var enrollments atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session-managers/enroll":
			enrollments.Add(1)
			_, _ = w.Write([]byte(`{"id":"manager-1","connection_token":"connection-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/internal/session-managers/manager-1/runtime-profile":
			require.Equal(t, "Bearer connection-1", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"revision":"test"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := fake.NewSimpleClientset()
	initial := sessionManagerInstallOptions{
		upstream:          server.URL,
		registrationToken: "registration-1",
		namespace:         "sessions",
		release:           "manager",
		pool:              "dev",
		instanceID:        "sessions/manager",
		connectionSecret:  "manager-parent",
	}
	installed, err := ensureManagerCredentials(context.Background(), client, initial)
	require.NoError(t, err)

	upgrade := initial
	upgrade.registrationToken = ""
	updated, err := ensureManagerCredentials(context.Background(), client, upgrade)
	require.NoError(t, err)
	require.Equal(t, installed.ManagerID, updated.ManagerID)
	require.Equal(t, installed.ConnectionToken, updated.ConnectionToken)
	require.Equal(t, int32(1), enrollments.Load())
}

func TestEnsureManagerCredentialsRejectsRegistrationTokenWhenSecretExists(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "manager-parent", Namespace: "sessions"}, Data: map[string][]byte{
		"manager-id": []byte("manager-1"), "connection-token": []byte("connection-1"),
	}})
	opts := sessionManagerInstallOptions{
		registrationToken: "registration-2",
		namespace:         "sessions",
		connectionSecret:  "manager-parent",
	}

	_, err := ensureManagerCredentials(context.Background(), client, opts)
	require.ErrorContains(t, err, "--registration-token is only valid for the initial install")

	secret, getErr := client.CoreV1().Secrets("sessions").Get(context.Background(), "manager-parent", metav1.GetOptions{})
	require.NoError(t, getErr)
	require.Equal(t, "manager-1", string(secret.Data["manager-id"]))
	require.Equal(t, "connection-1", string(secret.Data["connection-token"]))
}

func TestEnsureManagerCredentialsRequiresRegistrationTokenInitially(t *testing.T) {
	t.Parallel()
	_, err := ensureManagerCredentials(context.Background(), fake.NewSimpleClientset(), sessionManagerInstallOptions{namespace: "sessions", connectionSecret: "manager-parent"})
	require.ErrorContains(t, err, "registration-token is required")
}

func TestEnsureManagerCredentialsIssuesTokenAndEnrolls(t *testing.T) {
	t.Setenv("INSTALL_TEST_API_KEY", "api-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session-managers/enroll" {
			require.Equal(t, "api-key", r.Header.Get("X-API-Key"))
		}
		switch r.URL.Path {
		case "/api/v1/session-managers":
			_, _ = w.Write([]byte(`{"session_managers":[]}`))
		case "/api/v1/session-managers/registration-tokens":
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.NotContains(t, payload, "pool")
			require.NotContains(t, payload, "default")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"registration_token":"registration-1"}`))
		case "/api/v1/session-managers/enroll":
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.NotContains(t, payload, "pool")
			require.NotContains(t, payload, "default")
			_, _ = w.Write([]byte(`{"id":"manager-1","connection_token":"connection-1"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	opts := sessionManagerInstallOptions{upstream: server.URL, apiKeyEnv: "INSTALL_TEST_API_KEY", scope: "user", name: "manager", pool: "dev", namespace: "sessions", release: "manager", instanceID: "sessions/manager", connectionSecret: "manager-parent"}
	result, err := ensureManagerCredentials(context.Background(), fake.NewSimpleClientset(), opts)
	require.NoError(t, err)
	require.Equal(t, "manager-1", result.ManagerID)
}

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
