package remotekv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

// Store keeps application persistence in the API process while presenting the
// same storage boundary to workers and session managers.
type Store struct {
	baseURL string
	token   string
	client  *http.Client
}

func New(baseURL, token string) *Store {
	return &Store{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: http.DefaultClient}
}

func (s *Store) Create(ctx context.Context, record kvstore.Record) (kvstore.Record, error) {
	var result kvstore.Record
	err := s.do(ctx, http.MethodPost, nil, record, &result)
	return result, err
}

func (s *Store) Update(ctx context.Context, record kvstore.Record) (kvstore.Record, error) {
	var result kvstore.Record
	err := s.do(ctx, http.MethodPut, nil, record, &result)
	return result, err
}

func (s *Store) Get(ctx context.Context, kind kvstore.Kind, namespace, key string) (kvstore.Record, error) {
	var result kvstore.Record
	err := s.do(ctx, http.MethodGet, recordQuery(kind, namespace, key), nil, &result)
	return result, err
}

func (s *Store) Delete(ctx context.Context, kind kvstore.Kind, namespace, key string, version int64) error {
	query := recordQuery(kind, namespace, key)
	query.Set("version", fmt.Sprint(version))
	return s.do(ctx, http.MethodDelete, query, nil, nil)
}

func (s *Store) List(ctx context.Context, query kvstore.Query) ([]kvstore.Record, error) {
	values := url.Values{"kind": {string(query.Kind)}, "namespace": {query.Namespace}, "label_selector": {query.LabelSelector}, "key_prefix": {query.KeyPrefix}}
	var result []kvstore.Record
	err := s.do(ctx, http.MethodGet, values, nil, &result)
	return result, err
}

func (s *Store) Close() error { return nil }

func recordQuery(kind kvstore.Kind, namespace, key string) url.Values {
	return url.Values{"kind": {string(kind)}, "namespace": {namespace}, "key": {key}}
}

func (s *Store) do(ctx context.Context, method string, query url.Values, input, output any) error {
	var body *bytes.Reader
	if input == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := s.baseURL + "/internal/application-kv"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return kvstore.ErrNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return kvstore.ErrConflict
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("application KV API returned %s", resp.Status)
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return err
	}
	return nil
}
