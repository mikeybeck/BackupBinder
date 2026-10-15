package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/app"
	"local-backup/internal/model"
)

func TestHandlersSaveSiteSettings(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := Handler(application)

	home := get(t, handler, "/")
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "No sites yet") {
		t.Fatalf("home %d %s", home.Code, home.Body.String())
	}
	missing := get(t, handler, "/sites/missing")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing site %d", missing.Code)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Client Site", "", false); err != nil {
		t.Fatal(err)
	}

	page := get(t, handler, "/sites/client-site")
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Client Site") || !strings.Contains(body, dir) {
		t.Fatalf("site page %d", page.Code)
	}

	schedule := postForm(t, handler, "/sites/client-site/schedule", url.Values{"cron": {"15 3 * * *"}, "enabled": {"on"}})
	if schedule.Code != http.StatusSeeOther || !strings.Contains(schedule.Header().Get("Location"), "Schedule+saved") {
		t.Fatal(schedule.Header().Get("Location"))
	}
	if !strings.Contains(get(t, handler, "/sites/client-site").Body.String(), `value="15 3 * * *"`) || !strings.Contains(get(t, handler, "/sites/client-site").Body.String(), "checked") {
		t.Fatal("schedule was not shown")
	}

	limits := postForm(t, handler, "/sites/client-site/limits", url.Values{"limit_upload": {"64"}, "limit_download": {"32"}})
	if limits.Code != http.StatusSeeOther || !strings.Contains(limits.Header().Get("Location"), "64+KiB") {
		t.Fatal(limits.Header().Get("Location"))
	}
	limited := get(t, handler, "/sites/client-site").Body.String()
	if !strings.Contains(limited, `name="limit_upload"`) || !strings.Contains(limited, `value="64"`) || !strings.Contains(limited, `value="32"`) {
		t.Fatal("limits were not shown")
	}

	retention := postForm(t, handler, "/sites/client-site/retention", url.Values{
		"keep_last": {"0"}, "keep_hourly": {"0"}, "keep_daily": {"0"},
		"keep_weekly": {"0"}, "keep_monthly": {"0"}, "keep_yearly": {"0"},
	})
	if retention.Code != http.StatusSeeOther || !strings.Contains(retention.Header().Get("Location"), "Every+snapshot+is+kept") {
		t.Fatal(retention.Header().Get("Location"))
	}

	badHost := postForm(t, handler, "/sites/client-site/location", url.Values{
		"kind": {"sftp"}, "host": {"files.example;id"}, "user": {"backup"}, "remote_path": {"/var/www"},
	})
	if badHost.Code != http.StatusSeeOther || !strings.Contains(badHost.Header().Get("Location"), "bad=1") || !strings.Contains(badHost.Header().Get("Location"), "shell") {
		t.Fatal(badHost.Header().Get("Location"))
	}

	secret := "super-secret-key-value"
	saved := postForm(t, handler, "/sites/client-site/destinations", url.Values{
		"kind": {"s3"}, "bucket": {"site-backups"}, "access_key": {"AKIAEXAMPLE"}, "secret_key": {secret},
	})
	if saved.Code != http.StatusSeeOther || strings.Contains(saved.Header().Get("Location"), "bad=1") {
		t.Fatal(saved.Header().Get("Location"))
	}
	shown := get(t, handler, "/sites/client-site").Body.String()
	if strings.Contains(shown, secret) || strings.Contains(shown, "AKIAEXAMPLE") {
		t.Fatal("secret was rendered")
	}
	if !strings.Contains(shown, "credentials present") {
		t.Fatal(shown)
	}

	dbPassword := "db-secret-value"
	database := postForm(t, handler, "/sites/client-site/database", url.Values{
		"enabled": {"on"}, "name": {"app"}, "user": {"backup"}, "host": {"localhost"}, "password": {dbPassword},
	})
	if database.Code != http.StatusSeeOther || strings.Contains(database.Header().Get("Location"), "bad=1") {
		t.Fatal(database.Header().Get("Location"))
	}
	dbPage := get(t, handler, "/sites/client-site").Body.String()
	if strings.Contains(dbPage, dbPassword) {
		t.Fatal("database password was rendered")
	}
	if !strings.Contains(dbPage, "Password: present") || !strings.Contains(dbPage, `value="app"`) {
		t.Fatal(dbPage)
	}
	off := postForm(t, handler, "/sites/client-site/database", url.Values{})
	if off.Code != http.StatusSeeOther || !strings.Contains(off.Header().Get("Location"), "turned+off") {
		t.Fatal(off.Header().Get("Location"))
	}
	if strings.Contains(get(t, handler, "/sites/client-site").Body.String(), "Password: present") {
		t.Fatal("database dump still enabled")
	}
}

