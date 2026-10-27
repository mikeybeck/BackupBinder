package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/model"
)

func TestRetentionPolicy(t *testing.T) {
	if !model.DefaultRetention().Active() {
		t.Fatal("default policy is empty")
	}
	if (model.Retention{}).Active() {
		t.Fatal("zero policy should keep every snapshot")
	}
	got := strings.Join(model.Retention{KeepDaily: 1}.ForgetArgs(), " ")
	if got != "--keep-daily 1" {
		t.Fatal(got)
	}
	n, set, err := ParseCount(" 0 ")
	if err != nil || !set || n != 0 {
		t.Fatal(n, set, err)
	}
	if _, _, err := ParseCount("nope"); err == nil {
		t.Fatal("expected bad count")
	}
}

func TestRetentionDropsOlderSnapshots(t *testing.T) {
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
	if first.Retention == nil || first.Retention.Kept < 1 {
		t.Fatalf("first retention %#v", first.Retention)
	}
	if _, err := application.TestRestore("notes", first.Snapshot.RunID, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateBackup("notes"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("three"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := application.CreateBackup("notes")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := application.ListBackups("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Backups) != 3 {
		t.Fatalf("default policy removed a snapshot: %#v", listed.Backups)
	}

	report, err := application.SetRetention("notes", model.Retention{KeepDaily: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || len(report.Removed) != 2 || report.Kept != 1 {
		t.Fatalf("%#v", report)
	}
	listed, err = application.ListBackups("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Backups) != 1 || listed.Backups[0].RunID != third.Snapshot.RunID {
		t.Fatalf("%#v", listed.Backups)
	}
	if _, err := application.Verify("notes", first.Snapshot.RunID); err == nil {
		t.Fatal("removed run still verifies")
	}
	if _, err := os.Stat(application.manifestPath("notes", first.Snapshot.RunID)); !os.IsNotExist(err) {
		t.Fatal("manifest file still exists", err)
	}
	site, err := application.DB.GetSite("notes")
	if err != nil {
		t.Fatal(err)
	}
	if site.RestoreConfidence.RunID != "" || site.RestoreConfidence.LastVerifiedAt != "" {
		t.Fatalf("restore confidence %#v", site.RestoreConfidence)
	}
	if n := snapshotCount(t, application, disk); n != 1 {
		t.Fatalf("disk copy has %d snapshots", n)
	}
	if _, err := application.Verify("notes", third.Snapshot.RunID); err != nil {
		t.Fatal(err)
	}

	kept, err := application.SetRetention("notes", model.Retention{})
	if err != nil {
		t.Fatal(err)
	}
	if kept.Applied || kept.Note != "Every snapshot is kept." {
		t.Fatalf("%#v", kept)
	}
	if _, err := application.CreateBackup("notes"); err != nil {
		t.Fatal(err)
	}
	listed, err = application.ListBackups("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Backups) != 2 {
		t.Fatalf("keep-all removed a snapshot: %#v", listed.Backups)
	}
	if n := snapshotCount(t, application, disk); n != 2 {
		t.Fatalf("disk copy has %d snapshots", n)
	}
}

func snapshotCount(t *testing.T, application *App, repo string) int {
	t.Helper()
	password, err := application.passwordFile("notes")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(application.Restic.Binary, "--cache-dir", application.Restic.CacheDir, "-r", repo, "--password-file", password, "snapshots", "--json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var snaps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &snaps); err != nil {
		t.Fatal(err)
	}
	return len(snaps)
}
