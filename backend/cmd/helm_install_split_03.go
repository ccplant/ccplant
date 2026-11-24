package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func validateGeneratedRelease(ctx context.Context, runner installCommandRunner, o *helmInstallOptions, generatedValuesPath string) error {
	args := []string{"template", o.release, o.chart, "--namespace", o.namespace, "--values", generatedValuesPath}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	for _, file := range o.values {
		args = append(args, "--values", file)
	}
	for _, value := range o.sets {
		args = append(args, "--set", value)
	}
	if err := runner.Run(ctx, io.Discard, io.Discard, "helm", args...); err != nil {
		return fmt.Errorf("generated values compatibility check failed: %w", err)
	}
	return nil
}

func validateHelmInstallOptions(o *helmInstallOptions) error {
	for label, value := range map[string]string{"release": o.release, "namespace": o.namespace, "chart": o.chart, "hostname": o.hostname, "api-hostname": o.apiHostname, "cookie-secret-name": o.cookieSecretName, "cookie-secret-key": o.cookieSecretKey} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s must not be empty", label)
		}
	}
	if o.timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	return nil
}

func resolveClusterDefaults(ctx context.Context, client kubernetes.Interface, o *helmInstallOptions) error {
	if o.ingressClass == "" {
		className, err := resolveIngressClass(ctx, client)
		if err != nil {
			return err
		}
		o.ingressClass = className
	}
	if o.persistence && o.storageClass == "" {
		className, err := resolveStorageClass(ctx, client)
		if err != nil {
			return err
		}
		o.storageClass = className
	}
	return nil
}

func resolveIngressClass(ctx context.Context, client kubernetes.Interface) (string, error) {
	classes, err := client.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("detect IngressClass: %w", err)
	}
	for _, class := range classes.Items {
		if class.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true" {
			return class.Name, nil
		}
	}
	if len(classes.Items) == 1 {
		return classes.Items[0].Name, nil
	}
	return "", fmt.Errorf("no default IngressClass was detected; specify --ingress-class")
}

func resolveStorageClass(ctx context.Context, client kubernetes.Interface) (string, error) {
	classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("detect StorageClass: %w", err)
	}
	for _, class := range classes.Items {
		annotations := class.Annotations
		if annotations["storageclass.kubernetes.io/is-default-class"] == "true" || annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			return class.Name, nil
		}
	}
	if len(classes.Items) == 1 {
		return classes.Items[0].Name, nil
	}
	return "", fmt.Errorf("persistence is enabled but no default StorageClass was detected; specify --storage-class or --persistence=false")
}

func generateInstallValues(o *helmInstallOptions) ([]byte, error) {
	scheme := "http"
	if o.tls {
		scheme = "https"
	}
	values := map[string]any{
		"global": map[string]any{
			"hostname": o.hostname, "apiHostname": o.apiHostname,
			"ingress": map[string]any{"className": o.ingressClass, "tls": map[string]any{"enabled": o.tls}},
		},
		"backend": map[string]any{
			"enabled":           true,
			"ingress":           map[string]any{"enabled": true, "className": o.ingressClass},
			"kubernetesSession": map[string]any{"enabled": true, "pvc": map[string]any{"enabled": o.persistence, "storageClass": o.storageClass, "storageSize": o.persistenceSize}},
		},
		"frontend": map[string]any{
			"enabled":                true,
			"ingress":                map[string]any{"enabled": true, "className": o.ingressClass},
			"cookieEncryptionSecret": map[string]any{"enabled": true, "secretName": o.cookieSecretName, "secretKey": o.cookieSecretKey},
			"config":                 map[string]any{"publicUrl": scheme + "://" + o.hostname},
		},
	}
	if o.version != "" {
		tag := imageVersion(o.version)
		values["backend"].(map[string]any)["image"] = map[string]any{"tag": tag}
		values["frontend"].(map[string]any)["image"] = map[string]any{"tag": tag}
	}
	if o.tls {
		values["backend"].(map[string]any)["ingress"].(map[string]any)["tls"] = []any{map[string]any{"secretName": o.release + "-backend-tls", "hosts": []string{o.apiHostname}}}
		values["frontend"].(map[string]any)["ingress"].(map[string]any)["tls"] = []any{map[string]any{"secretName": o.release + "-frontend-tls", "hosts": []string{o.hostname}}}
	}
	data, err := yaml.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("generate installer values: %w", err)
	}
	return data, nil
}

func materializeInstallValues(values []byte, outputPath string) (string, func(), error) {
	if outputPath != "" {
		path, err := filepath.Abs(outputPath)
		if err != nil {
			return "", func() {}, fmt.Errorf("resolve --values-out: %w", err)
		}
		if err := os.WriteFile(path, values, 0o600); err != nil {
			return "", func() {}, fmt.Errorf("write generated values: %w", err)
		}
		return path, func() {}, nil
	}
	file, err := os.CreateTemp("", "ccplant-install-values-*.yaml")
	if err != nil {
		return "", func() {}, fmt.Errorf("create generated values file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove generated installer values %s: %v", path, err)
		}
	}
	if err := file.Chmod(0o600); err != nil {
		cleanupErr := closeAndRemoveInstallValues(file, path)
		return "", func() {}, errors.Join(fmt.Errorf("protect generated values file: %w", err), cleanupErr)
	}
	written, err := file.Write(values)
	if err != nil || written != len(values) {
		if err == nil {
			err = io.ErrShortWrite
		}
		cleanupErr := closeAndRemoveInstallValues(file, path)
		return "", func() {}, errors.Join(fmt.Errorf("write generated values file: %w", err), cleanupErr)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close generated values file: %w", err)
	}
	return path, cleanup, nil
}

