package esmcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const kvConnectionTTL = 75 * time.Second

// KVStore is a portable ESM/direct-runtime transport for deployments, such as
// Cloud Run, that have a shared application KV store but no Redis endpoint.
// Messages are immutable records; readers poll the shared store while honoring
// the same cursor contract as the Redis Streams implementation.
type KVStore struct {
	store     kvstore.Store
	namespace string
	now       func() time.Time
}

func NewKVStore(store kvstore.Store, namespace string) *KVStore {
	return &KVStore{store: store, namespace: namespace + "-esm-control", now: time.Now}
}

type kvEnvelope struct {
	StreamID string          `json:"stream_id,omitempty"`
	Expires  time.Time       `json:"expires"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func (s *KVStore) put(ctx context.Context, key, recordType, owner string, payload any, ttl time.Duration) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%020d-%s", s.now().UnixNano(), uuid.NewString())
	envelope := kvEnvelope{StreamID: id, Expires: s.now().Add(ttl), Payload: raw}
	value, err := encodeKVRecord(key, recordType, owner, envelope)
	if err != nil {
		return "", err
	}
	_, err = s.store.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: s.namespace, Key: key,
		Labels: map[string]string{"agentapi.proxy/type": recordType, "agentapi.proxy/owner": digest(owner)}, Value: value})
	return id, err
}

func (s *KVStore) upsert(ctx context.Context, key, recordType, owner string, payload any, ttl time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	envelope := kvEnvelope{Expires: s.now().Add(ttl), Payload: raw}
	value, err := encodeKVRecord(key, recordType, owner, envelope)
	if err != nil {
		return err
	}
	record := kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: s.namespace, Key: key,
		Labels: map[string]string{"agentapi.proxy/type": recordType, "agentapi.proxy/owner": digest(owner)}, Value: value}
	created, err := s.store.Create(ctx, record)
	if err == nil {
		_ = created
		return nil
	}
	if !errors.Is(err, kvstore.ErrConflict) {
		return err
	}
	current, err := s.store.Get(ctx, record.Kind, record.Namespace, record.Key)
	if err != nil {
		return err
	}
	record.Version = current.Version
	_, err = s.store.Update(ctx, record)
	return err
}

func (s *KVStore) TouchManager(ctx context.Context, managerID, instanceID string) error {
	if managerID == "" {
		return fmt.Errorf("manager id is required")
	}
	return s.upsert(ctx, "connection-"+digest(managerID), "esm-connection", managerID, instanceID, kvConnectionTTL)
}

func (s *KVStore) IsManagerConnected(ctx context.Context, managerID string) (bool, error) {
	record, err := s.store.Get(ctx, kvstore.KindConfigMap, s.namespace, "connection-"+digest(managerID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	env, err := decodeKVRecord(record.Value)
	if err != nil {
		return false, err
	}
	return s.now().Before(env.Expires), nil
}

func (s *KVStore) EnqueueCommand(ctx context.Context, managerID string, command core.Command) (string, error) {
	id, err := s.put(ctx, "command-"+uuid.NewString(), "esm-command", managerID, command, streamTTL)
	if err != nil {
		return "", err
	}
	if err := s.upsert(ctx, "request-owner-"+digest(command.ID), "esm-request-owner", command.ID, managerID, streamTTL); err != nil {
		return "", err
	}
	return id, nil
}

func (s *KVStore) ReadCommands(ctx context.Context, managerID, after string, wait time.Duration, count int64) ([]core.Command, error) {
	if record, err := s.store.Get(ctx, kvstore.KindConfigMap, s.namespace, "command-ack-"+digest(managerID)); err == nil {
		if env, decodeErr := decodeKVRecord(record.Value); decodeErr == nil {
			var ack string
			if json.Unmarshal(env.Payload, &ack) == nil && ack > after {
				after = ack
			}
		}
	} else if !errors.Is(err, kvstore.ErrNotFound) {
		return nil, err
	}
	deadline := s.now().Add(wait)
	for {
		var out []core.Command
		err := s.read(ctx, "esm-command", managerID, after, count, func(env kvEnvelope) error {
			var item core.Command
			if err := json.Unmarshal(env.Payload, &item); err != nil {
				return err
			}
			item.StreamID = env.StreamID
			out = append(out, item)
			return nil
		})
		if err != nil || len(out) > 0 || wait <= 0 || !s.now().Before(deadline) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *KVStore) AckCommand(ctx context.Context, managerID, streamID string) error {
	if streamID == "" {
		return nil
	}
	return s.upsert(ctx, "command-ack-"+digest(managerID), "esm-command-ack", managerID, streamID, streamTTL)
}

func (s *KVStore) AppendFrames(ctx context.Context, requestID string, frames []core.ResponseFrame) (string, error) {
	last := ""
	for _, frame := range frames {
		id, err := s.put(ctx, "frame-"+digest(requestID+"\x00"+frame.ID), "esm-frame", requestID, frame, streamTTL)
		if errors.Is(err, kvstore.ErrConflict) {
			continue
		}
		if err != nil {
			return "", err
		}
		last = id
	}
	return last, nil
}

func (s *KVStore) ReadFrames(ctx context.Context, requestID, after string, wait time.Duration, count int64) ([]core.ResponseFrame, error) {
	deadline := s.now().Add(wait)
	for {
		var out []core.ResponseFrame
		err := s.read(ctx, "esm-frame", requestID, after, count, func(env kvEnvelope) error {
			var item core.ResponseFrame
			if err := json.Unmarshal(env.Payload, &item); err != nil {
				return err
			}
			item.StreamID = env.StreamID
			out = append(out, item)
			return nil
		})
		if err != nil || len(out) > 0 || wait <= 0 || !s.now().Before(deadline) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *KVStore) RequestBelongsToManager(ctx context.Context, requestID, managerID string) (bool, error) {
	record, err := s.store.Get(ctx, kvstore.KindConfigMap, s.namespace, "request-owner-"+digest(requestID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	env, err := decodeKVRecord(record.Value)
	if err != nil {
		return false, err
	}
	var owner string
	if err := json.Unmarshal(env.Payload, &owner); err != nil {
		return false, err
	}
	return owner == managerID && s.now().Before(env.Expires), nil
}

func (s *KVStore) read(ctx context.Context, recordType, owner, after string, count int64, decode func(kvEnvelope) error) error {
	records, err := s.store.List(ctx, kvstore.Query{Kind: kvstore.KindConfigMap, Namespace: s.namespace,
		LabelSelector: "agentapi.proxy/type=" + recordType + ",agentapi.proxy/owner=" + digest(owner)})
	if err != nil {
		return err
	}
	type entry struct{ env kvEnvelope }
	entries := make([]entry, 0, len(records))
	for _, record := range records {
		env, decodeErr := decodeKVRecord(record.Value)
		if decodeErr != nil || !s.now().Before(env.Expires) || env.StreamID <= after {
			continue
		}
		entries = append(entries, entry{env})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].env.StreamID < entries[j].env.StreamID })
	if count <= 0 || count > 100 {
		count = 100
	}
	if int64(len(entries)) > count {
		entries = entries[:count]
	}
	for _, item := range entries {
		if err := decode(item.env); err != nil {
			return err
		}
	}
	return nil
}

func encodeKVRecord(key, recordType, owner string, envelope kvEnvelope) ([]byte, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key, Labels: map[string]string{
		"agentapi.proxy/type": recordType, "agentapi.proxy/owner": digest(owner),
	}}, Data: map[string]string{"record.json": string(raw)}})
}

func decodeKVRecord(raw []byte) (kvEnvelope, error) {
	var object corev1.ConfigMap
	if err := json.Unmarshal(raw, &object); err != nil {
		return kvEnvelope{}, err
	}
	var envelope kvEnvelope
	err := json.Unmarshal([]byte(object.Data["record.json"]), &envelope)
	return envelope, err
}
