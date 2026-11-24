package provisioner

import (
	"bytes"
	"context"
	"fmt"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func waitForAgentAPI(ctx context.Context, agentapiURL string, maxRetries int) error {
	client := &http.Client{Timeout: 3 * time.Second}
	return waitForAgentAPIWithClient(ctx, client, agentapiURL, maxRetries)
}

func waitForAgentAPIWithClient(ctx context.Context, client *http.Client, agentapiURL string, maxRetries int) error {
	statusURL := agentapiURL + "/status"

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		default:
		}

		resp, err := client.Get(statusURL)
		if err == nil {
			closeutils.SafeCloseResponseBody(resp.Body)
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("agentapi not ready after %d retries", maxRetries)
}

func waitForSciaProxy(ctx context.Context, proxyURL string, timeout time.Duration) {
	if proxyURL == "" {
		return
	}
	healthURL := strings.TrimRight(proxyURL, "/") + "/_scia/healthz"
	client := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		resp, err := client.Get(healthURL)
		if err == nil {
			closeutils.SafeCloseResponseBody(resp.Body)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				log.Printf("[PROVISIONER] scia proxy is ready at %s", proxyURL)
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	log.Printf("[PROVISIONER] Warning: scia proxy did not become ready before timeout: %s", proxyURL)
}

func prepareSciaCABundle(ctx context.Context, envMap map[string]string) {
	proxyURL := envMap["AGENTAPI_SCIA_PROXY_URL"]
	if proxyURL == "" {
		return
	}
	waitForSciaProxy(ctx, proxyURL, 15*time.Second)

	sciaCAPath := envMap["NODE_EXTRA_CA_CERTS"]
	if sciaCAPath == "" {
		sciaCAPath = "/etc/scia/mitm/ca.pem"
	}
	bundlePath := envMap["SSL_CERT_FILE"]
	if bundlePath == "" || bundlePath == sciaCAPath {
		return
	}

	sciaCA, err := os.ReadFile(sciaCAPath)
	if err != nil {
		log.Printf("[PROVISIONER] Warning: failed to read scia CA %s: %v", sciaCAPath, err)
		return
	}

	var bundle []byte
	for _, systemPath := range []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/ca-bundle.pem",
	} {
		systemCA, err := os.ReadFile(systemPath)
		if err == nil && len(systemCA) > 0 {
			bundle = append(bundle, systemCA...)
			if !bytes.HasSuffix(bundle, []byte("\n")) {
				bundle = append(bundle, '\n')
			}
			break
		}
	}
	bundle = append(bundle, sciaCA...)
	if !bytes.HasSuffix(bundle, []byte("\n")) {
		bundle = append(bundle, '\n')
	}
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write scia CA bundle %s: %v", bundlePath, err)
		return
	}
	log.Printf("[PROVISIONER] Wrote scia CA bundle to %s", bundlePath)
}

// agentStatusResponse is the minimal shape of agentapi's /status response.
