package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/addon"
	"backupbinder/internal/model"
)

func TestExportMySQLPassesDefaultsOnStdin(t *testing.T) {
	record := t.TempDir()
	binDir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$1" > "$RECORD/arg"
cat > "$RECORD/stdin"
out=
for arg in "$@"; do
  case "$arg" in
    --result-file=*) out="${arg#--result-file=}" ;;
  esac
done
printf '%s\n' 'CREATE TABLE t (a int);' 'INSERT INTO t VALUES (1);' > "$out"
`
	if err := os.WriteFile(filepath.Join(binDir, "mysqldump"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECORD", record)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	secretDir := t.TempDir()
	const password = "stub-secret"
	if err := os.WriteFile(filepath.Join(secretDir, "fixture-database.env"), []byte("DB_PASSWORD="+password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	site := model.Site{
		ID: "fixture",
		Sources: []model.Source{{
			ID: "database", Kind: "database", Name: "app", User: "backup", Host: "localhost",
			PasswordSource: "saved",
		}},
	}
	output := filepath.Join(t.TempDir(), "database.sql")
	if _, _, err := exportMySQL(addon.Addon{}, addon.Dump{}, site, output, secretDir, nil); err != nil {
		t.Fatal(err)
	}

	arg, err := os.ReadFile(filepath.Join(record, "arg"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(arg)) != "--defaults-file=/dev/stdin" {
		t.Fatalf("arg %q", arg)
	}
	stdin, err := os.ReadFile(filepath.Join(record, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdin), "password="+password) || !strings.Contains(string(stdin), "user=backup") {
		t.Fatalf("stdin %q", stdin)
	}
	matches, _ := filepath.Glob(filepath.Join(secretDir, "mysqldump-*.cnf"))
	if len(matches) != 0 {
		t.Fatal(matches)
	}
}
