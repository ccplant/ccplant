package repositories

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	// LabelCredentials is the label key for credentials resources
	LabelCredentials = "agentapi.proxy/credentials"
	// LabelCredentialsName is the label key for credentials name
	LabelCredentialsName = "agentapi.proxy/credentials-name"
	// AgentFilesSecretPrefix is the prefix for the agentapi-agent-files-* Secrets.
	AgentFilesSecretPrefix = "agentapi-agent-files-"
	// AnnotationCredentialsName stores the original (unsanitised) credentials name
	AnnotationCredentialsName = "agentapi.proxy/credentials-name"
	// AnnotationCredentialsCreatedAt stores the creation timestamp in RFC3339 format
	AnnotationCredentialsCreatedAt = "agentapi.proxy/credentials-created-at"
	// AnnotationCredentialsUpdatedAt stores the last update timestamp in RFC3339 format
	AnnotationCredentialsUpdatedAt = "agentapi.proxy/credentials-updated-at"
)

// KubernetesCredentialsRepository implements CredentialsRepository using the
// agentapi-agent-files-{name} Kubernetes Secret with index-based KV format.
//
// Each file is stored as a pair of entries:
//
//	"<index>.path"    → absolute file path (e.g. /home/agentapi/.codex/auth.json)
//	"<index>.content" → raw file content (JSON for credential files)
//
// The mapping between file-type names and indices is determined by
// sessionsettings.ManagedFileTypeOrder.
type KubernetesCredentialsRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesCredentialsRepository creates a new KubernetesCredentialsRepository.
func NewKubernetesCredentialsRepository(client kubernetes.Interface, namespace string) *KubernetesCredentialsRepository {
	return &KubernetesCredentialsRepository{
		client:    client,
		namespace: namespace,
	}
}

// SaveFiles replaces the managed-file snapshot for a credential owner. The
// backing client may be a KV adapter; callers do not need Kubernetes access.
func (r *KubernetesCredentialsRepository) SaveFiles(ctx context.Context, name string, files []sessionsettings.ManagedFile) error {
	if name == "" {
		return fmt.Errorf("credentials name is required")
	}
	secretName := r.secretName(name)
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := now
	existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil && existing.Annotations != nil && existing.Annotations[AnnotationCredentialsCreatedAt] != "" {
		createdAt = existing.Annotations[AnnotationCredentialsCreatedAt]
	} else if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to read credentials snapshot: %w", err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: secretName, Namespace: r.namespace,
		Labels:      map[string]string{LabelCredentials: "true", LabelCredentialsName: sanitizeLabelValue(name)},
		Annotations: map[string]string{AnnotationCredentialsName: name, AnnotationCredentialsCreatedAt: createdAt, AnnotationCredentialsUpdatedAt: now},
	}, Type: corev1.SecretTypeOpaque, Data: sessionsettings.FilesToSecretData(files)}
	if apierrors.IsNotFound(err) {
		_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	} else {
		secret.ResourceVersion = existing.ResourceVersion
		_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("save managed-file snapshot: %w", err)
	}
	return nil
}

// credentialsSaveMaxRetries is the maximum number of optimistic-concurrency
// retries performed by Save when the backing Secret is updated concurrently.
const credentialsSaveMaxRetries = 5

