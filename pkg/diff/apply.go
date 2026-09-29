package diff

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ApplyOptions configures how changes are applied.
type ApplyOptions struct {
	SkipDestructive bool   // Leave out destructive changes, see [WithoutDestructive]
	BackupPath      string // Path to create backup (empty = no backup)
}

// defaultBusyTimeoutMS is the busy timeout applied to the migration
// connection. It applies per lock wait, so concurrent writers make the
// migration wait briefly instead of failing with SQLITE_BUSY immediately.
const defaultBusyTimeoutMS = 5000

// Apply compares the database against the .sql files in fsys and applies
// the resulting changes. It returns the changes that were applied.
func Apply(ctx context.Context, db *sql.DB, fsys fs.FS, opts ApplyOptions) ([]Change, error) {
	changes, err := Compare(ctx, db, fsys)
	if err != nil {
		return nil, err
	}
	if opts.SkipDestructive {
		changes = WithoutDestructive(changes)
	}
	if err := ApplyChanges(ctx, db, changes, opts.BackupPath); err != nil {
		return nil, err
	}
	return changes, nil
}

// ApplyChanges applies changes, as returned by [Compare], to the database.
// If backupPath is not empty, a backup is written there first. All changes
// run on a single connection inside one transaction.
func ApplyChanges(ctx context.Context, db *sql.DB, changes []Change, backupPath string) error {
	if len(changes) == 0 {
		return nil
	}

	// A dedicated connection is required because the pragmas below are
	// per-connection settings, and foreign_keys and legacy_alter_table must
	// be set outside of the transaction on the connection that runs it.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	restoreBusy, err := setPragma(ctx, conn, "busy_timeout", defaultBusyTimeoutMS)
	if err != nil {
		return err
	}
	defer restoreBusy()

	// VACUUM INTO cannot run inside a transaction, so the backup happens
	// before the migration starts.
	if backupPath != "" {
		if err := createBackup(ctx, conn, backupPath); err != nil {
			return err
		}
	}

	return migrate(ctx, conn, changes)
}

// createBackup writes to a temporary file first, so a failed backup never
// destroys the previous one.
func createBackup(ctx context.Context, conn *sql.Conn, backupPath string) error {
	tmpPath := backupPath + ".tmp"
	_ = os.Remove(tmpPath)
	safePath := strings.ReplaceAll(tmpPath, "'", "''")
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", safePath)); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	if err := os.Rename(tmpPath, backupPath); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	return nil
}

func migrate(ctx context.Context, conn *sql.Conn, changes []Change) error {
	restoreFK, err := setPragma(ctx, conn, "foreign_keys", 0)
	if err != nil {
		return err
	}
	defer restoreFK()

	// Table recreation drops a table and renames a copy into its place.
	// Modern ALTER TABLE RENAME validates the whole schema and fails on
	// views and triggers that reference the table while it is gone.
	restoreLegacy, err := setPragma(ctx, conn, "legacy_alter_table", 1)
	if err != nil {
		return err
	}
	defer restoreLegacy()

	// BEGIN IMMEDIATE takes the write lock up front so a concurrent writer
	// (or a second migration) fails fast instead of deadlocking at the
	// first write statement.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := execChanges(ctx, conn, changes); err != nil {
		return rollback(ctx, conn, err)
	}

	if err := checkForeignKeys(ctx, conn); err != nil {
		return rollback(ctx, conn, err)
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return rollback(ctx, conn, fmt.Errorf("commit: %w", err))
	}

	return nil
}

// setPragma sets an integer pragma on conn and returns a func that restores
// the previous value, even after ctx is canceled.
func setPragma(ctx context.Context, conn *sql.Conn, name string, value int) (func(), error) {
	var prev int
	if err := conn.QueryRowContext(ctx, "PRAGMA "+name).Scan(&prev); err != nil {
		return nil, fmt.Errorf("read %s pragma: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA %s = %d", name, value)); err != nil {
		return nil, fmt.Errorf("set %s pragma: %w", name, err)
	}
	return func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("PRAGMA %s = %d", name, prev))
	}, nil
}

func execChanges(ctx context.Context, conn *sql.Conn, changes []Change) error {
	for _, change := range changes {
		for _, stmt := range change.SQL {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("%s: %w\nSQL: %s", change.Description, err, stmt)
			}
		}
	}
	return nil
}

func checkForeignKeys(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if rows.Next() {
		var table, parent sql.NullString
		var rowid, fkid sql.NullInt64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("foreign key check: %w", err)
		}
		return fmt.Errorf(
			"migration would create foreign key violation in table %q (row %d) referencing parent %q",
			table.String,
			rowid.Int64,
			parent.String,
		)
	}

	return rows.Err()
}

// rollback aborts the open transaction on conn and returns cause, joined with
// any rollback error. It runs even when ctx is already canceled.
func rollback(ctx context.Context, conn *sql.Conn, cause error) error {
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
		return errors.Join(cause, fmt.Errorf("rollback: %w", err))
	}
	return cause
}
