package services

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
)

// The sidecar runs as UID 0 (root) so that iptables rules can skip its traffic
// via the "--uid-owner 0" match, preventing redirect loops.
func (m *KubernetesSessionManager) buildSandboxContainers(sandbox *entities.SandboxParams) ([]corev1.Container, *corev1.Container, []corev1.EnvVar) {
	var filterEnvVars []corev1.EnvVar
	if sandbox != nil && len(sandbox.AllowedDomains) > 0 {
		filterEnvVars = []corev1.EnvVar{
			{Name: "NETWORK_FILTER_ALLOWED_DOMAINS", Value: strings.Join(sandbox.AllowedDomains, ",")},
		}
	} else if sandbox != nil && len(sandbox.DeniedDomains) > 0 {
		filterEnvVars = []corev1.EnvVar{
			{Name: "NETWORK_FILTER_DENIED_DOMAINS", Value: strings.Join(sandbox.DeniedDomains, ",")},
		}
	}
	if sandbox != nil && sandbox.CountMode {
		filterEnvVars = append(filterEnvVars, corev1.EnvVar{Name: "NETWORK_FILTER_COUNT_MODE", Value: "true"})
	}

	rootUID := int64(0)
	falseVal := false

	nfaImage := m.k8sConfig.NetworkFilterImage
	nfaConfig := buildNFAConfig(sandbox)

	initResources := buildResourceRequirements(m.k8sConfig.NetworkFilterInitCPURequest, m.k8sConfig.NetworkFilterInitCPULimit, m.k8sConfig.NetworkFilterInitMemoryRequest, m.k8sConfig.NetworkFilterInitMemoryLimit)

	generateRulesInitContainer := corev1.Container{
		Name:            "network-filter-generate-iptables",
		Image:           nfaImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Command: []string{"/bin/sh", "-ec", `
printf '%s' "$NFA_CONFIG" > /tmp/nfa-config.yaml
exec nfa setup-iptables --output /etc/iptables/rules.v4 --config /tmp/nfa-config.yaml
`},
		Env: []corev1.EnvVar{{Name: "NFA_CONFIG", Value: nfaConfig}},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    &rootUID,
			RunAsNonRoot: &falseVal,
		},
		Resources: initResources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "sandbox-iptables",
				MountPath: "/etc/iptables",
			},
		},
	}

	restoreRulesInitContainer := corev1.Container{
		Name:            "network-filter-setup",
		Image:           nfaImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Command:         []string{"iptables-restore", "/etc/iptables/rules.v4"},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    &rootUID,
			RunAsNonRoot: &falseVal,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN"},
			},
		},
		Resources: initResources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "sandbox-iptables",
				MountPath: "/etc/iptables",
				ReadOnly:  true,
			},
		},
	}

	proxyAddr := fmt.Sprintf("http://127.0.0.1:%d", nfaProxyPort)

	sidecar := corev1.Container{
		Name:            "network-filter",
		Image:           nfaImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Command:         []string{"/bin/sh", "-ec"},
		Args: []string{fmt.Sprintf(
			"exec nfa proxy --deferred-policy --listen-address 127.0.0.1:3128 --control-socket %s --control-socket-mode 438 2>%s",
			nfaControlSocketPath, nfaLogPath,
		)},
		Env: filterEnvVars,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                &rootUID,
			RunAsNonRoot:             &falseVal,
			AllowPrivilegeEscalation: &falseVal,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN"},
			},
		},
		Resources: buildResourceRequirements(m.k8sConfig.NetworkFilterCPURequest, m.k8sConfig.NetworkFilterCPULimit, m.k8sConfig.NetworkFilterMemoryRequest, m.k8sConfig.NetworkFilterMemoryLimit),
		Ports: []corev1.ContainerPort{
			{Name: "proxy", ContainerPort: int32(nfaProxyPort), Protocol: corev1.ProtocolTCP},
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", networkFilterPreStopScript()}},
			},
		},
	}

	// Inject HTTP_PROXY / HTTPS_PROXY env vars into the main container so that
	// proxy-aware clients (curl, Go's http.Client, pip, npm, etc.) route all
	// HTTP and HTTPS traffic through the sidecar via CONNECT tunnels.
	// This covers non-standard ports and avoids SNI-peeking for HTTPS.
	// Cluster-internal requests must pass through nfa: direct connections would be
	// rejected by the sandbox's default TCP rule. nfa always bypasses *.svc.cluster.local.
	// Anthropic API endpoints bypass the proxy so Claude API calls always succeed.
	noProxy := "127.0.0.1,localhost,anthropic.com,*.anthropic.com"
	proxyEnvVars := []corev1.EnvVar{
		{Name: "HTTP_PROXY", Value: proxyAddr},
		{Name: "HTTPS_PROXY", Value: proxyAddr},
		{Name: "http_proxy", Value: proxyAddr},
		{Name: "https_proxy", Value: proxyAddr},
		{Name: "NO_PROXY", Value: noProxy},
		{Name: "no_proxy", Value: noProxy},
	}

	return []corev1.Container{generateRulesInitContainer, restoreRulesInitContainer}, &sidecar, proxyEnvVars
}

