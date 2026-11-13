package provisioner

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHerdrRPCForwardsAllowedMethod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell")
	}
	dir := t.TempDir()
	hc := filepath.Join(dir, "hc")
	if err := os.WriteFile(hc, []byte("#!/bin/sh\nread request\nprintf '%s\\n' \"$request\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	body := `{"jsonrpc":"2.0","id":7,"method":"terminal.list","params":{"all":false}}`
	req := httptest.NewRequest(http.MethodPost, "/herdr/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	New(9001, "").handleHerdrRPC(rec, req)

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != body {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHerdrRPCRejectsProcessControlMethods(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":8,"method":"terminal.run","params":{"line":"echo unsafe"}}`)
	req := httptest.NewRequest(http.MethodPost, "/herdr/rpc", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	New(9001, "").handleHerdrRPC(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":-32601`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
