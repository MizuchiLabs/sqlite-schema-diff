package diff

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// ApplyOptions configures how changes are applied
type ApplyOptions struct {
	DryRun          bool
	SkipDestructive bool
	BackupPath      string // Path to create backup (empty = no backup)
}

// Apply applies schema changes to a database
func Apply(db *sql.DB, schemaDir string, opts ApplyOptions) error {
	changes, err := Compare(db, schemaDir)
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

	// Create backup if path provided
	if opts.BackupPath != "" {
		_ = os.Remove(opts.BackupPath)                             // Ignore error if doesn't exist
		safePath := strings.ReplaceAll(opts.BackupPath, "'", "''") // Escape single quotes for SQL
		if _, err := db.Exec(fmt.Sprintf("VACUUM INTO '%s'", safePath)); err != nil {
			return fmt.Errorf("create backup: %w", err)
		}
	}

	// Execute in transaction with foreign keys disabled
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}

	for _, change := range changes {
		for _, stmt := range change.SQL {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || strings.HasPrefix(stmt, "--") {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("%s: %w\nSQL: %s", change.Description, err, stmt)
			}
		}
	}

	if _, err := tx.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	// Check for FK violations before committing
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	hasViolations := rows.Next()
	_ = rows.Close()
	if hasViolations {
		return fmt.Errorf("migration would create foreign key violations")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
