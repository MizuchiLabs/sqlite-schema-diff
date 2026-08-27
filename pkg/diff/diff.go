// Package diff provides schema comparison and migration generation.
package diff

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/parser"
	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"
)

// ChangeType represents the type of schema change.
type ChangeType string

const (
	CreateTable   ChangeType = "CREATE_TABLE"
	DropTable     ChangeType = "DROP_TABLE"
	RenameTable   ChangeType = "RENAME_TABLE"
	AddColumn     ChangeType = "ADD_COLUMN"
	RenameColumn  ChangeType = "RENAME_COLUMN"
	RecreateTable ChangeType = "RECREATE_TABLE"
	CreateIndex   ChangeType = "CREATE_INDEX"
	DropIndex     ChangeType = "DROP_INDEX"
	CreateView    ChangeType = "CREATE_VIEW"
	DropView      ChangeType = "DROP_VIEW"
	CreateTrigger ChangeType = "CREATE_TRIGGER"
	DropTrigger   ChangeType = "DROP_TRIGGER"
)

// Change represents a single schema change.
type Change struct {
	Type        ChangeType
	Object      string   // Name of the object being changed
	Description string   // Human-readable description
	SQL         []string // SQL statements to apply
	Destructive bool     // Whether this change may lose data
}

// Diff compares two schemas and returns the changes.
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

	// Tables that were dropped or renamed by letter case only. SQLite table
	// names are case-insensitive, so "users" -> "USERS" must become a
	// rename; dropping and recreating would lose all data.
	renamed := map[string]string{} // old name -> new name
	for name := range from.Tables {
		if _, exists := to.Tables[name]; exists {
			continue
		}
		newName := caseOnlyRename(to.Tables, name)
		if newName == "" {
			changes = append(changes, Change{
				Type:        DropTable,
				Object:      name,
				Description: fmt.Sprintf("Drop table %q", name),
				SQL:         []string{fmt.Sprintf("DROP TABLE %s;", schema.QuoteIdentifier(name))},
				Destructive: true,
			})
			continue
		}
		renamed[name] = newName
		// The rename copies data to the new name and drops the old table,
		// which also drops its indexes and triggers; mark the table so
		// those are recreated.
		recreatedTables[name] = true
		recreatedTables[newName] = true
		changes = append(changes, Change{
			Type:        RenameTable,
			Object:      name,
			Description: fmt.Sprintf("Rename table %q to %q", name, newName),
			SQL:         generateCaseRenameSQL(from.Tables[name], to.Tables[newName]),
		})
	}

	// New tables (skip rename targets, they already exist under another name)
	for name, table := range to.Tables {
		if isRenameTarget(renamed, name) {
			continue
		}
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

	// Modified tables (renamed pairs included: the rename runs first)
	for name, toTable := range to.Tables {
		fromTable, exists := from.Tables[name]
		if !exists {
			orig, ok := renameSource(renamed, name)
			if !ok {
				continue
			}
			// Operate under the new name; the rename already ran.
			adjusted := *from.Tables[orig]
			adjusted.Name = name
			fromTable = &adjusted
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

// caseOnlyRename returns the name of the target table that differs from
// name only by letter case, or "" when there is no unambiguous match.
func caseOnlyRename(tables map[string]*schema.Table, name string) string {
	match := ""
	for toName := range tables {
		if toName != name && strings.EqualFold(toName, name) {
			if match != "" {
				return "" // ambiguous, do not guess
			}
			match = toName
		}
	}
	return match
}

// generateCaseRenameSQL builds the statements that rename a table by
// copying it through a temporary table. SQLite resolves table names
// case-insensitively, so it refuses both CREATE under the new name and
// ALTER TABLE RENAME to a name that differs only in letter case.
func generateCaseRenameSQL(from, to *schema.Table) []string {
	return generateRecreateSQLTo(from.Name, to.Name, from, to)
}

func isRenameTarget(renamed map[string]string, name string) bool {
	for _, to := range renamed {
		if to == name {
			return true
		}
	}
	return false
}

func renameSource(renamed map[string]string, name string) (string, bool) {
	for from, to := range renamed {
		if to == name {
			return from, true
		}
	}
	return "", false
}

func diffTableColumns(from, to *schema.Table) []Change {
	var changes []Change

	var droppedCols []schema.Column
	for _, col := range from.Columns {
		if !to.HasColumn(col.Name) {
			droppedCols = append(droppedCols, col)
		}
	}

	var newCols []schema.Column
	for _, col := range to.Columns {
		if !from.HasColumn(col.Name) {
			newCols = append(newCols, col)
		}
	}

	// Check for column rename
	if len(droppedCols) == 1 && len(newCols) == 1 {
		oldCol := droppedCols[0]
		newCol := newCols[0]

		// Ensure column properties match
		if !columnChanged(oldCol, newCol) {
			fromNorm := normalizeSQL(from.SQL)
			toNorm := normalizeSQL(to.SQL)

			// Replace old column name with new column name in normalized SQL
			// Use regex to ensure word boundaries
			oldNameLower := regexp.QuoteMeta(strings.ToLower(oldCol.Name))
			re := regexp.MustCompile(`\b` + oldNameLower + `\b`)
			fromNormRenamed := re.ReplaceAllString(fromNorm, strings.ToLower(newCol.Name))

			if fromNormRenamed == toNorm {
				return []Change{{
					Type:   RenameColumn,
					Object: from.Name,
					Description: fmt.Sprintf(
						"Rename column %q to %q on table %q",
						oldCol.Name,
						newCol.Name,
						from.Name,
					),
					SQL: []string{
						fmt.Sprintf(
							"ALTER TABLE %s RENAME COLUMN %s TO %s;",
							schema.QuoteIdentifier(from.Name),
							schema.QuoteIdentifier(oldCol.Name),
							schema.QuoteIdentifier(newCol.Name),
						),
					},
					Destructive: false,
				}}
			}
		}
	}

	if len(droppedCols) > 0 {
		// Column removed (or complex rename) - needs table recreation
		return []Change{recreateTableChange(from.Name, from, to)}
	}

	// If new columns cannot be added via ALTER TABLE without losing
	// constraints or diverging from the target definition,
	// we need RECREATE_TABLE
	if len(newCols) > 0 && !canAddColumns(from, to, newCols) {
		return []Change{recreateTableChange(from.Name, from, to)}
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
	definitionChanged := normalizeSQL(from.SQL) != normalizeSQL(to.SQL)
	if len(newCols) == 0 && definitionChanged {
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

func columnChanged(from, to schema.Column) bool {
	// Compare type (case-insensitive)
	if !strings.EqualFold(from.Type, to.Type) {
		return true
	}

	if from.Hidden != to.Hidden {
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
		fromDefault = strings.ToLower(strings.TrimSpace(*from.Default))
	}
	if to.Default != nil {
		toDefault = strings.ToLower(strings.TrimSpace(*to.Default))
	}
	if fromDefault != toDefault {
		return true
	}

	return false
}

// canAddColumns reports whether every new column can be appended with
// ALTER TABLE ADD COLUMN such that the result matches the target exactly.
// Fast static guards cover the documented SQLite restrictions (UNIQUE and
// PRIMARY KEY columns, generated columns, non-constant defaults), and a
// simulation replays the ADD COLUMN statements on a scratch in-memory copy
// of the source table to catch everything PRAGMAs cannot see, such as
// CHECK constraints and other table-level constraint changes.
func canAddColumns(from, to *schema.Table, newCols []schema.Column) bool {
	// ALTER TABLE ADD COLUMN always appends to the end, so new columns in
	// the middle require a recreation to preserve column order.
	if !newColumnsAtEnd(from, to) {
		return false
	}

	for _, col := range newCols {
		switch {
		case col.PrimaryKey > 0, col.Hidden != 0:
			return false
		case slices.Contains(to.UniqueColumns, col.Name),
			slices.Contains(to.ForeignKeyColumns, col.Name):
			return false
		case col.NotNull && col.Default == nil:
			// A default would have to be synthesized, which diverges from
			// the target definition.
			return false
		case col.Default != nil && isNonConstantDefault(*col.Default):
			return false
		}
	}

	return simulatedTableMatches(from, to, newCols)
}

// simulatedTableMatches replays the generated ADD COLUMN statements on a
// scratch in-memory database and reports whether the resulting table matches
// the target. If SQLite rejects any statement or the result diverges from
// the target, the table must be recreated instead.
func simulatedTableMatches(from, to *schema.Table, newCols []schema.Column) bool {
	if from.SQL == "" {
		// Hand-built schemas without SQL cannot be simulated; the static
		// guards alone decide.
		return true
	}

	// The scratch database is transient, in-memory, and contains no data,
	// so it does not need cancellation: context.Background() is intentional.
	ctx := context.Background()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return false
	}
	defer func() {
		_ = db.Close()
	}()

	if _, err := db.ExecContext(ctx, from.SQL); err != nil {
		return false
	}
	for _, col := range newCols {
		if _, err := db.ExecContext(ctx, generateAddColumnSQL(from.Name, col)); err != nil {
			return false
		}
	}

	simulated, err := parser.FromDB(ctx, db)
	if err != nil {
		return false
	}
	result := simulated.Tables[from.Name]
	if result == nil {
		return false
	}

	return tableMatches(result, to)
}

// tableMatches reports whether two tables are equivalent for migration
// purposes: same definition, same columns in the same order, and the same
// constraint columns.
func tableMatches(from, to *schema.Table) bool {
	if normalizeSQL(from.SQL) != normalizeSQL(to.SQL) {
		return false
	}
	if len(from.Columns) != len(to.Columns) {
		return false
	}
	for i, toCol := range to.Columns {
		fromCol := &from.Columns[i]
		if fromCol.Name != toCol.Name || columnChanged(*fromCol, toCol) {
			return false
		}
	}
	if !slices.Equal(from.UniqueColumns, to.UniqueColumns) {
		return false
	}
	return slices.Equal(from.ForeignKeyColumns, to.ForeignKeyColumns)
}

// isNonConstantDefault reports whether a default value is rejected by
// ALTER TABLE ADD COLUMN on a non-empty table.
func isNonConstantDefault(d string) bool {
	d = strings.ToUpper(strings.TrimSpace(d))
	return strings.HasPrefix(d, "(") ||
		d == "CURRENT_TIME" ||
		d == "CURRENT_DATE" ||
		d == "CURRENT_TIMESTAMP"
}

// newColumnsAtEnd checks if all new columns appear at the end of the target schema.
// This is important because ALTER TABLE ADD COLUMN always appends to the end.
// If new columns should be in the middle, we need RECREATE_TABLE to preserve order.
func newColumnsAtEnd(from, to *schema.Table) bool {
	seenNew := false
	for _, col := range to.Columns {
		isNew := !from.HasColumn(col.Name)
		if isNew {
			seenNew = true
		} else if seenNew {
			return false
		}
	}
	return true
}

func generateAddColumnSQL(tableName string, col schema.Column) string {
	var sb strings.Builder
	fmt.Fprintf(
		&sb,
		"ALTER TABLE %s ADD COLUMN %s",
		schema.QuoteIdentifier(tableName),
		schema.QuoteIdentifier(col.Name),
	)

	if col.Type != "" {
		fmt.Fprintf(&sb, " %s", col.Type)
	}

	if col.NotNull {
		if col.Default != nil {
			fmt.Fprintf(&sb, " NOT NULL DEFAULT %s", *col.Default)
		} else {
			// SQLite requires a DEFAULT for NOT NULL columns added via
			// ALTER TABLE, so fall back to a type-appropriate value.
			fmt.Fprintf(&sb, " NOT NULL DEFAULT %s", defaultForType(col.Type))
		}
	} else if col.Default != nil {
		fmt.Fprintf(&sb, " DEFAULT %s", *col.Default)
	}

	sb.WriteString(";")
	return sb.String()
}

// defaultForType returns a sensible default value for a SQLite type.
func defaultForType(colType string) string {
	switch strings.ToUpper(colType) {
	case "INTEGER", "INT", "BIGINT", "SMALLINT", "TINYINT":
		return "0"
	case "REAL", "FLOAT", "DOUBLE":
		return "0.0"
	case "BLOB":
		return "X''"
	default:
		return "''"
	}
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
	return generateRecreateSQLTo(name, name, from, to)
}

// generateRecreateSQLTo builds the statements that recreate the table under
// finalName, copying the data from its current name. A temporary table is
// used because SQLite resolves table names case-insensitively: the new
// table must not exist while the old one is still there.
func generateRecreateSQLTo(name, finalName string, from, to *schema.Table) []string {
	tempName := name + "__new"

	// Build the data migration in one pass so the INSERT column list and
	// the SELECT expressions stay aligned: shared columns are copied
	// (with COALESCE for columns that became NOT NULL), new columns are
	// filled with their default, and generated columns are left out
	// because SQLite computes them automatically.
	var insertCols []string
	var selectExprs []string
	for i := range to.Columns {
		toCol := &to.Columns[i]
		if toCol.Hidden != 0 {
			continue
		}

		fromCol := from.GetColumn(toCol.Name)
		if fromCol == nil {
			// New column: fill it with its default so NOT NULL holds for
			// the migrated rows.
			if toCol.Default != nil {
				insertCols = append(insertCols, schema.QuoteIdentifier(toCol.Name))
				selectExprs = append(selectExprs, *toCol.Default)
			} else if toCol.NotNull {
				insertCols = append(insertCols, schema.QuoteIdentifier(toCol.Name))
				selectExprs = append(selectExprs, defaultForType(toCol.Type))
			}
			continue
		}

		insertCols = append(insertCols, schema.QuoteIdentifier(toCol.Name))

		if !fromCol.NotNull && toCol.NotNull {
			// Column became NOT NULL, provide a default value to prevent constraint failure
			defValue := defaultForType(toCol.Type)
			if toCol.Default != nil {
				defValue = *toCol.Default
			}
			selectExprs = append(
				selectExprs,
				fmt.Sprintf("COALESCE(%s, %s)", schema.QuoteIdentifier(toCol.Name), defValue),
			)
		} else {
			selectExprs = append(selectExprs, schema.QuoteIdentifier(toCol.Name))
		}
	}

	createSQL := replaceTableName(to.SQL, tempName)

	stmts := []string{
		ensureSemicolon(createSQL),
	}

	if len(insertCols) > 0 {
		stmts = append(
			stmts,
			fmt.Sprintf(
				"INSERT INTO %s (%s) SELECT %s FROM %s;",
				schema.QuoteIdentifier(tempName),
				strings.Join(insertCols, ", "),
				strings.Join(selectExprs, ", "),
				schema.QuoteIdentifier(name),
			),
		)
	}

	stmts = append(
		stmts,
		fmt.Sprintf("DROP TABLE %s;", schema.QuoteIdentifier(name)),
		fmt.Sprintf(
			"ALTER TABLE %s RENAME TO %s;",
			schema.QuoteIdentifier(tempName),
			schema.QuoteIdentifier(finalName),
		),
	)

	return stmts
}

var tableNameRe = regexp.MustCompile(
	`(?i)(CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?)\s*(?:"(?:[^"]|"")*"|'(?:[^']|'')*'|\x60(?:[^\x60]|\x60\x60)*\x60|\[[^\]]*\]|[a-zA-Z0-9_]+)`,
)

func replaceTableName(sql, newName string) string {
	return tableNameRe.ReplaceAllStringFunc(sql, func(match string) string {
		prefix := tableNameRe.FindStringSubmatch(match)[1]
		return prefix + schema.QuoteIdentifier(newName)
	})
}

// diffNamed compares named schema objects (indexes, views, or triggers)
// between the two schemas. Objects attached to a recreated table are
// recreated unconditionally; when dropOnRecreate is true they are also
// dropped up front, which triggers require because they are not dropped
// implicitly with their table.
func diffNamed[T any](
	from, to map[string]T,
	tableOf func(T) string,
	sqlOf func(T) string,
	recreatedTables map[string]bool,
	dropOnRecreate bool,
	createType, dropType ChangeType,
) []Change {
	var changes []Change

	// Dropped objects
	for name, obj := range from {
		if recreatedTables[tableOf(obj)] {
			if dropOnRecreate {
				changes = append(changes, dropChange(dropType, name, "(will recreate)"))
			}
			continue
		}
		if _, exists := to[name]; !exists {
			changes = append(changes, dropChange(dropType, name, ""))
		}
	}

	// New or modified objects
	for name, obj := range to {
		if recreatedTables[tableOf(obj)] {
			changes = append(changes, createChange(createType, name, sqlOf(obj)))
			continue
		}

		prev, exists := from[name]
		switch {
		case !exists:
			changes = append(changes, createChange(createType, name, sqlOf(obj)))
		case normalizeSQL(sqlOf(prev)) != normalizeSQL(sqlOf(obj)):
			changes = append(changes, dropChange(dropType, name, "(will recreate)"))
			changes = append(changes, createChange(createType, name, sqlOf(obj)))
		}
	}

	return changes
}

// objectKind returns the human-readable object kind for a change type.
func objectKind(t ChangeType) string {
	switch t {
	case CreateTable, DropTable, RecreateTable, RenameTable:
		return "table"
	case AddColumn, RenameColumn:
		return "column"
	case CreateIndex, DropIndex:
		return "index"
	case CreateView, DropView:
		return "view"
	case CreateTrigger, DropTrigger:
		return "trigger"
	default:
		return "object"
	}
}

func createChange(t ChangeType, name, objectSQL string) Change {
	return Change{
		Type:        t,
		Object:      name,
		Description: fmt.Sprintf("Create %s %q", objectKind(t), name),
		SQL:         []string{ensureSemicolon(objectSQL)},
	}
}

func dropChange(t ChangeType, name, suffix string) Change {
	description := fmt.Sprintf("Drop %s %q", objectKind(t), name)
	if suffix != "" {
		description += " " + suffix
	}
	return Change{
		Type:        t,
		Object:      name,
		Description: description,
		SQL: []string{
			fmt.Sprintf(
				"DROP %s IF EXISTS %s;",
				strings.ToUpper(objectKind(t)),
				schema.QuoteIdentifier(name),
			),
		},
	}
}

func diffIndexes(from, to *schema.Database, recreatedTables map[string]bool) []Change {
	return diffNamed(
		from.Indexes, to.Indexes,
		func(i *schema.Index) string { return i.Table },
		func(i *schema.Index) string { return i.SQL },
		recreatedTables,
		false,
		CreateIndex, DropIndex,
	)
}

func diffViews(from, to *schema.Database) []Change {
	return diffNamed(
		from.Views, to.Views,
		func(_ *schema.View) string { return "" },
		func(v *schema.View) string { return v.SQL },
		nil,
		false,
		CreateView, DropView,
	)
}

func diffTriggers(from, to *schema.Database, recreatedTables map[string]bool) []Change {
	return diffNamed(
		from.Triggers, to.Triggers,
		func(t *schema.Trigger) string { return t.Table },
		func(t *schema.Trigger) string { return t.SQL },
		recreatedTables,
		true,
		CreateTrigger, DropTrigger,
	)
}

func ensureSemicolon(sql string) string {
	sql = strings.TrimSpace(sql)
	if !strings.HasSuffix(sql, ";") {
		sql += ";"
	}
	return sql
}

// sortChanges orders changes for safe execution.
func sortChanges(changes []Change) {
	priority := map[ChangeType]int{
		DropTrigger:   1,
		DropView:      2,
		DropIndex:     3,
		DropTable:     4,
		RecreateTable: 5,
		RenameTable:   6,
		CreateTable:   7,
		RenameColumn:  8,
		AddColumn:     9,
		CreateIndex:   10,
		CreateView:    11,
		CreateTrigger: 12,
	}

	slices.SortStableFunc(changes, func(a, b Change) int {
		pa, pb := priority[a.Type], priority[b.Type]
		if pa != pb {
			return cmp.Compare(pa, pb)
		}
		return cmp.Compare(a.Object, b.Object)
	})
}

// HasDestructive returns true if any changes are destructive.
func HasDestructive(changes []Change) bool {
	for _, c := range changes {
		if c.Destructive {
			return true
		}
	}
	return false
}
