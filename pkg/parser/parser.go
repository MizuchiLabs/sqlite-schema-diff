// Package parser provides functions to load and parse SQLite schemas.
package parser

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"

	// Pure-Go driver used for the in-memory databases that parse SQL.
	_ "modernc.org/sqlite"
)

// schemaQualifierRe matches schema qualifiers such as "main." in SQL code.
var schemaQualifierRe = regexp.MustCompile(`\bmain\s*\.\s*`)

// sqlStatement represents a SQL statement with its source file.
type sqlStatement struct {
	sql      string
	fileName string
}

// FromDB extracts the schema from an open database connection.
func FromDB(ctx context.Context, db *sql.DB) (*schema.Database, error) {
	return extractSchema(ctx, db)
}

// FromSQL parses SQL by executing it against an in-memory SQLite database.
func FromSQL(ctx context.Context, sqlContent string) (*schema.Database, error) {
	db, err := openMemory()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	// Strip schema qualifiers to allow SQL to work in any database context
	cleanedSQL := stripSchemaQualifiers(sqlContent)

	if _, err := db.ExecContext(ctx, cleanedSQL); err != nil {
		return nil, fmt.Errorf("execute schema SQL: %w", err)
	}

	return extractSchema(ctx, db)
}

// ReadFiles parses all .sql files in fsys, including subdirectories, into a
// schema. Use [os.DirFS] for a directory on disk, or [fs.Sub] to point an
// [embed.FS] at the schema directory.
func ReadFiles(ctx context.Context, fsys fs.FS) (*schema.Database, error) {
	var files []string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".sql") {
			return err
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return nil, err
	}
	slices.Sort(files)

	db, err := openMemory()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	// Tables run first so files can be ordered freely.
	var tableStmts, otherStmts []sqlStatement
	for _, file := range files {
		content, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		for _, stmt := range parseStatements(stripSchemaQualifiers(string(content)), file) {
			if isTableStatement(stmt.sql) {
				tableStmts = append(tableStmts, stmt)
			} else {
				otherStmts = append(otherStmts, stmt)
			}
		}
	}

	for _, stmt := range slices.Concat(tableStmts, otherStmts) {
		if _, err := db.ExecContext(ctx, stmt.sql); err != nil {
			return nil, fmt.Errorf("execute %s: %w", stmt.fileName, err)
		}
	}

	return extractSchema(ctx, db)
}

