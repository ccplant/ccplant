package sessionstate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPackUnpackClaude(t *testing.T) {
	srcHome, srcCwd := t.TempDir(), t.TempDir()
	id := "11111111-1111-1111-1111-111111111111"
	mustWrite(t, filepath.Join(srcCwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", id+".jsonl"), "main\n")
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", id, "subagents", "agent-a.jsonl"), "sub\n")
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", "other.jsonl"), "nope\n")
	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, srcHome, srcCwd); err != nil {
		t.Fatal(err)
	}
	dstHome, dstCwd := t.TempDir(), t.TempDir()
	if err := Unpack(&archive, dstHome, dstCwd); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dstCwd, ".acp-session-id"), id)
	assertFile(t, filepath.Join(dstHome, ".claude", "projects", "project", id+".jsonl"), "main\n")
	if _, err := os.Stat(filepath.Join(dstHome, ".claude", "projects", "project", "other.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("unrelated transcript restored: %v", err)
	}
}

func TestPackUnpackCodex(t *testing.T) {
	srcHome, srcCwd := t.TempDir(), t.TempDir()
	id := "22222222-2222-2222-2222-222222222222"
	mustWrite(t, filepath.Join(srcCwd, ".acp-session-id"), id)
	rollout := filepath.Join(".codex", "sessions", "2026", "08", "03", "rollout-x-"+id+".jsonl")
	mustWrite(t, filepath.Join(srcHome, rollout), "rollout\n")
	var archive bytes.Buffer
	if err := Pack(&archive, "codex-acp", id, srcHome, srcCwd); err != nil {
		t.Fatal(err)
	}
	dstHome, dstCwd := t.TempDir(), t.TempDir()
	if err := Unpack(&archive, dstHome, dstCwd); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dstHome, rollout), "rollout\n")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
