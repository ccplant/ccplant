package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	_ "modernc.org/sqlite" // register the sqlite driver for local file:// databases
)

func (s *LibSQLStore) Update(ctx context.Context, record Record) (Record, error) {
	if err := s.validateValueMode(record.Value); err != nil {
		return Record{}, err
	}
	metadata, err := marshalRecordMetadata(record.Labels)
	if err != nil {
		return Record{}, err
	}
	record.OwnerScope = ownerScopeForRecord(record)
	queryValues, err := libSQLQueryColumnValues(metadata)
	if err != nil {
		return Record{}, err
	}
	locationKey := libSQLLocationKey(record.Kind, record.Namespace, record.Key)
	var table string
	if cached, ok := s.tableLocations.Load(locationKey); ok {
		table = cached.(string)
	} else if routed, ok := libSQLSessionRunnerTableForKey(record.Kind, record.Key); ok && routed == libSQLTableForRecord(record) {
		// The key and labels independently agree on the dedicated table. Avoid
		// a cold location query; records read from legacy fallback are already
		// represented in the cache and take the branch above.
		table = routed
	} else {
		var err error
		table, err = s.tableContaining(ctx, record.Kind, record.Namespace, record.Key)
		if err != nil {
			return Record{}, ErrConflict
		}
	}
	assignments := strings.Join(libSQLQueryColumns, " = ?, ") + " = ?"
	where := "namespace = ? AND key = ? AND version = ?"
	statement := fmt.Sprintf(`UPDATE %s SET version = version + 1,
metadata = ?, owner_scope = ?, value = ?, updated_at = ?, %s
WHERE %s`, table, assignments, where)
	args := []any{metadata, record.OwnerScope, record.Value, time.Now().UTC().Format(time.RFC3339Nano)}
	for _, value := range queryValues {
		args = append(args, value)
	}
	if table == libSQLFallbackTable {
		where = "kind = ? AND " + where
		statement = fmt.Sprintf(`UPDATE %s SET version = version + 1,
metadata = ?, owner_scope = ?, value = ?, updated_at = ?, %s
WHERE %s`, table, assignments, where)
		args = append(args, record.Kind)
	}
	args = append(args, record.Namespace, record.Key, record.Version)
	result, err := s.db.ExecContext(ctx, statement, args...)
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
	s.tableLocations.Store(locationKey, table)
	record.Version++
	return record, nil
}

func (s *LibSQLStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	record := Record{Kind: kind, Namespace: namespace, Key: key}
	var metadata []byte
	locationKey := libSQLLocationKey(kind, namespace, key)
	if _, cached := s.tableLocations.Load(locationKey); !cached {
		if dedicated, ok := libSQLSessionRunnerTableForKey(kind, key); ok {
			statement := fmt.Sprintf(`SELECT table_name, version, metadata, owner_scope, value FROM (
SELECT '%s' AS table_name, version, metadata, owner_scope, value FROM %s WHERE namespace = ? AND key = ?
UNION ALL
SELECT '%s' AS table_name, version, metadata, owner_scope, value FROM %s WHERE kind = ? AND namespace = ? AND key = ?
) LIMIT 1`, dedicated, dedicated, libSQLFallbackTable, libSQLFallbackTable)
			var table string
			err := s.db.QueryRowContext(ctx, statement, namespace, key, kind, namespace, key).
				Scan(&table, &record.Version, &metadata, &record.OwnerScope, &record.Value)
			if errors.Is(err, sql.ErrNoRows) {
				return Record{}, ErrNotFound
			}
			if err != nil {
				return Record{}, fmt.Errorf("get libSQL session-runner record: %w", err)
			}
			s.tableLocations.Store(locationKey, table)
			return s.decodeLibSQLRecord(record, metadata)
		}
	}
	table, err := s.tableContaining(ctx, kind, namespace, key)
	if err != nil {
		return Record{}, err
	}
	where := "namespace = ? AND key = ?"
	args := []any{namespace, key}
	if table == libSQLFallbackTable {
		where = "kind = ? AND " + where
		args = append([]any{kind}, args...)
	}
	statement := fmt.Sprintf(`SELECT version, metadata, owner_scope, value FROM %s WHERE %s`, table, where)
	err = s.db.QueryRowContext(ctx, statement, args...).Scan(&record.Version, &metadata, &record.OwnerScope, &record.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get libSQL record: %w", err)
	}
	return s.decodeLibSQLRecord(record, metadata)
}

