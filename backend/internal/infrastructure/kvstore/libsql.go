package kvstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tursodatabase/libsql-client-go/libsql"
	"k8s.io/apimachinery/pkg/labels"
	_ "modernc.org/sqlite" // register the sqlite driver for local file:// databases
)

type LibSQLStore struct{ db *sql.DB }

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
	db.SetMaxOpenConns(8)
	s := &LibSQLStore{db: db}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agentapi_kv (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
	metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/v1","labels":{}}' CHECK (json_valid(metadata)),
PRIMARY KEY (kind, namespace, key))`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize libSQL schema: %w", err)
	}
	if err := ensureLibSQLMetadataColumn(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
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
		_ = rows.Close()
		return fmt.Errorf("inspect libSQL schema: %w", err)
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
			_ = rows.Close()
			return fmt.Errorf("scan legacy libSQL metadata: %w", err)
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

func (s *LibSQLStore) Create(ctx context.Context, record Record) (Record, error) {
	record.Version = 1
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agentapi_kv
(kind, namespace, key, version, metadata, value, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?)`,
		record.Kind, record.Namespace, record.Key, metadata, record.Value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		if _, getErr := s.Get(ctx, record.Kind, record.Namespace, record.Key); getErr == nil {
			return Record{}, ErrConflict
		}
		return Record{}, fmt.Errorf("create libSQL record: %w", err)
	}
	return record, nil
}

func (s *LibSQLStore) Update(ctx context.Context, record Record) (Record, error) {
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE agentapi_kv SET version = version + 1,
metadata = ?, value = ?, updated_at = ? WHERE kind = ? AND namespace = ? AND key = ? AND version = ?`,
		metadata, record.Value, time.Now().UTC().Format(time.RFC3339Nano), record.Kind, record.Namespace, record.Key, record.Version)
	if err != nil {
		return Record{}, fmt.Errorf("update libSQL record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Record{}, fmt.Errorf("read libSQL update result: %w", err)
	}
	if affected == 0 {
		return Record{}, ErrConflict
	}
	record.Version++
	return record, nil
}

func (s *LibSQLStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	record := Record{Kind: kind, Namespace: namespace, Key: key}
	var metadata []byte
	err := s.db.QueryRowContext(ctx, `SELECT version, metadata, value FROM agentapi_kv
WHERE kind = ? AND namespace = ? AND key = ?`, kind, namespace, key).Scan(&record.Version, &metadata, &record.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get libSQL record: %w", err)
	}
	record.Labels, err = unmarshalRecordMetadata(metadata)
	if err != nil {
		return Record{}, fmt.Errorf("decode libSQL metadata: %w", err)
	}
	return record, nil
}

func (s *LibSQLStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agentapi_kv WHERE kind = ? AND namespace = ? AND key = ? AND version = ?`, kind, namespace, key, version)
	if err != nil {
		return fmt.Errorf("delete libSQL record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read libSQL delete result: %w", err)
	}
	if affected == 0 {
		if _, getErr := s.Get(ctx, kind, namespace, key); errors.Is(getErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

func (s *LibSQLStore) List(ctx context.Context, query Query) ([]Record, error) {
	selector, err := labels.Parse(query.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("parse label selector: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, version, metadata, value FROM agentapi_kv
WHERE kind = ? AND namespace = ? ORDER BY key`, query.Kind, query.Namespace)
	if err != nil {
		return nil, fmt.Errorf("list libSQL records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var records []Record
	for rows.Next() {
		record := Record{Kind: query.Kind, Namespace: query.Namespace}
		var metadata []byte
		if err := rows.Scan(&record.Key, &record.Version, &metadata, &record.Value); err != nil {
			return nil, fmt.Errorf("scan libSQL record: %w", err)
		}
		record.Labels, err = unmarshalRecordMetadata(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode libSQL metadata for %s: %w", record.Key, err)
		}
		if !selector.Matches(labels.Set(record.Labels)) {
			continue
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

const recordMetadataFormat = "agentapi-kv-metadata/v1"

type recordMetadata struct {
	Format string            `json:"format"`
	Labels map[string]string `json:"labels"`
}

func marshalRecordMetadata(recordLabels map[string]string) ([]byte, error) {
	if recordLabels == nil {
		recordLabels = map[string]string{}
	}
	metadata, err := json.Marshal(recordMetadata{Format: recordMetadataFormat, Labels: recordLabels})
	if err != nil {
		return nil, fmt.Errorf("encode libSQL metadata: %w", err)
	}
	return metadata, nil
}

func unmarshalRecordMetadata(data []byte) (map[string]string, error) {
	var metadata recordMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	if metadata.Format != recordMetadataFormat {
		return nil, fmt.Errorf("unsupported metadata format %q", metadata.Format)
	}
	if metadata.Labels == nil {
		metadata.Labels = map[string]string{}
	}
	return metadata.Labels, nil
}
