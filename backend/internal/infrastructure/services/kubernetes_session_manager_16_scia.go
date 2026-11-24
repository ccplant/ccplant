package services

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	sciaCAPath       = "/etc/scia/mitm/ca.pem"
	sciaCABundlePath = "/tmp/scia-ca-bundle.pem"
	sciaNoProxyBase  = "127.0.0.1,localhost,.svc.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,anthropic.com,*.anthropic.com"
)

func (m *KubernetesSessionManager) sciaSessionSidecarEnabled(req *entities.RunServerRequest) bool {
	return m.config != nil && m.config.Scia.Enabled && m.config.Scia.SessionSidecarEnabled
}

func (m *KubernetesSessionManager) buildSciaSidecarContainers(req *entities.RunServerRequest, sandboxEnabled bool, userToken string) (*corev1.Container, *corev1.Container, []corev1.EnvVar) {
	scia := m.config.Scia
	port := scia.SessionSidecarPort
	if port == 0 {
		port = 18081
	}
	userNamespace := scia.UserNamespace
	if userNamespace == "" {
		userNamespace = sanitizeSciaDynamicUserID(req.UserID)
	}
	if userNamespace == "" {
		userNamespace = "default"
	}
	credentialID := scia.Credential
	if credentialID == "" {
		credentialID = userNamespace + ".google"
	}
	hosts := scia.GoogleHosts
	if len(hosts) == 0 {
		hosts = []string{"www.googleapis.com"}
	}
	paths := scia.GooglePaths
	if len(paths) == 0 {
		paths = []string{"/calendar/v3/*"}
	}
	todoistCredentialID := scia.TodoistCredential
	if todoistCredentialID == "" {
		todoistCredentialID = userNamespace + ".todoist"
	}
	todoistHosts := scia.TodoistHosts
	if len(todoistHosts) == 0 {
		todoistHosts = []string{"api.todoist.com"}
	}
	todoistPaths := scia.TodoistPaths
	if len(todoistPaths) == 0 {
		todoistPaths = []string{"/api/v1/*"}
	}

	configYAML := buildSciaSidecarConfigYAML(m.namespace, userNamespace, credentialID, todoistCredentialID, userToken, port, hosts, paths, todoistHosts, todoistPaths, sandboxEnabled)
	configScript := fmt.Sprintf("cat > /etc/scia-config/config.yaml <<'EOF'\n%sEOF\n", configYAML)

	initContainer := corev1.Container{
		Name:            "scia-config",
		Image:           defaultIfEmpty(scia.SessionSidecarConfigImage, "busybox:1.36"),
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Command:         []string{"sh", "-c", configScript},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "scia-config", MountPath: "/etc/scia-config"},
		},
	}

	sidecar := corev1.Container{
		Name:            "scia-proxy",
		Image:           defaultIfEmpty(scia.SessionSidecarImage, "ghcr.io/takutakahashi/scia:0.17.0"),
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Args:            []string{"-config", "/etc/scia-config/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "scia-proxy", ContainerPort: int32(port), Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "scia-config", MountPath: "/etc/scia-config", ReadOnly: true},
			{Name: "scia-mitm-ca", MountPath: "/etc/scia/mitm"},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(port),
				},
			},
			InitialDelaySeconds: 2,
			PeriodSeconds:       2,
		},
	}

	proxyAddr := fmt.Sprintf("http://127.0.0.1:%d", port)
	noProxy := mergeNoProxy(sciaNoProxyBase, scia.NoProxy)
	envVars := []corev1.EnvVar{
		{Name: "HTTP_PROXY", Value: proxyAddr},
		{Name: "HTTPS_PROXY", Value: proxyAddr},
		{Name: "http_proxy", Value: proxyAddr},
		{Name: "https_proxy", Value: proxyAddr},
		{Name: "NO_PROXY", Value: noProxy},
		{Name: "no_proxy", Value: noProxy},
		{Name: "SSL_CERT_FILE", Value: sciaCABundlePath},
		{Name: "REQUESTS_CA_BUNDLE", Value: sciaCABundlePath},
		{Name: "CURL_CA_BUNDLE", Value: sciaCABundlePath},
		{Name: "GIT_SSL_CAINFO", Value: sciaCABundlePath},
		{Name: "NODE_EXTRA_CA_CERTS", Value: sciaCAPath},
		{Name: "AGENTAPI_SCIA_PROXY_URL", Value: proxyAddr},
		{Name: "AGENTAPI_SCIA_GOOGLE_CREDENTIAL", Value: credentialID},
		{Name: "AGENTAPI_SCIA_TODOIST_CREDENTIAL", Value: todoistCredentialID},
		{Name: "AGENTAPI_SCIA_USER_NAMESPACE", Value: userNamespace},
	}

	return &initContainer, &sidecar, envVars
}

