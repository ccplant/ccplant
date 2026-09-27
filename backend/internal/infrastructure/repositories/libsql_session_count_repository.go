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
		`CREATE TABLE IF NOT EXISTS agentapi_session_count_samples (
sampled_at TEXT NOT NULL, pool TEXT NOT NULL, principal_id TEXT NOT NULL,
active_count INTEGER NOT NULL, running_count INTEGER NOT NULL,
PRIMARY KEY (sampled_at, pool, principal_id))`,
		`ALTER TABLE agentapi_session_count_samples ADD COLUMN all_count INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS agentapi_session_count_samples_principal_time
ON agentapi_session_count_samples(principal_id, sampled_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_session_count_samples_pool_time
ON agentapi_session_count_samples(pool, sampled_at)`,
	}
	for index, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			if index == 2 && isDuplicateColumnError(err) {
				continue
			}
			return fmt.Errorf("initialize session count schema: %w", err)
		}
	}
	return nil
}

func (r *LibSQLSessionCountRepository) ListDimensions(ctx context.Context) ([]entities.SessionCountDimension, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT pool, principal_id FROM agentapi_session_count_dimensions ORDER BY pool, principal_id`)
	if err != nil {
		return nil, fmt.Errorf("list session count dimensions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []entities.SessionCountDimension{}
	for rows.Next() {
		var dimension entities.SessionCountDimension
		if err := rows.Scan(&dimension.Pool, &dimension.PrincipalID); err != nil {
			return nil, fmt.Errorf("scan session count dimension: %w", err)
		}
		result = append(result, dimension)
	}
	return result, rows.Err()
}

func (r *LibSQLSessionCountRepository) SaveSnapshot(ctx context.Context, sampledAt time.Time, samples []entities.SessionCountSample) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session count snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	type count struct{ all, active, running int }
	latest := map[entities.SessionCountDimension]count{}
	rows, err := tx.QueryContext(ctx, `SELECT sample.pool,sample.principal_id,sample.all_count,sample.active_count,sample.running_count
FROM agentapi_session_count_samples sample
JOIN (SELECT pool,principal_id,MAX(sampled_at) AS sampled_at
      FROM agentapi_session_count_samples GROUP BY pool,principal_id) newest
ON sample.pool=newest.pool AND sample.principal_id=newest.principal_id AND sample.sampled_at=newest.sampled_at`)
	if err != nil {
		return fmt.Errorf("list latest session counts: %w", err)
	}
	for rows.Next() {
		var dimension entities.SessionCountDimension
		var value count
		if err := rows.Scan(&dimension.Pool, &dimension.PrincipalID, &value.all, &value.active, &value.running); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan latest session count: %w", err)
		}
		latest[dimension] = value
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close latest session counts: %w", err)
	}
	timestamp := sampledAt.UTC().Format(time.RFC3339Nano)
	for _, sample := range samples {
		previous, exists := latest[sample.SessionCountDimension]
		if exists && previous.all == sample.AllCount && previous.active == sample.ActiveCount && previous.running == sample.RunningCount {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_session_count_dimensions (pool,principal_id) VALUES (?,?)`, sample.Pool, sample.PrincipalID); err != nil {
			return fmt.Errorf("save session count dimension: %w", err)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO agentapi_session_count_samples
(sampled_at,pool,principal_id,all_count,active_count,running_count) VALUES (?,?,?,?,?,?)
ON CONFLICT(sampled_at,pool,principal_id) DO UPDATE SET
all_count=excluded.all_count,active_count=excluded.active_count,running_count=excluded.running_count`,
			timestamp, sample.Pool, sample.PrincipalID, sample.AllCount, sample.ActiveCount, sample.RunningCount)
		if err != nil {
			return fmt.Errorf("save session count sample: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session count snapshot: %w", err)
	}
	return nil
}

func (r *LibSQLSessionCountRepository) Close() error { return r.db.Close() }

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}
