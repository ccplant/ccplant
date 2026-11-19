package storageattachment

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestManagedNamespaceOverlapAllowed(t *testing.T) {
	regularRoot := entities.StorageAttachment{StorageID: "state", MountPath: "/home/agentapi/memory"}
	managedState := entities.StorageAttachment{StorageID: "state", StoragePath: "session-state/codex", MountPath: "/home/agentapi/.codex", Managed: true}
	repeatedRoot := entities.StorageAttachment{StorageID: "state", MountPath: "/home/agentapi/other"}
	regularManagedPath := entities.StorageAttachment{StorageID: "state", StoragePath: "session-state", MountPath: "/home/agentapi/state"}

	if !managedNamespaceOverlapAllowed(regularRoot, managedState) {
		t.Fatal("regular root and managed session state should be safely partitioned")
	}
	if managedNamespaceOverlapAllowed(regularRoot, repeatedRoot) {
		t.Fatal("two regular overlapping attachments must remain rejected")
	}
	if managedNamespaceOverlapAllowed(regularManagedPath, managedState) {
		t.Fatal("an explicit regular managed-namespace attachment must remain rejected")
	}
}

func TestRegularRootExcludesManagedNamespaces(t *testing.T) {
	regularRoot := entities.StorageAttachment{StorageID: "state", MountPath: "/home/agentapi/memory"}
	for _, path := range []string{"session-state/codex/state.json", "workspace/repo/file.txt"} {
		if attachmentIncludesRemotePath(regularRoot, path) {
			t.Fatalf("regular root unexpectedly includes managed path %q", path)
		}
	}
	if !attachmentIncludesRemotePath(regularRoot, "notes/todo.md") {
		t.Fatal("regular root should include ordinary storage files")
	}
}

func TestNormalizeMountPath(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "absolute path", value: " /data/shared ", want: "/data/shared"},
		{name: "relative path", value: "workdir/shared", want: "/home/agentapi/workdir/shared"},
		{name: "relative dotfile", value: ".cache/models", want: "/home/agentapi/.cache/models"},
		{name: "empty", value: "", wantErr: true},
		{name: "root", value: "/", wantErr: true},
		{name: "relative traversal", value: "../data", wantErr: true},
		{name: "absolute traversal", value: "/data/../other", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMountPath(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeMountPath(%q) unexpectedly succeeded with %q", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeMountPath(%q): %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeMountPath(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
