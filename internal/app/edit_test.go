package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/model"
)

func TestSetNameKeepsTheID(t *testing.T) {
	application := openTest(t)
	dir := registerEditNotes(t, application)
	same, unchanged, err := application.SetName("notes", "Notes")
	if err != nil || !unchanged || same.ID != "notes" {
		t.Fatalf("unchanged %v %#v %v", unchanged, same, err)
	}
	renamed, unchanged, err := application.SetName("notes", "Client notes")
	if err != nil || unchanged || renamed.ID != "notes" || renamed.Name != "Client notes" {
		t.Fatalf("unchanged %v %#v %v", unchanged, renamed, err)
	}
	found, err := application.Identify("Client notes")
	if err != nil || len(found.Matches) != 1 || found.Matches[0].ID != "notes" {
		t.Fatalf("%#v %v", found, err)
	}
	_, err = application.Setup(SetupInput{ID: "notes", Root: dir, Name: "Other"})
	if err == nil || !strings.Contains(err.Error(), "set_name --id notes --root") {
		t.Fatal(err)
	}
}

func TestSetDestinationKeepsTheID(t *testing.T) {
	application := openTest(t)
	registerEditNotes(t, application)
	first := t.TempDir()
	second := t.TempDir()
	dest, err := application.AddDestination("notes", model.Destination{
		Kind: "disk", Path: first, Enabled: false,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.SetDestination("notes", dest.ID, DestinationPatch{Path: &second})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != dest.ID || updated.Path != second || updated.Enabled {
		t.Fatalf("%#v", updated)
	}
}

func TestSetDatabaseOmitEnabledKeepsDump(t *testing.T) {
	application := openTest(t)
	registerEditNotes(t, application)
	if _, err := application.SetDatabase("notes", DatabaseInput{
		Enabled: boolPtr(true), Name: "app", User: "backup", Host: "db.example", Password: "secret-one",
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := application.SetDatabase("notes", DatabaseInput{Password: "secret-two"})
	if err != nil {
		t.Fatal(err)
	}
	db := model.DatabaseSource(updated)
	if db == nil || db.PasswordSource != "saved" || db.Name != "app" {
		t.Fatalf("%#v", db)
	}
	secret, err := os.ReadFile(application.databaseSecretPath("notes"))
	if err != nil || !strings.Contains(string(secret), "DB_PASSWORD=secret-two") {
		t.Fatal(err, string(secret))
	}
	left, err := application.SetDatabase("notes", DatabaseInput{})
	if err != nil || model.DatabaseSource(left) == nil || model.DatabaseSource(left).Name != "app" {
		t.Fatal(err)
	}
	if _, err := application.SetDatabase("notes", DatabaseInput{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetDatabase("notes", DatabaseInput{Password: "nope"}); err == nil || !strings.Contains(err.Error(), "--enabled true") {
		t.Fatal(err)
	}
}

func TestRemoveSiteLeavesTheRepository(t *testing.T) {
	application := openTest(t)
	registerEditNotes(t, application)
	site, err := application.DB.GetSite("notes")
	if err != nil {
		t.Fatal(err)
	}
	repo := application.repoPath(site)
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetDatabase("notes", DatabaseInput{
		Enabled: boolPtr(true), Name: "app", User: "backup", Host: "db.example", Password: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	dest, err := application.AddDestination("notes", model.Destination{
		Kind: "s3", Bucket: "bucket-name", Enabled: false,
	}, "access-key", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if dest.CredentialFile == "" {
		t.Fatal("expected a credential file")
	}
	left, err := application.RemoveSite("notes")
	if err != nil || left != repo {
		t.Fatalf("repo %s err %v", left, err)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := application.DB.GetSite("notes"); err == nil {
		t.Fatal("site still registered")
	}
	if _, err := os.Stat(application.siteFile("notes")); !os.IsNotExist(err) {
		t.Fatal("site file still present", err)
	}
	if _, err := os.Stat(application.databaseSecretPath("notes")); !os.IsNotExist(err) {
		t.Fatal("database secret still present", err)
	}
	if _, err := os.Stat(dest.CredentialFile); !os.IsNotExist(err) {
		t.Fatal("destination secret still present", err)
	}
}

func TestInspectShowsRestoreInProgress(t *testing.T) {
	application := openTest(t)
	registerEditNotes(t, application)
	if err := application.writeRestoreStatus("notes", RestoreStatus{InProgress: true, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	inspected, err := application.Inspect("notes")
	if err != nil || inspected.Restore == nil || !inspected.Restore.InProgress {
		t.Fatalf("%#v %v", inspected.Restore, err)
	}
}

func registerEditNotes(t *testing.T, application *App) string {
	t.Helper()
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	return dir
}
