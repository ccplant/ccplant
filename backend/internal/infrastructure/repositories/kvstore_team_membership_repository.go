package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelTeamMembership         = "agentapi.proxy/team-membership"
	LabelTeamMembershipMember   = "agentapi.proxy/team-membership-member-"
	TeamMembershipRecordPrefix  = "agentapi-team-membership-"
	teamMembershipDataKey       = "snapshot.json"
	teamMembershipRateLimit     = time.Minute
	teamMembershipLeaseDuration = 2 * time.Minute
)

type teamMembershipDocument struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string][]byte `json:"data"`
}

// KVStoreTeamMembershipRepository stores membership documents directly via
// the application KV boundary. Kubernetes is only one possible Store backend.
type KVStoreTeamMembershipRepository struct {
	store     kvstore.Store
	namespace string
}

func NewKVStoreTeamMembershipRepository(store kvstore.Store, namespace string) *KVStoreTeamMembershipRepository {
	return &KVStoreTeamMembershipRepository{store: store, namespace: namespace}
}

func (r *KVStoreTeamMembershipRepository) Get(ctx context.Context, teamPrincipalID string) (*entities.TeamMembershipSnapshot, bool, error) {
	record, err := r.store.Get(ctx, kvstore.KindSecret, r.namespace, r.recordName(teamPrincipalID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get team membership snapshot: %w", err)
	}
	snapshot, err := decodeTeamMembershipRecord(record)
	return snapshot, err == nil, err
}

func (r *KVStoreTeamMembershipRepository) List(ctx context.Context) ([]*entities.TeamMembershipSnapshot, error) {
	return r.list(ctx, LabelTeamMembership+"=true")
}

// ListForPrincipal returns only snapshots containing the requested principal.
// Membership labels are hashed because principal IDs are not guaranteed to be
// valid Kubernetes label names and should not be exposed in metadata.
func (r *KVStoreTeamMembershipRepository) ListForPrincipal(ctx context.Context, principalID string) ([]*entities.TeamMembershipSnapshot, error) {
	selector := LabelTeamMembership + "=true," + membershipMemberLabel(principalID) + "=true"
	return r.list(ctx, selector)
}

func (r *KVStoreTeamMembershipRepository) list(ctx context.Context, selector string) ([]*entities.TeamMembershipSnapshot, error) {
	records, err := r.store.List(ctx, kvstore.Query{Kind: kvstore.KindSecret, Namespace: r.namespace, LabelSelector: selector, KeyPrefix: TeamMembershipRecordPrefix})
	if err != nil {
		return nil, fmt.Errorf("list team membership snapshots: %w", err)
	}
	result := make([]*entities.TeamMembershipSnapshot, 0, len(records))
	for _, record := range records {
		snapshot, decodeErr := decodeTeamMembershipRecord(record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TeamPrincipalID < result[j].TeamPrincipalID })
	return result, nil
}

func (r *KVStoreTeamMembershipRepository) AcquireSync(ctx context.Context, teamPrincipalID, operationID string, now time.Time) (*entities.TeamMembershipSnapshot, error) {
	now = now.UTC()
	record, err := r.store.Get(ctx, kvstore.KindSecret, r.namespace, r.recordName(teamPrincipalID))
	if errors.Is(err, kvstore.ErrNotFound) {
		snapshot := &entities.TeamMembershipSnapshot{SchemaVersion: 1, TeamPrincipalID: teamPrincipalID, ExternalMembers: []entities.ExternalTeamMember{}, Members: []entities.TeamMember{}, LastStartedAt: now, LeaseUntil: now.Add(teamMembershipLeaseDuration), OperationID: operationID}
		created, createErr := r.store.Create(ctx, r.newRecord(snapshot))
		if errors.Is(createErr, kvstore.ErrConflict) {
			return nil, ports.ErrTeamSyncConflict
		}
		if createErr != nil {
			return nil, fmt.Errorf("create team membership lease: %w", createErr)
		}
		return decodeTeamMembershipRecord(created)
	}
	if err != nil {
		return nil, fmt.Errorf("get team membership lease: %w", err)
	}
	snapshot, err := decodeTeamMembershipRecord(record)
	if err != nil {
		return nil, err
	}
	if snapshot.OperationID != "" && snapshot.LeaseUntil.After(now) {
		return nil, ports.ErrTeamSyncInProgress
	}
	if !snapshot.LastStartedAt.IsZero() && now.Sub(snapshot.LastStartedAt) < teamMembershipRateLimit {
		return nil, ports.ErrTeamSyncRateLimited
	}
	snapshot.LastStartedAt = now
	snapshot.LeaseUntil = now.Add(teamMembershipLeaseDuration)
	snapshot.OperationID = operationID
	if err := r.update(ctx, record, snapshot); err != nil {
		if errors.Is(err, kvstore.ErrConflict) {
			return nil, ports.ErrTeamSyncConflict
		}
		return nil, err
	}
	return snapshot, nil
}

func (r *KVStoreTeamMembershipRepository) Replace(ctx context.Context, snapshot *entities.TeamMembershipSnapshot, operationID string) error {
	record, err := r.store.Get(ctx, kvstore.KindSecret, r.namespace, r.recordName(snapshot.TeamPrincipalID))
	if err != nil {
		return fmt.Errorf("get team membership before replace: %w", err)
	}
	current, err := decodeTeamMembershipRecord(record)
	if err != nil {
		return err
	}
	if current.OperationID != operationID {
		return ports.ErrTeamSyncConflict
	}
	snapshot.SchemaVersion = 1
	snapshot.Generation = current.Generation + 1
	snapshot.LastStartedAt = current.LastStartedAt
	snapshot.LeaseUntil = time.Time{}
	snapshot.OperationID = ""
	if err := r.update(ctx, record, snapshot); err != nil {
		if errors.Is(err, kvstore.ErrConflict) {
			return ports.ErrTeamSyncConflict
		}
		return err
	}
	return nil
}

func (r *KVStoreTeamMembershipRepository) ReleaseSync(ctx context.Context, teamPrincipalID, operationID string) error {
	record, err := r.store.Get(ctx, kvstore.KindSecret, r.namespace, r.recordName(teamPrincipalID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	snapshot, err := decodeTeamMembershipRecord(record)
	if err != nil || snapshot.OperationID != operationID {
		return err
	}
	snapshot.OperationID = ""
	snapshot.LeaseUntil = time.Time{}
	return r.update(ctx, record, snapshot)
}

func (r *KVStoreTeamMembershipRepository) Delete(ctx context.Context, teamPrincipalID string) error {
	record, err := r.store.Get(ctx, kvstore.KindSecret, r.namespace, r.recordName(teamPrincipalID))
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.store.Delete(ctx, record.Kind, record.Namespace, record.Key, record.Version)
}

func (r *KVStoreTeamMembershipRepository) recordName(teamPrincipalID string) string {
	return TeamMembershipRecordPrefix + teamPrincipalID
}

func (r *KVStoreTeamMembershipRepository) newRecord(snapshot *entities.TeamMembershipSnapshot) kvstore.Record {
	labels := membershipLabels(snapshot)
	return kvstore.Record{Kind: kvstore.KindSecret, Namespace: r.namespace, Key: r.recordName(snapshot.TeamPrincipalID), Labels: labels, Value: encodeTeamMembershipDocument(r.namespace, r.recordName(snapshot.TeamPrincipalID), labels, snapshot)}
}

func (r *KVStoreTeamMembershipRepository) update(ctx context.Context, record kvstore.Record, snapshot *entities.TeamMembershipSnapshot) error {
	record.Labels = membershipLabels(snapshot)
	record.Value = encodeTeamMembershipDocument(record.Namespace, record.Key, record.Labels, snapshot)
	_, err := r.store.Update(ctx, record)
	return err
}

func membershipLabels(snapshot *entities.TeamMembershipSnapshot) map[string]string {
	labels := map[string]string{LabelTeamMembership: "true", LabelTeamID: snapshot.TeamPrincipalID}
	for _, member := range snapshot.Members {
		if principalID := strings.TrimSpace(member.PrincipalID); principalID != "" {
			labels[membershipMemberLabel(principalID)] = "true"
		}
	}
	return labels
}

func membershipMemberLabel(principalID string) string {
	return LabelTeamMembershipMember + services.HashLabelValue(principalID)
}

func encodeTeamMembershipDocument(namespace, name string, labels map[string]string, snapshot *entities.TeamMembershipSnapshot) []byte {
	snapshotData, _ := json.Marshal(snapshot)
	document := teamMembershipDocument{APIVersion: "v1", Kind: "Secret", Type: "Opaque", Data: map[string][]byte{teamMembershipDataKey: snapshotData}}
	document.Metadata.Name = name
	document.Metadata.Namespace = namespace
	document.Metadata.Labels = labels
	data, _ := json.Marshal(document)
	return data
}

func decodeTeamMembershipRecord(record kvstore.Record) (*entities.TeamMembershipSnapshot, error) {
	var document teamMembershipDocument
	if err := json.Unmarshal(record.Value, &document); err != nil {
		return nil, fmt.Errorf("decode team membership document %s: %w", record.Key, err)
	}
	var snapshot entities.TeamMembershipSnapshot
	if err := json.Unmarshal(document.Data[teamMembershipDataKey], &snapshot); err != nil {
		return nil, fmt.Errorf("decode team membership snapshot %s: %w", record.Key, err)
	}
	if snapshot.ExternalMembers == nil {
		snapshot.ExternalMembers = []entities.ExternalTeamMember{}
	}
	if snapshot.Members == nil {
		snapshot.Members = []entities.TeamMember{}
	}
	return &snapshot, nil
}
