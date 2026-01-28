# Usage Guide

Comprehensive guide for using sqlite-schema-diff.

## Table of Contents

- [Installation](#installation)
- [CLI Usage](#cli-usage)
- [Library Usage](#library-usage)
- [Schema File Format](#schema-file-format)
- [Migration Workflow](#migration-workflow)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Installation

### From Source

```bash
git clone https://github.com/yourusername/sqlite-schema-diff
cd sqlite-schema-diff
make install
```

### As a Go Module

```bash
go get sqlite-schema-diff
```

## CLI Usage

### Basic Commands

#### 1. Show Differences

```bash
# Basic diff
sqlite-schema-diff diff --database app.db --schema ./schema

# Output as SQL
sqlite-schema-diff diff --database app.db --schema ./schema --sql

# Save SQL to file
sqlite-schema-diff diff --database app.db --schema ./schema --sql > migration.sql
```

#### 2. Apply Changes

```bash
# Interactive (prompts for destructive changes)
sqlite-schema-diff apply --database app.db --schema ./schema

# Force (no prompts)
sqlite-schema-diff apply --database app.db --schema ./schema --force

# Dry run (show what would happen)
sqlite-schema-diff apply --database app.db --schema ./schema --dry-run

# Skip destructive operations
sqlite-schema-diff apply --database app.db --schema ./schema --skip-destructive

# Without backup
sqlite-schema-diff apply --database app.db --schema ./schema --backup=false
```

#### 3. Dump Schema

```bash
# Export current database schema to files
sqlite-schema-diff dump --database app.db --output ./schema
```

### CI/CD Integration

```bash
#!/bin/bash
# Check for schema changes in CI

sqlite-schema-diff diff --database app.db --schema ./schema

if [ $? -eq 0 ]; then
  changes=$(sqlite-schema-diff diff --database app.db --schema ./schema 2>&1)
  if [[ $changes == *"No schema changes"* ]]; then
    echo "Schema is up to date"
    exit 0
  else
    echo "Schema changes detected:"
    echo "$changes"
    exit 1
  fi
fi
```

## Library Usage

### Basic Example

```go
package main

import (
    "fmt"
    "log"

    "sqlite-schema-diff/pkg/diff"
)

func main() {
    // Compare and get changes
    changes, err := diff.CompareSchemas("app.db", "./schema")
    if err != nil {
        log.Fatal(err)
    }

    if len(changes) == 0 {
        fmt.Println("Schema is up to date")
        return
    }

    // Apply changes
    opts := diff.ApplyOptions{
        BackupBeforeApply: true,
    }

    if err := diff.Apply("app.db", changes, opts); err != nil {
        log.Fatal(err)
    }
}
```

### Advanced Example

```go
package main

import (
    "fmt"
    "log"

    "sqlite-schema-diff/pkg/diff"
    "sqlite-schema-diff/pkg/parser"
)

func main() {
    // Load schemas manually
    currentSchema, err := parser.ExtractFromDatabase("app.db")
    if err != nil {
        log.Fatal(err)
    }

    targetSchema, err := parser.LoadSchemaFromDirectory("./schema")
    if err != nil {
        log.Fatal(err)
    }

    // Compute diff
    changes, err := diff.Diff(currentSchema, targetSchema)
    if err != nil {
        log.Fatal(err)
    }

    // Analyze changes
    hasDestructive := false
    for _, change := range changes {
        if change.Destructive {
            hasDestructive = true
            fmt.Printf("WARNING: %s - %s\n", change.Type, change.Description)
        }
    }

    if hasDestructive {
        fmt.Println("Aborting due to destructive changes")
        return
    }

    // Generate SQL
    sql := diff.GenerateSQL(changes)
    fmt.Println(sql)
}
```

### Integration with HTTP Server

```go
package main

import (
    "encoding/json"
    "net/http"

    "sqlite-schema-diff/pkg/diff"
)

type DiffResponse struct {
    Changes      []diff.Change `json:"changes"`
    HasDestructive bool        `json:"has_destructive"`
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
    dbPath := r.URL.Query().Get("db")
    schemaDir := r.URL.Query().Get("schema")

    changes, err := diff.CompareSchemas(dbPath, schemaDir)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    resp := DiffResponse{
        Changes:        changes,
        HasDestructive: diff.HasDestructiveChanges(changes),
    }

    json.NewEncoder(w).Encode(resp)
}
```

## Schema File Format

### File Organization

Schema files should be `.sql` files containing standard SQLite DDL statements.

**Recommended structure:**

```
schema/
├── 01_users.sql        # Tables
├── 02_posts.sql
├── 03_comments.sql
├── indexes.sql         # Indexes
├── views.sql          # Views
└── triggers.sql       # Triggers
```

**Alternative structure:**

```
schema/
├── tables/
│   ├── users.sql
│   ├── posts.sql
│   └── comments.sql
├── indexes/
│   └── all.sql
└── triggers/
    └── audit.sql
```

### Supported SQL

#### Tables

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    bio TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT email_format CHECK (email LIKE '%@%')
);
```

#### Indexes

```sql
-- Basic index
CREATE INDEX idx_users_email ON users(email);

-- Unique index
CREATE UNIQUE INDEX idx_users_username ON users(username);

-- Partial index
CREATE INDEX idx_active_users ON users(created_at)
WHERE deleted_at IS NULL;

-- Multi-column index
CREATE INDEX idx_posts_user_date ON posts(user_id, created_at DESC);
```

#### Views

```sql
CREATE VIEW active_users AS
SELECT id, email, username
FROM users
WHERE deleted_at IS NULL;
```

#### Triggers

```sql
CREATE TRIGGER update_timestamp
AFTER UPDATE ON users
BEGIN
    UPDATE users
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
```

#### Foreign Keys

```sql
CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    title TEXT NOT NULL,

    FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        ON UPDATE CASCADE
);
```

### File Format Notes

- Files can contain multiple statements
- Statements should end with semicolons
- SQL comments (`--` and `/* */`) are supported
- File order doesn't matter (tables sorted alphabetically)
- Each object (table, index, etc.) must have a unique name

## Migration Workflow

### Initial Setup

1. **Dump existing database schema:**

```bash
sqlite-schema-diff dump --database prod.db --output ./schema
```

2. **Commit schema files:**

```bash
git add schema/
git commit -m "Initial schema"
```

### Making Changes

1. **Edit schema files** to reflect desired state:

```sql
-- schema/users.sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    full_name TEXT,  -- NEW COLUMN
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

2. **Preview changes:**

```bash
sqlite-schema-diff diff --database dev.db --schema ./schema
```

3. **Apply to development database:**

```bash
sqlite-schema-diff apply --database dev.db --schema ./schema
```

4. **Test changes**, then commit:

```bash
git add schema/
git commit -m "Add full_name column to users"
```

### Deploying to Production

1. **Generate migration SQL:**

```bash
sqlite-schema-diff diff \
    --database prod.db \
    --schema ./schema \
    --sql > migration-$(date +%Y%m%d).sql
```

2. **Review migration:**

```bash
cat migration-20260127.sql
```

3. **Apply to production:**

```bash
# With backup
sqlite-schema-diff apply \
    --database prod.db \
    --schema ./schema \
    --backup

# Or apply SQL file manually
sqlite3 prod.db < migration-20260127.sql
```

## Best Practices

### 1. Version Control

✅ **DO:**

- Commit schema files to git
- Review schema changes in PRs
- Use meaningful commit messages

❌ **DON'T:**

- Commit database files
- Make schema changes without review
- Mix schema and data changes

### 2. Naming Conventions

```sql
-- Tables: singular nouns
CREATE TABLE user (...);
CREATE TABLE post (...);

-- Indexes: idx_{table}_{columns}
CREATE INDEX idx_users_email ON users(email);

-- Foreign key indexes: idx_{table}_{ref_table}
CREATE INDEX idx_posts_user ON posts(user_id);

-- Triggers: {timing}_{action}_{table}
CREATE TRIGGER after_insert_users ...

-- Views: descriptive names
CREATE VIEW active_users ...
```

### 3. Destructive Changes

When making destructive changes:

1. **Backup first:**

   ```bash
   cp prod.db prod.db.backup
   ```

2. **Test on copy:**

   ```bash
   sqlite-schema-diff apply --database prod.db.copy --schema ./schema
   ```

3. **Consider data migration:**
   - For column renames, use separate SQL script
   - For table renames, use ALTER TABLE RENAME
   - For data transformations, write custom migration

### 4. Large Schemas

For large schemas, organize by domain:

```
schema/
├── auth/
│   ├── users.sql
│   └── sessions.sql
├── content/
│   ├── posts.sql
│   └── comments.sql
└── analytics/
    └── events.sql
```

### 5. Testing

```go
func TestSchemaUpToDate(t *testing.T) {
    changes, err := diff.CompareSchemas("test.db", "./schema")
    if err != nil {
        t.Fatal(err)
    }

    if len(changes) > 0 {
        t.Errorf("Schema out of date: %d changes", len(changes))
    }
}
```

## Troubleshooting

### Issue: "Duplicate table/index found"

**Cause:** Same object defined in multiple files.

**Solution:** Ensure each object has a unique name across all schema files.

### Issue: Foreign key constraint fails

**Cause:** Referenced table doesn't exist or columns mismatch.

**Solution:**

- Ensure foreign key tables are defined
- Check column types match
- Use PRAGMA foreign_keys = OFF during migration (done automatically)

### Issue: Data loss on table recreation

**Cause:** SQLite limitations require recreating tables for some changes.

**Solution:**

- Review changes with `--dry-run`
- Create backup before applying
- For critical changes, write custom migration script

### Issue: "Invalid CREATE TABLE syntax"

**Cause:** Parser doesn't handle complex SQL.

**Solution:**

- Simplify SQL syntax
- Ensure proper formatting
- Check for SQLite-specific syntax

### Getting Help

1. Enable debug logging:

   ```bash
   sqlite-schema-diff --debug diff --database app.db --schema ./schema
   ```

2. Generate migration SQL to inspect:

   ```bash
   sqlite-schema-diff diff --database app.db --schema ./schema --sql
   ```

3. Test with dry-run:
   ```bash
   sqlite-schema-diff apply --database app.db --schema ./schema --dry-run
   ```

### Common Patterns

#### Adding a column

```sql
-- Simply add to table definition
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL,
    phone TEXT  -- new column
);
```

The tool will recreate the table and preserve data.

#### Renaming a column

SQLite doesn't detect renames, so this is treated as drop + add:

```sql
-- To preserve data, use ALTER TABLE manually first:
-- ALTER TABLE users RENAME COLUMN old_name TO new_name;

-- Then update schema file
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    new_name TEXT
);
```

#### Changing column type

```sql
-- Update schema - will require table recreation
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    age INTEGER  -- changed from TEXT to INTEGER
);
```

**Warning:** Data conversion may fail or lose precision.
