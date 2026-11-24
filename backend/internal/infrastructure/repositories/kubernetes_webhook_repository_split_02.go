package repositories

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

func (r *KubernetesWebhookRepository) loadWebhook(ctx context.Context, id string) (*entities.Webhook, error) {
	secretName := webhookSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, entities.ErrWebhookNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get webhook secret: %w", err)
	}

	data, ok := secret.Data[SecretKeyWebhook]
	if !ok {
		return nil, fmt.Errorf("webhook secret missing data key: %s", SecretKeyWebhook)
	}

	var wj webhookJSON
	if err := json.Unmarshal(data, &wj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhook: %w", err)
	}

	return r.jsonToEntity(&wj), nil
}

// loadAllWebhooks loads all webhooks from Kubernetes Secrets
func (r *KubernetesWebhookRepository) loadAllWebhooks(ctx context.Context) ([]*entities.Webhook, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelWebhook)
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list webhook secrets: %w", err)
	}

	result := make([]*entities.Webhook, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		data, ok := secret.Data[SecretKeyWebhook]
		if !ok {
			continue
		}

		var wj webhookJSON
		if err := json.Unmarshal(data, &wj); err != nil {
			continue
		}

		// Prefer team_id from annotation over JSON data
		// Use the annotation value as-is (should be in slash format: org/team-slug)
		if annotationTeamID, ok := secret.Annotations[AnnotationWebhookTeamID]; ok && annotationTeamID != "" {
			wj.TeamID = annotationTeamID
		}

		webhook := r.jsonToEntity(&wj)
		result = append(result, webhook)
	}

	return result, nil
}

// saveWebhook saves a webhook to its own Kubernetes Secret
func (r *KubernetesWebhookRepository) saveWebhook(ctx context.Context, webhook *entities.Webhook) error {
	wj := r.entityToJSON(webhook)
	data, err := json.Marshal(wj)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook: %w", err)
	}

	secretName := webhookSecretName(webhook.ID())
	labels := map[string]string{
		LabelWebhook:       "true",
		LabelWebhookID:     webhook.ID(),
		LabelWebhookScope:  string(webhook.Scope()),
		LabelWebhookUserID: webhook.UserID(),
	}
	annotations := make(map[string]string)
	if webhook.TeamID() != "" {
		// Use sha256 hash for team-id label to avoid issues with "/" in team IDs
		// The original team_id is stored in annotations for restoration
		labels[LabelWebhookTeamIDHash] = services.HashTeamID(webhook.TeamID())
		annotations[AnnotationWebhookTeamID] = webhook.TeamID()
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   r.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyWebhook: data,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeyWebhook] = data
			// Ensure labels and annotations are set
			existing.Labels = labels
			existing.Annotations = annotations

			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update webhook secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create webhook secret: %w", err)
	}

	return nil
}

// jsonToEntity converts JSON representation to entity
func (r *KubernetesWebhookRepository) jsonToEntity(wj *webhookJSON) *entities.Webhook {
	webhook := entities.NewWebhook(wj.ID, wj.Name, wj.UserID, wj.Type)
	webhook.SetScope(wj.Scope)
	webhook.SetTeamID(wj.TeamID)
	if len(wj.UserTeams) > 0 {
		webhook.SetUserTeams(wj.UserTeams)
	}
	webhook.SetStatus(wj.Status)
	webhook.SetSecret(wj.Secret)
	if wj.SignatureHeader != "" {
		webhook.SetSignatureHeader(wj.SignatureHeader)
	}
	if wj.SignatureType != "" {
		webhook.SetSignatureType(wj.SignatureType)
	}
	if wj.SignaturePrefix != "" {
		webhook.SetSignaturePrefix(wj.SignaturePrefix)
	}
	if wj.MaxSessions > 0 {
		webhook.SetMaxSessions(wj.MaxSessions)
	}

	// GitHub config
	if wj.GitHub != nil {
		github := entities.NewWebhookGitHubConfig()
		github.SetEnterpriseURL(wj.GitHub.EnterpriseURL)
		github.SetAllowedEvents(wj.GitHub.AllowedEvents)
		github.SetAllowedRepositories(wj.GitHub.AllowedRepositories)
		webhook.SetGitHub(github)
	}

	// Triggers
	triggers := make([]entities.WebhookTrigger, 0, len(wj.Triggers))
	for _, tj := range wj.Triggers {
		trigger := entities.NewWebhookTrigger(tj.ID, tj.Name)
		trigger.SetPriority(tj.Priority)
		trigger.SetEnabled(tj.Enabled)
		trigger.SetStopOnMatch(tj.StopOnMatch)

		// Conditions
		var conditions entities.WebhookTriggerConditions
		if tj.Conditions.GitHub != nil {
			ghCond := entities.NewWebhookGitHubConditions()
			ghCond.SetEvents(tj.Conditions.GitHub.Events)
			ghCond.SetActions(tj.Conditions.GitHub.Actions)
			ghCond.SetBranches(tj.Conditions.GitHub.Branches)
			ghCond.SetRepositories(tj.Conditions.GitHub.Repositories)
			ghCond.SetLabels(tj.Conditions.GitHub.Labels)
			ghCond.SetPaths(tj.Conditions.GitHub.Paths)
			ghCond.SetBaseBranches(tj.Conditions.GitHub.BaseBranches)
			ghCond.SetDraft(tj.Conditions.GitHub.Draft)
			ghCond.SetSender(tj.Conditions.GitHub.Sender)
			conditions.SetGitHub(ghCond)
		}
		if tj.Conditions.GoTemplate != "" {
			conditions.SetGoTemplate(tj.Conditions.GoTemplate)
		}
		trigger.SetConditions(conditions)

		// Session config
		if tj.SessionConfig != nil {
			trigger.SetSessionConfig(r.sessionConfigJSONToEntity(tj.SessionConfig))
		}

		triggers = append(triggers, trigger)
	}
	webhook.SetTriggers(triggers)

	// Session config
	if wj.SessionConfig != nil {
		webhook.SetSessionConfig(r.sessionConfigJSONToEntity(wj.SessionConfig))
	}

	// Last delivery
	if wj.LastDelivery != nil {
		record := entities.NewWebhookDeliveryRecord(wj.LastDelivery.ID, wj.LastDelivery.Status)
		if !wj.LastDelivery.ReceivedAt.IsZero() {
			record.SetReceivedAt(wj.LastDelivery.ReceivedAt)
		}
		record.SetMatchedTrigger(wj.LastDelivery.MatchedTrigger)
		record.SetSessionID(wj.LastDelivery.SessionID)
		record.SetError(wj.LastDelivery.Error)
		webhook.SetLastDelivery(record)
	}

	// Restore persisted metadata last because the setters above update timestamps.
	if !wj.CreatedAt.IsZero() {
		webhook.SetCreatedAt(wj.CreatedAt)
	}
	webhook.SetDeliveryCount(wj.DeliveryCount)
	if !wj.UpdatedAt.IsZero() {
		webhook.SetUpdatedAt(wj.UpdatedAt)
	}

	return webhook
}

