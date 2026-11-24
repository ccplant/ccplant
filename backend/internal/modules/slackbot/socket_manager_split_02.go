package slackbot

import (
	"log"
	"strings"
)

func (m *SlackSocketManager) stopWorker(key string) {
	m.mu.Lock()
	entry, ok := m.running[key]
	if ok {
		delete(m.running, key)
	}
	m.mu.Unlock()

	if ok {
		log.Printf("[SOCKET_MANAGER] Stopping worker for botKey=%s", key)
		entry.cancel()
		<-entry.done
	}
}

// stopAll cancels all running workers
func (m *SlackSocketManager) stopAll() {
	m.mu.Lock()
	keys := make([]string, 0, len(m.running))
	for k := range m.running {
		keys = append(keys, k)
	}
	m.mu.Unlock()

	for _, key := range keys {
		m.stopWorker(key)
	}
}

// sanitizeLeaseName converts a bot key to a valid Kubernetes Lease name component.
// Kubernetes Lease names must be lowercase alphanumeric or '-'.
func sanitizeLeaseName(key string) string {
	// Replace any non-alphanumeric characters with '-'
	var result strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}
	}
	// Trim leading/trailing dashes
	s := strings.Trim(result.String(), "-")
	if s == "" {
		s = "unknown"
	}
	return s
}
