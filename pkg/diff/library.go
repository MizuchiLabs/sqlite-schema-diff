package diff

import (
	"fmt"

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

// ShowChanges prints a list of changes
func ShowChanges(changes []Change) {
	for _, c := range changes {
		symbol := "+"
		if c.Destructive {
			symbol = "-"
		}
		fmt.Printf("[%s] %s: %s\n", symbol, c.Type, c.Description)
	}

	destructive := 0
	for _, c := range changes {
		if c.Destructive {
			destructive++
		}
	}

	fmt.Printf("\nTotal changes: %d (%d destructive)\n", len(changes), destructive)
}
