package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

func encryptedControlPlanePath(path string) string {
	extension := filepath.Ext(path)
	if extension == ".yaml" || extension == ".yml" {
		return strings.TrimSuffix(path, extension) + ".sops" + extension
	}
	return path + ".sops.yaml"
}

func samePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbsolute == rightAbsolute
}

func writeNewFileAtomically(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".control-plane-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove temporary control-plane file: %v", err)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return errors.Join(err, temporary.Close())
	}
	written, writeErr := temporary.Write(data)
	if writeErr != nil || written != len(data) {
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		return errors.Join(writeErr, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// Publish without overwriting an output that appeared concurrently.
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return os.Remove(temporaryPath)
}

func defaultInstallConfig(target string) (*installConfig, error) {
	cookieEncryptionKey, err := generateCookieEncryptionKey()
	if err != nil {
		return nil, err
	}
	cfg := &installConfig{APIVersion: installConfigAPIVersion, Kind: "ControlPlane", Metadata: installConfigMetadata{Name: "ccplant"}, Spec: installConfigSpec{Version: "latest", Frontend: installEndpoint{Hostname: "agentapi.local"}, Backend: installEndpoint{Hostname: "api.agentapi.local"}, Persistence: installPersistence{Enabled: true, Size: "10Gi"}, Secrets: installSecrets{CookieEncryptionKey: cookieEncryptionKey}}, Target: installTarget{Type: target}}
	switch target {
	case "kubernetes":
		cfg.Target.Kubernetes = &installKubernetesTarget{Namespace: "ccplant", Chart: defaultCCPlantChart}
	case "compose":
		cfg.Spec.Frontend.Hostname, cfg.Spec.Backend.Hostname = "localhost", "localhost"
		cfg.Target.Compose = &installComposeTarget{ProjectName: "ccplant", Output: "compose.generated.yaml", FrontendPort: 3000, BackendPort: 8080}
	default:
		return nil, fmt.Errorf("unsupported target %q (supported: kubernetes, compose)", target)
	}
	return cfg, nil
}

func newControlPlaneApplyCommand() *cobra.Command {
	var file, ageKeyFile string
	var dryRun bool
	command := &cobra.Command{Use: "apply", Short: "Validate and apply a control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadInstallConfig(cmd.Context(), file, ageKeyFile)
		if err != nil {
			return err
		}
		adapter, err := adapterForInstallTarget(cfg.Target.Type)
		if err != nil {
			return err
		}
		options := installApplyOptions{DryRun: dryRun, Runner: execInstallCommandRunner{}}
		if cfg.Target.Type == "kubernetes" {
			restConfig, configErr := ctrl.GetConfig()
			if configErr != nil {
				return fmt.Errorf("get Kubernetes config: %w", configErr)
			}
			options.Client, configErr = kubernetes.NewForConfig(restConfig)
			if configErr != nil {
				return fmt.Errorf("create Kubernetes client: %w", configErr)
			}
		}
		return adapter.Apply(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cfg, options)
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "control plane configuration")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and render without applying")
	command.Flags().StringVar(&ageKeyFile, "sops-age-key-file", "", "age identity file for SOPS (otherwise SOPS_AGE_KEY_FILE or the SOPS default is used)")
	return command
}

func loadInstallConfig(ctx context.Context, path, ageKeyFile string) (*installConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read control plane configuration: %w", err)
	}
	if hasSOPSMetadata(data) {
		data, err = decryptControlPlaneWithSOPS(ctx, path, ageKeyFile)
		if err != nil {
			return nil, err
		}
	}
	var cfg installConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode control plane configuration: %w", err)
	}
	if err := validateInstallConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func hasSOPSMetadata(data []byte) bool {
	var document map[string]any
	if yaml.Unmarshal(data, &document) != nil {
		return false
	}
	_, ok := document["sops"]
	return ok
}

