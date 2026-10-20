package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ccplant/ccplant/e2e/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS runs (
 id TEXT PRIMARY KEY, scenario_id TEXT NOT NULL, scenario_description TEXT NOT NULL,
 category_id TEXT NOT NULL, category_description TEXT NOT NULL, kind TEXT NOT NULL,
 environment TEXT NOT NULL, parameters TEXT NOT NULL, status TEXT NOT NULL,
 cancel_requested INTEGER NOT NULL DEFAULT 0, worker_id TEXT NOT NULL DEFAULT '',
 lease_expires_at TEXT, created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT
);
CREATE INDEX IF NOT EXISTS runs_queue ON runs(status, created_at);
CREATE INDEX IF NOT EXISTS runs_filters ON runs(category_id, scenario_id, status);
CREATE TABLE IF NOT EXISTS run_events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, type TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT '', test TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}',
 created_at TEXT NOT NULL, FOREIGN KEY(run_id) REFERENCES runs(id)
);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateRun(ctx context.Context, r domain.Run) error {
	p, _ := json.Marshal(r.Parameters)
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs(id,scenario_id,scenario_description,category_id,category_description,kind,environment,parameters,status,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.ScenarioID, r.ScenarioDescription, r.CategoryID, r.CategoryDescription, r.Kind, r.Environment, string(p), r.Status, format(r.CreatedAt))
	return err
}

func (s *Store) ListRuns(ctx context.Context, category, scenario, status string) ([]domain.Run, error) {
	q := `SELECT ` + runColumns + ` FROM runs WHERE (?='' OR category_id=?) AND (?='' OR scenario_id=?) AND (?='' OR status=?) ORDER BY created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, q, category, category, scenario, scenario, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, id))
}

func (s *Store) Events(ctx context.Context, id string, after int64) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,run_id,type,message,test,payload,created_at FROM run_events WHERE run_id=? AND sequence>? ORDER BY sequence`, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var payload, created string
		if err := rows.Scan(&e.Sequence, &e.RunID, &e.Type, &e.Message, &e.Test, &payload, &created); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(payload), &e.Payload)
		e.CreatedAt = parse(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AddEvent(ctx context.Context, e domain.Event) (int64, error) {
	p, _ := json.Marshal(e.Payload)
	res, err := s.db.ExecContext(ctx, `INSERT INTO run_events(run_id,type,message,test,payload,created_at) VALUES(?,?,?,?,?,?)`, e.RunID, e.Type, e.Message, e.Test, string(p), format(time.Now().UTC()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Cancel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET cancel_requested=1,status=CASE WHEN status='queued' THEN 'cancelled' ELSE status END,finished_at=CASE WHEN status='queued' THEN ? ELSE finished_at END WHERE id=? AND status IN ('queued','running')`, format(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Claim(ctx context.Context, worker string, lease time.Duration, kinds []string) (domain.Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Run{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	placeholders := ""
	args := []any{format(now)}
	for _, k := range kinds {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, k)
	}
	if placeholders == "" {
		return domain.Run{}, sql.ErrNoRows
	}
	q := `SELECT ` + runColumns + ` FROM runs WHERE status='queued' AND cancel_requested=0 AND created_at<=? AND kind IN (` + placeholders + `) ORDER BY created_at LIMIT 1`
	r, err := scanRun(tx.QueryRowContext(ctx, q, args...))
	if err != nil {
		return domain.Run{}, err
	}
	expires := now.Add(lease)
	res, err := tx.ExecContext(ctx, `UPDATE runs SET status='running',worker_id=?,lease_expires_at=?,started_at=? WHERE id=? AND status='queued'`, worker, format(expires), format(now), r.ID)
	if err != nil {
		return domain.Run{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return domain.Run{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return domain.Run{}, err
	}
	r.Status = "running"
	r.WorkerID = worker
	r.LeaseExpiresAt = &expires
	r.StartedAt = &now
	return r, nil
}

func (s *Store) Heartbeat(ctx context.Context, id, worker string, lease time.Duration) (bool, error) {
	expires := time.Now().UTC().Add(lease)
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET lease_expires_at=? WHERE id=? AND worker_id=? AND status='running'`, format(expires), id, worker)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, sql.ErrNoRows
	}
	r, err := s.GetRun(ctx, id)
	return r.CancelRequested, err
}
func (s *Store) Complete(ctx context.Context, id, worker, status string) error {
	if status != "passed" && status != "failed" && status != "infra_error" && status != "cancelled" {
		return fmt.Errorf("invalid status")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?,finished_at=?,lease_expires_at=NULL WHERE id=? AND worker_id=? AND status='running'`, status, format(time.Now().UTC()), id, worker)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

const runColumns = `id,scenario_id,scenario_description,category_id,category_description,kind,environment,parameters,status,cancel_requested,worker_id,lease_expires_at,created_at,started_at,finished_at`

type scanner interface{ Scan(...any) error }

func scanRun(s scanner) (domain.Run, error) {
	var r domain.Run
	var p, lease, created, started, finished sql.NullString
	var cancel int
	err := s.Scan(&r.ID, &r.ScenarioID, &r.ScenarioDescription, &r.CategoryID, &r.CategoryDescription, &r.Kind, &r.Environment, &p, &r.Status, &cancel, &r.WorkerID, &lease, &created, &started, &finished)
	if err != nil {
		return r, err
	}
	r.CancelRequested = cancel != 0
	json.Unmarshal([]byte(p.String), &r.Parameters)
	r.CreatedAt = parse(created.String)
	r.LeaseExpiresAt = parsePtr(lease)
	r.StartedAt = parsePtr(started)
	r.FinishedAt = parsePtr(finished)
	return r, nil
}
func format(t time.Time) string { return t.Format(time.RFC3339Nano) }
func parse(v string) time.Time  { t, _ := time.Parse(time.RFC3339Nano, v); return t }
func parsePtr(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t := parse(v.String)
	return &t
}

var _ = errors.Is
