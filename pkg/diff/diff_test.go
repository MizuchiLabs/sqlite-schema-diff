package diff

import (
	"strings"
	"testing"

	"sqlite-schema-diff/pkg/schema"
)

func TestDiffNoChanges(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	changes := Diff(db1, db2)

	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

func TestDiffCreateTable(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != CreateTable {
		t.Errorf("expected CREATE_TABLE, got %s", changes[0].Type)
	}

	if changes[0].Destructive {
		t.Error("CREATE TABLE should not be destructive")
	}
}

func TestDiffDropTable(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != DropTable {
		t.Errorf("expected DROP_TABLE, got %s", changes[0].Type)
	}

	if !changes[0].Destructive {
		t.Error("DROP TABLE should be destructive")
	}
}

func TestDiffAddColumn(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		},
	}

	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != AddColumn {
		t.Errorf("expected ADD_COLUMN, got %s", changes[0].Type)
	}

	if changes[0].Destructive {
		t.Error("ADD COLUMN should not be destructive")
	}
}

func TestDiffCreateIndex(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db2.Indexes["idx_users_email"] = &schema.Index{
		Name:  "idx_users_email",
		Table: "users",
		SQL:   "CREATE INDEX idx_users_email ON users(email)",
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != CreateIndex {
		t.Errorf("expected CREATE_INDEX, got %s", changes[0].Type)
	}
}

func TestHasDestructive(t *testing.T) {
	changes := []Change{
		{Type: CreateTable, Destructive: false},
		{Type: DropTable, Destructive: true},
	}

	if !HasDestructive(changes) {
		t.Error("expected destructive changes to be detected")
	}

	nonDestructive := []Change{
		{Type: CreateTable, Destructive: false},
		{Type: CreateIndex, Destructive: false},
	}

	if HasDestructive(nonDestructive) {
		t.Error("expected no destructive changes")
	}
}

func TestGenerateSQL(t *testing.T) {
	changes := []Change{
		{
			Type:        CreateTable,
			Description: "Create table users",
			SQL:         []string{"CREATE TABLE users (id INTEGER PRIMARY KEY);"},
		},
	}

	sql := GenerateSQL(changes)

	if sql == "" {
		t.Error("expected SQL output")
	}

	if !strings.Contains(sql, "CREATE TABLE users") {
		t.Error("expected SQL to contain CREATE TABLE statement")
	}

	if !strings.Contains(sql, "BEGIN TRANSACTION") {
		t.Error("expected SQL to contain transaction")
	}
}

func TestRecreateTableOnColumnChange(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
		},
	}

	// Change column type
	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name VARCHAR(255))",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "VARCHAR(255)"},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != RecreateTable {
		t.Errorf("expected RECREATE_TABLE, got %s", changes[0].Type)
	}

	if !changes[0].Destructive {
		t.Error("RECREATE_TABLE should be destructive")
	}
}

func TestRecreateTableOnColumnDrop(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
	}

	// Drop email column
	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != RecreateTable {
		t.Errorf("expected RECREATE_TABLE, got %s", changes[0].Type)
	}
}

func TestNormalizeSQL(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		// Same SQL with different whitespace
		{
			"CREATE INDEX idx ON t(a)",
			"CREATE INDEX idx ON t(a)\n\n",
			true,
		},
		// Different spacing around parens
		{
			"CREATE INDEX idx ON t(a)",
			"CREATE INDEX idx ON t (a)",
			true,
		},
		// Multi-line vs single line
		{
			"CREATE INDEX idx ON t(a, b)",
			"CREATE INDEX idx ON t(\n  a,\n  b\n)",
			true,
		},
		// Actually different
		{
			"CREATE INDEX idx ON t(a)",
			"CREATE INDEX idx ON t(b)",
			false,
		},
	}

	for _, tt := range tests {
		got := normalizeSQL(tt.a) == normalizeSQL(tt.b)
		if got != tt.want {
			t.Errorf("normalizeSQL(%q) == normalizeSQL(%q) = %v, want %v\nnormalized a: %q\nnormalized b: %q",
				tt.a, tt.b, got, tt.want, normalizeSQL(tt.a), normalizeSQL(tt.b))
		}
	}
}

