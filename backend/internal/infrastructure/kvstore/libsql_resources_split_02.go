package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func backfillLibSQLSessionRunnerQueryColumns(ctx context.Context, db *sql.DB) error {
	const migration = "materialized-session-runner-columns-v1"
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv_schema_migrations WHERE name = ?`, migration).Scan(&applied); err != nil {
		return fmt.Errorf("inspect libSQL session runner column migration: %w", err)
	}
	if applied == 0 {
		for _, resource := range libSQLResourceTables {
			if resource.labelKey != "agentapi.proxy/session-runner-resource" {
				continue
			}
			statement := fmt.Sprintf(`UPDATE %s SET
runner_pool = COALESCE(json_extract(metadata, '$.labels."agentapi.proxy/session-runner-pool-hash"'), ''),
runner_status = COALESCE(json_extract(metadata, '$.labels."agentapi.proxy/session-runner-status"'), ''),
runner_dind = COALESCE(json_extract(metadata, '$.labels."agentapi.proxy/session-runner-dind"'), '')`, resource.name)
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("backfill libSQL session runner query columns in %s: %w", resource.name, err)
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_kv_schema_migrations(name) VALUES (?)`, migration); err != nil {
			return fmt.Errorf("record libSQL session runner column migration: %w", err)
		}
	}
	const managerMigration = "materialized-session-runner-manager-column-v1"
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv_schema_migrations WHERE name = ?`, managerMigration).Scan(&applied); err != nil {
		return fmt.Errorf("inspect libSQL session runner manager column migration: %w", err)
	}
	if applied != 0 {
		return nil
	}
	for _, resource := range []string{"agentapi_session_runners", "agentapi_session_allocations"} {
		statement := fmt.Sprintf(`UPDATE %s SET runner_manager = COALESCE(json_extract(metadata, '$.labels."agentapi.proxy/session-runner-manager-hash"'), '')`, resource)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill libSQL session runner manager column in %s: %w", resource, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_kv_schema_migrations(name) VALUES (?)`, managerMigration); err != nil {
		return fmt.Errorf("record libSQL session runner manager column migration: %w", err)
	}
	return nil
}

// migrateLibSQLResourceTables moves records created by older releases out of
// the catch-all table. INSERT OR IGNORE plus the correlated DELETE makes the
// migration restart-safe if a process is interrupted between resource groups.
func migrateLibSQLResourceTables(ctx context.Context, db *sql.DB) error {
	for _, resource := range libSQLResourceTables {
		var predicate string
		var args []any
		if resource.labelKey != "" {
			predicate = `json_extract(metadata, '$.labels."' || ? || '"') = ?`
			args = []any{resource.labelKey, resource.labelValue}
		} else {
			predicate = `substr(key, 1, length(?)) = ?`
			args = []any{resource.keyPrefix, resource.keyPrefix}
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin libSQL resource migration: %w", err)
		}
		insert := fmt.Sprintf(`INSERT OR IGNORE INTO %s
(namespace, key, version, value, updated_at, owner_scope, metadata)
SELECT namespace, key, version, value, updated_at, owner_scope, metadata
FROM agentapi_kv WHERE kind = ? AND %s`, resource.name, predicate)
		migrationArgs := append([]any{resource.kind}, args...)
		if _, err = tx.ExecContext(ctx, insert, migrationArgs...); err == nil {
			remove := fmt.Sprintf(`DELETE FROM agentapi_kv WHERE kind = ? AND %s AND EXISTS (
SELECT 1 FROM %s target WHERE target.namespace = agentapi_kv.namespace
AND target.key = agentapi_kv.key)`, predicate, resource.name)
			_, err = tx.ExecContext(ctx, remove, migrationArgs...)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate libSQL resource table %s: %w", resource.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit libSQL resource migration for %s: %w", resource.name, err)
		}
	}
	return nil
}

func (s *LibSQLStore) tableContaining(ctx context.Context, kind Kind, namespace, key string) (string, error) {
	locationKey := libSQLLocationKey(kind, namespace, key)
	if cached, ok := s.tableLocations.Load(locationKey); ok {
		return cached.(string), nil
	}
	var statement strings.Builder
	var args []any
	wroteTable := false
	statement.WriteString("SELECT table_name FROM (")
	tables := allLibSQLDataTables()
	if table, ok := libSQLSessionRunnerTableForKey(kind, key); ok {
		// Keep fallback compatibility for rolling upgrades without probing every
		// unrelated resource table on each runner point read or write.
		tables = []string{table, libSQLFallbackTable}
	}
	for _, table := range tables {
		resource, dedicated := libSQLResourceTableNamed(table)
		if dedicated && resource.kind != kind {
			continue
		}
		if wroteTable {
			statement.WriteString(" UNION ALL ")
		}
		if dedicated {
			fmt.Fprintf(&statement, "SELECT '%s' AS table_name FROM %s WHERE namespace = ? AND key = ?", table, table)
			args = append(args, namespace, key)
		} else {
			fmt.Fprintf(&statement, "SELECT '%s' AS table_name FROM %s WHERE kind = ? AND namespace = ? AND key = ?", table, table)
			args = append(args, kind, namespace, key)
		}
		wroteTable = true
	}
	statement.WriteString(") LIMIT 1")
	var table string
	if err := s.db.QueryRowContext(ctx, statement.String(), args...).Scan(&table); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("locate libSQL resource: %w", err)
	}
	s.tableLocations.Store(locationKey, table)
	return table, nil
}

func libSQLLocationKey(kind Kind, namespace, key string) string {
	return string(kind) + "\x00" + namespace + "\x00" + key
}
