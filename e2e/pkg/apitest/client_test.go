package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialAndBasePath(t *testing.T) {
	for _, kind := range []string{"bearer", "api-key"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/user/info" {
					t.Errorf("base path not preserved")
				}
				header, want := "Authorization", "Bearer credential"
				if kind == "api-key" {
					header, want = "X-API-Key", "credential"
				}
				if r.Header.Get(header) != want {
					t.Error("authentication header missing")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"principal_id":"test"}`))
			}))
			defer server.Close()
			t.Setenv("E2E_TARGET_API_URL", server.URL+"/api/v1/")
			t.Setenv("E2E_AUTH_TYPE", kind)
			t.Setenv("E2E_AUTH_VALUE", "credential")
			c, err := FromEnv()
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]string
			if err := c.Do(context.Background(), "GET", "/user/info", nil, &out, 200); err != nil {
				t.Fatal(err)
			}
			if out["principal_id"] != "test" {
				t.Fatal("response not decoded")
			}
		})
	}
}
func TestNoRedirectOrErrorBodyLeak(t *testing.T) {
	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", other.URL)
		w.WriteHeader(302)
		w.Write([]byte("sensitive-response"))
	}))
	defer server.Close()
	t.Setenv("E2E_TARGET_API_URL", server.URL)
	t.Setenv("E2E_AUTH_TYPE", "bearer")
	t.Setenv("E2E_AUTH_VALUE", "credential")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), "GET", "/redirect", nil, nil, 200)
	if err == nil || reached {
		t.Fatal("redirect must not forward credentials")
	}
	if strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "sensitive-response") {
		t.Fatal("error leaks confidential content")
	}
}
