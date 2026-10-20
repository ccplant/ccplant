package catalog

import (
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"testing"
)

func TestValidate(t *testing.T) {
	c := &domain.Catalog{Categories: []domain.Category{{ID: "smoke", Description: "スモークテスト"}}, Scenarios: []domain.Scenario{{ID: "api.smoke.health", Category: "smoke", Description: "ヘルスチェック", Kind: "go-test", Package: "./scenarios/api", Test: "TestHealth", Timeout: "30s"}}}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.Scenarios[0].Category = "missing"
	if err := Validate(c); err == nil {
		t.Fatal("expected validation error")
	}
}
