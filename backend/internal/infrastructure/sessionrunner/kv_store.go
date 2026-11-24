package sessionrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

const (
	labelResource    = "agentapi.proxy/session-runner-resource"
	labelPoolHash    = "agentapi.proxy/session-runner-pool-hash"
	labelManagerHash = "agentapi.proxy/session-runner-manager-hash"
	labelStatus      = "agentapi.proxy/session-runner-status"
	labelDind        = "agentapi.proxy/session-runner-dind"
	dataKey          = "resource.json"
	managerPrefix    = "agentapi-session-manager-"
	logicalPrefix    = "agentapi-session-logical-pool-"
	supplierPrefix   = "agentapi-session-pool-supplier-"
	bindingPrefix    = "agentapi-session-pool-binding-"
	runnerPrefix     = "agentapi-session-runner-"
	allocationPrefix = "agentapi-session-allocation-pool-"
)

type Store struct {
	kv        kvstore.Store
	namespace string
	now       func() time.Time

	metaRepairAttempted sync.Map
}

func NewStore(kv kvstore.Store, namespace string) *Store {
	return &Store{kv: kv, namespace: namespace, now: func() time.Time { return time.Now().UTC() }}
}

type secretDocument struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   documentMetadata  `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type documentMetadata struct {
	Name            string                `json:"name"`
	Namespace       string                `json:"namespace,omitempty"`
	Labels          map[string]string     `json:"labels,omitempty"`
	OwnerReferences []core.OwnerReference `json:"ownerReferences,omitempty"`
}

func hashName(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func managerName(id string) string       { return managerPrefix + hashName(id) }
func logicalPoolName(pool string) string { return logicalPrefix + hashName(pool) }
func poolSupplierName(managerID, pool string) string {
	return supplierPrefix + hashName(managerID+"\x00"+pool)
}
func bindingName(id string) string    { return bindingPrefix + hashName(id) }
func runnerName(id string) string     { return runnerPrefix + hashName(id) }
func allocationName(id string) string { return allocationPrefix + hashName(id) }

func resourcePrefix(resource string) string {
	switch resource {
	case "manager":
		return managerPrefix
	case "logical-pool":
		return logicalPrefix
	case "pool-supplier":
		return supplierPrefix
	case "binding":
		return bindingPrefix
	case "runner":
		return runnerPrefix
	case "allocation":
		return allocationPrefix
	case allocationSettingsResource:
		return allocationSettingsPrefix
	default:
		return ""
	}
}

func (s *Store) create(ctx context.Context, name, resource, pool string, value any) error {
	return s.createWithLabels(ctx, name, resource, pool, nil, value)
}

func (s *Store) createWithLabels(ctx context.Context, name, resource, pool string, additionalLabels map[string]string, value any) error {
	raw, err := s.encodeResource(ctx, value)
	if err != nil {
		return err
	}
	labels := map[string]string{labelResource: resource}
	if pool != "" {
		labels[labelPoolHash] = hashName(pool)
	}
	for key, value := range additionalLabels {
		labels[key] = value
	}
	addSessionRunnerQueryLabels(labels, value)
	document, err := json.Marshal(secretDocument{APIVersion: "v1", Kind: "Secret",
		Metadata: documentMetadata{Name: name, Namespace: s.namespace, Labels: labels},
		Type:     "Opaque", Data: map[string][]byte{dataKey: raw}})
	if err != nil {
		return err
	}
	_, err = s.kv.Create(ctx, kvstore.Record{Kind: kvstore.KindSecret, Namespace: s.namespace, Key: name, Labels: labels, Value: document})
	if errors.Is(err, kvstore.ErrConflict) {
		return core.ErrConflict
	}
	return err
}

func (s *Store) get(ctx context.Context, name string, out any) (kvstore.Record, error) {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
	if errors.Is(err, kvstore.ErrNotFound) {
		return kvstore.Record{}, core.ErrNotFound
	}
	if err != nil {
		return kvstore.Record{}, err
	}
	var document secretDocument
	if err := json.Unmarshal(record.Value, &document); err != nil {
		return kvstore.Record{}, err
	}
	if err := json.Unmarshal(document.Data[dataKey], out); err != nil {
		return kvstore.Record{}, err
	}
	return record, nil
}

func (s *Store) update(ctx context.Context, name string, value any) error {
	for attempts := 0; attempts < 5; attempts++ {
		record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
		if errors.Is(err, kvstore.ErrNotFound) {
			return core.ErrNotFound
		}
		if err != nil {
			return err
		}
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return err
		}
		raw, err := s.encodeResource(ctx, value)
		if err != nil {
			return err
		}
		document.Data[dataKey] = raw
		addSessionRunnerQueryLabels(document.Metadata.Labels, value)
		record.Labels = document.Metadata.Labels
		record.Value, err = json.Marshal(document)
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

func (s *Store) delete(ctx context.Context, name string) error {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.kv.Delete(ctx, kvstore.KindSecret, s.namespace, name, record.Version)
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if errors.Is(err, kvstore.ErrConflict) {
		return core.ErrConflict
	}
	return err
}

func (s *Store) list(ctx context.Context, resource string, decode func([]byte) error, pools ...string) error {
	return s.listWithSelector(ctx, resource, decode, poolSelector(pools...))
}

func poolSelector(pools ...string) string {
	if len(pools) > 0 && pools[0] != "" {
		return labelPoolHash + "=" + hashName(pools[0])
	}
	return ""
}

func (s *Store) listWithSelector(ctx context.Context, resource string, decode func([]byte) error, additionalSelector string) error {
	selector := labelResource + "=" + resource
	if additionalSelector != "" {
		selector += "," + additionalSelector
	}
	items, err := s.kv.List(ctx, kvstore.Query{
		Kind:          kvstore.KindSecret,
		Namespace:     s.namespace,
		KeyPrefix:     resourcePrefix(resource),
		LabelSelector: selector,
	})
	if err != nil {
		return err
	}
	for i := range items {
		var document secretDocument
		if err := json.Unmarshal(items[i].Value, &document); err != nil {
			continue
		}
		if document.Metadata.Labels[labelResource] != resource {
			continue
		}
		if err := decode(document.Data[dataKey]); err != nil {
			return err
		}
	}
	return nil
}

func addSessionRunnerQueryLabels(labels map[string]string, value any) {
	if labels == nil {
		return
	}
	switch resource := value.(type) {
	case *core.Runner:
		setManagerQueryLabel(labels, resource.ManagerID)
		labels[labelStatus] = string(resource.Status)
		labels[labelDind] = fmt.Sprintf("%t", capabilityEnabled(resource.Capabilities, "dind"))
	case *core.Allocation:
		setManagerQueryLabel(labels, resource.ManagerID)
		labels[labelStatus] = string(resource.Status)
		labels[labelDind] = fmt.Sprintf("%t", capabilityEnabled(resource.Requirements, "dind"))
	}
}

func setManagerQueryLabel(labels map[string]string, managerID string) {
	if managerID == "" {
		labels[labelManagerHash] = "unassigned"
		return
	}
	labels[labelManagerHash] = hashName(managerID)
}

func (s *Store) backfillManagerQueryLabel(ctx context.Context, name, managerID string) error {
	for range 5 {
		record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
		if errors.Is(err, kvstore.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return err
		}
		var owner struct {
			ManagerID string `json:"manager_id"`
		}
		if err := json.Unmarshal(document.Data[dataKey], &owner); err != nil {
			return err
		}
		if owner.ManagerID != managerID || document.Metadata.Labels[labelManagerHash] != "" {
			return nil
		}
		setManagerQueryLabel(document.Metadata.Labels, managerID)
		record.Labels = document.Metadata.Labels
		record.Value, err = json.Marshal(document)
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

func (s *Store) CreateManager(ctx context.Context, manager *core.Manager) error {
	now := s.now()
	if manager.ID == "" {
		manager.ID = uuid.NewString()
	}
	manager.CreatedAt, manager.UpdatedAt = now, now
	return s.create(ctx, managerName(manager.ID), "manager", "", manager)
}

func (s *Store) GetManager(ctx context.Context, id string) (*core.Manager, error) {
	var value core.Manager
	_, err := s.get(ctx, managerName(id), &value)
	return &value, err
}

func (s *Store) ListManagers(ctx context.Context) ([]*core.Manager, error) {
	var result []*core.Manager
	err := s.list(ctx, "manager", func(raw []byte) error {
		var value core.Manager
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
}

func (s *Store) UpdateManager(ctx context.Context, manager *core.Manager) error {
	current, err := s.GetManager(ctx, manager.ID)
	if err != nil {
		return err
	}
	manager.CreatedAt = current.CreatedAt
	if manager.ConnectionTokenHash == "" {
		manager.ConnectionTokenHash = current.ConnectionTokenHash
	}
	manager.UpdatedAt = s.now()
	return s.update(ctx, managerName(manager.ID), manager)
}

func (s *Store) TouchManagerHeartbeat(ctx context.Context, id string, at time.Time) error {
	var manager core.Manager
	return s.mutate(ctx, managerName(id), &manager, func() error {
		manager.LastHeartbeatAt = at
		return nil
	})
}

func (s *Store) DeleteManager(ctx context.Context, id string) error {
	return s.delete(ctx, managerName(id))
}

func (s *Store) CreateLogicalPool(ctx context.Context, pool *core.LogicalPool) error {
	now := s.now()
	pool.CreatedAt, pool.UpdatedAt = now, now
	return s.create(ctx, logicalPoolName(pool.Name), "logical-pool", pool.Name, pool)
}

func (s *Store) GetLogicalPool(ctx context.Context, name string) (*core.LogicalPool, error) {
	var value core.LogicalPool
	_, err := s.get(ctx, logicalPoolName(name), &value)
	return &value, err
}
