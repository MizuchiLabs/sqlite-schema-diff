package parser

import (
	"database/sql"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name: "no schema objects",
			sql:  `SELECT 1;`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := FromSQL(t.Context(), tt.sql)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.wantTables, keys(db.Tables), "tables")
			assert.ElementsMatch(t, tt.wantIndexes, keys(db.Indexes), "indexes")
			assert.ElementsMatch(t, tt.wantViews, keys(db.Views), "views")
			assert.ElementsMatch(t, tt.wantTriggers, keys(db.Triggers), "triggers")
		})
	}
}

func TestFromSQL_InvalidSQL(t *testing.T) {
	_, err := FromSQL(t.Context(), `INSERT INTO nonexistent_table VALUES (1);`)
	assert.Error(t, err)
}

func TestFromSQL_ColumnDetails(t *testing.T) {
	db, err := FromSQL(t.Context(), `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE,
		age INTEGER DEFAULT 0,
		bio TEXT
	);`)
	require.NoError(t, err)

	table := db.Tables["users"]
	require.NotNil(t, table)

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
			col := table.GetColumn(tt.colName)
			require.NotNil(t, col)
			assert.Equal(t, tt.wantType, col.Type, "type")
			assert.Equal(t, tt.wantNotNull, col.NotNull, "not null")
			assert.Equal(t, tt.wantPK, col.PrimaryKey, "primary key")
			assert.Equal(t, tt.wantDefault, col.Default, "default")
		})
	}
}

func TestFromSQL_WithSchemaQualifiers(t *testing.T) {
	db, err := FromSQL(t.Context(), `
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
		CREATE INDEX idx_users_email ON main.users(email);
	`)
	require.NoError(t, err)

	assert.Equal(t, []string{"users"}, keys(db.Tables))
	assert.Equal(t, []string{"idx_users_email"}, keys(db.Indexes))
}

func TestFromSQL_PreservesMainInStringLiterals(t *testing.T) {
	db, err := FromSQL(t.Context(), `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			note TEXT DEFAULT 'main.title',
			body TEXT
		);
		CREATE TRIGGER log_update AFTER UPDATE ON users
		BEGIN INSERT INTO audit (message) VALUES ('main.users changed'); END;
	`)
	require.NoError(t, err)

	require.NotNil(t, db.Tables["users"])
	note := db.Tables["users"].GetColumn("note")
	require.NotNil(t, note)
	assert.Equal(t, new("'main.title'"), note.Default, "default value corrupted")

	require.NotNil(t, db.Triggers["log_update"])
	assert.Contains(t, db.Triggers["log_update"].SQL, "'main.users changed'", "trigger SQL corrupted")
}

// TestFromSQL_WeirdSQL feeds hostile SQL through the statement scanner:
// semicolons inside strings, comments, and quoted identifiers, trigger
// bodies with CASE and multiple statements, odd line endings and encodings.
func TestFromSQL_WeirdSQL(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		wantTables   []string
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
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantTables, keys(db.Tables), "tables")
			assert.Empty(t, db.Views, "views")
			assert.ElementsMatch(t, tt.wantTriggers, keys(db.Triggers), "triggers")
		})
	}
}

func TestFromSQL_BlankAndCommentOnly(t *testing.T) {
	for _, sql := range []string{"", ";;;", "-- nothing\n/* still nothing */", "   \n\t  "} {
		db, err := FromSQL(t.Context(), sql)
		require.NoError(t, err, "FromSQL(%q)", sql)
		assert.Empty(t, db.Tables, "FromSQL(%q)", sql)
		assert.Empty(t, db.Views, "FromSQL(%q)", sql)
		assert.Empty(t, db.Triggers, "FromSQL(%q)", sql)
	}
}

func TestFromSQL_SkipsShadowTables(t *testing.T) {
	db, err := FromSQL(t.Context(), `CREATE VIRTUAL TABLE f USING fts5(body);`)
	require.NoError(t, err)
	assert.Equal(t, []string{"f"}, keys(db.Tables), "expected only the virtual table")
}

func TestParseStatements_TriggerWithExtraWhitespace(t *testing.T) {
	stmts := parseStatements(
		"CREATE\n  TRIGGER t AFTER INSERT ON a\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND;\nCREATE TABLE b (id INT);",
		"x.sql",
	)
	assert.Len(t, stmts, 2)
}

func TestFromDB(t *testing.T) {
	schemaSQL := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
		CREATE INDEX idx_name ON users(name);
	`
	sqlDB := openTestDB(t, schemaSQL)

	fromDB, err := FromDB(t.Context(), sqlDB)
	require.NoError(t, err)
	fromSQL, err := FromSQL(t.Context(), schemaSQL)
	require.NoError(t, err)

	assert.Equal(t, keys(fromSQL.Tables), keys(fromDB.Tables))
	assert.Equal(t, keys(fromSQL.Indexes), keys(fromDB.Indexes))
}

func TestFromDB_ExtractsUniqueAndForeignKeyColumns(t *testing.T) {
	sqlDB := openTestDB(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			slug TEXT NOT NULL,
			tags TEXT NOT NULL,
			user_id INTEGER REFERENCES users(id),
			UNIQUE (slug, tags)
		);
	`)

	db, err := FromDB(t.Context(), sqlDB)
	require.NoError(t, err)

	posts := db.Tables["posts"]
	require.NotNil(t, posts)
	assert.Equal(t, []string{"slug", "tags"}, posts.UniqueColumns)
	assert.Equal(t, []string{"user_id"}, posts.ForeignKeyColumns)

	users := db.Tables["users"]
	require.NotNil(t, users)
	assert.Empty(t, users.UniqueColumns)
	assert.Empty(t, users.ForeignKeyColumns)
}

