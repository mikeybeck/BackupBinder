package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/model"
)

func TestSetDatabaseToggleAndPassword(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })

	root := filepath.Join("..", "..", "fixtures", "wordpress-site")
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := application.Register(root, "Fixture WordPress", "fixture-wordpress", true)
	if err != nil {
		t.Fatal(err)
	}
	if model.DatabaseSource(registered.Site) == nil {
		t.Fatal("expected discovered database")
	}

	off, err := application.SetDatabase("fixture-wordpress", DatabaseInput{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if model.DatabaseSource(off) != nil {
		t.Fatalf("database still enabled: %#v", off.Sources)
	}

	on, err := application.SetDatabase("fixture-wordpress", DatabaseInput{
		Enabled: true, Password: "ui-secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	db := model.DatabaseSource(on)
	if db == nil || db.PasswordSource != "saved" {
		t.Fatalf("%#v", db)
	}
	if application.DatabasePasswordLabel(on) != "present" {
		t.Fatal(application.DatabasePasswordLabel(on))
	}
	secret, err := os.ReadFile(application.databaseSecretPath("fixture-wordpress"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(secret), "DB_PASSWORD=ui-secret-password") {
		t.Fatal(string(secret))
	}
	raw, err := os.ReadFile(filepath.Join(application.Home, "registry.sqlite"))
	if err == nil && strings.Contains(string(raw), "ui-secret-password") {
		t.Fatal("registry stored the database password")
	}

	cleared, err := application.SetDatabase("fixture-wordpress", DatabaseInput{
		Enabled: true, PreferConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	db = model.DatabaseSource(cleared)
	if db == nil || db.PasswordSource == "saved" {
		t.Fatalf("%#v", db)
	}
	if _, err := os.Stat(application.databaseSecretPath("fixture-wordpress")); !os.IsNotExist(err) {
		t.Fatal("saved password still present")
	}
}

func TestSetDatabaseManualFields(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetDatabase("notes", DatabaseInput{Enabled: true}); err == nil {
		t.Fatal("expected missing connection fields")
	}
	saved, err := application.SetDatabase("notes", DatabaseInput{
		Enabled: true, Name: "app", User: "backup", Host: "db.example", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	db := model.DatabaseSource(saved)
	if db == nil || db.Name != "app" || db.User != "backup" || db.Host != "db.example" || db.PasswordSource != "saved" {
		t.Fatalf("%#v", db)
	}
}
