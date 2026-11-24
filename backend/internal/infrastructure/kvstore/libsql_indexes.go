package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Resource indexes are one schema change per table. In particular, replacing
// the pool index must not leave a table without its old index if creation fails.
func ensureLibSQLResourceIndexes(ctx context.Context, db *sql.DB, table string, columns []string) (resultErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin libSQL resource indexes for %s: %w", table, err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback libSQL resource indexes for %s: %w", table, err))
		}
	}()
	for _, column := range columns {
		if err := createLibSQLResourceIndex(ctx, tx, table, column); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit libSQL resource indexes for %s: %w", table, err)
	}
	return nil
}

func createLibSQLResourceIndex(ctx context.Context, tx *sql.Tx, table, column string) error {
	index := table + "_" + column + "_lookup"
	trailing := "namespace"
	if table == libSQLFallbackTable {
		trailing = "kind, namespace"
	} else if column == "runner_pool" {
		trailing = "namespace, key"
	}
	// IF NOT EXISTS cannot evolve the older two-column pool index.
	if column == "runner_pool" {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP INDEX IF EXISTS %s`, index)); err != nil {
			return fmt.Errorf("replace libSQL resource index %s: %w", index, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s, %s)`, index, table, column, trailing)); err != nil {
		return fmt.Errorf("initialize libSQL resource index %s: %w", index, err)
	}
	return nil
}
