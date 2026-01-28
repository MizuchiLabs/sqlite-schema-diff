# sqlite-schema-diff

A lightweight SQLite schema diff tool that replaces traditional migrations. Define your database schema in SQL files and let the tool handle synchronization.

## Features

- **Schema-first approach**: Define tables, indexes, views, and triggers in `.sql` files
- **Automatic diffing**: Compare current database state with desired schema
- **Safe migrations**: Warns on destructive operations with confirmation prompts
- **Library and CLI**: Use as a command-line tool or import as a Go library
- **Modern SQLite**: Targets latest SQLite versions with ALTER TABLE support

## Installation

```bash
go install github.com/yourusername/sqlite-schema-diff/cmd@latest
```

Or build from source:

```bash
git clone https://github.com/yourusername/sqlite-schema-diff
cd sqlite-schema-diff
go build -o sqlite-schema-diff ./cmd
```

## Quick Start

### 1. Define your schema

Create a directory with `.sql` files:

```sql
-- schema/users.sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
```

### 2. Compare schema against database

```bash
sqlite-schema-diff diff --database myapp.db --schema ./schema
```

### 3. Apply changes

```bash
sqlite-schema-diff apply --database myapp.db --schema ./schema
```

## CLI Usage

### Commands

#### `diff` - Show schema differences

```bash
sqlite-schema-diff diff --database myapp.db --schema ./schema

# Output migration SQL
sqlite-schema-diff diff --database myapp.db --schema ./schema --sql
```

#### `apply` - Apply schema changes

```bash
# Interactive (prompts for destructive changes)
sqlite-schema-diff apply --database myapp.db --schema ./schema

# Dry run (show what would happen)
sqlite-schema-diff apply --database myapp.db --schema ./schema --dry-run

# Force apply without confirmation
sqlite-schema-diff apply --database myapp.db --schema ./schema --force

# Skip destructive changes
sqlite-schema-diff apply --database myapp.db --schema ./schema --skip-destructive

# No backup
sqlite-schema-diff apply --database myapp.db --schema ./schema --backup=false
```

#### `dump` - Export database schema to files

```bash
sqlite-schema-diff dump --database myapp.db --output ./schema
```

This creates organized schema files:

- `tables.sql` - All table definitions
- `indexes.sql` - All indexes
- `views.sql` - All views
- `triggers.sql` - All triggers

## Library Usage

Use sqlite-schema-diff as a library in your Go projects:

```go
package main

import (
    "fmt"
    "log"

    "sqlite-schema-diff/pkg/diff"
    "sqlite-schema-diff/pkg/parser"
)

func main() {
    // Compare database with schema directory
    changes, err := diff.CompareSchemas("myapp.db", "./schema")
    if err != nil {
        log.Fatal(err)
    }

    // Check for changes
    if len(changes) == 0 {
        fmt.Println("Schema is up to date!")
        return
    }

    // Print changes
    for _, change := range changes {
        fmt.Printf("%s: %s\n", change.Type, change.Description)
    }

    // Apply changes
    opts := diff.ApplyOptions{
        DryRun:            false,
        SkipDestructive:   false,
        BackupBeforeApply: true,
    }

    if err := diff.Apply("myapp.db", changes, opts); err != nil {
        log.Fatal(err)
    }

    fmt.Println("Schema updated successfully!")
}
```

### Advanced Library Usage

```go
// Compare two databases
changes, err := diff.CompareDatabases("old.db", "new.db")

// Extract schema from database
schema, err := parser.ExtractFromDatabase("myapp.db")

// Load schema from directory
schema, err := parser.LoadSchemaFromDirectory("./schema")

// Parse SQL file
schema, err := parser.ParseSchemaFile(sqlContent)

// Generate migration SQL
sql := diff.GenerateSQL(changes)

// Check for destructive changes
hasDestructive := diff.HasDestructiveChanges(changes)
```

## Destructive Operations

The tool identifies and warns about operations that may lose data:

- **DROP TABLE**: Deletes entire table and data
- **DROP COLUMN**: Loses column data (requires table recreation in SQLite)
- **RECREATE TABLE**: May lose data if column types change incompatibly

When applying changes:

- Interactive mode prompts for confirmation
- Use `--force` to skip prompts
- Use `--skip-destructive` to skip these operations
- Backups are created by default (use `--backup=false` to disable)

## Schema Organization

You can organize schema files however you like:

```
schema/
├── 01_users.sql
├── 02_posts.sql
├── 03_comments.sql
├── indexes/
│   ├── users.sql
│   └── posts.sql
└── triggers/
    └── audit.sql
```

Files are loaded alphabetically and merged. Duplicate object names will cause an error.

## How It Works

1. **Extract current schema**: Queries `sqlite_master` to get current database state
2. **Load target schema**: Parses `.sql` files from schema directory
3. **Compute diff**: Compares schemas and generates change list
4. **Generate SQL**: Creates migration statements
5. **Apply changes**: Executes migration in a transaction with foreign keys disabled

### Supported Objects

- ✅ Tables
- ✅ Indexes (including unique, partial, and expression indexes)
- ✅ Views
- ✅ Triggers
- ✅ Foreign keys
- ✅ Check constraints
- ✅ Unique constraints

## Comparison with Traditional Migrations

### Traditional Migrations

```
migrations/
├── 001_create_users.sql
├── 002_add_email_index.sql
├── 003_create_posts.sql
├── 004_alter_users.sql
└── ... (grows forever)
```

Problems:

- Migration files accumulate indefinitely
- Hard to see current schema state
- Merge conflicts on migration numbers
- Must track applied migrations

### Schema-First Approach

```
schema/
├── users.sql
├── posts.sql
└── indexes.sql
```

Benefits:

- Schema is the source of truth
- Easy to review current state
- No migration numbering conflicts
- Tool handles synchronization

## Limitations

- **Complex table alterations**: SQLite has limited ALTER TABLE support. The tool recreates tables when necessary, which may lose data if not careful.
- **Data migrations**: The tool doesn't handle data transformations. For complex migrations, you may need custom SQL.
- **Column renames**: SQLite doesn't distinguish renames from drop+add, so data will be lost unless you manually handle it.

## Examples

See `examples/` directory for sample schema files.

## Development

```bash
# Run tests
go test ./...

# Build
go build -o sqlite-schema-diff ./cmd

# Run example
./sqlite-schema-diff diff --database examples/test.db --schema examples/schema
```

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
