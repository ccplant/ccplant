package repositories

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
)

func TestKubernetesAPITokenRepository_RandomGeneratedIDCRUD(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	id, err := apitoken.GenerateTokenID()
	if err != nil {
		t.Fatalf("GenerateTokenID: %v", err)
	}
	if !strings.HasPrefix(id, apitoken.TokenIDPrefix) {
		t.Fatalf("generated id %q missing tok_ prefix", id)
	}
	name := repo.secretName(id)
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		t.Fatalf("generated id secret name %q invalid DNS subdomain: %v", name, errs)
	}

	tok := newTestPersonalToken(id, "apt_randomsecretxxx")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID() != id {
		t.Errorf("id = %q want %q", got.ID(), id)
	}
}
