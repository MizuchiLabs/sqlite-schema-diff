package diff

import (
	"slices"
	"strings"
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
			wantChangeTypes: []ChangeType{DropTrigger, RecreateTable, CreateTrigger},
			wantObjects:     []string{"trg_users_audit", "users", "trg_users_audit"},
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
			// Explicitly drops trigger to prevent errors during recreation
			wantChangeTypes: []ChangeType{DropTrigger, RecreateTable},
			wantObjects:     []string{"trg_old", "users"},
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
			wantChangeTypes: []ChangeType{DropTrigger, RecreateTable, CreateIndex, CreateTrigger},
			wantObjects:     []string{"trg_posts_ts", "posts", "idx_posts_title", "trg_posts_ts"},
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
				Default: new("1"),
			},
			wantSQL: `ALTER TABLE "users" ADD COLUMN "active" INTEGER NOT NULL DEFAULT 1;`,
		},
		{
			name:      "not null integer without default gets zero",
			tableName: "users",
			col:       schema.Column{Name: "count", Type: "INTEGER", NotNull: true},
			wantSQL:   `ALTER TABLE "users" ADD COLUMN "count" INTEGER NOT NULL DEFAULT 0;`,
		},
		{
			name:      "not null text without default gets empty string",
			tableName: "users",
			col:       schema.Column{Name: "status", Type: "TEXT", NotNull: true},
			wantSQL:   `ALTER TABLE "users" ADD COLUMN "status" TEXT NOT NULL DEFAULT '';`,
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
			to:   schema.Column{Name: "flag", Type: "INTEGER", Default: new("0")},
			want: true,
		},
		{
			name: "default removed",
			from: schema.Column{Name: "flag", Type: "INTEGER", Default: new("0")},
			to:   schema.Column{Name: "flag", Type: "INTEGER"},
			want: true,
		},
		{
			name: "default changed",
			from: schema.Column{Name: "flag", Type: "INTEGER", Default: new("0")},
			to:   schema.Column{Name: "flag", Type: "INTEGER", Default: new("1")},
			want: true,
		},
		{
			name: "default whitespace normalized",
			from: schema.Column{Name: "flag", Type: "TEXT", Default: new("'foo'")},
			to:   schema.Column{Name: "flag", Type: "TEXT", Default: new(" 'foo' ")},
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

func TestReplaceTableName(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		newName string
		want    string
	}{
		{
			name:    "simple table",
			sql:     "CREATE TABLE users (id int)",
			newName: "users__new",
			want:    "CREATE TABLE \"users__new\" (id int)",
		},
		{
			name:    "if not exists",
			sql:     "CREATE TABLE IF NOT EXISTS users (id int)",
			newName: "users__new",
			want:    "CREATE TABLE IF NOT EXISTS \"users__new\" (id int)",
		},
		{
			name:    "double quotes",
			sql:     "CREATE TABLE \"users - old\" (id int)",
			newName: "users__new",
			want:    "CREATE TABLE \"users__new\" (id int)",
		},
		{
			name:    "single quotes",
			sql:     "CREATE TABLE 'users' (id int)",
			newName: "users__new",
			want:    "CREATE TABLE \"users__new\" (id int)",
		},
		{
			name:    "backticks",
			sql:     "CREATE TABLE `users` (id int)",
			newName: "users__new",
			want:    "CREATE TABLE \"users__new\" (id int)",
		},
		{
			name:    "brackets",
			sql:     "CREATE TABLE [users] (id int)",
			newName: "users__new",
			want:    "CREATE TABLE \"users__new\" (id int)",
		},
		{
			name:    "multiline",
			sql:     "CREATE \n TABLE \n users (\n id int)",
			newName: "users__new",
			want:    "CREATE \n TABLE \n \"users__new\" (\n id int)",
		},
		{
			name:    "extra spaces",
			sql:     "CREATE TABLE    users (id int)",
			newName: "users__new",
			want:    "CREATE TABLE    \"users__new\" (id int)", // The regex currently collapses the spaces or keeps them depending on capture group 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceTableName(tt.sql, tt.newName)
			if got != tt.want {
				t.Errorf("replaceTableName(%q, %q) = %q, want %q", tt.sql, tt.newName, got, tt.want)
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

func TestCanAddColumns(t *testing.T) {
	from := &schema.Table{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
		SQL:     `CREATE TABLE users (id INTEGER PRIMARY KEY)`,
	}

	tests := []struct {
		name    string
		col     schema.Column
		after   schema.Column
		inFrom  bool
		unique  []string
		fks     []string
		toSQL   string // overrides the generated target definition
		want    bool
		wantSQL string
	}{
		{
			name:    "nullable column can be added",
			col:     schema.Column{Name: "bio", Type: "TEXT"},
			want:    true,
			wantSQL: `ALTER TABLE "users" ADD COLUMN "bio" TEXT;`,
		},
		{
			name: "not null with constant default can be added",
			col: schema.Column{
				Name:    "status",
				Type:    "TEXT",
				NotNull: true,
				Default: new("'active'"),
			},
			want:    true,
			wantSQL: `ALTER TABLE "users" ADD COLUMN "status" TEXT NOT NULL DEFAULT 'active';`,
			toSQL:   `CREATE TABLE users (id INTEGER PRIMARY KEY, status TEXT NOT NULL DEFAULT 'active')`,
		},
		{
			name: "not null without default is recreated",
			col:  schema.Column{Name: "email", Type: "TEXT", NotNull: true},
			want: false,
		},
		{
			name: "non-constant default is recreated",
			col: schema.Column{
				Name:    "created_at",
				Type:    "DATETIME",
				Default: new("CURRENT_TIMESTAMP"),
			},
			want: false,
		},
		{
			name: "parenthesized default is recreated",
			col:  schema.Column{Name: "score", Type: "INTEGER", Default: new("(1+1)")},
			want: false,
		},
		{
			name:   "unique constraint column is recreated",
			col:    schema.Column{Name: "email", Type: "TEXT"},
			unique: []string{"email"},
			want:   false,
		},
		{
			name: "primary key column is recreated",
			col:  schema.Column{Name: "code", Type: "TEXT", PrimaryKey: 1},
			want: false,
		},
		{
			name: "foreign key column is recreated",
			col:  schema.Column{Name: "user_id", Type: "INTEGER"},
			fks:  []string{"user_id"},
			want: false,
		},
		{
			name: "generated column is recreated",
			col:  schema.Column{Name: "label", Type: "TEXT", Hidden: 3},
			want: false,
		},
		{
			name:   "new column in the middle is recreated",
			col:    schema.Column{Name: "zzz", Type: "TEXT"},
			after:  schema.Column{Name: "old", Type: "TEXT"},
			inFrom: true,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fromCols := []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}}
			toCols := []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, tt.col}
			fromSQL := `CREATE TABLE users (id INTEGER PRIMARY KEY)`
			toSQL := `CREATE TABLE users (id INTEGER PRIMARY KEY, ` +
				tt.col.Name + ` ` + tt.col.Type + `)`
			if tt.inFrom {
				toCols = append(toCols, tt.after)
				fromCols = append(fromCols, tt.after)
				fromSQL = `CREATE TABLE users (id INTEGER PRIMARY KEY, ` +
					tt.after.Name + ` ` + tt.after.Type + `)`
				toSQL = `CREATE TABLE users (id INTEGER PRIMARY KEY, ` +
					tt.col.Name + ` ` + tt.col.Type + `, ` +
					tt.after.Name + ` ` + tt.after.Type + `)`
			}

			to := &schema.Table{
				Name:              "users",
				Columns:           toCols,
				SQL:               toSQL,
				UniqueColumns:     tt.unique,
				ForeignKeyColumns: tt.fks,
			}
			if tt.toSQL != "" {
				to.SQL = tt.toSQL
			}
			from.Columns = fromCols
			from.SQL = fromSQL

			got := canAddColumns(from, to, []schema.Column{tt.col})
			if got != tt.want {
				t.Fatalf("canAddColumns() = %v, want %v", got, tt.want)
			}

			if got && tt.wantSQL != "" {
				added := diffTableColumns(from, to)
				if len(added) != 1 || added[0].Type != AddColumn {
					t.Fatalf("expected single AddColumn change, got %+v", added)
				}
				if added[0].SQL[0] != tt.wantSQL {
					t.Errorf("SQL = %q, want %q", added[0].SQL[0], tt.wantSQL)
				}
			}
		})
	}
}

