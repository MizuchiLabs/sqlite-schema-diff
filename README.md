# sqlite-schema-diff

A schema-first approach to SQLite migrations. Define your schema in `.sql` files, and let the tool figure out what changed.

## Why?

Traditional migrations are error-prone and hard to maintain. Instead:

1. **Define** your desired schema in SQL files
2. **Diff** against your database to see what changed
3. **Apply** changes automatically

No more numbered migration files. No more merge conflicts. Just SQL.

## Installation

```bash
go install github.com/mizuchilabs/sqlite-schema-diff/cmd@latest
```

Or build from source:

```bash
git clone https://github.com/mizuchilabs/sqlite-schema-diff
cd sqlite-schema-diff
go build -o sqlite-schema-diff ./cmd
```

## Quick Start

```bash
# Define your schema
cat > schema/users.sql << 'EOF'
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
EOF

# Preview changes
sqlite-schema-diff diff --database app.db --schema ./schema

# Apply changes
sqlite-schema-diff apply --database app.db --schema ./schema
```

## CLI Reference

### `diff` — Preview changes

```bash
sqlite-schema-diff diff --database app.db --schema ./schema
sqlite-schema-diff diff --database app.db --schema ./schema --sql  # Output raw SQL
```

### `apply` — Apply changes

```bash
sqlite-schema-diff apply --database app.db --schema ./schema
```

| Flag                  | Description                               |
| --------------------- | ----------------------------------------- |
| `--dry-run`           | Show what would happen without applying   |
| `--force`             | Skip confirmation for destructive changes |
| `--skip-destructive`  | Skip DROP operations                      |
| `--backup=false`      | Disable automatic backup                  |
| `--show-changes=true` | Show changes before applying              |

### `dump` — Export existing schema

```bash
sqlite-schema-diff dump --database app.db --output ./schema
```

## Library Usage

```go
import "github.com/mizuchilabs/sqlite-schema-diff/pkg/diff"

// Compare and get changes
changes, err := diff.Compare("app.db", "./schema")
if err != nil {
    log.Fatal(err)
}

// Check what's changing
for _, c := range changes {
    fmt.Printf("%s: %s (destructive: %v)\n", c.Type, c.Description, c.Destructive)
}

// Generate SQL without applying
sql := diff.GenerateSQL(changes)

// Apply changes
err = diff.Apply("app.db", "./schema", diff.ApplyOptions{
    Backup:          true,
    SkipDestructive: false,
})
```

### Available Functions

| Function                         | Description                     |
| -------------------------------- | ------------------------------- |
| `Compare(dbPath, schemaDir)`     | Diff database against SQL files |
| `CompareDatabases(fromDB, toDB)` | Diff two databases              |
| `GenerateSQL(changes)`           | Generate migration SQL          |
| `HasDestructive(changes)`        | Check for destructive changes   |
| `Apply(dbPath, schemaDir, opts)` | Apply changes to database       |

## Supported Objects

- Tables (with columns, constraints, foreign keys)
- Indexes
- Views
- Triggers

## Destructive Changes

Operations that may lose data are flagged as destructive:

| Operation        | Risk                          |
| ---------------- | ----------------------------- |
| `DROP TABLE`     | Deletes table and all data    |
| `DROP COLUMN`    | Loses column data             |
| `RECREATE TABLE` | Required for some alterations |

By default, the CLI:

- Creates a backup before applying (`app.db.backup`)
- Prompts for confirmation on destructive changes

Use `--skip-destructive` to safely apply only additive changes.

## Schema Organization

Organize your `.sql` files however you like:

```
schema/
├── tables/
│   ├── users.sql
│   └── posts.sql
├── indexes.sql
└── triggers.sql
```

All files are merged. Each object name must be unique across all files.

## Examples

See `examples/` directory for working examples.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
