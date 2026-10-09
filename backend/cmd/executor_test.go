package cmd

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/executorproxy"
)

func TestExecutorServerStreamsOutputAndExit(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "executor.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runExecutorServer(ctx, socket, "/bin/bash", dir) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("executor socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	managedPath := filepath.Join(dir, "profile", "managed.txt")
	request := executorproxy.Request{
		Args: []string{"bash", "-c", `cat "$0"; printf err >&2; exit 7`, managedPath},
		Cwd:  dir,
		Env:  os.Environ(),
		Files: []executorproxy.ManagedFile{{
			Path: managedPath,
			Data: []byte("out"),
			Mode: 0o600,
		}},
	}
	if err := executorproxy.WriteJSON(conn, request); err != nil {
		t.Fatal(err)
	}
	if err := executorproxy.WriteFrame(conn, executorproxy.FrameStdinEOF, nil); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr string
	for {
		kind, data, err := executorproxy.ReadFrame(conn)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case executorproxy.FrameStdout:
			stdout += string(data)
		case executorproxy.FrameStderr:
			stderr += string(data)
		case executorproxy.FrameExit:
			var exit executorproxy.Exit
			if err := json.Unmarshal(data, &exit); err != nil {
				t.Fatal(err)
			}
			if stdout != "out" || stderr != "err" || exit.Code != 7 {
				t.Fatalf("stdout=%q stderr=%q exit=%#v", stdout, stderr, exit)
			}
			return
		}
	}
}

func TestExecutorClientBasename(t *testing.T) {
	args, ok := ExecutorClientArgsForBasename([]string{"/bin/bash", "-lc", "true"})
	if !ok || len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("args=%q ok=%v", args, ok)
	}
	if _, ok := ExecutorClientArgsForBasename([]string{"ccplant", "server"}); ok {
		t.Fatal("ccplant must not dispatch as a shell")
	}
}

func TestExecutorManagedFilesAreHydratedAtOriginalPath(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "agent", ".config", "profile")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("profile-value"), 0o640); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	files, err := readExecutorManagedFiles(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "executor", ".config", "profile")
	files[0].Path = target
	if err := hydrateExecutorManagedFiles(files); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "profile-value" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}
