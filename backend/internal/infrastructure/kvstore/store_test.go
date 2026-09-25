package kvstore

import (
	"errors"
	"testing"
)

func TestValidateListQueryRejectsUnboundedQuery(t *testing.T) {
	err := ValidateListQuery(Query{Kind: KindSecret, Namespace: "ns"})
	if !errors.Is(err, ErrUnboundedQuery) {
		t.Fatalf("ValidateListQuery() error = %v, want ErrUnboundedQuery", err)
	}
}

func TestValidateListQueryAcceptsLabelSelector(t *testing.T) {
	if err := ValidateListQuery(Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "app=test"}); err != nil {
		t.Fatalf("ValidateListQuery() error = %v", err)
	}
}

func TestValidateListQueryRejectsNegativeOnlySelector(t *testing.T) {
	err := ValidateListQuery(Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "app!=test"})
	if !errors.Is(err, ErrUnboundedQuery) {
		t.Fatalf("ValidateListQuery() error = %v, want ErrUnboundedQuery", err)
	}
}
