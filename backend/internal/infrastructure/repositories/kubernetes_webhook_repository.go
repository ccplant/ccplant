package repositories

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// LabelWebhook is the label key for webhook resources
	LabelWebhook = "agentapi.proxy/webhook"
	// LabelWebhookID is the label key for webhook ID
	LabelWebhookID = "agentapi.proxy/webhook-id"
	// LabelWebhookScope is the label key for webhook scope (user or team)
	LabelWebhookScope = "agentapi.proxy/webhook-scope"
	// LabelWebhookUserID is the label key for webhook user ID
	LabelWebhookUserID = "agentapi.proxy/webhook-user-id"
	// LabelWebhookTeamIDHash is the label key for hashed webhook team ID
	LabelWebhookTeamIDHash = "agentapi.proxy/webhook-team-id-hash"
	// AnnotationWebhookTeamID is the annotation key for original webhook team ID
	AnnotationWebhookTeamID = "agentapi.proxy/webhook-team-id"
	// SecretKeyWebhook is the key in the Secret data for webhook JSON
	SecretKeyWebhook = "webhook.json"
	// WebhookSecretPrefix is the prefix for webhook Secret names
	WebhookSecretPrefix = "agentapi-webhook-"
)

// webhookJSON is the JSON representation for storage
type webhookJSON struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	UserID string                 `json:"user_id"`
	Scope  entities.ResourceScope `json:"scope,omitempty"`
	TeamID string                 `json:"team_id,omitempty"`
	// UserTeams holds the GitHub team slugs of the webhook creator, captured at
	// create/update time.  Persisted so the webhook handler can inject team-level
	// settings without a live auth context.
	UserTeams       []string                      `json:"user_teams,omitempty"`
	Status          entities.WebhookStatus        `json:"status"`
	Type            entities.WebhookType          `json:"type"`
	Secret          string                        `json:"secret"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	SignaturePrefix string                        `json:"signature_prefix,omitempty"`
	GitHub          *webhookGitHubConfigJSON      `json:"github,omitempty"`
	Triggers        []webhookTriggerJSON          `json:"triggers"`
	SessionConfig   *webhookSessionConfigJSON     `json:"session_config,omitempty"`
	MaxSessions     int                           `json:"max_sessions,omitempty"`
	CreatedAt       time.Time                     `json:"created_at"`
	UpdatedAt       time.Time                     `json:"updated_at"`
	LastDelivery    *webhookDeliveryRecordJSON    `json:"last_delivery,omitempty"`
	DeliveryCount   int64                         `json:"delivery_count"`
}

type webhookGitHubConfigJSON struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

type webhookTriggerJSON struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Priority      int                          `json:"priority"`
	Enabled       bool                         `json:"enabled"`
	Conditions    webhookTriggerConditionsJSON `json:"conditions"`
	SessionConfig *webhookSessionConfigJSON    `json:"session_config,omitempty"`
	StopOnMatch   bool                         `json:"stop_on_match"`
}

type webhookTriggerConditionsJSON struct {
	GitHub     *webhookGitHubConditionsJSON `json:"github,omitempty"`
	GoTemplate string                       `json:"go_template,omitempty"`
}

type webhookGitHubConditionsJSON struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

type webhookSessionConfigJSON struct {
	Environment            map[string]string       `json:"environment,omitempty"`
	Tags                   map[string]string       `json:"tags,omitempty"`
	InitialMessageTemplate string                  `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                  `json:"reuse_message_template,omitempty"`
	Params                 *entities.SessionParams `json:"params,omitempty"`
	ReuseSession           bool                    `json:"reuse_session,omitempty"`
	MountPayload           bool                    `json:"mount_payload,omitempty"`
	SessionProfileID       string                  `json:"session_profile_id,omitempty"`
}

