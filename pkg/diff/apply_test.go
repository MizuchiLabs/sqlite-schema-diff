package diff

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply_NoChanges(t *testing.T) {
	db, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	applied, err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	require.NoError(t, err)
	assert.Empty(t, applied)
}

func TestApply_AddColumn(t *testing.T) {
	db, dbPath := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'name'`),
		"column not added")
}

func TestApply_SkipDestructive(t *testing.T) {
	db, dbPath := newTestDB(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{SkipDestructive: true})
	require.NoError(t, err)

	assert.Equal(t, 1, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'`),
		"posts table was dropped despite SkipDestructive")
}

func TestApply_Backup(t *testing.T) {
	db, dbPath := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	backupPath := dbPath + ".backup"
	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{BackupPath: backupPath})
	require.NoError(t, err)

	require.FileExists(t, backupPath)
	assert.Equal(t, 1, queryInt(t, backupPath, `SELECT COUNT(*) FROM pragma_table_info('users')`),
		"backup should have the original schema")
}

func TestApply_NoBackupWhenEmpty(t *testing.T) {
	db, dbPath := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{BackupPath: ""})
	require.NoError(t, err)

	assert.NoFileExists(t, dbPath+".backup")
}

func TestApply_BackupKeptWhenNewBackupFails(t *testing.T) {
	db, dbPath := newTestDB(t, `CREATE TABLE a (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(t, "a.sql", `CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);`)

	backupPath := dbPath + ".backup"
	require.NoError(t, os.WriteFile(backupPath, []byte("old"), 0o600))
	// A non-empty directory at the temp path makes VACUUM INTO fail.
	require.NoError(t, os.Mkdir(backupPath+".tmp", 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(backupPath+".tmp", "f"), nil, 0o600))

	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{BackupPath: backupPath})
	require.Error(t, err)

	b, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, "old", string(b), "previous backup was destroyed by a failed backup")
}

func TestApply_SkipDestructiveAllFiltered(t *testing.T) {
	db, dbPath := newTestDB(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)
	// Dropping posts is the only change, and it is destructive.
	schemaDir := createSchemaDir(t, "users.sql", `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	applied, err := Apply(t.Context(), db, schemaDir, ApplyOptions{SkipDestructive: true})
	require.NoError(t, err)
	assert.Empty(t, applied)

	assert.Equal(t, 2, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`))
}

func TestApply_InvalidSchemaDir(t *testing.T) {
	db, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := Apply(t.Context(), db, os.DirFS("/nonexistent/schema/dir"), ApplyOptions{})
	assert.Error(t, err)
}

// TestApply_ConvergesInOneApply guards against surprises: after a single
// apply, diffing again must report no changes. Constraints that ALTER TABLE
// cannot express (UNIQUE, non-constant defaults) force a table recreation
// instead of a lossy ADD COLUMN.
func TestApply_ConvergesInOneApply(t *testing.T) {
	dbPath, left := applyVersions(t,
		`CREATE TABLE users (id INTEGER PRIMARY KEY);`,
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		ApplyOptions{},
	)
	require.Empty(t, left, "expected no changes after one apply")

	assert.Equal(t, 1, queryInt(t, dbPath,
		`SELECT "notnull" FROM pragma_table_info('users') WHERE name = 'email'`),
		"email column lost its NOT NULL constraint")
}

// TestApply_WaitsForConcurrentWriter verifies the busy timeout: a concurrent
// writer holding the write lock makes the migration wait instead of failing
// immediately with SQLITE_BUSY.
func TestApply_WaitsForConcurrentWriter(t *testing.T) {
	db, dbPath := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	holder, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()

	tx, err := holder.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO users (id) VALUES (1)`)
	require.NoError(t, err)

	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(100 * time.Millisecond)
		_ = tx.Rollback()
	}()

	_, err = Apply(t.Context(), db, schemaDir, ApplyOptions{})
	<-released
	assert.NoError(t, err, "apply should wait for the concurrent writer")
}

// TestApply_FKViolationRollsBack verifies the pre-commit foreign key check:
// dropping a parent table while child rows still reference it must roll
// back the whole migration instead of committing broken data.
func TestApply_FKViolationRollsBack(t *testing.T) {
	db, dbPath := newTestDB(t, `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
		INSERT INTO users VALUES (1);
		INSERT INTO posts VALUES (10, 1);
	`)

	// Target schema only keeps posts, so dropping users orphans the post.
	schemaDir := createSchemaDir(
		t,
		"posts.sql",
		`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`,
	)

	_, err := Apply(t.Context(), db, schemaDir, ApplyOptions{})
	require.ErrorContains(t, err, "foreign key violation")

	assert.Equal(t, 1, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'`),
		"users table was not restored by the rollback")
}

