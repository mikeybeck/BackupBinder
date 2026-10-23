package destination

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-backup/internal/model"
)

func TestLoadShippedKinds(t *testing.T) {
	kinds, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, kind := range kinds {
		got[kind.ID] = true
	}
	for _, id := range []string{"sftp", "s3", "b2", "disk", "gdrive"} {
		if !got[id] {
			t.Fatalf("missing %s", id)
		}
	}
	if !strings.Contains(IDs(), "gdrive") {
		t.Fatal(IDs())
	}
}

func TestGdriveRcloneGlobalOptsPaceDrive(t *testing.T) {
	kind, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kind.RcloneArgs, "serve restic --stdio") {
		t.Fatalf("rclone.args must keep serve restic --stdio, got %q", kind.RcloneArgs)
	}
	if !strings.Contains(kind.RcloneArgs, "tpslimit") || !strings.Contains(kind.RcloneArgs, "transfers 1") {
		t.Fatalf("expected Drive pacing args, got %q", kind.RcloneArgs)
	}
	opts := kind.RcloneGlobalOpts()
	joined := strings.Join(opts, " ")
	if !strings.Contains(joined, "rclone.args=") || !strings.Contains(joined, "serve restic") {
		t.Fatalf("%v", opts)
	}
}

func TestURLTemplates(t *testing.T) {
	cases := []struct {
		kind string
		dest model.Destination
		want string
	}{
		{"sftp", model.Destination{User: "backup", Host: "files.example", Path: "/repos/site"}, "sftp:backup@files.example:/repos/site"},
		{"s3", model.Destination{Bucket: "site-backups", Prefix: "a"}, "s3:s3.amazonaws.com/site-backups/a"},
		{"s3", model.Destination{Bucket: "site-backups", Endpoint: "https://s3.amazonaws.com"}, "s3:s3.amazonaws.com/site-backups"},
		{"b2", model.Destination{Bucket: "bucket", Prefix: "site"}, "b2:bucket:site"},
		{"b2", model.Destination{Bucket: "bucket"}, "b2:bucket:/"},
		{"disk", model.Destination{Path: "/var/backups/site"}, "/var/backups/site"},
		{"gdrive", model.Destination{Host: "gdrive", Path: "local-backup/site"}, "rclone:gdrive:local-backup/site"},
		{"gdrive", model.Destination{Host: "gdrive", Path: "/"}, "rclone:gdrive:/"},
	}
	for _, tc := range cases {
		kind, err := ByID(tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		got, err := kind.URL(tc.dest)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q want %q (%v)", tc.kind, got, tc.want, err)
		}
	}
}

func TestValidateRequiresFields(t *testing.T) {
	site := model.Site{Root: "/tmp/site"}
	repo := "/tmp/repo"
	gdrive, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	err = gdrive.Validate(site, repo, model.Destination{Kind: "gdrive", Path: "local-backup/site"})
	if err == nil || !strings.Contains(err.Error(), "Google Drive remote is required") {
		t.Fatalf("expected required host, got %v", err)
	}
	if err := gdrive.CheckAccess(model.Destination{Kind: "gdrive", Path: "local-backup/site"}, false); err == nil {
		t.Fatal("expected CheckAccess to require host")
	}
}

func TestValidateRejectsUnsafe(t *testing.T) {
	site := model.Site{Root: "/tmp/site"}
	repo := "/tmp/repo"
	kind, err := ByID("sftp")
	if err != nil {
		t.Fatal(err)
	}
	if err := kind.Validate(site, repo, model.Destination{Host: "files.example;id", User: "backup", Path: "/repos/site"}); err == nil {
		t.Fatal("bad host")
	}
	disk, err := ByID("disk")
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.Validate(site, repo, model.Destination{Path: "relative"}); err == nil {
		t.Fatal("relative disk")
	}
	if err := disk.Validate(site, repo, model.Destination{Path: site.Root}); err == nil {
		t.Fatal("live site disk")
	}
}

func TestCredentialBody(t *testing.T) {
	s3, err := ByID("s3")
	if err != nil {
		t.Fatal(err)
	}
	body, err := s3.CredentialBody("AKIA", "secret")
	if err != nil || !strings.Contains(body, "AWS_ACCESS_KEY_ID=AKIA") || !strings.Contains(body, "AWS_SECRET_ACCESS_KEY=secret") {
		t.Fatal(body, err)
	}
	sftp, err := ByID("sftp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftp.CredentialBody("a", "b"); err == nil {
		t.Fatal("sftp accepted secrets")
	}
}

func TestRcloneRemoteListed(t *testing.T) {
	out := "gdrive:\nother:\n"
	if !rcloneRemoteListed(out, "gdrive") {
		t.Fatal("expected gdrive")
	}
	if !rcloneRemoteListed(out, "gdrive:") {
		t.Fatal("expected gdrive with colon")
	}
	if rcloneRemoteListed(out, "missing") {
		t.Fatal("missing should be absent")
	}
	if rcloneRemoteListed("", "gdrive") {
		t.Fatal("empty list")
	}
}

