package diff

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ApplyOptions configures how changes are applied.
type ApplyOptions struct {
	DryRun          bool
	SkipDestructive bool
	BackupPath      string // Path to create backup (empty = no backup)
}

// defaultBusyTimeoutMS is the busy timeout applied to the migration
// connection. It applies per lock wait, so concurrent writers make the
// migration wait briefly instead of failing with SQLITE_BUSY immediately.
const defaultBusyTimeoutMS = 5000

// Apply compares the database against a schema directory and applies the
// resulting changes. Backup and migration run on a single connection inside
// one transaction.
func Apply(ctx context.Context, db *sql.DB, schemaDir string, opts ApplyOptions) error {
	changes, err := Compare(ctx, db, schemaDir)
	if err != nil {
		return err
	}

	if opts.DryRun || len(changes) == 0 {
		return nil
	}

	// Filter out destructive if requested
	if opts.SkipDestructive {
		var filtered []Change
		for _, c := range changes {
			if !c.Destructive {
				filtered = append(filtered, c)
			}
		}
		changes = filtered
		if len(changes) == 0 {
			return nil
		}
	}

	// All statements run on a single connection. A dedicated connection is
	// required because PRAGMA foreign_keys and PRAGMA busy_timeout are
	// per-connection settings, and the foreign_keys pragma is a no-op
	// inside a transaction, so it must be toggled outside of the
	// transaction on the same connection that runs the migration.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	prevBusyTimeout, err := busyTimeoutMS(ctx, conn)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(
		ctx,
		fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMS),
	); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(
			context.WithoutCancel(ctx),
			fmt.Sprintf("PRAGMA busy_timeout = %d", prevBusyTimeout),
		)
	}()

	// Create backup if path provided. VACUUM INTO cannot run inside a
	// transaction, so it happens before the migration starts.
	if opts.BackupPath != "" {
		if err := createBackup(ctx, conn, opts.BackupPath); err != nil {
			return err
		}
	}

	return migrate(ctx, conn, changes)
}

func createBackup(ctx context.Context, conn *sql.Conn, backupPath string) error {
	_ = os.Remove(backupPath)                             // Ignore error if doesn't exist
	safePath := strings.ReplaceAll(backupPath, "'", "''") // Escape single quotes for SQL
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", safePath)); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	return nil
}

func migrate(ctx context.Context, conn *sql.Conn, changes []Change) error {
	foreignKeysOn, err := foreignKeysEnabled(ctx, conn)
	if err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer func() {
		if foreignKeysOn {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")
		}
	}()

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

func foreignKeysEnabled(ctx context.Context, conn *sql.Conn) (bool, error) {
	var enabled int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
		return false, fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	return enabled == 1, nil
}

func busyTimeoutMS(ctx context.Context, conn *sql.Conn) (int, error) {
	var ms int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&ms); err != nil {
		return 0, fmt.Errorf("read busy_timeout pragma: %w", err)
	}
	return ms, nil
}

func execChanges(ctx context.Context, conn *sql.Conn, changes []Change) error {
	for _, change := range changes {
		for _, raw := range change.SQL {
			stmt := strings.TrimSpace(raw)
			if stmt == "" || strings.HasPrefix(stmt, "--") {
				continue
			}
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