// openMemory opens an in-memory SQLite database. It is limited to a single
// connection because every connection to ":memory:" is a separate, empty
// database. Callers must close rows before running the next query.
func openMemory() (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("create in-memory database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// lexer scans SQL character by character and splits it into statements.
// Semicolons inside string literals, comments, and trigger bodies do not
// terminate a statement.
type lexer struct {
	runes       []rune
	pos         int
	current     strings.Builder
	inLineCmt   bool
	inBlockCmt  bool
	inString    bool
	stringEnd   rune
	triggerNest int
}

// parseStatements splits SQL content into individual statements.
func parseStatements(content, fileName string) []sqlStatement {
	l := &lexer{runes: []rune(content)}
	var stmts []sqlStatement

	for l.pos < len(l.runes) {
		if l.consume() {
			continue
		}

		r := l.runes[l.pos]

		if isBoundary(r) && l.current.Len() > 0 {
			l.trackTrigger()
		}

		switch r {
		case '-':
			if l.peek() == '-' {
				l.inLineCmt = true
				l.current.WriteString("--")
				l.pos += 2
			} else {
				l.write(r)
			}
		case '/':
			if l.peek() == '*' {
				l.inBlockCmt = true
				l.current.WriteString("/*")
				l.pos += 2
			} else {
				l.write(r)
			}
		case '\'', '"', '`':
			l.inString = true
			l.stringEnd = r
			l.write(r)
		case '[':
			l.inString = true
			l.stringEnd = ']'
			l.write(r)
		case ';':
			l.write(r)
			if l.triggerNest == 0 {
				if stmt := l.take(); stmt != "" {
					stmts = append(stmts, sqlStatement{sql: stmt, fileName: fileName})
				}
			}
		default:
			l.write(r)
		}
	}

	if stmt := l.take(); stmt != "" {
		if !strings.HasSuffix(stmt, ";") {
			stmt += ";"
		}
		stmts = append(stmts, sqlStatement{sql: stmt, fileName: fileName})
	}

	return stmts
}

// consume processes the rune at the current position while inside a comment
// or string literal. It reports whether such a state was active.
func (l *lexer) consume() bool {
	r := l.runes[l.pos]

	switch {
	case l.inLineCmt:
		l.pos++
		if r == '\n' {
			l.inLineCmt = false
		}
		l.current.WriteRune(r)

	case l.inBlockCmt:
		if r == '*' && l.peek() == '/' {
			l.pos += 2
			l.current.WriteString("*/")
		} else {
			l.pos++
			l.current.WriteRune(r)
		}

	case l.inString:
		l.pos++
		if r != l.stringEnd {
			l.current.WriteRune(r)
			break
		}
		// Handle escaped quotes like '' -> '
		doubled := l.pos < len(l.runes) && l.runes[l.pos] == l.stringEnd &&
			(l.stringEnd == '\'' || l.stringEnd == '"')
		if doubled {
			l.current.WriteRune(r)
			l.current.WriteRune(l.stringEnd)
			l.pos++
		} else {
			l.inString = false
			l.current.WriteRune(r)
		}

	default:
		return false
	}

	return true
}

// write appends the rune at the current position to the buffer and advances.
func (l *lexer) write(r rune) {
	l.current.WriteRune(r)
	l.pos++
}

// peek returns the rune after the current position, or 0 at the end.
func (l *lexer) peek() rune {
	if l.pos+1 < len(l.runes) {
		return l.runes[l.pos+1]
	}
	return 0
}

// take returns the buffered statement with leading comments stripped and
// resets the buffer. It returns an empty string for blank statements.
func (l *lexer) take() string {
	s := stripLeadingComments(l.current.String())
	l.current.Reset()
	if s == "" || s == ";" {
		return ""
	}
	return s
}

// trackTrigger updates the trigger nesting when the buffered text belongs to
// a CREATE TRIGGER statement and ends with BEGIN, CASE, or END.
func (l *lexer) trackTrigger() {
	s := l.current.String()
	head := stripLeadingComments(s)
	head = strings.ToUpper(strings.Join(strings.Fields(head[:min(len(head), 64)]), " "))
	isTrigger := strings.HasPrefix(head, "CREATE TRIGGER") ||
		strings.HasPrefix(head, "CREATE TEMP TRIGGER") ||
		strings.HasPrefix(head, "CREATE TEMPORARY TRIGGER")
	if !isTrigger {
		return
	}

	switch strings.ToUpper(s[strings.LastIndexFunc(s, isBoundary)+1:]) {
	case "BEGIN", "CASE":
		l.triggerNest++
	case "END":
		l.triggerNest = max(l.triggerNest-1, 0)
	}
}

func isBoundary(r rune) bool {
	return r <= ' ' || r == ';' || r == '(' || r == ')' || r == ','
}

func stripLeadingComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			idx := strings.Index(sql, "\n")
			if idx == -1 {
				return ""
			}
			sql = sql[idx:]
		case strings.HasPrefix(sql, "/*"):
			idx := strings.Index(sql, "*/")
			if idx == -1 {
				return ""
			}
			sql = sql[idx+2:]
		default:
			return sql
		}
	}
}

// isTableStatement checks if a SQL statement creates a table.
func isTableStatement(sql string) bool {
	sql = strings.TrimSpace(strings.ToUpper(sql))
	return strings.HasPrefix(sql, "CREATE TABLE")
}

// stripSchemaQualifiers removes schema qualifiers ("main.") from SQL.
// String literals and comments are copied verbatim so text like
// 'main.title' is not corrupted.
func stripSchemaQualifiers(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))

	for i := 0; i < len(sql); {
		j := nextSpecial(sql, i)
		out.WriteString(schemaQualifierRe.ReplaceAllString(sql[i:j], ""))
		i = copyProtected(sql, j, &out)
	}

	return out.String()
}

// nextSpecial returns the index of the next character that starts a quoted
// region or comment, or len(sql) if there is none.
func nextSpecial(sql string, from int) int {
	for i := from; i < len(sql); i++ {
		switch sql[i] {
		case '\'', '"', '`', '[', '-', '/':
			return i
		}
	}
	return len(sql)
}

