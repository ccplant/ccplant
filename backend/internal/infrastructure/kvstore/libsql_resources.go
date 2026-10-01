package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// libSQLResourceTable is a physical persistence boundary. Repositories keep
// using the Kubernetes compatibility API, while libSQL stores each application
// resource independently instead of mixing every document in agentapi_kv.
type libSQLResourceTable struct {
	name, labelKey, labelValue, keyPrefix string
	indexColumns                          []string
}

var libSQLQueryColumns = []string{
	"user_principal_id", "team_principal_id", "owner_principal_id", "resource_scope",
	"slack_channel", "slack_thread_ts",
}

var libSQLLabelColumns = map[string]string{
	"agentapi.proxy/user-id":                           "user_principal_id",
	"agentapi.proxy/session-profile-user-id":           "user_principal_id",
	"agentapi.proxy/session-route-user-id":             "user_principal_id",
	"agentapi.proxy/slackbot-user-id":                  "user_principal_id",
	"agentapi.proxy/webhook-user-id":                   "user_principal_id",
	"agentapi.proxy/schedule-user-id":                  "user_principal_id",
	"agentapi.proxy/team-id":                           "team_principal_id",
	"agentapi.proxy/session-profile-team-id-hash":      "team_principal_id",
	"agentapi.proxy/session-route-team-id-hash":        "team_principal_id",
	"agentapi.proxy/slackbot-team-id-hash":             "team_principal_id",
	"agentapi.proxy/webhook-team-id-hash":              "team_principal_id",
	"agentapi.proxy/team-hash":                         "team_principal_id",
	"agentapi.proxy/schedule-team-id":                  "team_principal_id",
	"agentapi.proxy/api-token-owner":                   "owner_principal_id",
	"agentapi.proxy/owner-hash":                        "owner_principal_id",
	"agentapi.proxy/api-token-scope":                   "resource_scope",
	"agentapi.proxy/scope":                             "resource_scope",
	"agentapi.proxy/session-profile-scope":             "resource_scope",
	"agentapi.proxy/session-route-scope":               "resource_scope",
	"agentapi.proxy/slackbot-scope":                    "resource_scope",
	"agentapi.proxy/webhook-scope":                     "resource_scope",
	"agentapi.proxy/schedule-scope":                    "resource_scope",
	"agentapi.proxy/session-route-tag-slack_channel":   "slack_channel",
	"agentapi.proxy/session-route-tag-slack_thread_ts": "slack_thread_ts",
}

var libSQLResourceTables = []libSQLResourceTable{
	{name: "agentapi_settings", labelKey: "agentapi.proxy/settings", labelValue: "true"},
	{name: "agentapi_credentials", labelKey: "agentapi.proxy/credentials", labelValue: "true"},
	{name: "agentapi_shares", labelKey: "agentapi.proxy/shares", labelValue: "true"},
	{name: "agentapi_team_configs", labelKey: "agentapi.proxy/team-config", labelValue: "true", indexColumns: []string{"team_principal_id"}},
	{name: "agentapi_personal_api_keys", labelKey: "agentapi.proxy/personal-api-key", labelValue: "true", indexColumns: []string{"user_principal_id"}},
	{name: "agentapi_api_tokens", labelKey: "agentapi.proxy/api-token", labelValue: "true", indexColumns: []string{"owner_principal_id", "resource_scope"}},
	{name: "agentapi_local_users", labelKey: "agentapi.proxy/local-user", labelValue: "true"},
	{name: "agentapi_sandbox_policies", labelKey: "agentapi.proxy/type", labelValue: "sandbox-policy", indexColumns: []string{"owner_principal_id", "team_principal_id", "resource_scope"}},
	{name: "agentapi_sandbox_domains", labelKey: "agentapi.proxy/type", labelValue: "sandbox-domains"},
	{name: "agentapi_session_routes", labelKey: "agentapi.proxy/session-route", labelValue: "true", indexColumns: []string{"user_principal_id", "team_principal_id", "resource_scope", "slack_channel", "slack_thread_ts"}},
	{name: "agentapi_user_files", labelKey: "agentapi.proxy/user-files", labelValue: "true"},
	{name: "agentapi_session_profiles", labelKey: "agentapi.proxy/session-profile", labelValue: "true", indexColumns: []string{"user_principal_id", "team_principal_id", "resource_scope"}},
	{name: "agentapi_slackbots", labelKey: "agentapi.proxy/slackbot", labelValue: "true", indexColumns: []string{"user_principal_id", "team_principal_id", "resource_scope"}},
	{name: "agentapi_webhooks", labelKey: "agentapi.proxy/webhook", labelValue: "true", indexColumns: []string{"user_principal_id", "team_principal_id", "resource_scope"}},
	{name: "agentapi_user_team_mappings", labelKey: "agentapi.proxy/type", labelValue: "user-team-mapping"},
	{name: "agentapi_codex_auth_attempts", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "true"},
	{name: "agentapi_codex_auth_locks", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "lock"},
	{name: "agentapi_schedules", labelKey: "agentapi.proxy/schedule", labelValue: "true", indexColumns: []string{"user_principal_id", "team_principal_id", "resource_scope"}},
	{name: "agentapi_system_settings", keyPrefix: "agentapi-admin-system-settings-"},
}

