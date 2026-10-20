//go:build e2e

package api_test

import "testing"

func TestHealth(t *testing.T) {
	var body struct {
		Status string `json:"status"`
	}
	request(t, client(t), "GET", "/health", nil, &body, 200)
	if body.Status != "ok" {
		t.Fatal("health status must be ok")
	}
}
