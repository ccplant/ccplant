package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

func writeHistoryFile(t *testing.T, updates ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	lines := make([]string, len(updates))
	for i, update := range updates {
		lines[i] = `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old","update":` + update + `,"time":"2026-11-06T08:50:00Z"}}`
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func restoredBridge(t *testing.T, updates ...string) *Bridge {
	t.Helper()
	b := New(nil, "session", false, "", false)
	if len(updates) > 0 {
		if err := b.SetHistoryFile(writeHistoryFile(t, updates...)); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func parseUpdate(t *testing.T, raw string, replayed bool) acp.SessionUpdate {
	t.Helper()
	var update acp.SessionUpdate
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		t.Fatal(err)
	}
	update.Replayed = replayed
	return update
}

func emitAll(t *testing.T, b *Bridge, replayed bool, updates ...string) {
	t.Helper()
	for _, raw := range updates {
		b.emitUpdate(parseUpdate(t, raw, replayed))
	}
	b.flushChunkBuffer()
}

func historyKinds(t *testing.T, b *Bridge) []string {
	t.Helper()
	kinds := make([]string, len(b.history))
	for i, raw := range b.history {
		var msg struct {
			Params struct {
				Update struct {
					SessionUpdate string          `json:"sessionUpdate"`
					Content       json.RawMessage `json:"content"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		kinds[i] = msg.Params.Update.SessionUpdate
		if text := acp.ExtractTextContent(msg.Params.Update.Content); text != "" {
			kinds[i] += ":" + text
		}
	}
	return kinds
}

const (
	ad1aUser1          = `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"今日の社会情勢を教えてください。"}}`
	ad1aAgentHead      = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"最新の情報を確認するため、Web検索ツールを読"}}`
	ad1aUsage          = `{"sessionUpdate":"usage_update"}`
	ad1aAgentTail      = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"み込みます。"}}`
	ad1aSearchCall     = `{"sessionUpdate":"tool_call","toolCallId":"toolu_01HXuncE2fgmPHbzdeRXti5R","kind":"fetch","title":"Web search","status":"pending","content":[]}`
	ad1aSearchTitle    = `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01HXuncE2fgmPHbzdeRXti5R","title":"Search \"主要ニュース 2026年11月6日\"","rawInput":{"query":"主要ニュース 2026年11月6日"}}`
	ad1aSearchDone     = `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01HXuncE2fgmPHbzdeRXti5R","status":"completed"}`
	ad1aAnswer1        = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"2026年11月6日の主な動きを、検索結果をもとにまとめます。"}}`
	ad1aUser2          = `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"レスポンスが早いですねw"}}`
	ad1aTerminalCall   = `{"sessionUpdate":"tool_call","toolCallId":"toolu_01AbCdEf4nxJdm","kind":"execute","title":"Terminal","status":"pending","content":[]}`
	ad1aTerminalTitle  = `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01AbCdEf4nxJdm","title":"ccplant client send-notification --title done"}`
	ad1aTerminalFailed = `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01AbCdEf4nxJdm","status":"failed"}`
	ad1aAnswer2        = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"速かったのは、2つの検索を同時に走らせたからです。"}}`

	ad1aReplayAgent      = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"最新の情報を確認するため、Web検索ツールを読み込みます。"}}`
	ad1aReplaySearch     = `{"sessionUpdate":"tool_call","toolCallId":"toolu_01HXuncE2fgmPHbzdeRXti5R","kind":"fetch","title":"Search \"主要ニュース 2026年11月6日\"","status":"pending","rawInput":{"query":"主要ニュース 2026年11月6日"},"content":[]}`
	ad1aReplaySearchDone = `{"sessionUpdate":"tool_call_update","toolCallId":"toolu_01HXuncE2fgmPHbzdeRXti5R","status":"completed","rawOutput":{"result":"..."}}`
	ad1aReplayTerminal   = `{"sessionUpdate":"tool_call","toolCallId":"toolu_01AbCdEf4nxJdm","kind":"execute","title":"ccplant client send-notification --title done","status":"pending","content":[]}`
	ad1aAvailableCmds    = `{"sessionUpdate":"available_commands_update","availableCommands":[]}`
)

