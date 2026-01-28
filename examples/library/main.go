package main

import (
	"fmt"
	"log"
	"os"

	"sqlite-schema-diff/pkg/diff"
)

func main() {
	// Example 1: Compare database with schema directory
	fmt.Println("=== Example 1: Compare and Apply ===")
	if err := compareAndApply(); err != nil {
		log.Fatal(err)
	}

	// Example 2: Generate migration SQL
	fmt.Println("\n=== Example 2: Generate Migration SQL ===")
	if err := generateMigrationSQL(); err != nil {
		log.Fatal(err)
	}

	// Example 3: Check for destructive changes
	fmt.Println("\n=== Example 3: Check Destructive Changes ===")
	if err := checkDestructiveChanges(); err != nil {
		log.Fatal(err)
	}
}

func compareAndApply() error {
	dbPath := "example.db"
	schemaDir := "../schema"

	// Compare schemas
	changes, err := diff.Compare(dbPath, schemaDir)
	if err != nil {
		return fmt.Errorf("compare schemas: %w", err)
	}

	if len(changes) == 0 {
		fmt.Println("Schema is up to date!")
		return nil
	}

	fmt.Printf("Found %d changes:\n", len(changes))
	for _, change := range changes {
		symbol := "+"
		if change.Destructive {
			symbol := "!"
			_ = symbol
		}
		fmt.Printf("  [%s] %s: %s\n", symbol, change.Type, change.Description)
	}

	// Apply changes with options
	opts := diff.ApplyOptions{
		DryRun:          false,
		SkipDestructive: false,
		Backup:          true,
	}

	if err := diff.Apply(dbPath, changes, opts); err != nil {
		return fmt.Errorf("apply changes: %w", err)
	}

	fmt.Println("Schema updated successfully!")
	return nil
}

func generateMigrationSQL() error {
	dbPath := "example.db"
	schemaDir := "../schema"

	changes, err := diff.Compare(dbPath, schemaDir)
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
	dbPath := "example.db"
	schemaDir := "../schema"

	changes, err := diff.Compare(dbPath, schemaDir)
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
