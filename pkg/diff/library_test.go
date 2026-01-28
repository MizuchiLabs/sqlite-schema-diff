package diff

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestCompare_IdenticalSchema(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	changes, err := Compare(dbPath, schemaDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

func TestCompare_AddTable(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(t, "schema.sql", `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)

	changes, err := Compare(dbPath, schemaDir)
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

func TestCompare_InvalidDBPath(t *testing.T) {
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := Compare("/nonexistent/db.sqlite", schemaDir)
	if err == nil {
		t.Error("expected error for invalid db path")
	}
}

func TestCompare_InvalidSchemaDir(t *testing.T) {
	dbPath := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := Compare(dbPath, "/nonexistent/schema/dir")
	if err == nil {
		t.Error("expected error for invalid schema dir")
	}
}

func TestCompareDatabases(t *testing.T) {
	fromDB := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	toDB := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)

	changes, err := CompareDatabases(fromDB, toDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) == 0 {
		t.Error("expected changes between databases")
	}
}

func TestCompareDatabases_InvalidFrom(t *testing.T) {
	toDB := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := CompareDatabases("/nonexistent/db.sqlite", toDB)
	if err == nil {
		t.Error("expected error for invalid from db")
	}
}

func TestCompareDatabases_InvalidTo(t *testing.T) {
	fromDB := createTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := CompareDatabases(fromDB, "/nonexistent/db.sqlite")
	if err == nil {
		t.Error("expected error for invalid to db")
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