// copyProtected copies the quoted region or comment starting at i verbatim
// and returns the index just past it.
func copyProtected(sql string, i int, out *strings.Builder) int {
	if i >= len(sql) {
		return i
	}

	switch sql[i] {
	case '\'', '"', '`':
		end := quotedEnd(sql, i)
		out.WriteString(sql[i:end])
		return end
	case '[':
		if end := strings.IndexByte(sql[i+1:], ']'); end != -1 {
			end += i + 2 // include the closing bracket
			out.WriteString(sql[i:end])
			return end
		}
	case '-':
		if i+1 < len(sql) && sql[i+1] == '-' {
			if end := strings.IndexByte(sql[i+2:], '\n'); end != -1 {
				end += i + 3 // include the newline
				out.WriteString(sql[i:end])
				return end
			}
			out.WriteString(sql[i:])
			return len(sql)
		}
	case '/':
		if i+1 < len(sql) && sql[i+1] == '*' {
			if end := strings.Index(sql[i+2:], "*/"); end != -1 {
				end += i + 4 // include the closing marker
				out.WriteString(sql[i:end])
				return end
			}
			out.WriteString(sql[i:])
			return len(sql)
		}
	}

	out.WriteByte(sql[i])
	return i + 1
}

// quotedEnd returns the index just past the quoted region starting at i.
// SQLite escapes quotes inside a region by doubling them.
func quotedEnd(sql string, i int) int {
	quote := sql[i]
	for j := i + 1; j < len(sql); j++ {
		if sql[j] != quote {
			continue
		}
		if j+1 < len(sql) && sql[j+1] == quote {
			j++
			continue
		}
		return j + 1
	}
	return len(sql)
}

// extractSchema extracts the complete schema from a database connection.
func extractSchema(ctx context.Context, db *sql.DB) (*schema.Database, error) {
	s := schema.NewDatabase()

	if err := extractTables(ctx, db, s); err != nil {
		return nil, err
	}
	if err := extractIndexes(ctx, db, s); err != nil {
		return nil, err
	}
	if err := extractViews(ctx, db, s); err != nil {
		return nil, err
	}
	if err := extractTriggers(ctx, db, s); err != nil {
		return nil, err
	}

	return s, nil
}

func extractTables(ctx context.Context, db *sql.DB, s *schema.Database) error {
	// Rows must be closed before the per-table queries, in-memory databases
	// only have one connection. Shadow tables (FTS5 internals) belong to
	// their virtual table and are left out.
	rows, err := db.QueryContext(ctx, `
		SELECT name, sql FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		AND name NOT IN (SELECT name FROM pragma_table_list WHERE schema='main' AND type='shadow')
		ORDER BY name
	`)
	if err != nil {
		return err
	}

	type tableInfo struct {
		name string
		sql  string
	}
	var tables []tableInfo

	for rows.Next() {
		var ti tableInfo
		if err := rows.Scan(&ti.name, &ti.sql); err != nil {
			_ = rows.Close()
			return err
		}
		tables = append(tables, ti)
	}
	_ = rows.Close()

	if err := rows.Err(); err != nil {
		return err
	}

	// Second pass: get column info for each table
	for _, ti := range tables {
		table := &schema.Table{Name: ti.name, SQL: ti.sql}

		if err := extractColumns(ctx, db, table); err != nil {
			return err
		}
		if table.UniqueColumns, err = uniqueConstraintColumns(ctx, db, ti.name); err != nil {
			return err
		}
		if table.ForeignKeyColumns, err = foreignKeyColumns(ctx, db, ti.name); err != nil {
			return err
		}

		s.Tables[ti.name] = table
	}

	return nil
}

