package provisioner

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

var managedFilePaths = []string{
	filepath.Join(runtimeHome, ".codex", "auth.json"),
	filepath.Join(runtimeHome, ".claude", ".credentials.json"),
	filepath.Join(runtimeHome, ".ccplant", "antigravity-auth.json"),
	filepath.Join(runtimeHome, ".config", "gcloud", "application_default_credentials.json"),
}

// writeFiles writes each ManagedFile to its path, creating parent directories
// as needed.  The file mode is taken from ManagedFile.Permissions (octal string,
// e.g. "0600"); if empty or unparseable, 0600 is used as a safe default.
// Called once at provision time to restore files delivered via SessionSettings.
func writeFiles(files []sessionsettings.ManagedFile) error {
	var firstErr error
	for _, f := range files {
		perm := os.FileMode(0600)
		if f.Permissions != "" {
			if p, err := strconv.ParseUint(f.Permissions, 8, 32); err == nil {
				perm = os.FileMode(p)
			} else {
				log.Printf("[PROVISIONER] Warning: invalid permissions %q for %s, using 0600", f.Permissions, f.Path)
			}
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to create dir for %s: %v", f.Path, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.WriteFile(f.Path, []byte(f.Content), perm); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to write %s: %v", f.Path, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Printf("[PROVISIONER] Restored managed file: %s (mode %s)", f.Path, perm)
	}
	return firstErr
}

type portableAntigravityAuthBundle struct {
	Version int `json:"version"`
	Files   []struct {
		Path    string `json:"path"`
		Content string `json:"content_base64"`
	} `json:"files"`
}

func managedFilePresent(files []sessionsettings.ManagedFile, fileType string) bool {
	want := sessionsettings.ManagedFileTypes[fileType]
	want = filepath.Join(runtimeHome, strings.TrimPrefix(want, "/home/agentapi/"))
	for _, file := range files {
		if file.Path == want && strings.TrimSpace(file.Content) != "" {
			return true
		}
	}
	return false
}

func restoreAntigravityAuthBundle(files []sessionsettings.ManagedFile) error {
	bundlePath := sessionsettings.ManagedFileTypes[sessionsettings.FileTypeAntigravityAuth]
	bundlePath = filepath.Join(runtimeHome, strings.TrimPrefix(bundlePath, "/home/agentapi/"))
	var raw string
	for _, file := range files {
		if file.Path == bundlePath {
			raw = file.Content
			break
		}
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var bundle portableAntigravityAuthBundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil || bundle.Version != 1 {
		return errors.New("invalid Antigravity auth bundle")
	}
	root := filepath.Join(runtimeHome, ".gemini", "antigravity-cli")
	for _, file := range bundle.Files {
		rel := filepath.Clean(filepath.FromSlash(file.Path))
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("invalid Antigravity auth bundle path")
		}
		content, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return errors.New("invalid Antigravity auth bundle content")
		}
		dest := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dest, content, 0o600); err != nil {
			return err
		}
	}
	log.Printf("[PROVISIONER] Restored %d Antigravity authentication file(s)", len(bundle.Files))
	return nil
}

func writeCodexRequirements(hooksJSON map[string]interface{}) error {
	if len(hooksJSON) == 0 {
		return nil
	}

	content, err := buildCodexRequirementsTOML(sessionsettings.MergeCodexManagedHooks(hooksJSON))
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return writeCodexRequirementsToPath(codexRequirementsPath, content)
}

func writeCodexRequirementsToPath(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	log.Printf("[PROVISIONER] Wrote Codex managed requirements to %s", path)
	return nil
}

