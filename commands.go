package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
	_ "modernc.org/sqlite"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/diff"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/parser"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"
)

var commands = []*cli.Command{diffCMD, applyCMD, dumpCMD}

var databaseFlag = &cli.StringFlag{
	Name:     "database",
	Aliases:  []string{"db"},
	Usage:    "Path to SQLite database file",
	Required: true,
}

var schemaFlag = &cli.StringFlag{
	Name:    "schema",
	Aliases: []string{"s"},
	Value:   "schema",
	Usage:   "Path to schema directory containing .sql files",
}

var diffCMD = &cli.Command{
	Name:  "diff",
	Usage: "Show schema differences between database and schema files",
	Flags: []cli.Flag{
		databaseFlag,
		schemaFlag,
		&cli.BoolFlag{
			Name:  "sql",
			Usage: "Output migration SQL instead of human-readable diff",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		db, err := openDB(cmd.String("database"), true)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		changes, err := compare(ctx, db, cmd.String("schema"))
		if err != nil {
			return err
		}

		if len(changes) == 0 {
			fmt.Println("No schema changes detected.")
			return nil
		}

		if cmd.Bool("sql") {
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
		databaseFlag,
		schemaFlag,
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Show what would be applied without making changes",
		},
		&cli.BoolFlag{
			Name:  "skip-destructive",
			Usage: "Skip destructive changes (drops, table recreations, column renames)",
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

		// apply may create a new database, that is how a fresh one is set up.
		db, err := openDB(dbPath, false)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		changes, err := compare(ctx, db, cmd.String("schema"))
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Println("No schema changes detected.")
			return nil
		}

		if cmd.Bool("skip-destructive") {
			all := len(changes)
			changes = diff.WithoutDestructive(changes)
			if skipped := all - len(changes); skipped > 0 {
				fmt.Printf("Skipping %d change(s) that are destructive or depend on one.\n", skipped)
			}
			if len(changes) == 0 {
				fmt.Println("Nothing left to apply.")
				return nil
			}
		}

		fmt.Println("Schema changes to be applied:")
		showChanges(changes)

		if cmd.Bool("dry-run") {
			fmt.Println("\nDry run - no changes applied.")
			return nil
		}

		if diff.HasDestructive(changes) && !cmd.Bool("force") {
			ok, err := confirm("\nWARNING: Destructive changes detected. Continue? (yes/no): ")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}
		}

		backupPath := ""
		if cmd.Bool("backup") {
			backupPath = dbPath + ".backup"
		}

		if err := diff.ApplyChanges(ctx, db, changes, backupPath); err != nil {
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
		databaseFlag,
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   "out",
			Usage:   "Output directory for schema files",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		db, err := openDB(cmd.String("database"), true)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		return dumpSchema(ctx, db, cmd.String("output"))
	},
}

// openDB opens the database. With mustExist, a missing file is an error
// instead of silently creating an empty database.
func openDB(path string, mustExist bool) (*sql.DB, error) {
	if mustExist {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

func compare(ctx context.Context, db *sql.DB, schemaDir string) ([]diff.Change, error) {
	changes, err := diff.Compare(ctx, db, os.DirFS(schemaDir))
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", schemaDir, err)
	}
	return changes, nil
}

func confirm(prompt string) (bool, error) {
	fmt.Print(prompt)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && answer == "" {
		return false, errors.New(
			"no answer to the confirmation prompt, use --force to apply destructive changes without asking",
		)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "yes" || answer == "y", nil
}

func showChanges(changes []diff.Change) {
	destructive := 0
	for _, c := range changes {
		symbol := "+"
		if c.Destructive {
			symbol = "-"
			destructive++
		}
		fmt.Printf("[%s] %s: %s\n", symbol, c.Type, c.Description)
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
