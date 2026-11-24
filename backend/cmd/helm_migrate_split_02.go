package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func inspectVerifyRuntime(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, verify *migrationVerify) error {
	pods, err := client.CoreV1().Pods(o.namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=agentapi-session"})
	if err != nil {
		return err
	}
	verify.Inventory.Sessions = len(pods.Items)
	for _, pod := range pods.Items {
		if podUsesLegacyBackend(pod, o.backendRelease) {
			verify.Inventory.LegacySessions++
		}
	}
	secrets, err := client.CoreV1().Secrets(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		if isMigrationRuntimeSecret(secret) {
			verify.Inventory.RuntimeSecrets++
		}
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, "agentapi-session-") {
			verify.Inventory.SessionPVCs++
		}
	}
	if verify.Inventory.LegacySessions > 0 {
		addVerifyFinding(verify, "warn", "legacy sessions", fmt.Sprintf("%d session(s) still require Service/%s", verify.Inventory.LegacySessions, o.backendRelease))
	} else {
		addVerifyFinding(verify, "pass", "session callbacks", "all discovered session Pods use a release-independent callback")
	}
	return nil
}

func podUsesLegacyBackend(pod corev1.Pod, backendRelease string) bool {
	legacyURL := "http://" + backendRelease + "."
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if strings.Contains(env.Value, legacyURL) {
				return true
			}
		}
	}
	return false
}

func selectorContains(actual, want map[string]string) bool {
	for key, value := range want {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func addVerifyFinding(verify *migrationVerify, level, subject, message string) {
	verify.Findings = append(verify.Findings, migrationFinding{Level: level, Subject: subject, Message: message})
}

func buildMigrationShadowValues(backend, frontend map[string]any) map[string]any {
	b := cloneMigrationMap(backend)
	f := cloneMigrationMap(frontend)
	delete(b, "fullnameOverride")
	delete(f, "fullnameOverride")
	setMigrationValue(b, false, "ingress", "enabled")
	setMigrationValue(b, false, "controlPlaneService", "create")
	setMigrationValue(b, "agentapi-proxy-session", "kubernetesSession", "serviceAccountName")
	setMigrationValue(b, false, "kubernetesSession", "rbac", "create")
	setMigrationValue(f, false, "ingress", "enabled")
	return map[string]any{"backend": b, "frontend": f}
}

func cloneMigrationMap(in map[string]any) map[string]any {
	return cloneMigrationValue(in).(map[string]any)
}

func cloneMigrationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, item := range typed {
			clone[key] = cloneMigrationValue(item)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = cloneMigrationValue(item)
		}
		return clone
	default:
		return value
	}
}

func setMigrationValue(root map[string]any, value any, path ...string) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func inspectControlService(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) {
	service, err := client.CoreV1().Services(o.namespace).Get(ctx, "control", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		addFinding(plan, "block", "control Service", "missing; upgrade the old backend chart before migration")
		return
	}
	if err != nil {
		addFinding(plan, "block", "control Service", err.Error())
		return
	}
	if service.Annotations["helm.sh/resource-policy"] != "keep" {
		addFinding(plan, "block", "control Service", "helm.sh/resource-policy=keep is missing")
	} else {
		addFinding(plan, "pass", "control Service", "retained on Helm uninstall")
	}
	if len(service.Spec.Selector) == 0 {
		addFinding(plan, "block", "control Service", "selector is empty")
	} else {
		addFinding(plan, "pass", "control selector", formatStringMap(service.Spec.Selector))
	}
}

func inspectSharedRBAC(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) {
	if _, err := client.CoreV1().ServiceAccounts(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "ServiceAccount/agentapi-proxy-session is missing")
		return
	}
	if _, err := client.RbacV1().Roles(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "Role/agentapi-proxy-session is missing")
		return
	}
	if _, err := client.RbacV1().RoleBindings(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "RoleBinding/agentapi-proxy-session is missing")
		return
	}
	addFinding(plan, "pass", "shared session RBAC", "fixed-name ServiceAccount, Role, and RoleBinding exist; shadow values reuse them")
	addFinding(plan, "warn", "backend uninstall", "the old release owns shared session RBAC; transfer or externalize it before uninstalling the backend")
}

type secretReferenceInspection struct {
	client    kubernetes.Interface
	namespace string
	values    map[string]any
	prefix    string
	plan      *migrationPlan
}

func inspectSecretReferences(ctx context.Context, inspection secretReferenceInspection) error {
	refs := discoverSecretReferences(inspection.values)
	if len(refs) == 0 {
		addFinding(inspection.plan, "pass", inspection.prefix+" Secret references", "none configured")
		return nil
	}
	failed := 0
	for _, ref := range refs {
		secret, err := inspection.client.CoreV1().Secrets(inspection.namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			failed++
			continue
		}
		for _, key := range ref.Keys {
			if len(secret.Data[key]) == 0 {
				failed++
			}
		}
	}
	if failed > 0 {
		addFinding(inspection.plan, "block", inspection.prefix+" Secret references", fmt.Sprintf("%d referenced Secret or key checks failed; run agentapi-proxy doctor for details", failed))
	} else {
		addFinding(inspection.plan, "pass", inspection.prefix+" Secret references", fmt.Sprintf("%d references resolve without exposing values", len(refs)))
	}
	return nil
}

