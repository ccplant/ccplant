package sessionsettings

import (
	"path/filepath"
	"testing"
)

func TestPathWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "home", "agentapi", "workdir")
	tests := []struct {
		name string
		root string
		path string
		want bool
	}{
		{name: "root itself", root: root, path: root, want: true},
		{name: "direct child", root: root, path: filepath.Join(root, "repo"), want: true},
		{name: "nested child", root: root, path: filepath.Join(root, "owner", "repo"), want: true},
		{name: "sibling with shared prefix", root: root, path: root + "-other", want: false},
		{name: "parent", root: root, path: filepath.Dir(root), want: false},
		{name: "unrelated", root: root, path: filepath.Join(string(filepath.Separator), "tmp", "repo"), want: false},
		{name: "empty root", root: "", path: filepath.Join(root, "repo"), want: false},
		{name: "empty path", root: root, path: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PathWithin(tt.root, tt.path); got != tt.want {
				t.Fatalf("PathWithin(%q, %q) = %v, want %v", tt.root, tt.path, got, tt.want)
			}
		})
	}
}

func TestKeepsExistingClone(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "home", "agentapi", "workdir")
	cloneDir := filepath.Join(root, "repo")
	tests := []struct {
		name     string
		settings *SessionSettings
		root     string
		want     bool
	}{
		{name: "first provisioning checks out the target", settings: &SessionSettings{}, root: "", want: false},
		{name: "restart keeps the clone", settings: &SessionSettings{Restart: true}, root: "", want: true},
		{name: "preserved workspace keeps the clone", settings: &SessionSettings{}, root: root, want: true},
		{name: "clone outside the preserved workspace is checked out", settings: &SessionSettings{}, root: filepath.Join(root, "other"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keepsExistingClone(tt.settings, cloneDir, tt.root); got != tt.want {
				t.Fatalf("keepsExistingClone() = %v, want %v", got, tt.want)
			}
		})
	}
}