const networkFilterProvisionerDrainSeconds = 25

func networkFilterPreStopScript() string {
	return fmt.Sprintf(`i=0; while [ "$i" -lt %d ] && [ -S %s ]; do sleep 1; i=$((i+1)); done`, networkFilterProvisionerDrainSeconds, provisionerSocketPath)
}

func (m *KubernetesSessionManager) buildMoliSidecar() *corev1.Container {
	port := m.k8sConfig.MoliPort
	args := []string{"serve", "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	if m.k8sConfig.MoliLayout {
		args = append(args, "--layout")
	}
	trueVal := true
	falseVal := false
	uid := int64(999)
	// Kubelet HTTP/TCP probes target the Pod IP, but Moli intentionally only
	// listens on loopback so its unauthenticated CDP endpoint is not exposed to
	// other Pods. Run the probe inside the container to exercise that loopback
	// endpoint without weakening the bind address.
	probeCommand := fmt.Sprintf(`exec 3<>/dev/tcp/127.0.0.1/%d; printf 'GET /json/version HTTP/1.0\r\nHost: localhost\r\n\r\n' >&3; IFS= read -r line <&3; [[ "$line" == *" 200 "* ]]`, port)
	probe := func() corev1.ProbeHandler {
		return corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/bash", "-ec", probeCommand}}}
	}
	return &corev1.Container{
		Name:            "moli",
		Image:           m.k8sConfig.MoliImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Args:            args,
		Ports:           []corev1.ContainerPort{{Name: "moli-cdp", ContainerPort: int32(port), Protocol: corev1.ProtocolTCP}},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: &uid, RunAsGroup: &uid, RunAsNonRoot: &trueVal,
			AllowPrivilegeEscalation: &falseVal, ReadOnlyRootFilesystem: &trueVal,
			Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		Resources: buildResourceRequirements(
			m.k8sConfig.MoliCPURequest, m.k8sConfig.MoliCPULimit,
			m.k8sConfig.MoliMemoryRequest, m.k8sConfig.MoliMemoryLimit,
		),
		VolumeMounts:   []corev1.VolumeMount{{Name: "moli-tmp", MountPath: "/tmp"}},
		StartupProbe:   &corev1.Probe{ProbeHandler: probe(), PeriodSeconds: 1, FailureThreshold: 30},
		ReadinessProbe: &corev1.Probe{ProbeHandler: probe(), PeriodSeconds: 5, FailureThreshold: 3},
		LivenessProbe:  &corev1.Probe{ProbeHandler: probe(), PeriodSeconds: 10, FailureThreshold: 3},
	}
}

func buildNFAConfig(sandbox *entities.SandboxParams) string {
	mode := "denylist"
	domains := []string(nil)
	countMode := false
	if sandbox != nil {
		countMode = sandbox.CountMode
		switch {
		case len(sandbox.AllowedDomains) > 0:
			mode = "allowlist"
			domains = sandbox.AllowedDomains
		case len(sandbox.DeniedDomains) > 0:
			domains = sandbox.DeniedDomains
		default:
			mode = "allowlist"
		}
	}
	if mode == "allowlist" {
		domains = append(domains, sessionsettings.SandboxLocalAddressRanges...)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "filter:\n  mode: %s\n  countMode: %t\n  domains:\n", mode, countMode)
	for _, domain := range domains {
		fmt.Fprintf(&b, "    - %s\n", strconv.Quote(domain))
	}
	b.WriteString("deferredPolicy: true\n")
	return b.String()
}
