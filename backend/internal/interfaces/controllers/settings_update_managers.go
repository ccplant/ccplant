package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type externalManagerUpdate struct {
	current entities.ExternalSessionManagerEntry
}

func (c *SettingsController) applyExternalManagerSettings(settings *entities.Settings, req *UpdateSettingsRequest) ([]entities.ExternalSessionManagerEntry, []externalManagerUpdate, error) {
	var removedExternalManagers []entities.ExternalSessionManagerEntry

	var updatedExternalManagers []externalManagerUpdate
	// Update already-enrolled external session managers. New registrations are
	// accepted only through the one-time enrollment-token API.
	if req.ExternalSessionManagers != nil {
		existing := make(map[string]entities.ExternalSessionManagerEntry)
		for _, e := range settings.ExternalSessionManagers() {
			existing[e.ID] = e
		}

		updated := make([]entities.ExternalSessionManagerEntry, 0, len(*req.ExternalSessionManagers))
		retained := make(map[string]bool, len(*req.ExternalSessionManagers))
		for _, m := range *req.ExternalSessionManagers {
			if m.ID == "" {
				return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "external session managers must be enrolled with a registration token")
			}
			prev, ok := existing[m.ID]
			if !ok || prev.HMACSecret == "" {
				return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "external session manager is not enrolled")
			}
			entry := entities.ExternalSessionManagerEntry{
				ID:                 m.ID,
				InstanceID:         m.InstanceID,
				Name:               m.Name,
				HMACSecret:         prev.HMACSecret,
				Labels:             m.Labels,
				BindingSubjectType: prev.BindingSubjectType, BindingSubjectID: prev.BindingSubjectID,
			}
			// Pool membership is owned exclusively by PoolSupplier resources.
			// Preserve deprecated stored fields only during rolling upgrades.
			entry.Pool, entry.PoolEnabled = prev.Pool, prev.PoolEnabled
			entry.AutomaticAssignmentEnabled = prev.AutomaticAssignmentEnabled
			entry.LegacySchedulable, entry.LegacyDefault = prev.LegacySchedulable, prev.LegacyDefault
			retained[m.ID] = true
			// These fields are owned by the daemon registration/heartbeat API and
			// must survive a legacy settings update.
			entry.Version = prev.Version
			entry.ActiveSessions = prev.ActiveSessions
			entry.LastHeartbeatAt = prev.LastHeartbeatAt
			updated = append(updated, entry)
			updatedExternalManagers = append(updatedExternalManagers, externalManagerUpdate{current: entry})
		}
		// Pending one-time enrollments are not returned by settings responses and
		// must survive unrelated settings saves.
		for _, prev := range existing {
			if prev.HMACSecret == "" && prev.EnrollmentTokenHash != "" {
				updated = append(updated, prev)
			}
			if prev.HMACSecret != "" && !retained[prev.ID] {
				removedExternalManagers = append(removedExternalManagers, prev)
			}
		}
		settings.SetExternalSessionManagers(updated)
	}
	return removedExternalManagers, updatedExternalManagers, nil
}
