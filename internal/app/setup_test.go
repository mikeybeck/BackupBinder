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
	if !first.OK || first.SiteID != "fixture-wordpress" || first.Phase != "confirm" || first.Next == nil || first.Next.Step != "database" || !first.Next.Confirm {
		t.Fatalf("%#v", first.Next)
	}
	if first.Config == "" {
		t.Fatal("config path missing")
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
	if _, err := application.Setup(SetupInput{ID: "fixture-wordpress", Root: registered.Site.Root, NoDatabase: true}); err != nil {
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
	file, err := os.ReadFile(saved.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "super-secret-value") || strings.Contains(string(file), "AKIAEXAMPLE") {
		t.Fatal(string(file))
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

func TestSetupGoogleDriveAsksBeforeSaving(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	registered, err := application.Setup(SetupInput{Path: root, ID: "fixture-wordpress"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Setup(SetupInput{ID: "fixture-wordpress", Root: registered.Site.Root, NoDatabase: true}); err != nil {
		t.Fatal(err)
	}
	asked, err := application.Setup(SetupInput{ID: "fixture-wordpress", Root: registered.Site.Root, Remote: "gdrive"})
	if err != nil {
		t.Fatal(err)
	}
	if asked.Phase != "needs_input" || asked.Next == nil || asked.Next.Kind != "gdrive" {
		t.Fatalf("%#v", asked.Next)
	}
	for _, field := range []string{"host", "remote-path"} {
		if !contains(asked.Next.Fields, field) {
			t.Fatalf("missing %s in %#v", field, asked.Next.Fields)
		}
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

func TestSetupDeclinesUntilReady(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	first, err := application.Setup(SetupInput{Path: root, Name: "Fixture WordPress", ID: "fixture-wordpress"})
	if err != nil {
		t.Fatal(err)
	}
	var last SetupResult
	for _, in := range []SetupInput{
		{ID: first.SiteID, Root: first.Site.Root, NoDatabase: true},
		{ID: first.SiteID, Root: first.Site.Root, Remote: "none"},
		{ID: first.SiteID, Root: first.Site.Root, NoSchedule: true},
		{ID: first.SiteID, Root: first.Site.Root, NoBackup: true},
	} {
		last, err = application.Setup(in)
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Phase != "ready" || last.Next != nil || len(last.Setup) != 0 {
		t.Fatalf("%s %#v", last.Phase, last.Next)
	}
}

func TestSetupStartsFromServer(t *testing.T) {
	application := openTest(t)
	partial, err := application.Setup(SetupInput{Host: "files.example", User: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Phase != "needs_input" || partial.SiteID != "" {
		t.Fatalf("%#v", partial.Next)
	}
	listed, err := application.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Sites) != 0 {
		t.Fatal("server was saved before every field was provided")
	}
	result, err := application.Setup(SetupInput{Host: "files.example", User: "backup", RemotePath: "/var/www/site", Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Next == nil || result.Next.Step != "database" || !strings.Contains(result.Next.Reason, "first copy") {
		t.Fatalf("%#v", result.Next)
	}
	site, err := application.DB.GetSite(result.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	if site.Location.Kind != "sftp" || site.Location.Host != "files.example" || site.Location.Path != "/var/www/site" {
		t.Fatalf("%#v", site.Location)
	}
	if !strings.HasPrefix(site.Root, filepath.Join(application.Home, "copies")) {
		t.Fatal(site.Root)
	}
	both, err := application.Setup(SetupInput{Path: site.Root, Host: "other.example", User: "backup", RemotePath: "/var/www/other"})
	if err != nil {
		t.Fatal(err)
	}
	if both.Phase != "needs_input" {
		t.Fatalf("%#v", both.Next)
	}
}

func TestSiteFileReloadAndRejectsSecrets(t *testing.T) {
	home := t.TempDir()
	application, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := application.Setup(SetupInput{Path: dir, Name: "Demo", ID: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(first.Config)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"name": "Demo"`, `"name": "Renamed"`, 1)
	if edited == string(raw) {
		t.Fatal(string(raw))
	}
	if err := os.WriteFile(first.Config, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	site, err := again.DB.GetSite("demo")
	if err != nil {
		t.Fatal(err)
	}
	if site.Name != "Renamed" {
		t.Fatalf("name %s", site.Name)
	}
	bad := strings.Replace(edited, `"name": "Renamed"`, `"name": "Renamed", "password": "nope"`, 1)
	if err := os.WriteFile(first.Config, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := again.ReloadSites(); err == nil {
		t.Fatal("expected the site file to be rejected")
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
