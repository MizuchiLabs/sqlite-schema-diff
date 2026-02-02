package diff

import (
	"testing"

	"github.com/mizuchilabs/sqlite-schema-diff/pkg/schema"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name            string
		from            *schema.Database
		to              *schema.Database
		wantChangeTypes []ChangeType
		wantDestructive bool
	}{
		{
			name:            "empty to empty",
			from:            &schema.Database{Tables: map[string]*schema.Table{}},
			to:              &schema.Database{Tables: map[string]*schema.Table{}},
			wantChangeTypes: nil,
			wantDestructive: false,
		},
		{
			name: "create table",
			from: &schema.Database{Tables: map[string]*schema.Table{}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"users": {Name: "users", SQL: "CREATE TABLE users (id INTEGER PRIMARY KEY)"},
			}},
			wantChangeTypes: []ChangeType{CreateTable},
			wantDestructive: false,
		},
		{
			name: "drop table",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"users": {Name: "users", SQL: "CREATE TABLE users (id INTEGER PRIMARY KEY)"},
			}},
			to:              &schema.Database{Tables: map[string]*schema.Table{}},
			wantChangeTypes: []ChangeType{DropTable},
			wantDestructive: true,
		},
		{
			name: "add column",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name:    "users",
					SQL:     "CREATE TABLE users (id INTEGER PRIMARY KEY)",
					Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
				},
			}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name: "users",
					SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "name", Type: "TEXT"},
					},
				},
			}},
			wantChangeTypes: []ChangeType{AddColumn},
			wantDestructive: false,
		},
		{
			name: "drop column triggers recreate",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name: "users",
					SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "name", Type: "TEXT"},
					},
				},
			}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name:    "users",
					SQL:     "CREATE TABLE users (id INTEGER PRIMARY KEY)",
					Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
				},
			}},
			wantChangeTypes: []ChangeType{RecreateTable},
			wantDestructive: true,
		},
		{
			name: "modify column type triggers recreate",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name: "users",
					SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "age", Type: "TEXT"},
					},
				},
			}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"users": {
					Name: "users",
					SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "age", Type: "INTEGER"},
					},
				},
			}},
			wantChangeTypes: []ChangeType{RecreateTable},
			wantDestructive: true,
		},
		{
			name: "add column at end uses ADD COLUMN",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"posts": {
					Name: "posts",
					SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "title", Type: "TEXT"},
					},
				},
			}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"posts": {
					Name: "posts",
					SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT, published BOOLEAN)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "title", Type: "TEXT"},
						{Name: "published", Type: "BOOLEAN"},
					},
				},
			}},
			wantChangeTypes: []ChangeType{AddColumn},
			wantDestructive: false,
		},
		{
			name: "add column in middle triggers recreate",
			from: &schema.Database{Tables: map[string]*schema.Table{
				"posts": {
					Name: "posts",
					SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT, published BOOLEAN)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "title", Type: "TEXT"},
						{Name: "published", Type: "BOOLEAN"},
					},
				},
			}},
			to: &schema.Database{Tables: map[string]*schema.Table{
				"posts": {
					Name: "posts",
					SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT, content TEXT, published BOOLEAN)",
					Columns: []schema.Column{
						{Name: "id", Type: "INTEGER", PrimaryKey: 1},
						{Name: "title", Type: "TEXT"},
						{Name: "content", Type: "TEXT"},
						{Name: "published", Type: "BOOLEAN"},
					},
				},
			}},
			wantChangeTypes: []ChangeType{RecreateTable},
			wantDestructive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initMaps(tt.from)
			initMaps(tt.to)

			changes := Diff(tt.from, tt.to)

			if len(changes) != len(tt.wantChangeTypes) {
				t.Errorf("got %d changes, want %d", len(changes), len(tt.wantChangeTypes))
				return
			}

			for i, ct := range tt.wantChangeTypes {
				if changes[i].Type != ct {
					t.Errorf("change[%d].Type = %v, want %v", i, changes[i].Type, ct)
				}
			}

			if got := HasDestructive(changes); got != tt.wantDestructive {
				t.Errorf("HasDestructive() = %v, want %v", got, tt.wantDestructive)
			}
		})
	}
}

