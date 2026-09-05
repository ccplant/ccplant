package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type LocalUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role"`
	Status      string `json:"status"`
}
type CreateLocalUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`
}
type CreateLocalUserTokenRequest struct {
	Name      string `json:"name"`
	ExpiresIn string `json:"expires_in,omitempty"`
}
type LocalUserToken struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	TokenPrefix string  `json:"token_prefix"`
}
type CreateLocalUserTokenResponse struct {
	Token          LocalUserToken `json:"token"`
	PlaintextToken string         `json:"plaintext_token"`
}
type LocalUserTokenList struct {
	Items []LocalUserToken `json:"items"`
}

func (c *Client) localUserRequest(ctx context.Context, method, path string, input, output any, expected int) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.applyMiddlewares(req); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expected {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, b)
	}
	if output != nil {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	return nil
}

func (c *Client) CreateLocalUser(ctx context.Context, in *CreateLocalUserRequest) (*LocalUser, error) {
	var out LocalUser
	err := c.localUserRequest(ctx, http.MethodPost, "/admin/users", in, &out, http.StatusCreated)
	return &out, err
}
func (c *Client) GetLocalUser(ctx context.Context, id string) (*LocalUser, error) {
	var out LocalUser
	err := c.localUserRequest(ctx, http.MethodGet, "/admin/users/"+url.PathEscape(id), nil, &out, http.StatusOK)
	return &out, err
}
func (c *Client) CreateLocalUserToken(ctx context.Context, id string, in *CreateLocalUserTokenRequest) (*CreateLocalUserTokenResponse, error) {
	var out CreateLocalUserTokenResponse
	err := c.localUserRequest(ctx, http.MethodPost, "/admin/users/"+url.PathEscape(id)+"/api-tokens", in, &out, http.StatusCreated)
	return &out, err
}
func (c *Client) ListLocalUserTokens(ctx context.Context, id string) (*LocalUserTokenList, error) {
	var out LocalUserTokenList
	err := c.localUserRequest(ctx, http.MethodGet, "/admin/users/"+url.PathEscape(id)+"/api-tokens", nil, &out, http.StatusOK)
	return &out, err
}
func (c *Client) DeleteLocalUserToken(ctx context.Context, id, tokenID string) error {
	return c.localUserRequest(ctx, http.MethodDelete, "/admin/users/"+url.PathEscape(id)+"/api-tokens/"+url.PathEscape(tokenID), nil, nil, http.StatusNoContent)
}