const libSQLFallbackTable = "agentapi_kv"

func allLibSQLDataTables() []string {
	tables := make([]string, 0, len(libSQLResourceTables)+1)
	for _, resource := range libSQLResourceTables {
		tables = append(tables, resource.name)
	}
	return append(tables, libSQLFallbackTable)
}

func libSQLTablesForSelector(selector labels.Selector) []string {
	requirements, selectable := selector.Requirements()
	if !selectable {
		return allLibSQLDataTables()
	}
	for _, requirement := range requirements {
		if requirement.Operator() != selection.Equals && requirement.Operator() != selection.DoubleEquals && requirement.Operator() != selection.In {
			continue
		}
		values := requirement.Values()
		for _, resource := range libSQLResourceTables {
			if resource.labelKey == requirement.Key() && values.Has(resource.labelValue) {
				return []string{resource.name}
			}
		}
	}
	return allLibSQLDataTables()
}

func libSQLTableForRecord(record Record) string {
	for _, resource := range libSQLResourceTables {
		if resource.labelKey != "" && record.Labels[resource.labelKey] == resource.labelValue {
			return resource.name
		}
		if resource.keyPrefix != "" && strings.HasPrefix(record.Key, resource.keyPrefix) {
			return resource.name
		}
	}
	return libSQLFallbackTable
}

func ensureLibSQLResourceTables(ctx context.Context, db *sql.DB) error {
	for _, table := range allLibSQLDataTables() {
		statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
owner_scope TEXT NOT NULL DEFAULT '',
user_principal_id TEXT NOT NULL DEFAULT '', team_principal_id TEXT NOT NULL DEFAULT '',
owner_principal_id TEXT NOT NULL DEFAULT '', resource_scope TEXT NOT NULL DEFAULT '',
slack_channel TEXT NOT NULL DEFAULT '', slack_thread_ts TEXT NOT NULL DEFAULT '',
metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/v1","labels":{}}' CHECK (json_valid(metadata)),
PRIMARY KEY (kind, namespace, key))`, table)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize libSQL resource table %s: %w", table, err)
		}
		if err := ensureLibSQLQueryColumns(ctx, db, table); err != nil {
			return err
		}
		columns := []string{"owner_scope"}
		for _, resource := range libSQLResourceTables {
			if resource.name == table {
				columns = append(columns, resource.indexColumns...)
				break
			}
		}
		if table == libSQLFallbackTable {
			columns = append(columns, libSQLQueryColumns...)
		}
		for _, column := range columns {
			index := table + "_" + column + "_lookup"
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s, kind, namespace)`, index, table, column)); err != nil {
				return fmt.Errorf("initialize libSQL resource index %s: %w", index, err)
			}
		}
	}
	if err := migrateLibSQLResourceTables(ctx, db); err != nil {
		return err
	}
	return backfillLibSQLQueryColumns(ctx, db)
}

