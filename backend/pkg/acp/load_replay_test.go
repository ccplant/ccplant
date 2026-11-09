package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestLoadSessionMarksUpdatesReceivedBeforeResponseAsReplayed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentOut, toClient := io.Pipe()
	fromClient, agentIn := io.Pipe()
	c := NewClient(agentOut, agentIn, false)
	go func() { _ = c.Listen(ctx) }()

	notify := func(text string) {
		_, _ = fmt.Fprintf(toClient, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":%q}}}}`+"\n", text)
	}
	agentErr := make(chan error, 1)
	go func() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(fromClient).Decode(&req); err != nil {
			agentErr <- err
			return
		}
		if req.Method != "session/load" {
			agentErr <- fmt.Errorf("method=%s", req.Method)
			return
		}
		notify("old answer part 1")
		notify("old answer part 2")
		_, _ = fmt.Fprintf(toClient, `{"jsonrpc":"2.0","id":%s,"result":{}}`+"\n", req.ID)
		agentErr <- nil
	}()

	if err := c.LoadSession(ctx, "s", "/tmp", nil); err != nil {
		t.Fatal(err)
	}
	if err := <-agentErr; err != nil {
		t.Fatal(err)
	}
	notify("live answer")

	for _, want := range []struct {
		text     string
		replayed bool
	}{
		{"old answer part 1", true},
		{"old answer part 2", true},
		{"live answer", false},
	} {
		select {
		case update := <-c.Updates():
			if got := ExtractTextContent(update.Content); got != want.text || update.Replayed != want.replayed {
				t.Fatalf("update text=%q replayed=%v, want text=%q replayed=%v", got, update.Replayed, want.text, want.replayed)
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %q", want.text)
		}
	}
}
