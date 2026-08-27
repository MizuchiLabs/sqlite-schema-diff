package parser

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

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
			db, err := FromSQL(t.Context(), tt.sql)
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

	db, err := FromSQL(t.Context(), sql)
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
		{"age", "INTEGER", false, 0, new("0")},
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
	db, err := FromSQL(t.Context(), schemaSQL)
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
	dbFromFile, err := FromDB(t.Context(), sqlDB)
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

	db, err := ReadFiles(t.Context(), tmpDir)
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

	db, err := ReadFiles(t.Context(), tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(db.Tables) != 2 {
		t.Errorf("expected 2 tables from nested dirs, got %d", len(db.Tables))
	}
}

func TestFromDirectory_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := ReadFiles(t.Context(), tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(db.Tables) != 0 {
		t.Errorf("expected 0 tables, got %d", len(db.Tables))
	}
}

func TestFromDirectory_NonExistent(t *testing.T) {
	_, err := ReadFiles(t.Context(), "/nonexistent/path")
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

	_, err := ReadFiles(t.Context(), tmpDir)
	if err == nil {
		t.Error("expected error for invalid SQL")
	}
}

func TestFromDirectory_IgnoreNonSQL(t *testing.T) {
	tmpDir := t.TempDir()

	// Valid SQL file
	if err := os.WriteFile(
		filepath.Join(tmpDir, "01_valid.sql"),
		[]byte(`CREATE TABLE t1(id INT);`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Non-SQL file that should be ignored
	if err := os.WriteFile(
		filepath.Join(tmpDir, "README.md"),
		[]byte(`This should be ignored`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Hidden file (dotfile) - should be ignored based on .sql suffix check, but just in case
	if err := os.WriteFile(filepath.Join(tmpDir, ".config"), []byte(`ignored`), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := ReadFiles(t.Context(), tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(db.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(db.Tables))
	}
	if _, ok := db.Tables["t1"]; !ok {
		t.Error("expected table t1 to exist")
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

func openAndExec(path, sqlStr string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(sqlStr)
	return db, err
}

func TestReadFiles_WithBaseFS(t *testing.T) {
	// Create a mock filesystem using fstest.MapFS
	mockFS := fstest.MapFS{
		"schema/01_users.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`),
		},
		"schema/02_posts.sql": &fstest.MapFile{
			Data: []byte(
				`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`,
			),
		},
		"schema/03_index.sql": &fstest.MapFile{
			Data: []byte(`CREATE INDEX idx_posts_user ON posts(user_id);`),
		},
	}

	// Set the base FS
	SetBaseFS(mockFS)
	defer SetBaseFS(nil) // Reset after test

	db, err := ReadFiles(t.Context(), "schema")
	if err != nil {
		t.Fatalf("ReadFiles with baseFS failed: %v", err)
	}

	if len(db.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(db.Tables))
	}
	if _, ok := db.Tables["users"]; !ok {
		t.Error("expected table 'users' to exist")
	}
	if _, ok := db.Tables["posts"]; !ok {
		t.Error("expected table 'posts' to exist")
	}
	if len(db.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(db.Indexes))
	}
}

func TestReadFiles_BaseFS_NestedDirs(t *testing.T) {
	mockFS := fstest.MapFS{
		"db/migrations/001.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE a (id INTEGER);`),
		},
		"db/migrations/sub/002.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE b (id INTEGER);`),
		},
	}

	SetBaseFS(mockFS)
	defer SetBaseFS(nil)

	db, err := ReadFiles(t.Context(), "db/migrations")
	if err != nil {
		t.Fatalf("ReadFiles with nested dirs failed: %v", err)
	}

	if len(db.Tables) != 2 {
		t.Errorf("expected 2 tables from nested dirs, got %d", len(db.Tables))
	}
}

func TestReadFiles_BaseFS_NonExistent(t *testing.T) {
	mockFS := fstest.MapFS{}

	SetBaseFS(mockFS)
	defer SetBaseFS(nil)

	_, err := ReadFiles(t.Context(), "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent directory in baseFS")
	}
}

func TestFromSQL_WithSchemaQualifiers(t *testing.T) {
	// Test that SQL with schema qualifiers can be parsed successfully
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
		CREATE INDEX idx_users_email ON main.users(email);
	`

	db, err := FromSQL(t.Context(), sql)
	if err != nil {
		t.Fatalf("FromSQL() with schema qualifiers failed: %v", err)
	}

	if len(db.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(db.Tables))
	}
	if _, ok := db.Tables["users"]; !ok {
		t.Error("expected table 'users' to exist")
	}
	if len(db.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(db.Indexes))
	}
	if _, ok := db.Indexes["idx_users_email"]; !ok {
		t.Error("expected index 'idx_users_email' to exist")
	}
}

func TestFromSQL_PreservesMainInStringLiterals(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			note TEXT DEFAULT 'main.title',
			body TEXT
		);
		CREATE TRIGGER log_update AFTER UPDATE ON users
		BEGIN INSERT INTO audit (message) VALUES ('main.users changed'); END;
	`

	db, err := FromSQL(t.Context(), sql)
	if err != nil {
		t.Fatalf("FromSQL() failed: %v", err)
	}

	table := db.Tables["users"]
	if table == nil {
		t.Fatal("users table not found")
	}

	note := table.GetColumn("note")
	if note == nil {
		t.Fatal("note column not found")
	}
	if note.Default == nil || *note.Default != "'main.title'" {
		t.Errorf("default value corrupted: %v", note.Default)
	}

	trigger := db.Triggers["log_update"]
	if trigger == nil {
		t.Fatal("log_update trigger not found")
	}
	if !strings.Contains(trigger.SQL, "'main.users changed'") {
		t.Errorf("trigger SQL corrupted: %s", trigger.SQL)
	}
}

func TestFromDirectory_WithSchemaQualifiers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SQL files with schema qualifiers
	files := map[string]string{
		"01_tables.sql":  `CREATE TABLE http_routers (id INTEGER PRIMARY KEY, name TEXT);`,
		"02_indexes.sql": `CREATE INDEX idx_router_name ON main.http_routers(name);`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := ReadFiles(t.Context(), tmpDir)
	if err != nil {
		t.Fatalf("ReadFiles() with schema qualifiers failed: %v", err)
	}

	if len(db.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(db.Tables))
	}
	if _, ok := db.Tables["http_routers"]; !ok {
		t.Error("expected table 'http_routers' to exist")
	}
	if len(db.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(db.Indexes))
	}
}

