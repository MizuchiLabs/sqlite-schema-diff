package diff

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newTestDB creates a database file with the given schema. It is closed
// when the test ends.
func newTestDB(t *testing.T, schema string) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err, "open test db")
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(schema)
	require.NoError(t, err, "exec schema")
	return db, dbPath
}

func createSchemaDir(t *testing.T, filename, content string) fs.FS {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))
	return os.DirFS(dir)
}

// queryInt runs query on a fresh connection to dbPath, so it sees only
// what was committed.
func queryInt(t *testing.T, dbPath, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var n int
	require.NoError(t, db.QueryRow(query).Scan(&n), "query: %s", query)
	return n
}

// applyVersions applies v1 to a fresh database, then v2 with opts, and
// returns the database path and the changes still left after that.
func applyVersions(t *testing.T, v1, v2 string, opts ApplyOptions) (string, []Change) {
	t.Helper()
	db, dbPath := newTestDB(t, v1)
	schemaDir := createSchemaDir(t, "schema.sql", v2)

	_, err := Apply(t.Context(), db, schemaDir, opts)
	require.NoError(t, err, "apply")

	changes, err := Compare(t.Context(), db, schemaDir)
	require.NoError(t, err, "re-compare")
	return dbPath, changes
}

func changeTypes(changes []Change) []ChangeType {
	var types []ChangeType
	for _, c := range changes {
		types = append(types, c.Type)
	}
	return types
}
