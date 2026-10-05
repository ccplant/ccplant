package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/tursodatabase/libsql-client-go/libsql"
)

type LibSQLSessionCountRepository struct{ db *sql.DB }

var _ portrepos.SessionCountRepository = (*LibSQLSessionCountRepository)(nil)

func NewLibSQLSessionCountRepository(ctx context.Context, databaseURL, authToken string) (*LibSQLSessionCountRepository, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("session count database URL is required")
	}
	opts := []libsql.Option{}
	if authToken != "" {
		opts = append(opts, libsql.WithAuthToken(authToken))
	}
	connector, err := libsql.NewConnector(databaseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("create session count libSQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	r := &LibSQLSessionCountRepository{db: db}
	if err := r.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *LibSQLSessionCountRepository) initialize(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agentapi_session_count_dimensions (
pool TEXT NOT NULL, principal_id TEXT NOT NULL,
PRIMARY KEY (pool, principal_id))`,
		`CREATE TABLE IF NOT EXISTS agentapi_session_status_events (
event_id TEXT PRIMARY KEY, occurred_at TEXT NOT NULL, session_id TEXT NOT NULL,
pool TEXT NOT NULL, scope TEXT NOT NULL, principal_id TEXT NOT NULL, status TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS agentapi_session_status_events_principal_time
ON agentapi_session_status_events(principal_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_session_status_events_pool_time
ON agentapi_session_status_events(pool, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_session_status_events_session_time
ON agentapi_session_status_events(session_id, occurred_at)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize session count schema: %w", err)
		}
	}
	return nil
}

func (r *LibSQLSessionCountRepository) SaveEvent(ctx context.Context, event entities.SessionStatusUsageEvent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session status event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_session_count_dimensions
(pool,principal_id) VALUES (?,?)`, event.Pool, event.PrincipalID); err != nil {
		return fmt.Errorf("save session count dimension: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_session_status_events
(event_id,occurred_at,session_id,pool,scope,principal_id,status) VALUES (?,?,?,?,?,?,?)`,
		event.EventID, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.SessionID,
		event.Pool, event.Scope, event.PrincipalID, event.Status)
	if err != nil {
		return fmt.Errorf("save session status event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session status event: %w", err)
	}
	return nil
}

func (r *LibSQLSessionCountRepository) Close() error { return r.db.Close() }
