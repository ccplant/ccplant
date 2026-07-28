package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	LabelInitialMessageHistory        = "agentapi.proxy/initial-message-history"
	AnnotationInitialMessageOwnerID   = "agentapi.proxy/owner-id"
	SecretKeyInitialMessageHistory    = "history.json"
	initialMessageHistorySecretPrefix = "agentapi-initial-messages-"
	initialMessageHistoryVersion      = 1
	initialMessageHistoryMaxRetries   = 5
)

type initialMessageHistoryJSON struct {
	Version   int                                  `json:"version"`
	UserID    string                               `json:"user_id"`
	Items     []entities.InitialMessageHistoryItem `json:"items"`
	UpdatedAt time.Time                            `json:"updated_at"`
}

type KubernetesInitialMessageHistoryRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesInitialMessageHistoryRepository(client kubernetes.Interface, namespace string) *KubernetesInitialMessageHistoryRepository {
	return &KubernetesInitialMessageHistoryRepository{client: client, namespace: namespace}
}

func (r *KubernetesInitialMessageHistoryRepository) List(ctx context.Context, userID string, limit int) ([]entities.InitialMessageHistoryItem, error) {
	history, _, err := r.load(ctx, userID)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return []entities.InitialMessageHistoryItem{}, nil
		}
		return nil, err
	}
	if limit < len(history.Items) {
		history.Items = history.Items[:limit]
	}
	return history.Items, nil
}

func (r *KubernetesInitialMessageHistoryRepository) UpsertAndTrim(ctx context.Context, userID, content string, maxItems int) (entities.InitialMessageHistoryItem, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return entities.InitialMessageHistoryItem{}, fmt.Errorf("initial message must not be empty")
	}
	if maxItems <= 0 {
		return entities.InitialMessageHistoryItem{}, fmt.Errorf("max items must be positive")
	}

	for attempt := 0; attempt < initialMessageHistoryMaxRetries; attempt++ {
		history, secret, err := r.load(ctx, userID)
		if err != nil && !k8serrors.IsNotFound(err) {
			return entities.InitialMessageHistoryItem{}, err
		}

		now := time.Now().UTC()
		item := entities.InitialMessageHistoryItem{ID: uuid.NewString(), Content: content, LastUsedAt: now}
		items := make([]entities.InitialMessageHistoryItem, 0, len(history.Items)+1)
		for _, existing := range history.Items {
			if existing.Content == content {
				item.ID = existing.ID
				continue
			}
			items = append(items, existing)
		}
		items = append([]entities.InitialMessageHistoryItem{item}, items...)
		if len(items) > maxItems {
			items = items[:maxItems]
		}

		payload := initialMessageHistoryJSON{
			Version: initialMessageHistoryVersion, UserID: userID, Items: items, UpdatedAt: now,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return entities.InitialMessageHistoryItem{}, fmt.Errorf("marshal initial message history: %w", err)
		}

		if secret == nil {
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: r.secretName(userID), Namespace: r.namespace,
					Labels:      map[string]string{LabelInitialMessageHistory: "true"},
					Annotations: map[string]string{AnnotationInitialMessageOwnerID: userID},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{SecretKeyInitialMessageHistory: data},
			}
			_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
		} else {
			secret.Data = map[string][]byte{SecretKeyInitialMessageHistory: data}
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		}
		if err == nil {
			return item, nil
		}
		if !k8serrors.IsConflict(err) && !k8serrors.IsAlreadyExists(err) {
			return entities.InitialMessageHistoryItem{}, fmt.Errorf("save initial message history: %w", err)
		}
		delay := time.Duration(1<<attempt) * 10 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return entities.InitialMessageHistoryItem{}, ctx.Err()
		case <-timer.C:
		}
	}
	return entities.InitialMessageHistoryItem{}, fmt.Errorf("save initial message history: concurrent update retries exhausted")
}

func (r *KubernetesInitialMessageHistoryRepository) DeleteAll(ctx context.Context, userID string) error {
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, r.secretName(userID), metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete initial message history: %w", err)
	}
	return nil
}

func (r *KubernetesInitialMessageHistoryRepository) load(ctx context.Context, userID string) (initialMessageHistoryJSON, *corev1.Secret, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(userID), metav1.GetOptions{})
	if err != nil {
		return initialMessageHistoryJSON{Version: initialMessageHistoryVersion, UserID: userID, Items: []entities.InitialMessageHistoryItem{}}, nil, err
	}
	var history initialMessageHistoryJSON
	if err := json.Unmarshal(secret.Data[SecretKeyInitialMessageHistory], &history); err != nil {
		return initialMessageHistoryJSON{}, nil, fmt.Errorf("decode initial message history: %w", err)
	}
	if history.Version != initialMessageHistoryVersion || history.UserID != userID {
		return initialMessageHistoryJSON{}, nil, fmt.Errorf("invalid initial message history payload")
	}
	if history.Items == nil {
		history.Items = []entities.InitialMessageHistoryItem{}
	}
	return history, secret, nil
}

func (r *KubernetesInitialMessageHistoryRepository) secretName(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return initialMessageHistorySecretPrefix + hex.EncodeToString(sum[:])[:16]
}
