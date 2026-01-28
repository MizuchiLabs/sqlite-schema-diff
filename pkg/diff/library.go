package diff

import (
	"sqlite-schema-diff/pkg/parser"
)

// Compare compares a database against a schema directory and returns changes
func Compare(dbPath, schemaDir string) ([]Change, error) {
	current, err := parser.FromDatabase(dbPath)
	if err != nil {
		return nil, err
	}

	target, err := parser.FromDirectory(schemaDir)
	if err != nil {
		return nil, err
	}

	return Diff(current, target), nil
}

// CompareDatabases compares two databases
func CompareDatabases(fromDB, toDB string) ([]Change, error) {
	from, err := parser.FromDatabase(fromDB)
	if err != nil {
		return nil, err
	}

	to, err := parser.FromDatabase(toDB)
	if err != nil {
		return nil, err
	}

	return Diff(from, to), nil
}

// ApplySchema applies a schema directory to a database
func ApplySchema(dbPath, schemaDir string, opts ApplyOptions) error {
	changes, err := Compare(dbPath, schemaDir)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		return nil
	}

	return Apply(dbPath, changes, opts)
}
