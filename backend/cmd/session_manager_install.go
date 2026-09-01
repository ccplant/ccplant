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
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

type sessionManagerInstallOptions struct {
	targetType, upstream, publicURL, registrationToken, registrationTokenFile string
	apiKeyEnv, apiKeyFile, scope, teamID                                      string
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
	flags.StringVar(&opts.apiKeyEnv, "api-key-env", "AGENTAPI_KEY", "environment variable containing the parent API key")
	flags.StringVar(&opts.apiKeyFile, "api-key-file", "", "file containing the parent API key")
	flags.StringVar(&opts.scope, "scope", "user", "manager ownership scope (user or team)")
	flags.StringVar(&opts.teamID, "team-id", "", "manager owner team when --scope=team")
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
	if opts.scope != "user" && opts.scope != "team" {
		return errors.New("--scope must be user or team")
	}
	if opts.scope == "team" && strings.TrimSpace(opts.teamID) == "" {
		return errors.New("--team-id is required when --scope=team")
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
	if err = verifyManagerCredential(ctx, opts.upstream, credentials); err != nil {
		return fmt.Errorf("verify installed manager credentials: %w", err)
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
		credentials := &installedManagerCredentials{ManagerID: managerID, ConnectionToken: token}
		storedPool, storedUpstream := string(existing.Data["pool"]), string(existing.Data["upstream-url"])
		drifted := (storedPool != "" && storedPool != opts.pool) || (storedUpstream != "" && storedUpstream != apiBaseURL(opts.upstream))
		if !drifted && opts.registrationToken == "" {
			if verifyErr := verifyManagerCredential(ctx, opts.upstream, credentials); verifyErr == nil {
				if err = persistManagerCredentials(ctx, secrets, existing, opts, credentials); err != nil {
					return nil, err
				}
				return credentials, nil
			} else if readInstallAPIKey(opts) == "" {
				return nil, fmt.Errorf("existing manager credentials are rejected by the parent: %w; provide an API key to re-enroll", verifyErr)
			}
		}
		registration, enrollErr := enrollWithResolvedToken(ctx, opts)
		if enrollErr != nil {
			return nil, enrollErr
		}
		if err = persistManagerCredentials(ctx, secrets, existing, opts, registration); err != nil {
			return nil, err
		}
		return registration, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("read connection Secret: %w", err)
	}
	registration, err := enrollWithResolvedToken(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err = persistManagerCredentials(ctx, secrets, nil, opts, registration); err != nil {
		return nil, err
	}
	return registration, nil
}

func enrollWithResolvedToken(ctx context.Context, opts sessionManagerInstallOptions) (*installedManagerCredentials, error) {
	if opts.registrationToken == "" {
		token, err := issueManagerRegistrationToken(ctx, opts)
		if err != nil {
			return nil, err
		}
		opts.registrationToken = token
	}
	return enrollKubernetesManager(ctx, opts)
}

func persistManagerCredentials(ctx context.Context, secrets typedcorev1.SecretInterface, existing *corev1.Secret, opts sessionManagerInstallOptions, credentials *installedManagerCredentials) error {
	secret := existing
	if secret == nil {
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: opts.connectionSecret, Namespace: opts.namespace}, Type: corev1.SecretTypeOpaque}
	}
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations["helm.sh/resource-policy"] = "keep"
	secret.Data = map[string][]byte{
		"manager-id": []byte(credentials.ManagerID), "connection-token": []byte(credentials.ConnectionToken),
		"hmac-secret": []byte(credentials.ConnectionToken), "instance-id": []byte(opts.instanceID),
		"pool": []byte(opts.pool), "upstream-url": []byte(apiBaseURL(opts.upstream)),
	}
	var err error
	if existing == nil {
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	} else {
		_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("persist connection Secret: %w", err)
	}
	return nil
}

func issueManagerRegistrationToken(ctx context.Context, opts sessionManagerInstallOptions) (string, error) {
	apiKey := readInstallAPIKey(opts)
	if apiKey == "" {
		return "", errors.New("--registration-token is required unless a parent API key is available through --api-key-file or --api-key-env")
	}
	base := apiBaseURL(opts.upstream)
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session-managers", nil)
	if err != nil {
		return "", err
	}
	listReq.Header.Set("X-API-Key", apiKey)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("list session managers: %w", err)
	}
	if listResp.StatusCode != http.StatusOK {
		defer listResp.Body.Close() //nolint:errcheck
		return "", responseError(listResp)
	}
	var listed struct {
		Managers []struct {
			ID          string            `json:"id"`
			Name        string            `json:"name"`
			InstallPool string            `json:"install_pool"`
			Labels      map[string]string `json:"labels"`
		} `json:"session_managers"`
	}
	if err = json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		_ = listResp.Body.Close()
		return "", err
	}
	_ = listResp.Body.Close()
	for _, manager := range listed.Managers {
		if manager.Name != opts.name || manager.InstallPool != opts.pool || manager.Labels["namespace"] != opts.namespace || manager.Labels["release"] != opts.release {
			continue
		}
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/session-managers/"+url.PathEscape(manager.ID)+"/registration-token", nil)
		if requestErr != nil {
			return "", requestErr
		}
		req.Header.Set("X-API-Key", apiKey)
		return requestRegistrationToken(req, http.StatusOK)
	}
	payload, _ := json.Marshal(map[string]any{"name": opts.name, "scope": opts.scope, "team_id": opts.teamID, "pool": opts.pool,
		"labels": map[string]string{"type": "kubernetes", "namespace": opts.namespace, "release": opts.release}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/session-managers/registration-tokens", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	return requestRegistrationToken(req, http.StatusCreated)
}

func requestRegistrationToken(req *http.Request, expectedStatus int) (string, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("issue manager registration token: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != expectedStatus {
		return "", responseError(resp)
	}
	var result struct {
		RegistrationToken string `json:"registration_token"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.RegistrationToken == "" {
		return "", errors.New("registration response did not contain a token")
	}
	return result.RegistrationToken, nil
}

func readInstallAPIKey(opts sessionManagerInstallOptions) string {
	if opts.apiKeyFile != "" {
		data, err := os.ReadFile(opts.apiKeyFile)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		return ""
	}
	return strings.TrimSpace(os.Getenv(opts.apiKeyEnv))
}

func verifyManagerCredential(ctx context.Context, upstream string, credentials *installedManagerCredentials) error {
	if strings.TrimSpace(upstream) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL(upstream)+"/internal/session-managers/"+url.PathEscape(credentials.ManagerID)+"/runtime-profile", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.ConnectionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
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
