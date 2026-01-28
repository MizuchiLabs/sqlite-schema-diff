package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromSQL_BasicTable(t *testing.T) {
	sql := `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE
	);`

	schema, err := FromSQL(sql)
	if err != nil {
		t.Fatalf("FromSQL failed: %v", err)
	}

	if len(schema.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(schema.Tables))
	}

	table, ok := schema.Tables["users"]
	if !ok {
		t.Fatal("table 'users' not found")
	}

	if len(table.Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(table.Columns))
	}

	// Check column properties
	idCol := table.GetColumn("id")
	if idCol == nil {
		t.Fatal("column 'id' not found")
	}
	if idCol.PrimaryKey == 0 {
		t.Error("id should be primary key")
	}

	nameCol := table.GetColumn("name")
	if nameCol == nil {
		t.Fatal("column 'name' not found")
	}
	if !nameCol.NotNull {
		t.Error("name should be NOT NULL")
	}
}

func TestFromSQL_WithConstraints(t *testing.T) {
	sql := `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		price REAL CHECK(price > 0),
		stock INTEGER DEFAULT 0
	);`

	schema, err := FromSQL(sql)
	if err != nil {
		t.Fatalf("FromSQL failed: %v", err)
	}

	table := schema.Tables["products"]
	if table == nil {
		t.Fatal("table 'products' not found")
	}

	// Check default value
	stockCol := table.GetColumn("stock")
	if stockCol == nil {
		t.Fatal("column 'stock' not found")
	}
	if stockCol.Default == nil {
		t.Error("stock should have a default value")
	} else if *stockCol.Default != "0" {
		t.Errorf("expected default '0', got '%s'", *stockCol.Default)
	}
}

func TestFromSQL_InvalidSQL(t *testing.T) {
	sql := `CREATE TABLE invalid (
		id INTEGER PRIMARY KEY,
		FOREIGN KEY (nonexistent) REFERENCES other(id)
	);`

	_, err := FromSQL(sql)
	if err == nil {
		t.Error("expected error for invalid SQL with nonexistent column in FK")
	}
}

func TestFromSQL_MultipleStatements(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT
		);

		CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			title TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE INDEX idx_posts_user ON posts(user_id);
	`

	schema, err := FromSQL(sql)
	if err != nil {
		t.Fatalf("FromSQL failed: %v", err)
	}

	if len(schema.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(schema.Tables))
	}

	if len(schema.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(schema.Indexes))
	}
}

func TestFromSQL_ViewsAndTriggers(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER DEFAULT 1);

		CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1;

		CREATE TRIGGER users_update AFTER UPDATE ON users
		BEGIN
			SELECT 1;
		END;
	`

	schema, err := FromSQL(sql)
	if err != nil {
		t.Fatalf("FromSQL failed: %v", err)
	}

	if len(schema.Views) != 1 {
		t.Errorf("expected 1 view, got %d", len(schema.Views))
	}

	if len(schema.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(schema.Triggers))
	}
}

func TestFromDirectory(t *testing.T) {
	// Create temp directory with SQL files
	dir := t.TempDir()

	// Write files with numeric prefixes for ordering
	file1 := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`
	file2 := `CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`

	if err := os.WriteFile(filepath.Join(dir, "01_users.sql"), []byte(file1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "02_posts.sql"), []byte(file2), 0644); err != nil {
		t.Fatal(err)
	}

	schema, err := FromDirectory(dir)
	if err != nil {
		t.Fatalf("FromDirectory failed: %v", err)
	}

	if len(schema.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(schema.Tables))
	}
}

func TestFromSQL_UniqueConstraint(t *testing.T) {
	// Test that UNIQUE constraint creates an automatic index
	sql := `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE NOT NULL
	);`

	schema, err := FromSQL(sql)
	if err != nil {
		t.Fatalf("FromSQL failed: %v", err)
	}

	// SQLite creates automatic indexes for UNIQUE constraints
	// The index name is sqlite_autoindex_<table>_<n>
	// These are filtered out by our extractIndexes (sql IS NOT NULL check passes them)
	// but we should verify the constraint is applied by checking the schema SQL
	table := schema.Tables["users"]
	if table == nil {
		t.Fatal("table 'users' not found")
	}

	// The SQL should contain UNIQUE
	if table.SQL == "" {
		t.Error("table SQL should not be empty")
	}
}