func encryptControlPlaneWithSOPS(ctx context.Context, data []byte, recipient string) ([]byte, error) {
	command := exec.CommandContext(ctx, "sops", "encrypt", "--input-type", "yaml", "--output-type", "yaml", "--encrypted-regex", "^secrets$", "--age", recipient, "/dev/stdin")
	command.Stdin = strings.NewReader(string(data))
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("encrypt control plane configuration with SOPS: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

func decryptControlPlaneWithSOPS(ctx context.Context, path, ageKeyFile string) ([]byte, error) {
	command := exec.CommandContext(ctx, "sops", "decrypt", "--output-type", "yaml", path)
	command.Env = sopsEnvironment(ageKeyFile)
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("decrypt control plane configuration with SOPS: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

func sopsEnvironment(ageKeyFile string) []string {
	environment := os.Environ()
	if ageKeyFile != "" {
		environment = append(environment, "SOPS_AGE_KEY_FILE="+ageKeyFile)
	}
	return environment
}

func generateCookieEncryptionKey() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate cookie encryption key: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func validateInstallConfig(cfg *installConfig) error {
	if cfg.APIVersion != installConfigAPIVersion || cfg.Kind != "ControlPlane" {
		return fmt.Errorf("expected apiVersion %q and kind ControlPlane", installConfigAPIVersion)
	}
	if cfg.Metadata.Name == "" || cfg.Spec.Frontend.Hostname == "" || cfg.Spec.Backend.Hostname == "" {
		return fmt.Errorf("metadata.name and spec frontend/backend hostnames are required")
	}
	if strings.TrimSpace(cfg.Spec.Version) == "" {
		return fmt.Errorf("spec.version is required")
	}
	if cfg.Spec.Persistence.Enabled && cfg.Spec.Persistence.Size == "" {
		return fmt.Errorf("spec.persistence.size is required when persistence is enabled")
	}
	_, err := adapterForInstallTarget(cfg.Target.Type)
	return err
}

func adapterForInstallTarget(target string) (installAdapter, error) {
	switch target {
	case "kubernetes":
		return kubernetesInstallAdapter{}, nil
	case "compose":
		return composeInstallAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported target %q", target)
	}
}

type kubernetesInstallAdapter struct{}

func (kubernetesInstallAdapter) Apply(ctx context.Context, out, errOut io.Writer, cfg *installConfig, apply installApplyOptions) error {
	target := cfg.Target.Kubernetes
	if target == nil {
		return fmt.Errorf("target.kubernetes is required for kubernetes")
	}
	o := &helmInstallOptions{
		release: cfg.Metadata.Name, namespace: target.Namespace, chart: target.Chart, version: chartVersion(cfg.Spec.Version, target.Version),
		hostname: cfg.Spec.Frontend.Hostname, apiHostname: cfg.Spec.Backend.Hostname,
		ingressClass: target.IngressClass, storageClass: target.StorageClass,
		persistenceSize:  cfg.Spec.Persistence.Size,
		cookieSecretName: defaultCookieSecretName, cookieSecretKey: defaultCookieSecretKey,
		cookieSecretValue: cfg.Spec.Secrets.CookieEncryptionKey,
		tls:               cfg.Spec.TLS, persistence: cfg.Spec.Persistence.Enabled, createNamespace: true,
		dryRun: apply.DryRun, timeout: 10 * time.Minute,
	}
	if o.namespace == "" {
		o.namespace = "ccplant"
	}
	if o.chart == "" {
		o.chart = defaultCCPlantChart
	}
	return runHelmInstall(ctx, out, errOut, apply.Client, apply.Runner, o)
}

type composeInstallAdapter struct{}

func (composeInstallAdapter) Apply(ctx context.Context, out, errOut io.Writer, cfg *installConfig, apply installApplyOptions) error {
	target := cfg.Target.Compose
	if target == nil {
		return fmt.Errorf("target.compose is required for compose")
	}
	if target.ProjectName == "" {
		target.ProjectName = cfg.Metadata.Name
	}
	if target.Output == "" {
		target.Output = "compose.generated.yaml"
	}
	if target.FrontendPort == 0 {
		target.FrontendPort = 3000
	}
	if target.BackendPort == 0 {
		target.BackendPort = 8080
	}
	manifest, err := generateComposeManifest(cfg, target)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(target.Output)
	if err != nil {
		return fmt.Errorf("resolve compose output: %w", err)
	}
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		return fmt.Errorf("write compose manifest: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Rendered Compose manifest: %s\n", path); err != nil {
		return err
	}
	args := []string{"compose", "--project-name", target.ProjectName, "--file", path, "config", "--quiet"}
	if err := apply.Runner.Run(ctx, io.Discard, errOut, "docker", args...); err != nil {
		return fmt.Errorf("validate Compose manifest: %w", err)
	}
	if apply.DryRun {
		return nil
	}
	args[len(args)-2], args[len(args)-1] = "up", "--detach"
	if err := apply.Runner.Run(ctx, out, errOut, "docker", args...); err != nil {
		return fmt.Errorf("apply Compose installation: %w", err)
	}
	return nil
}

func generateComposeManifest(cfg *installConfig, target *installComposeTarget) ([]byte, error) {
	scheme := "http"
	if cfg.Spec.TLS {
		return nil, fmt.Errorf("compose target does not provide TLS termination; set spec.tls=false or use an external proxy")
	}
	backendURL := fmt.Sprintf("%s://backend:8080", scheme)
	publicURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Spec.Frontend.Hostname, target.FrontendPort)
	imageTag := imageVersion(cfg.Spec.Version)
	services := map[string]any{
		"backend": map[string]any{
			"image":       "ghcr.io/ccplant/ccplant-backend:" + imageTag,
			"environment": map[string]string{"AGENTAPI_AUTH_STATIC_ENABLED": "false", "AGENTAPI_AUTH_GITHUB_ENABLED": "false"},
			"ports":       []string{fmt.Sprintf("%d:8080", target.BackendPort)},
		},
		"frontend": map[string]any{
			"image":       "ghcr.io/ccplant/ccplant-frontend:" + imageTag,
			"environment": map[string]string{"AGENTAPI_PROXY_URL": backendURL, "NEXT_PUBLIC_BASE_URL": publicURL, "COOKIE_ENCRYPTION_SECRET": cfg.Spec.Secrets.CookieEncryptionKey},
			"depends_on":  []string{"backend"}, "ports": []string{fmt.Sprintf("%d:3000", target.FrontendPort)},
		},
	}
	manifest := map[string]any{"services": services}
	if cfg.Spec.Persistence.Enabled {
		services["backend"].(map[string]any)["volumes"] = []string{"ccplant-data:/data"}
		manifest["volumes"] = map[string]any{"ccplant-data": map[string]any{}}
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("generate Compose manifest: %w", err)
	}
	return data, nil
}

func imageVersion(version string) string {
	if version == "latest" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func chartVersion(version, targetOverride string) string {
	if targetOverride != "" {
		return strings.TrimPrefix(targetOverride, "v")
	}
	if version == "latest" {
		return ""
	}
	return strings.TrimPrefix(version, "v")
}

func runHelmInstall(ctx context.Context, out, errOut io.Writer, client kubernetes.Interface, runner installCommandRunner, o *helmInstallOptions) error {
	if err := validateHelmInstallOptions(o); err != nil {
		return err
	}
	kubeVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("kubernetes compatibility check failed: %w", err)
	}
	semanticVersion, err := utilversion.ParseSemantic(kubeVersion.GitVersion)
	if err != nil {
		return fmt.Errorf("kubernetes compatibility check failed: parse server version %q: %w", kubeVersion.GitVersion, err)
	}
	if semanticVersion.LessThan(utilversion.MustParseSemantic("v1.19.0")) {
		return fmt.Errorf("kubernetes compatibility check failed: Kubernetes >=1.19 is required, got %s", kubeVersion.GitVersion)
	}
	if err := checkHelmVersion(ctx, runner); err != nil {
		return err
	}
	metadata, err := loadUmbrellaChartMetadata(ctx, runner, o)
	if err != nil {
		return err
	}
	if err := validateUmbrellaChart(metadata); err != nil {
		return fmt.Errorf("chart compatibility check failed: %w", err)
	}
	if err := resolveClusterDefaults(ctx, client, o); err != nil {
		return err
	}

	upgrade, err := helmReleaseExists(ctx, client, o.namespace, o.release)
	if err != nil {
		return err
	}
	verb := "install"
	if upgrade {
		verb = "upgrade"
	}
	if _, err := fmt.Fprintf(out, "Preflight passed: Helm v%d, chart %s with backend/frontend, operation=%s\n", minimumHelmMajorVersion, metadata.Name, verb); err != nil {
		return err
	}

	generatedValues, err := generateInstallValues(o)
	if err != nil {
		return err
	}
	if o.printValues {
		if _, err := fmt.Fprintf(out, "--- # generated installer values\n%s", generatedValues); err != nil {
			return fmt.Errorf("print generated values: %w", err)
		}
	}
	generatedPath, cleanup, err := materializeInstallValues(generatedValues, o.valuesOut)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := validateGeneratedRelease(ctx, runner, o, generatedPath); err != nil {
		return err
	}

	if !o.dryRun {
		if err := ensureInstallNamespace(ctx, client, o.namespace, o.createNamespace); err != nil {
			return err
		}
		config := cookieSecretConfig{namespace: o.namespace, name: o.cookieSecretName, key: o.cookieSecretKey, configuredValue: o.cookieSecretValue}
		if err := ensureCookieSecret(ctx, client, config); err != nil {
			return err
		}
	}
	args := helmInstallArgs(o, upgrade, generatedPath)
	if err := runner.Run(ctx, out, errOut, "helm", args...); err != nil {
		return fmt.Errorf("helm %s failed: %w", verb, err)
	}
	return writeNativeOutput(out, fmt.Sprintf("ccplant %s completed for %s/%s.\n", verb, o.namespace, o.release))
}
