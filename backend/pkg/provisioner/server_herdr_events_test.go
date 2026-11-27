package provisioner

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHerdrEventsReconnectsWhenHCExits(t *testing.T) {
	binDir := t.TempDir()
	countPath := filepath.Join(binDir, "count")
	hcPath := filepath.Join(binDir, "hc")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf '%%s' "$count" > %q
if [ "$count" -eq 1 ]; then exit 0; fi
printf '%%s\n' '{"kind":"snapshot","terminal":"stream-marker"}'
`, countPath, countPath, countPath)
	if err := os.WriteFile(hcPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	server := httptest.NewServer(http.HandlerFunc((&Server{}).handleHerdrEvents))
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()

	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "stream-marker") {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("event stream closed before the reconnected hc process emitted data")
}