func TestDiffIndexes(t *testing.T) {
	tests := []struct {
		name            string
		from            *schema.Database
		to              *schema.Database
		wantChangeTypes []ChangeType
	}{
		{
			name: "create index",
			from: &schema.Database{},
			to: &schema.Database{
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name)",
					},
				},
			},
			wantChangeTypes: []ChangeType{CreateIndex},
		},
		{
			name: "drop index",
			from: &schema.Database{
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name)",
					},
				},
			},
			to:              &schema.Database{},
			wantChangeTypes: []ChangeType{DropIndex},
		},
		{
			name: "modify index drops and recreates",
			from: &schema.Database{
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name)",
					},
				},
			},
			to: &schema.Database{
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name, email)",
					},
				},
			},
			wantChangeTypes: []ChangeType{DropIndex, CreateIndex},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initMaps(tt.from)
			initMaps(tt.to)

			changes := Diff(tt.from, tt.to)

			if len(changes) != len(tt.wantChangeTypes) {
				t.Errorf("got %d changes, want %d", len(changes), len(tt.wantChangeTypes))
				return
			}

			for i, ct := range tt.wantChangeTypes {
				if changes[i].Type != ct {
					t.Errorf("change[%d].Type = %v, want %v", i, changes[i].Type, ct)
				}
			}
		})
	}
}

func TestDiffViews(t *testing.T) {
	tests := []struct {
		name            string
		from            *schema.Database
		to              *schema.Database
		wantChangeTypes []ChangeType
	}{
		{
			name: "create view",
			from: &schema.Database{},
			to: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1",
					},
				},
			},
			wantChangeTypes: []ChangeType{CreateView},
		},
		{
			name: "drop view",
			from: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1",
					},
				},
			},
			to:              &schema.Database{},
			wantChangeTypes: []ChangeType{DropView},
		},
		{
			name: "modify view drops and recreates",
			from: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1",
					},
				},
			},
			to: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT id, name FROM users WHERE active = 1",
					},
				},
			},
			wantChangeTypes: []ChangeType{DropView, CreateView},
		},
		{
			name: "unchanged view",
			from: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT * FROM users",
					},
				},
			},
			to: &schema.Database{
				Views: map[string]*schema.View{
					"active_users": {
						Name: "active_users",
						SQL:  "CREATE VIEW active_users AS SELECT * FROM users",
					},
				},
			},
			wantChangeTypes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initMaps(tt.from)
			initMaps(tt.to)

			changes := Diff(tt.from, tt.to)

			if len(changes) != len(tt.wantChangeTypes) {
				t.Errorf("got %d changes, want %d", len(changes), len(tt.wantChangeTypes))
				return
			}

			for i, ct := range tt.wantChangeTypes {
				if changes[i].Type != ct {
					t.Errorf("change[%d].Type = %v, want %v", i, changes[i].Type, ct)
				}
			}
		})
	}
}

