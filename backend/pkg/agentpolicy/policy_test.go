package agentpolicy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAndAllows(t *testing.T) {
	p, err := Parse("version: v1\nresources:\n  sessions:\n    read: true\n    update: true\n  '*':\n    create: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Allows("sessions", "read") || p.Allows("sessions", "create") {
		t.Fatal("specific resource must override wildcard")
	}
	if !p.Allows("schedules", "create") || p.Allows("schedules", "read") {
		t.Fatal("wildcard actions not applied")
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	if _, err := Parse("version: v1\nresources:\n  sessions:\n    execute: true\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDenyAll(t *testing.T) {
	p := DenyAll()
	if p.Allows("sessions", "read") || p.Allows("notifications", "create") {
		t.Fatal("deny-all policy granted an operation")
	}
}

func TestRequestOperation(t *testing.T) {
	tests := []struct{ method, path, resource, action string }{
		{http.MethodPost, "/start", "sessions", "create"},
		{http.MethodGet, "/search", "sessions", "read"},
		{http.MethodPost, "/schedules", "schedules", "create"},
		{http.MethodPost, "/schedules/id/trigger", "schedules", "update"},
		{http.MethodDelete, "/webhooks/id", "webhooks", "delete"},
	}
	for _, tt := range tests {
		r, a, ok := RequestOperation(httptest.NewRequest(tt.method, tt.path, nil))
		if !ok || r != tt.resource || a != tt.action {
			t.Errorf("%s %s = %s/%s/%v", tt.method, tt.path, r, a, ok)
		}
	}
}
