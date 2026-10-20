package catalog

import (
	"os/exec"
	"strings"
	"testing"
)

// Compile and enumerate without executing network scenarios or requiring credentials.
func TestAPICatalogMatchesTests(t *testing.T) {
	c, err := Load("../../scenarios/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-tags=e2e", "-list", "^Test", "./scenarios/api")
	cmd.Dir = "../.."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list scenarios: %v\n%s", err, output)
	}
	tests := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Test") {
			tests[line] = false
		}
	}
	for _, scenario := range c.Scenarios {
		if scenario.Kind != "go-test" {
			continue
		}
		if _, ok := tests[scenario.Test]; !ok {
			t.Errorf("catalog test %s not found", scenario.Test)
		}
		if tests[scenario.Test] {
			t.Errorf("duplicate catalog test %s", scenario.Test)
		}
		tests[scenario.Test] = true
	}
	for name, registered := range tests {
		if !registered {
			t.Errorf("test %s is not registered in catalog", name)
		}
	}
}
