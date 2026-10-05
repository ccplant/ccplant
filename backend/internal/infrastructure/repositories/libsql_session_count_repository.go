package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

func (r *LibSQLSessionCountRepository) ListRuntimeEvents(ctx context.Context, query entities.SessionRuntimeQuery) ([]entities.SessionStatusUsageEvent, error) {
	filters := []string{"principal_id = ?"}
	args := []interface{}{query.PrincipalID}
	if query.Pool != "" {
		filters = append(filters, "pool = ?")
		args = append(args, query.Pool)
	}
	where := strings.Join(filters, " AND ")
	statement := `WITH ranked AS (
SELECT event_id,occurred_at,session_id,pool,scope,principal_id,status,
ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY occurred_at DESC,event_id DESC) AS position
FROM agentapi_session_status_events WHERE ` + where + ` AND occurred_at < ?),
selected AS (
SELECT event_id,occurred_at,session_id,pool,scope,principal_id,status FROM ranked WHERE position = 1
UNION ALL
SELECT event_id,occurred_at,session_id,pool,scope,principal_id,status
FROM agentapi_session_status_events WHERE ` + where + ` AND occurred_at >= ? AND occurred_at < ?)
SELECT event_id,occurred_at,session_id,pool,scope,principal_id,status
FROM selected ORDER BY session_id,occurred_at,event_id`
	queryArgs := append(append([]interface{}{}, args...), query.From.UTC().Format(time.RFC3339Nano))
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, query.From.UTC().Format(time.RFC3339Nano), query.To.UTC().Format(time.RFC3339Nano))
	rows, err := r.db.QueryContext(ctx, statement, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list session runtime events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []entities.SessionStatusUsageEvent{}
	for rows.Next() {
		var event entities.SessionStatusUsageEvent
		var occurredAt string
		if err := rows.Scan(&event.EventID, &occurredAt, &event.SessionID, &event.Pool, &event.Scope, &event.PrincipalID, &event.Status); err != nil {
			return nil, fmt.Errorf("scan session runtime event: %w", err)
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse session runtime event time: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *LibSQLSessionCountRepository) RuntimeCoverageStart(ctx context.Context, principalID string) (*time.Time, error) {
	var value sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(occurred_at) FROM agentapi_session_status_events WHERE principal_id = ?`, principalID).Scan(&value); err != nil {
		return nil, fmt.Errorf("read session runtime coverage: %w", err)
	}
	if !value.Valid {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, fmt.Errorf("parse session runtime coverage: %w", err)
	}
	return &parsed, nil
}
