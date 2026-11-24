package kvstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func newResourceIndexDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "indexes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, statement := range []string{
		libSQLDataTableDDL("test_resource", false),
		`CREATE INDEX test_resource_runner_pool_lookup ON test_resource (runner_pool, namespace)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func resourceIndexColumnNames(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_info(test_resource_runner_pool_lookup)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	var columns []string
	for rows.Next() {
		var sequence, id int
		var name string
		if err := rows.Scan(&sequence, &id, &name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(columns, ",")
}

func TestResourceIndexesReplacePoolIndexAtomically(t *testing.T) {
	db := newResourceIndexDatabase(t)
	if err := ensureLibSQLResourceIndexes(context.Background(), db, "test_resource", []string{"owner_scope", "runner_pool"}); err != nil {
		t.Fatal(err)
	}
	if got := resourceIndexColumnNames(t, db); got != "runner_pool,namespace,key" {
		t.Fatalf("pool index columns = %s", got)
	}
	if err := ensureLibSQLResourceIndexes(context.Background(), db, "test_resource", []string{"owner_scope", "runner_pool"}); err != nil {
		t.Fatalf("repeated initialization: %v", err)
	}
}

func TestResourceIndexesRestorePreviousIndexOnFailure(t *testing.T) {
	db := newResourceIndexDatabase(t)
	err := ensureLibSQLResourceIndexes(context.Background(), db, "test_resource", []string{"runner_pool", "missing_column"})
	if err == nil {
		t.Fatal("expected missing-column failure")
	}
	if got := resourceIndexColumnNames(t, db); got != "runner_pool,namespace" {
		t.Fatalf("previous index was not restored: %s", got)
	}
}
