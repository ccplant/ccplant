package cmd

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

func TestParseCodexAuthChallenge(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	result := make(chan codexauth.Challenge, 1)
	errs := make(chan error, 1)
	parseCodexAuthChallenge(strings.NewReader("Open https://auth.openai.com/codex/device\nEnter ABCD-EFGH\n"), result, errs)
	select {
	case challenge := <-result:
		if challenge.UserCode != "ABCD-EFGH" || challenge.VerificationURI != "https://auth.openai.com/codex/device" {
			t.Fatalf("unexpected challenge: %#v", challenge)
		}
	case err := <-errs:
		t.Fatal(err)
	}
	if output := logs.String(); strings.Contains(output, "ABCD-EFGH") || strings.Contains(output, "auth.openai.com") {
		t.Fatalf("diagnostic logs leaked challenge data: %q", output)
	}
}

func TestParseCodexAuthChallengeRejectsOversizedOutput(t *testing.T) {
	result := make(chan codexauth.Challenge, 1)
	errs := make(chan error, 1)
	parseCodexAuthChallenge(strings.NewReader(strings.Repeat("x", 20<<10)), result, errs)
	select {
	case challenge := <-result:
		t.Fatalf("unexpected challenge: %#v", challenge)
	case err := <-errs:
		if err == nil {
			t.Fatal("expected scanner error")
		}
	}
}
