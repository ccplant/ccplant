package repositories

import (
	"crypto/sha256"
	"fmt"
)

// hashID returns a short stable hash suitable for Kubernetes label values.
func hashID(id string) string {
	hash := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", hash[:8])
}
