package sqlcheck

import (
	"os"
	"path/filepath"
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