// Save persists credentials for a single file type in the agentapi-agent-files-{name} Secret.
// It reads the current Secret first to preserve any existing entries for other file types.
//
// The backing KV store enforces strict version checking on Update, so the
// current Secret's ResourceVersion is always propagated to the Update call and
// the read-merge-write cycle is retried on conflict.
func (r *KubernetesCredentialsRepository) Save(ctx context.Context, creds *entities.Credentials) error {
	if err := creds.Validate(); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}

	filePath, ok := sessionsettings.ManagedFileTypes[creds.FileType()]
	if !ok {
		return fmt.Errorf("unknown file type: %s", creds.FileType())
	}
	secretName := r.secretName(creds.Name())
	labelValue := sanitizeLabelValue(creds.Name())
	content := string(creds.Data())

	secrets := r.client.CoreV1().Secrets(r.namespace)
	var lastErr error
	for attempt := 0; attempt < credentialsSaveMaxRetries; attempt++ {
		now := time.Now().UTC().Format(time.RFC3339)

		existing, getErr := secrets.Get(ctx, secretName, metav1.GetOptions{})
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return fmt.Errorf("failed to read credentials secret: %w", getErr)
		}

		var currentFiles []sessionsettings.ManagedFile
		createdAt := now
		hasExisting := getErr == nil
		if hasExisting {
			currentFiles = sessionsettings.SecretDataToFiles(existing.Data)
			if v := existing.Annotations[AnnotationCredentialsCreatedAt]; v != "" {
				createdAt = v
			}
		}

		// Update or append the entry for this file type.
		updated := false
		for i, f := range currentFiles {
			if f.Path == filePath {
				currentFiles[i].Content = content
				updated = true
				break
			}
		}
		if !updated {
			currentFiles = append(currentFiles, sessionsettings.ManagedFile{
				Path:    filePath,
				Content: content,
			})
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: r.namespace,
				Labels: map[string]string{
					LabelCredentials:     "true",
					LabelCredentialsName: labelValue,
				},
				Annotations: map[string]string{
					AnnotationCredentialsName:      creds.Name(),
					AnnotationCredentialsCreatedAt: createdAt,
					AnnotationCredentialsUpdatedAt: now,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: sessionsettings.FilesToSecretData(currentFiles),
		}

		if !hasExisting {
			_, err := secrets.Create(ctx, secret, metav1.CreateOptions{})
			if err == nil {
				return nil
			}
			lastErr = err
			// A concurrent Create raced us: fall through to the Update path by
			// re-Getting on the next iteration.
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("failed to create credentials secret: %w", err)
		}

		// Propagate the current ResourceVersion so the strict-version KV store
		// accepts the Update. The previous implementation left this empty, which
		// caused every Update on an existing Secret to fail with "kv record
		// version conflict".
		secret.ResourceVersion = existing.ResourceVersion
		_, err := secrets.Update(ctx, secret, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}
		lastErr = err
		if apierrors.IsConflict(err) {
			// Stale version: re-Get and retry.
			continue
		}
		return fmt.Errorf("failed to update credentials secret: %w", err)
	}

	return fmt.Errorf("failed to save credentials secret after %d retries: %w", credentialsSaveMaxRetries, lastErr)
}

// FindByName retrieves all managed files for the given name from the
// agentapi-agent-files-{name} Secret and returns an aggregate Credentials entity.
func (r *KubernetesCredentialsRepository) FindByName(ctx context.Context, name string) (*entities.Credentials, error) {
	secretName := r.secretName(name)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get credentials secret: %w", err)
	}

	files := sessionsettings.SecretDataToFiles(secret.Data)

	// Build a Credentials entity that summarises all stored files.
	// Data() is intentionally empty — individual file contents are never exposed via the API.
	credName := name
	if secret.Annotations != nil {
		if v := secret.Annotations[AnnotationCredentialsName]; v != "" {
			credName = v
		}
	}

	creds := entities.NewCredentials(credName, nil)
	creds.SetFiles(files)

	if secret.Annotations != nil {
		if v := secret.Annotations[AnnotationCredentialsCreatedAt]; v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				creds.SetCreatedAt(t)
			}
		}
		if v := secret.Annotations[AnnotationCredentialsUpdatedAt]; v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				creds.SetUpdatedAt(t)
			}
		}
	}

	return creds, nil
}

// Delete removes the entire agentapi-agent-files-{name} Secret.
func (r *KubernetesCredentialsRepository) Delete(ctx context.Context, name string) error {
	secretName := r.secretName(name)

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("credentials not found: %s", name)
		}
		return fmt.Errorf("failed to delete credentials secret: %w", err)
	}
	return nil
}

// Exists checks whether the agentapi-agent-files-{name} Secret exists.
func (r *KubernetesCredentialsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(name), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check credentials existence: %w", err)
	}
	return true, nil
}

// List retrieves metadata for all agentapi-agent-files-* Secrets.
func (r *KubernetesCredentialsRepository) List(ctx context.Context) ([]*entities.Credentials, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelCredentials),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials secrets: %w", err)
	}

	var list []*entities.Credentials
	for i := range secrets.Items {
		s := &secrets.Items[i]
		credName := s.Labels[LabelCredentialsName]
		if s.Annotations != nil {
			if v := s.Annotations[AnnotationCredentialsName]; v != "" {
				credName = v
			}
		}
		creds := entities.NewCredentials(credName, nil)
		creds.SetFiles(sessionsettings.SecretDataToFiles(s.Data))
		list = append(list, creds)
	}
	return list, nil
}

// secretName returns the agentapi-agent-files-{sanitized} Secret name for the given name.
func (r *KubernetesCredentialsRepository) secretName(name string) string {
	sanitized := sanitizeSecretName(name)
	maxLen := 253 - len(AgentFilesSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return AgentFilesSecretPrefix + sanitized
}