func TestGenerateRecreateSQL_FillsDefaultsForNewColumns(t *testing.T) {
	from := &schema.Table{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
		SQL:     `CREATE TABLE users (id INTEGER PRIMARY KEY)`,
	}
	to := &schema.Table{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "email", Type: "TEXT", NotNull: true},
			{Name: "created_at", Type: "DATETIME", Default: new("CURRENT_TIMESTAMP")},
			{Name: "bio", Type: "TEXT"},
		},
		SQL: `CREATE TABLE "users" (id INTEGER PRIMARY KEY, email TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, bio TEXT)`,
	}

	stmts := generateRecreateSQL("users", from, to)

	insert := stmts[1]
	if !strings.Contains(insert, `"email"`) || !strings.Contains(insert, "''") {
		t.Errorf("expected default fill for NOT NULL new column, got %q", insert)
	}
	if !strings.Contains(insert, "CURRENT_TIMESTAMP") {
		t.Errorf("expected default fill for new column with default, got %q", insert)
	}
	if strings.Contains(insert, `"bio"`) {
		t.Errorf("nullable new column without default should be omitted, got %q", insert)
	}
}

// TestDiff_TableConstraintChangeRecreates pins the simulation behavior:
// adding a column to a table whose target definition also gained a
// table-level constraint (invisible to PRAGMAs) must recreate the table
// instead of silently dropping the constraint via ADD COLUMN.
func TestDiff_TableConstraintChangeRecreates(t *testing.T) {
	from := &schema.Table{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
		SQL:     `CREATE TABLE users (id INTEGER PRIMARY KEY)`,
	}
	to := &schema.Table{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
		},
		SQL: `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, CHECK (length(name) > 0))`,
	}

	changes := diffTableColumns(from, to)
	if len(changes) != 1 || changes[0].Type != RecreateTable {
		t.Fatalf("expected single RecreateTable, got %+v", changes)
	}
}

