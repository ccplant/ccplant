//go:build e2e

package api_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

const storageNamespaceMountPath = "/home/agentapi/storage-namespace-e2e"

type storageNamespaceResource struct {
	ID string `json:"id"`
}

type storageNamespacePreview struct {
	Settings struct {
		Storages []struct {
			StorageID   string `json:"storage_id"`
			StoragePath string `json:"storage_path"`
		} `json:"storages"`
	} `json:"settings"`
}

type storageNamespaceManifest struct {
	Files []struct {
		Path    string `json:"path"`
		Deleted bool   `json:"deleted"`
	} `json:"files"`
}

func TestStorageTagNamespace(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the storage namespace scenario")
	}

	repositoryA := envOrDefault("E2E_PARAM_REPOSITORY_A", "ccplant/ccplant")
	repositoryB := envOrDefault("E2E_PARAM_REPOSITORY_B", "ccplant/ccplant-deploy")
	if repositoryA == repositoryB {
		t.Fatal("E2E_PARAM_REPOSITORY_A and E2E_PARAM_REPOSITORY_B must differ")
	}
	pool := envOrDefault("E2E_PARAM_POOL", "fly-dev")
	agentType := envOrDefault("E2E_PARAM_AGENT_TYPE", "codex-acp")

	c := client(t)
	name := apitest.UniqueName()
	var storage, profile storageNamespaceResource
	var sessionIDs []string
	t.Cleanup(func() {
		cleanupStorageNamespaceResources(t, c, sessionIDs, profile.ID, storage.ID)
	})

	request(t, c, http.MethodPost, "/storages", map[string]any{
		"name": name + "-storage", "description": "E2E tag namespace storage; automatically deleted",
		"scope": "user", "kind": "object_snapshot",
	}, &storage, http.StatusCreated)
	if storage.ID == "" {
		t.Fatal("create storage response must contain id")
	}

	config := storageNamespaceProfileConfig(storage.ID, pool, agentType, initialNamespaceWrapper())
	request(t, c, http.MethodPost, "/session-profiles", map[string]any{
		"name": name + "-profile", "description": "E2E tag namespace profile; automatically deleted",
		"scope": "user", "is_default": false, "config": config,
	}, &profile, http.StatusCreated)
	if profile.ID == "" {
		t.Fatal("create profile response must contain id")
	}

	pathA := previewStorageNamespace(t, c, profile.ID, storage.ID, repositoryA)
	pathB := previewStorageNamespace(t, c, profile.ID, storage.ID, repositoryB)
	if pathA == pathB {
		t.Fatalf("different repository tags resolved to the same storage path %q", pathA)
	}
	for repository, path := range map[string]string{repositoryA: pathA, repositoryB: pathB} {
		if !strings.HasPrefix(path, "repositories/.ccplant/namespaces/v1/") {
			t.Fatalf("repository %q resolved outside the namespace prefix: %q", repository, path)
		}
		if strings.Contains(path, repository) {
			t.Fatalf("resolved storage path exposes repository tag %q", repository)
		}
	}

	sessionA := startStorageNamespaceSession(t, c, name, profile.ID, repositoryA)
	sessionIDs = append(sessionIDs, sessionA)
	waitForSessionStatus(t, c, name, sessionA, 4*time.Minute, "active", "running", "stable")
	waitForStorageFiles(t, c, storage.ID, 2*time.Minute, pathA+"/marker.txt")
	deleteStorageNamespaceSession(t, c, sessionA)
	waitForSessionDeletion(t, c, name, sessionA, 2*time.Minute)

	sessionB := startStorageNamespaceSession(t, c, name, profile.ID, repositoryB)
	sessionIDs = append(sessionIDs, sessionB)
	waitForSessionStatus(t, c, name, sessionB, 4*time.Minute, "active", "running", "stable")
	waitForStorageFiles(t, c, storage.ID, 2*time.Minute, pathA+"/marker.txt", pathB+"/marker.txt")
	deleteStorageNamespaceSession(t, c, sessionB)
	waitForSessionDeletion(t, c, name, sessionB, 2*time.Minute)

	config = storageNamespaceProfileConfig(storage.ID, pool, agentType, restoreNamespaceWrapper())
	request(t, c, http.MethodPut, "/session-profiles/"+url.PathEscape(profile.ID), map[string]any{
		"config": config,
	}, &profile, http.StatusOK)

	restoredA := startStorageNamespaceSession(t, c, name, profile.ID, repositoryA)
	sessionIDs = append(sessionIDs, restoredA)
	waitForSessionStatus(t, c, name, restoredA, 4*time.Minute, "active", "running", "stable")
	waitForStorageFiles(t, c, storage.ID, 2*time.Minute,
		pathA+"/marker.txt", pathA+"/restored.txt", pathB+"/marker.txt")

	if repeated := previewStorageNamespace(t, c, profile.ID, storage.ID, repositoryA); repeated != pathA {
		t.Fatalf("same repository tag changed storage namespace: first=%q repeated=%q", pathA, repeated)
	}
}

