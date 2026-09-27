package repositories

import (
	"context"
	"fmt"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// NewSessionCountRepository selects the configured statistics database without
// exposing a concrete database implementation to the worker.
func NewSessionCountRepository(ctx context.Context, backend, databaseURL, authToken string) (portrepos.SessionCountRepository, error) {
	switch backend {
	case "libsql":
		return NewLibSQLSessionCountRepository(ctx, databaseURL, authToken)
	default:
		return nil, fmt.Errorf("unsupported session count backend %q", backend)
	}
}
