<p align="center">
<img src="./.github/logo.svg" width="80">
<br><br>
<img alt="GitHub Tag" src="https://img.shields.io/github/v/tag/MizuchiLabs/sqlite-schema-diff?label=Version">
<img alt="GitHub License" src="https://img.shields.io/github/license/MizuchiLabs/sqlite-schema-diff">
<img alt="GitHub Issues or Pull Requests" src="https://img.shields.io/github/issues/MizuchiLabs/sqlite-schema-diff">
</p>

# sqlite-schema-diff

A schema-first approach to SQLite migrations. Define your schema in `.sql` files, and let the tool figure out what changed.

## Why?

Traditional migrations are error-prone and hard to maintain. Instead:

1. **Define** your desired schema in SQL files
2. **Diff** against your database to see what changed
3. **Apply** changes automatically

No more numbered migration files. No more merge conflicts. Just SQL.

> [!WARNING]
> This tool is under active development. While it has been tested, not every edge case is covered. **Always back up your database before applying changes.** You are responsible for your data.

## Installation

Download the latest binary from [Releases](https://github.com/MizuchiLabs/sqlite-schema-diff/releases)

Or compile from source:

```bash
go install github.com/mizuchilabs/sqlite-schema-diff@latest
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

| Flag                 | Description                               |
| -------------------- | ----------------------------------------- |
| `--dry-run`          | Show what would happen without applying   |
| `--force`            | Skip confirmation for destructive changes |
| `--skip-destructive` | Skip destructive changes (see below)      |
| `--backup=false`     | Disable automatic backup                  |

### `dump` — Export existing schema

```bash
sqlite-schema-diff dump --database app.db --output ./schema
```

## Library Usage

```go
import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "os"

    "github.com/mizuchilabs/sqlite-schema-diff/pkg/diff"
    _ "modernc.org/sqlite"
)

