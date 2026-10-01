package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tursodatabase/libsql-client-go/libsql"
)

const (
	fallbackTable = "agentapi_kv"
	markerValue   = "ccplant-libsql-resource-migration-check/v1"
)

type resource struct {
	table, kind, labelKey, labelValue, key string
}

var resources = []resource{
	{table: "agentapi_settings", kind: "secret", labelKey: "agentapi.proxy/settings", labelValue: "true", key: "settings"},
	{table: "agentapi_credentials", kind: "secret", labelKey: "agentapi.proxy/credentials", labelValue: "true", key: "credentials"},
	{table: "agentapi_shares", kind: "secret", labelKey: "agentapi.proxy/shares", labelValue: "true", key: "share"},
	{table: "agentapi_team_configs", kind: "secret", labelKey: "agentapi.proxy/team-config", labelValue: "true", key: "team-config"},
	{table: "agentapi_personal_api_keys", kind: "secret", labelKey: "agentapi.proxy/personal-api-key", labelValue: "true", key: "personal-api-key"},
	{table: "agentapi_api_tokens", kind: "secret", labelKey: "agentapi.proxy/api-token", labelValue: "true", key: "api-token"},
	{table: "agentapi_local_users", kind: "secret", labelKey: "agentapi.proxy/local-user", labelValue: "true", key: "local-user"},
	{table: "agentapi_sandbox_policies", kind: "configmap", labelKey: "agentapi.proxy/type", labelValue: "sandbox-policy", key: "sandbox-policy"},
	{table: "agentapi_sandbox_domains", kind: "configmap", labelKey: "agentapi.proxy/type", labelValue: "sandbox-domains", key: "sandbox-domains"},
	{table: "agentapi_session_routes", kind: "secret", labelKey: "agentapi.proxy/session-route", labelValue: "true", key: "session-route"},
	{table: "agentapi_user_files", kind: "secret", labelKey: "agentapi.proxy/user-files", labelValue: "true", key: "user-files"},
	{table: "agentapi_session_profiles", kind: "secret", labelKey: "agentapi.proxy/session-profile", labelValue: "true", key: "session-profile"},
	{table: "agentapi_slackbots", kind: "secret", labelKey: "agentapi.proxy/slackbot", labelValue: "true", key: "slackbot"},
	{table: "agentapi_webhooks", kind: "secret", labelKey: "agentapi.proxy/webhook", labelValue: "true", key: "webhook"},
	{table: "agentapi_user_team_mappings", kind: "configmap", labelKey: "agentapi.proxy/type", labelValue: "user-team-mapping", key: "user-team-mapping"},
	{table: "agentapi_codex_auth_attempts", kind: "secret", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "true", key: "codex-auth-attempt"},
	{table: "agentapi_codex_auth_locks", kind: "secret", labelKey: "agentapi.proxy/codex-device-auth-attempt", labelValue: "lock", key: "codex-auth-lock"},
	{table: "agentapi_schedules", kind: "secret", labelKey: "agentapi.proxy/schedule", labelValue: "true", key: "schedule"},
	{table: "agentapi_system_settings", kind: "secret", key: "agentapi-admin-system-settings-check"},
}

type snapshotRow struct {
	Table   string         `json:"table"`
	Columns map[string]any `json:"columns"`
}

func main() {
	mode := flag.String("mode", "", "snapshot, prepare, verify, or cleanup")
	namespace := flag.String("namespace", "", "isolated verification namespace")
	output := flag.String("output", "", "snapshot output path")
	flag.Parse()
	if *mode == "" {
		fatal(errors.New("mode is required"))
	}
	databaseURL := os.Getenv("AGENTAPI_KV_STORE_DATABASE_URL")
	authToken := os.Getenv("AGENTAPI_KV_STORE_AUTH_TOKEN")
	if databaseURL == "" {
		fatal(errors.New("AGENTAPI_KV_STORE_DATABASE_URL is required"))
	}
	connector, err := libsql.NewConnector(databaseURL, libsql.WithAuthToken(authToken))
	if err != nil {
		fatal(err)
	}
	db := sql.OpenDB(connector)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch *mode {
	case "snapshot":
		if *output == "" {
			fatal(errors.New("output is required for snapshot"))
		}
		err = snapshot(ctx, db, *output)
	case "prepare":
		err = requireNamespace(*namespace)
		if err == nil {
			err = prepare(ctx, db, *namespace)
		}
	case "verify":
		err = requireNamespace(*namespace)
		if err == nil {
			err = verify(ctx, db, *namespace)
		}
	case "cleanup":
		err = requireNamespace(*namespace)
		if err == nil {
			err = cleanup(ctx, db, *namespace)
		}
	default:
		err = fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		fatal(err)
	}
}