func buildCodexRequirementsTOML(hooksJSON map[string]interface{}) (string, error) {
	rawHooks, ok := hooksJSON["hooks"]
	if !ok {
		return "", nil
	}
	hooksMap, ok := rawHooks.(map[string]interface{})
	if !ok || len(hooksMap) == 0 {
		return "", nil
	}

	var events []string
	for event := range hooksMap {
		// Codex does not support Claude Code's Notification hook event.
		if event == "Notification" {
			continue
		}
		entries := asInterfaceSlice(hooksMap[event])
		if len(entries) == 0 {
			continue
		}
		events = append(events, event)
	}
	sort.Strings(events)
	if len(events) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("[hooks]\n")
	b.WriteString("managed_dir = \"/etc/codex\"\n")

	for _, event := range events {
		entries := asInterfaceSlice(hooksMap[event])
		for _, entry := range entries {
			entryMap := asStringMap(entry)
			if len(entryMap) == 0 {
				continue
			}
			hookEntries := asInterfaceSlice(entryMap["hooks"])
			if len(hookEntries) == 0 {
				continue
			}

			b.WriteString("\n")
			b.WriteString("[[hooks.")
			b.WriteString(event)
			b.WriteString("]]\n")

			for _, hook := range hookEntries {
				hookMap := asStringMap(hook)
				if len(hookMap) == 0 {
					continue
				}
				hookType, _ := hookMap["type"].(string)
				command, _ := hookMap["command"].(string)
				if hookType == "" || command == "" {
					continue
				}
				b.WriteString("\n")
				b.WriteString("[[hooks.")
				b.WriteString(event)
				b.WriteString(".hooks]]\n")
				b.WriteString("type = ")
				b.WriteString(tomlQuote(hookType))
				b.WriteString("\n")
				b.WriteString("command = ")
				b.WriteString(tomlQuote(command))
				b.WriteString("\n")
			}
		}
	}

	return b.String(), nil
}

func asInterfaceSlice(v interface{}) []interface{} {
	switch typed := v.(type) {
	case []interface{}:
		return typed
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func asStringMap(v interface{}) map[string]interface{} {
	if typed, ok := v.(map[string]interface{}); ok {
		return typed
	}
	return nil
}

func tomlQuote(s string) string {
	return strconv.Quote(s)
}

// mergeHooksIntoSettingsFile reads the Claude Code settings.json at path, merges
// any hooks from compiledSettings into it (compiled hooks win per event name),
// and writes the result back.  This ensures that proxy-injected hooks (oneshot
// delete-session, cycle, etc.) survive even when a managed settings.json file
// was restored on top of the compiled one.
func mergeHooksIntoSettingsFile(path string, compiledSettings map[string]interface{}) error {
	compiledHooksRaw, ok := compiledSettings["hooks"]
	if !ok {
		return nil // no hooks to inject
	}
	compiledHooks, ok := compiledHooksRaw.(map[string]interface{})
	if !ok || len(compiledHooks) == 0 {
		return nil
	}

	// Read the current file (may have been overwritten by writeFiles)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var current map[string]interface{}
	if err := json.Unmarshal(data, &current); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Merge: start with whatever hooks the managed file already has, then
	// overwrite per-event with the compiled hooks so proxy-injected entries win.
	merged := make(map[string]interface{})
	if existingRaw, ok := current["hooks"]; ok {
		if existing, ok := existingRaw.(map[string]interface{}); ok {
			for k, v := range existing {
				merged[k] = v
			}
		}
	}
	for k, v := range compiledHooks {
		merged[k] = v
	}
	current["hooks"] = merged

	out, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	log.Printf("[PROVISIONER] Re-applied compiled hooks to %s", path)
	return nil
}

// writeCredentials writes the given JSON string to ~/.codex/auth.json (mode 0600).
// Deprecated: used only for backward compatibility with old SessionSettings that
// do not have the Files field.  New sessions use writeFiles instead.
func writeCredentials(credentialsJSON string) error {
	codexDir := filepath.Join(os.Getenv("HOME"), ".codex")
	if codexDir == "/.codex" {
		codexDir = "/home/agentapi/.codex"
	}
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}
	destPath := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(destPath, []byte(credentialsJSON), 0600); err != nil {
		return fmt.Errorf("failed to write auth.json: %w", err)
	}
	return nil
}

// runFilesSync watches managed files and sends snapshots through the
// session-scoped control API. Session Pods never receive Kubernetes write access.
// The goroutine is tied to ctx: when ctx is cancelled the loop exits.
