package store

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	"backupbinder/internal/model"

	_ "modernc.org/sqlite"
)

func TestLimitColumnsMigrate(t *testing.T) {
	home := t.TempDir()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(home, "registry.sqlite"))}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE sites (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root TEXT NOT NULL,
  adapter TEXT NOT NULL,
  sources_json TEXT NOT NULL,
  schedule_cron TEXT,
  schedule_enabled INTEGER NOT NULL DEFAULT 0,
  schedule_last_slot TEXT,
  restore_verified_at TEXT,
  restore_run_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO sites (id, name, root, adapter, sources_json, schedule_enabled, created_at, updated_at)
		VALUES ('demo', 'Demo', '/tmp/demo', 'files', '[]', 0, 't', 't')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	site, err := opened.GetSite("demo")
	if err != nil {
		t.Fatal(err)
	}
	if site.Limits.UploadKiB != 0 || site.Limits.DownloadKiB != 0 {
		t.Fatalf("%#v", site.Limits)
	}
	if err := opened.SaveLimits("demo", model.Limits{UploadKiB: 8, DownloadKiB: 4}); err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	site, err = again.GetSite("demo")
	if err != nil {
		t.Fatal(err)
	}
	if site.Limits.UploadKiB != 8 || site.Limits.DownloadKiB != 4 {
		t.Fatalf("%#v", site.Limits)
	}
	if site.Retention != model.DefaultRetention() {
		t.Fatalf("default retention %#v", site.Retention)
	}
	if err := again.SaveRetention("demo", model.Retention{}); err != nil {
		t.Fatal(err)
	}
	site, err = again.GetSite("demo")
	if err != nil {
		t.Fatal(err)
	}
	if site.Retention.Active() {
		t.Fatalf("zeros should keep every snapshot: %#v", site.Retention)
	}
}
