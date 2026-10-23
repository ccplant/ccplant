package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// ShareConfigMapName is the name of the ConfigMap storing session shares
	ShareConfigMapName = "agentapi-session-shares"
	// ShareConfigMapDataKey is the key in the ConfigMap data
	ShareConfigMapDataKey = "shares.json"
	// ShareSecretName is the Secret used for bearer share metadata.
	ShareSecretName = "agentapi-session-shares"
	// LabelShare is the label key for share resources
	LabelShare = "agentapi.proxy/shares"
)

// shareDataJSON is the JSON structure stored in ConfigMap
type shareDataJSON struct {
	Shares         map[string]*shareJSON `json:"shares"`           // token -> share
	SessionToToken map[string]string     `json:"session_to_token"` // sessionID -> token
}

// shareJSON is the JSON representation of a single share
type shareJSON struct {
	Token     string     `json:"token"`
	SessionID string     `json:"session_id"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// KubernetesShareRepository implements ShareRepository using a Kubernetes
// Secret. The legacy ConfigMap is read only as a migration fallback.
type KubernetesShareRepository struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.Mutex
}

// NewKubernetesShareRepository creates a new KubernetesShareRepository
func NewKubernetesShareRepository(client kubernetes.Interface, namespace string) *KubernetesShareRepository {
	return &KubernetesShareRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save persists a session share
func (r *KubernetesShareRepository) Save(share *entities.SessionShare) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()

	// Load existing data
	data, err := r.loadData(ctx)
	if err != nil {
		data = &shareDataJSON{
			Shares:         make(map[string]*shareJSON),
			SessionToToken: make(map[string]string),
		}
	}

	// Remove old share if exists for this session
	if oldToken, exists := data.SessionToToken[share.SessionID()]; exists {
		delete(data.Shares, oldToken)
	}

	// Add new share
	data.Shares[share.Token()] = &shareJSON{
		Token:     share.Token(),
		SessionID: share.SessionID(),
		CreatedBy: share.CreatedBy(),
		CreatedAt: share.CreatedAt(),
		ExpiresAt: share.ExpiresAt(),
	}
	data.SessionToToken[share.SessionID()] = share.Token()

	// Save data
	return r.saveData(ctx, data)
}

// FindByToken retrieves a share by its token
func (r *KubernetesShareRepository) FindByToken(token string) (*entities.SessionShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	data, err := r.loadData(ctx)
	if err != nil {
		return nil, fmt.Errorf("share not found for token")
	}

	sj, exists := data.Shares[token]
	if !exists {
		return nil, fmt.Errorf("share not found for token")
	}

	return entities.NewSessionShareWithToken(sj.Token, sj.SessionID, sj.CreatedBy, sj.CreatedAt, sj.ExpiresAt), nil
}

// FindBySessionID retrieves a share by session ID
func (r *KubernetesShareRepository) FindBySessionID(sessionID string) (*entities.SessionShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	data, err := r.loadData(ctx)
	if err != nil {
		return nil, fmt.Errorf("share not found for session: %s", sessionID)
	}

	token, exists := data.SessionToToken[sessionID]
	if !exists {
		return nil, fmt.Errorf("share not found for session: %s", sessionID)
	}

	sj, exists := data.Shares[token]
	if !exists {
		return nil, fmt.Errorf("share not found for session: %s", sessionID)
	}

	return entities.NewSessionShareWithToken(sj.Token, sj.SessionID, sj.CreatedBy, sj.CreatedAt, sj.ExpiresAt), nil
}

// Delete removes a share by session ID
func (r *KubernetesShareRepository) Delete(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	data, err := r.loadData(ctx)
	if err != nil {
		return fmt.Errorf("share not found for session: %s", sessionID)
	}

	token, exists := data.SessionToToken[sessionID]
	if !exists {
		return fmt.Errorf("share not found for session: %s", sessionID)
	}

	delete(data.Shares, token)
	delete(data.SessionToToken, sessionID)

	return r.saveData(ctx, data)
}

// DeleteByToken removes a share by token
func (r *KubernetesShareRepository) DeleteByToken(token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	data, err := r.loadData(ctx)
	if err != nil {
		return fmt.Errorf("share not found for token")
	}

	sj, exists := data.Shares[token]
	if !exists {
		return fmt.Errorf("share not found for token")
	}

	delete(data.SessionToToken, sj.SessionID)
	delete(data.Shares, token)

	return r.saveData(ctx, data)
}

// CleanupExpired removes all expired shares
func (r *KubernetesShareRepository) CleanupExpired() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	data, err := r.loadData(ctx)
	if err != nil {
		// No data means no shares to clean up
		return 0, nil
	}

	var toDelete []string
	now := time.Now()
	for token, sj := range data.Shares {
		if sj.ExpiresAt != nil && now.After(*sj.ExpiresAt) {
			toDelete = append(toDelete, token)
		}
	}

	if len(toDelete) == 0 {
		return 0, nil
	}

	for _, token := range toDelete {
		sj := data.Shares[token]
		delete(data.SessionToToken, sj.SessionID)
		delete(data.Shares, token)
	}

	if err := r.saveData(ctx, data); err != nil {
		return 0, err
	}

	return len(toDelete), nil
}

// loadData loads share data from ConfigMap
func (r *KubernetesShareRepository) loadData(ctx context.Context) (*shareDataJSON, error) {
	secret, secretErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, ShareSecretName, metav1.GetOptions{})
	if secretErr == nil {
		dataBytes, ok := secret.Data[ShareConfigMapDataKey]
		if !ok {
			return nil, fmt.Errorf("shares data key not found")
		}
		var data shareDataJSON
		if err := json.Unmarshal(dataBytes, &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal shares data: %w", err)
		}
		return &data, nil
	}
	if !errors.IsNotFound(secretErr) {
		return nil, fmt.Errorf("failed to get shares secret: %w", secretErr)
	}
	// Migration compatibility for deployments created before shares were moved
	// out of a broadly readable ConfigMap.
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, ShareConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("shares configmap not found")
		}
		return nil, fmt.Errorf("failed to get shares configmap: %w", err)
	}

	dataStr, ok := cm.Data[ShareConfigMapDataKey]
	if !ok {
		return nil, fmt.Errorf("shares data key not found")
	}

	var data shareDataJSON
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shares data: %w", err)
	}

	return &data, nil
}

// saveData saves share data to a Secret.
func (r *KubernetesShareRepository) saveData(ctx context.Context, data *shareDataJSON) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal shares data: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ShareSecretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelShare: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			ShareConfigMapDataKey: dataBytes,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
			current, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, ShareSecretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to load shares secret for update: %w", getErr)
			}
			secret.ResourceVersion = current.ResourceVersion
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update shares secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create shares secret: %w", err)
	}

	return nil
}
