package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/archive"
	"local-backup/internal/model"
)

func TestAddDestinationRejectsUnsafeTargets(t *testing.T) {
	application := openTest(t)
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
	repo := application.repoPath(model.Site{ID: "notes"})
	bad := []model.Destination{
		{Kind: "ftp", Host: "files.example"},
		{Kind: "sftp", Host: "files.example;id", User: "backup", Path: "/repos/site"},
		{Kind: "sftp", Host: "files.example", User: "backup;id", Path: "/repos/site"},
		{Kind: "sftp", Host: "files.example", User: "backup", Path: "repos/site"},
		{Kind: "sftp", Host: "files.example", User: "backup", Path: "/repos/my site"},
		{Kind: "disk", Path: "relative"},
		{Kind: "disk", Path: dir},
		{Kind: "disk", Path: repo},
		{Kind: "disk", Path: filepath.Dir(repo)},
		{Kind: "s3", Bucket: "bad bucket"},
		{Kind: "s3", Bucket: "site-backups", Endpoint: "http://evil host"},
		{Kind: "gdrive", Host: "gdrive;rm", Path: "local-backup/site"},
		{Kind: "gdrive", Host: "gdrive", Path: "local backup/site"},
	}
	for _, dest := range bad {
		if _, err := application.AddDestination("notes", dest, "", ""); err == nil {
			t.Fatalf("accepted %#v", dest)
		}
	}
	if _, err := application.AddDestination("notes", model.Destination{Kind: "sftp", Host: "files.example", User: "backup", Path: "/repos/site"}, "key", "secret"); err == nil {
		t.Fatal("sftp accepted a secret")
	}
	if _, err := application.AddDestination("notes", model.Destination{Kind: "s3", Bucket: "site-backups"}, "key\n", "secret"); err == nil {
		t.Fatal("newline in a credential was accepted")
	}
	if _, err := application.AddDestination("notes", model.Destination{Kind: "gdrive", Host: "gdrive", Path: "local-backup/site"}, "key", "secret"); err == nil {
		t.Fatal("gdrive accepted a secret")
	}
}

func TestAddDestinationStoresGoogleDrive(t *testing.T) {
	fakeRclone(t, "gdrive", true)
	application := openTest(t)
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
	dest, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/notes", Enabled: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if dest.ID != "gdrive-1" || dest.Host != "gdrive" || dest.Path != "local-backup/notes" || !dest.Enabled {
		t.Fatalf("%#v", dest)
	}
	url, err := archive.DestinationURL(dest)
	if err != nil || url != "rclone:gdrive:local-backup/notes" {
		t.Fatal(url, err)
	}
}

func TestRemoveAndDisableDestination(t *testing.T) {
	fakeRclone(t, "gdrive", true)
	application := openTest(t)
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
	first, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/a", Enabled: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/b", Enabled: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := application.SetDestinationEnabled("notes", first.ID, false)
	if err != nil || disabled.Enabled {
		t.Fatalf("%#v %v", disabled, err)
	}
	if err := application.RemoveDestination("notes", second.ID); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("notes")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, dest := range site.Destinations {
		if dest.ID == second.ID {
			t.Fatal("second destination still present")
		}
		if dest.ID == first.ID {
			found = true
			if dest.Enabled {
				t.Fatal("first should stay disabled")
			}
		}
		if dest.Kind == "local_restic" {
			if err := application.RemoveDestination("notes", dest.ID); err == nil {
				t.Fatal("removed local restic")
			}
		}
	}
	if !found {
		t.Fatal("first destination missing")
	}
}

func TestAddDestinationRejectsMissingRcloneRemote(t *testing.T) {
	fakeRclone(t, "other", false)
	application := openTest(t)
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
	_, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/notes", Enabled: false,
	}, "", "")
	if err == nil {
		t.Fatal("expected missing remote")
	}
	if !strings.Contains(err.Error(), "not configured") || !strings.Contains(err.Error(), "gdrive") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "local snapshot is already stored") {
		t.Fatal(err)
	}
}

func TestCreateBackupRejectsMissingRcloneRemoteBeforeArchive(t *testing.T) {
	fakeRclone(t, "gdrive", true)
	application := openTest(t)
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/notes", Enabled: true,
	}, "", ""); err != nil {
		t.Fatal(err)
	}
	fakeRclone(t, "other", false)
	_, err := application.CreateBackup("notes")
	if err == nil {
		t.Fatal("expected missing remote")
	}
	if !strings.Contains(err.Error(), "not configured") || !strings.Contains(err.Error(), "gdrive") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "local snapshot is already stored") {
		t.Fatal(err)
	}
	runs, listErr := application.DB.ListRuns("notes")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(runs) != 0 {
		t.Fatalf("backup run should not have started: %#v", runs)
	}
}

func fakeRclone(t *testing.T, listed string, aboutOK bool) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	about := "exit 1"
	if aboutOK {
		about = "exit 0"
	}
	body := "#!/bin/sh\ncase \"$1\" in\nlistremotes) echo '" + listed + ":'; exit 0 ;;\nabout) " + about + " ;;\n*) echo unexpected \"$1\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)
}

func TestAddDestinationStoresS3KeysOutsideTheRegistry(t *testing.T) {
	application := openTest(t)
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
	secret := "super-secret-key-value"
	dest, err := application.AddDestination("notes", model.Destination{
		Kind: "s3", Bucket: "site-backups", Prefix: "notes", Endpoint: "https://s3.amazonaws.com",
	}, "AKIAEXAMPLE", secret)
	if err != nil {
		t.Fatal(err)
	}
	if dest.Credentials != "present" || dest.CredentialFile == "" {
		t.Fatalf("%#v", dest)
	}
	raw, err := json.Marshal(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "AKIAEXAMPLE") {
		t.Fatal(string(raw))
	}
	info, err := os.Stat(dest.CredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(dest.CredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "AWS_SECRET_ACCESS_KEY="+secret) {
		t.Fatal(string(body))
	}
	registry, err := os.ReadFile(filepath.Join(application.Home, "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registry), secret) {
		t.Fatal("registry contains the secret key")
	}
}
