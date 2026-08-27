package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

type sessionManagerInstallOptions struct {
	targetType, upstream, publicURL, registrationToken, registrationTokenFile string
	namespace, release, chart, version, pool, name, instanceID                string
	connectionSecret, internalSecret, provisionerSecret                       string
	createNamespace, wait                                                     bool
	timeout                                                                   string
}

type installedManagerCredentials struct {
	ManagerID, ConnectionToken string
}

func newSessionManagerInstallCommand() *cobra.Command {
	var opts sessionManagerInstallOptions
	command := &cobra.Command{
		Use:   "install",
		Short: "Install and enroll a session manager",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionManagerInstall(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, execInstallCommandRunner{})
		},
	}
	flags := command.Flags()
	flags.StringVar(&opts.targetType, "type", "kubernetes", "installation type (kubernetes)")
	flags.StringVar(&opts.upstream, "upstream", "", "parent API base URL (for example https://dev.ccplant.com/api/v1)")
	flags.StringVar(&opts.publicURL, "public-url", "", "manager URL reachable by the parent (defaults to the in-cluster Service URL)")
	flags.StringVar(&opts.registrationToken, "registration-token", "", "one-time registration token (initial install only)")
	flags.StringVar(&opts.registrationTokenFile, "registration-token-file", "", "file containing a one-time registration token")
	flags.StringVarP(&opts.namespace, "namespace", "n", "ccplant-session", "Kubernetes namespace")
	flags.StringVar(&opts.release, "release", "session-manager", "Helm release name")
	flags.StringVar(&opts.chart, "chart", "oci://ghcr.io/ccplant/charts/session-manager", "session-manager Helm chart")
	flags.StringVar(&opts.version, "version", "", "Helm chart version")
	flags.StringVar(&opts.pool, "pool", "default", "logical pool supplied by this manager")
	flags.StringVar(&opts.name, "name", "", "manager display name")
	flags.StringVar(&opts.instanceID, "instance-id", "", "stable manager instance ID")
	flags.StringVar(&opts.connectionSecret, "connection-secret", "", "Secret holding manager credentials")
	flags.StringVar(&opts.internalSecret, "internal-secret", "", "Secret holding the internal API token")
	flags.StringVar(&opts.provisionerSecret, "provisioner-secret", "", "Secret holding the provisioner token")
	flags.BoolVar(&opts.createNamespace, "create-namespace", true, "create the namespace if missing")
	flags.BoolVar(&opts.wait, "wait", true, "wait for Helm resources to become ready")
	flags.StringVar(&opts.timeout, "timeout", "10m", "Helm operation timeout")
	_ = command.MarkFlagRequired("upstream")
	return command
}

func runSessionManagerInstall(ctx context.Context, stdout, stderr io.Writer, opts sessionManagerInstallOptions, runner installCommandRunner) error {
	if opts.targetType != "kubernetes" {
		return fmt.Errorf("unsupported session manager type %q", opts.targetType)
	}
	if opts.registrationToken != "" && opts.registrationTokenFile != "" {
		return errors.New("use only one of --registration-token and --registration-token-file")
	}
	if opts.registrationTokenFile != "" {
		data, err := os.ReadFile(opts.registrationTokenFile)
		if err != nil {
			return err
		}
		opts.registrationToken = strings.TrimSpace(string(data))
	}
	if opts.name == "" {
		opts.name = opts.release
	}
	if opts.instanceID == "" {
		opts.instanceID = opts.namespace + "/" + opts.release
	}
	if opts.connectionSecret == "" {
		opts.connectionSecret = opts.release + "-parent"
	}
	if opts.internalSecret == "" {
		opts.internalSecret = opts.release + "-internal"
	}
	if opts.provisionerSecret == "" {
		opts.provisionerSecret = opts.release + "-provisioner"
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	if opts.createNamespace {
		_, err = client.CoreV1().Namespaces().Get(ctx, opts.namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opts.namespace}}, metav1.CreateOptions{})
		}
		if err != nil {
			return fmt.Errorf("ensure namespace: %w", err)
		}
	}
	credentials, err := ensureManagerCredentials(ctx, client, opts)
	if err != nil {
		return err
	}
	if err = ensureOpaqueSecret(ctx, client, opts.namespace, opts.internalSecret, "token"); err != nil {
		return err
	}
	if err = ensureOpaqueSecret(ctx, client, opts.namespace, opts.provisionerSecret, "provisioner-token"); err != nil {
		return err
	}

	publicURL := opts.publicURL
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", opts.release, opts.namespace)
	}
	values := map[string]any{
		"fullnameOverride": opts.release,
		"parent": map[string]any{"url": apiBaseURL(opts.upstream), "publicUrl": publicURL,
			"connectionTokenSecretRef": map[string]any{"name": opts.connectionSecret, "key": "connection-token"},
			"hmacSecretRef":            map[string]any{"name": opts.connectionSecret, "key": "hmac-secret"}},
		"runner":      map[string]any{"managerId": credentials.ManagerID, "pool": opts.pool},
		"internalApi": map[string]any{"tokenSecretRef": map[string]any{"name": opts.internalSecret, "key": "token"}},
		"session":     map[string]any{"provisioner": map[string]any{"tokenSecretRef": map[string]any{"name": opts.provisionerSecret, "key": "provisioner-token"}}},
	}
	data, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "ccplant-session-manager-values-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	args := []string{"upgrade", "--install", opts.release, opts.chart, "--namespace", opts.namespace, "--values", tmpName, "--timeout", opts.timeout}
	if opts.createNamespace {
		args = append(args, "--create-namespace")
	}
	if opts.wait {
		args = append(args, "--wait")
	}
	if opts.version != "" {
		args = append(args, "--version", opts.version)
	}
	if err = runner.Run(ctx, stdout, stderr, "helm", args...); err != nil {
		return fmt.Errorf("helm upgrade --install: %w", err)
	}
	if _, err = fmt.Fprintf(stdout, "Session manager %s installed in namespace %s (manager %s)\n", opts.release, opts.namespace, credentials.ManagerID); err != nil {
		return err
	}
	return nil
}

