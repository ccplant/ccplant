package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/sync/singleflight"
	_ "modernc.org/sqlite" // register the sqlite driver for local file:// databases
)

type LibSQLStore struct {
	db              *sql.DB
	encryptedValues bool
	listGroup       singleflight.Group
	tableLocations  sync.Map
}

func closeLibSQLAfterInitializationError(db *sql.DB, initializationErr error) error {
	return errors.Join(initializationErr, db.Close())
}

func (s *LibSQLStore) expectEncryptedValues() { s.encryptedValues = true }

func (s *LibSQLStore) validateValueMode(value []byte) error {
	isEncrypted := isEnvelopeCandidate(value)
	if s.encryptedValues && !isEncrypted {
		return ErrPlaintextInEncryptedStore
	}
	if !s.encryptedValues && isEncrypted {
		return ErrEncryptedInPlaintextStore
	}
	return nil
}

func NewLibSQLStore(ctx context.Context, databaseURL, authToken string) (*LibSQLStore, error) {
	opts := []libsql.Option{}
	if authToken != "" {
		opts = append(opts, libsql.WithAuthToken(authToken))
	}
	connector, err := libsql.NewConnector(databaseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("create libSQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	// Remote libSQL may close an autocommit stream without returning a baton.
	// The current Go driver only reports that state when the pooled connection
	// is reused. Do not retain or hand off those one-shot logical connections;
	// the underlying HTTP transport still pools its physical connections.
	if strings.HasPrefix(databaseURL, "libsql://") || strings.HasPrefix(databaseURL, "https://") || strings.HasPrefix(databaseURL, "http://") {
		db.SetMaxIdleConns(0)
		db.SetMaxOpenConns(0)
	} else {
		db.SetMaxOpenConns(8)
	}
	s := &LibSQLStore{db: db}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agentapi_kv (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
	owner_scope TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/v1","labels":{}}' CHECK (json_valid(metadata)),
PRIMARY KEY (kind, namespace, key))`); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, fmt.Errorf("initialize libSQL schema: %w", err))
	}
	if err := ensureLibSQLMetadataColumn(ctx, db); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, err)
	}
	if err := ensureLibSQLOwnerScope(ctx, db); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, err)
	}
	if err := ensureLibSQLResourceTables(ctx, db); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, err)
	}
	if err := ensureLibSQLLookupIndexes(ctx, db); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, err)
	}
	if err := ensureLibSQLBranchKeyTable(ctx, db); err != nil {
		return nil, closeLibSQLAfterInitializationError(db, err)
	}
	return s, nil
}

func ensureLibSQLLookupIndexes(ctx context.Context, db *sql.DB) error {
	// Dedicated columns supersede the legacy JSON expression indexes. Dropping
	// them also avoids maintaining two indexes for the same lookup on fallback
	// rows left behind by older or third-party resources.
	statements := []string{
		`DROP INDEX IF EXISTS agentapi_kv_session_profile_lookup`,
		`DROP INDEX IF EXISTS agentapi_kv_session_route_reuse_lookup`,
		`DROP INDEX IF EXISTS agentapi_kv_user_session_route_reuse_lookup`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize libSQL lookup index: %w", err)
		}
	}
	return nil
}

func ensureLibSQLOwnerScope(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(agentapi_kv)`)
	if err != nil {
		return fmt.Errorf("inspect libSQL owner scope schema: %w", err)
	}
	hasColumn := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return errors.Join(fmt.Errorf("scan libSQL owner scope schema: %w", err), rows.Close())
		}
		hasColumn = hasColumn || name == "owner_scope"
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agentapi_kv ADD COLUMN owner_scope TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add libSQL owner scope: %w", err)
		}
	}
	backfill, err := db.QueryContext(ctx, `SELECT kind, namespace, key, metadata FROM agentapi_kv WHERE owner_scope = ''`)
	if err != nil {
		return fmt.Errorf("list libSQL owner scope backfill: %w", err)
	}
	type pendingScope struct {
		kind                  Kind
		namespace, key, scope string
	}
	var pending []pendingScope
	for backfill.Next() {
		var item pendingScope
		var metadata []byte
		if err := backfill.Scan(&item.kind, &item.namespace, &item.key, &metadata); err != nil {
			return errors.Join(err, backfill.Close())
		}
		labels, err := unmarshalRecordMetadata(metadata)
		if err != nil {
			return errors.Join(fmt.Errorf("decode owner scope metadata for %s: %w", item.key, err), backfill.Close())
		}
		item.scope = ownerScopeForRecord(Record{Kind: item.kind, Namespace: item.namespace, Key: item.key, Labels: labels})
		pending = append(pending, item)
	}
	if err := backfill.Close(); err != nil {
		return err
	}
	for _, item := range pending {
		if _, err := db.ExecContext(ctx, `UPDATE agentapi_kv SET owner_scope = ? WHERE kind = ? AND namespace = ? AND key = ? AND owner_scope = ''`, item.scope, item.kind, item.namespace, item.key); err != nil {
			return fmt.Errorf("backfill owner scope for %s: %w", item.key, err)
		}
	}
	return nil
}

func ensureLibSQLBranchKeyTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agentapi_kv_branch_keys (
provider TEXT NOT NULL, key_id TEXT NOT NULL, generation INTEGER NOT NULL,
kms_key_ref TEXT NOT NULL, wrapped_key BLOB NOT NULL, status TEXT NOT NULL,
scope TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, PRIMARY KEY (provider, key_id, scope, generation))`); err != nil {
		return fmt.Errorf("initialize libSQL branch key table: %w", err)
	}
	if err := migrateLibSQLBranchKeyScope(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS agentapi_kv_branch_keys_active`); err != nil {
		return fmt.Errorf("drop legacy libSQL active branch key index: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS agentapi_kv_branch_keys_active_scope
ON agentapi_kv_branch_keys(provider, key_id, scope) WHERE status = 'active'`); err != nil {
		return fmt.Errorf("initialize libSQL active branch key index: %w", err)
	}
	return nil
}

func migrateLibSQLBranchKeyScope(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(agentapi_kv_branch_keys)`)
	if err != nil {
		return fmt.Errorf("inspect libSQL branch key schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return errors.Join(fmt.Errorf("scan libSQL branch key schema: %w", err), rows.Close())
		}
		found = found || name == "scope"
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close libSQL branch key schema rows: %w", err)
	}
	if found {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin libSQL branch key migration: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			log.Printf("rollback libSQL branch key migration: %v", rollbackErr)
		}
	}()
	statements := []string{
		`DROP INDEX IF EXISTS agentapi_kv_branch_keys_active`,
		`ALTER TABLE agentapi_kv_branch_keys RENAME TO agentapi_kv_branch_keys_legacy`,
		`CREATE TABLE agentapi_kv_branch_keys (
provider TEXT NOT NULL, key_id TEXT NOT NULL, generation INTEGER NOT NULL,
kms_key_ref TEXT NOT NULL, wrapped_key BLOB NOT NULL, status TEXT NOT NULL,
scope TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, PRIMARY KEY (provider, key_id, scope, generation))`,
		`INSERT INTO agentapi_kv_branch_keys
(provider, key_id, generation, kms_key_ref, wrapped_key, status, scope, created_at)
SELECT provider, key_id, generation, kms_key_ref, wrapped_key, status, '', created_at
FROM agentapi_kv_branch_keys_legacy`,
		`DROP TABLE agentapi_kv_branch_keys_legacy`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate libSQL branch key scope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit libSQL branch key migration: %w", err)
	}
	return nil
}

func ensureLibSQLMetadataColumn(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(agentapi_kv)`)
	if err != nil {
		return fmt.Errorf("inspect libSQL schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan libSQL schema: %w", err)
		}
		if name == "metadata" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return errors.Join(fmt.Errorf("inspect libSQL schema: %w", err), rows.Close())
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close libSQL schema rows: %w", err)
	}
	if found {
		return backfillLegacyLibSQLMetadata(ctx, db)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE agentapi_kv ADD COLUMN metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/legacy","labels":{}}' CHECK (json_valid(metadata))`); err != nil {
		return fmt.Errorf("add libSQL metadata column: %w", err)
	}
	return backfillLegacyLibSQLMetadata(ctx, db)
}

func backfillLegacyLibSQLMetadata(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT kind, namespace, key, value FROM agentapi_kv
WHERE json_extract(metadata, '$.format') = 'agentapi-kv-metadata/legacy'`)
	if err != nil {
		return fmt.Errorf("list legacy libSQL metadata: %w", err)
	}
	type legacyRecord struct {
		kind           Kind
		namespace, key string
		value          []byte
	}
	var records []legacyRecord
	for rows.Next() {
		var record legacyRecord
		if err := rows.Scan(&record.kind, &record.namespace, &record.key, &record.value); err != nil {
			return errors.Join(fmt.Errorf("scan legacy libSQL metadata: %w", err), rows.Close())
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy libSQL metadata rows: %w", err)
	}
	for _, record := range records {
		recordLabels, err := documentLabels(record.kind, record.value)
		if err != nil {
			return fmt.Errorf("extract labels for legacy %s/%s: %w", record.kind, record.key, err)
		}
		metadata, err := marshalRecordMetadata(recordLabels)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE agentapi_kv SET metadata = ? WHERE kind = ? AND namespace = ? AND key = ?`, metadata, record.kind, record.namespace, record.key); err != nil {
			return fmt.Errorf("backfill metadata for legacy %s/%s: %w", record.kind, record.key, err)
		}
	}
	return nil
}