func main() {
    ctx := context.Background()

    // Open your database connection
    db, err := sql.Open("sqlite", "app.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Compare and get changes. Any fs.FS works, use fs.Sub for an embed.FS.
    schema := os.DirFS("./schema")
    changes, err := diff.Compare(ctx, db, schema)
    if err != nil {
        log.Fatal(err)
    }

    // Check what's changing
    for _, c := range changes {
        fmt.Printf("%s: %s (destructive: %v)\n", c.Type, c.Description, c.Destructive)
    }

    // Generate SQL without applying
    sql := diff.GenerateSQL(changes)
    _ = sql

    // Apply exactly the changes you looked at
    err = diff.ApplyChanges(ctx, db, changes, "app.db.backup") // "" = no backup

    // Or compare and apply in one go
    applied, err := diff.Apply(ctx, db, schema, diff.ApplyOptions{
        BackupPath:      "app.db.backup",
        SkipDestructive: true,
    })
    _ = applied
}
```

### Available Functions

| Function                                 | Description                                  |
| ---------------------------------------- | -------------------------------------------- |
| `Compare(ctx, db, fsys)`                 | Diff database against SQL files              |
| `CompareDatabases(ctx, fromDB, toDB)`    | Diff two databases                           |
| `GenerateSQL(changes)`                   | Generate migration SQL                       |
| `HasDestructive(changes)`                | Check for destructive changes                |
| `WithoutDestructive(changes)`            | Drop destructive changes and their followers |
| `ApplyChanges(ctx, db, changes, backup)` | Apply a list of changes to the database      |
| `Apply(ctx, db, fsys, opts)`             | Compare and apply, returns applied changes   |

### Parser Functions

| Function                      | Description                         |
| ----------------------------- | ----------------------------------- |
| `parser.FromDB(ctx, db)`      | Extract schema from open database   |
| `parser.FromSQL(ctx, sql)`    | Parse schema from SQL string        |
| `parser.ReadFiles(ctx, fsys)` | Load schema from .sql files in fsys |

### Upgrading from v0.2

- `Compare`, `Apply` and `parser.ReadFiles` take an `fs.FS` instead of a path. Use `os.DirFS(dir)`, or `fs.Sub(embedFS, "schema")` instead of `SetBaseFS`.
- `Apply` returns the applied changes. `ApplyOptions.DryRun` is gone, use `Compare` for that.

## How apply works

To keep your data safe, `apply` follows a fixed sequence:

1. **Backup** (unless `--backup=false`): creates `app.db.backup` via `VACUUM INTO` before touching anything. The backup gets the file mode of the database.
2. **Single transaction**: all statements run on one connection inside one transaction. The transaction is started with `BEGIN IMMEDIATE`, so a concurrent writer fails fast instead of deadlocking, and the connection waits up to 5 seconds for other writers (busy timeout). An interruption rolls everything back, including on Ctrl-C.
3. **Foreign keys disabled** for the duration of the migration and restored afterwards (only if they were enabled before). `legacy_alter_table` is turned on the same way, so recreating a table does not trip over views and triggers that reference it.
4. **Foreign key check** before commit: if the migrated schema would violate a foreign key, the transaction is rolled back with an error instead of committing broken data.

> [!NOTE]
> `PRAGMA foreign_keys` is a per-connection setting. The tool disables it only on its own migration connection and restores it afterwards. If you rely on foreign key enforcement, enable it through the connection string (`file:app.db?_pragma=foreign_keys(1)`) so every connection, including the tool's, starts with it enabled.

## Supported Objects

- Tables (with columns, constraints, foreign keys)
- Indexes
- Views
- Triggers

> [!WARNING]
> Virtual tables (`CREATE VIRTUAL TABLE`, such as FTS5) support no `ALTER TABLE`. When one changes it is dropped and created again, and its content is not copied. For an FTS index over another table, run its `rebuild` command afterwards. This is flagged as destructive.

## Destructive Changes

Operations that may lose data are flagged as destructive:

| Operation        | Risk                                                         |
| ---------------- | ------------------------------------------------------------ |
| `DROP TABLE`     | Deletes table and all data                                   |
| `RECREATE TABLE` | Required when a table cannot be altered                      |
| `RENAME COLUMN`  | Guessed from a dropped plus an added column, check it is one |

By default, the CLI:

- Creates a backup before applying (`app.db.backup`)
- Prompts for confirmation on destructive changes

Use `--skip-destructive` to safely apply only additive changes. Changes that depend on a skipped one, like the indexes of a table whose recreation is skipped, are skipped too.

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

## FAQ

**Q: What happens when I change a nullable column to NOT NULL?**

A: Existing NULL values are replaced with a type-appropriate empty value during table recreation (for example, empty string for TEXT, 0 for INTEGER).

**Q: Why do quoted table names “stick”?**

A: The diff ignores quote style, so quoting differences never trigger changes. However, SQLite preserves the quoting of the stored schema, so the text you see in `sqlite_master` stays as it was written.

**Q: Why does apply recreate a table instead of just adding a column?**

A: SQLite only permits a restricted form of `ALTER TABLE ADD COLUMN`: no UNIQUE or PRIMARY KEY on the new column, no non-constant defaults such as `CURRENT_TIMESTAMP` on a non-empty table, no generated columns, and no foreign keys on the new column. Applying such a change with `ADD COLUMN` would silently drop those constraints, so the tool recreates the table and copies the data instead. The tool verifies every additive change by replaying it against a scratch copy of the table, so constraint changes that PRAGMAs cannot see (such as a new CHECK constraint) also trigger a recreation. The result always matches your schema files.

**Q: How are column renames detected?**

A: If a table loses exactly one column and gains exactly one with the same type and constraints, and the rest of the table is unchanged, the tool guesses a rename and keeps the data. It cannot tell this apart from "drop A, add B", so the change is marked destructive and needs your confirmation.

**Q: Is apply safe to run more than once?**

A: Yes. Once the database matches your schema files, `apply` reports "No schema changes detected" and does nothing. A single apply is enough to converge - you should never need a second one.

## Examples

See `examples/` directory for working examples.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
