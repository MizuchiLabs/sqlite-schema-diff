package diff

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApply_NoChanges(t *testing.T) {
	db, _ := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(db, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApply_DryRun(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	err := Apply(db, schemaDir, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no changes were made - open fresh connection to check
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()

	var count int
	if err := checkDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 column, got %d", count)
	}
}

func TestApply_AddColumn(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	err := Apply(db, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify using fresh connection
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()

	var colName string
	err = checkDB.QueryRow("SELECT name FROM pragma_table_info('users') WHERE name = 'name'").
		Scan(&colName)
	if err != nil {
		t.Fatalf("column not added: %v", err)
	}
}

func TestApply_SkipDestructive(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(db, schemaDir, ApplyOptions{SkipDestructive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// posts table should still exist - verify with fresh connection
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()

	var name string
	err = checkDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='posts'").
		Scan(&name)
	if err != nil {
		t.Error("posts table was dropped despite SkipDestructive")
	}
}

func TestApply_Backup(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	backupPath := dbPath + ".backup"
	err := Apply(db, schemaDir, ApplyOptions{BackupPath: backupPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup file was not created")
	}

	// Verify backup has original schema
	backupDB, _ := sql.Open("sqlite", backupPath)
	defer func() { _ = backupDB.Close() }()

	var count int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 1 {
		t.Errorf("backup should have 1 column, got %d", count)
	}
}

// Helper functions

func createTestDBWithPath(t *testing.T, schema string) (*sql.DB, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("exec schema: %v", err)
	}

	return db, dbPath
}

func createSchemaDir(t *testing.T, filename, content string) string {
	t.Helper()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write schema file: %v", err)
	}

	return tmpDir
}

func TestApply_SkipDestructiveAllFiltered(t *testing.T) {
	// When all changes are destructive and SkipDestructive is true,
	// there should be no changes applied
	db, dbPath := createTestDBWithPath(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)
	defer func() { _ = db.Close() }()
	// Only keep users table - dropping posts is destructive
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(db, schemaDir, ApplyOptions{SkipDestructive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify posts table still exists
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()

	var count int
	err = checkDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").
		Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 tables, got %d", count)
	}
}

func TestApply_InvalidSchemaDir(t *testing.T) {
	db, _ := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()

	err := Apply(db, "/nonexistent/schema/dir", ApplyOptions{})
	if err == nil {
		t.Error("expected error for invalid schema dir")
	}
}

func TestApply_NoBackupWhenEmpty(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	// Empty BackupPath means no backup
	err := Apply(db, schemaDir, ApplyOptions{BackupPath: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no backup was created
	backupPath := dbPath + ".backup"
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("backup file should not exist when BackupPath is empty")
	}
}
