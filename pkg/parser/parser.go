// Package parser provides functions to load and parse SQLite schemas
package parser

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"
	_ "modernc.org/sqlite"
)

var baseFS fs.FS

// SetBaseFS sets the base filesystem for reading schema files.
// Use an embed.FS to read from embedded files.
// Pass nil to revert to the OS filesystem.
func SetBaseFS(fsys fs.FS) {
	baseFS = fsys
}

func BaseFS() fs.FS {
	return baseFS
}

// FromDB extracts the schema from an open database connection
func FromDB(db *sql.DB) (*schema.Database, error) {
	return extractSchema(db)
}

// FromSQL parses SQL by executing it against an in-memory SQLite database
func FromSQL(sqlContent string) (*schema.Database, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("create in-memory database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if _, err := db.Exec(sqlContent); err != nil {
		return nil, fmt.Errorf("execute schema SQL: %w", err)
	}

	return extractSchema(db)
}

func ReadFiles(dir string) (*schema.Database, error) {
	var err error
	var files []string
	if baseFS != nil {
		files, err = fromFS(baseFS, dir)
		if err != nil {
			return nil, err
		}
	} else {
		files, err = fromDir(dir)
		if err != nil {
			return nil, err
		}
	}

	// Create the in-memory database once
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("create in-memory database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	// Execute each file individually
	for _, path := range files {
		content, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return nil, fmt.Errorf("execute %s: %w", filepath.Base(path), err)
		}
	}
	return extractSchema(db)
}

// fromDir loads all .sql files from a directory
func fromDir(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".sql") {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

// fromFS loads all .sql files from an fs.FS
func fromFS(fsys fs.FS, dir string) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".sql") {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

// extractSchema extracts the complete schema from a database connection
func extractSchema(db *sql.DB) (*schema.Database, error) {
	s := schema.NewDatabase()

	if err := extractTables(db, s); err != nil {
		return nil, err
	}
	if err := extractIndexes(db, s); err != nil {
		return nil, err
	}
	if err := extractViews(db, s); err != nil {
		return nil, err
	}
	if err := extractTriggers(db, s); err != nil {
		return nil, err
	}

	return s, nil
}

func extractTables(db *sql.DB, s *schema.Database) error {
	// First pass: collect all table names and SQL
	// We must close this query before running nested queries (driver limitation)
	rows, err := db.Query(`
		SELECT name, sql FROM sqlite_master 
		WHERE type='table' AND name NOT LIKE 'sqlite_%' 
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

		colRows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", ti.name))
		if err != nil {
			return err
		}

		for colRows.Next() {
			var cid int
			var cname, ctype string
			var notnull, pk int
			var dflt sql.NullString

			if err := colRows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
				_ = colRows.Close()
				return err
			}

			col := schema.Column{
				Name:       cname,
				Type:       ctype,
				NotNull:    notnull == 1,
				PrimaryKey: pk,
			}
			if dflt.Valid {
				col.Default = &dflt.String
			}
			table.Columns = append(table.Columns, col)
		}
		_ = colRows.Close()

		s.Tables[ti.name] = table
	}

	return nil
}

func extractIndexes(db *sql.DB, s *schema.Database) error {
	rows, err := db.Query(`
		SELECT name, tbl_name, sql FROM sqlite_master 
		WHERE type='index' AND sql IS NOT NULL AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var name, table string
		var sqlText sql.NullString
		if err := rows.Scan(&name, &table, &sqlText); err != nil {
			return err
		}
		if !sqlText.Valid {
			continue
		}
		s.Indexes[name] = &schema.Index{Name: name, Table: table, SQL: sqlText.String}
	}

	return rows.Err()
}

func extractViews(db *sql.DB, s *schema.Database) error {
	rows, err := db.Query(`
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

func extractTriggers(db *sql.DB, s *schema.Database) error {
	rows, err := db.Query(`
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
