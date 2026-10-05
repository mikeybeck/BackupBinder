// Package sqlcheck decides whether a dump can be imported, without a MySQL server.
package sqlcheck

import (
	"regexp"
	"strings"
	"unicode"
)

// Result is an in-memory import of the dump.
type Result struct {
	Importable bool     `json:"importable"`
	Mode       string   `json:"mode"`
	Tables     []string `json:"tables"`
	Statements int      `json:"statements"`
	Inserts    int      `json:"inserts"`
	Creates    int      `json:"creates"`
	Reason     string   `json:"reason,omitempty"`
}

var (
	createTableRe = regexp.MustCompile(`(?i)^CREATE TABLE\s+(?:IF NOT EXISTS\s+)?[` + "`" + `"]?([A-Za-z0-9_]+)`)
	insertRe      = regexp.MustCompile(`(?i)^(?:INSERT|REPLACE) INTO\s+[` + "`" + `"]?([A-Za-z0-9_]+)`)
	dropRe        = regexp.MustCompile(`(?i)^DROP TABLE\s+(?:IF EXISTS\s+)?[` + "`" + `"]?([A-Za-z0-9_]+)`)
)

// Split breaks SQL on semicolons that are outside quotes and comments.
func Split(text string) []string {
	var statements []string
	var buf []byte
	n := len(text)
	inSingle, inDouble, inBack := false, false, false
	for i := 0; i < n; {
		c := text[i]
		next := byte(0)
		if i+1 < n {
			next = text[i+1]
		}
		if !inSingle && !inDouble && !inBack {
			if c == '-' && next == '-' && sqlCommentDash(text, i) {
				for i < n && text[i] != '\n' {
					i++
				}
				continue
			}
			if c == '#' {
				for i < n && text[i] != '\n' {
					i++
				}
				continue
			}
			if c == '/' && next == '*' {
				end := strings.Index(text[i+2:], "*/")
				if end < 0 {
					i = n
				} else {
					i = i + 2 + end + 2
				}
				continue
			}
			switch c {
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '`':
				inBack = true
			case ';':
				if stmt := strings.TrimSpace(string(buf)); stmt != "" {
					statements = append(statements, stmt)
				}
				buf = buf[:0]
				i++
				continue
			}
			buf = append(buf, c)
			i++
			continue
		}
		buf = append(buf, c)
		if c == '\\' && (inSingle || inDouble) && i+1 < n {
			buf = append(buf, text[i+1])
			i += 2
			continue
		}
		if inSingle && c == '\'' {
			if next == '\'' {
				buf = append(buf, next)
				i += 2
				continue
			}
			inSingle = false
		} else if inDouble && c == '"' {
			if next == '"' {
				buf = append(buf, next)
				i += 2
				continue
			}
			inDouble = false
		} else if inBack && c == '`' {
			if next == '`' {
				buf = append(buf, next)
				i += 2
				continue
			}
			inBack = false
		}
		i++
	}
	if tail := strings.TrimSpace(string(buf)); tail != "" {
		statements = append(statements, tail)
	}
	return statements
}

// sqlCommentDash reports a MySQL line comment. The second dash must be
// followed by whitespace or a control character, including a newline.
func sqlCommentDash(text string, i int) bool {
	if i+2 >= len(text) {
		return true
	}
	next := text[i+2]
	return next == ' ' || next == '\t' || next < ' '
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Assess imports the dump into an in-memory set of tables.
func Assess(text string) Result {
	if strings.Contains(text, "\x00") {
		return Result{Mode: "in_memory", Reason: "dump contains NUL bytes", Tables: []string{}}
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	statements := Split(text)
	tables := map[string]struct{}{}
	var problems []string
	inserts, creates := 0, 0
	allowed := []string{
		"CREATE DATABASE", "DROP DATABASE", "LOCK TABLES", "UNLOCK TABLES",
		"SET ", "START TRANSACTION", "COMMIT", "ROLLBACK", "USE ",
		"ALTER TABLE", "CREATE INDEX", "CREATE UNIQUE INDEX",
		"CREATE VIEW", "DROP VIEW", "CREATE ALGORITHM",
	}
	for _, statement := range statements {
		head := collapse(statement)
		upper := strings.ToUpper(head)
		switch {
		case strings.HasPrefix(upper, "CREATE TABLE"):
			name := first(createTableRe, head)
			if name == "" {
				problems = append(problems, "CREATE TABLE without a table name")
			} else {
				tables[name] = struct{}{}
				creates++
			}
		case strings.HasPrefix(upper, "INSERT INTO"), strings.HasPrefix(upper, "REPLACE INTO"):
			name := first(insertRe, head)
			inserts++
			if name == "" {
				problems = append(problems, "INSERT without a table name")
			} else if _, ok := tables[name]; !ok {
				problems = append(problems, "INSERT into unknown table "+name)
			}
		case strings.HasPrefix(upper, "DROP TABLE"):
			if name := first(dropRe, head); name != "" {
				delete(tables, name)
			}
		default:
			ok := false
			for _, prefix := range allowed {
				if strings.HasPrefix(upper, prefix) {
					ok = true
					break
				}
			}
			if !ok {
				snippet := upper
				if len(snippet) > 80 {
					snippet = snippet[:80]
				}
				problems = append(problems, "unrecognized statement: "+snippet)
			}
		}
	}
	if creates == 0 {
		problems = append(problems, "dump has no CREATE TABLE")
	}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sortStrings(names)
	result := Result{
		Importable: len(problems) == 0,
		Mode:       "in_memory",
		Tables:     names,
		Statements: len(statements),
		Inserts:    inserts,
		Creates:    creates,
	}
	if len(problems) > 0 {
		if len(problems) > 8 {
			problems = problems[:8]
		}
		result.Reason = strings.Join(problems, "; ")
	}
	return result
}

func first(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		j := i
		for j > 0 && values[j] < values[j-1] {
			values[j], values[j-1] = values[j-1], values[j]
			j--
		}
	}
}

// ServerVersion reads a mysqldump header when one is present.
func ServerVersion(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Server version") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if strings.EqualFold(field, "version") && i+1 < len(fields) {
					return strings.Trim(fields[i+1], "`'\"")
				}
			}
		}
		if len(line) > 0 && !strings.HasPrefix(line, "--") && !strings.HasPrefix(line, "/*") {
			if !unicode.IsSpace(rune(line[0])) && strings.Contains(strings.ToLower(line), "create ") {
				break
			}
		}
	}
	return ""
}