func TestDiffTriggers(t *testing.T) {
	tests := []struct {
		name            string
		from            *schema.Database
		to              *schema.Database
		wantChangeTypes []ChangeType
	}{
		{
			name: "create trigger",
			from: &schema.Database{},
			to: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 1; END",
					},
				},
			},
			wantChangeTypes: []ChangeType{CreateTrigger},
		},
		{
			name: "drop trigger",
			from: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 1; END",
					},
				},
			},
			to:              &schema.Database{},
			wantChangeTypes: []ChangeType{DropTrigger},
		},
		{
			name: "modify trigger drops and recreates",
			from: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 1; END",
					},
				},
			},
			to: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 2; END",
					},
				},
			},
			wantChangeTypes: []ChangeType{DropTrigger, CreateTrigger},
		},
		{
			name: "unchanged trigger",
			from: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 1; END",
					},
				},
			},
			to: &schema.Database{
				Triggers: map[string]*schema.Trigger{
					"trg_users_updated": {
						Name:  "trg_users_updated",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_updated AFTER UPDATE ON users BEGIN SELECT 1; END",
					},
				},
			},
			wantChangeTypes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initMaps(tt.from)
			initMaps(tt.to)

			changes := Diff(tt.from, tt.to)

			if len(changes) != len(tt.wantChangeTypes) {
				t.Errorf("got %d changes, want %d", len(changes), len(tt.wantChangeTypes))
				return
			}

			for i, ct := range tt.wantChangeTypes {
				if changes[i].Type != ct {
					t.Errorf("change[%d].Type = %v, want %v", i, changes[i].Type, ct)
				}
			}
		})
	}
}

func TestRecreatedTableCascades(t *testing.T) {
	// When a table is recreated (e.g., column dropped), indexes and triggers
	// on that table should be recreated too, not dropped explicitly
	tests := []struct {
		name            string
		from            *schema.Database
		to              *schema.Database
		wantChangeTypes []ChangeType
		wantObjects     []string
	}{
		{
			name: "recreate table recreates its index",
			from: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)",
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
							{Name: "email", Type: "TEXT"},
						},
					},
				},
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name)",
					},
				},
			},
			to: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)", // dropped email column
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
						},
					},
				},
				Indexes: map[string]*schema.Index{
					"idx_users_name": {
						Name:  "idx_users_name",
						Table: "users",
						SQL:   "CREATE INDEX idx_users_name ON users(name)",
					},
				},
			},
			wantChangeTypes: []ChangeType{RecreateTable, CreateIndex},
			wantObjects:     []string{"users", "idx_users_name"},
		},
		{
			name: "recreate table recreates its trigger",
			from: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, old_col TEXT)",
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
							{Name: "old_col", Type: "TEXT"},
						},
					},
				},
				Triggers: map[string]*schema.Trigger{
					"trg_users_audit": {
						Name:  "trg_users_audit",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_audit AFTER INSERT ON users BEGIN SELECT 1; END",
					},
				},
			},
			to: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)", // dropped old_col
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
						},
					},
				},
				Triggers: map[string]*schema.Trigger{
					"trg_users_audit": {
						Name:  "trg_users_audit",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_users_audit AFTER INSERT ON users BEGIN SELECT 1; END",
					},
				},
			},
			wantChangeTypes: []ChangeType{RecreateTable, CreateTrigger},
			wantObjects:     []string{"users", "trg_users_audit"},
		},
		{
			name: "recreate table skips explicit trigger drop",
			from: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, old_col TEXT)",
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
							{Name: "old_col", Type: "TEXT"},
						},
					},
				},
				Triggers: map[string]*schema.Trigger{
					"trg_old": {
						Name:  "trg_old",
						Table: "users",
						SQL:   "CREATE TRIGGER trg_old AFTER INSERT ON users BEGIN SELECT 1; END",
					},
				},
			},
			to: &schema.Database{
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						SQL:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "name", Type: "TEXT"},
						},
					},
				},
				Triggers: map[string]*schema.Trigger{}, // trigger removed
			},
			// Should only have RecreateTable, not DropTrigger (trigger dropped implicitly with table)
			wantChangeTypes: []ChangeType{RecreateTable},
			wantObjects:     []string{"users"},
		},
		{
			name: "recreate table with both index and trigger",
			from: &schema.Database{
				Tables: map[string]*schema.Table{
					"posts": {
						Name: "posts",
						SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT, body TEXT)",
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "title", Type: "TEXT"},
							{Name: "body", Type: "TEXT"},
						},
					},
				},
				Indexes: map[string]*schema.Index{
					"idx_posts_title": {
						Name:  "idx_posts_title",
						Table: "posts",
						SQL:   "CREATE INDEX idx_posts_title ON posts(title)",
					},
				},
				Triggers: map[string]*schema.Trigger{
					"trg_posts_ts": {
						Name:  "trg_posts_ts",
						Table: "posts",
						SQL:   "CREATE TRIGGER trg_posts_ts AFTER INSERT ON posts BEGIN SELECT 1; END",
					},
				},
			},
			to: &schema.Database{
				Tables: map[string]*schema.Table{
					"posts": {
						Name: "posts",
						SQL:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT)", // dropped body
						Columns: []schema.Column{
							{Name: "id", Type: "INTEGER", PrimaryKey: 1},
							{Name: "title", Type: "TEXT"},
						},
					},
				},
				Indexes: map[string]*schema.Index{
					"idx_posts_title": {
						Name:  "idx_posts_title",
						Table: "posts",
						SQL:   "CREATE INDEX idx_posts_title ON posts(title)",
					},
				},
				Triggers: map[string]*schema.Trigger{
					"trg_posts_ts": {
						Name:  "trg_posts_ts",
						Table: "posts",
						SQL:   "CREATE TRIGGER trg_posts_ts AFTER INSERT ON posts BEGIN SELECT 1; END",
					},
				},
			},
			wantChangeTypes: []ChangeType{RecreateTable, CreateIndex, CreateTrigger},
			wantObjects:     []string{"posts", "idx_posts_title", "trg_posts_ts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initMaps(tt.from)
			initMaps(tt.to)

			changes := Diff(tt.from, tt.to)

			if len(changes) != len(tt.wantChangeTypes) {
				t.Errorf("got %d changes, want %d", len(changes), len(tt.wantChangeTypes))
				for i, c := range changes {
					t.Logf("  change[%d]: %v %s", i, c.Type, c.Object)
				}
				return
			}

			for i, ct := range tt.wantChangeTypes {
				if changes[i].Type != ct {
					t.Errorf("change[%d].Type = %v, want %v", i, changes[i].Type, ct)
				}
				if changes[i].Object != tt.wantObjects[i] {
					t.Errorf(
						"change[%d].Object = %q, want %q",
						i,
						changes[i].Object,
						tt.wantObjects[i],
					)
				}
			}
		})
	}
}

