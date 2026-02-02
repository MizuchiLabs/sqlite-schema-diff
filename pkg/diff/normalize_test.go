package diff

import (
	"testing"
)

func TestNormalizeSQL(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		stripQuotes bool
		want        string
	}{
		{
			name:        "Basic normalization",
			input:       "CREATE   TABLE  foo  ( a INT )",
			stripQuotes: true,
			want:        "create table foo(a int)",
		},
		{
			name:        "String literal preservation",
			input:       "SELECT 'Hello,   World'",
			stripQuotes: false,
			want:        "select 'Hello,   World'",
		},
		{
			name:        "Escaped quotes in string",
			input:       "SELECT 'O''Neil'",
			stripQuotes: false,
			want:        "select 'O''Neil'",
		},
		{
			name:        "Mixed content",
			input:       "CREATE VIEW v AS SELECT 'foo,  bar' AS x, column2 FROM t",
			stripQuotes: false,
			want:        "create view v as select 'foo,  bar' as x, column2 from t",
		},
		{
			name:        "Strip quotes from identifiers",
			input:       `CREATE TABLE "MyTable" ([id] INT)`,
			stripQuotes: true,
			want:        "create table mytable(id int)",
		},
		{
			name:        "Don't strip quotes from identifiers if false",
			input:       `CREATE TABLE "MyTable"`,
			stripQuotes: false,
			want:        `create table "mytable"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSQL(tt.input, tt.stripQuotes)
			if got != tt.want {
				t.Errorf("normalizeSQL() = %q, want %q", got, tt.want)
			}
		})
	}
}
