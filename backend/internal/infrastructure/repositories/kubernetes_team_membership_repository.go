package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelTeamMembership         = "agentapi.proxy/team-membership"
	TeamMembershipSecretPrefix  = "agentapi-team-membership-"
	teamMembershipDataKey       = "snapshot.json"
	teamMembershipRateLimit     = time.Minute
	teamMembershipLeaseDuration = 2 * time.Minute
)

var (
	ErrTeamSyncRateLimited = ports.ErrTeamSyncRateLimited
	ErrTeamSyncInProgress  = ports.ErrTeamSyncInProgress
	ErrTeamSyncConflict    = ports.ErrTeamSyncConflict
)

type KubernetesTeamMembershipRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesTeamMembershipRepository(client kubernetes.Interface, namespace string) *KubernetesTeamMembershipRepository {
	return &KubernetesTeamMembershipRepository{client: client, namespace: namespace}
}

func (r *KubernetesTeamMembershipRepository) Get(ctx context.Context, teamPrincipalID string) (*entities.TeamMembershipSnapshot, bool, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(teamPrincipalID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get team membership snapshot: %w", err)
	}
	snapshot, err := decodeTeamMembership(secret)
	return snapshot, err == nil, err
}

func (r *KubernetesTeamMembershipRepository) List(ctx context.Context) ([]*entities.TeamMembershipSnapshot, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: LabelTeamMembership + "=true"})
	if err != nil {
		return nil, fmt.Errorf("list team membership snapshots: %w", err)
	}
	result := make([]*entities.TeamMembershipSnapshot, 0, len(secrets.Items))
	for i := range secrets.Items {
		snapshot, decodeErr := decodeTeamMembership(&secrets.Items[i])
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TeamPrincipalID < result[j].TeamPrincipalID })
	return result, nil
}

func (r *KubernetesTeamMembershipRepository) AcquireSync(ctx context.Context, teamPrincipalID, operationID string, now time.Time) (*entities.TeamMembershipSnapshot, error) {
	now = now.UTC()
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(teamPrincipalID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		snapshot := &entities.TeamMembershipSnapshot{SchemaVersion: 1, TeamPrincipalID: teamPrincipalID, ExternalMembers: []entities.ExternalTeamMember{}, Members: []entities.TeamMember{}, LastStartedAt: now, LeaseUntil: now.Add(teamMembershipLeaseDuration), OperationID: operationID}
		created, createErr := r.client.CoreV1().Secrets(r.namespace).Create(ctx, r.toSecret(snapshot), metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			return nil, ErrTeamSyncConflict
		}
		if createErr != nil {
			return nil, fmt.Errorf("create team membership lease: %w", createErr)
		}
		return decodeTeamMembership(created)
	}
	if err != nil {
		return nil, fmt.Errorf("get team membership lease: %w", err)
	}
	snapshot, err := decodeTeamMembership(secret)
	if err != nil {
		return nil, err
	}
	if snapshot.OperationID != "" && snapshot.LeaseUntil.After(now) {
		return nil, ErrTeamSyncInProgress
	}
	if !snapshot.LastStartedAt.IsZero() && now.Sub(snapshot.LastStartedAt) < teamMembershipRateLimit {
		return nil, ErrTeamSyncRateLimited
	}
	snapshot.LastStartedAt = now
	snapshot.LeaseUntil = now.Add(teamMembershipLeaseDuration)
	snapshot.OperationID = operationID
	if err := updateTeamMembershipSecret(ctx, r.client, r.namespace, secret, snapshot); err != nil {
		if apierrors.IsConflict(err) {
			return nil, ErrTeamSyncConflict
		}
		return nil, err
	}
	return snapshot, nil
}

func (r *KubernetesTeamMembershipRepository) Replace(ctx context.Context, snapshot *entities.TeamMembershipSnapshot, operationID string) error {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(snapshot.TeamPrincipalID), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get team membership before replace: %w", err)
	}
	current, err := decodeTeamMembership(secret)
	if err != nil {
		return err
	}
	if current.OperationID != operationID {
		return ErrTeamSyncConflict
	}
	snapshot.SchemaVersion = 1
	snapshot.Generation = current.Generation + 1
	snapshot.LastStartedAt = current.LastStartedAt
	snapshot.LeaseUntil = time.Time{}
	snapshot.OperationID = ""
	if err := updateTeamMembershipSecret(ctx, r.client, r.namespace, secret, snapshot); err != nil {
		if apierrors.IsConflict(err) {
			return ErrTeamSyncConflict
		}
		return err
	}
	return nil
}

func (r *KubernetesTeamMembershipRepository) ReleaseSync(ctx context.Context, teamPrincipalID, operationID string) error {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(teamPrincipalID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	snapshot, err := decodeTeamMembership(secret)
	if err != nil || snapshot.OperationID != operationID {
		return err
	}
	snapshot.OperationID = ""
	snapshot.LeaseUntil = time.Time{}
	return updateTeamMembershipSecret(ctx, r.client, r.namespace, secret, snapshot)
}

func (r *KubernetesTeamMembershipRepository) Delete(ctx context.Context, teamPrincipalID string) error {
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, r.secretName(teamPrincipalID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *KubernetesTeamMembershipRepository) secretName(teamPrincipalID string) string {
	return TeamMembershipSecretPrefix + teamPrincipalID
}

func (r *KubernetesTeamMembershipRepository) toSecret(snapshot *entities.TeamMembershipSnapshot) *corev1.Secret {
	data, _ := json.Marshal(snapshot)
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.secretName(snapshot.TeamPrincipalID), Namespace: r.namespace, Labels: map[string]string{LabelTeamMembership: "true"}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{teamMembershipDataKey: data}}
}

func decodeTeamMembership(secret *corev1.Secret) (*entities.TeamMembershipSnapshot, error) {
	var snapshot entities.TeamMembershipSnapshot
	if err := json.Unmarshal(secret.Data[teamMembershipDataKey], &snapshot); err != nil {
		return nil, fmt.Errorf("decode team membership snapshot %s: %w", secret.Name, err)
	}
	if snapshot.ExternalMembers == nil {
		snapshot.ExternalMembers = []entities.ExternalTeamMember{}
	}
	if snapshot.Members == nil {
		snapshot.Members = []entities.TeamMember{}
	}
	return &snapshot, nil
}

func updateTeamMembershipSecret(ctx context.Context, client kubernetes.Interface, namespace string, secret *corev1.Secret, snapshot *entities.TeamMembershipSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	secret.Data[teamMembershipDataKey] = data
	_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}
