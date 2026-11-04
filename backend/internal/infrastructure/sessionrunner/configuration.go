package sessionrunner

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

const configurationPrefix = "agentapi-session-configuration-"

func configurationName(id string) string { return configurationPrefix + hashName(id) }
func (s *Store) CreateConfiguration(ctx context.Context, c *core.Configuration) error {
	c.Revision = 1
	c.UpdatedAt = s.now()
	return s.createWithLabels(ctx, configurationName(c.SessionID), "configuration", "", map[string]string{
		"agentapi.proxy/session-id": c.SessionID,
	}, c)
}
func (s *Store) GetConfiguration(ctx context.Context, id string) (*core.Configuration, error) {
	var c core.Configuration
	record, err := s.get(ctx, configurationName(id), &c)
	c.Version = record.Version
	return &c, err
}

// SetConfigurationOwnerReference makes the configuration follow the lifecycle
// of the canonical per-session Kubernetes resource.
func (s *Store) SetConfigurationOwnerReference(ctx context.Context, id string, owner core.OwnerReference) error {
	if owner.APIVersion == "" || owner.Kind == "" || owner.Name == "" || owner.UID == "" {
		return errors.New("configuration owner reference is incomplete")
	}
	for attempts := 0; attempts < 5; attempts++ {
		record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, configurationName(id))
		if errors.Is(err, kvstore.ErrNotFound) {
			return core.ErrNotFound
		}
		if err != nil {
			return err
		}
		var doc secretDocument
		if err := json.Unmarshal(record.Value, &doc); err != nil {
			return err
		}
		doc.Metadata.OwnerReferences = []core.OwnerReference{owner}
		record.Value, err = json.Marshal(doc)
		if err != nil {
			return err
		}
		if _, err = s.kv.Update(ctx, record); errors.Is(err, kvstore.ErrConflict) {
			continue
		}
		return err
	}
	return core.ErrConflict
}

// ListConfigurationRefs returns lifecycle metadata for every saved session
// configuration. Only the session ID and update time are decoded from the
// payload; the startup input and settings are never returned to the caller.
func (s *Store) ListConfigurationRefs(ctx context.Context) ([]core.ConfigurationRef, error) {
	items, err := s.kv.List(ctx, kvstore.Query{
		Kind:          kvstore.KindSecret,
		Namespace:     s.namespace,
		KeyPrefix:     configurationPrefix,
		LabelSelector: labelResource + "=configuration",
	})
	if err != nil {
		return nil, err
	}
	refs := make([]core.ConfigurationRef, 0, len(items))
	for i := range items {
		var doc secretDocument
		if err := json.Unmarshal(items[i].Value, &doc); err != nil {
			continue
		}
		if doc.Metadata.Labels[labelResource] != "configuration" {
			continue
		}
		var meta struct {
			SessionID string    `json:"session_id"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		if err := json.Unmarshal(doc.Data[dataKey], &meta); err != nil || meta.SessionID == "" {
			continue
		}
		refs = append(refs, core.ConfigurationRef{
			SessionID: meta.SessionID,
			UpdatedAt: meta.UpdatedAt,
			HasOwner:  len(doc.Metadata.OwnerReferences) > 0,
		})
	}
	return refs, nil
}

// DeleteConfiguration removes saved startup input after the session lifecycle
// ends. This is required for non-Kubernetes KV backends where ownerReferences
// are metadata only and no garbage collector observes them.
func (s *Store) DeleteConfiguration(ctx context.Context, id string) error {
	return s.delete(ctx, configurationName(id))
}

// Compare-and-swap, deliberately without retrying a stale configuration.
func (s *Store) SaveConfiguration(ctx context.Context, c *core.Configuration) error {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, configurationName(c.SessionID))
	if err != nil {
		return err
	}
	if record.Version != c.Version {
		return core.ErrConflict
	}
	var doc secretDocument
	if err := json.Unmarshal(record.Value, &doc); err != nil {
		return err
	}
	c.UpdatedAt = s.now()
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	doc.Data[dataKey] = raw
	record.Value, err = json.Marshal(doc)
	if err != nil {
		return err
	}
	saved, err := s.kv.Update(ctx, record)
	if errors.Is(err, kvstore.ErrConflict) {
		return core.ErrConflict
	}
	if err == nil {
		c.Version = saved.Version
	}
	return err
}
func (s *Store) UpdateProvisionSettings(ctx context.Context, id string, settings []byte) error {
	var a core.Allocation
	record, err := s.get(ctx, allocationName(id), &a)
	if err != nil {
		return err
	}
	a.ProvisionSettings = settings
	a.UpdatedAt = s.now()
	var doc secretDocument
	if err := json.Unmarshal(record.Value, &doc); err != nil {
		return err
	}
	doc.Data[dataKey], err = json.Marshal(&a)
	if err != nil {
		return err
	}
	record.Value, err = json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = s.kv.Update(ctx, record)
	return err
}