func buildSciaSidecarConfigYAML(namespace, userNamespace, credentialID, todoistCredentialID, userToken string, port int, hosts, paths, todoistHosts, todoistPaths []string, useNFA bool) string {
	var b strings.Builder
	b.WriteString("server:\n")
	b.WriteString("  mode: proxy\n")
	fmt.Fprintf(&b, "  listen: %q\n", fmt.Sprintf("0.0.0.0:%d", port))
	if useNFA {
		b.WriteString("  backendProxy:\n")
		fmt.Fprintf(&b, "    url: %q\n", fmt.Sprintf("http://127.0.0.1:%d", nfaProxyPort))
	}
	b.WriteString("  integrations:\n")
	b.WriteString("    google:\n")
	b.WriteString("      hosts:\n")
	for _, host := range hosts {
		fmt.Fprintf(&b, "        - %q\n", host)
	}
	b.WriteString("    todoist:\n")
	b.WriteString("      hosts:\n")
	for _, host := range todoistHosts {
		fmt.Fprintf(&b, "        - %q\n", host)
	}
	b.WriteString("  mitm:\n")
	fmt.Fprintf(&b, "    caCertPath: %q\n", sciaCAPath)
	b.WriteString("    caKeyPath: \"/etc/scia/mitm/ca.key\"\n")
	b.WriteString("  secrets:\n")
	b.WriteString("    mode: kubernetes\n")
	b.WriteString("    kubernetes:\n")
	fmt.Fprintf(&b, "      namespace: %q\n", namespace)
	b.WriteString("      dynamicUsers: true\n")
	b.WriteString("      dynamicUserSecretNamePrefix: \"scia-oauth-\"\n")
	b.WriteString("credentials:\n")
	fmt.Fprintf(&b, "  - id: %q\n", credentialID)
	b.WriteString("    type: google-oauth-refresh-token\n")
	b.WriteString("    params:\n")
	fmt.Fprintf(&b, "      user: %q\n", userNamespace)
	fmt.Fprintf(&b, "      token_broker_url: %q\n", sciaTokenBrokerURL(namespace, "google", userToken))
	fmt.Fprintf(&b, "  - id: %q\n", todoistCredentialID)
	b.WriteString("    type: todoist-oauth-refresh-token\n")
	b.WriteString("    params:\n")
	fmt.Fprintf(&b, "      user: %q\n", userNamespace)
	fmt.Fprintf(&b, "      token_broker_url: %q\n", sciaTokenBrokerURL(namespace, "todoist", userToken))
	b.WriteString("rules:\n")
	b.WriteString("  - name: inject-google-oauth-token\n")
	b.WriteString("    hosts:\n")
	for _, host := range hosts {
		fmt.Fprintf(&b, "      - %q\n", host)
	}
	b.WriteString("    paths:\n")
	for _, path := range paths {
		fmt.Fprintf(&b, "      - %q\n", path)
	}
	b.WriteString("    action: allow\n")
	b.WriteString("    credentials:\n")
	fmt.Fprintf(&b, "      - %q\n", credentialID)
	b.WriteString("  - name: inject-todoist-oauth-token\n")
	b.WriteString("    hosts:\n")
	for _, host := range todoistHosts {
		fmt.Fprintf(&b, "      - %q\n", host)
	}
	b.WriteString("    paths:\n")
	for _, path := range todoistPaths {
		fmt.Fprintf(&b, "      - %q\n", path)
	}
	b.WriteString("    action: allow\n")
	b.WriteString("    credentials:\n")
	fmt.Fprintf(&b, "      - %q\n", todoistCredentialID)
	return b.String()
}

func sciaTokenBrokerURL(namespace, provider, userToken string) string {
	brokerURL := fmt.Sprintf("http://scia-oauth.%s.svc.cluster.local:8081/oauth/%s/token", namespace, url.PathEscape(provider))
	if userToken == "" {
		return brokerURL
	}
	return brokerURL + "?user_token=" + url.QueryEscape(userToken)
}

var sciaDynamicUserInvalidChars = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeSciaDynamicUserID(value string) string {
	value = strings.ToLower(value)
	value = sciaDynamicUserInvalidChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "default"
	}
	if len(value) > 63 {
		value = strings.Trim(value[:63], "-")
	}
	if value == "" {
		return "default"
	}
	return value
}

// buildDinDContainers returns the DinD sidecar container, env vars for the main container,
// and extra volumes needed for Docker-in-Docker support.