func storageNamespaceProfileConfig(storageID, pool, agentType, wrapper string) map[string]any {
	return map[string]any{
		"pool":                     pool,
		"command_wrapper_template": wrapper,
		"params": map[string]any{
			"agent_type":  agentType,
			"session_ttl": "10m",
			"storages": []map[string]any{{
				"storage_id": storageID, "storage_path": "repositories",
				"namespace":  map[string]any{"mode": "tags", "tag_keys": []string{"repository"}},
				"mount_path": storageNamespaceMountPath,
			}},
		},
	}
}

func initialNamespaceWrapper() string {
	return "mkdir -p " + storageNamespaceMountPath + "; printf marker > " + storageNamespaceMountPath + "/marker.txt; exec {{ .Command }}"
}

func restoreNamespaceWrapper() string {
	return "mkdir -p " + storageNamespaceMountPath + "; if test -f " + storageNamespaceMountPath + "/marker.txt; then printf restored > " + storageNamespaceMountPath + "/restored.txt; else printf marker > " + storageNamespaceMountPath + "/marker.txt; fi; exec {{ .Command }}"
}

func previewStorageNamespace(t *testing.T, c *apitest.Client, profileID, storageID, repository string) string {
	t.Helper()
	var preview storageNamespacePreview
	request(t, c, http.MethodPost, "/start?dry_run=true", map[string]any{
		"scope": "user", "session_profile_id": profileID,
		"tags":   map[string]string{"repository": repository},
		"params": map[string]any{"repo_full_name": repository},
	}, &preview, http.StatusOK)
	for _, storage := range preview.Settings.Storages {
		if storage.StorageID == storageID {
			if storage.StoragePath == "" {
				t.Fatal("dry-run returned an empty resolved storage path")
			}
			return storage.StoragePath
		}
	}
	t.Fatalf("dry-run settings did not contain storage %q", storageID)
	return ""
}

func startStorageNamespaceSession(t *testing.T, c *apitest.Client, e2eRun, profileID, repository string) string {
	t.Helper()
	var created struct {
		SessionID string `json:"session_id"`
	}
	request(t, c, http.MethodPost, "/start", map[string]any{
		"scope": "user", "session_profile_id": profileID,
		"tags":   map[string]string{"e2e_run": e2eRun, "repository": repository},
		"params": map[string]any{"repo_full_name": repository},
	}, &created, http.StatusOK)
	if created.SessionID == "" {
		t.Fatal("start response must contain session_id")
	}
	return created.SessionID
}

func waitForStorageFiles(t *testing.T, c *apitest.Client, storageID string, timeout time.Duration, paths ...string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var missing []string
	for {
		var manifest storageNamespaceManifest
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Do(ctx, http.MethodGet, "/storages/"+url.PathEscape(storageID)+"/files", nil, &manifest, http.StatusOK)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		present := make(map[string]bool, len(manifest.Files))
		for _, file := range manifest.Files {
			if !file.Deleted {
				present[file.Path] = true
			}
		}
		missing = missing[:0]
		for _, path := range paths {
			if !present[path] {
				missing = append(missing, path)
			}
		}
		if len(missing) == 0 {
			return
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("storage did not contain expected files; missing=%v", missing)
		}
		time.Sleep(2 * time.Second)
	}
}

func deleteStorageNamespaceSession(t *testing.T, c *apitest.Client, sessionID string) {
	t.Helper()
	request(t, c, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil, nil,
		http.StatusOK, http.StatusAccepted, http.StatusNotFound)
}

func waitForSessionDeletion(t *testing.T, c *apitest.Client, tag, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var out struct {
			Sessions []searchSession `json:"sessions"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Do(ctx, http.MethodGet, "/search?scope=user&tag.e2e_run="+url.QueryEscape(tag), nil, &out, http.StatusOK)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, session := range out.Sessions {
			found = found || session.SessionID == sessionID
		}
		if !found {
			return
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("session %s was not deleted", sessionID)
		}
		time.Sleep(2 * time.Second)
	}
}

func cleanupStorageNamespaceResources(t *testing.T, c *apitest.Client, sessionIDs []string, profileID, storageID string) {
	t.Helper()
	for _, sessionID := range sessionIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.Do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil, nil,
			http.StatusOK, http.StatusAccepted, http.StatusNotFound)
		cancel()
		if err != nil {
			t.Errorf("cleanup session %s failed: %v", sessionID, err)
		}
	}
	resources := []struct {
		label string
		path  string
	}{
		{"profile", "/session-profiles/" + url.PathEscape(profileID)},
		{"storage", "/storages/" + url.PathEscape(storageID)},
	}
	for _, resource := range resources {
		label, path := resource.label, resource.path
		if strings.HasSuffix(path, "/") {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK, http.StatusNoContent, http.StatusNotFound)
		cancel()
		if err != nil {
			t.Errorf("cleanup %s failed: %v", label, err)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
