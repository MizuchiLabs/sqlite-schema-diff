package diff

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestApply_NoChanges(t *testing.T) {
	db, _ := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
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

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no changes were made - open fresh connection to check
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()

	var count int
	if err := checkDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").
		Scan(&count); err != nil {
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

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
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

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{SkipDestructive: true})
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
	err := Apply(t.Context(), db, schemaDir, ApplyOptions{BackupPath: backupPath})
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
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users')").
		Scan(&count); err != nil {
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

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{SkipDestructive: true})
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

	err := Apply(t.Context(), db, "/nonexistent/schema/dir", ApplyOptions{})
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
	err := Apply(t.Context(), db, schemaDir, ApplyOptions{BackupPath: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no backup was created
	backupPath := dbPath + ".backup"
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("backup file should not exist when BackupPath is empty")
	}
}

// TestApply_ConvergesInOneApply guards against surprises: after a single
// apply, diffing again must report no changes. Constraints that ALTER TABLE
// cannot express (UNIQUE, non-constant defaults) force a table recreation
// instead of a lossy ADD COLUMN.
func TestApply_ConvergesInOneApply(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	)

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	changes, err := Compare(t.Context(), db, schemaDir)
	if err != nil {
		t.Fatalf("re-compare: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes after one apply, got %d: %+v", len(changes), changes)
	}

	// NOT NULL on the added column must be preserved by the recreation.
	var notNull int
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()
	if err := checkDB.QueryRow(
		`SELECT "notnull" FROM pragma_table_info('users') WHERE name = 'email'`,
	).Scan(&notNull); err != nil {
		t.Fatal(err)
	}
	if notNull != 1 {
		t.Error("email column lost its NOT NULL constraint")
	}
}

// TestApply_WaitsForConcurrentWriter verifies the busy timeout: a concurrent
// writer holding the write lock makes the migration wait instead of failing
// immediately with SQLITE_BUSY.
func TestApply_WaitsForConcurrentWriter(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	holder, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()

	tx, err := holder.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO users (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(100 * time.Millisecond)
		_ = tx.Rollback()
	}()

	err = Apply(t.Context(), db, schemaDir, ApplyOptions{})
	<-released
	if err != nil {
		t.Fatalf("apply should wait for the concurrent writer, got: %v", err)
	}
}

// TestApply_FKViolationRollsBack verifies the pre-commit foreign key check:
// dropping a parent table while child rows still reference it must roll
// back the whole migration instead of committing broken data.
func TestApply_FKViolationRollsBack(t *testing.T) {
	db, dbPath := createTestDBWithPath(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
		INSERT INTO users VALUES (1);
		INSERT INTO posts VALUES (10, 1);
	`)
	defer func() { _ = db.Close() }()

	// Target schema only keeps posts: dropping users would orphan the post.
	schemaDir := createSchemaDir(
		t,
		"posts.sql",
		`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`,
	)

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
	if !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The rollback must have restored the dropped users table.
	checkDB, _ := sql.Open("sqlite", dbPath)
	defer func() { _ = checkDB.Close() }()
	var name string
	if err := checkDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='users'`,
	).Scan(&name); err != nil {
		t.Fatal("users table was not restored by the rollback")
	}
}

// TestApply_QuotedIdentifierNames runs a full migration against objects
// whose names contain double quotes and semicolons.
func TestApply_QuotedIdentifierNames(t *testing.T) {
	db, _ := createTestDBWithPath(t, `CREATE TABLE "user ""admin""" (id INTEGER PRIMARY KEY);`)
	defer func() { _ = db.Close() }()
	schemaDir := createSchemaDir(
		t,
		"weird.sql",
		`CREATE TABLE "user ""admin""" (id INTEGER PRIMARY KEY, note TEXT DEFAULT 'a;b');`,
	)

	err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	changes, err := Compare(t.Context(), db, schemaDir)
	if err != nil {
		t.Fatalf("re-compare: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected convergence, got %+v", changes)
	}
}