type webhookDeliveryRecordJSON struct {
	ID             string                  `json:"id"`
	ReceivedAt     time.Time               `json:"received_at"`
	Status         entities.DeliveryStatus `json:"status"`
	MatchedTrigger string                  `json:"matched_trigger,omitempty"`
	SessionID      string                  `json:"session_id,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

// KubernetesWebhookRepository implements WebhookRepository using Kubernetes Secrets
type KubernetesWebhookRepository struct {
	client                      kubernetes.Interface
	namespace                   string
	defaultGitHubEnterpriseHost string
	mu                          sync.RWMutex
}

// NewKubernetesWebhookRepository creates a new KubernetesWebhookRepository
func NewKubernetesWebhookRepository(client kubernetes.Interface, namespace string) *KubernetesWebhookRepository {
	return &KubernetesWebhookRepository{
		client:    client,
		namespace: namespace,
	}
}

// SetDefaultGitHubEnterpriseHost sets the default GitHub Enterprise host for webhook matching
// When set, webhooks without explicit enterprise_url will match against this host
func (r *KubernetesWebhookRepository) SetDefaultGitHubEnterpriseHost(host string) {
	r.defaultGitHubEnterpriseHost = host
}

// webhookSecretName returns the Secret name for a given webhook ID
func webhookSecretName(id string) string {
	return WebhookSecretPrefix + id
}

// Create creates a new webhook
func (r *KubernetesWebhookRepository) Create(ctx context.Context, webhook *entities.Webhook) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if webhook already exists
	secretName := webhookSecretName(webhook.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("webhook already exists: %s", webhook.ID())
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check webhook existence: %w", err)
	}

	// Generate secret if not provided
	if webhook.Secret() == "" {
		secret, err := generateWebhookSecret(32)
		if err != nil {
			return fmt.Errorf("failed to generate secret: %w", err)
		}
		webhook.SetSecret(secret)
	}

	// Save webhook to its own Secret
	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// Get retrieves a webhook by ID
func (r *KubernetesWebhookRepository) Get(ctx context.Context, id string) (*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.loadWebhook(ctx, id)
}

// List retrieves webhooks matching the filter
func (r *KubernetesWebhookRepository) List(ctx context.Context, filter repositories.WebhookFilter) ([]*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	webhooks, err := r.loadAllWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*entities.Webhook
	for _, w := range webhooks {
		if filter.UserID != "" && w.UserID() != filter.UserID {
			continue
		}
		if filter.Status != "" && w.Status() != filter.Status {
			continue
		}
		if filter.Type != "" && w.WebhookType() != filter.Type {
			continue
		}
		if filter.Scope != "" && w.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && w.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && w.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if w.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, w)
	}

	return result, nil
}

// Update updates an existing webhook
func (r *KubernetesWebhookRepository) Update(ctx context.Context, webhook *entities.Webhook) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if webhook exists
	secretName := webhookSecretName(webhook.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrWebhookNotFound{ID: webhook.ID()}
		}
		return fmt.Errorf("failed to get webhook: %w", err)
	}

	webhook.SetUpdatedAt(time.Now())

	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// Delete removes a webhook by ID
func (r *KubernetesWebhookRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := webhookSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrWebhookNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete webhook secret: %w", err)
	}

	return nil
}

// FindByGitHubRepository finds webhooks that may match a GitHub webhook
func (r *KubernetesWebhookRepository) FindByGitHubRepository(ctx context.Context, matcher repositories.GitHubMatcher) ([]*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	webhooks, err := r.loadAllWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*entities.Webhook
	for _, w := range webhooks {
		// Only GitHub webhooks
		if w.WebhookType() != entities.WebhookTypeGitHub {
			continue
		}

		// Only active webhooks
		if w.Status() != entities.WebhookStatusActive {
			continue
		}

		github := w.GitHub()
		if github != nil {
			// Check enterprise URL match
			// Use webhook's explicit enterprise_url if set, otherwise use the default from config
			webhookEnterpriseURL := github.EnterpriseURL()
			if webhookEnterpriseURL == "" && r.defaultGitHubEnterpriseHost != "" {
				webhookEnterpriseURL = r.defaultGitHubEnterpriseHost
			}
			normalizedWebhookURL := normalizeEnterpriseURL(webhookEnterpriseURL)
			normalizedMatcherURL := normalizeEnterpriseURL(matcher.EnterpriseURL)
			if normalizedWebhookURL != normalizedMatcherURL {
				continue
			}

			// Check allowed events
			if len(github.AllowedEvents()) > 0 {
				eventAllowed := false
				for _, allowedEvent := range github.AllowedEvents() {
					if allowedEvent == matcher.Event {
						eventAllowed = true
						break
					}
				}
				if !eventAllowed {
					continue
				}
			}

			// Check allowed repositories
			if len(github.AllowedRepositories()) > 0 {
				repoAllowed := false
				for _, allowedRepo := range github.AllowedRepositories() {
					if matchWebhookRepository(allowedRepo, matcher.Repository) {
						repoAllowed = true
						break
					}
				}
				if !repoAllowed {
					continue
				}
			}
		}

		result = append(result, w)
	}

	return result, nil
}

// RecordDelivery records a webhook delivery
func (r *KubernetesWebhookRepository) RecordDelivery(ctx context.Context, id string, record *entities.WebhookDeliveryRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	webhook, err := r.loadWebhook(ctx, id)
	if err != nil {
		return err
	}

	webhook.SetLastDelivery(record)
	webhook.IncrementDeliveryCount()

	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// loadWebhook loads a single webhook from its Kubernetes Secret
