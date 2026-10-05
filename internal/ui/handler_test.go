package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/app"
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
	registered := postForm(t, handler, "/register", url.Values{"name": {"Client Site"}, "path": {dir}})
	if registered.Code != http.StatusSeeOther || registered.Header().Get("Location") != "/sites/client-site" {
		t.Fatalf("register %d %s", registered.Code, registered.Header().Get("Location"))
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
	io.Copy(io.Discard, rec.Body)
	return rec
}
