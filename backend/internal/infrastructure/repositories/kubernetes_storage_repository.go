package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelStorageType      = "agentapi.proxy/type"
	LabelStorageTypeValue = "storage"
	LabelStorageScope     = "agentapi.proxy/scope"
	LabelStorageOwnerHash = "agentapi.proxy/owner-hash"
	LabelStorageTeamHash  = "agentapi.proxy/team-hash"

	AnnotationStorageOwnerID = "agentapi.proxy/owner-id"
	AnnotationStorageTeamID  = "agentapi.proxy/team-id"

	ConfigMapKeyStorage = "storage.json"
	StoragePrefix       = "agentapi-storage-"
)

type storageJSON struct {
	ID          string                          `json:"id"`
	Name        string                          `json:"name"`
	Description string                          `json:"description,omitempty"`
	OwnerID     string                          `json:"owner_id"`
	Scope       entities.ResourceScope          `json:"scope"`
	TeamID      string                          `json:"team_id,omitempty"`
	Kind        entities.StorageKind            `json:"kind"`
	Pool        string                          `json:"pool,omitempty"`
	Provider    string                          `json:"provider,omitempty"`
	Size        string                          `json:"size,omitempty"`
	State       entities.StorageState           `json:"state"`
	Revision    int64                           `json:"revision"`
	Files       map[string]entities.StorageFile `json:"files,omitempty"`
	CreatedAt   time.Time                       `json:"created_at"`
	UpdatedAt   time.Time                       `json:"updated_at"`
}

type KubernetesStorageRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesStorageRepository(client kubernetes.Interface, namespace string) *KubernetesStorageRepository {
	return &KubernetesStorageRepository{client: client, namespace: namespace}
}

func (r *KubernetesStorageRepository) Create(ctx context.Context, storage *entities.Storage) error {
	if err := storage.Validate(); err != nil {
		return fmt.Errorf("invalid storage: %w", err)
	}
	raw, err := r.marshal(storage)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.name(storage.ID()), Namespace: r.namespace,
			Labels: r.labels(storage), Annotations: r.annotations(storage),
		},
		Data: map[string]string{ConfigMapKeyStorage: string(raw)},
	}
	created, err := r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("storage already exists: %s", storage.ID())
	}
	if err != nil {
		return fmt.Errorf("create storage metadata: %w", err)
	}
	storage.SetVersion(created.ResourceVersion)
	return nil
}

func (r *KubernetesStorageRepository) GetByID(ctx context.Context, id string) (*entities.Storage, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.name(id), metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, entities.ErrStorageNotFound{ID: id}
	}
	if err != nil {
		return nil, fmt.Errorf("get storage metadata: %w", err)
	}
	return r.decode(cm)
}

func (r *KubernetesStorageRepository) List(ctx context.Context, filter portrepos.StorageFilter) ([]*entities.Storage, error) {
	selector := []string{LabelStorageType + "=" + LabelStorageTypeValue}
	if len(filter.TeamIDs) == 0 {
		if filter.Scope != "" {
			selector = append(selector, LabelStorageScope+"="+string(filter.Scope))
		}
		if filter.OwnerID != "" {
			selector = append(selector, LabelStorageOwnerHash+"="+hashID(filter.OwnerID))
		}
		if filter.TeamID != "" {
			selector = append(selector, LabelStorageTeamHash+"="+hashID(filter.TeamID))
		}
	} else if filter.Scope != "" {
		selector = append(selector, LabelStorageScope+"="+string(filter.Scope))
	}
	list, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: strings.Join(selector, ",")})
	if err != nil {
		return nil, fmt.Errorf("list storage metadata: %w", err)
	}
	teams := make(map[string]struct{}, len(filter.TeamIDs))
	for _, teamID := range filter.TeamIDs {
		teams[teamID] = struct{}{}
	}
	result := make([]*entities.Storage, 0, len(list.Items))
	for i := range list.Items {
		storage, decodeErr := r.decode(&list.Items[i])
		if decodeErr != nil {
			continue
		}
		if len(teams) > 0 {
			if _, ok := teams[storage.TeamID()]; !ok {
				continue
			}
		}
		result = append(result, storage)
	}
	return result, nil
}

func (r *KubernetesStorageRepository) Update(ctx context.Context, storage *entities.Storage) error {
	if err := storage.Validate(); err != nil {
		return fmt.Errorf("invalid storage: %w", err)
	}
	raw, err := r.marshal(storage)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.name(storage.ID()), Namespace: r.namespace,
			ResourceVersion: storage.Version(), Labels: r.labels(storage), Annotations: r.annotations(storage),
		},
		Data: map[string]string{ConfigMapKeyStorage: string(raw)},
	}
	updated, err := r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if k8serrors.IsNotFound(err) {
		return entities.ErrStorageNotFound{ID: storage.ID()}
	}
	if err != nil {
		return err
	}
	storage.SetVersion(updated.ResourceVersion)
	return nil
}

func (r *KubernetesStorageRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.name(id), metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return entities.ErrStorageNotFound{ID: id}
	}
	return err
}

func (r *KubernetesStorageRepository) name(id string) string { return StoragePrefix + id }

func (r *KubernetesStorageRepository) labels(storage *entities.Storage) map[string]string {
	result := map[string]string{
		LabelStorageType: LabelStorageTypeValue, LabelStorageScope: string(storage.Scope()),
		LabelStorageOwnerHash: hashID(storage.OwnerID()),
	}
	if storage.TeamID() != "" {
		result[LabelStorageTeamHash] = hashID(storage.TeamID())
	}
	return result
}

func (r *KubernetesStorageRepository) annotations(storage *entities.Storage) map[string]string {
	result := map[string]string{AnnotationStorageOwnerID: storage.OwnerID()}
	if storage.TeamID() != "" {
		result[AnnotationStorageTeamID] = storage.TeamID()
	}
	return result
}

func (r *KubernetesStorageRepository) marshal(storage *entities.Storage) ([]byte, error) {
	return json.Marshal(storageJSON{
		ID: storage.ID(), Name: storage.Name(), Description: storage.Description(),
		OwnerID: storage.OwnerID(), Scope: storage.Scope(), TeamID: storage.TeamID(),
		Kind: storage.Kind(), Pool: storage.Pool(), Provider: storage.Provider(), Size: storage.Size(),
		State: storage.State(), Revision: storage.Revision(), Files: storage.Files(),
		CreatedAt: storage.CreatedAt(), UpdatedAt: storage.UpdatedAt(),
	})
}

func (r *KubernetesStorageRepository) decode(cm *corev1.ConfigMap) (*entities.Storage, error) {
	raw, ok := cm.Data[ConfigMapKeyStorage]
	if !ok {
		return nil, fmt.Errorf("storage ConfigMap %s is missing %s", cm.Name, ConfigMapKeyStorage)
	}
	var data storageJSON
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("decode storage metadata: %w", err)
	}
	if value := cm.Annotations[AnnotationStorageOwnerID]; value != "" {
		data.OwnerID = value
	}
	if value := cm.Annotations[AnnotationStorageTeamID]; value != "" {
		data.TeamID = value
	}
	storage := entities.NewStorage(data.ID, data.Name, data.Description, data.OwnerID, data.Scope, data.TeamID, data.Kind, data.Pool, data.Provider, data.Size)
	storage.SetState(data.State)
	storage.SetRevision(data.Revision)
	storage.SetFiles(data.Files)
	storage.SetCreatedAt(data.CreatedAt)
	storage.SetUpdatedAt(data.UpdatedAt)
	storage.SetVersion(cm.ResourceVersion)
	return storage, nil
}