func ensureLibSQLQueryColumns(ctx context.Context, db *sql.DB, table string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect libSQL resource table %s: %w", table, err)
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range libSQLQueryColumns {
		if !existing[column] {
			if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''", table, column)); err != nil {
				// API, worker, and session-manager processes can initialize the same
				// remote database concurrently during a rolling deployment.
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("add libSQL query column %s.%s: %w", table, column, err)
				}
			}
		}
	}
	return nil
}

func libSQLQueryColumnValues(metadata []byte) ([]string, error) {
	labels, err := unmarshalRecordMetadata(metadata)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(libSQLQueryColumns))
	for label, column := range libSQLLabelColumns {
		if value := labels[label]; value != "" {
			values[column] = value
		}
	}
	result := make([]string, len(libSQLQueryColumns))
	for i, column := range libSQLQueryColumns {
		result[i] = values[column]
	}
	return result, nil
}

func backfillLibSQLQueryColumns(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agentapi_kv_schema_migrations (
name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("initialize libSQL schema migration table: %w", err)
	}
	const migration = "materialized-query-columns-v1"
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv_schema_migrations WHERE name = ?`, migration).Scan(&applied); err != nil {
		return fmt.Errorf("inspect libSQL query column migration: %w", err)
	}
	if applied != 0 {
		return nil
	}
	for _, table := range allLibSQLDataTables() {
		rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT kind, namespace, key, metadata FROM %s", table))
		if err != nil {
			return err
		}
		type pending struct {
			kind, namespace, key string
			values               []string
		}
		var updates []pending
		for rows.Next() {
			var item pending
			var metadata []byte
			if err := rows.Scan(&item.kind, &item.namespace, &item.key, &metadata); err != nil {
				_ = rows.Close()
				return err
			}
			item.values, err = libSQLQueryColumnValues(metadata)
			if err != nil {
				_ = rows.Close()
				return err
			}
			updates = append(updates, item)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		set := strings.Join(libSQLQueryColumns, " = ?, ") + " = ?"
		for _, item := range updates {
			args := make([]any, 0, len(item.values)+3)
			for _, value := range item.values {
				args = append(args, value)
			}
			args = append(args, item.kind, item.namespace, item.key)
			if _, err := db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE kind = ? AND namespace = ? AND key = ?", table, set), args...); err != nil {
				return fmt.Errorf("backfill libSQL query columns in %s: %w", table, err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_kv_schema_migrations(name) VALUES (?)`, migration); err != nil {
		return fmt.Errorf("record libSQL query column migration: %w", err)
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
(kind, namespace, key, version, value, updated_at, owner_scope, metadata)
SELECT kind, namespace, key, version, value, updated_at, owner_scope, metadata
FROM agentapi_kv WHERE %s`, resource.name, predicate)
		if _, err = tx.ExecContext(ctx, insert, args...); err == nil {
			remove := fmt.Sprintf(`DELETE FROM agentapi_kv WHERE %s AND EXISTS (
SELECT 1 FROM %s target WHERE target.kind = agentapi_kv.kind
AND target.namespace = agentapi_kv.namespace AND target.key = agentapi_kv.key)`, predicate, resource.name)
			_, err = tx.ExecContext(ctx, remove, args...)
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
	var statement strings.Builder
	var args []any
	statement.WriteString("SELECT table_name FROM (")
	for i, table := range allLibSQLDataTables() {
		if i > 0 {
			statement.WriteString(" UNION ALL ")
		}
		fmt.Fprintf(&statement, "SELECT '%s' AS table_name FROM %s WHERE kind = ? AND namespace = ? AND key = ?", table, table)
		args = append(args, kind, namespace, key)
	}
	statement.WriteString(") LIMIT 1")
	var table string
	if err := s.db.QueryRowContext(ctx, statement.String(), args...).Scan(&table); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("locate libSQL resource: %w", err)
	}
	return table, nil
}
