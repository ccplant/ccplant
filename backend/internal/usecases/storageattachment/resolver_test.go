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

func TestResolveNamespaceFromTags(t *testing.T) {
	left := entities.StorageAttachment{StoragePath: "repositories", Namespace: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"environment", "repository"}}}
	right := entities.StorageAttachment{StoragePath: "repositories", Namespace: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"repository", "environment"}}}
	tags := map[string]string{"repository": "acme/api", "environment": "development"}
	leftID, err := resolveNamespace(&left, tags)
	if err != nil {
		t.Fatalf("resolve left namespace: %v", err)
	}
	rightID, err := resolveNamespace(&right, tags)
	if err != nil {
		t.Fatalf("resolve right namespace: %v", err)
	}
	if leftID == "" || leftID != rightID {
		t.Fatalf("namespace IDs must be stable across tag key order: %q != %q", leftID, rightID)
	}
	want := "repositories/.ccplant/namespaces/v1/" + leftID[:2] + "/" + leftID
	if left.StoragePath != want || right.StoragePath != want {
		t.Fatalf("resolved paths = %q and %q, want %q", left.StoragePath, right.StoragePath, want)
	}
	if left.Namespace.TagKeys[0] != "environment" || left.Namespace.TagKeys[1] != "repository" {
		t.Fatalf("tag keys were not normalized: %#v", left.Namespace.TagKeys)
	}
}

func TestResolveNamespaceRejectsInvalidSelectors(t *testing.T) {
	tests := []struct {
		name     string
		selector *entities.StorageNamespaceSelector
		tags     map[string]string
	}{
		{name: "unknown mode", selector: &entities.StorageNamespaceSelector{Mode: "path", TagKeys: []string{"repository"}}, tags: map[string]string{"repository": "acme/api"}},
		{name: "no keys", selector: &entities.StorageNamespaceSelector{Mode: "tags"}},
		{name: "duplicate keys", selector: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"repository", " repository "}}, tags: map[string]string{"repository": "acme/api"}},
		{name: "missing tag", selector: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"repository"}}},
		{name: "empty tag", selector: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"repository"}}, tags: map[string]string{"repository": "  "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachment := entities.StorageAttachment{Namespace: tt.selector}
			if _, err := resolveNamespace(&attachment, tt.tags); err == nil {
				t.Fatal("resolveNamespace unexpectedly succeeded")
			}
		})
	}
}

func TestResolveNamespaceDoesNotExposeTagValueInPath(t *testing.T) {
	attachment := entities.StorageAttachment{Namespace: &entities.StorageNamespaceSelector{Mode: "tags", TagKeys: []string{"repository"}}}
	if _, err := resolveNamespace(&attachment, map[string]string{"repository": "../private/repository"}); err != nil {
		t.Fatalf("resolveNamespace: %v", err)
	}
	if attachment.StoragePath == "" || attachment.StoragePath == "../private/repository" {
		t.Fatalf("tag value leaked into resolved path: %q", attachment.StoragePath)
	}
}