var ad1aRestored = []string{
	ad1aUser1, ad1aAgentHead, ad1aUsage, ad1aAgentTail,
	ad1aSearchCall, ad1aSearchTitle, ad1aSearchDone, ad1aAnswer1,
	ad1aUser2, ad1aTerminalCall, ad1aTerminalTitle, ad1aTerminalFailed, ad1aAnswer2,
}

func TestSetHistoryFileRestoresRawMessages(t *testing.T) {
	b := restoredBridge(t, `{"sessionUpdate":"user_message_chunk"}`)
	if len(b.history) != 1 || len(b.userMessageIndices) != 1 {
		t.Fatalf("history=%d user messages=%d", len(b.history), len(b.userMessageIndices))
	}
}

func TestSetHistoryFileDropsLegacyPromptContextCancellationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	history := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"acp session/prompt: context canceled"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}

	b := New(nil, "session", false, "", false)
	if err := b.SetHistoryFile(path); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != 1 {
		t.Fatalf("restored history length = %d, want 1", len(b.history))
	}
	if b.UserPromptCount() != 1 {
		t.Fatalf("user prompt count = %d, want 1", b.UserPromptCount())
	}
}

func TestEmitUpdateDropsSessionLoadReplayWithDifferentShape(t *testing.T) {
	b := restoredBridge(t, ad1aRestored...)
	want := historyKinds(t, b)

	emitAll(t, b, true,
		ad1aUser1, ad1aReplayAgent, ad1aReplaySearch, ad1aReplaySearchDone, ad1aAnswer1,
		ad1aUser2, ad1aReplayTerminal, ad1aAnswer2,
	)

	if got := historyKinds(t, b); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("history after replay:\n%s\nwant restored history only:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if b.UserPromptCount() != 2 {
		t.Fatalf("user prompts=%d, want 2", b.UserPromptCount())
	}
}

func TestEmitUpdateKeepsNonTranscriptUpdatesDuringSessionLoad(t *testing.T) {
	b := restoredBridge(t, ad1aRestored...)

	emitAll(t, b, true, ad1aReplayAgent, ad1aAvailableCmds)

	kinds := historyKinds(t, b)
	if len(kinds) != len(ad1aRestored)+1 || kinds[len(kinds)-1] != "available_commands_update" {
		t.Fatalf("history=%v, want restored history plus available_commands_update", kinds)
	}
}

func TestEmitUpdateRecordsLiveOutputMatchingRestoredText(t *testing.T) {
	b := restoredBridge(t, ad1aRestored...)

	emitAll(t, b, true, ad1aReplayAgent, ad1aAnswer2)
	emitAll(t, b, false, `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"もう一度"}}`, ad1aAnswer2)

	kinds := historyKinds(t, b)
	if len(kinds) != len(ad1aRestored)+2 {
		t.Fatalf("history=%d, want restored history plus the new prompt and its answer", len(kinds))
	}
	if kinds[len(kinds)-1] != "agent_message_chunk:速かったのは、2つの検索を同時に走らせたからです。" {
		t.Fatalf("last history entry=%q, want live answer", kinds[len(kinds)-1])
	}
}

func TestEmitUpdateDropsReplayDeliveredAfterNewPrompt(t *testing.T) {
	b := restoredBridge(t, ad1aRestored...)

	emitAll(t, b, false, `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"続けて"}}`)
	emitAll(t, b, true, ad1aReplayAgent, ad1aReplaySearch)
	emitAll(t, b, false, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"新しい回答"}}`)

	kinds := historyKinds(t, b)
	got := strings.Join(kinds[len(ad1aRestored):], "\n")
	if want := "user_message_chunk:続けて\nagent_message_chunk:新しい回答"; got != want {
		t.Fatalf("history after restored:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitUpdateRecordsReplayWithoutRestoredHistory(t *testing.T) {
	b := restoredBridge(t)

	emitAll(t, b, true, ad1aUser1, ad1aReplayAgent)

	if got := strings.Join(historyKinds(t, b), "\n"); got != "user_message_chunk:今日の社会情勢を教えてください。\nagent_message_chunk:最新の情報を確認するため、Web検索ツールを読み込みます。" {
		t.Fatalf("history=%s, want replay recorded", got)
	}
}