func TestGenerateAddColumnSQL(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		col       schema.Column
		wantSQL   string
	}{
		{
			name:      "simple column",
			tableName: "users",
			col:       schema.Column{Name: "email", Type: "TEXT"},
			wantSQL:   `ALTER TABLE "users" ADD COLUMN "email" TEXT;`,
		},
		{
			name:      "not null with default",
			tableName: "users",
			col: schema.Column{
				Name:    "active",
				Type:    "INTEGER",
				NotNull: true,
				Default: ptr("1"),
			},
			wantSQL: `ALTER TABLE "users" ADD COLUMN "active" INTEGER NOT NULL DEFAULT 1;`,
		},
		{
			name:      "not null integer without default gets zero",
			tableName: "users",
			col:       schema.Column{Name: "count", Type: "INTEGER", NotNull: true},
			wantSQL:   `ALTER TABLE "users" ADD COLUMN "count" INTEGER DEFAULT 0;`,
		},
		{
			name:      "not null text without default gets empty string",
			tableName: "users",
			col:       schema.Column{Name: "status", Type: "TEXT", NotNull: true},
			wantSQL:   `ALTER TABLE "users" ADD COLUMN "status" TEXT DEFAULT '';`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateAddColumnSQL(tt.tableName, tt.col)
			if got != tt.wantSQL {
				t.Errorf("generateAddColumnSQL() = %q, want %q", got, tt.wantSQL)
			}
		})
	}
}

