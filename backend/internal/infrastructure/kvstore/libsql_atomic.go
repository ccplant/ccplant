package kvstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ApplyAtomically applies a version-checked batch in one libSQL transaction.
func (s *LibSQLStore) ApplyAtomically(ctx context.Context, mutations []Mutation) ([]Record, error) {
	if len(mutations) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin atomic libSQL update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	updated := make([]Record, len(mutations))
	for i, mutation := range mutations {
		record := mutation.Record
		if mutation.Type != MutationDelete {
			if err := s.validateValueMode(record.Value); err != nil {
				return nil, err
			}
		}
		switch mutation.Type {
		case MutationCreate:
			updated[i], err = s.createRecordTx(ctx, tx, record)
		case MutationUpdate:
			updated[i], err = s.updateRecordTx(ctx, tx, record)
		case MutationDelete:
			updated[i], err = s.deleteRecordTx(ctx, tx, record)
		default:
			return nil, fmt.Errorf("unsupported atomic mutation %q", mutation.Type)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit atomic libSQL update: %w", err)
	}
	for i, record := range updated {
		locationKey := libSQLLocationKey(record.Kind, record.Namespace, record.Key)
		if mutations[i].Type == MutationDelete {
			s.tableLocations.Delete(locationKey)
		} else {
			s.tableLocations.Store(locationKey, libSQLTableForRecord(record))
		}
	}
	return updated, nil
}

func (s *LibSQLStore) createRecordTx(ctx context.Context, tx *sql.Tx, record Record) (Record, error) {
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	record.OwnerScope = ownerScopeForRecord(record)
	queryValues, err := libSQLQueryColumnValues(metadata)
	if err != nil {
		return Record{}, err
	}
	table := libSQLTableForRecord(record)
	columns := strings.Join(libSQLQueryColumns, ", ")
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(libSQLQueryColumns)), ", ")
	args := []any{record.Namespace, record.Key, metadata, record.OwnerScope, record.Value, time.Now().UTC().Format(time.RFC3339Nano)}
	statement := fmt.Sprintf(`INSERT INTO %s
(namespace, key, version, metadata, owner_scope, value, updated_at, %s)
VALUES (?, ?, 1, ?, ?, ?, ?, %s)`, table, columns, placeholders)
	if table == libSQLFallbackTable {
		statement = fmt.Sprintf(`INSERT INTO %s
(kind, namespace, key, version, metadata, owner_scope, value, updated_at, %s)
VALUES (?, ?, ?, 1, ?, ?, ?, ?, %s)`, table, columns, placeholders)
		args = append([]any{record.Kind}, args...)
	}
	for _, value := range queryValues {
		args = append(args, value)
	}
	if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "unique") || strings.Contains(message, "constraint") {
			return Record{}, ErrConflict
		}
		return Record{}, fmt.Errorf("atomic libSQL create %s: %w", record.Key, err)
	}
	record.Version = 1
	return record, nil
}

func (s *LibSQLStore) updateRecordTx(ctx context.Context, tx *sql.Tx, record Record) (Record, error) {
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	record.OwnerScope = ownerScopeForRecord(record)
	queryValues, err := libSQLQueryColumnValues(metadata)
	if err != nil {
		return Record{}, err
	}
	table := libSQLTableForRecord(record)
	assignments := strings.Join(libSQLQueryColumns, " = ?, ") + " = ?"
	where := "namespace = ? AND key = ? AND version = ?"
	args := []any{metadata, record.OwnerScope, record.Value, time.Now().UTC().Format(time.RFC3339Nano)}
	for _, value := range queryValues {
		args = append(args, value)
	}
	if table == libSQLFallbackTable {
		where = "kind = ? AND " + where
		args = append(args, record.Kind)
	}
	args = append(args, record.Namespace, record.Key, record.Version)
	statement := fmt.Sprintf(`UPDATE %s SET version = version + 1,
metadata = ?, owner_scope = ?, value = ?, updated_at = ?, %s
WHERE %s`, table, assignments, where)
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return Record{}, fmt.Errorf("atomic libSQL update %s: %w", record.Key, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Record{}, fmt.Errorf("read atomic libSQL update result: %w", err)
	}
	if affected != 1 {
		return Record{}, ErrConflict
	}
	record.Version++
	return record, nil
}

func (s *LibSQLStore) deleteRecordTx(ctx context.Context, tx *sql.Tx, record Record) (Record, error) {
	table := libSQLTableForRecord(record)
	where := "namespace = ? AND key = ? AND version = ?"
	args := []any{record.Namespace, record.Key, record.Version}
	if table == libSQLFallbackTable {
		where = "kind = ? AND " + where
		args = append([]any{record.Kind}, args...)
	}
	result, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", table, where), args...)
	if err != nil {
		return Record{}, fmt.Errorf("atomic libSQL delete %s: %w", record.Key, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Record{}, err
	}
	if affected != 1 {
		return Record{}, ErrConflict
	}
	return record, nil
}
