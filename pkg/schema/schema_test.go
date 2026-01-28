package schema

import (
	"testing"
)

func TestNewDatabase(t *testing.T) {
	db := NewDatabase()

	if db == nil {
		t.Fatal("NewDatabase returned nil")
	}

	if db.Tables == nil {
		t.Error("Tables map not initialized")
	}

	if db.Indexes == nil {
		t.Error("Indexes map not initialized")
	}

	if db.Views == nil {
		t.Error("Views map not initialized")
	}

	if db.Triggers == nil {
		t.Error("Triggers map not initialized")
	}

	if len(db.Tables) != 0 {
		t.Error("Tables should be empty initially")
	}
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

	names := table.ColumnNames()

	if len(names) != 3 {
		t.Fatalf("expected 3 column names, got %d", len(names))
	}

	if names[0] != "id" || names[1] != "name" || names[2] != "email" {
		t.Errorf("unexpected column names: %v", names)
	}
}

func TestTableHasColumn(t *testing.T) {
	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	if !table.HasColumn("id") {
		t.Error("HasColumn should return true for 'id'")
	}

	if !table.HasColumn("name") {
		t.Error("HasColumn should return true for 'name'")
	}

	if table.HasColumn("email") {
		t.Error("HasColumn should return false for 'email'")
	}

	if table.HasColumn("") {
		t.Error("HasColumn should return false for empty string")
	}
}

func TestTableGetColumn(t *testing.T) {
	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		},
	}

	// Test existing column
	idCol := table.GetColumn("id")
	if idCol == nil {
		t.Fatal("GetColumn returned nil for 'id'")
	}
	if idCol.Name != "id" {
		t.Error("wrong column returned")
	}
	if idCol.PrimaryKey != 1 {
		t.Error("id should be primary key")
	}

	// Test empty name
	emptyCol := table.GetColumn("")
	if emptyCol != nil {
		t.Error("GetColumn should return nil for empty string")
	}
}
