package addon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstDatabaseHostWins(t *testing.T) {
	dir := t.TempDir()
	config := "<?php\ndefine('DB_HOST', 'localhost:/tmp/local.sock');\ndefine('DB_HOST', 'localhost');\ndefine('DB_NAME', 'local');\ndefine('DB_USER', 'root');\ndefine('DB_PASSWORD', 'secret');\n"
	if err := os.WriteFile(filepath.Join(dir, "wp-config.php"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	item, err := ByID("wordpress")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := item.ReadConnection(dir)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Socket != "/tmp/local.sock" || conn.Host != "localhost" {
		t.Fatalf("%#v", conn)
	}
	if conn.PasswordSource != "wp-config" {
		t.Fatal(conn.PasswordSource)
	}
	raw, _ := json.Marshal(conn)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("password returned from ReadConnection")
	}
}

func TestAddonDirectoryOverridesAndRejects(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.26.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addons := filepath.Join(dir, "addons")
	if err := os.MkdirAll(addons, 0o755); err != nil {
		t.Fatal(err)
	}
	override := `{
	  "id": "wordpress",
	  "note": "replaced by the local addon directory",
	  "detect": {"files": ["wp-config.php"], "depth": 1}
	}`
	ghost := `{
	  "id": "ghost",
	  "detect": {"files": ["ghost.json"], "depth": 1},
	  "excludes": ["logs"]
	}`
	if err := os.WriteFile(filepath.Join(addons, "wordpress.json"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(addons, "ghost.json"), []byte(ghost), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, item := range loaded {
		ids[item.ID] = item.Note
	}
	if ids["wordpress"] != "replaced by the local addon directory" {
		t.Fatalf("wordpress %#v", ids)
	}
	if _, ok := ids["ghost"]; !ok {
		t.Fatalf("ids %#v", ids)
	}
	site := filepath.Join(dir, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "ghost.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, root, ok, err := Find(site)
	if err != nil || !ok || found.ID != "ghost" || root != site {
		t.Fatalf("found %#v root %s ok %v err %v", found, root, ok, err)
	}
	evil := `{"id":"evil","detect":{"files":["x"]},"dumps":{"x":{"engine":"argv","argv":["echo","{password}"]}}}`
	if err := os.WriteFile(filepath.Join(addons, "evil.json"), []byte(evil), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("an addon with an unknown placeholder was loaded")
	}
}
