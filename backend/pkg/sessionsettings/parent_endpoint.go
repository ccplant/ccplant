package sessionsettings

import (
	"net/url"
	"strings"
)

const (
	ProxyEndpointEnv     = "AGENTAPI_PROXY_ENDPOINT"
	GitHubBrokerURLEnv   = "AGENTAPI_GITHUB_BROKER_URL"
	githubBrokerPathRoot = "/internal/sessions/"
)

func RouteEnvToParent(env map[string]string, parentURL string) {
	parentURL = strings.TrimRight(strings.TrimSpace(parentURL), "/")
	if env == nil || parentURL == "" {
		return
	}
	if strings.TrimSpace(env[ProxyEndpointEnv]) == "" {
		env[ProxyEndpointEnv] = parentURL
	}
	if brokerURL, ok := parentGitHubBrokerURL(env[GitHubBrokerURLEnv], parentURL); ok {
		env[GitHubBrokerURLEnv] = brokerURL
	}
}

func parentGitHubBrokerURL(brokerURL, parentURL string) (string, bool) {
	brokerURL = strings.TrimSpace(brokerURL)
	if brokerURL == "" {
		return "", false
	}
	parsed, err := url.Parse(brokerURL)
	if err != nil {
		return "", false
	}
	path := parsed.EscapedPath()
	index := strings.Index(path, githubBrokerPathRoot)
	if index < 0 {
		return "", false
	}
	return parentURL + path[index:], true
}