func TestParseRcloneRemotes(t *testing.T) {
	got := parseRcloneRemotes("gdrive:\nother:\n")
	if len(got) != 2 || got[0] != "gdrive" || got[1] != "other" {
		t.Fatalf("%#v", got)
	}
	got = parseRcloneRemotes("gdrive: drive\n")
	if len(got) != 1 || got[0] != "gdrive" {
		t.Fatalf("%#v", got)
	}
}

func TestApplyDefaultsFillsSoleDriveRemote(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\nif [ \"$1\" = listremotes ]; then echo 'gdrive:'; exit 0; fi\necho unexpected \"$1\" >&2; exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)

	kind, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	dest := model.Destination{Kind: "gdrive", Path: "local-backup/site"}
	kind.ApplyDefaults(&dest)
	if dest.Host != "gdrive" {
		t.Fatalf("host %#v", dest.Host)
	}
}

func TestCheckAccessRejectsMissingRcloneRemote(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\nif [ \"$1\" = listremotes ]; then echo 'other:'; exit 0; fi\necho unexpected \"$1\" >&2; exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)

	kind, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	if !kind.HasHook("rclone_remote") {
		t.Fatal("gdrive missing rclone_remote hook")
	}
	err = kind.CheckAccess(model.Destination{Host: "gdrive", Path: "local-backup/site"}, false)
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

func TestCheckAccessAcceptsConfiguredRemote(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\ncase \"$1\" in\nlistremotes) echo 'gdrive:'; exit 0 ;;\nabout) exit 0 ;;\n*) echo unexpected \"$1\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)

	kind, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	dest := model.Destination{Host: "gdrive", Path: "local-backup/site"}
	if err := kind.CheckAccess(dest, false); err != nil {
		t.Fatal(err)
	}
	if err := kind.CheckAccess(dest, true); err != nil {
		t.Fatal(err)
	}
}

func TestRcloneProbeTimeoutCoversSlowDriveAbout(t *testing.T) {
	if rcloneProbeTimeout < 2*time.Minute {
		t.Fatalf("probe timeout %s is shorter than a slow Google Drive about", rcloneProbeTimeout)
	}
}

func TestCheckAccessReportsSlowRcloneAbout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\ncase \"$1\" in\nlistremotes) echo 'gdrive:'; exit 0 ;;\nabout) exec sleep 30 ;;\n*) echo unexpected \"$1\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)
	previous := rcloneProbeTimeout
	rcloneProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { rcloneProbeTimeout = previous })

	kind, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	err = kind.CheckAccess(model.Destination{Host: "gdrive", Path: "local-backup/site"}, true)
	if err == nil || !strings.Contains(err.Error(), "in time") || !strings.Contains(err.Error(), "gdrive") {
		t.Fatal(err)
	}
}

func TestSetupMissing(t *testing.T) {
	gdrive, err := ByID("gdrive")
	if err != nil {
		t.Fatal(err)
	}
	missing := gdrive.SetupMissing(SetupFields{})
	for _, field := range []string{"host", "remote-path"} {
		if !contains(missing, field) {
			t.Fatalf("missing %s in %#v", field, missing)
		}
	}
	s3, err := ByID("s3")
	if err != nil {
		t.Fatal(err)
	}
	missing = s3.SetupMissing(SetupFields{Bucket: "b"})
	for _, field := range []string{"access-key", "secret-key"} {
		if !contains(missing, field) {
			t.Fatalf("missing %s in %#v", field, missing)
		}
	}
}

func TestOverlayDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := `{
	  "id": "sftp",
	  "label": "Custom SFTP",
	  "note": "replaced",
	  "offsite": true,
	  "fields": [{"name": "host", "label": "Host", "required": true, "pattern": "hostname", "setup": "host"}],
	  "url": "sftp:{host}:/repos",
	  "progress": "Copying to {host}",
	  "detail": "{host}"
	}`
	if err := os.WriteFile(filepath.Join(destDir, "sftp.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := `{
	  "id": "memory",
	  "label": "Memory",
	  "note": "test",
	  "offsite": false,
	  "fields": [{"name": "path", "label": "Path", "required": true, "pattern": "abs_path", "setup": "remote-path"}],
	  "url": "{path}",
	  "progress": "Copying to {path}",
	  "detail": "{path}"
	}`
	if err := os.WriteFile(filepath.Join(destDir, "memory.json"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	kinds, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sftp, err := ByID("sftp")
	if err != nil || sftp.Label != "Custom SFTP" {
		t.Fatalf("%#v %v", sftp, err)
	}
	if _, err := ByID("memory"); err != nil {
		t.Fatal(err)
	}
	if len(kinds) < 6 {
		t.Fatalf("got %d kinds", len(kinds))
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
