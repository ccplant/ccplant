// Package apitest provides authenticated, bounded HTTP requests for black-box API tests.
package apitest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	BaseURL             string
	HTTP                *http.Client
	authType, authValue string
}

func FromEnv() (*Client, error) {
	base := os.Getenv("E2E_TARGET_API_URL")
	if base == "" {
		return nil, fmt.Errorf("E2E_TARGET_API_URL is required")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid target API URL")
	}
	kind, value := os.Getenv("E2E_AUTH_TYPE"), os.Getenv("E2E_AUTH_VALUE")
	if kind != "" && kind != "none" && kind != "bearer" && kind != "api-key" {
		return nil, fmt.Errorf("unsupported authentication type")
	}
	if (kind == "bearer" || kind == "api-key") && value == "" {
		return nil, fmt.Errorf("authentication value is required")
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), authType: kind, authValue: value, HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Do never includes response bodies, URLs or credential values in errors.
func (c *Client) Do(ctx context.Context, method, path string, input, output any, want ...int) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode request")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("invalid request")
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch c.authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.authValue)
	case "api-key":
		req.Header.Set("X-API-Key", c.authValue)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed (transport or timeout)", method)
	}
	defer res.Body.Close()
	ok := false
	for _, v := range want {
		if res.StatusCode == v {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%s returned HTTP %d; expected %v", method, res.StatusCode, want)
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(output); err != nil {
			return fmt.Errorf("invalid JSON response")
		}
	}
	return nil
}

func UniqueName() string {
	var b [8]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic("cannot generate test name")
	}
	return "e2e-" + hex.EncodeToString(b[:])
}
