package controllers

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

func (c *SettingsController) applySlackSettings(settings *entities.Settings, req *UpdateSettingsRequest, user *entities.User) {
	if req.SlackUserID == nil {
		return
	}
	settings.SetSlackUserID(*req.SlackUserID)
	if c.notificationSvc == nil {
		return
	}
	channels := settings.NotificationChannels()
	slackEnabled := len(channels) == 0 || containsString(channels, "slack")
	if *req.SlackUserID == "" {
		if err := c.notificationSvc.DeleteSlackSubscription(string(user.ID())); err != nil {
			log.Printf("[SETTINGS] Failed to delete Slack subscription: %v", err)
		}
		return
	}
	if slackEnabled {
		if _, err := c.notificationSvc.SubscribeSlack(user, *req.SlackUserID); err != nil {
			log.Printf("[SETTINGS] Failed to create Slack subscription: %v", err)
		}
	}
}

func (c *SettingsController) applyNotificationChannels(settings *entities.Settings, req *UpdateSettingsRequest, user *entities.User) {
	if req.NotificationChannels == nil {
		return
	}
	settings.SetNotificationChannels(*req.NotificationChannels)
	if c.notificationSvc == nil {
		return
	}
	userID := string(user.ID())
	channels := *req.NotificationChannels
	// Empty channels retain the legacy behavior: every channel is enabled.
	slackEnabled := len(channels) == 0 || containsString(channels, "slack")
	webEnabled := len(channels) == 0 || containsString(channels, "web")
	if err := c.notificationSvc.SetSubscriptionTypeActive(userID, notification.SubscriptionTypeSlack, slackEnabled); err != nil {
		log.Printf("[SETTINGS] Failed to update Slack subscription active state: %v", err)
	}
	if err := c.notificationSvc.SetSubscriptionTypeActive(userID, notification.SubscriptionTypeWebPush, webEnabled); err != nil {
		log.Printf("[SETTINGS] Failed to update WebPush subscription active state: %v", err)
	}
}
