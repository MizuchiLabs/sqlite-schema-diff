package diff

import (
	"fmt"
	"regexp"
	"strings"
)

// stringLiteralRe matches SQLite string literals, including escaped quotes (e.g. 'O”Neil')
var stringLiteralRe = regexp.MustCompile(`'((?:[^']|'')*)'`)

func normalizeSQL(sql string) string {
	// Mask string literals to protect them from normalization
	var literals []string
	maskedSQL := stringLiteralRe.ReplaceAllStringFunc(sql, func(match string) string {
		literals = append(literals, match)
		// Use a unique placeholder that will survive tokenization and lowercasing
		return fmt.Sprintf(" __str_protect_%d__ ", len(literals)-1)
	})

	normalized := performNormalization(maskedSQL)

	// Unmask string literals, the normalization process lowercases the placeholder, so we match that
	for i, lit := range literals {
		placeholder := fmt.Sprintf("__str_protect_%d__", i)
		normalized = strings.Replace(normalized, placeholder, lit, 1)
	}

	return normalized
}

func performNormalization(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")

	// Remove quotes around identifiers (SQLite accepts both quoted and unquoted)
	// We MUST do this because SQLite's ALTER TABLE RENAME TO forces double quotes
	// around the new table name in sqlite_master, meaning unquoted schema tables
	// would infinitely recreate if we did not treat them as identical.
	sql = strings.ReplaceAll(sql, "\"", "")
	sql = strings.ReplaceAll(sql, "`", "")
	sql = strings.ReplaceAll(sql, "[", "")
	sql = strings.ReplaceAll(sql, "]", "")

	// Collapse all whitespace to single spaces and lowercase everything
	sql = strings.ToLower(strings.Join(strings.Fields(sql), " "))

	// Normalize spacing around punctuation
	for _, ch := range []string{"(", ")", ",", "="} {
		sql = strings.ReplaceAll(sql, " "+ch, ch)
		sql = strings.ReplaceAll(sql, ch+" ", ch)
	}

	// Add space after comma for consistency
	sql = strings.ReplaceAll(sql, ",", ", ")

	// Collapse any double spaces created
	for strings.Contains(sql, "  ") {
		sql = strings.ReplaceAll(sql, "  ", " ")
	}

	return strings.TrimSpace(sql)
}
