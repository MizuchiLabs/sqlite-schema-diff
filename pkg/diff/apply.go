package diff

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// ApplyOptions configures how changes are applied
type ApplyOptions struct {
	DryRun          bool
	SkipDestructive bool
	Backup          bool
	ShowChanges     bool
}

// Apply applies schema changes to a database
func Apply(dbPath, schemaDir string, opts ApplyOptions) error {
	changes, err := Compare(dbPath, schemaDir)
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

	if opts.ShowChanges {
		ShowChanges(changes)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	// Create backup if requested (remove existing backup first)
	if opts.Backup {
		backupPath := dbPath + ".backup"
		_ = os.Remove(backupPath) // Ignore error if doesn't exist
		if _, err := db.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
