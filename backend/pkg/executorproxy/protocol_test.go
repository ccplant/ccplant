package executorproxy

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteFrame(&buffer, FrameStderr, []byte("boom")); err != nil {
		t.Fatal(err)
	}
	kind, data, err := ReadFrame(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if kind != FrameStderr || string(data) != "boom" {
		t.Fatalf("got kind=%d data=%q", kind, data)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	want := Request{Args: []string{"bash", "-c", "echo ok"}, Cwd: "/workspace", Env: []string{"A=B"}, Files: []ManagedFile{{Path: "/home/agentapi/profile", Data: []byte("value"), Mode: 0o640}}}
	var buffer bytes.Buffer
	if err := WriteJSON(&buffer, want); err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := ReadJSON(&buffer, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cwd != want.Cwd || len(got.Args) != 3 || got.Args[2] != "echo ok" || len(got.Files) != 1 || string(got.Files[0].Data) != "value" || got.Files[0].Mode != 0o640 {
		t.Fatalf("got %#v", got)
	}
}
