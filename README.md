# sqlite-schema-diff

A lightweight tool that compares SQLite database schemas against `.sql` files and applies changes automatically. Replaces traditional migrations with a schema-first approach.

## Quick Start

```bash
# 1. Define your schema in SQL files
echo "CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE
);" > schema/users.sql

# 2. See what changes are needed
sqlite-schema-diff diff --database app.db --schema ./schema

# 3. Apply changes
sqlite-schema-diff apply --database app.db --schema ./schema
```

## Installation

```bash
# From source
git clone https://github.com/mizuchilabs/sqlite-schema-diff
cd sqlite-schema-diff
go build -o sqlite-schema-diff ./cmd

# Or install globally
go install ./cmd
```

## CLI Commands

| Command | Description                      |
| ------- | -------------------------------- |
| `diff`  | Show schema differences          |
| `apply` | Apply schema changes to database |
| `dump`  | Export database schema to files  |

### `diff` - Preview changes

```bash
sqlite-schema-diff diff --database app.db --schema ./schema

# Output as SQL
sqlite-schema-diff diff --database app.db --schema ./schema --sql
```

### `apply` - Apply changes

```bash
# Interactive (prompts for destructive changes)
sqlite-schema-diff apply --database app.db --schema ./schema

# Dry run - show what would happen
sqlite-schema-diff apply --database app.db --schema ./schema --dry-run

# Force apply without confirmation
sqlite-schema-diff apply --database app.db --schema ./schema --force

# Skip destructive operations
sqlite-schema-diff apply --database app.db --schema ./schema --skip-destructive

# Skip backup
sqlite-schema-diff apply --database app.db --schema ./schema --backup=false
```

### `dump` - Export existing schema

```bash
sqlite-schema-diff dump --database app.db --output ./dump
```

Outputs: `tables.sql`, `indexes.sql`, `views.sql`, `triggers.sql`

## Library Usage

```go
import (
    "fmt"
    "log"

    "sqlite-schema-diff/pkg/diff"
)

func main() {
    // Compare database with schema directory
    changes, err := diff.Compare("app.db", "./schema")
    if err != nil {
        log.Fatal(err)
    }

    if len(changes) == 0 {
        fmt.Println("Schema is up to date")
        return
    }

    // Print changes
    for _, c := range changes {
        symbol := "+"
        if c.Destructive {
            symbol = "-"
        }
        fmt.Printf("[%s] %s: %s\n", symbol, c.Type, c.Description)
    }

    // Apply changes
    opts := diff.ApplyOptions{
        DryRun:          false,
        SkipDestructive: false,
        Backup:          true,
    }

    if err := diff.Apply("app.db", changes, opts); err != nil {
        log.Fatal(err)
    }
}
```

### Library Functions

| Function                            | Description                        |
| ----------------------------------- | ---------------------------------- |
| `diff.Compare(dbPath, schemaDir)`   | Compare database with schema files |
| `diff.CompareDatabases(from, to)`   | Compare two databases              |
| `diff.GenerateSQL(changes)`         | Generate migration SQL             |
| `diff.HasDestructive(changes)`      | Check for destructive changes      |
| `diff.Apply(dbPath, changes, opts)` | Apply changes to database          |

## Schema Organization

Organize `.sql` files any way you like:

```
schema/
├── users.sql
├── posts.sql
├── indexes.sql
└── triggers.sql
```

Files will be sorted and merged. Each object (table, index, etc.) must have a unique name.

## What It Does

1. **Extracts** current schema from database via `sqlite_master`
2. **Parses** your `.sql` files
3. **Compares** and identifies missing/changed objects
4. **Generates** SQLite-compatible migration SQL
5. **Applies** changes with transaction safety

## Supported Objects

- Tables (CREATE TABLE)
- Indexes (CREATE INDEX)
- Views (CREATE VIEW)
- Triggers (CREATE TRIGGER)
- Foreign keys
- Check constraints
- Unique constraints

## Destructive Changes

The tool warns about operations that may lose data:

- `DROP TABLE` - deletes table and all data
- `DROP COLUMN` - loses column data
- `RECREATE TABLE` - may lose data on type changes

For destructive changes:

- Interactive mode prompts for confirmation
- Use `--force` to skip prompts
- Use `--skip-destructive` to skip these operations
- Backups are created by default (`--backup=false` to disable)

## Examples

See `examples/` directory for working examples.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

