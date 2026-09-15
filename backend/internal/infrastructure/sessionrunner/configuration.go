package sessionrunner

import (
	"context"
	"encoding/json"
	"errors"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func configurationName(id string) string { return "agentapi-session-configuration-" + hashName(id) }
func (s *Store) CreateConfiguration(ctx context.Context, c *core.Configuration) error {
	c.Revision = 1
	c.UpdatedAt = s.now()
	return s.create(ctx, configurationName(c.SessionID), "configuration", "", c)
}
func (s *Store) GetConfiguration(ctx context.Context, id string) (*core.Configuration, error) {
	var c core.Configuration
	record, err := s.get(ctx, configurationName(id), &c)
	c.Version = record.Version
	return &c, err
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
