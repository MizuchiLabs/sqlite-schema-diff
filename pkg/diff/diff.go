// Package diff provides schema comparison and migration generation
package diff

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"sqlite-schema-diff/pkg/schema"
)

// ChangeType represents the type of schema change
type ChangeType string

const (
	CreateTable   ChangeType = "CREATE_TABLE"
	DropTable     ChangeType = "DROP_TABLE"
	AddColumn     ChangeType = "ADD_COLUMN"
	RecreateTable ChangeType = "RECREATE_TABLE"
	CreateIndex   ChangeType = "CREATE_INDEX"
	DropIndex     ChangeType = "DROP_INDEX"
	CreateView    ChangeType = "CREATE_VIEW"
	DropView      ChangeType = "DROP_VIEW"
	CreateTrigger ChangeType = "CREATE_TRIGGER"
	DropTrigger   ChangeType = "DROP_TRIGGER"
)

// Change represents a single schema change
type Change struct {
	Type        ChangeType
	Object      string   // Name of the object being changed
	Description string   // Human-readable description
	SQL         []string // SQL statements to apply
	Destructive bool     // Whether this change may lose data
}

// Diff compares two schemas and returns the changes needed to go from 'from' to 'to'
func Diff(from, to *schema.Database) []Change {
	var changes []Change

	// Track tables being recreated - their indexes will be dropped implicitly
	// and need to be recreated as part of the table recreation
	recreatedTables := make(map[string]bool)

	tableChanges := diffTables(from, to, recreatedTables)
	changes = append(changes, tableChanges...)
	changes = append(changes, diffIndexes(from, to, recreatedTables)...)
	changes = append(changes, diffViews(from, to)...)
	changes = append(changes, diffTriggers(from, to, recreatedTables)...)

	sortChanges(changes)
	return changes
}

func diffTables(from, to *schema.Database, recreatedTables map[string]bool) []Change {
	var changes []Change

	// Dropped tables
	for name := range from.Tables {
		if _, exists := to.Tables[name]; !exists {
			changes = append(changes, Change{
				Type:        DropTable,
				Object:      name,
				Description: fmt.Sprintf("Drop table %q", name),
				SQL:         []string{fmt.Sprintf("DROP TABLE %q;", name)},
				Destructive: true,
			})
		}
	}

	// New tables
	for name, table := range to.Tables {
		if _, exists := from.Tables[name]; !exists {
			changes = append(changes, Change{
				Type:        CreateTable,
				Object:      name,
				Description: fmt.Sprintf("Create table %q", name),
				SQL:         []string{ensureSemicolon(table.SQL)},
				Destructive: false,
			})
		}
	}

	// Modified tables
	for name, toTable := range to.Tables {
		fromTable, exists := from.Tables[name]
		if !exists {
			continue
		}

		tableChanges := diffTableColumns(fromTable, toTable)
		for _, c := range tableChanges {
			if c.Type == RecreateTable {
				recreatedTables[name] = true
			}
		}
		changes = append(changes, tableChanges...)
	}

	return changes
}

func diffTableColumns(from, to *schema.Table) []Change {
	var changes []Change

	// Check for dropped columns (requires table recreation)
	for _, col := range from.Columns {
		if !to.HasColumn(col.Name) {
			// Column removed - needs table recreation
			return []Change{recreateTableChange(from.Name, from, to)}
		}
	}

	// Check for new columns (can use ALTER TABLE ADD COLUMN)
	var newCols []schema.Column
	for _, col := range to.Columns {
		if !from.HasColumn(col.Name) {
			newCols = append(newCols, col)
		}
	}

	// Check for modified columns (requires table recreation)
	for _, toCol := range to.Columns {
		fromCol := from.GetColumn(toCol.Name)
		if fromCol == nil {
			continue // new column, handled above
		}

		if columnChanged(*fromCol, toCol) {
			// Column modified - needs table recreation
			return []Change{recreateTableChange(from.Name, from, to)}
		}
	}

	// If we're only adding columns, check if the table SQL has other changes
	// (e.g., UNIQUE, CHECK, FOREIGN KEY constraints that PRAGMA table_info doesn't expose)
	if len(newCols) == 0 && tableDefinitionChanged(from, to) {
		return []Change{recreateTableChange(from.Name, from, to)}
	}

	// Add new columns via ALTER TABLE
	for _, col := range newCols {
		changes = append(changes, Change{
			Type:        AddColumn,
			Object:      from.Name,
			Description: fmt.Sprintf("Add column %q to table %q", col.Name, from.Name),
			SQL:         []string{generateAddColumnSQL(from.Name, col)},
			Destructive: false,
		})
	}

	return changes
}

