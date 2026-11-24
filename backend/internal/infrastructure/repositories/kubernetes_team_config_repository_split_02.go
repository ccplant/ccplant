package repositories

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (r *KubernetesTeamConfigRepository) fromSecret(secret *corev1.Secret) (*entities.TeamConfig, error) {
	data, ok := secret.Data[SecretKeyConfig]
	if !ok {
		return nil, fmt.Errorf("secret %s does not contain %s key", secret.Name, SecretKeyConfig)
	}

	var jsonData teamConfigJSON
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal team config: %w", err)
	}

	// Convert service account if present
	var serviceAccount *entities.ServiceAccount
	if jsonData.ServiceAccount != nil {
		sa := jsonData.ServiceAccount

		// Parse timestamps
		createdAt, err := time.Parse(time.RFC3339, sa.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339, sa.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at: %w", err)
		}

		// Convert permissions
		permissions := make([]entities.Permission, len(sa.Permissions))
		for i, p := range sa.Permissions {
			permissions[i] = entities.Permission(p)
		}

		serviceAccount = entities.NewServiceAccount(
			jsonData.TeamID,
			entities.UserID(sa.UserID),
			sa.APIKey,
			permissions,
		)
		// Set the actual timestamps from storage
		serviceAccount.SetCreatedAt(createdAt)
		serviceAccount.SetUpdatedAt(updatedAt)
	}

	config := entities.NewTeamConfig(jsonData.TeamID, serviceAccount, jsonData.EnvVars)
	config.SetPrincipalID(jsonData.PrincipalID)
	config.SetName(jsonData.Name)
	config.SetOwnerIDs(jsonData.OwnerIDs)
	config.SetExternalTeams(jsonData.ExternalTeams)
	return config, nil
}

// sanitizeTeamIDForLabel converts team ID to a valid Kubernetes label value
func sanitizeTeamIDForLabel(teamID string) string {
	// Kubernetes label values must be 63 characters or less
	// and match regex: (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?
	sanitized := strings.ToLower(teamID)
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	// Replace any invalid characters
	reg := regexp.MustCompile(`[^a-z0-9-_.]`)
	sanitized = reg.ReplaceAllString(sanitized, "-")

	// Ensure it starts and ends with alphanumeric
	sanitized = strings.Trim(sanitized, "-_.")

	// Truncate to 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
		sanitized = strings.TrimRight(sanitized, "-_.")
	}

	return sanitized
}
