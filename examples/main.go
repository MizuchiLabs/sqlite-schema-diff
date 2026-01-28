package main

import (
	"fmt"
	"log"
	"os"

	"sqlite-schema-diff/pkg/diff"
)

const (
	dbPath     = "example.db"
	schemaPath = "./schema"
)

func main() {
	fmt.Println("\n=== Example 1: Generate Migration SQL ===")
	if err := generateMigrationSQL(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n=== Example 2: Check Destructive Changes ===")
	if err := checkDestructiveChanges(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Example 3: Apply Schema Changes ===")
	if err := apply(); err != nil {
		log.Fatal(err)
	}
}

func generateMigrationSQL() error {
	changes, err := diff.Compare(dbPath, schemaPath)
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

func checkDestructiveChanges() error {
	changes, err := diff.Compare(dbPath, schemaPath)
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

func apply() error {
	// Apply changes with options
	opts := diff.ApplyOptions{
		DryRun:          false,
		SkipDestructive: false,
		Backup:          true,
		ShowChanges:     true,
	}

	if err := diff.Apply(dbPath, schemaPath, opts); err != nil {
		return fmt.Errorf("apply changes: %w", err)
	}

	fmt.Println("Schema updated successfully!")
	return nil
}