func TestIndexFormattingNoChange(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	// Same index, different formatting
	db1.Indexes["idx"] = &schema.Index{
		Name:  "idx",
		Table: "users",
		SQL:   "CREATE INDEX idx ON users(email)",
	}
	db2.Indexes["idx"] = &schema.Index{
		Name:  "idx",
		Table: "users",
		SQL:   "CREATE INDEX idx ON users (email)\n",
	}

	changes := Diff(db1, db2)

	if len(changes) != 0 {
		t.Errorf("expected no changes for formatting-only difference, got %d: %v", len(changes), changes)
	}
}

func TestRecreateTableOnUniqueConstraintAdd(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	// Table without UNIQUE constraint
	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "email", Type: "TEXT", NotNull: true},
		},
	}

	// Same table with UNIQUE constraint added
	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "email", Type: "TEXT", NotNull: true},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != RecreateTable {
		t.Errorf("expected RECREATE_TABLE for UNIQUE constraint change, got %s", changes[0].Type)
	}
}

func TestRecreateTableOnCheckConstraintAdd(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	// Table without CHECK constraint
	db1.Tables["products"] = &schema.Table{
		Name: "products",
		SQL:  "CREATE TABLE products (id INTEGER PRIMARY KEY, price REAL)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "price", Type: "REAL"},
		},
	}

	// Same table with CHECK constraint added
	db2.Tables["products"] = &schema.Table{
		Name: "products",
		SQL:  "CREATE TABLE products (id INTEGER PRIMARY KEY, price REAL CHECK(price > 0))",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "price", Type: "REAL"},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != RecreateTable {
		t.Errorf("expected RECREATE_TABLE for CHECK constraint change, got %s", changes[0].Type)
	}
}

func TestRecreateTableOnForeignKeyAdd(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	// Table without FK
	db1.Tables["posts"] = &schema.Table{
		Name: "posts",
		SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "user_id", Type: "INTEGER"},
		},
	}

	// Same table with FK added
	db2.Tables["posts"] = &schema.Table{
		Name: "posts",
		SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "user_id", Type: "INTEGER"},
		},
	}

	changes := Diff(db1, db2)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Type != RecreateTable {
		t.Errorf("expected RECREATE_TABLE for FK constraint change, got %s", changes[0].Type)
	}
}

func TestRecreateTableIncludesIndexes(t *testing.T) {
	db1 := schema.NewDatabase()
	db2 := schema.NewDatabase()

	// Table with UNIQUE constraint (before removal)
	db1.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "email", Type: "TEXT", NotNull: true},
		},
	}
	db1.Indexes["idx_users_email"] = &schema.Index{
		Name:  "idx_users_email",
		Table: "users",
		SQL:   "CREATE INDEX idx_users_email ON users(email)",
	}

	// Table without UNIQUE constraint (after removal)
	db2.Tables["users"] = &schema.Table{
		Name: "users",
		SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL)",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "email", Type: "TEXT", NotNull: true},
		},
	}
	// Same index should still exist in target
	db2.Indexes["idx_users_email"] = &schema.Index{
		Name:  "idx_users_email",
		Table: "users",
		SQL:   "CREATE INDEX idx_users_email ON users(email)",
	}

	changes := Diff(db1, db2)

	// Should have RECREATE_TABLE and CREATE_INDEX (to recreate the index after table recreation)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (RECREATE_TABLE + CREATE_INDEX), got %d: %v", len(changes), changes)
	}

	hasRecreate := false
	hasCreateIndex := false
	for _, c := range changes {
		if c.Type == RecreateTable {
			hasRecreate = true
		}
		if c.Type == CreateIndex && c.Object == "idx_users_email" {
			hasCreateIndex = true
		}
	}

	if !hasRecreate {
		t.Error("expected RECREATE_TABLE change")
	}
	if !hasCreateIndex {
		t.Error("expected CREATE_INDEX change for idx_users_email (index must be recreated after table recreation)")
	}
}