// entityToJSON converts entity to JSON representation
func (r *KubernetesWebhookRepository) entityToJSON(w *entities.Webhook) *webhookJSON {
	wj := &webhookJSON{
		ID:              w.ID(),
		Name:            w.Name(),
		UserID:          w.UserID(),
		Scope:           w.Scope(),
		TeamID:          w.TeamID(),
		UserTeams:       w.UserTeams(),
		Status:          w.Status(),
		Type:            w.WebhookType(),
		Secret:          w.Secret(),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   w.SignatureType(),
		SignaturePrefix: w.SignaturePrefix(),
		MaxSessions:     w.MaxSessions(),
		CreatedAt:       w.CreatedAt(),
		UpdatedAt:       w.UpdatedAt(),
		DeliveryCount:   w.DeliveryCount(),
	}

	// GitHub config
	if gh := w.GitHub(); gh != nil {
		wj.GitHub = &webhookGitHubConfigJSON{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}

	// Triggers
	triggers := w.Triggers()
	wj.Triggers = make([]webhookTriggerJSON, 0, len(triggers))
	for _, t := range triggers {
		tj := webhookTriggerJSON{
			ID:          t.ID(),
			Name:        t.Name(),
			Priority:    t.Priority(),
			Enabled:     t.Enabled(),
			StopOnMatch: t.StopOnMatch(),
		}

		// Conditions
		cond := t.Conditions()
		if ghCond := cond.GitHub(); ghCond != nil {
			tj.Conditions.GitHub = &webhookGitHubConditionsJSON{
				Events:       ghCond.Events(),
				Actions:      ghCond.Actions(),
				Branches:     ghCond.Branches(),
				Repositories: ghCond.Repositories(),
				Labels:       ghCond.Labels(),
				Paths:        ghCond.Paths(),
				BaseBranches: ghCond.BaseBranches(),
				Draft:        ghCond.Draft(),
				Sender:       ghCond.Sender(),
			}
		}
		if goTemplate := cond.GoTemplate(); goTemplate != "" {
			tj.Conditions.GoTemplate = goTemplate
		}

		// Session config
		if sc := t.SessionConfig(); sc != nil {
			tj.SessionConfig = r.sessionConfigEntityToJSON(sc)
		}

		wj.Triggers = append(wj.Triggers, tj)
	}

	// Session config
	if sc := w.SessionConfig(); sc != nil {
		wj.SessionConfig = r.sessionConfigEntityToJSON(sc)
	}

	// Last delivery
	if ld := w.LastDelivery(); ld != nil {
		wj.LastDelivery = &webhookDeliveryRecordJSON{
			ID:             ld.ID(),
			ReceivedAt:     ld.ReceivedAt(),
			Status:         ld.Status(),
			MatchedTrigger: ld.MatchedTrigger(),
			SessionID:      ld.SessionID(),
			Error:          ld.Error(),
		}
	}

	return wj
}

func (r *KubernetesWebhookRepository) sessionConfigJSONToEntity(scj *webhookSessionConfigJSON) *entities.WebhookSessionConfig {
	sc := entities.NewWebhookSessionConfig()
	sc.SetEnvironment(scj.Environment)
	sc.SetTags(scj.Tags)
	sc.SetInitialMessageTemplate(scj.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(scj.ReuseMessageTemplate)
	sc.SetReuseSession(scj.ReuseSession)
	sc.SetMountPayload(scj.MountPayload)
	if scj.Params != nil {
		// Use entities.SessionParams directly from JSON - no need to copy fields
		sc.SetParams(scj.Params)
	}
	if scj.SessionProfileID != "" {
		sc.SetSessionProfileID(scj.SessionProfileID)
	}
	return sc
}

func (r *KubernetesWebhookRepository) sessionConfigEntityToJSON(sc *entities.WebhookSessionConfig) *webhookSessionConfigJSON {
	scj := &webhookSessionConfigJSON{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
		SessionProfileID:       sc.SessionProfileID(),
	}
	if params := sc.Params(); params != nil {
		// Use entities.SessionParams directly for JSON - no need to copy fields
		scj.Params = params
	}
	return scj
}

// Helper functions