func extractColumns(ctx context.Context, db *sql.DB, table *schema.Table) error {
	colRows, err := db.QueryContext(
		ctx,
		fmt.Sprintf("PRAGMA table_xinfo(%s)", schema.QuoteIdentifier(table.Name)),
	)
	if err != nil {
		return err
	}

	for colRows.Next() {
		var cid int
		var cname, ctype string
		var notnull, pk int
		var dflt sql.NullString
		var hidden int

		if err := colRows.Scan(
			&cid,
			&cname,
			&ctype,
			&notnull,
			&dflt,
			&pk,
			&hidden,
		); err != nil {
			_ = colRows.Close()
			return err
		}

		col := schema.Column{
			Name:       cname,
			Type:       ctype,
			NotNull:    notnull == 1,
			PrimaryKey: pk,
			Hidden:     hidden,
		}
		if dflt.Valid {
			col.Default = &dflt.String
		}
		table.Columns = append(table.Columns, col)
	}
	_ = colRows.Close()

	if colRows.Err() != nil {
		return colRows.Err()
	}

	return nil
}

// uniqueConstraintColumns returns the columns that participate in a UNIQUE
// table constraint. Constraint-backed indexes are marked with origin "u" by
// PRAGMA index_list; CREATE INDEX-based unique indexes are not included
// because they are diffed as separate objects.
func uniqueConstraintColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(
		ctx,
		fmt.Sprintf("PRAGMA index_list(%s)", schema.QuoteIdentifier(table)),
	)
	if err != nil {
		return nil, err
	}

	var constraintIdx []string
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if origin == "u" {
			constraintIdx = append(constraintIdx, name)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var columns []string
	for _, idx := range constraintIdx {
		idxRows, err := db.QueryContext(
			ctx,
			fmt.Sprintf("PRAGMA index_info(%s)", schema.QuoteIdentifier(idx)),
		)
		if err != nil {
			return nil, err
		}

		for idxRows.Next() {
			var seqno int
			var cid sql.NullInt64
			var cname sql.NullString
			if err := idxRows.Scan(&seqno, &cid, &cname); err != nil {
				_ = idxRows.Close()
				return nil, err
			}
			// Expression indexes have no column name; they cannot be
			// attributed to a column, so they are skipped.
			if cname.Valid && cname.String != "" && !slices.Contains(columns, cname.String) {
				columns = append(columns, cname.String)
			}
		}
		_ = idxRows.Close()
		if err := idxRows.Err(); err != nil {
			return nil, err
		}
	}

	slices.Sort(columns)
	return columns, nil
}

// foreignKeyColumns returns the columns that are the referencing side of a
// foreign key, from PRAGMA foreign_key_list.
func foreignKeyColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(
		ctx,
		fmt.Sprintf("PRAGMA foreign_key_list(%s)", schema.QuoteIdentifier(table)),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var columns []string
	for rows.Next() {
		var id, seq int
		var parentTable, from, to, onUpdate, onDelete, match sql.NullString
		if err := rows.Scan(
			&id,
			&seq,
			&parentTable,
			&from,
			&to,
			&onUpdate,
			&onDelete,
			&match,
		); err != nil {
			return nil, err
		}
		if from.Valid && from.String != "" && !slices.Contains(columns, from.String) {
			columns = append(columns, from.String)
		}
	}

	slices.Sort(columns)
	return columns, rows.Err()
}

func extractIndexes(ctx context.Context, db *sql.DB, s *schema.Database) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name, tbl_name, sql FROM sqlite_master
		WHERE type='index' AND sql IS NOT NULL
		ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var name, table, sqlText string
		if err := rows.Scan(&name, &table, &sqlText); err != nil {
			return err
		}
		s.Indexes[name] = &schema.Index{Name: name, Table: table, SQL: sqlText}
	}

	return rows.Err()
}

func extractViews(ctx context.Context, db *sql.DB, s *schema.Database) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name, sql FROM sqlite_master WHERE type='view' ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var name, sqlText string
		if err := rows.Scan(&name, &sqlText); err != nil {
			return err
		}
		s.Views[name] = &schema.View{Name: name, SQL: sqlText}
	}

	return rows.Err()
}

func extractTriggers(ctx context.Context, db *sql.DB, s *schema.Database) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name, tbl_name, sql FROM sqlite_master WHERE type='trigger' ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var name, table, sqlText string
		if err := rows.Scan(&name, &table, &sqlText); err != nil {
			return err
		}
		s.Triggers[name] = &schema.Trigger{Name: name, Table: table, SQL: sqlText}
	}

	return rows.Err()
}