// TestApply_QuotedIdentifierNames runs a full migration against objects
// whose names contain double quotes and semicolons.
func TestApply_QuotedIdentifierNames(t *testing.T) {
	_, left := applyVersions(t,
		`CREATE TABLE "user ""admin""" (id INTEGER PRIMARY KEY);`,
		`CREATE TABLE "user ""admin""" (id INTEGER PRIMARY KEY, note TEXT DEFAULT 'a;b');`,
		ApplyOptions{},
	)
	assert.Empty(t, left, "expected convergence")
}

func TestApply_ViewOnRecreatedTable(t *testing.T) {
	_, left := applyVersions(t,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT, y TEXT);
		CREATE VIEW v AS SELECT id, x FROM a;`,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);
		CREATE VIEW v AS SELECT id, x FROM a;`,
		ApplyOptions{},
	)
	assert.Empty(t, left, "expected convergence")
}

func TestApply_TriggerReferencingRecreatedTable(t *testing.T) {
	_, left := applyVersions(t,
		`CREATE TABLE log (id INTEGER PRIMARY KEY, n INT, z TEXT);
		CREATE TABLE a (id INTEGER PRIMARY KEY);
		CREATE TRIGGER t AFTER INSERT ON a BEGIN INSERT INTO log(n) VALUES (new.id); END;`,
		`CREATE TABLE log (id INTEGER PRIMARY KEY, n INT);
		CREATE TABLE a (id INTEGER PRIMARY KEY);
		CREATE TRIGGER t AFTER INSERT ON a BEGIN INSERT INTO log(n) VALUES (new.id); END;`,
		ApplyOptions{},
	)
	assert.Empty(t, left, "expected convergence")
}

func TestApply_SkipDestructiveKeepsIndexesOfSkippedTable(t *testing.T) {
	dbPath, left := applyVersions(t,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT, y TEXT);
		CREATE INDEX ix ON a(x);`,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);
		CREATE INDEX ix ON a(x);
		CREATE TABLE b (id INTEGER PRIMARY KEY);`,
		ApplyOptions{SkipDestructive: true},
	)

	assert.Empty(t, WithoutDestructive(left), "expected only the skipped recreation to remain")
	assert.Equal(t, 2, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('ix', 'b')`),
		"expected index ix kept and table b created")
}

func TestApply_SkipDestructiveSkipsGuessedRename(t *testing.T) {
	dbPath, left := applyVersions(t,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, old_name TEXT);
		CREATE INDEX ix ON a(old_name);`,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, new_name TEXT);
		CREATE INDEX ix ON a(new_name);`,
		ApplyOptions{SkipDestructive: true},
	)

	assert.Empty(t, WithoutDestructive(left), "expected only the skipped rename to remain")
	assert.Equal(t, 1, queryInt(t, dbPath,
		`SELECT COUNT(*) FROM pragma_table_info('a') WHERE name = 'old_name'`),
		"column was renamed despite SkipDestructive")
}

func TestApply_VirtualTable(t *testing.T) {
	_, left := applyVersions(t,
		`CREATE TABLE a (id INTEGER PRIMARY KEY);`,
		`CREATE TABLE a (id INTEGER PRIMARY KEY);
		CREATE VIRTUAL TABLE f USING fts5(body);`,
		ApplyOptions{},
	)
	assert.Empty(t, left, "expected convergence after create")

	_, left = applyVersions(t,
		`CREATE VIRTUAL TABLE f USING fts5(body);`,
		`CREATE VIRTUAL TABLE f USING fts5(title, body);`,
		ApplyOptions{},
	)
	assert.Empty(t, left, "expected convergence after change")
}

func TestApply_InsteadOfTriggerSurvivesViewChange(t *testing.T) {
	const s = `CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);
		CREATE VIEW v AS SELECT %s FROM a;
		CREATE TRIGGER vt INSTEAD OF INSERT ON v BEGIN INSERT INTO a(id) VALUES (new.id); END;`
	_, left := applyVersions(t, fmt.Sprintf(s, "id"), fmt.Sprintf(s, "id, x"), ApplyOptions{})
	assert.Empty(t, left, "expected convergence")
}

func TestApply_CommentOnlyChangeIsNoop(t *testing.T) {
	db, _ := newTestDB(t, "CREATE TABLE a (\n id INTEGER PRIMARY KEY -- the id\n);")
	schemaDir := createSchemaDir(t, "a.sql", "CREATE TABLE a (\n id INTEGER PRIMARY KEY -- primary key\n);")

	changes, err := Compare(t.Context(), db, schemaDir)
	require.NoError(t, err)
	assert.Empty(t, changes, "a comment edit must not change anything")
}
