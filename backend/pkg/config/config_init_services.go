package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

func applyWorkerEnv(config *Config) {
	if namespace := os.Getenv("AGENTAPI_K8S_SESSION_NAMESPACE"); namespace != "" {
		config.KubernetesSession.Namespace = namespace
	}
	if namespace := os.Getenv("AGENTAPI_SCHEDULE_WORKER_NAMESPACE"); namespace != "" {
		config.ScheduleWorker.Namespace = namespace
	}
	if namespace := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_NAMESPACE"); namespace != "" {
		config.StockInventoryWorker.Namespace = namespace
	}
	if enabled := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_ENABLED"); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			config.StockInventoryWorker.Enabled = parsed
		}
	}
	if checkInterval := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_CHECK_INTERVAL"); checkInterval != "" {
		config.StockInventoryWorker.CheckInterval = checkInterval
	}
	if targetCount := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_TARGET_COUNT"); targetCount != "" {
		if parsed, err := strconv.Atoi(targetCount); err == nil {
			config.StockInventoryWorker.TargetCount = parsed
		}
	}
	if dockerEnabled := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_DOCKER_ENABLED"); dockerEnabled != "" {
		if parsed, err := strconv.ParseBool(dockerEnabled); err == nil {
			config.StockInventoryWorker.DockerEnabled = parsed
		}
	}
	if leaseDuration := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_LEASE_DURATION"); leaseDuration != "" {
		config.StockInventoryWorker.LeaseDuration = leaseDuration
	}
	if renewDeadline := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_RENEW_DEADLINE"); renewDeadline != "" {
		config.StockInventoryWorker.RenewDeadline = renewDeadline
	}
	if retryPeriod := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_RETRY_PERIOD"); retryPeriod != "" {
		config.StockInventoryWorker.RetryPeriod = retryPeriod
	}
	if poolsJSON := os.Getenv("AGENTAPI_STOCK_INVENTORY_WORKER_POOLS"); poolsJSON != "" {
		pools, err := parseStockInventoryPoolsJSON(poolsJSON)
		if err != nil {
			log.Printf("[CONFIG] Warning: Failed to parse stock inventory worker pools JSON: %v", err)
		} else {
			config.StockInventoryWorker.Pools = pools
		}
	}
}

func applySciaEnv(config *Config) {
	if enabled, ok := os.LookupEnv("AGENTAPI_SCIA_ENABLED"); ok {
		config.Scia.Enabled = strings.EqualFold(enabled, "true")
	}
	if publicBaseURL := os.Getenv("AGENTAPI_SCIA_PUBLIC_BASE_URL"); publicBaseURL != "" {
		config.Scia.PublicBaseURL = publicBaseURL
	}
	if proxyURL := os.Getenv("AGENTAPI_SCIA_PROXY_URL"); proxyURL != "" {
		config.Scia.ProxyURL = proxyURL
	}
	if credential := os.Getenv("AGENTAPI_SCIA_CREDENTIAL"); credential != "" {
		config.Scia.Credential = credential
	}
	if userNamespace := os.Getenv("AGENTAPI_SCIA_USER_NAMESPACE"); userNamespace != "" {
		config.Scia.UserNamespace = userNamespace
	}
	if noProxy := os.Getenv("AGENTAPI_SCIA_NO_PROXY"); noProxy != "" {
		config.Scia.NoProxy = noProxy
	}
	if enabled, ok := os.LookupEnv("AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED"); ok {
		config.Scia.SessionSidecarEnabled = strings.EqualFold(enabled, "true")
	}
	if image := os.Getenv("AGENTAPI_SCIA_SESSION_SIDECAR_IMAGE"); image != "" {
		config.Scia.SessionSidecarImage = image
	}
	if image := os.Getenv("AGENTAPI_SCIA_SESSION_SIDECAR_CONFIG_IMAGE"); image != "" {
		config.Scia.SessionSidecarConfigImage = image
	}
	if port := os.Getenv("AGENTAPI_SCIA_SESSION_SIDECAR_PORT"); port != "" {
		if parsed, err := strconv.Atoi(port); err == nil {
			config.Scia.SessionSidecarPort = parsed
		}
	}
	if hosts := commaSeparatedList(os.Getenv("AGENTAPI_SCIA_GOOGLE_HOSTS")); len(hosts) > 0 {
		config.Scia.GoogleHosts = hosts
	}
	if paths := commaSeparatedList(os.Getenv("AGENTAPI_SCIA_GOOGLE_PATHS")); len(paths) > 0 {
		config.Scia.GooglePaths = paths
	}
	if credential := os.Getenv("AGENTAPI_SCIA_TODOIST_CREDENTIAL"); credential != "" {
		config.Scia.TodoistCredential = credential
	}
	if hosts := commaSeparatedList(os.Getenv("AGENTAPI_SCIA_TODOIST_HOSTS")); len(hosts) > 0 {
		config.Scia.TodoistHosts = hosts
	}
	if paths := commaSeparatedList(os.Getenv("AGENTAPI_SCIA_TODOIST_PATHS")); len(paths) > 0 {
		config.Scia.TodoistPaths = paths
	}
}
