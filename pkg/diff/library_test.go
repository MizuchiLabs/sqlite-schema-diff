package diff

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompare_IdenticalSchema(t *testing.T) {
	db, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)
	schemaDir := createSchemaDir(
		t,
		"users.sql",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`,
	)

	changes, err := Compare(t.Context(), db, schemaDir)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestCompare_AddTable(t *testing.T) {
	db, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	schemaDir := createSchemaDir(t, "schema.sql", `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY);
	`)

	changes, err := Compare(t.Context(), db, schemaDir)
	require.NoError(t, err)
	assert.Equal(t, []ChangeType{CreateTable}, changeTypes(changes))
}

func TestCompare_InvalidSchemaDir(t *testing.T) {
	db, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)

	_, err := Compare(t.Context(), db, os.DirFS("/nonexistent/schema/dir"))
	assert.Error(t, err)
}

func TestCompareDatabases(t *testing.T) {
	fromDB, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY);`)
	toDB, _ := newTestDB(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`)

	changes, err := CompareDatabases(t.Context(), fromDB, toDB)
	require.NoError(t, err)
	assert.Equal(t, []ChangeType{AddColumn}, changeTypes(changes))
}

func TestGenerateSQL(t *testing.T) {
	sql := GenerateSQL([]Change{{
		Type:        CreateTable,
		Object:      "users",
		Description: "Create table users",
		SQL:         []string{"CREATE TABLE users (id INTEGER);"},
	}})

	for _, want := range []string{
		"PRAGMA foreign_keys = OFF",
		"PRAGMA legacy_alter_table = ON",
		"BEGIN TRANSACTION",
		"CREATE TABLE users",
		"COMMIT",
		"PRAGMA legacy_alter_table = OFF",
		"PRAGMA foreign_keys = ON",
	} {
		assert.Contains(t, sql, want)
	}
}

func TestGenerateSQLEmpty(t *testing.T) {
	assert.Empty(t, GenerateSQL(nil))
}
