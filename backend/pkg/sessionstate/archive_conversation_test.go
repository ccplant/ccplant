package sessionstate

import (
	"archive/tar"
	"bytes"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func archiveEntries(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names
}

func conversationWorkspace(t *testing.T) (home, cwd, id string) {
	t.Helper()
	home, cwd = t.TempDir(), t.TempDir()
	runTestGit(t, cwd, "init")
	runTestGit(t, cwd, "config", "user.email", "test@example.com")
	runTestGit(t, cwd, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "base\n")
	runTestGit(t, cwd, "add", ".")
	runTestGit(t, cwd, "commit", "-m", "base")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "base\nchanged\n")
	mustWrite(t, filepath.Join(cwd, "untracked.bin"), strings.Repeat("x", 1<<20))
	id = "88888888-8888-8888-8888-888888888888"
	mustWrite(t, filepath.Join(cwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(home, ".claude", "projects", "p", id+".jsonl"), "turn1\n")
	mustWrite(t, filepath.Join(home, ".session", "acp-history.jsonl"), "turn1\n")
	return home, cwd, id
}

func TestPackConversationOmitsWorkspace(t *testing.T) {
	home, cwd, id := conversationWorkspace(t)
	var archive bytes.Buffer
	if err := PackConversation(&archive, "claude-acp", id, home, cwd); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cwd/.acp-session-id",
		"home/.claude/projects/p/" + id + ".jsonl",
		"home/.session/acp-history.jsonl",
	}
	if got := archiveEntries(t, archive.Bytes()); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestPackConversationRestoresConversationAndKeepsWorkspace(t *testing.T) {
	home, cwd, id := conversationWorkspace(t)
	var archive bytes.Buffer
	if err := PackConversation(&archive, "claude-acp", id, home, cwd); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "base\nchanged\nnewer\n")
	runTestGit(t, cwd, "commit", "-am", "local")
	head := runTestGit(t, cwd, "rev-parse", "HEAD")

	for name, unpack := range map[string]func(io.Reader, string, string) error{"UnpackMissing": UnpackMissing, "Unpack": Unpack} {
		t.Run(name, func(t *testing.T) {
			dstHome := t.TempDir()
			if err := unpack(bytes.NewReader(archive.Bytes()), dstHome, cwd); err != nil {
				t.Fatal(err)
			}
			assertFile(t, filepath.Join(dstHome, ".claude", "projects", "p", id+".jsonl"), "turn1\n")
			assertFile(t, filepath.Join(dstHome, ".session", "acp-history.jsonl"), "turn1\n")
			assertFile(t, filepath.Join(cwd, "tracked.txt"), "base\nchanged\nnewer\n")
			if got := runTestGit(t, cwd, "rev-parse", "HEAD"); got != head {
				t.Fatalf("HEAD = %q, want %q", got, head)
			}
		})
	}
}
