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
}

var libSQLResourceTables = []libSQLResourceTable{
	{name: "agentapi_settings", labelKey: "agentapi.proxy/settings", labelValue: "true"},
	{name: "agentapi_credentials", labelKey: "agentapi.proxy/credentials", labelValue: "true"},
	{name: "agentapi_shares", labelKey: "agentapi.proxy/shares", labelValue: "true"},
	{name: "agentapi_team_configs", labelKey: "agentapi.proxy/team-config", labelValue: "true"},
	{name: "agentapi_personal_api_keys", labelKey: "agentapi.proxy/personal-api-key", labelValue: "true"},
	{name: "agentapi_api_tokens", labelKey: "agentapi.proxy/api-token", labelValue: "true"},
	{name: "agentapi_local_users", labelKey: "agentapi.proxy/local-user", labelValue: "true"},
	{name: "agentapi_sandbox_policies", labelKey: "agentapi.proxy/type", labelValue: "sandbox-policy"},
	{name: "agentapi_sandbox_domains", labelKey: "agentapi.proxy/type", labelValue: "sandbox-domains"},
	{name: "agentapi_session_routes", labelKey: "agentapi.proxy/session-route", labelValue: "true"},
	{name: "agentapi_user_files", labelKey: "agentapi.proxy/user-files", labelValue: "true"},
	{name: "agentapi_session_profiles", labelKey: "agentapi.proxy/session-profile", labelValue: "true"},
	{name: "agentapi_slackbots", labelKey: "agentapi.proxy/slackbot", labelValue: "true"},
	{name: "agentapi_webhooks", labelKey: "agentapi.proxy/webhook", labelValue: "true"},
	{name: "agentapi_user_team_mappings", labelKey: "agentapi.proxy/type", labelValue: "user-team-mapping"},
	{name: "agentapi_codex_auth_attempts", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "true"},
	{name: "agentapi_codex_auth_locks", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "lock"},
	{name: "agentapi_schedules", labelKey: "agentapi.proxy/schedule", labelValue: "true"},
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
metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/v1","labels":{}}' CHECK (json_valid(metadata)),
PRIMARY KEY (kind, namespace, key))`, table)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize libSQL resource table %s: %w", table, err)
		}
		if table != libSQLFallbackTable {
			index := table + "_owner_scope_lookup"
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_scope, kind, namespace)`, index, table)); err != nil {
				return fmt.Errorf("initialize libSQL resource index %s: %w", table, err)
			}
		}
	}
	return migrateLibSQLResourceTables(ctx, db)
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
