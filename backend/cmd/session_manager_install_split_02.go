package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

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

type managerCredentialPersistence struct {
	existing    *corev1.Secret
	opts        sessionManagerInstallOptions
	credentials *installedManagerCredentials
}

func persistManagerCredentials(ctx context.Context, secrets typedcorev1.SecretInterface, persistence managerCredentialPersistence) error {
	secret := persistence.existing
	opts := persistence.opts
	credentials := persistence.credentials
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
	var persisted *corev1.Secret
	if persistence.existing == nil {
		persisted, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	} else {
		persisted, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("persist connection Secret: %w", err)
	}
	if persisted == nil {
		return errors.New("persist connection Secret returned no object")
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
		defer closeutils.SafeCloseResponseBody(listResp.Body) //nolint:errcheck
		return "", responseError(listResp)
	}
	var listed struct {
		Managers []struct {
			ID     string            `json:"id"`
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"session_managers"`
	}
	if err = json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		closeutils.SafeCloseResponseBody(listResp.Body)
		return "", err
	}
	closeutils.SafeCloseResponseBody(listResp.Body)
	for _, manager := range listed.Managers {
		if manager.Name != opts.name || manager.Labels["namespace"] != opts.namespace || manager.Labels["release"] != opts.release {
			continue
		}
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/session-managers/"+url.PathEscape(manager.ID)+"/registration-token", nil)
		if requestErr != nil {
			return "", requestErr
		}
		req.Header.Set("X-API-Key", apiKey)
		return requestRegistrationToken(req, http.StatusOK)
	}
	payload, err := json.Marshal(map[string]any{"name": opts.name, "scope": opts.scope, "team_id": opts.teamID,
		"labels": map[string]string{"type": "kubernetes", "namespace": opts.namespace, "release": opts.release}})
	if err != nil {
		return "", fmt.Errorf("encode manager registration request: %w", err)
	}
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
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
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
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
}

func enrollKubernetesManager(ctx context.Context, opts sessionManagerInstallOptions) (*installedManagerCredentials, error) {
	payload, err := json.Marshal(map[string]any{"registration_token": opts.registrationToken, "instance_id": opts.instanceID,
		"labels":       map[string]string{"type": "kubernetes", "namespace": opts.namespace, "release": opts.release},
		"capabilities": []string{"runner_claim_v1", "direct_session_runtime_v1"}})
	if err != nil {
		return nil, fmt.Errorf("encode manager enrollment request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL(opts.upstream)+"/session-managers/enroll", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enroll manager: %w", err)
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
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

type opaqueSecretConfig struct {
	namespace, name, key string
}

func ensureOpaqueSecret(ctx context.Context, client kubernetes.Interface, config opaqueSecretConfig) error {
	secrets := client.CoreV1().Secrets(config.namespace)
	if _, err := secrets.Get(ctx, config.name, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	created, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: config.name, Namespace: config.namespace,
		Annotations: map[string]string{"helm.sh/resource-policy": "keep"}}, Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{config.key: []byte(hex.EncodeToString(random))}}, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if created == nil {
		return errors.New("create opaque Secret returned no object")
	}
	return nil
}

func apiBaseURL(raw string) string {
	value := strings.TrimRight(raw, "/")
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Path == "" || parsed.Path == "/") {
		return value + "/api/v1"
	}
	return value
}
