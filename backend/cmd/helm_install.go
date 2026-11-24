package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

const (
	defaultCCPlantChart     = "oci://ghcr.io/ccplant/charts/ccplant"
	defaultCookieSecretName = "agentapi-ui-encryption"
	defaultCookieSecretKey  = "cookie-encryption-secret"
	minimumHelmMajorVersion = 3
)

type helmInstallOptions struct {
	release, namespace, chart, version                     string
	hostname, apiHostname, ingressClass                    string
	cookieSecretName, cookieSecretKey                      string
	cookieSecretValue                                      string
	storageClass, persistenceSize, valuesOut               string
	values, sets                                           []string
	tls, persistence, createNamespace, dryRun, printValues bool
	timeout                                                time.Duration
}

const installConfigAPIVersion = "ccplant.io/v1alpha1"

type installConfig struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   installConfigMetadata `json:"metadata"`
	Encryption installEncryption     `json:"encryption,omitempty"`
	Spec       installConfigSpec     `json:"spec"`
	Target     installTarget         `json:"target"`
}

type installConfigMetadata struct {
	Name string `json:"name"`
}

type installConfigSpec struct {
	Version     string             `json:"version"`
	Frontend    installEndpoint    `json:"frontend"`
	Backend     installEndpoint    `json:"backend"`
	TLS         bool               `json:"tls"`
	Persistence installPersistence `json:"persistence"`
	Secrets     installSecrets     `json:"secrets,omitempty"`
}

type installEncryption struct {
	Provider string `json:"provider,omitempty"`
}

type installSecrets struct {
	CookieEncryptionKey string `json:"cookieEncryptionKey,omitempty"`
}

type installEndpoint struct {
	Hostname string `json:"hostname"`
}

type installPersistence struct {
	Enabled bool   `json:"enabled"`
	Size    string `json:"size"`
}

type installTarget struct {
	Type       string                   `json:"type"`
	Kubernetes *installKubernetesTarget `json:"kubernetes,omitempty"`
	Compose    *installComposeTarget    `json:"compose,omitempty"`
}