// tableDefinitionChanged compares the normalized CREATE TABLE statements
// to detect changes that PRAGMA table_info doesn't capture (UNIQUE, CHECK, FK, etc.)
func tableDefinitionChanged(from, to *schema.Table) bool {
	return normalizeTableSQL(from.SQL) != normalizeTableSQL(to.SQL)
}

// normalizeTableSQL normalizes a CREATE TABLE statement for comparison
func normalizeTableSQL(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")
	// Remove quotes around identifiers (SQLite accepts both quoted and unquoted)
	sql = strings.ReplaceAll(sql, "\"", "")
	sql = strings.ReplaceAll(sql, "`", "")
	sql = strings.ReplaceAll(sql, "[", "")
	sql = strings.ReplaceAll(sql, "]", "")
	// Collapse all whitespace to single spaces
	sql = strings.ToLower(strings.Join(strings.Fields(sql), " "))
	// Normalize spacing around punctuation
	for _, ch := range []string{"(", ")", ",", "="} {
		sql = strings.ReplaceAll(sql, " "+ch, ch)
		sql = strings.ReplaceAll(sql, ch+" ", ch)
	}
	// Add space after comma for consistency
	sql = strings.ReplaceAll(sql, ",", ", ")
	// Collapse any double spaces created
	for strings.Contains(sql, "  ") {
		sql = strings.ReplaceAll(sql, "  ", " ")
	}
	return sql
}

func columnChanged(from, to schema.Column) bool {
	// Compare type (case-insensitive)
	if !strings.EqualFold(from.Type, to.Type) {
		return true
	}

	// Compare NOT NULL
	if from.NotNull != to.NotNull {
		return true
	}

	// Compare PRIMARY KEY
	if (from.PrimaryKey > 0) != (to.PrimaryKey > 0) {
		return true
	}

	// Compare default values
	fromDefault := ""
	toDefault := ""
	if from.Default != nil {
		fromDefault = normalizeDefault(*from.Default)
	}
	if to.Default != nil {
		toDefault = normalizeDefault(*to.Default)
	}
	if fromDefault != toDefault {
		return true
	}

	return false
}

func normalizeDefault(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return s
}

func generateAddColumnSQL(tableName string, col schema.Column) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ALTER TABLE %q ADD COLUMN %q", tableName, col.Name)

	if col.Type != "" {
		fmt.Fprintf(&sb, " %s", col.Type)
	}

	if col.NotNull {
		if col.Default != nil {
			fmt.Fprintf(&sb, " NOT NULL DEFAULT %s", *col.Default)
		} else {
			// SQLite requires DEFAULT for NOT NULL in ADD COLUMN
			sb.WriteString(" DEFAULT ''")
		}
	} else if col.Default != nil {
		fmt.Fprintf(&sb, " DEFAULT %s", *col.Default)
	}

	sb.WriteString(";")
	return sb.String()
}

func recreateTableChange(name string, from, to *schema.Table) Change {
	return Change{
		Type:        RecreateTable,
		Object:      name,
		Description: fmt.Sprintf("Recreate table %q (schema changed)", name),
		SQL:         generateRecreateSQL(name, from, to),
		Destructive: true,
	}
}

func generateRecreateSQL(name string, from, to *schema.Table) []string {
	tempName := name + "__new"

	// Find common columns for data migration
	common := commonColumns(from, to)
	cols := strings.Join(common, ", ")

	createSQL := replaceTableName(to.SQL, tempName)

	stmts := []string{
		ensureSemicolon(createSQL),
	}

	if len(common) > 0 {
		stmts = append(
			stmts,
			fmt.Sprintf("INSERT INTO %q (%s) SELECT %s FROM %q;", tempName, cols, cols, name),
		)
	}

	stmts = append(stmts,
		fmt.Sprintf("DROP TABLE %q;", name),
		fmt.Sprintf("ALTER TABLE %q RENAME TO %q;", tempName, name),
	)

	return stmts
}

