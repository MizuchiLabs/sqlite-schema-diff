package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDatabase(t *testing.T) {
	db := NewDatabase()
	require.NotNil(t, db)

	assert.NotNil(t, db.Tables, "Tables map not initialized")
	assert.NotNil(t, db.Indexes, "Indexes map not initialized")
	assert.NotNil(t, db.Views, "Views map not initialized")
	assert.NotNil(t, db.Triggers, "Triggers map not initialized")
	assert.Empty(t, db.Tables)
}

func TestTableColumnNames(t *testing.T) {
	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
	}

	assert.Equal(t, []string{"id", "name", "email"}, table.ColumnNames())
}

func TestTableHasColumn(t *testing.T) {
	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	assert.True(t, table.HasColumn("id"))
	assert.True(t, table.HasColumn("name"))
	assert.False(t, table.HasColumn("email"))
	assert.False(t, table.HasColumn(""))
}

func TestTableGetColumn(t *testing.T) {
	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		},
	}

	idCol := table.GetColumn("id")
	require.NotNil(t, idCol)
	assert.Equal(t, "id", idCol.Name)
	assert.Equal(t, 1, idCol.PrimaryKey)

	assert.Nil(t, table.GetColumn(""))
}
