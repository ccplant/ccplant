package main

import "testing"

func TestResourceDefinitionsAreUniqueAndSafe(t *testing.T) {
	tables := map[string]bool{}
	keys := map[string]bool{}
	for _, resource := range resources {
		if !safeIdentifier(resource.table) {
			t.Errorf("unsafe table %q", resource.table)
		}
		if tables[resource.table] {
			t.Errorf("duplicate table %q", resource.table)
		}
		if keys[resource.key] {
			t.Errorf("duplicate key %q", resource.key)
		}
		tables[resource.table] = true
		keys[resource.key] = true
	}
	if len(resources) != 19 {
		t.Fatalf("resource count = %d, want 19", len(resources))
	}
}

func TestRequireNamespace(t *testing.T) {
	if err := requireNamespace("migration-verification-run-123"); err != nil {
		t.Fatal(err)
	}
	if err := requireNamespace("agentapi-ui-dev-v2"); err == nil {
		t.Fatal("production namespace was accepted")
	}
}