func TestFromDirectory_IndexBeforeTable(t *testing.T) {
	tmpDir := t.TempDir()

	// Test the scenario where index file comes alphabetically before table file
	// This was causing "no such table: main.users" errors
	files := map[string]string{
		"00_index.sql": `
			CREATE INDEX idx_users_email ON users (email);
			CREATE INDEX idx_users_username ON users (username);
		`,
		"01_users.sql": `
			CREATE TABLE users (
				id INTEGER PRIMARY KEY,
				email TEXT NOT NULL,
				username TEXT NOT NULL
			);
		`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := ReadFiles(t.Context(), tmpDir)
	if err != nil {
		t.Fatalf("ReadFiles() with index before table failed: %v", err)
	}

	if len(db.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(db.Tables))
	}
	if _, ok := db.Tables["users"]; !ok {
		t.Error("expected table 'users' to exist")
	}
	if len(db.Indexes) != 2 {
		t.Errorf("expected 2 indexes, got %d", len(db.Indexes))
	}
	if _, ok := db.Indexes["idx_users_email"]; !ok {
		t.Error("expected index 'idx_users_email' to exist")
	}
	if _, ok := db.Indexes["idx_users_username"]; !ok {
		t.Error("expected index 'idx_users_username' to exist")
	}
}

func TestFromDB_ExtractsUniqueAndForeignKeyColumns(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	sqlDB, err := openAndExec(dbPath, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			slug TEXT NOT NULL,
			tags TEXT NOT NULL,
			user_id INTEGER REFERENCES users(id),
			UNIQUE (slug, tags)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()

	db, err := FromDB(t.Context(), sqlDB)
	if err != nil {
		t.Fatal(err)
	}

	posts := db.Tables["posts"]
	if posts == nil {
		t.Fatal("posts table not found")
	}

	if !slices.Equal(posts.UniqueColumns, []string{"slug", "tags"}) {
		t.Errorf("UniqueColumns = %v, want [slug tags]", posts.UniqueColumns)
	}
	if !slices.Equal(posts.ForeignKeyColumns, []string{"user_id"}) {
		t.Errorf("ForeignKeyColumns = %v, want [user_id]", posts.ForeignKeyColumns)
	}

	users := db.Tables["users"]
	if users == nil {
		t.Fatal("users table not found")
	}
	if len(users.UniqueColumns) != 0 || len(users.ForeignKeyColumns) != 0 {
		t.Errorf("users should have no unique/FK columns, got %v / %v",
			users.UniqueColumns, users.ForeignKeyColumns)
	}
}

// TestFromSQL_WeirdSQL feeds hostile SQL through the statement scanner:
// semicolons inside strings, comments, and quoted identifiers; trigger
// bodies with CASE and multiple statements; odd line endings and encodings.
func TestFromSQL_WeirdSQL(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		wantTables   []string
		wantViews    []string
		wantTriggers []string
	}{
		{
			name:       "semicolon in string literal",
			sql:        `CREATE TABLE t1 (note TEXT DEFAULT 'a;b');`,
			wantTables: []string{"t1"},
		},
		{
			name:       "escaped quote in string",
			sql:        `CREATE TABLE t2 (note TEXT DEFAULT 'it''s');`,
			wantTables: []string{"t2"},
		},
		{
			name:       "semicolon in line comment",
			sql:        "-- hi; CREATE TABLE evil (id);\nCREATE TABLE t3 (id);",
			wantTables: []string{"t3"},
		},
		{
			name:       "semicolon in block comment",
			sql:        "/* ; */ CREATE TABLE t4 (id);",
			wantTables: []string{"t4"},
		},
		{
			name:       "unterminated block comment at end",
			sql:        "CREATE TABLE t5 (id); /* oops",
			wantTables: []string{"t5"},
		},
		{
			name:       "bracket identifier with semicolon",
			sql:        `CREATE TABLE [we ird;tbl] (id);`,
			wantTables: []string{"we ird;tbl"},
		},
		{
			name:       "quoted identifier with semicolon",
			sql:        `CREATE TABLE "ta;ble" (id);`,
			wantTables: []string{"ta;ble"},
		},
		{
			name:       "doubled quote in identifier",
			sql:        `CREATE TABLE "q""t" (id);`,
			wantTables: []string{`q"t`},
		},
		{
			name: "trigger with CASE keyword",
			sql: `CREATE TABLE t9 (id, updated_at TEXT);
				CREATE TRIGGER tr AFTER UPDATE ON t9
				BEGIN UPDATE t9 SET updated_at = CASE WHEN id THEN 'a' ELSE 'b' END; END;`,
			wantTables:   []string{"t9"},
			wantTriggers: []string{"tr"},
		},
		{
			name: "trigger with multiple body statements",
			sql: `CREATE TABLE t10 (id);
				CREATE TRIGGER tr10 AFTER INSERT ON t10 BEGIN INSERT INTO t10 VALUES (1); UPDATE t10 SET id = 2; END;`,
			wantTables:   []string{"t10"},
			wantTriggers: []string{"tr10"},
		},
		{
			name:       "no trailing semicolon",
			sql:        "CREATE TABLE t12 (id)",
			wantTables: []string{"t12"},
		},
		{
			name:       "CRLF line endings",
			sql:        "CREATE TABLE t13 (id);\r\nCREATE TABLE t14 (id);\r\n",
			wantTables: []string{"t13", "t14"},
		},
		{
			name:       "quoted main.table",
			sql:        `CREATE TABLE "main"."t16" (id);`,
			wantTables: []string{"t16"},
		},
		{
			name:       "string literal containing main.",
			sql:        `CREATE TABLE t19 (note TEXT DEFAULT 'main.x');`,
			wantTables: []string{"t19"},
		},
		{
			name:       "without rowid",
			sql:        `CREATE TABLE t26 (id TEXT PRIMARY KEY, v INT) WITHOUT ROWID;`,
			wantTables: []string{"t26"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := FromSQL(t.Context(), tt.sql)
			if err != nil {
				t.Fatalf("FromSQL() error = %v", err)
			}
			assertKeys(t, "tables", keys(db.Tables), tt.wantTables)
			assertKeys(t, "views", keys(db.Views), tt.wantViews)
			assertKeys(t, "triggers", keys(db.Triggers), tt.wantTriggers)
		})
	}
}

func TestFromSQL_BlankAndCommentOnly(t *testing.T) {
	for _, sql := range []string{"", ";;;", "-- nothing\n/* still nothing */", "   \n\t  "} {
		db, err := FromSQL(t.Context(), sql)
		if err != nil {
			t.Fatalf("FromSQL(%q) error = %v", sql, err)
		}
		if len(db.Tables)+len(db.Views)+len(db.Triggers) != 0 {
			t.Errorf("FromSQL(%q) should produce an empty schema", sql)
		}
	}
}
