package webhook

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (c *WebhookGitHubController) buildDefaultInitialMessage(event string, payload *GitHubPayload) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "GitHub %s event received.\n\n", event)

	if payload.Repository != nil {
		fmt.Fprintf(&sb, "Repository: %s\n", payload.Repository.FullName)
		if payload.Repository.HTMLURL != "" {
			fmt.Fprintf(&sb, "URL: %s\n", payload.Repository.HTMLURL)
		}
	}

	switch event {
	case "push":
		if payload.Ref != "" {
			branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
			fmt.Fprintf(&sb, "Branch: %s\n", branch)
		}
		if payload.HeadCommit != nil {
			fmt.Fprintf(&sb, "Commit: %s\n", payload.HeadCommit.ID[:7])
			fmt.Fprintf(&sb, "Message: %s\n", payload.HeadCommit.Message)
		}

	case "pull_request":
		if payload.PullRequest != nil {
			fmt.Fprintf(&sb, "\nPull Request #%d: %s\n", payload.PullRequest.Number, payload.PullRequest.Title)
			fmt.Fprintf(&sb, "Action: %s\n", payload.Action)
			fmt.Fprintf(&sb, "URL: %s\n", payload.PullRequest.HTMLURL)
			if payload.PullRequest.Base != nil && payload.PullRequest.Head != nil {
				fmt.Fprintf(&sb, "Base: %s <- Head: %s\n", payload.PullRequest.Base.Ref, payload.PullRequest.Head.Ref)
			}
			if payload.PullRequest.Body != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", payload.PullRequest.Body)
			}
		}

	case "issues":
		if payload.Issue != nil {
			fmt.Fprintf(&sb, "\nIssue #%d: %s\n", payload.Issue.Number, payload.Issue.Title)
			fmt.Fprintf(&sb, "Action: %s\n", payload.Action)
			fmt.Fprintf(&sb, "URL: %s\n", payload.Issue.HTMLURL)
			if payload.Issue.Body != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", payload.Issue.Body)
			}
		}
	}

	if payload.Sender != nil {
		fmt.Fprintf(&sb, "\nTriggered by: %s\n", payload.Sender.Login)
	}

	return sb.String()
}

// Helper functions

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func matchAnyRepository(patterns []string, fullName string) bool {
	for _, pattern := range patterns {
		if matchRepository(pattern, fullName) {
			return true
		}
	}
	return false
}

func matchRepository(pattern, fullName string) bool {
	if pattern == fullName {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		owner := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(fullName, owner+"/")
	}
	return false
}

func extractBranch(event string, payload *GitHubPayload) string {
	switch event {
	case "push":
		if strings.HasPrefix(payload.Ref, "refs/heads/") {
			return strings.TrimPrefix(payload.Ref, "refs/heads/")
		}
		return payload.Ref
	case "pull_request":
		if payload.PullRequest != nil && payload.PullRequest.Head != nil {
			return payload.PullRequest.Head.Ref
		}
	case "create", "delete":
		return payload.Ref
	}
	return ""
}

func extractLabels(payload *GitHubPayload) []string {
	var labels []string
	if payload.PullRequest != nil {
		for _, label := range payload.PullRequest.Labels {
			labels = append(labels, label.Name)
		}
	}
	if payload.Issue != nil {
		for _, label := range payload.Issue.Labels {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

func extractChangedFiles(payload *GitHubPayload) []string {
	fileSet := make(map[string]bool)
	for _, commit := range payload.Commits {
		for _, f := range commit.Added {
			fileSet[f] = true
		}
		for _, f := range commit.Modified {
			fileSet[f] = true
		}
		for _, f := range commit.Removed {
			fileSet[f] = true
		}
	}

	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	return files
}

// anyStringInSlice returns true if any string from required is found in available.
func anyStringInSlice(required, available []string) bool {
	if len(available) == 0 {
		return false
	}
	for _, r := range required {
		if containsString(available, r) {
			return true
		}
	}
	return false
}

// anyFileMatchesPatterns returns true if any file matches any of the patterns.
func anyFileMatchesPatterns(patterns, files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		for _, pattern := range patterns {
			if matchPath(pattern, file) {
				return true
			}
		}
	}
	return false
}

func matchPatterns(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, value string) bool {
	if pattern == value {
		return true
	}
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

func matchPath(pattern, path string) bool {
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}

			if suffix != "" {
				remaining := path
				if prefix != "" {
					remaining = strings.TrimPrefix(path, prefix)
					remaining = strings.TrimPrefix(remaining, "/")
				}
				pathParts := strings.Split(remaining, "/")
				for i := range pathParts {
					testPath := strings.Join(pathParts[i:], "/")
					if matched, _ := filepath.Match(suffix, testPath); matched {
						return true
					}
					if matched, _ := filepath.Match(suffix, pathParts[len(pathParts)-1]); matched {
						return true
					}
				}
				return false
			}
			return true
		}
	}

	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}

// MatchTriggersForTest evaluates triggers against a test payload.
// This reuses the same matching logic as HandleGitHubWebhook but without
// signature verification or HTTP context dependency.
func (c *WebhookGitHubController) MatchTriggersForTest(
	triggers []entities.WebhookTrigger,
	event string,
	payload *GitHubPayload,
) *entities.WebhookTrigger {
	return c.matchTriggers(triggers, event, payload)
}

// BuildDefaultInitialMessageForTest builds a default initial message for a test payload.
func (c *WebhookGitHubController) BuildDefaultInitialMessageForTest(event string, payload *GitHubPayload) string {
	return c.buildDefaultInitialMessage(event, payload)
}

// BuildGitHubTagsForTest builds GitHub-specific metadata tags for a test payload.
func (c *WebhookGitHubController) BuildGitHubTagsForTest(
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
	event string,
	payload *GitHubPayload,
) map[string]string {
	return buildGitHubTags(webhook, trigger, event, payload)
}

func buildGitHubTags(
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
	event string,
	payload *GitHubPayload,
) map[string]string {
	tags := map[string]string{
		"webhook_id":   webhook.ID(),
		"webhook_name": webhook.Name(),
		"trigger_id":   trigger.ID(),
		"trigger_name": trigger.Name(),
		"github_event": event,
	}
	if payload.Repository != nil {
		tags["repository"] = payload.Repository.FullName
	}
	if payload.Action != "" {
		tags["github_action"] = payload.Action
	}
	if payload.Sender != nil && strings.TrimSpace(payload.Sender.Login) != "" {
		tags["github_sender"] = strings.TrimSpace(payload.Sender.Login)
		if payload.Sender.ID > 0 {
			tags["github_sender_id"] = strconv.FormatInt(payload.Sender.ID, 10)
		}
		tags["credential_source"] = "github_sender"
	}
	return tags
}

// SessionService returns the session service for external access.
func (c *WebhookGitHubController) SessionService() *WebhookSessionService {
	return c.sessionService
}
