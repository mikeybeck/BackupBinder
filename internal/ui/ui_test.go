package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"local-backup/internal/fail"
	"local-backup/internal/model"
)

func TestPageTemplateParses(t *testing.T) {
	template.Must(template.New("page").Parse(pageHTML))
}

func TestRunningBackupIsVisible(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var site bytes.Buffer
	err := tmpl.ExecuteTemplate(&site, "site", page{
		Refresh: true,
		Site:    &siteView{ID: "demo", Name: "Demo", Status: "running", When: "2026-09-30T00:00:00Z", Confidence: "never"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := site.String()
	for _, want := range []string{"Backup in progress since 2026-09-30T00:00:00Z", "Backup in progress…", `http-equiv="refresh"`, "disabled"} {
		if !strings.Contains(body, want) {
			t.Fatalf("site page missing %q", want)
		}
	}
	var home bytes.Buffer
	err = tmpl.ExecuteTemplate(&home, "home", page{
		Refresh: true,
		Cards:   []card{{ID: "demo", Name: "Demo", Adapter: "files", Status: "running", When: "2026-09-30T00:00:00Z"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(home.String(), "Backup in progress since 2026-09-30T00:00:00Z") {
		t.Fatal(home.String())
	}
}

func TestRunningBackupShowsProgress(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	progress := progressView{Phase: "Saving the archive", File: "wp-content/uploads/note.txt", Percent: 27, Known: true, Count: "10 of 10 files", Written: "27.4 MiB written", Limit: "Upload limited to 100 KiB/s"}
	var site bytes.Buffer
	err := tmpl.ExecuteTemplate(&site, "site", page{
		Refresh: true,
		Site:    &siteView{ID: "demo", Name: "Demo", Status: "running", When: "2026-09-30T00:00:00Z", Progress: progress},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := site.String()
	for _, want := range []string{`role="progressbar"`, `aria-valuenow="27"`, `style="width: 27%"`, "Saving the archive", "wp-content/uploads/note.txt", "10 of 10 files", "27.4 MiB written", "Upload limited to 100 KiB/s"} {
		if !strings.Contains(body, want) {
			t.Fatalf("site page missing %q\n%s", want, body)
		}
	}
	var home bytes.Buffer
	err = tmpl.ExecuteTemplate(&home, "home", page{
		Refresh: true,
		Cards:   []card{{ID: "demo", Name: "Demo", Status: "running", When: "2026-09-30T00:00:00Z", Progress: progressView{Phase: "Reading site files"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(home.String(), "Reading site files") || !strings.Contains(home.String(), "bar unknown") {
		t.Fatal(home.String())
	}
}

func TestBackupNoticeReportsWhatChanged(t *testing.T) {
	added := backupNotice(model.Manifest{
		Captured: model.Captured{
			Files:    model.FileCapture{FileCount: 31, ByteSize: 166131382},
			Database: &model.DBCapture{Bytes: 2048, Method: "mysqldump"},
		},
		Snapshot: model.Snapshot{RunID: "run-1", DataAdded: 12},
	})
	if added != "Backup stored locally. Run run-1. 31 files, 166131382 bytes. Database dump included (2 KiB via mysqldump). 12 of those bytes were new in the repository." {
		t.Fatal(added)
	}
	same := backupNotice(model.Manifest{
		Captured: model.Captured{Files: model.FileCapture{FileCount: 2, ByteSize: 40}},
		Snapshot: model.Snapshot{RunID: "run-2"},
	})
	if same != "Backup stored locally. Run run-2. 2 files, 40 bytes. No database dump. Those files were already in the repository." {
		t.Fatal(same)
	}
	trimmed := backupNotice(model.Manifest{
		Captured:  model.Captured{Files: model.FileCapture{FileCount: 2, ByteSize: 40}},
		Snapshot:  model.Snapshot{RunID: "run-3", DataAdded: 4},
		Retention: &model.RetentionApplied{Removed: []string{"run-1"}, Kept: 1},
	})
	if trimmed != "Backup stored locally. Run run-3. 2 files, 40 bytes. No database dump. 4 of those bytes were new in the repository. Removed 1 older snapshot." {
		t.Fatal(trimmed)
	}
}

func TestDatabaseFormShowsSettings(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{
			ID: "demo", Name: "Demo", Status: "success", Size: "100 bytes",
			Database:  "Database dump 12 KiB via sql_file",
			DBEnabled: true, DBName: "local", DBUser: "root", DBHost: "localhost",
			DBMethod: "auto", DBPasswordLabel: "present",
			DBSummary: "Database local on localhost will be dumped into each backup",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`action="/sites/demo/database"`, `name="enabled"`, "checked",
		`name="name"`, `value="local"`, `name="user"`, `value="root"`,
		`name="host"`, `value="localhost"`, `name="password"`, `type="password"`,
		"Password: present", "Database dump 12 KiB via sql_file",
		"Include a database dump in each backup",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q\n%s", want, body)
		}
	}
}

func TestSpeedLimitFormShowsSavedRates(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{ID: "demo", Name: "Demo", Status: "none", UploadKiB: "128", DownloadKiB: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{`action="/sites/demo/limits"`, `name="limit_upload"`, `value="128"`, `name="limit_download"`, `placeholder="unlimited"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestRetentionFormShowsSavedCounts(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{
			ID: "demo", Name: "Demo", Status: "none",
			KeepLast: "3", KeepHourly: "24", KeepDaily: "14", KeepWeekly: "8", KeepMonthly: "12", KeepYearly: "2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`action="/sites/demo/retention"`, `name="keep_last"`, `value="3"`,
		`name="keep_hourly"`, `value="24"`, `name="keep_daily"`, `value="14"`,
		`name="keep_weekly"`, `value="8"`, `name="keep_monthly"`, `value="12"`,
		`name="keep_yearly"`, `value="2"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDestinationFormShowsFailureReason(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{
			ID: "demo", Name: "Demo", Status: "success", Root: "/tmp/copy",
			DestWarning: "Google Drive / rclone remote 'gdrive' is not configured.",
			Destinations: []destView{{
				ID: "gdrive-1", Kind: "gdrive", Enabled: true, Status: "failed",
				Detail: "gdrive:local-backup/site",
				Reason: "Google Drive / rclone remote 'gdrive' is not configured. Run rclone config and create a Google Drive remote named 'gdrive'.",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		"last run failed", "rclone remote", "not configured", "Run rclone config",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestDestinationFormOffersGoogleDrive(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{ID: "demo", Name: "Demo", Status: "none", Root: "/tmp/copy"},
		DestKinds: []destKindView{
			{ID: "gdrive", Label: "Google Drive", Note: "uses rclone", Fields: []destFieldView{
				{Name: "host", Label: "rclone remote name", Placeholder: "gdrive"},
				{Name: "path", Label: "Folder in Google Drive", Placeholder: "local-backup/site"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`value="gdrive"`, `data-for="gdrive"`, "rclone remote name", "Folder in Google Drive",
		`input.disabled = !show`, "destinations/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestLocationFormOffersOneServer(t *testing.T) {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "site", page{
		Site: &siteView{
			ID: "demo", Name: "Demo", Status: "none", Root: "/tmp/copy",
			LocationKind: "sftp", LocationHost: "files.example", LocationUser: "backup", LocationPath: "/var/www/site",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`action="/sites/demo/location"`, `name="kind"`, `value="sftp" selected`,
		`value="files.example"`, `value="backup"`, `value="/var/www/site"`,
		`data-for="local"`, `data-for="sftp"`, `backup@files.example:/var/www/site`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestNoticeTextIncludesHint(t *testing.T) {
	got := noticeText(&fail.Error{Message: "Backup failed.", Hint: "The files are still in place."})
	if !strings.Contains(got, "Backup failed.") || !strings.Contains(got, "The files are still in place.") {
		t.Fatal(got)
	}
}
