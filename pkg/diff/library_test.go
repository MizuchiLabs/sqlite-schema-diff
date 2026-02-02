package diff

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCompare_IdenticalSchema(t *testing.T) {
	db := openTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	changes, err := Compare(db, schemaDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

func TestCompare_AddTable(t *testing.T) {
	db := openTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(t, "schema.sql", `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)

	changes, err := Compare(db, schemaDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) == 0 {
		t.Error("expected changes for new table")
	}

	found := false
	for _, c := range changes {
		if c.Type == CreateTable {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected create_table change")
	}
}

func TestCompare_InvalidSchemaDir(t *testing.T) {
	db := openTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()

	_, err := Compare(db, "/nonexistent/schema/dir")
	if err == nil {
		t.Error("expected error for invalid schema dir")
	}
}

func TestCompareDatabases(t *testing.T) {
	fromDB := openTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = fromDB.Close() }()
	toDB := openTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)
	defer func() { _ = toDB.Close() }()

	changes, err := CompareDatabases(fromDB, toDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) == 0 {
		t.Error("expected changes between databases")
	}
}

func TestGenerateSQL(t *testing.T) {
	changes := []Change{
		{
			Type:        CreateTable,
			Object:      "users",
			Description: "Create table users",
			SQL:         []string{"CREATE TABLE users (id INTEGER);"},
		},
	}

	sql := GenerateSQL(changes)

	// Check key parts are present
	checks := []string{
		"PRAGMA foreign_keys = OFF",
		"BEGIN TRANSACTION",
		"CREATE TABLE users",
		"COMMIT",
		"PRAGMA foreign_keys = ON",
	}

	for _, check := range checks {
		if !contains(sql, check) {
			t.Errorf("GenerateSQL() missing %q", check)
		}
	}
}

func TestGenerateSQLEmpty(t *testing.T) {
	if got := GenerateSQL(nil); got != "" {
		t.Errorf("GenerateSQL(nil) = %q, want empty", got)
	}
}

func TestShowChanges(t *testing.T) {
	changes := []Change{
		{Type: "create_table", Description: "Create table posts", Destructive: false},
		{Type: "drop_table", Description: "Drop table old_data", Destructive: true},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ShowChanges(changes)

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("[+] create_table")) {
		t.Error("expected non-destructive change with + symbol")
	}
	if !bytes.Contains([]byte(output), []byte("[-] drop_table")) {
		t.Error("expected destructive change with - symbol")
	}
	if !bytes.Contains([]byte(output), []byte("Total changes: 2 (1 destructive)")) {
		t.Error("expected summary line")
	}
}

func TestShowChanges_Empty(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ShowChanges([]Change{})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("Total changes: 0 (0 destructive)")) {
		t.Error("expected empty summary")
	}
}

// Helper functions

func openTestDB(t *testing.T, schema string) *sql.DB {
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

	return db
}
