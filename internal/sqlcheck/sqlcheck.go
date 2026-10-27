// Package sqlcheck decides whether a dump can be imported, without a MySQL server.
package sqlcheck

import (
	"bufio"
	"io"
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
		return nulDump("in_memory")
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	a := newAssessor()
	for _, statement := range Split(text) {
		a.statement(statement)
	}
	return a.finish("in_memory")
}

// AssessReader applies Assess without holding the dump in memory.
// Only the start of each statement is kept; quote state still covers the rest,
// so a semicolon inside a long string does not end the statement.
func AssessReader(r io.Reader) Result {
	in := bufio.NewReader(r)
	if bom, err := in.Peek(3); err == nil && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		_, _ = in.Discard(3)
	}
	var push []byte
	var readErr error
	read := func() (byte, bool) {
		if readErr != nil {
			return 0, false
		}
		if len(push) > 0 {
			b := push[0]
			push = push[1:]
			return b, true
		}
		b, err := in.ReadByte()
		if err == io.EOF {
			return 0, false
		}
		if err != nil {
			readErr = err
			return 0, false
		}
		return b, true
	}
	unread := func(b byte) {
		push = append([]byte{b}, push...)
	}
	a := newAssessor()
	var head []byte
	started := false
	add := func(b byte) {
		if !started {
			if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
				return
			}
			started = true
		}
		if len(head) < 8192 {
			head = append(head, b)
		}
	}
	flush := func() {
		if stmt := strings.TrimSpace(string(head)); stmt != "" {
			a.statement(stmt)
		}
		head = head[:0]
		started = false
	}
	skipLine := func(first byte) Result {
		if first == 0 {
			return nulDump("stream")
		}
		if first == '\n' {
			unread(first)
			return Result{}
		}
		for {
			b, ok := read()
			if !ok {
				return Result{}
			}
			if b == 0 {
				return nulDump("stream")
			}
			if b == '\n' {
				unread(b)
				return Result{}
			}
		}
	}

	inSingle, inDouble, inBack := false, false, false
	for {
		c, ok := read()
		if !ok {
			break
		}
		if c == 0 {
			return nulDump("stream")
		}
		if !inSingle && !inDouble && !inBack {
			if c == '-' {
				n1, ok1 := read()
				if ok1 && n1 == '-' {
					n2, ok2 := read()
					if !ok2 || n2 == ' ' || n2 == '\t' || n2 < ' ' {
						if ok2 {
							if bad := skipLine(n2); bad.Reason != "" {
								return bad
							}
						}
						continue
					}
					unread(n2)
				}
				if ok1 {
					unread(n1)
				}
				add(c)
				continue
			}
			if c == '#' {
				for {
					b, okb := read()
					if !okb {
						break
					}
					if b == 0 {
						return nulDump("stream")
					}
					if b == '\n' {
						unread(b)
						break
					}
				}
				continue
			}
			if c == '/' {
				n1, ok1 := read()
				if ok1 && n1 == '*' {
					prev := byte(0)
					for {
						b, okb := read()
						if !okb {
							break
						}
						if b == 0 {
							return nulDump("stream")
						}
						if prev == '*' && b == '/' {
							break
						}
						prev = b
					}
					continue
				}
				if ok1 {
					unread(n1)
				}
				add(c)
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
				flush()
				continue
			}
			add(c)
			continue
		}
		add(c)
		if c == '\\' && (inSingle || inDouble) {
			n1, ok1 := read()
			if !ok1 {
				continue
			}
			if n1 == 0 {
				return nulDump("stream")
			}
			add(n1)
			continue
		}
		if inSingle && c == '\'' {
			n1, ok1 := read()
			if ok1 && n1 == '\'' {
				add(n1)
				continue
			}
			if ok1 {
				unread(n1)
			}
			inSingle = false
		} else if inDouble && c == '"' {
			n1, ok1 := read()
			if ok1 && n1 == '"' {
				add(n1)
				continue
			}
			if ok1 {
				unread(n1)
			}
			inDouble = false
		} else if inBack && c == '`' {
			n1, ok1 := read()
			if ok1 && n1 == '`' {
				add(n1)
				continue
			}
			if ok1 {
				unread(n1)
			}
			inBack = false
		}
	}
	flush()
	if readErr != nil {
		return Result{Mode: "stream", Reason: "could not read the dump", Tables: []string{}}
	}
	return a.finish("stream")
}

func nulDump(mode string) Result {
	return Result{Mode: mode, Reason: "dump contains NUL bytes", Tables: []string{}}
}

type assessor struct {
	tables     map[string]struct{}
	problems   []string
	inserts    int
	creates    int
	statements int
}

func newAssessor() *assessor {
	return &assessor{tables: map[string]struct{}{}}
}

func (a *assessor) statement(stmt string) {
	a.statements++
	head := collapse(stmt)
	upper := strings.ToUpper(head)
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"):
		name := first(createTableRe, head)
		if name == "" {
			a.problems = append(a.problems, "CREATE TABLE without a table name")
		} else {
			a.tables[name] = struct{}{}
			a.creates++
		}
	case strings.HasPrefix(upper, "INSERT INTO"), strings.HasPrefix(upper, "REPLACE INTO"):
		name := first(insertRe, head)
		a.inserts++
		if name == "" {
			a.problems = append(a.problems, "INSERT without a table name")
		} else if _, ok := a.tables[name]; !ok {
			a.problems = append(a.problems, "INSERT into unknown table "+name)
		}
	case strings.HasPrefix(upper, "DROP TABLE"):
		if name := first(dropRe, head); name != "" {
			delete(a.tables, name)
		}
	default:
		allowed := []string{
			"CREATE DATABASE", "DROP DATABASE", "LOCK TABLES", "UNLOCK TABLES",
			"SET ", "START TRANSACTION", "COMMIT", "ROLLBACK", "USE ",
			"ALTER TABLE", "CREATE INDEX", "CREATE UNIQUE INDEX",
			"CREATE VIEW", "DROP VIEW", "CREATE ALGORITHM",
		}
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
			a.problems = append(a.problems, "unrecognized statement: "+snippet)
		}
	}
}

func (a *assessor) finish(mode string) Result {
	if a.creates == 0 {
		a.problems = append(a.problems, "dump has no CREATE TABLE")
	}
	names := make([]string, 0, len(a.tables))
	for name := range a.tables {
		names = append(names, name)
	}
	sortStrings(names)
	result := Result{
		Importable: len(a.problems) == 0,
		Mode:       mode,
		Tables:     names,
		Statements: a.statements,
		Inserts:    a.inserts,
		Creates:    a.creates,
	}
	if len(a.problems) > 0 {
		problems := a.problems
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
