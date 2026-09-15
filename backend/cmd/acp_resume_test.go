package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredConversationRestore(t *testing.T) {
	for _, tc := range []struct {
		name       string
		supported  bool
		marker     string
		loadErr    error
		wantLoaded bool
	}{
		{"saved conversation", true, "conversation-1", nil, true},
		{"missing marker", true, "", nil, false},
		{"unsupported adapter", false, "conversation-1", nil, false},
		{"agent rejected resume", true, "conversation-1", errors.New("not found"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".acp-session-id")
			if tc.marker != "" {
				if err := os.WriteFile(path, []byte(tc.marker), 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			loaded, err := loadSavedACPSession(context.Background(), path, "/workspace", tc.supported, true, func(_ context.Context, id, cwd string) error {
				calls++
				if id != tc.marker || cwd != "/workspace" {
					t.Fatal("wrong identity")
				}
				return tc.loadErr
			})
			if loaded != tc.wantLoaded || (err == nil) != tc.wantLoaded {
				t.Fatalf("loaded=%v error=%v", loaded, err)
			}
			if !tc.supported && calls != 0 {
				t.Fatal("unsupported adapter was called")
			}
		})
	}
}
func TestNewConversationMayStartWithoutState(t *testing.T) {
	loaded, err := loadSavedACPSession(context.Background(), filepath.Join(t.TempDir(), "missing"), "/workspace", true, false, nil)
	if loaded || err != nil {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
}
