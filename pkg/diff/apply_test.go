package diff

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApply_NoChanges(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(dbPath, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApply_DryRun(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	err := Apply(dbPath, schemaDir, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no changes were made
	db, _ := sql.Open("sqlite", dbPath)
	defer func() {
		_ = db.Close()
	}()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 column, got %d", count)
	}
}

func TestApply_AddColumn(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	err := Apply(dbPath, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer func() {
		_ = db.Close()
	}()

	var colName string
	err = db.QueryRow("SELECT name FROM pragma_table_info('users') WHERE name = 'name'").
		Scan(&colName)
	if err != nil {
		t.Fatalf("column not added: %v", err)
	}
}

func TestApply_SkipDestructive(t *testing.T) {
	dbPath := createTestDB(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(dbPath, schemaDir, ApplyOptions{SkipDestructive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// posts table should still exist
	db, _ := sql.Open("sqlite", dbPath)
	defer func() {
		_ = db.Close()
	}()

	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='posts'").
		Scan(&name)
	if err != nil {
		t.Error("posts table was dropped despite SkipDestructive")
	}
}

func TestApply_Backup(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	err := Apply(dbPath, schemaDir, ApplyOptions{Backup: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backupPath := dbPath + ".backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup file was not created")
	}

	// Verify backup has original schema
	db, _ := sql.Open("sqlite", backupPath)
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 1 {
		t.Errorf("backup should have 1 column, got %d", count)
	}
}

func TestApply_InvalidDBPath(t *testing.T) {
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply("/nonexistent/path/db.sqlite", schemaDir, ApplyOptions{})
	if err == nil {
		t.Error("expected error for invalid db path")
	}
}

// Helper functions

func createTestDB(t *testing.T, schema string) string {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("exec schema: %v", err)
	}

	return dbPath
}

func createSchemaDir(t *testing.T, filename, content string) string {
	t.Helper()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write schema file: %v", err)
	}

	return tmpDir
}