// TestDiff_ExistingConstraintKeepsAddColumn pins SQLite's ALTER TABLE
// behavior: the added column is placed before table-level constraints in the
// stored definition, so adding a column to a table that already has a CHECK
// still converges via ADD COLUMN in a single apply.
func TestDiff_ExistingConstraintKeepsAddColumn(t *testing.T) {
	from := &schema.Table{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "INTEGER", PrimaryKey: 1}},
		SQL:     `CREATE TABLE users (id INTEGER PRIMARY KEY, CHECK (id > 0))`,
	}
	to := &schema.Table{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT"},
		},
		SQL: `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, CHECK (id > 0))`,
	}

	changes := diffTableColumns(from, to)
	if len(changes) != 1 || changes[0].Type != AddColumn {
		t.Fatalf("expected single AddColumn, got %+v", changes)
	}
}

// TestDiff_CaseOnlyRename pins the rename-not-drop behavior: SQLite table
// names are case-insensitive, so changing the case in the schema must
// produce a RENAME_TABLE instead of a destructive drop + create.
func TestDiff_CaseOnlyRename(t *testing.T) {
	from := &schema.Database{
		Tables: map[string]*schema.Table{
			"users": {
				Name: "users",
				SQL:  `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`,
				Columns: []schema.Column{
					{Name: "id", Type: "INTEGER", PrimaryKey: 1},
					{Name: "name", Type: "TEXT"},
				},
			},
		},
	}
	initMaps(from)
	to := &schema.Database{
		Tables: map[string]*schema.Table{
			"USERS": {
				Name: "USERS",
				SQL:  `CREATE TABLE USERS (id INTEGER PRIMARY KEY, name TEXT)`,
				Columns: []schema.Column{
					{Name: "id", Type: "INTEGER", PrimaryKey: 1},
					{Name: "name", Type: "TEXT"},
				},
			},
		},
	}
	initMaps(to)

	changes := Diff(from, to)
	if len(changes) != 1 || changes[0].Type != RenameTable {
		t.Fatalf("expected single RenameTable, got %+v", changes)
	}
	if changes[0].Destructive {
		t.Error("rename must not be flagged destructive")
	}
	// SQLite resolves table names case-insensitively, so the rename goes
	// through a temporary table: create, copy, drop old, rename.
	wantSQL := []string{
		`CREATE TABLE "users__new" (id INTEGER PRIMARY KEY, name TEXT);`,
		`INSERT INTO "users__new" ("id", "name") SELECT "id", "name" FROM "users";`,
		`DROP TABLE "users";`,
		`ALTER TABLE "users__new" RENAME TO "USERS";`,
	}
	if !slices.Equal(changes[0].SQL, wantSQL) {
		t.Errorf("SQL = %q, want %q", changes[0].SQL, wantSQL)
	}
}
