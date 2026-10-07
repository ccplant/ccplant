package repositories

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	contextTemplatePrefix    = "agentapi-context-template-"
	contextTemplateKey       = "template.json"
	contextTemplateLabel     = "agentapi.proxy/context-template"
	contextTemplateUserLabel = "agentapi.proxy/context-template-user"
)

type KubernetesSessionContextTemplateRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesSessionContextTemplateRepository(client kubernetes.Interface, namespace string) *KubernetesSessionContextTemplateRepository {
	return &KubernetesSessionContextTemplateRepository{client: client, namespace: namespace}
}

func (r *KubernetesSessionContextTemplateRepository) Create(ctx context.Context, template *entities.SessionContextTemplate) error {
	raw, err := json.Marshal(template)
	if err != nil {
		return err
	}
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: contextTemplatePrefix + template.ID, Labels: map[string]string{contextTemplateLabel: "true", contextTemplateUserLabel: sanitizeLabelValue(template.OwnerUserID)}}, Data: map[string][]byte{contextTemplateKey: raw}}, metav1.CreateOptions{})
	return err
}

func (r *KubernetesSessionContextTemplateRepository) Get(ctx context.Context, id string) (*entities.SessionContextTemplate, error) {
	s, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, contextTemplatePrefix+id, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var template entities.SessionContextTemplate
	if err := json.Unmarshal(s.Data[contextTemplateKey], &template); err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *KubernetesSessionContextTemplateRepository) List(ctx context.Context, filter portrepos.SessionContextTemplateFilter) ([]*entities.SessionContextTemplate, error) {
	items, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: contextTemplateLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]*entities.SessionContextTemplate, 0, len(items.Items))
	for _, item := range items.Items {
		var template entities.SessionContextTemplate
		if json.Unmarshal(item.Data[contextTemplateKey], &template) != nil {
			continue
		}
		allowed := template.Scope != entities.ScopeTeam && template.OwnerUserID == filter.UserID
		if template.Scope == entities.ScopeTeam {
			for _, team := range filter.TeamIDs {
				if team == template.TeamID {
					allowed = true
					break
				}
			}
		}
		if allowed {
			copy := template
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (r *KubernetesSessionContextTemplateRepository) Update(ctx context.Context, template *entities.SessionContextTemplate) error {
	s, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, contextTemplatePrefix+template.ID, metav1.GetOptions{})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(template)
	if err != nil {
		return err
	}
	s.Data[contextTemplateKey] = raw
	_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, s, metav1.UpdateOptions{})
	return err
}

func (r *KubernetesSessionContextTemplateRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, contextTemplatePrefix+id, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

var _ portrepos.SessionContextTemplateRepository = (*KubernetesSessionContextTemplateRepository)(nil)
