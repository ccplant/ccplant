package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const localUserDataKey = "user.json"

type KubernetesLocalUserRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesLocalUserRepository(client kubernetes.Interface, namespace string) *KubernetesLocalUserRepository {
	return &KubernetesLocalUserRepository{client: client, namespace: namespace}
}

func localUserSecretName(id entities.UserID) string {
	sum := sha256.Sum256([]byte(id))
	return "agentapi-local-user-" + hex.EncodeToString(sum[:16])
}

func (r *KubernetesLocalUserRepository) Create(ctx context.Context, user *entities.LocalUser) error {
	if err := user.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(user)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: localUserSecretName(user.ID), Namespace: r.namespace,
		Labels: map[string]string{"agentapi.proxy/local-user": "true"}}, Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{localUserDataKey: b}}
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return entities.ErrLocalUserAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("create local user: %w", err)
	}
	return nil
}

func (r *KubernetesLocalUserRepository) GetByID(ctx context.Context, id entities.UserID) (*entities.LocalUser, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, localUserSecretName(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, entities.ErrLocalUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get local user: %w", err)
	}
	var user entities.LocalUser
	if err := json.Unmarshal(secret.Data[localUserDataKey], &user); err != nil {
		return nil, fmt.Errorf("decode local user: %w", err)
	}
	if err := user.Validate(); err != nil {
		return nil, fmt.Errorf("invalid persisted local user: %w", err)
	}
	if user.ID != id {
		return nil, errors.New("persisted local user id mismatch")
	}
	return &user, nil
}
