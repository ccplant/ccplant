package schedule

import (
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func timePtr(t time.Time) *time.Time {
	return &t
}

func strPtr(s string) *string {
	return &s
}

func statusPtr(s ScheduleStatus) *ScheduleStatus {
	return &s
}

// setTestUser sets a test user in the echo context for testing
func setTestUser(c echo.Context, userID string) {
	// Create a test user with necessary permissions
	user := entities.NewUser(entities.UserID(userID), entities.UserTypeAPIKey, userID)
	user.SetPermissions([]entities.Permission{
		entities.PermissionSessionCreate,
		entities.PermissionSessionRead,
		entities.PermissionSessionUpdate,
		entities.PermissionSessionDelete,
	})
	c.Set("internal_user", user)
}
