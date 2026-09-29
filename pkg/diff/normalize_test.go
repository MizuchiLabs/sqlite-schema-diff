package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSQL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Basic normalization",
			input: "CREATE   TABLE  foo  ( a INT )",
			want:  "create table foo(a int)",
		},
		{
			name:  "String literal preservation",
			input: "SELECT 'Hello,   World'",
			want:  "select 'Hello,   World'",
		},
		{
			name:  "Escaped quotes in string",
			input: "SELECT 'O''Neil'",
			want:  "select 'O''Neil'",
		},
		{
			name:  "Mixed content",
			input: "CREATE VIEW v AS SELECT 'foo,  bar' AS x, column2 FROM t",
			want:  "create view v as select 'foo,  bar' as x, column2 from t",
		},
		{
			name:  "Strips quotes from identifiers unconditionally",
			input: `CREATE TABLE "MyTable" ([id] INT)`,
			want:  `create table mytable(id int)`,
		},
		{
			name:  "Strips comments",
			input: "CREATE TABLE t (\n a INT, -- don't touch\n b TEXT /* note */\n)",
			want:  "create table t(a int, b text)",
		},
		{
			name:  "Keeps comment markers inside literals and identifiers",
			input: `CREATE TABLE t (a TEXT DEFAULT '--x', "b--c" INT)`,
			want:  "create table t(a text default '--x', b--c int)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeSQL(tt.input))
		})
	}
}
