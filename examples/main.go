package main

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/diff"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/parser"
	_ "modernc.org/sqlite"
)

const (
	dbPath     = "example.db"
	schemaPath = "schema"
)

//go:embed schema/*.sql
var schemaFS embed.FS

func main() {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	fmt.Println("=== Example 1: Generate Migration SQL ===")
	if err := generateMigrationSQL(db); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Example 2: Check Destructive Changes ===")
	if err := checkDestructiveChanges(db); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Example 3: Apply Schema Changes ===")
	if err := apply(db); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Example 3: Apply Embedded Changes ===")
	if err := applyEmbedded(db); err != nil {
		log.Fatal(err)
	}
}

func generateMigrationSQL(db *sql.DB) error {
	changes, err := diff.Compare(db, schemaPath)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		fmt.Println("No changes to generate")
		return nil
	}

	// Generate SQL
	sql := diff.GenerateSQL(changes)

	// Write to file
	if err := os.WriteFile("migration.sql", []byte(sql), 0o600); err != nil {
		return err
	}

	fmt.Println("Migration SQL written to migration.sql")
	return nil
}

func checkDestructiveChanges(db *sql.DB) error {
	changes, err := diff.Compare(db, schemaPath)
	if err != nil {
		return err
	}

	if diff.HasDestructive(changes) {
		fmt.Println("Warning: Destructive changes detected!")
		for _, change := range changes {
			if change.Destructive {
				fmt.Printf("  - %s: %s\n", change.Type, change.Description)
			}
		}
		return nil
	}

	fmt.Println("No destructive changes")
	return nil
}

func apply(db *sql.DB) error {
	// Apply changes with options
	opts := diff.ApplyOptions{
		DryRun:          false,
		SkipDestructive: false,
		BackupPath:      dbPath + ".backup",
	}

	if err := diff.Apply(db, schemaPath, opts); err != nil {
		return fmt.Errorf("apply changes: %w", err)
	}

	fmt.Println("Schema updated successfully!")
	return nil
}

func applyEmbedded(db *sql.DB) error {
	parser.SetBaseFS(schemaFS)

	// Apply changes with options
	opts := diff.ApplyOptions{
		DryRun:          false,
		SkipDestructive: false,
		BackupPath:      dbPath + ".backup",
	}

	if err := diff.Apply(db, schemaPath, opts); err != nil {
		return fmt.Errorf("apply changes: %w", err)
	}

	fmt.Println("Schema updated successfully!")
	return nil
}
