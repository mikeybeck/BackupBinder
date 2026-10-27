package sqlcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemicolonInsideString(t *testing.T) {
	dump := "CREATE TABLE `wp_options` (`option_value` longtext);\nINSERT INTO `wp_options` (`option_value`) VALUES ('a;b');\n"
	got := Assess(dump)
	if !got.Importable {
		t.Fatal(got.Reason)
	}
	if got.Inserts != 1 || got.Creates != 1 {
		t.Fatalf("creates=%d inserts=%d", got.Creates, got.Inserts)
	}
	if len(got.Tables) != 1 || got.Tables[0] != "wp_options" {
		t.Fatalf("tables %#v", got.Tables)
	}
}

func TestInsertBeforeCreate(t *testing.T) {
	got := Assess("INSERT INTO wp_options (option_id) VALUES (1);\nCREATE TABLE wp_options (option_id bigint);\n")
	if got.Importable {
		t.Fatal("expected not importable")
	}
}

func TestMariaDBDumpComments(t *testing.T) {
	dump := "-- MariaDB dump 10.19\n--\n-- Host: localhost    Database: local\n--\nDROP TABLE IF EXISTS `wp_posts`;\nCREATE TABLE `wp_posts` (id bigint);\nINSERT INTO `wp_posts` (id) VALUES (1);\n"
	got := Assess(dump)
	if !got.Importable {
		t.Fatal(got.Reason)
	}
	if got.Creates != 1 || got.Inserts != 1 || len(got.Tables) != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestFixtureDump(t *testing.T) {
	root := moduleRoot(t)
	text, err := os.ReadFile(filepath.Join(root, "fixtures", "wordpress-site", "database.sql"))
	if err != nil {
		t.Fatal(err)
	}
	got := Assess(string(text))
	if !got.Importable {
		t.Fatal(got.Reason)
	}
	if got.Inserts != 2 {
		t.Fatalf("inserts %d", got.Inserts)
	}
}

func TestAssessReaderMatchesAssess(t *testing.T) {
	dumps := []string{
		"CREATE TABLE `wp_options` (`option_value` longtext);\nINSERT INTO `wp_options` (`option_value`) VALUES ('a;b');\n",
		"INSERT INTO wp_options (option_id) VALUES (1);\nCREATE TABLE wp_options (option_id bigint);\n",
		"-- MariaDB dump 10.19\n--\nDROP TABLE IF EXISTS `wp_posts`;\nCREATE TABLE `wp_posts` (id bigint);\nINSERT INTO `wp_posts` (id) VALUES (1);\n",
		"CREATE TABLE t (a int);\n",
		"",
		"\uFEFFCREATE TABLE t (a int);\nINSERT INTO t VALUES (1);\n",
	}
	root := moduleRoot(t)
	text, err := os.ReadFile(filepath.Join(root, "fixtures", "wordpress-site", "database.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dumps = append(dumps, string(text))
	body := strings.Repeat("a", 9000) + ";b"
	dumps = append(dumps, "CREATE TABLE t (a int);\nINSERT INTO t VALUES ('"+body+"');\n")
	for _, dump := range dumps {
		want := Assess(dump)
		got := AssessReader(strings.NewReader(dump))
		if got.Importable != want.Importable || got.Reason != want.Reason || got.Inserts != want.Inserts || got.Creates != want.Creates || got.Statements != want.Statements || strings.Join(got.Tables, ",") != strings.Join(want.Tables, ",") {
			t.Fatalf("stream %#v\nmemory %#v", got, want)
		}
	}
}

func TestAssessReaderReadsACreatePastTheFirstMegabyte(t *testing.T) {
	var b strings.Builder
	for b.Len() < (1<<20)+10 {
		b.WriteString("-- comment\n")
	}
	b.WriteString("CREATE TABLE t (a int);\nINSERT INTO t VALUES (1);\n")
	dump := b.String()
	want := Assess(dump)
	got := AssessReader(strings.NewReader(dump))
	if !got.Importable || got.Reason != want.Reason || got.Creates != 1 || got.Inserts != 1 {
		t.Fatalf("stream %#v memory %#v", got, want)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
