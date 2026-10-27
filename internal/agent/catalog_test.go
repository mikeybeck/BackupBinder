package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/app"
	"backupbinder/internal/fail"
)

func TestCatalogIncludesSetup(t *testing.T) {
	doc := CatalogDocument()
	if !doc.OK || doc.Instructions == "" {
		t.Fatal(doc.Instructions)
	}
	found := false
	for _, cmd := range doc.Commands {
		if cmd.Name == "setup" {
			found = true
			sawPath := false
			sawRemote := false
			for _, flag := range cmd.Flags {
				if flag.Name == "remote-path" {
					sawPath = true
				}
				if flag.Name == "remote" {
					sawRemote = true
					if !strings.Contains(flag.Placeholder, "gdrive") || !strings.Contains(flag.Placeholder, "sftp") {
						t.Fatalf("remote placeholder %q", flag.Placeholder)
					}
				}
			}
			if !sawPath {
				t.Fatal("remote-path missing")
			}
			if !sawRemote {
				t.Fatal("remote missing")
			}
		}
	}
	if !found {
		t.Fatal("setup missing")
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("setup"); !ok {
		t.Fatal("setup")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("unknown command")
	}
	cmd, ok := Lookup("set_limits")
	if !ok || !cmd.Tool {
		t.Fatal("set_limits")
	}
	sawUpload, sawDownload := false, false
	for _, flag := range cmd.Flags {
		if flag.Name == "limit-upload" {
			sawUpload = true
		}
		if flag.Name == "limit-download" {
			sawDownload = true
		}
	}
	if !sawUpload || !sawDownload {
		t.Fatal(cmd.Flags)
	}
}

func TestSetLimitsCommand(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Limited", "limited", true); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(application, "set_limits", map[string]any{
		"id": "limited", "root": dir, "limit-upload": "128",
	}); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("limited")
	if err != nil {
		t.Fatal(err)
	}
	if site.Limits.UploadKiB != 128 || site.Limits.DownloadKiB != 0 {
		t.Fatalf("%#v", site.Limits)
	}
	_, err = Execute(application, "set_limits", map[string]any{"id": "limited", "root": dir})
	if fe, ok := err.(*fail.Error); !ok || fe.Code != "bad_limit" {
		t.Fatal(err)
	}
	_, err = Execute(application, "set_limits", map[string]any{
		"id": "limited", "root": dir, "limit-download": "nope",
	})
	if fe, ok := err.(*fail.Error); !ok || fe.Code != "bad_limit" {
		t.Fatal(err)
	}
}

func TestSetRetentionCommand(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Kept", "kept", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("set_retention"); !ok {
		t.Fatal("set_retention")
	}
	if _, ok := Lookup("apply_retention"); !ok {
		t.Fatal("apply_retention")
	}
	raw, err := Execute(application, "set_retention", map[string]any{
		"id": "kept", "root": dir, "keep-daily": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := raw.(app.RetentionReport)
	if !ok || !report.OK || report.Retention.KeepDaily != 1 || report.Retention.KeepLast != 3 {
		t.Fatalf("%#v", raw)
	}
	if report.Note != "These rules apply on the next backup." {
		t.Fatal(report.Note)
	}
	_, err = Execute(application, "set_retention", map[string]any{"id": "kept", "root": dir})
	if fe, ok := err.(*fail.Error); !ok || fe.Code != "bad_retention" {
		t.Fatal(err)
	}
	_, err = Execute(application, "set_retention", map[string]any{
		"id": "kept", "root": dir, "keep-weekly": "-1",
	})
	if fe, ok := err.(*fail.Error); !ok || fe.Code != "bad_retention" {
		t.Fatal(err)
	}
	if _, err := Execute(application, "set_retention", map[string]any{
		"id": "kept", "root": dir, "keep-last": "0", "keep-hourly": "0", "keep-daily": "0",
		"keep-weekly": "0", "keep-monthly": "0", "keep-yearly": "0",
	}); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("kept")
	if err != nil {
		t.Fatal(err)
	}
	if site.Retention.Active() {
		t.Fatalf("%#v", site.Retention)
	}
}