func ensureManagerCredentials(ctx context.Context, client kubernetes.Interface, opts sessionManagerInstallOptions) (*installedManagerCredentials, error) {
	secrets := client.CoreV1().Secrets(opts.namespace)
	existing, err := secrets.Get(ctx, opts.connectionSecret, metav1.GetOptions{})
	if err == nil {
		managerID, token := string(existing.Data["manager-id"]), string(existing.Data["connection-token"])
		if managerID == "" || token == "" {
			return nil, fmt.Errorf("secret %s/%s is missing manager-id or connection-token", opts.namespace, opts.connectionSecret)
		}
		if opts.registrationToken != "" {
			return nil, errors.New("registration token must not be supplied on upgrade; existing connection Secret is reused")
		}
		return &installedManagerCredentials{ManagerID: managerID, ConnectionToken: token}, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("read connection Secret: %w", err)
	}
	if opts.registrationToken == "" {
		return nil, errors.New("--registration-token or --registration-token-file is required for the initial install")
	}
	registration, err := enrollKubernetesManager(ctx, opts)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: opts.connectionSecret, Namespace: opts.namespace,
		Annotations: map[string]string{"helm.sh/resource-policy": "keep"}}, Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"manager-id": []byte(registration.ManagerID), "connection-token": []byte(registration.ConnectionToken), "hmac-secret": []byte(registration.ConnectionToken)}}
	if _, err = secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("persist connection Secret: %w", err)
	}
	return registration, nil
}

func enrollKubernetesManager(ctx context.Context, opts sessionManagerInstallOptions) (*installedManagerCredentials, error) {
	payload, _ := json.Marshal(map[string]any{"registration_token": opts.registrationToken, "instance_id": opts.instanceID,
		"pool":         opts.pool,
		"default":      true,
		"labels":       map[string]string{"type": "kubernetes", "namespace": opts.namespace, "release": opts.release},
		"capabilities": []string{"runner_claim_v1", "direct_session_runtime_v1"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL(opts.upstream)+"/session-managers/enroll", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enroll manager: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var result struct {
		ID              string `json:"id"`
		ConnectionToken string `json:"connection_token"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.ID == "" || result.ConnectionToken == "" {
		return nil, errors.New("enrollment response did not contain manager credentials")
	}
	return &installedManagerCredentials{ManagerID: result.ID, ConnectionToken: result.ConnectionToken}, nil
}

func ensureOpaqueSecret(ctx context.Context, client kubernetes.Interface, namespace, name, key string) error {
	secrets := client.CoreV1().Secrets(namespace)
	if _, err := secrets.Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	_, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace,
		Annotations: map[string]string{"helm.sh/resource-policy": "keep"}}, Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{key: []byte(hex.EncodeToString(random))}}, metav1.CreateOptions{})
	return err
}

func apiBaseURL(raw string) string {
	value := strings.TrimRight(raw, "/")
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Path == "" || parsed.Path == "/") {
		return value + "/api/v1"
	}
	return value
}