func requireNamespace(namespace string) error {
	if !strings.HasPrefix(namespace, "migration-verification-") {
		return errors.New("verification namespace must start with migration-verification-")
	}
	return nil
}

func snapshot(ctx context.Context, db *sql.DB, output string) error {
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	writer := bufio.NewWriter(file)
	defer func() { _ = writer.Flush() }()

	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	for _, table := range tables {
		if err := snapshotTable(ctx, db, table, encoder); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	return file.Sync()
}

func snapshotTable(ctx context.Context, db *sql.DB, table string, encoder *json.Encoder) error {
	if !safeIdentifier(table) {
		return fmt.Errorf("unsafe table name %q", table)
	}
	rows, err := db.QueryContext(ctx, `SELECT * FROM "`+table+`"`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return err
		}
		mapped := make(map[string]any, len(columns))
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				mapped[columns[i]] = map[string]string{"base64": base64.StdEncoding.EncodeToString(bytes)}
			} else {
				mapped[columns[i]] = value
			}
		}
		if err := encoder.Encode(snapshotRow{Table: table, Columns: mapped}); err != nil {
			return err
		}
	}
	return rows.Err()
}

func safeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func prepare(ctx context.Context, db *sql.DB, namespace string) error {
	if err := cleanup(ctx, db, namespace); err != nil {
		return err
	}
	for index, resource := range resources {
		labels := map[string]string{}
		if resource.labelKey != "" {
			labels[resource.labelKey] = resource.labelValue
		}
		metadata, err := json.Marshal(map[string]any{"format": "agentapi-kv-metadata/v1", "labels": labels})
		if err != nil {
			return err
		}
		value := []byte(fmt.Sprintf("%s:%s", markerValue, resource.table))
		_, err = db.ExecContext(ctx, `INSERT INTO agentapi_kv
(kind, namespace, key, version, value, updated_at, owner_scope, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, resource.kind, namespace, resource.key, index+11, value,
			time.Now().UTC().Format(time.RFC3339Nano), "verification", metadata)
		if err != nil {
			return fmt.Errorf("prepare %s: %w", resource.table, err)
		}
	}
	fmt.Printf("prepared %d resource fixtures in %s\n", len(resources), namespace)
	return nil
}

func verify(ctx context.Context, db *sql.DB, namespace string) error {
	var verified []string
	for index, resource := range resources {
		if !safeIdentifier(resource.table) {
			return fmt.Errorf("unsafe table name %q", resource.table)
		}
		var version int
		var value []byte
		var ownerScope string
		query := `SELECT version, value, owner_scope FROM "` + resource.table + `" WHERE kind = ? AND namespace = ? AND key = ?`
		if err := db.QueryRowContext(ctx, query, resource.kind, namespace, resource.key).Scan(&version, &value, &ownerScope); err != nil {
			return fmt.Errorf("verify %s: %w", resource.table, err)
		}
		wantValue := fmt.Sprintf("%s:%s", markerValue, resource.table)
		if version != index+11 || string(value) != wantValue || ownerScope != "verification" {
			return fmt.Errorf("verify %s: data changed during migration", resource.table)
		}
		var fallback int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv WHERE kind = ? AND namespace = ? AND key = ?`, resource.kind, namespace, resource.key).Scan(&fallback); err != nil {
			return err
		}
		if fallback != 0 {
			return fmt.Errorf("verify %s: legacy row remains", resource.table)
		}
		verified = append(verified, resource.table)
	}
	sort.Strings(verified)
	fmt.Printf("verified %d migrated resource tables: %s\n", len(verified), strings.Join(verified, ","))
	return nil
}

func cleanup(ctx context.Context, db *sql.DB, namespace string) error {
	for _, table := range append(resourceTables(), fallbackTable) {
		if !safeIdentifier(table) {
			return fmt.Errorf("unsafe table name %q", table)
		}
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM "`+table+`" WHERE namespace = ?`, namespace); err != nil {
			return fmt.Errorf("cleanup %s: %w", table, err)
		}
	}
	return nil
}

func resourceTables() []string {
	tables := make([]string, 0, len(resources))
	for _, resource := range resources {
		tables = append(tables, resource.table)
	}
	return tables
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
