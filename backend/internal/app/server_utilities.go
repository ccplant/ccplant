package app

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	corerepo "github.com/takutakahashi/agentapi-proxy/internal/core/repository"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func ExtractRepositoryInfo(tags map[string]string, cloneDir string) *entities.RepositoryInfo {
	repoInfo, err := corerepo.ExtractInfo(tags, cloneDir)
	if err != nil {
		log.Printf("Failed to extract repository full name: %v", err)
		return nil
	}
	return repoInfo
}

func extractRepoFullNameFromURL(repoURL string) (string, error) {
	return corerepo.FullNameFromURL(repoURL)
}

// extractRepositoryInfo extracts repository information from tags (internal method with verbose logging)
func (s *Server) extractRepositoryInfo(sessionID string, tags map[string]string) *entities.RepositoryInfo {
	if tags == nil {
		return nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil
	}

	// Only process repository URLs that look like valid GitHub URLs
	if !corerepo.IsValidURL(repoURL) {
		if s.verbose {
			log.Printf("Repository tag found: %s, but it's not a valid repository URL. Skipping repository setup.", repoURL)
		}
		return nil
	}

	if s.verbose {
		log.Printf("Repository tag found: %s. Will pass to script as parameters.", repoURL)
	}

	repoInfo := ExtractRepositoryInfo(tags, sessionID)
	if repoInfo != nil && s.verbose {
		log.Printf("Extracted repository info - FullName: %s, CloneDir: %s", repoInfo.FullName, sessionID)
	}

	return repoInfo
}

// isValidRepositoryURL checks if a repository URL is valid for GitHub
// cleanupDefunctProcesses periodically checks for and cleans up defunct processes
func (s *Server) cleanupDefunctProcesses() {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupDefunctProcessesOnce()
	}
}

// cleanupDefunctProcessesOnce performs a single cleanup of defunct processes
func (s *Server) cleanupDefunctProcessesOnce() {
	// Find defunct processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get process list for defunct cleanup: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	defunctCount := 0

	for _, line := range lines {
		if strings.Contains(line, "<defunct>") || strings.Contains(line, " Z ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pidStr := fields[1]
				if pid, err := strconv.Atoi(pidStr); err == nil {
					// Try to reap the defunct process by sending signal 0
					// This doesn't actually send a signal but checks if we can access the process
					if err := syscall.Kill(pid, 0); err != nil {
						// Process is already gone or we can't access it
						continue
					}
					defunctCount++
				}
			}
		}
	}

	if defunctCount > 0 {
		log.Printf("Found %d defunct processes during periodic cleanup", defunctCount)

		// Try to trigger process reaping by the init system
		// This is a best-effort approach
		if defunctCount > 10 {
			log.Printf("High number of defunct processes detected (%d). Consider investigating process management.", defunctCount)
		}
	}
}

// cleanupExpiredShares periodically removes expired session shares
func (s *Server) cleanupExpiredShares() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if s.shareRepo != nil {
			count, err := s.shareRepo.CleanupExpired()
			if err != nil {
				log.Printf("Failed to cleanup expired shares: %v", err)
			} else if count > 0 {
				log.Printf("Cleaned up %d expired session shares", count)
			}
		}
	}
}

// buildStatusEventRepository constructs the appropriate StatusEventRepository
// based on the config:
//   - When cfg.Redis.Addr is non-empty a real RedisStatusRepository is returned.
//   - Otherwise a NoopStatusRepository is returned so existing single-pod
//     behaviour is preserved without any code changes in the callers.