func commonColumns(from, to *schema.Table) []string {
	fromCols := make(map[string]bool)
	for _, c := range from.Columns {
		fromCols[c.Name] = true
	}

	var common []string
	for _, c := range to.Columns {
		if fromCols[c.Name] {
			common = append(common, c.Name)
		}
	}
	return common
}

var tableNameRe = regexp.MustCompile(
	`(?i)(CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?)["'\x60]?(\w+)["'\x60]?`,
)

func replaceTableName(sql, newName string) string {
	return tableNameRe.ReplaceAllString(sql, fmt.Sprintf("${1}%q", newName))
}

func diffIndexes(from, to *schema.Database, recreatedTables map[string]bool) []Change {
	var changes []Change

	// Dropped indexes (skip if table is being recreated - index is dropped implicitly)
	for name, idx := range from.Indexes {
		if recreatedTables[idx.Table] {
			continue // Index will be dropped when table is recreated
		}
		if _, exists := to.Indexes[name]; !exists {
			changes = append(changes, Change{
				Type:        DropIndex,
				Object:      name,
				Description: fmt.Sprintf("Drop index %q", name),
				SQL:         []string{fmt.Sprintf("DROP INDEX IF EXISTS %q;", name)},
				Destructive: false,
			})
		}
	}

	// New or modified indexes
	for name, toIdx := range to.Indexes {
		fromIdx, exists := from.Indexes[name]

		// If the table is being recreated, we need to create the index
		// (even if it existed before, it was dropped with the old table)
		if recreatedTables[toIdx.Table] {
			changes = append(changes, Change{
				Type:        CreateIndex,
				Object:      name,
				Description: fmt.Sprintf("Create index %q", name),
				SQL:         []string{ensureSemicolon(toIdx.SQL)},
				Destructive: false,
			})
			continue
		}

		if !exists {
			changes = append(changes, Change{
				Type:        CreateIndex,
				Object:      name,
				Description: fmt.Sprintf("Create index %q", name),
				SQL:         []string{ensureSemicolon(toIdx.SQL)},
				Destructive: false,
			})
		} else if normalizeSQL(fromIdx.SQL) != normalizeSQL(toIdx.SQL) {
			// Index changed - drop and recreate
			changes = append(changes, Change{
				Type:        DropIndex,
				Object:      name,
				Description: fmt.Sprintf("Drop index %q (will recreate)", name),
				SQL:         []string{fmt.Sprintf("DROP INDEX IF EXISTS %q;", name)},
				Destructive: false,
			})
			changes = append(changes, Change{
				Type:        CreateIndex,
				Object:      name,
				Description: fmt.Sprintf("Create index %q", name),
				SQL:         []string{ensureSemicolon(toIdx.SQL)},
				Destructive: false,
			})
		}
	}

	return changes
}

func diffViews(from, to *schema.Database) []Change {
	var changes []Change

	for name := range from.Views {
		if _, exists := to.Views[name]; !exists {
			changes = append(changes, Change{
				Type:        DropView,
				Object:      name,
				Description: fmt.Sprintf("Drop view %q", name),
				SQL:         []string{fmt.Sprintf("DROP VIEW IF EXISTS %q;", name)},
				Destructive: false,
			})
		}
	}

	for name, toView := range to.Views {
		fromView, exists := from.Views[name]
		if !exists {
			changes = append(changes, Change{
				Type:        CreateView,
				Object:      name,
				Description: fmt.Sprintf("Create view %q", name),
				SQL:         []string{ensureSemicolon(toView.SQL)},
				Destructive: false,
			})
		} else if normalizeSQL(fromView.SQL) != normalizeSQL(toView.SQL) {
			changes = append(changes, Change{
				Type:        DropView,
				Object:      name,
				Description: fmt.Sprintf("Drop view %q (will recreate)", name),
				SQL:         []string{fmt.Sprintf("DROP VIEW IF EXISTS %q;", name)},
				Destructive: false,
			})
			changes = append(changes, Change{
				Type:        CreateView,
				Object:      name,
				Description: fmt.Sprintf("Create view %q", name),
				SQL:         []string{ensureSemicolon(toView.SQL)},
				Destructive: false,
			})
		}
	}

	return changes
}