func inspectRuntimeResources(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) error {
	pods, err := client.CoreV1().Pods(o.namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=agentapi-session"})
	if err != nil {
		return err
	}
	plan.Inventory.Sessions = len(pods.Items)
	for _, pod := range pods.Items {
		if podUsesLegacyBackend(pod, o.backendRelease) {
			plan.Inventory.LegacySessions++
		}
	}
	if plan.Inventory.LegacySessions > 0 {
		addFinding(plan, "warn", "legacy sessions", fmt.Sprintf("%d of %d session Pods reference %s directly; drain them or retain a compatibility Service", plan.Inventory.LegacySessions, plan.Inventory.Sessions, o.backendRelease))
	} else {
		addFinding(plan, "pass", "session callbacks", fmt.Sprintf("no legacy callback found across %d session Pods", plan.Inventory.Sessions))
	}
	if err := inspectRuntimeSecrets(ctx, client, o.namespace, plan); err != nil {
		return err
	}
	if err := inspectRuntimePVCs(ctx, client, o.namespace, plan); err != nil {
		return err
	}
	addFinding(plan, "pass", "runtime inventory", fmt.Sprintf("%d runtime Secrets and %d session PVCs recorded", plan.Inventory.RuntimeSecrets, plan.Inventory.SessionPVCs))
	return nil
}

func inspectRuntimeSecrets(ctx context.Context, client kubernetes.Interface, namespace string, plan *migrationPlan) error {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		if isMigrationRuntimeSecret(secret) {
			plan.Inventory.RuntimeSecrets++
			appendHelmOwnership(secret.ObjectMeta, "Secret", &plan.Findings)
		}
	}
	return nil
}

func inspectRuntimePVCs(ctx context.Context, client kubernetes.Interface, namespace string, plan *migrationPlan) error {
	pvcs, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, "agentapi-session-") {
			plan.Inventory.SessionPVCs++
			appendHelmOwnership(pvc.ObjectMeta, "PersistentVolumeClaim", &plan.Findings)
		}
	}
	return nil
}

func appendHelmOwnership(meta metav1.ObjectMeta, kind string, findings *[]migrationFinding) {
	if release := meta.Annotations["meta.helm.sh/release-name"]; release != "" {
		*findings = append(*findings, migrationFinding{Level: "block", Subject: kind + "/" + meta.Name, Message: "runtime resource is owned by Helm release " + release})
	}
}

func isMigrationRuntimeSecret(s corev1.Secret) bool {
	return strings.HasPrefix(s.Name, "agentapi-session-") || strings.HasPrefix(s.Name, "agentapi-settings-") || strings.HasPrefix(s.Name, "agentapi-agent-files-") || strings.HasPrefix(s.Name, "agentapi-user-files-") || s.Labels["agentapi.proxy/settings"] == "true"
}

func referencedSecretsFingerprint(ctx context.Context, client kubernetes.Interface, namespace string, refs []secretReference) string {
	unique := map[string]struct{}{}
	for _, ref := range refs {
		unique[ref.Name] = struct{}{}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		if !writeFingerprint(h, []byte(name)) {
			return ""
		}
		keys := make([]string, 0, len(secret.Data))
		for key := range secret.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !writeFingerprint(h, []byte{0}, []byte(key), []byte{0}, secret.Data[key]) {
				return ""
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFingerprint(destination hash.Hash, parts ...[]byte) bool {
	for _, part := range parts {
		if _, err := destination.Write(part); err != nil {
			return false
		}
	}
	return true
}

func migrationCommands(o *helmMigratePlanOptions, legacy int) []string {
	commands := []string{fmt.Sprintf("helm upgrade --install %s %s --namespace %s --version %s --values %s --wait", o.targetRelease, o.chart, o.namespace, o.version, o.valuesOut), fmt.Sprintf("agentapi-proxy helm migrate verify --namespace %s --backend-release %s --target-release %s", o.namespace, o.backendRelease, o.targetRelease), fmt.Sprintf("kubectl -n %s patch service control --type merge -p '{\"spec\":{\"selector\":{\"app.kubernetes.io/name\":\"backend\",\"app.kubernetes.io/instance\":\"%s\",\"app.kubernetes.io/component\":\"proxy\"}}}'", o.namespace, o.targetRelease)}
	if legacy > 0 {
		commands = append(commands, fmt.Sprintf("# Keep Service/%s until %d legacy session(s) are drained", o.backendRelease, legacy))
	}
	commands = append(commands, fmt.Sprintf("# Verify traffic, transfer shared session RBAC, then uninstall %s and %s", o.frontendRelease, o.backendRelease))
	return commands
}

func addFinding(plan *migrationPlan, level, subject, message string) {
	plan.Findings = append(plan.Findings, migrationFinding{Level: level, Subject: subject, Message: message})
}

func hasBlockingFinding(findings []migrationFinding) bool {
	for _, finding := range findings {
		if finding.Level == "block" {
			return true
		}
	}
	return false
}

func formatStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ",")
}
