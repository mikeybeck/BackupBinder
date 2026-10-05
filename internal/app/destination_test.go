package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
