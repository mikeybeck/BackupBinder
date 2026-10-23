package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-backup/internal/model"
)

func TestFixtureBackupAndScratchRestore(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	before := mustRead(t, filepath.Join(root, "wp-content", "uploads", "2024", "note.txt"))
	if _, err := application.Register(root, "Fixture WordPress", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.AddDestination("fixture-wordpress", destinationSFTP(false), "", ""); err != nil {
		t.Fatal(err)
	}
	manifest, err := application.CreateBackup("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "local-backup.manifest/v1" || manifest.Snapshot.ResticSnapshotID == "" {
		t.Fatalf("manifest snapshot %#v", manifest.Snapshot)
	}
	if manifest.Restore.Confidence != "never" || len(manifest.Restore.Steps) < 4 {
		t.Fatalf("restore %#v", manifest.Restore)
	}
	if manifest.Captured.Database == nil || manifest.Captured.Database.Method != "sql_file" || !manifest.Captured.Database.Importable {
		t.Fatalf("database %#v", manifest.Captured.Database)
	}
	if manifest.Site.Versions.WordPress != "6.7.1" {
		t.Fatal(manifest.Site.Versions.WordPress)
	}
	for _, item := range manifest.Captured.Files.Inventory {
		if strings.Contains(item.Path, "node_modules") || strings.Contains(item.Path, "cache") || strings.Contains(item.Path, "upgrade") {
			t.Fatalf("junk was captured: %s", item.Path)
		}
	}
	kept := false
	for _, item := range manifest.Captured.Files.Inventory {
		if item.Path == "wp-content/uploads/2024/note.txt" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("upload was not captured")
	}
	var sawDisabled, sawLocal bool
	for _, dest := range manifest.Destinations {
		switch dest.Status {
		case "disabled":
			sawDisabled = true
			if dest.Written {
				t.Fatal("disabled destination was written")
			}
		case "stored":
			sawLocal = dest.Written
		case "copied", "failed":
			t.Fatalf("unexpected destination status %#v", dest)
		}
	}
	if !sawDisabled || !sawLocal {
		t.Fatalf("destinations %#v", manifest.Destinations)
	}
	registry, err := os.ReadFile(filepath.Join(application.Home, "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registry), "fixture-password") {
		t.Fatal("registry contains the database password")
	}
	after := mustRead(t, filepath.Join(root, "wp-content", "uploads", "2024", "note.txt"))
	if after != before {
		t.Fatal("backup changed the live site")
	}
	verified, err := application.Verify("fixture-wordpress", manifest.Snapshot.RunID)
	if err != nil || !verified.Readable || verified.ResticCheck != "passed" {
		t.Fatal(err, verified)
	}
	restored, err := application.TestRestore("fixture-wordpress", manifest.Snapshot.RunID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !restored.FilesMatch || !restored.DatabaseImportable || !restored.LiveSiteUntouched || restored.RestoreConfidence == "never" {
		t.Fatalf("%#v", restored)
	}
	if _, err := os.Stat(filepath.Join(restored.Scratch, "files", "wp-content", "uploads", "2024", "note.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(restored.Scratch, "files", "node_modules")); !os.IsNotExist(err) {
		t.Fatal("node_modules restored")
	}
	if _, err := application.RestoreBackup("fixture-wordpress", "", root); err == nil {
		t.Fatal("restore onto the live site was allowed")
	}
	if mustRead(t, filepath.Join(root, "index.php")) == "" {
		t.Fatal("live index missing")
	}
}

func TestCompareAndDiskCopy(t *testing.T) {
	application := openTest(t)
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(application.Home, "other-disk")
	if _, err := application.AddDestination("notes", diskDestination(disk, true), "", ""); err != nil {
		t.Fatal(err)
	}
	first, err := application.CreateBackup("notes")
	if err != nil {
		t.Fatal(err)
	}
	copied := false
	for _, dest := range first.Destinations {
		if dest.Kind == "disk" && dest.Status == "copied" && dest.Written {
			copied = true
		}
	}
	if !copied {
		t.Fatalf("disk copy %#v", first.Destinations)
	}
	if _, err := os.Stat(filepath.Join(disk, "config")); err != nil {
		t.Fatal("second restic repo was not initialized", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := application.CreateBackup("notes")
	if err != nil {
		t.Fatal(err)
	}
	compared, err := application.Compare("notes", first.Snapshot.RunID, second.Snapshot.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(compared.Added) != 1 || compared.Added[0] != "extra.txt" {
		t.Fatalf("%#v", compared)
	}
}

func TestUnreachableDatabase(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "unreachable-db")
	if _, err := application.Register(root, "Unreachable", "unreachable-db", true); err != nil {
		t.Fatal(err)
	}
	_, err := application.CreateBackup("unreachable-db")
	if err == nil {
		t.Fatal("expected database failure")
	}
	explained, err := application.Explain("unreachable-db", "")
	if err != nil {
		t.Fatal(err)
	}
	if explained.Failure == nil || explained.Failure.Step != "database_dump" {
		t.Fatalf("%#v", explained.Failure)
	}
	raw, err := os.ReadFile(filepath.Join(application.Home, "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-db-pass") {
		t.Fatal("failure record stored the database password")
	}
	matches, _ := filepath.Glob(filepath.Join(application.Home, "secrets", "mysqldump-*.cnf"))
	if len(matches) != 0 {
		t.Fatal("mysqldump defaults file was left behind")
	}
}

func TestScheduleRunsBackup(t *testing.T) {
	application := openTest(t)
	dir := filepath.Join(application.Home, "cron-site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Cron", "cron-site", true); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 25, 3, 15, 40, 0, time.UTC)
	if _, err := application.SetSchedule("cron-site", "15 3 * * *", true); err != nil {
		t.Fatal(err)
	}
	first, err := application.RunScheduled(when)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Ran) != 1 || !first.Ran[0].OK {
		t.Fatalf("%#v", first)
	}
	second, err := application.RunScheduled(when)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Ran) != 0 || len(second.Skipped) != 1 || second.Skipped[0].Reason != "already_ran" {
		t.Fatalf("%#v", second)
	}
}

func TestListSites(t *testing.T) {
	application := openTest(t)
	empty, err := application.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	if !empty.OK || len(empty.Sites) != 0 {
		t.Fatalf("%#v", empty)
	}
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture WordPress", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	listed, err := application.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Sites) != 1 || listed.Sites[0].ID != "fixture-wordpress" || listed.Sites[0].Name != "Fixture WordPress" || listed.Sites[0].Adapter != "wordpress" {
		t.Fatalf("%#v", listed)
	}
	if listed.Sites[0].Root == "" {
		t.Fatal("root was empty")
	}
}

func destinationSFTP(enabled bool) model.Destination {
	return model.Destination{Kind: "sftp", Enabled: enabled, Host: "files.example", User: "backup", Path: "/repos/site"}
}

func diskDestination(path string, enabled bool) model.Destination {
	return model.Destination{Kind: "disk", Enabled: enabled, Path: path}
}

func openTest(t *testing.T) *App {
	t.Helper()
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	return application
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

func mustRead(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
