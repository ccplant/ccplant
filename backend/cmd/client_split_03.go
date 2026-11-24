package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

const notifyRateLimitFile = "/tmp/notify"
const notifyRateLimitCooldown = 3 * time.Minute

const (
	notifyMaxAttempts = 3
	notifyTimeout     = 30 * time.Second
)

func sendNotificationWithRetry(ctx context.Context, send func(context.Context) (*client.SendNotificationResponse, error), wait func(context.Context, time.Duration) error) (*client.SendNotificationResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= notifyMaxAttempts; attempt++ {
		resp, err := send(ctx)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		var statusErr *client.HTTPStatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusServiceUnavailable || attempt == notifyMaxAttempts {
			break
		}
		if err := wait(ctx, time.Duration(attempt)*time.Second); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func waitForNotificationRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func checkNotifyRateLimit() bool {
	data, err := os.ReadFile(notifyRateLimitFile)
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return err == nil && time.Since(last) < notifyRateLimitCooldown
}

func recordNotifySent() {
	_ = os.WriteFile(notifyRateLimitFile, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func runClientSendNotification(cmd *cobra.Command, args []string) error {
	if clientNotifyTitle == "" {
		return fmt.Errorf("--title is required")
	}
	if clientNotifyBody == "" {
		return fmt.Errorf("--body is required")
	}
	if clientNotifySessionID == "" && clientNotifyUserID == "" {
		return fmt.Errorf("either --notify-session-id or --notify-user-id is required")
	}

	// Client-side rate limiting: skip if a notification was sent recently.
	if checkNotifyRateLimit() {
		fmt.Println("Notification skipped (rate limited)")
		return nil
	}

	c, err := resolveBaseClient()
	if err != nil {
		return fmt.Errorf("failed to resolve client: %w", err)
	}

	req := &client.SendNotificationRequest{
		Title:     clientNotifyTitle,
		Body:      clientNotifyBody,
		SessionID: clientNotifySessionID,
		UserID:    clientNotifyUserID,
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), notifyTimeout)
	defer cancel()
	resp, err := sendNotificationWithRetry(ctx, func(ctx context.Context) (*client.SendNotificationResponse, error) {
		return c.SendNotification(ctx, req)
	}, waitForNotificationRetry)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	if resp.Success {
		recordNotifySent()
		fmt.Println("Notification sent successfully")
	} else {
		fmt.Fprintf(os.Stderr, "Notification send failed: %s\n", resp.Message)
		os.Exit(1)
	}
	return nil
}