func (s *LibSQLStore) Close() error { return s.db.Close() }

func (s *LibSQLStore) GetActiveBranchKey(ctx context.Context, provider, keyID, scope string) (BranchKeyRecord, error) {
	var record BranchKeyRecord
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT provider, key_id, scope, generation, kms_key_ref, wrapped_key, created_at
	FROM agentapi_kv_branch_keys WHERE provider = ? AND key_id = ? AND scope = ? AND status = 'active'`, provider, keyID, scope).
		Scan(&record.Provider, &record.KeyID, &record.Scope, &record.Generation, &record.KMSKeyRef, &record.WrappedKey, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return BranchKeyRecord{}, ErrBranchKeyNotFound
	}
	if err != nil {
		return BranchKeyRecord{}, fmt.Errorf("get active libSQL branch key: %w", err)
	}
	record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return BranchKeyRecord{}, fmt.Errorf("parse libSQL branch key timestamp: %w", err)
	}
	return record, nil
}

func (s *LibSQLStore) NextBranchKeyGeneration(ctx context.Context, provider, keyID, scope string) (int64, error) {
	var maximum int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation), 0)
	FROM agentapi_kv_branch_keys WHERE provider = ? AND key_id = ? AND scope = ?`, provider, keyID, scope).Scan(&maximum); err != nil {
		return 0, fmt.Errorf("get next libSQL branch key generation: %w", err)
	}
	return maximum + 1, nil
}

func (s *LibSQLStore) CreateActiveBranchKey(ctx context.Context, record BranchKeyRecord) error {
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agentapi_kv_branch_keys
	(provider, key_id, scope, generation, kms_key_ref, wrapped_key, status, created_at)
	VALUES (?, ?, ?, ?, ?, ?, 'active', ?)`, record.Provider, record.KeyID, record.Scope, record.Generation,
		record.KMSKeyRef, record.WrappedKey, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create active libSQL branch key: %w", err)
	}
	return nil
}

func (s *LibSQLStore) Create(ctx context.Context, record Record) (Record, error) {
	if err := s.validateValueMode(record.Value); err != nil {
		return Record{}, err
	}
	table := libSQLTableForRecord(record)
	routedTable, routed := libSQLSessionRunnerTableForKey(record.Kind, record.Key)
	if !routed || routedTable != table {
		if _, err := s.tableContaining(ctx, record.Kind, record.Namespace, record.Key); err == nil {
			return Record{}, ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return Record{}, err
		}
	}
	record.Version = 1
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	record.OwnerScope = ownerScopeForRecord(record)
	queryValues, err := libSQLQueryColumnValues(metadata)
	if err != nil {
		return Record{}, err
	}
	columns := strings.Join(libSQLQueryColumns, ", ")
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(libSQLQueryColumns)), ", ")
	var statement string
	args := []any{record.Namespace, record.Key, metadata, record.OwnerScope, record.Value, time.Now().UTC().Format(time.RFC3339Nano)}
	if table == libSQLFallbackTable {
		statement = fmt.Sprintf(`INSERT INTO %s
	(kind, namespace, key, version, metadata, owner_scope, value, updated_at, %s)
	VALUES (?, ?, ?, 1, ?, ?, ?, ?, %s)`, table, columns, placeholders)
		args = append([]any{record.Kind}, args...)
	} else {
		statement = fmt.Sprintf(`INSERT INTO %s
	(namespace, key, version, metadata, owner_scope, value, updated_at, %s)
	VALUES (?, ?, 1, ?, ?, ?, ?, %s)`, table, columns, placeholders)
	}
	for _, value := range queryValues {
		args = append(args, value)
	}
	result, err := s.db.ExecContext(ctx, statement, args...)
	if err != nil {
		if _, getErr := s.Get(ctx, record.Kind, record.Namespace, record.Key); getErr == nil {
			return Record{}, ErrConflict
		}
		return Record{}, fmt.Errorf("create libSQL record: %w", err)
	}
	if result == nil {
		return Record{}, errors.New("create libSQL record returned no result")
	}
	s.tableLocations.Store(libSQLLocationKey(record.Kind, record.Namespace, record.Key), table)
	return record, nil
}