func TestSortChanges(t *testing.T) {
	changes := []Change{
		{Type: CreateTable, Object: "users"},
		{Type: DropIndex, Object: "idx_a"},
		{Type: DropTrigger, Object: "trg_a"},
		{Type: CreateIndex, Object: "idx_b"},
	}

	sortChanges(changes)

	want := []ChangeType{DropTrigger, DropIndex, CreateTable, CreateIndex}
	for i, ct := range want {
		if changes[i].Type != ct {
			t.Errorf("after sort: changes[%d].Type = %v, want %v", i, changes[i].Type, ct)
		}
	}
}

func TestColumnChanged(t *testing.T) {
	tests := []struct {
		name string
		from schema.Column
		to   schema.Column
		want bool
	}{
		{
			name: "identical columns",
			from: schema.Column{Name: "id", Type: "INTEGER", NotNull: true, PrimaryKey: 1},
			to:   schema.Column{Name: "id", Type: "INTEGER", NotNull: true, PrimaryKey: 1},
			want: false,
		},
		{
			name: "type change",
			from: schema.Column{Name: "age", Type: "INTEGER"},
			to:   schema.Column{Name: "age", Type: "TEXT"},
			want: true,
		},
		{
			name: "type case insensitive",
			from: schema.Column{Name: "age", Type: "INTEGER"},
			to:   schema.Column{Name: "age", Type: "integer"},
			want: false,
		},
		{
			name: "not null change",
			from: schema.Column{Name: "email", Type: "TEXT", NotNull: true},
			to:   schema.Column{Name: "email", Type: "TEXT", NotNull: false},
			want: true,
		},
		{
			name: "pk change",
			from: schema.Column{Name: "id", Type: "INTEGER", PrimaryKey: 0},
			to:   schema.Column{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			want: true,
		},
		{
			name: "default added",
			from: schema.Column{Name: "flag", Type: "INTEGER"},
			to:   schema.Column{Name: "flag", Type: "INTEGER", Default: ptr("0")},
			want: true,
		},
		{
			name: "default removed",
			from: schema.Column{Name: "flag", Type: "INTEGER", Default: ptr("0")},
			to:   schema.Column{Name: "flag", Type: "INTEGER"},
			want: true,
		},
		{
			name: "default changed",
			from: schema.Column{Name: "flag", Type: "INTEGER", Default: ptr("0")},
			to:   schema.Column{Name: "flag", Type: "INTEGER", Default: ptr("1")},
			want: true,
		},
		{
			name: "default whitespace normalized",
			from: schema.Column{Name: "flag", Type: "TEXT", Default: ptr("'foo'")},
			to:   schema.Column{Name: "flag", Type: "TEXT", Default: ptr(" 'foo' ")},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := columnChanged(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("columnChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultForType(t *testing.T) {
	tests := []struct {
		colType string
		want    string
	}{
		{"INTEGER", "0"},
		{"INT", "0"},
		{"BIGINT", "0"},
		{"SMALLINT", "0"},
		{"TINYINT", "0"},
		{"REAL", "0.0"},
		{"FLOAT", "0.0"},
		{"DOUBLE", "0.0"},
		{"BLOB", "X''"},
		{"TEXT", "''"},
		{"VARCHAR", "''"},
		{
			"BOOLEAN",
			"''",
		}, // SQLite doesn't have native BOOLEAN, usually maps to NUMERIC/INTEGER, but strict string matching returns ''
		{"UNKNOWN", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.colType, func(t *testing.T) {
			got := defaultForType(tt.colType)
			if got != tt.want {
				t.Errorf("defaultForType(%q) = %q, want %q", tt.colType, got, tt.want)
			}
		})
	}
}

// helpers

func initMaps(db *schema.Database) {
	if db.Tables == nil {
		db.Tables = make(map[string]*schema.Table)
	}
	if db.Indexes == nil {
		db.Indexes = make(map[string]*schema.Index)
	}
	if db.Views == nil {
		db.Views = make(map[string]*schema.View)
	}
	if db.Triggers == nil {
		db.Triggers = make(map[string]*schema.Trigger)
	}
}

func ptr(s string) *string {
	return &s
}
