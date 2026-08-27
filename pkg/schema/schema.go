// Package schema provides types for representing SQLite database schemas.
package schema

import "strings"

// QuoteIdentifier renders a SQLite identifier (table, column, index, ...)
// as a double-quoted identifier. Embedded double quotes are escaped by
// doubling them, as SQLite requires. Note that Go's %q verb cannot be used
// here: it produces backslash escapes such as "q\"t", which SQLite
// misparses.
func QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Database represents a complete SQLite database schema.
type Database struct {
	Tables   map[string]*Table
	Indexes  map[string]*Index
	Views    map[string]*View
	Triggers map[string]*Trigger
}

// NewDatabase creates a new empty database schema.
func NewDatabase() *Database {
	return &Database{
		Tables:   make(map[string]*Table),
		Indexes:  make(map[string]*Index),
		Views:    make(map[string]*View),
		Triggers: make(map[string]*Trigger),
	}
}

// Table represents a SQLite table.
type Table struct {
	Name    string
	Columns []Column
	SQL     string // Original CREATE TABLE statement
	// UniqueColumns lists columns that participate in a UNIQUE table
	// constraint. SQLite does not expose this through PRAGMA table_info.
	UniqueColumns []string
	// ForeignKeyColumns lists columns that are the referencing side of a
	// foreign key (column-level or table-level).
	ForeignKeyColumns []string
}

// Column represents a table column (from PRAGMA table_info).
type Column struct {
	Name       string
	Type       string
	NotNull    bool
	Default    *string
	PrimaryKey int // 0 = not PK, 1+ = PK position
	Hidden     int // 0 = normal, 2 = virtual/generated, 3 = stored
}

// Index represents a SQLite index.
type Index struct {
	Name  string
	Table string
	SQL   string
}

// View represents a SQLite view.
type View struct {
	Name string
	SQL  string
}

// Trigger represents a SQLite trigger.
type Trigger struct {
	Name  string
	Table string
	SQL   string
}

// ColumnNames returns the column names for a table.
func (t *Table) ColumnNames() []string {
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	return names
}

// HasColumn checks if a table has a column by name.
func (t *Table) HasColumn(name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

// GetColumn returns a column by name, or nil if not found.
func (t *Table) GetColumn(name string) *Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}