func diffTriggers(from, to *schema.Database, recreatedTables map[string]bool) []Change {
	var changes []Change

	// Dropped triggers (skip if table is being recreated - trigger is dropped implicitly)
	for name, trig := range from.Triggers {
		if recreatedTables[trig.Table] {
			continue // Trigger will be dropped when table is recreated
		}
		if _, exists := to.Triggers[name]; !exists {
			changes = append(changes, Change{
				Type:        DropTrigger,
				Object:      name,
				Description: fmt.Sprintf("Drop trigger %q", name),
				SQL:         []string{fmt.Sprintf("DROP TRIGGER IF EXISTS %q;", name)},
				Destructive: false,
			})
		}
	}

	for name, toTrig := range to.Triggers {
		fromTrig, exists := from.Triggers[name]

		// If the table is being recreated, we need to create the trigger
		// (even if it existed before, it was dropped with the old table)
		if recreatedTables[toTrig.Table] {
			changes = append(changes, Change{
				Type:        CreateTrigger,
				Object:      name,
				Description: fmt.Sprintf("Create trigger %q", name),
				SQL:         []string{ensureSemicolon(toTrig.SQL)},
				Destructive: false,
			})
			continue
		}

		if !exists {
			changes = append(changes, Change{
				Type:        CreateTrigger,
				Object:      name,
				Description: fmt.Sprintf("Create trigger %q", name),
				SQL:         []string{ensureSemicolon(toTrig.SQL)},
				Destructive: false,
			})
		} else if normalizeSQL(fromTrig.SQL) != normalizeSQL(toTrig.SQL) {
			changes = append(changes, Change{
				Type:        DropTrigger,
				Object:      name,
				Description: fmt.Sprintf("Drop trigger %q (will recreate)", name),
				SQL:         []string{fmt.Sprintf("DROP TRIGGER IF EXISTS %q;", name)},
				Destructive: false,
			})
			changes = append(changes, Change{
				Type:        CreateTrigger,
				Object:      name,
				Description: fmt.Sprintf("Create trigger %q", name),
				SQL:         []string{ensureSemicolon(toTrig.SQL)},
				Destructive: false,
			})
		}
	}

	return changes
}

func normalizeSQL(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")
	// Collapse all whitespace to single spaces
	sql = strings.ToLower(strings.Join(strings.Fields(sql), " "))
	// Normalize spacing around punctuation
	for _, ch := range []string{"(", ")", ",", "="} {
		sql = strings.ReplaceAll(sql, " "+ch, ch)
		sql = strings.ReplaceAll(sql, ch+" ", ch)
	}
	// Add space after comma for consistency
	sql = strings.ReplaceAll(sql, ",", ", ")
	// Collapse any double spaces created
	for strings.Contains(sql, "  ") {
		sql = strings.ReplaceAll(sql, "  ", " ")
	}
	return sql
}

func ensureSemicolon(sql string) string {
	sql = strings.TrimSpace(sql)
	if !strings.HasSuffix(sql, ";") {
		sql += ";"
	}
	return sql
}

// sortChanges orders changes for safe execution
func sortChanges(changes []Change) {
	priority := map[ChangeType]int{
		DropTrigger:   1,
		DropView:      2,
		DropIndex:     3,
		DropTable:     4,
		RecreateTable: 5,
		CreateTable:   6,
		AddColumn:     7,
		CreateIndex:   8,
		CreateView:    9,
		CreateTrigger: 10,
	}

	slices.SortStableFunc(changes, func(a, b Change) int {
		pa, pb := priority[a.Type], priority[b.Type]
		if pa != pb {
			return cmp.Compare(pa, pb)
		}
		return cmp.Compare(a.Object, b.Object)
	})
}

// HasDestructive returns true if any changes are destructive
func HasDestructive(changes []Change) bool {
	for _, c := range changes {
		if c.Destructive {
			return true
		}
	}
	return false
}

// GenerateSQL generates a complete migration script
func GenerateSQL(changes []Change) string {
	if len(changes) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("-- Generated by sqlite-schema-diff\n")
	sb.WriteString("PRAGMA foreign_keys = OFF;\n")
	sb.WriteString("BEGIN TRANSACTION;\n\n")

	for _, c := range changes {
		fmt.Fprintf(&sb, "-- %s: %s\n", c.Type, c.Description)

		for _, stmt := range c.SQL {
			sb.WriteString(stmt)
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}

	sb.WriteString("COMMIT;\n")
	sb.WriteString("PRAGMA foreign_keys = ON;\n")

	return sb.String()
}

// SortedKeys returns sorted keys from a map
func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}