func (s *LibSQLStore) decodeLibSQLRecord(record Record, metadata []byte) (Record, error) {
	var err error
	record.Labels, err = unmarshalRecordMetadata(metadata)
	if err != nil {
		return Record{}, fmt.Errorf("decode libSQL metadata: %w", err)
	}
	if err := s.validateValueMode(record.Value); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *LibSQLStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	locationKey := libSQLLocationKey(kind, namespace, key)
	if _, cached := s.tableLocations.Load(locationKey); !cached {
		if routed, ok := libSQLSessionRunnerTableForKey(kind, key); ok {
			result, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE namespace = ? AND key = ? AND version = ?`, routed), namespace, key, version)
			if err != nil {
				return fmt.Errorf("delete routed libSQL record: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("read routed libSQL delete result: %w", err)
			}
			if affected > 0 {
				return nil
			}
			// Version mismatch, missing row, and legacy fallback are uncommon.
			// Preserve their exact error semantics through the existing path.
		}
	}
	table, err := s.tableContaining(ctx, kind, namespace, key)
	if err != nil {
		return err
	}
	where := "namespace = ? AND key = ? AND version = ?"
	args := []any{namespace, key, version}
	if table == libSQLFallbackTable {
		where = "kind = ? AND " + where
		args = append([]any{kind}, args...)
	}
	statement := fmt.Sprintf(`DELETE FROM %s WHERE %s`, table, where)
	result, err := s.db.ExecContext(ctx, statement, args...)
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
	s.tableLocations.Delete(locationKey)
	return nil
}

func (s *LibSQLStore) List(ctx context.Context, query Query) ([]Record, error) {
	if err := ValidateListQuery(query); err != nil {
		return nil, err
	}
	return s.list(ctx, query)
}

func (s *LibSQLStore) Scan(ctx context.Context, query ScanQuery) ([]Record, error) {
	return s.list(ctx, Query{Kind: query.Kind, Namespace: query.Namespace})
}

func (s *LibSQLStore) list(ctx context.Context, query Query) ([]Record, error) {
	key := string(query.Kind) + "\x00" + query.Namespace + "\x00" + query.KeyPrefix + "\x00" + query.LabelSelector
	value, err, _ := s.listGroup.Do(key, func() (any, error) {
		return s.listOnce(ctx, query)
	})
	if err != nil {
		return nil, err
	}
	return cloneRecords(value.([]Record)), nil
}

func (s *LibSQLStore) listOnce(ctx context.Context, query Query) ([]Record, error) {
	selector, err := labels.Parse(query.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("parse label selector: %w", err)
	}
	var records []Record
	statements := make([]string, 0, len(libSQLResourceTables)+1)
	var args []any
	tables := libSQLTablesForSelector(selector)
	for _, table := range tables {
		if resource, dedicated := libSQLResourceTableNamed(table); dedicated && resource.kind != query.Kind {
			continue
		}
		statement, tableArgs := libSQLListQuery(table, query, selector)
		statements = append(statements, statement)
		args = append(args, tableArgs...)
	}
	if len(statements) == 0 {
		return records, nil
	}
	// A selector that cannot identify a dedicated resource may span every
	// physical table. UNION ALL keeps that operation to one remote round trip.
	// The compatibility contract is enforced by the Go sort below. Avoid a
	// second remote sort, especially the temporary merge sort required for a
	// UNION spanning several physical tables.
	rows, err := s.db.QueryContext(ctx, strings.Join(statements, " UNION ALL "), args...)
	if err != nil {
		return nil, fmt.Errorf("list libSQL records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		record := Record{Kind: query.Kind, Namespace: query.Namespace}
		var metadata []byte
		if err := rows.Scan(&record.Key, &record.Version, &metadata, &record.OwnerScope, &record.Value); err != nil {
			return nil, fmt.Errorf("scan libSQL record: %w", err)
		}
		record.Labels, err = unmarshalRecordMetadata(metadata)
		if err != nil {
			return nil, fmt.Errorf("decode libSQL metadata for %s: %w", record.Key, err)
		}
		if err := s.validateValueMode(record.Value); err != nil {
			return nil, fmt.Errorf("read libSQL record %s: %w", record.Key, err)
		}
		if selector.Matches(labels.Set(record.Labels)) {
			records = append(records, record)
			if len(tables) == 1 {
				s.tableLocations.Store(libSQLLocationKey(record.Kind, record.Namespace, record.Key), tables[0])
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	return records, nil
}

func cloneRecords(records []Record) []Record {
	cloned := make([]Record, len(records))
	for i := range records {
		cloned[i] = records[i]
		cloned[i].Value = append([]byte(nil), records[i].Value...)
		if records[i].Labels != nil {
			cloned[i].Labels = make(map[string]string, len(records[i].Labels))
			for key, value := range records[i].Labels {
				cloned[i].Labels[key] = value
			}
		}
	}
	return cloned
}

// libSQLListQuery pushes label requirements into SQLite so remote libSQL does
// not return every value in a namespace before the caller-side label filter is
// applied. The caller still checks selector.Matches after scanning as a safety
// net and to preserve Kubernetes selector semantics.
func libSQLListQuery(table string, query Query, selector labels.Selector) (string, []any) {
	statement := strings.Builder{}
	fmt.Fprintf(&statement, "SELECT key, version, metadata, owner_scope, value FROM %s WHERE ", table)
	args := make([]any, 0, 2)
	if table == libSQLFallbackTable {
		statement.WriteString("kind = ? AND ")
		args = append(args, query.Kind)
	}
	statement.WriteString("namespace = ?")
	args = append(args, query.Namespace)
	if query.KeyPrefix != "" {
		if upper, ok := stringPrefixUpperBound(query.KeyPrefix); ok {
			statement.WriteString(" AND key >= ? AND key < ?")
			args = append(args, query.KeyPrefix, upper)
		} else {
			statement.WriteString(" AND substr(key, 1, length(?)) = ?")
			args = append(args, query.KeyPrefix, query.KeyPrefix)
		}
	}
	requirements, selectable := selector.Requirements()
	if !selectable {
		statement.WriteString(" AND 0")
	}
	for i, requirement := range requirements {
		alias := fmt.Sprintf("label_%d", i)
		key := requirement.Key()
		values := requirement.Values().List()
		if column, ok := libSQLLabelColumns[key]; ok {
			writeLibSQLColumnMatch(&statement, &args, column, values, requirement.Operator())
			continue
		}
		switch requirement.Operator() {
		case selection.Equals, selection.DoubleEquals, selection.In:
			writeLibSQLLabelMatch(&statement, &args, table, key, values, false)
		case selection.NotEquals, selection.NotIn:
			writeLibSQLLabelExists(&statement, &args, table, alias, key, values, true)
		case selection.Exists:
			writeLibSQLLabelExists(&statement, &args, table, alias, key, nil, false)
		case selection.DoesNotExist:
			writeLibSQLLabelExists(&statement, &args, table, alias, key, nil, true)
		case selection.GreaterThan, selection.LessThan:
			comparison := ">"
			if requirement.Operator() == selection.LessThan {
				comparison = "<"
			}
			fmt.Fprintf(&statement, " AND EXISTS (SELECT 1 FROM json_each(%s.metadata, '$.labels') AS %s WHERE %s.key = ? AND CAST(%s.value AS INTEGER) %s ?)", table, alias, alias, alias, comparison)
			args = append(args, key, values[0])
		}
	}
	return statement.String(), args
}

// stringPrefixUpperBound returns the smallest string greater than every
// string with prefix. SQLite can use it as the upper edge of a B-tree range.
func stringPrefixUpperBound(prefix string) (string, bool) {
	runes := []rune(prefix)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] < '\U0010FFFF' {
			runes[i]++
			return string(runes[:i+1]), true
		}
	}
	return "", false
}

func writeLibSQLColumnMatch(statement *strings.Builder, args *[]any, column string, values []string, operator selection.Operator) {
	switch operator {
	case selection.Equals, selection.DoubleEquals, selection.In:
		fmt.Fprintf(statement, " AND %s IN (%s)", column, strings.TrimSuffix(strings.Repeat("?,", len(values)), ","))
		for _, value := range values {
			*args = append(*args, value)
		}
	case selection.NotEquals, selection.NotIn:
		fmt.Fprintf(statement, " AND %s NOT IN (%s)", column, strings.TrimSuffix(strings.Repeat("?,", len(values)), ","))
		for _, value := range values {
			*args = append(*args, value)
		}
	case selection.Exists:
		fmt.Fprintf(statement, " AND %s <> ''", column)
	case selection.DoesNotExist:
		fmt.Fprintf(statement, " AND %s = ''", column)
	case selection.GreaterThan, selection.LessThan:
		comparison := ">"
		if operator == selection.LessThan {
			comparison = "<"
		}
		fmt.Fprintf(statement, " AND CAST(%s AS INTEGER) %s ?", column, comparison)
		*args = append(*args, values[0])
	}
}
