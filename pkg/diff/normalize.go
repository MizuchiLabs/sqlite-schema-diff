package diff

import (
	"fmt"
	"strings"
)

func normalizeSQL(sql string) string {
	masked, literals := maskLiterals(sql)
	normalized := performNormalization(masked)

	for i, lit := range literals {
		placeholder := fmt.Sprintf("__str_protect_%d__", i)
		normalized = strings.Replace(normalized, placeholder, lit, 1)
	}

	return normalized
}

// maskLiterals replaces string literals with placeholders so normalization
// leaves them alone, and drops comments, which SQLite keeps in the stored
// schema but which carry no meaning.
func maskLiterals(sql string) (string, []string) {
	var out strings.Builder
	var literals []string

	for i := 0; i < len(sql); {
		rest := sql[i:]
		switch {
		case sql[i] == '\'':
			end := i + quotedLen(rest)
			literals = append(literals, sql[i:end])
			fmt.Fprintf(&out, " __str_protect_%d__ ", len(literals)-1)
			i = end
		case sql[i] == '"' || sql[i] == '`':
			end := i + quotedLen(rest)
			out.WriteString(sql[i:end])
			i = end
		case sql[i] == '[':
			end := len(sql)
			if j := strings.IndexByte(rest, ']'); j != -1 {
				end = i + j + 1
			}
			out.WriteString(sql[i:end])
			i = end
		case strings.HasPrefix(rest, "--"):
			j := strings.IndexByte(rest, '\n')
			if j == -1 {
				j = len(rest)
			}
			out.WriteByte(' ')
			i += j
		case strings.HasPrefix(rest, "/*"):
			j := strings.Index(rest[2:], "*/")
			if j == -1 {
				j = len(rest)
			} else {
				j += 4
			}
			out.WriteByte(' ')
			i += j
		default:
			out.WriteByte(sql[i])
			i++
		}
	}

	return out.String(), literals
}

// quotedLen returns the length of the quoted region at the start of s.
// SQLite escapes quotes inside a region by doubling them.
func quotedLen(s string) int {
	quote := s[0]
	for j := 1; j < len(s); j++ {
		if s[j] != quote {
			continue
		}
		if j+1 < len(s) && s[j+1] == quote {
			j++
			continue
		}
		return j + 1
	}
	return len(s)
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
