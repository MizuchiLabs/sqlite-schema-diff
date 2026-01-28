package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"sqlite-schema-diff/pkg/diff"
	"sqlite-schema-diff/pkg/parser"

	"github.com/urfave/cli/v3"
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
			Name:     "schema",
			Aliases:  []string{"s"},
			Usage:    "Path to schema directory containing .sql files",
			Required: true,
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

		changes, err := diff.Compare(dbPath, schemaDir)
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
			diff.ShowChanges(changes)
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
			Name:     "schema",
			Aliases:  []string{"s"},
			Usage:    "Path to schema directory containing .sql files",
			Required: true,
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

		changes, err := diff.Compare(dbPath, schemaDir)
		if err != nil {
			return err
		}

		if len(changes) == 0 {
			fmt.Println("No schema changes detected.")
			return nil
		}

		fmt.Println("Schema changes to be applied:")

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

		opts := diff.ApplyOptions{
			DryRun:          dryRun,
			SkipDestructive: skipDestructive,
			Backup:          backup,
		}

		if err := diff.ApplySchema(dbPath, schemaDir, opts); err != nil {
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
			Name:     "output",
			Aliases:  []string{"o"},
			Usage:    "Output directory for schema files",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dbPath := cmd.String("database")
		outputDir := cmd.String("output")

		return dumpSchema(dbPath, outputDir)
	},
}

func dumpSchema(dbPath, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	s, err := parser.FromDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("extract schema: %w", err)
	}

	// Write tables
	if len(s.Tables) > 0 {
		f, err := os.Create(filepath.Join(outputDir, "tables.sql"))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()

		for _, name := range diff.SortedKeys(s.Tables) {
			table := s.Tables[name]
			if _, err := fmt.Fprintf(f, "%s;\n\n", table.SQL); err != nil {
				return err
			}
		}
	}

	// Write indexes
	if len(s.Indexes) > 0 {
		f, err := os.Create(filepath.Join(outputDir, "indexes.sql"))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()

		for _, name := range diff.SortedKeys(s.Indexes) {
			index := s.Indexes[name]
			if _, err := fmt.Fprintf(f, "%s;\n\n", index.SQL); err != nil {
				return err
			}
		}
	}

	// Write views
	if len(s.Views) > 0 {
		f, err := os.Create(filepath.Join(outputDir, "views.sql"))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()

		for _, name := range diff.SortedKeys(s.Views) {
			view := s.Views[name]
			if _, err := fmt.Fprintf(f, "%s;\n\n", view.SQL); err != nil {
				return err
			}
		}
	}

	// Write triggers
	if len(s.Triggers) > 0 {
		f, err := os.Create(filepath.Join(outputDir, "triggers.sql"))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()

		for _, name := range diff.SortedKeys(s.Triggers) {
			trigger := s.Triggers[name]
			if _, err := fmt.Fprintf(f, "%s;\n\n", trigger.SQL); err != nil {
				return err
			}
		}
	}

	fmt.Printf("Schema dumped to %s/\n", outputDir)
	fmt.Printf("  Tables: %d\n", len(s.Tables))
	fmt.Printf("  Indexes: %d\n", len(s.Indexes))
	fmt.Printf("  Views: %d\n", len(s.Views))
	fmt.Printf("  Triggers: %d\n", len(s.Triggers))
	return nil
}