func TestDestinationRemoveAndDisable(t *testing.T) {
	fake := t.TempDir()
	script := filepath.Join(fake, "rclone")
	body := "#!/bin/sh\ncase \"$1\" in\nlistremotes) echo 'gdrive:'; exit 0 ;;\nabout) exit 0 ;;\n*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)

	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := Handler(application)

	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(siteDir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	dest, err := application.AddDestination("notes", model.Destination{
		Kind: "gdrive", Host: "gdrive", Path: "local-backup/notes", Enabled: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}

	page := get(t, handler, "/sites/notes").Body.String()
	if !strings.Contains(page, "/destinations/"+dest.ID+"/enabled") || !strings.Contains(page, "/destinations/"+dest.ID+"/remove") {
		t.Fatal("manage controls missing")
	}
	if strings.Contains(page, "/destinations/local/remove") {
		t.Fatal("local restic should not be removable")
	}

	disabled := postForm(t, handler, "/sites/notes/destinations/"+dest.ID+"/enabled", url.Values{})
	if disabled.Code != http.StatusSeeOther || !strings.Contains(disabled.Header().Get("Location"), "saved+only") {
		t.Fatal(disabled.Header().Get("Location"))
	}
	removed := postForm(t, handler, "/sites/notes/destinations/"+dest.ID+"/remove", url.Values{})
	if removed.Code != http.StatusSeeOther || !strings.Contains(removed.Header().Get("Location"), "removed") {
		t.Fatal(removed.Header().Get("Location"))
	}
	after := get(t, handler, "/sites/notes").Body.String()
	if strings.Contains(after, dest.ID) {
		t.Fatal("destination still listed")
	}
}

func TestDestinationSaveKeepsFieldsOnError(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\nif [ \"$1\" = listremotes ]; then echo 'other:'; exit 0; fi\necho unexpected \"$1\" >&2; exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_BACKUP_RCLONE", script)

	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := Handler(application)

	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(siteDir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}

	resp := postForm(t, handler, "/sites/notes/destinations", url.Values{
		"kind": {"gdrive"}, "host": {"gdrive"}, "path": {"local-backup/notes"}, "enabled": {"on"},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected re-render %d location %s", resp.Code, resp.Header().Get("Location"))
	}
	page := resp.Body.String()
	if !strings.Contains(page, "Destination was not saved") || !strings.Contains(page, "not configured") {
		t.Fatal(page)
	}
	if strings.Contains(page, "local snapshot is already stored") {
		t.Fatal(page)
	}
	if !strings.Contains(page, `id="dest-gdrive-host" name="host" value="gdrive"`) {
		t.Fatal("host field was cleared")
	}
	if !strings.Contains(page, `id="dest-gdrive-path" name="path" value="local-backup/notes"`) {
		t.Fatal("path field was cleared")
	}
	if !strings.Contains(page, `value="gdrive" selected`) {
		t.Fatal("gdrive kind was not kept selected")
	}
	if !strings.Contains(page, `name="enabled" checked`) {
		t.Fatal("enabled checkbox was cleared")
	}
	if !strings.Contains(page, `id="destinations"`) || !strings.Contains(page, `getElementById("destinations")`) {
		t.Fatal("focus target missing")
	}
}

func TestWizardLocationHasEveryField(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := Handler(application)

	form := get(t, handler, "/setup")
	body := form.Body.String()
	if form.Code != http.StatusOK {
		t.Fatalf("setup %d", form.Code)
	}
	for _, want := range []string{`name="local_path"`, `name="host"`, `name="user"`, `name="remote_path"`, "Set up a backup"} {
		if !strings.Contains(body, want) {
			t.Fatalf("location form missing %q", want)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := postForm(t, handler, "/setup", url.Values{"kind": {"local"}, "local_path": {dir}})
	if created.Code != http.StatusSeeOther || !strings.HasSuffix(created.Header().Get("Location"), "/setup") {
		t.Fatalf("create %d %s", created.Code, created.Header().Get("Location"))
	}
	namePage := get(t, handler, created.Header().Get("Location"))
	if namePage.Code != http.StatusOK || !strings.Contains(namePage.Body.String(), `name="name"`) || !strings.Contains(namePage.Body.String(), "Back") {
		t.Fatalf("name step %d %s", namePage.Code, namePage.Body.String())
	}
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postForm(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
