package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/fail"
)

func TestFixtureWordPress(t *testing.T) {
	root := moduleRoot(t)
	got, err := Site(filepath.Join(root, "fixtures", "wordpress-site"), "Fixture WordPress", "fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	if got.Site.Adapter != "wordpress" {
		t.Fatal(got.Site.Adapter)
	}
	if got.Detected.WordPress != "6.7.1" {
		t.Fatalf("wp %q", got.Detected.WordPress)
	}
	if len(got.Detected.Plugins) != 1 || got.Detected.Plugins[0].Version != "0.4.0" {
		t.Fatalf("plugins %#v", got.Detected.Plugins)
	}
	if len(got.Detected.Themes) != 1 || got.Detected.Themes[0].Version != "1.2.3" {
		t.Fatalf("themes %#v", got.Detected.Themes)
	}
	db := got.Site.Sources[1]
	if db.Method != "sql_file" || db.SQLFile != "database.sql" {
		t.Fatalf("database %#v", db)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "fixture-password") {
		t.Fatal("discover stored the database password")
	}
	if db.PasswordSource != "wp-config" {
		t.Fatal(db.PasswordSource)
	}
}

func TestUnreachableIsAuto(t *testing.T) {
	root := moduleRoot(t)
	got, err := Site(filepath.Join(root, "fixtures", "unreachable-db"), "Unreachable", "unreachable-db")
	if err != nil {
		t.Fatal(err)
	}
	if got.Site.Sources[1].Method != "auto" {
		t.Fatal(got.Site.Sources[1].Method)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "super-secret-db-pass") {
		t.Fatal("password leaked")
	}
}

func TestPlainFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Site(dir, "Notes", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if got.Site.Adapter != "files" || len(got.Site.Sources) != 1 {
		t.Fatalf("%#v", got.Site)
	}
}

func TestMissingPathAndAmbiguousSite(t *testing.T) {
	if _, err := Site(filepath.Join(t.TempDir(), "missing"), "", ""); err == nil {
		t.Fatal("missing path was accepted")
	}
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Site(file, "", ""); err == nil {
		t.Fatal("a file was accepted as a site")
	}

	parent := t.TempDir()
	for _, name := range []string{"one", "two"} {
		root := filepath.Join(parent, name, "wp-includes")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, name, "wp-config.php"), []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "version.php"), []byte("<?php $wp_version = '1.0';\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Site(parent, "", "")
	fe, ok := err.(*fail.Error)
	if !ok || fe.Code != "ambiguous_site" {
		t.Fatal(err)
	}
}

func TestExcludeReason(t *testing.T) {
	if ExcludeReason("wp-content/upgrade/partial.tmp", []string{"wp-content/upgrade"}) != "upgrade" {
		t.Fatal("upgrade")
	}
	if ExcludeReason("wp-content/debug.log", []string{"wp-content/debug.log"}) != "debug-log" {
		t.Fatal("debug log")
	}
	if ExcludeReason("wp-content/uploads/note.txt", []string{"wp-content/upgrade"}) != "" {
		t.Fatal("uploads")
	}
}

func TestSkipReason(t *testing.T) {
	if SkipReason("wp-content/cache/nested/page.html") != "cache" {
		t.Fatal("cache")
	}
	if SkipReason("node_modules/left-pad/index.js") != "node_modules" {
		t.Fatal("node_modules")
	}
	if SkipReason("wp-content/uploads/2024/note.txt") != "" {
		t.Fatal("uploads should be kept")
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
