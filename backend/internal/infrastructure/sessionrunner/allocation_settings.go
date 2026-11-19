package sessionrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

const (
	allocationSettingsPrefix   = "agentapi-session-allocation-settings-"
	allocationSettingsResource = "allocation-settings"
	allocationMigrationBatch   = 8
)

func allocationSettingsName(id string) string { return allocationSettingsPrefix + hashName(id) }

func sessionMetaFromSettings(settings []byte) json.RawMessage {
	var probe struct {
		Session json.RawMessage `json:"session"`
	}
	if json.Unmarshal(settings, &probe) != nil || len(probe.Session) == 0 || string(probe.Session) == "null" {
		return nil
	}
	return probe.Session
}

func marshalAllocationRecord(allocation *core.Allocation) ([]byte, error) {
	record := *allocation
	record.ProvisionSettings = nil
	return json.Marshal(&record)
}

func (s *Store) encodeResource(ctx context.Context, value any) ([]byte, error) {
	allocation, ok := value.(*core.Allocation)
	if !ok {
		return json.Marshal(value)
	}
	if err := s.detachAllocationSettings(ctx, allocation); err != nil {
		return nil, err
	}
	return marshalAllocationRecord(allocation)
}

func (s *Store) detachAllocationSettings(ctx context.Context, allocation *core.Allocation) error {
	if len(allocation.ProvisionSettings) == 0 {
		return nil
	}
	if err := s.saveAllocationSettings(ctx, allocation.SessionID, allocation.ProvisionSettings); err != nil {
		return err
	}
	allocation.SessionMeta = sessionMetaFromSettings(allocation.ProvisionSettings)
	return nil
}

func (s *Store) attachAllocationSettings(ctx context.Context, allocation *core.Allocation) error {
	if len(allocation.ProvisionSettings) > 0 {
		return nil
	}
	settings, err := s.loadAllocationSettings(ctx, allocation.SessionID)
	if err != nil {
		return err
	}
	allocation.ProvisionSettings = settings
	return nil
}

func (s *Store) saveAllocationSettings(ctx context.Context, sessionID string, settings []byte) error {
	name := allocationSettingsName(sessionID)
	labels := map[string]string{labelResource: allocationSettingsResource}
	for range 5 {
		record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
		if errors.Is(err, kvstore.ErrNotFound) {
			document, marshalErr := json.Marshal(secretDocument{APIVersion: "v1", Kind: "Secret",
				Metadata: documentMetadata{Name: name, Namespace: s.namespace, Labels: labels},
				Type:     "Opaque", Data: map[string][]byte{dataKey: settings}})
			if marshalErr != nil {
				return marshalErr
			}
			_, err = s.kv.Create(ctx, kvstore.Record{Kind: kvstore.KindSecret, Namespace: s.namespace, Key: name, Labels: labels, Value: document})
			if errors.Is(err, kvstore.ErrConflict) {
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return err
		}
		if bytes.Equal(document.Data[dataKey], settings) {
			return nil
		}
		if document.Data == nil {
			document.Data = map[string][]byte{}
		}
		document.Data[dataKey] = settings
		if record.Value, err = json.Marshal(document); err != nil {
			return err
		}
		if _, err = s.kv.Update(ctx, record); errors.Is(err, kvstore.ErrConflict) {
			continue
		}
		return err
	}
	return core.ErrConflict
}

func (s *Store) loadAllocationSettings(ctx context.Context, sessionID string) ([]byte, error) {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, allocationSettingsName(sessionID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document secretDocument
	if err := json.Unmarshal(record.Value, &document); err != nil {
		return nil, err
	}
	return document.Data[dataKey], nil
}

func (s *Store) migrateManagerAllocations(ctx context.Context, allocations []*core.Allocation) error {
	budget := allocationMigrationBatch
	for _, allocation := range allocations {
		if budget == 0 {
			return nil
		}
		inline := len(allocation.ProvisionSettings) > 0
		if !inline && (len(allocation.SessionMeta) > 0 || !s.claimMetaRepair(allocation.SessionID)) {
			continue
		}
		budget--
		migrated, err := s.migrateAllocation(ctx, allocation.SessionID)
		if errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		*allocation = *migrated
	}
	return nil
}

func (s *Store) claimMetaRepair(sessionID string) bool {
	_, attempted := s.metaRepairAttempted.LoadOrStore(sessionID, struct{}{})
	return !attempted
}

func (s *Store) migrateAllocation(ctx context.Context, sessionID string) (*core.Allocation, error) {
	var allocation core.Allocation
	err := s.mutate(ctx, allocationName(sessionID), &allocation, func() error {
		if len(allocation.ProvisionSettings) > 0 || len(allocation.SessionMeta) > 0 {
			return nil
		}
		settings, err := s.loadAllocationSettings(ctx, sessionID)
		if err != nil {
			return err
		}
		allocation.SessionMeta = sessionMetaFromSettings(settings)
		return nil
	})
	if err != nil {
		return nil, err
	}
	allocation.ProvisionSettings = nil
	return &allocation, nil
}