type installKubernetesTarget struct {
	Namespace    string `json:"namespace"`
	IngressClass string `json:"ingressClass,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Chart        string `json:"chart,omitempty"`
	Version      string `json:"version,omitempty"`
}

type installComposeTarget struct {
	ProjectName  string `json:"projectName,omitempty"`
	Output       string `json:"output,omitempty"`
	FrontendPort int    `json:"frontendPort,omitempty"`
	BackendPort  int    `json:"backendPort,omitempty"`
}

type installAdapter interface {
	Apply(context.Context, io.Writer, io.Writer, *installConfig, installApplyOptions) error
}

type installApplyOptions struct {
	DryRun bool
	Runner installCommandRunner
	Client kubernetes.Interface
}

type installCommandRunner interface {
	Run(context.Context, io.Writer, io.Writer, string, ...string) error
}

type execInstallCommandRunner struct{}

func (execInstallCommandRunner) Run(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout, command.Stderr = stdout, stderr
	return command.Run()
}

type umbrellaChartMetadata struct {
	APIVersion   string `yaml:"apiVersion"`
	Name         string `yaml:"name"`
	Dependencies []struct {
		Name, Alias, Version, Condition string
	} `yaml:"dependencies"`
}

// ControlPlaneCmd manages the desired state of a ccplant control plane.
// Target-specific mechanisms such as Helm are implementation details.
var ControlPlaneCmd = newControlPlaneCommand()

func newControlPlaneCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "control-plane", Short: "Initialize, generate, and apply a ccplant control plane configuration",
		Args: cobra.NoArgs,
	}
	command.AddCommand(newControlPlaneInitCommand(), newControlPlaneGenerateCommand(), newControlPlaneEncryptCommand(), newControlPlaneEditCommand(), newControlPlaneApplyCommand())
	return command
}

func newControlPlaneGenerateCommand() *cobra.Command {
	var output, namespace, release, chart string
	command := &cobra.Command{Use: "generate", Aliases: []string{"export"}, Short: "Generate a control plane configuration from an existing Kubernetes release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		restConfig, err := ctrl.GetConfig()
		if err != nil {
			return fmt.Errorf("get Kubernetes config: %w", err)
		}
		client, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return fmt.Errorf("create Kubernetes client: %w", err)
		}
		cfg, err := generateKubernetesInstallConfig(cmd.Context(), client, execInstallCommandRunner{}, namespace, release, chart)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		if output == "-" {
			return writeNativeOutput(cmd.OutOrStdout(), string(data))
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("configuration %q already exists", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output configuration: %w", err)
		}
		if err := writeNewFileAtomically(output, data, 0o600); err != nil {
			return fmt.Errorf("write control plane configuration: %w", err)
		}
		return writeNativeOutput(cmd.OutOrStdout(), fmt.Sprintf("Generated %s from Kubernetes release %s/%s.\n", output, namespace, release))
	}}
	command.Flags().StringVarP(&output, "output", "o", "control-plane.yaml", "configuration output path, or - for stdout")
	command.Flags().StringVarP(&namespace, "namespace", "n", "ccplant", "Kubernetes namespace containing the Helm release")
	command.Flags().StringVar(&release, "release", "ccplant", "Helm release name")
	command.Flags().StringVar(&chart, "chart", defaultCCPlantChart, "chart reference to record in the configuration")
	return command
}

func generateKubernetesInstallConfig(ctx context.Context, client kubernetes.Interface, runner installCommandRunner, namespace, release, chart string) (*installConfig, error) {
	for label, value := range map[string]string{"namespace": namespace, "release": release, "chart": chart} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("--%s must not be empty", label)
		}
	}
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", "get", "values", release, "--namespace", namespace, "--all", "--output", "yaml"); err != nil {
		return nil, fmt.Errorf("read Helm values for %s/%s: %w", namespace, release, err)
	}
	var values map[string]any
	if err := yaml.Unmarshal([]byte(output.String()), &values); err != nil {
		return nil, fmt.Errorf("decode Helm values for %s/%s: %w", namespace, release, err)
	}
	stringValue := func(path ...string) string {
		var current any = values
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				return ""
			}
			current = object[key]
		}
		value, _ := current.(string)
		return value
	}
	boolValue := func(path ...string) bool {
		var current any = values
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				return false
			}
			current = object[key]
		}
		value, _ := current.(bool)
		return value
	}
	hostname, apiHostname := stringValue("global", "hostname"), stringValue("global", "apiHostname")
	if hostname == "" || apiHostname == "" {
		return nil, fmt.Errorf("helm release %s/%s does not contain global.hostname and global.apiHostname", namespace, release)
	}
	secretName, secretKey := stringValue("frontend", "cookieEncryptionSecret", "secretName"), stringValue("frontend", "cookieEncryptionSecret", "secretKey")
	if secretName == "" {
		secretName = defaultCookieSecretName
	}
	if secretKey == "" {
		secretKey = defaultCookieSecretKey
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read cookie Secret %s/%s: %w", namespace, secretName, err)
	}
	cookieKey := string(secret.Data[secretKey])
	if cookieKey == "" {
		return nil, fmt.Errorf("cookie Secret %s/%s does not contain non-empty key %q", namespace, secretName, secretKey)
	}
	version := strings.TrimPrefix(stringValue("backend", "image", "tag"), "v")
	if version == "" {
		version = strings.TrimPrefix(stringValue("frontend", "image", "tag"), "v")
	}
	if version == "" {
		version = "latest"
	}
	persistence := boolValue("backend", "kubernetesSession", "pvc", "enabled")
	size := stringValue("backend", "kubernetesSession", "pvc", "storageSize")
	if size == "" {
		size = "10Gi"
	}
	return &installConfig{
		APIVersion: installConfigAPIVersion, Kind: "ControlPlane", Metadata: installConfigMetadata{Name: release},
		Spec:   installConfigSpec{Version: version, Frontend: installEndpoint{Hostname: hostname}, Backend: installEndpoint{Hostname: apiHostname}, TLS: boolValue("global", "ingress", "tls", "enabled"), Persistence: installPersistence{Enabled: persistence, Size: size}, Secrets: installSecrets{CookieEncryptionKey: cookieKey}},
		Target: installTarget{Type: "kubernetes", Kubernetes: &installKubernetesTarget{Namespace: namespace, IngressClass: stringValue("global", "ingress", "className"), StorageClass: stringValue("backend", "kubernetesSession", "pvc", "storageClass"), Chart: chart}},
	}, nil
}

func newControlPlaneInitCommand() *cobra.Command {
	var output, target, version, encryption, ageRecipient string
	command := &cobra.Command{Use: "init", Short: "Write an editable control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := defaultInstallConfig(target)
		if err != nil {
			return err
		}
		cfg.Spec.Version = version
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		if encryption == "sops" {
			if strings.TrimSpace(ageRecipient) == "" {
				return fmt.Errorf("--age-recipient is required with --encryption=sops")
			}
			cfg.Encryption.Provider = "sops"
			if cfg.Spec.Secrets.CookieEncryptionKey == "" {
				cfg.Spec.Secrets.CookieEncryptionKey, err = generateCookieEncryptionKey()
				if err != nil {
					return err
				}
			}
			data, err = yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("encode control plane configuration: %w", err)
			}
			data, err = encryptControlPlaneWithSOPS(cmd.Context(), data, ageRecipient)
			if err != nil {
				return err
			}
		} else if encryption != "none" {
			return fmt.Errorf("unsupported encryption %q (supported: none, sops)", encryption)
		}
		if output == "-" {
			return writeNativeOutput(cmd.OutOrStdout(), string(data))
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("configuration %q already exists", output)
		}
		if err := os.WriteFile(output, data, 0o600); err != nil {
			return fmt.Errorf("write control plane configuration: %w", err)
		}
		return writeNativeOutput(cmd.OutOrStdout(), fmt.Sprintf("Wrote %s. Edit it, then run: agentapi-proxy control-plane apply --file %s\n", output, output))
	}}
	command.Flags().StringVarP(&output, "output", "o", "control-plane.yaml", "configuration output path, or - for stdout")
	command.Flags().StringVar(&target, "target", "kubernetes", "deployment target: kubernetes or compose")
	command.Flags().StringVar(&version, "version", "latest", "ccplant release version shared by all components")
	command.Flags().StringVar(&encryption, "encryption", "none", "configuration encryption: none or sops")
	command.Flags().StringVar(&ageRecipient, "age-recipient", "", "age recipient used by SOPS encryption")
	return command
}

func newControlPlaneEditCommand() *cobra.Command {
	var file, ageKeyFile string
	command := &cobra.Command{Use: "edit", Short: "Edit a SOPS-encrypted control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("read control plane configuration: %w", err)
		}
		process := exec.CommandContext(cmd.Context(), "sops", file)
		process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		process.Env = sopsEnvironment(ageKeyFile)
		if err := process.Run(); err != nil {
			return fmt.Errorf("edit SOPS configuration: %w", err)
		}
		return nil
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "control plane configuration")
	command.Flags().StringVar(&ageKeyFile, "sops-age-key-file", "", "age identity file for SOPS (otherwise SOPS_AGE_KEY_FILE or the SOPS default is used)")
	return command
}

func newControlPlaneEncryptCommand() *cobra.Command {
	var file, output, ageRecipient string
	command := &cobra.Command{Use: "encrypt", Short: "Encrypt an existing control plane configuration with SOPS", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(ageRecipient) == "" {
			return fmt.Errorf("--age-recipient is required")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read control plane configuration: %w", err)
		}
		if hasSOPSMetadata(data) {
			return fmt.Errorf("configuration %q is already SOPS-encrypted", file)
		}
		var cfg installConfig
		if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
			return fmt.Errorf("decode control plane configuration: %w", err)
		}
		if err := validateInstallConfig(&cfg); err != nil {
			return err
		}
		cfg.Encryption.Provider = "sops"
		if cfg.Spec.Secrets.CookieEncryptionKey == "" {
			cfg.Spec.Secrets.CookieEncryptionKey, err = generateCookieEncryptionKey()
			if err != nil {
				return err
			}
		}
		data, err = yaml.Marshal(&cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		encrypted, err := encryptControlPlaneWithSOPS(cmd.Context(), data, ageRecipient)
		if err != nil {
			return err
		}
		if output == "" {
			output = encryptedControlPlanePath(file)
		}
		if samePath(file, output) {
			return fmt.Errorf("encrypted output must differ from input %q", file)
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("encrypted output %q already exists", output)
		}
		if err := writeNewFileAtomically(output, encrypted, 0o600); err != nil {
			return fmt.Errorf("write encrypted control plane configuration: %w", err)
		}
		return writeNativeOutput(cmd.OutOrStdout(), fmt.Sprintf("Encrypted %s with SOPS.\n", output))
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "plain control plane configuration")
	command.Flags().StringVarP(&output, "output", "o", "", "encrypted output path (default: <input>.sops.yaml)")
	command.Flags().StringVar(&ageRecipient, "age-recipient", "", "age recipient used by SOPS encryption")
	return command
}
