package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRegistersOnceAndAsksBeforeBackup(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	first, err := application.Setup(SetupInput{Path: root, Name: "Fixture WordPress", ID: "fixture-wordpress"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.OK || first.SiteID != "fixture-wordpress" || first.Phase != "confirm" || first.Next == nil || first.Next.Command != "create_backup" || !first.Next.Confirm {
		t.Fatalf("%#v", first)
	}
	second, err := application.Setup(SetupInput{Path: root, ID: "other-id"})
	if err != nil {
		t.Fatal(err)
	}
	if second.SiteID != "fixture-wordpress" {
		t.Fatalf("second id %s", second.SiteID)
	}
	listed, err := application.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Sites) != 1 {
		t.Fatalf("%#v", listed.Sites)
	}
}

func TestSetupRemoteAsksBeforeSaving(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	registered, err := application.Setup(SetupInput{Path: root, ID: "fixture-wordpress"})
	if err != nil {
		t.Fatal(err)
	}
	asked, err := application.Setup(SetupInput{ID: "fixture-wordpress", Root: registered.Site.Root, Remote: "sftp"})
	if err != nil {
		t.Fatal(err)
	}
	if asked.Phase != "needs_input" || asked.Next == nil || asked.Next.Kind != "sftp" {
		t.Fatalf("%#v", asked.Next)
	}
	for _, field := range []string{"host", "user", "remote-path"} {
		if !contains(asked.Next.Fields, field) {
			t.Fatalf("missing %s in %#v", field, asked.Next.Fields)
		}
	}
	site, err := application.Inspect("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range site.Site.Destinations {
		if dest.Kind == "sftp" {
			t.Fatal("sftp was saved before the fields were provided")
		}
	}
	saved, err := application.Setup(SetupInput{
		ID: "fixture-wordpress", Root: registered.Site.Root, Remote: "s3", Bucket: "backups", AccessKey: "AKIAEXAMPLE", SecretKey: "super-secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "super-secret-value") || strings.Contains(string(encoded), "AKIAEXAMPLE") {
		t.Fatal(string(encoded))
	}
	found := false
	for _, dest := range saved.Destinations {
		if dest.Kind == "s3" && !dest.Enabled && dest.Credentials == "present" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%#v", saved.Destinations)
	}
}

func TestSetupLocalPublicFolderName(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(t.TempDir(), "wpfoundry-local", "app", "public")
	if err := os.MkdirAll(filepath.Join(root, "wp-includes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wp-config.php"), []byte("<?php\ndefine('DB_NAME', 'local');\ndefine('DB_USER', 'root');\ndefine('DB_PASSWORD', 'secret');\ndefine('DB_HOST', 'localhost');\n$table_prefix = 'wp_';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wp-includes", "version.php"), []byte("<?php\n$wp_version = '6.9.1';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := application.Setup(SetupInput{Path: filepath.Dir(filepath.Dir(root))})
	if err != nil {
		t.Fatal(err)
	}
	if result.SiteID != "wpfoundry-local" || result.Site.Adapter != "wordpress" {
		t.Fatalf("%#v", result.Site)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal(string(encoded))
	}
}

func contains(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}
