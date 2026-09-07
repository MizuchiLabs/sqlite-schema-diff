package main

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/urfave/cli/v3"
	_ "modernc.org/sqlite"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/diff"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/parser"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"
)

var commands = []*cli.Command{diffCMD, applyCMD, dumpCMD}

var diffCMD = &cli.Command{
	Name:  "diff",
	Usage: "Show schema differences between database and schema files",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "database",
			Aliases:  []string{"db"},
			Usage:    "Path to SQLite database file",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "schema",
			Aliases: []string{"s"},
			Value:   "schema",
			Usage:   "Path to schema directory containing .sql files",
		},
		&cli.BoolFlag{
			Name:  "sql",
			Usage: "Output migration SQL instead of human-readable diff",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dbPath := cmd.String("database")
		schemaDir := cmd.String("schema")
		outputSQL := cmd.Bool("sql")

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		changes, err := diff.Compare(ctx, db, schemaDir)
		if err != nil {
			return err
		}

		if len(changes) == 0 {
			fmt.Println("No schema changes detected.")
			return nil
		}

		if outputSQL {
			fmt.Println(diff.GenerateSQL(changes))
		} else {
			showChanges(changes)
		}
		return nil
	},
}

var applyCMD = &cli.Command{
	Name:  "apply",
	Usage: "Apply schema changes to database",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "database",
			Aliases:  []string{"db"},
			Usage:    "Path to SQLite database file",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "schema",
			Aliases: []string{"s"},
			Value:   "schema",
			Usage:   "Path to schema directory containing .sql files",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Show what would be applied without making changes",
		},
		&cli.BoolFlag{
			Name:  "skip-destructive",
			Usage: "Skip destructive changes (drops, table recreations)",
		},
		&cli.BoolFlag{
			Name:  "backup",
			Usage: "Create backup before applying changes",
			Value: true,
		},
		&cli.BoolFlag{
			Name:    "force",
			Aliases: []string{"f"},
			Usage:   "Skip confirmation prompt for destructive changes",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dbPath := cmd.String("database")
		schemaDir := cmd.String("schema")
		dryRun := cmd.Bool("dry-run")
		skipDestructive := cmd.Bool("skip-destructive")
		backup := cmd.Bool("backup")
		force := cmd.Bool("force")

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		changes, err := diff.Compare(ctx, db, schemaDir)
		if err != nil {
			return err
		}

		if len(changes) == 0 {
			fmt.Println("No schema changes detected.")
			return nil
		}

		fmt.Println("Schema changes to be applied:")
		showChanges(changes)

		// Confirm destructive changes
		if diff.HasDestructive(changes) && !force && !dryRun {
			fmt.Print("\nWARNING: Destructive changes detected. Continue? (yes/no): ")
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				return err
			}
			if response != "yes" && response != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if dryRun {
			fmt.Println("\nDry run - no changes applied.")
			return nil
		}

		backupPath := ""
		if backup {
			backupPath = dbPath + ".backup"
		}

		opts := diff.ApplyOptions{
			DryRun:          dryRun,
			SkipDestructive: skipDestructive,
			BackupPath:      backupPath,
		}

		if err := diff.Apply(ctx, db, schemaDir, opts); err != nil {
			return fmt.Errorf("apply changes: %w", err)
		}

		fmt.Println("\nSchema changes applied successfully!")
		return nil
	},
}

var dumpCMD = &cli.Command{
	Name:  "dump",
	Usage: "Dump database schema to files",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "database",
			Aliases:  []string{"db"},
			Usage:    "Path to SQLite database file",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   "out",
			Usage:   "Output directory for schema files",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dbPath := cmd.String("database")
		outputDir := cmd.String("output")

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		return dumpSchema(ctx, db, outputDir)
	},
}

func showChanges(changes []diff.Change) {
	for _, c := range changes {
		symbol := "+"
		if c.Destructive {
			symbol = "-"
		}
		fmt.Printf("[%s] %s: %s\n", symbol, c.Type, c.Description)
	}

	destructive := 0
	for _, c := range changes {
		if c.Destructive {
			destructive++
		}
	}
	fmt.Printf("\nTotal changes: %d (%d destructive)\n", len(changes), destructive)
}

func dumpSchema(ctx context.Context, db *sql.DB, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	s, err := parser.FromDB(ctx, db)
	if err != nil {
		return fmt.Errorf("extract schema: %w", err)
	}

	dumps := []struct {
		file string
		sqls []string
	}{
		{"tables.sql", sortedSQL(s.Tables, func(t *schema.Table) string { return t.SQL })},
		{"indexes.sql", sortedSQL(s.Indexes, func(i *schema.Index) string { return i.SQL })},
		{"views.sql", sortedSQL(s.Views, func(v *schema.View) string { return v.SQL })},
		{"triggers.sql", sortedSQL(s.Triggers, func(t *schema.Trigger) string { return t.SQL })},
	}

	for _, d := range dumps {
		path := filepath.Join(outputDir, d.file)
		if len(d.sqls) == 0 {
			// Remove stale files from a previous dump so the output
			// directory always reflects the current database.
			_ = os.Remove(path)
			continue
		}
		if err := writeSQLFile(path, d.sqls); err != nil {
			return err
		}
	}

	fmt.Printf("Schema dumped to %s/\n", outputDir)
	fmt.Printf("  Tables: %d\n", len(s.Tables))
	fmt.Printf("  Indexes: %d\n", len(s.Indexes))
	fmt.Printf("  Views: %d\n", len(s.Views))
	fmt.Printf("  Triggers: %d\n", len(s.Triggers))
	return nil
}

// sortedSQL collects the SQL definitions of the named objects in name order.
func sortedSQL[T any](objects map[string]T, sqlOf func(T) string) []string {
	sqls := make([]string, 0, len(objects))
	for _, name := range slices.Sorted(maps.Keys(objects)) {
		sqls = append(sqls, sqlOf(objects[name]))
	}
	return sqls
}

// writeSQLFile writes each SQL statement followed by a blank line.
func writeSQLFile(name string, sqls []string) (err error) {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	for _, sql := range sqls {
		if _, err := fmt.Fprintf(f, "%s;\n\n", sql); err != nil {
			return err
		}
	}
	return nil
}
