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
	request := executorproxy.Request{Args: []string{"bash", "-c", "printf out; printf err >&2; exit 7"}, Cwd: dir, Env: os.Environ()}
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
