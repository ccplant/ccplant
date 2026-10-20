package worker

import "testing"

func TestSafeTest(t *testing.T) {
	for _, v := range []string{"TestHealth", "TestSession_Chat"} {
		if !safeTest(v) {
			t.Fatalf("expected safe: %s", v)
		}
	}
	for _, v := range []string{"TestHealth$|TestOther", "../TestHealth", ""} {
		if safeTest(v) {
			t.Fatalf("expected unsafe: %s", v)
		}
	}
}
func TestParameters(t *testing.T) {
	got := parameters(map[string]any{"repository-name": "ccplant"})
	if len(got) != 1 || got[0] != "E2E_PARAM_REPOSITORY_NAME=ccplant" {
		t.Fatalf("parameters=%v", got)
	}
}