func closeAndRemoveInstallValues(file *os.File, path string) error {
	closeErr := file.Close()
	removeErr := os.Remove(path)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func checkHelmVersion(ctx context.Context, runner installCommandRunner) error {
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", "version", "--template", "{{.Version}}"); err != nil {
		return fmt.Errorf("helm compatibility check failed: %w", err)
	}
	version := strings.TrimSpace(strings.TrimPrefix(output.String(), "v"))
	var major int
	if _, err := fmt.Sscanf(version, "%d.", &major); err != nil || major < minimumHelmMajorVersion {
		return fmt.Errorf("helm compatibility check failed: Helm >=%d is required, got %q", minimumHelmMajorVersion, output.String())
	}
	return nil
}

func loadUmbrellaChartMetadata(ctx context.Context, runner installCommandRunner, o *helmInstallOptions) (*umbrellaChartMetadata, error) {
	args := []string{"show", "chart", o.chart}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", args...); err != nil {
		return nil, fmt.Errorf("load chart metadata: %w", err)
	}
	var metadata umbrellaChartMetadata
	if err := yaml.Unmarshal([]byte(output.String()), &metadata); err != nil {
		return nil, fmt.Errorf("decode chart metadata: %w", err)
	}
	return &metadata, nil
}

func validateUmbrellaChart(metadata *umbrellaChartMetadata) error {
	if metadata.APIVersion != "v2" || metadata.Name != "ccplant" {
		return fmt.Errorf("expected Helm v2 application chart named ccplant, got apiVersion=%q name=%q", metadata.APIVersion, metadata.Name)
	}
	found := map[string]bool{}
	for _, dependency := range metadata.Dependencies {
		alias := dependency.Alias
		if alias == "" {
			alias = dependency.Name
		}
		if alias == "backend" || alias == "frontend" {
			if dependency.Version == "" || dependency.Condition != alias+".enabled" {
				return fmt.Errorf("%s dependency has incompatible version or condition", alias)
			}
			found[alias] = true
		}
	}
	if !found["backend"] || !found["frontend"] {
		return fmt.Errorf("chart must contain enabled backend and frontend dependencies")
	}
	return nil
}

func helmReleaseExists(ctx context.Context, client kubernetes.Interface, namespace, release string) (bool, error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("owner=helm,name=%s", release)})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("detect existing Helm release: %w", err)
	}
	return len(secrets.Items) > 0, nil
}

type cookieSecretConfig struct {
	namespace, name, key, configuredValue string
}

func ensureCookieSecret(ctx context.Context, client kubernetes.Interface, config cookieSecretConfig) error {
	secret, err := client.CoreV1().Secrets(config.namespace).Get(ctx, config.name, metav1.GetOptions{})
	if err == nil {
		if len(secret.Data[config.key]) == 0 {
			return fmt.Errorf("existing cookie Secret %s/%s does not contain non-empty key %q", config.namespace, config.name, config.key)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read cookie Secret %s/%s: %w", config.namespace, config.name, err)
	}
	value := config.configuredValue
	if value == "" {
		value, err = generateCookieEncryptionKey()
		if err != nil {
			return err
		}
	}
	created, err := client.CoreV1().Secrets(config.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: config.name, Namespace: config.namespace}, Type: corev1.SecretTypeOpaque, StringData: map[string]string{config.key: value}}, metav1.CreateOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("namespace %q does not exist; create it first or let Helm create it before rerunning", config.namespace)
	}
	if err != nil {
		return fmt.Errorf("create cookie Secret %s/%s: %w", config.namespace, config.name, err)
	}
	if created == nil {
		return fmt.Errorf("create cookie Secret %s/%s returned no object", config.namespace, config.name)
	}
	return nil
}

func ensureInstallNamespace(ctx context.Context, client kubernetes.Interface, namespace string, create bool) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read namespace %q: %w", namespace, err)
	}
	if !create {
		return fmt.Errorf("namespace %q does not exist (use --create-namespace)", namespace)
	}
	if _, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %q: %w", namespace, err)
	}
	return nil
}

func helmInstallArgs(o *helmInstallOptions, upgrade bool, generatedValuesPath string) []string {
	verb := "install"
	if upgrade {
		verb = "upgrade"
	}
	args := []string{verb, o.release, o.chart, "--namespace", o.namespace, "--atomic", "--wait", "--timeout", o.timeout.String()}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	if !upgrade && o.createNamespace {
		args = append(args, "--create-namespace")
	}
	args = append(args, "--values", generatedValuesPath)
	for _, file := range o.values {
		args = append(args, "--values", file)
	}
	for _, value := range o.sets {
		args = append(args, "--set", value)
	}
	if o.dryRun {
		args = append(args, "--dry-run")
	}
	return args
}