func TestReadFiles(t *testing.T) {
	db, err := ReadFiles(t.Context(), writeDir(t, map[string]string{
		"01_users.sql": `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
		"02_posts.sql": `CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`,
		"03_index.sql": `CREATE INDEX idx_posts_user ON posts(user_id);`,
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"posts", "users"}, keys(db.Tables))
	assert.Equal(t, []string{"idx_posts_user"}, keys(db.Indexes))
}

func TestReadFiles_Nested(t *testing.T) {
	db, err := ReadFiles(t.Context(), writeDir(t, map[string]string{
		"01.sql":            `CREATE TABLE a (id INTEGER);`,
		"migrations/02.sql": `CREATE TABLE b (id INTEGER);`,
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, keys(db.Tables))
}

func TestReadFiles_Empty(t *testing.T) {
	db, err := ReadFiles(t.Context(), os.DirFS(t.TempDir()))
	require.NoError(t, err)
	assert.Empty(t, db.Tables)
}

func TestReadFiles_NonExistent(t *testing.T) {
	_, err := ReadFiles(t.Context(), os.DirFS("/nonexistent/path"))
	assert.Error(t, err)
}

func TestReadFiles_InvalidSQL(t *testing.T) {
	_, err := ReadFiles(t.Context(), writeDir(t, map[string]string{"bad.sql": `INVALID SQL`}))
	assert.Error(t, err)
}

func TestReadFiles_IgnoresNonSQL(t *testing.T) {
	db, err := ReadFiles(t.Context(), writeDir(t, map[string]string{
		"01_valid.sql": `CREATE TABLE t1(id INT);`,
		"README.md":    `This should be ignored`,
		".config":      `ignored`,
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"t1"}, keys(db.Tables))
}

func TestReadFiles_WithSchemaQualifiers(t *testing.T) {
	db, err := ReadFiles(t.Context(), writeDir(t, map[string]string{
		"01_tables.sql":  `CREATE TABLE http_routers (id INTEGER PRIMARY KEY, name TEXT);`,
		"02_indexes.sql": `CREATE INDEX idx_router_name ON main.http_routers(name);`,
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"http_routers"}, keys(db.Tables))
	assert.Equal(t, []string{"idx_router_name"}, keys(db.Indexes))
}

// TestReadFiles_IndexBeforeTable covers an index file that sorts before
// its table file, which used to fail with "no such table: main.users".
func TestReadFiles_IndexBeforeTable(t *testing.T) {
	db, err := ReadFiles(t.Context(), writeDir(t, map[string]string{
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
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"users"}, keys(db.Tables))
	assert.Equal(t, []string{"idx_users_email", "idx_users_username"}, keys(db.Indexes))
}

func TestReadFiles_FS(t *testing.T) {
	mockFS := fstest.MapFS{
		"schema/01_users.sql": {Data: []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)},
		"schema/02_posts.sql": {
			Data: []byte(`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`),
		},
		"schema/03_index.sql": {Data: []byte(`CREATE INDEX idx_posts_user ON posts(user_id);`)},
	}

	sub, err := fs.Sub(mockFS, "schema")
	require.NoError(t, err)
	db, err := ReadFiles(t.Context(), sub)
	require.NoError(t, err)

	assert.Equal(t, []string{"posts", "users"}, keys(db.Tables))
	assert.Equal(t, []string{"idx_posts_user"}, keys(db.Indexes))
}

func TestReadFiles_FSNestedDirs(t *testing.T) {
	mockFS := fstest.MapFS{
		"db/migrations/001.sql":     {Data: []byte(`CREATE TABLE a (id INTEGER);`)},
		"db/migrations/sub/002.sql": {Data: []byte(`CREATE TABLE b (id INTEGER);`)},
	}

	sub, err := fs.Sub(mockFS, "db/migrations")
	require.NoError(t, err)
	db, err := ReadFiles(t.Context(), sub)
	require.NoError(t, err)

	assert.Equal(t, []string{"a", "b"}, keys(db.Tables))
}

func TestReadFiles_FSNonExistent(t *testing.T) {
	sub, err := fs.Sub(fstest.MapFS{}, "nonexistent")
	require.NoError(t, err)

	_, err = ReadFiles(t.Context(), sub)
	assert.Error(t, err)
}

// keys returns the sorted keys of m.
func keys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// writeDir writes files (path -> content) into a temp dir and returns it.
func writeDir(t *testing.T, files map[string]string) fs.FS {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return os.DirFS(dir)
}

// openTestDB creates a database file with the given schema. It is closed
// when the test ends.
func openTestDB(t *testing.T, schema string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}
