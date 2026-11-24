package sessionsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

const (
	defaultOllamaCloudBaseURL = "https://ollama.com"
	ollamaCloudRefreshWorkers = 8
)

type ollamaCloudModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaCloudModelCache struct {
	Timestamp int64                      `json:"timestamp"`
	Models    map[string]json.RawMessage `json:"models"`
}

func shouldRefreshPiCloud(settings *SessionSettings) bool {
	return settings != nil && settings.Session.AgentType == "pi"
}

func refreshPiCloudCache(env map[string]string, outputDir string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(env["OLLAMA_API_BASE"]), "/")
	if baseURL == "" {
		baseURL = defaultOllamaCloudBaseURL
	}
	client := &http.Client{Timeout: 15 * time.Second}
	apiKey := env["OLLAMA_API_KEY"]

	var list ollamaCloudModelList
	if err := ollamaCloudRequest(client, ollamaCloudRequestOptions{method: http.MethodGet, url: baseURL + "/v1/models", apiKey: apiKey, target: &list}); err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	models := make(map[string]json.RawMessage)
	var mu sync.Mutex
	jobs := make(chan string)
	workers := min(ollamaCloudRefreshWorkers, len(list.Data))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				body, err := json.Marshal(map[string]string{"model": id})
				if err != nil {
					continue
				}
				var detail json.RawMessage
				options := ollamaCloudRequestOptions{method: http.MethodPost, url: baseURL + "/api/show", apiKey: apiKey, body: body, target: &detail}
				if err := ollamaCloudRequest(client, options); err != nil {
					continue
				}
				mu.Lock()
				models[id] = detail
				mu.Unlock()
			}
		}()
	}
	for _, model := range list.Data {
		if model.ID != "" {
			jobs <- model.ID
		}
	}
	close(jobs)
	wg.Wait()

	if len(models) == 0 {
		return fmt.Errorf("no model details were retrieved")
	}
	cache, err := json.MarshalIndent(ollamaCloudModelCache{Timestamp: time.Now().UnixMilli(), Models: models}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	return writePiCloudCache(outputDir, cache)
}

func writePiCloudCache(outputDir string, cache []byte) (returnErr error) {
	cacheDir := filepath.Join(outputDir, ".pi", "agent", "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(cacheDir, ".ollama-cloud-models-*")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err := os.Remove(tmpName); err != nil && !os.IsNotExist(err) && returnErr == nil {
			returnErr = fmt.Errorf("remove temporary cache: %w", err)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return errors.Join(fmt.Errorf("set cache permissions: %w", err), tmp.Close())
	}
	written, err := tmp.Write(cache)
	if err != nil || written != len(cache) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return errors.Join(fmt.Errorf("write cache: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cache: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(cacheDir, "ollama-cloud-models.json")); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}

type ollamaCloudRequestOptions struct {
	method, url, apiKey string
	body                []byte
	target              any
}

func ollamaCloudRequest(client *http.Client, options ollamaCloudRequestOptions) error {
	req, err := http.NewRequest(options.method, options.url, bytes.NewReader(options.body))
	if err != nil {
		return err
	}
	if options.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+options.apiKey)
	}
	if options.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if err != nil {
			return fmt.Errorf("read error response: %w", err)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(resp.Body).Decode(options.target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
