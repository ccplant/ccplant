package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

const codexAuthAttemptLabel = "agentapi.proxy/codex-device-auth-attempt"

type KubernetesCodexAuthAttemptRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesCodexAuthAttemptRepository(client kubernetes.Interface, namespace string) *KubernetesCodexAuthAttemptRepository {
	return &KubernetesCodexAuthAttemptRepository{client: client, namespace: namespace}
}

func (r *KubernetesCodexAuthAttemptRepository) Create(ctx context.Context, attempt *codexauth.Attempt) error {
	secrets := r.client.CoreV1().Secrets(r.namespace)
	lock := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: codexAuthLockName(attempt.CredentialName), Labels: map[string]string{codexAuthAttemptLabel: "lock"}}, Data: map[string][]byte{"attempt_id": []byte(attempt.ID)}}
	if _, err := secrets.Create(ctx, lock, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return codexauth.ErrAttemptActive
		}
		return fmt.Errorf("claim device auth target: %w", err)
	}
	if err := r.createAttempt(ctx, attempt); err != nil {
		_ = secrets.Delete(ctx, lock.Name, metav1.DeleteOptions{})
		return err
	}
	return nil
}

func (r *KubernetesCodexAuthAttemptRepository) createAttempt(ctx context.Context, attempt *codexauth.Attempt) error {
	raw, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: codexAuthAttemptName(attempt.ID), Labels: map[string]string{codexAuthAttemptLabel: "true"}}, Data: map[string][]byte{"attempt.json": raw}}
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create device auth attempt: %w", err)
	}
	return nil
}

func (r *KubernetesCodexAuthAttemptRepository) Get(ctx context.Context, id string) (*codexauth.Attempt, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, codexAuthAttemptName(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, codexauth.ErrAttemptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device auth attempt: %w", err)
	}
	var attempt codexauth.Attempt
	if err := json.Unmarshal(secret.Data["attempt.json"], &attempt); err != nil {
		return nil, fmt.Errorf("decode device auth attempt: %w", err)
	}
	return &attempt, nil
}

func (r *KubernetesCodexAuthAttemptRepository) Update(ctx context.Context, attempt *codexauth.Attempt) error {
	secrets := r.client.CoreV1().Secrets(r.namespace)
	for i := 0; i < 5; i++ {
		secret, err := secrets.Get(ctx, codexAuthAttemptName(attempt.ID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		raw, err := json.Marshal(attempt)
		if err != nil {
			return err
		}
		secret.Data = map[string][]byte{"attempt.json": raw}
		if _, err = secrets.Update(ctx, secret, metav1.UpdateOptions{}); err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return err
		}
	}
	return fmt.Errorf("update device auth attempt: too many conflicts")
}

func (r *KubernetesCodexAuthAttemptRepository) ActiveByCredential(ctx context.Context, name string) (*codexauth.Attempt, error) {
	lock, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, codexAuthLockName(name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, codexauth.ErrAttemptNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, string(lock.Data["attempt_id"]))
}

func (r *KubernetesCodexAuthAttemptRepository) LatestByUser(ctx context.Context, userID string) (*codexauth.Attempt, error) {
	list, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: codexAuthAttemptLabel + "=true"})
	if err != nil {
		return nil, err
	}
	var matches []*codexauth.Attempt
	for _, secret := range list.Items {
		var attempt codexauth.Attempt
		if json.Unmarshal(secret.Data["attempt.json"], &attempt) == nil && attempt.UserID == userID {
			matches = append(matches, &attempt)
		}
	}
	if len(matches) == 0 {
		return nil, codexauth.ErrAttemptNotFound
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ExpiresAt.After(matches[j].ExpiresAt) })
	return matches[0], nil
}

func (r *KubernetesCodexAuthAttemptRepository) Release(ctx context.Context, attempt *codexauth.Attempt) error {
	secrets := r.client.CoreV1().Secrets(r.namespace)
	lock, err := secrets.Get(ctx, codexAuthLockName(attempt.CredentialName), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(lock.Data["attempt_id"]) != attempt.ID {
		return nil
	}
	return secrets.Delete(ctx, lock.Name, metav1.DeleteOptions{})
}

func codexAuthAttemptName(id string) string { return "agentapi-codex-auth-" + id }
func codexAuthLockName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "agentapi-codex-auth-lock-" + hex.EncodeToString(sum[:12])
}
