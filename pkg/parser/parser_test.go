package parser

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFromSQL(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		wantTables   []string
		wantIndexes  []string
		wantViews    []string
		wantTriggers []string
		wantErr      bool
	}{
		{
			name:       "single table",
			sql:        `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
			wantTables: []string{"users"},
		},
		{
			name: "multiple tables",
			sql: `
				CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
				CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER, title TEXT);
			`,
			wantTables: []string{"posts", "users"},
		},
		{
			name: "table with index",
			sql: `
				CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
				CREATE INDEX idx_users_email ON users(email);
			`,
			wantTables:  []string{"users"},
			wantIndexes: []string{"idx_users_email"},
		},
		{
			name: "table with view",
			sql: `
				CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);
				CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1;
			`,
			wantTables: []string{"users"},
			wantViews:  []string{"active_users"},
		},
		{
			name: "table with trigger",
			sql: `
				CREATE TABLE users (id INTEGER PRIMARY KEY, updated_at TEXT);
				CREATE TRIGGER update_timestamp AFTER UPDATE ON users
				BEGIN UPDATE users SET updated_at = datetime('now') WHERE id = NEW.id; END;
			`,
			wantTables:   []string{"users"},
			wantTriggers: []string{"update_timestamp"},
		},
		{
			name:    "invalid SQL",
			sql:     `INSERT INTO nonexistent_table VALUES (1);`,
			wantErr: true,
		},
		{
			name:       "empty schema",
			sql:        `SELECT 1;`,
			wantTables: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := FromSQL(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FromSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			assertKeys(t, "tables", keys(db.Tables), tt.wantTables)
			assertKeys(t, "indexes", keys(db.Indexes), tt.wantIndexes)
			assertKeys(t, "views", keys(db.Views), tt.wantViews)
			assertKeys(t, "triggers", keys(db.Triggers), tt.wantTriggers)
		})
	}
}

func TestFromSQL_ColumnDetails(t *testing.T) {
	sql := `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE,
		age INTEGER DEFAULT 0,
		bio TEXT
	);`

	db, err := FromSQL(sql)
	if err != nil {
		t.Fatal(err)
	}

	table := db.Tables["users"]
	if table == nil {
		t.Fatal("users table not found")
	}

	tests := []struct {
		colName     string
		wantType    string
		wantNotNull bool
		wantPK      int
		wantDefault *string
	}{
		{"id", "INTEGER", false, 1, nil},
		{"name", "TEXT", true, 0, nil},
		{"email", "TEXT", false, 0, nil},
		{"age", "INTEGER", false, 0, ptr("0")},
		{"bio", "TEXT", false, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.colName, func(t *testing.T) {
			var col *struct {
				Name, Type string
				NotNull    bool
				PK         int
				Default    *string
			}
			for _, c := range table.Columns {
				if c.Name == tt.colName {
					col = &struct {
						Name, Type string
						NotNull    bool
						PK         int
						Default    *string
					}{c.Name, c.Type, c.NotNull, c.PrimaryKey, c.Default}
					break
				}
			}
			if col == nil {
				t.Fatalf("column %s not found", tt.colName)
			}
			if col.Type != tt.wantType {
				t.Errorf("type = %s, want %s", col.Type, tt.wantType)
			}
			if col.NotNull != tt.wantNotNull {
				t.Errorf("notnull = %v, want %v", col.NotNull, tt.wantNotNull)
			}
			if col.PK != tt.wantPK {
				t.Errorf("pk = %d, want %d", col.PK, tt.wantPK)
			}
			if (col.Default == nil) != (tt.wantDefault == nil) {
				t.Errorf("default = %v, want %v", col.Default, tt.wantDefault)
			}
		})
	}
}

func TestFromDB(t *testing.T) {
	// Create temp database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database with schema
	schemaSQL := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
		CREATE INDEX idx_name ON users(name);
	`
	db, err := FromSQL(schemaSQL)
	if err != nil {
		t.Fatal(err)
	}

	// Write to file using sql.Open
	sqlDB, err := openAndExec(dbPath, schemaSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()

	// Test FromDB
	dbFromFile, err := FromDB(sqlDB)
	if err != nil {
		t.Fatal(err)
	}

	if len(dbFromFile.Tables) != len(db.Tables) {
		t.Errorf("table count mismatch: got %d, want %d", len(dbFromFile.Tables), len(db.Tables))
	}
}

func TestFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SQL files (should be applied in sorted order)
	files := map[string]string{
		"01_users.sql": `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
		"02_posts.sql": `CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`,
		"03_index.sql": `CREATE INDEX idx_posts_user ON posts(user_id);`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := FromDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(db.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(db.Tables))
	}
	if len(db.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(db.Indexes))
	}
}

func TestFromDirectory_Nested(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create SQL files
	topFile := filepath.Join(tmpDir, "01.sql")
	subFile := filepath.Join(subDir, "02.sql")
	if err := os.WriteFile(topFile, []byte(`CREATE TABLE a (id INTEGER);`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte(`CREATE TABLE b (id INTEGER);`), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := FromDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(db.Tables) != 2 {
		t.Errorf("expected 2 tables from nested dirs, got %d", len(db.Tables))
	}
}

func TestFromDirectory_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := FromDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(db.Tables) != 0 {
		t.Errorf("expected 0 tables, got %d", len(db.Tables))
	}
}

func TestFromDirectory_NonExistent(t *testing.T) {
	_, err := FromDirectory("/nonexistent/path")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestFromDirectory_InvalidSQL(t *testing.T) {
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "bad.sql")
	if err := os.WriteFile(badFile, []byte(`INVALID SQL`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := FromDirectory(tmpDir)
	if err == nil {
		t.Error("expected error for invalid SQL")
	}
}

// Helpers

func keys[K comparable, V any](m map[K]V) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, any(k).(string))
	}
	return result
}

func assertKeys(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s count = %d, want %d (got: %v)", name, len(got), len(want), got)
	}
}

func ptr(s string) *string { return &s }

func openAndExec(path, sqlStr string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(sqlStr)
	return db, err
}
