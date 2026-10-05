// Package ui is the localhost site list. It calls the same backup path as the CLI.
package ui

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"local-backup/internal/app"
	"local-backup/internal/fail"
	"local-backup/internal/model"
)

// Handler serves the local UI. Bind it to 127.0.0.1.
func Handler(application *app.App) http.Handler {
	mux := http.NewServeMux()
	ui := &server{app: application, tmpl: template.Must(template.New("page").Parse(pageHTML))}
	mux.HandleFunc("GET /", ui.home)
	mux.HandleFunc("GET /register", ui.registerForm)
	mux.HandleFunc("POST /register", ui.register)
	mux.HandleFunc("GET /sites/{id}", ui.site)
	mux.HandleFunc("POST /sites/{id}/backup", ui.backup)
	mux.HandleFunc("POST /sites/{id}/destinations", ui.destination)
	mux.HandleFunc("POST /sites/{id}/test-restore", ui.testRestore)
	mux.HandleFunc("POST /sites/{id}/schedule", ui.schedule)
	mux.HandleFunc("POST /sites/{id}/limits", ui.limits)
	mux.HandleFunc("POST /sites/{id}/retention", ui.retention)
	mux.HandleFunc("POST /sites/{id}/location", ui.location)
	mux.HandleFunc("POST /sites/{id}/adapter", ui.adapter)
	mux.HandleFunc("POST /sites/{id}/database", ui.database)
	return mux
}

type server struct {
	app  *app.App
	tmpl *template.Template
}

type card struct {
	ID         string
	Name       string
	Adapter    string
	Status     string
	When       string
	Size       string
	Database   string
	Confidence string
	Error      string
	Hint       string
	Progress   progressView
}

type page struct {
	Title     string
	Empty     bool
	Cards     []card
	Site      *siteView
	Notice    string
	NoticeBad bool
	Refresh   bool
	FormErr   string
	Tools     string
}

type siteView struct {
	ID              string
	Name            string
	Adapter         string
	Root            string
	Status          string
	When            string
	Size            string
	Database        string
	Confidence      string
	Error           string
	Hint            string
	Steps           []string
	Sources         []string
	Destinations    []destView
	Cron            string
	CronOn          bool
	UploadKiB       string
	DownloadKiB     string
	KeepLast        string
	KeepHourly      string
	KeepDaily       string
	KeepWeekly      string
	KeepMonthly     string
	KeepYearly      string
	LocationKind    string
	LocationHost    string
	LocationUser    string
	LocationPath    string
	Progress        progressView
	DBEnabled       bool
	DBName          string
	DBUser          string
	DBHost          string
	DBMethod        string
	DBPasswordLabel string
	DBSummary       string
}

type progressView struct {
	Phase   string
	File    string
	Percent int
	Known   bool
	Count   string
	Written string
	Limit   string
}

type destView struct {
	ID      string
	Kind    string
	Enabled bool
	Status  string
	Detail  string
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	sites, err := s.app.DB.ListSites()
	if err != nil {
		s.fail(w, err)
		return
	}
	view := page{Title: "Sites", Empty: len(sites) == 0, Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1", Tools: s.app.Restic.Binary}
	for _, site := range sites {
		item := s.card(site)
		if item.Status == "running" {
			view.Refresh = true
		}
		view.Cards = append(view.Cards, item)
	}
	s.render(w, "home", view)
}

func (s *server) card(site model.Site) card {
	item := card{ID: site.ID, Name: site.Name, Adapter: site.Adapter, Status: "none", Confidence: "never"}
	if model.DatabaseSource(site) != nil {
		item.Database = "Database dump planned"
	} else {
		item.Database = "Files only"
	}
	run, err := s.app.Latest(site.ID)
	if err != nil || run.RunID == "" {
		return item
	}
	item.When = run.StartedAt
	if run.InProgress() {
		item.Status = "running"
		item.Progress = s.progressView(site.ID, run.RunID)
		item.Progress.Limit = uploadLimitNote(site.Limits.UploadKiB)
		return item
	}
	if run.OK && run.Manifest != nil {
		item.Status = "success"
		item.Size = strconv.FormatInt(run.Manifest.SizeBytes, 10) + " bytes"
		item.Database = databaseCardNote(*run.Manifest)
		item.Confidence = run.Manifest.Restore.Confidence
		if item.Confidence == "" {
			item.Confidence = "never"
		}
		return item
	}
	item.Status = "failed"
	if run.Failure != nil {
		item.Error = run.Failure.Message
		item.Hint = run.Failure.Hint
	}
	return item
}

func (s *server) registerForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "register", page{Title: "Register a site", Tools: s.app.Restic.Binary})
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	result, err := s.app.Register(strings.TrimSpace(r.FormValue("path")), strings.TrimSpace(r.FormValue("name")), "", false)
	if err != nil {
		s.render(w, "register", page{Title: "Register a site", FormErr: err.Error(), Tools: s.app.Restic.Binary})
		return
	}
	http.Redirect(w, r, "/sites/"+result.Site.ID, http.StatusSeeOther)
}

func (s *server) site(w http.ResponseWriter, r *http.Request) {
	view, ok := s.siteView(w, r)
	if !ok {
		return
	}
	s.render(w, "site", page{
		Title: view.Name, Site: view, Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1",
		Refresh: view.Status == "running", Tools: s.app.Restic.Binary,
	})
}

func (s *server) siteView(w http.ResponseWriter, r *http.Request) (*siteView, bool) {
	id := r.PathValue("id")
	inspected, err := s.app.Inspect(id)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	view := &siteView{
		ID: inspected.Site.ID, Name: inspected.Site.Name, Adapter: inspected.Site.Adapter,
		Root: inspected.Site.Root, Status: "none", Confidence: "never",
		Cron: inspected.Site.Schedule.Cron, CronOn: inspected.Site.Schedule.Enabled,
		UploadKiB: formatKiB(inspected.Site.Limits.UploadKiB), DownloadKiB: formatKiB(inspected.Site.Limits.DownloadKiB),
		KeepLast: formatCount(inspected.Site.Retention.KeepLast), KeepHourly: formatCount(inspected.Site.Retention.KeepHourly),
		KeepDaily: formatCount(inspected.Site.Retention.KeepDaily), KeepWeekly: formatCount(inspected.Site.Retention.KeepWeekly),
		KeepMonthly: formatCount(inspected.Site.Retention.KeepMonthly), KeepYearly: formatCount(inspected.Site.Retention.KeepYearly),
		LocationKind: inspected.Site.Location.Kind, LocationHost: inspected.Site.Location.Host,
		LocationUser: inspected.Site.Location.User, LocationPath: inspected.Site.Location.Path,
	}
	if view.LocationKind == "" {
		view.LocationKind = "local"
	}
	for _, source := range inspected.Site.Sources {
		if source.Kind == "files" {
			if view.LocationKind == "sftp" {
				view.Sources = append(view.Sources, "Files on "+view.LocationUser+"@"+view.LocationHost+":"+view.LocationPath+" (copied to "+inspected.Site.Root+" before each backup; cache, node_modules, and similar junk skipped)")
			} else {
				view.Sources = append(view.Sources, "Files at "+inspected.Site.Root+" (cache, node_modules, and similar junk skipped)")
			}
		}
	}
	if db := model.DatabaseSource(inspected.Site); db != nil {
		view.DBEnabled = true
		view.DBName = db.Name
		view.DBUser = db.User
		view.DBHost = db.Host
		view.DBMethod = db.Method
		view.DBPasswordLabel = s.app.DatabasePasswordLabel(inspected.Site)
		view.DBSummary = databasePlanSummary(inspected.Site, *db)
		view.Sources = append(view.Sources, view.DBSummary)
	} else {
		view.DBSummary = "No database dump. Backups store files only."
		view.Sources = append(view.Sources, view.DBSummary)
	}
	run, err := s.app.Latest(id)
	if err == nil && run.RunID != "" {
		view.When = run.StartedAt
		if run.InProgress() {
			view.Status = "running"
			view.Progress = s.progressView(id, run.RunID)
			view.Progress.Limit = uploadLimitNote(inspected.Site.Limits.UploadKiB)
		} else if run.OK && run.Manifest != nil {
			view.Status = "success"
			view.Size = strconv.FormatInt(run.Manifest.SizeBytes, 10) + " bytes"
			view.Database = databaseCardNote(*run.Manifest)
			view.Confidence = run.Manifest.Restore.Confidence
			view.Steps = run.Manifest.Restore.Steps
		} else {
			view.Status = "failed"
			if run.Failure != nil {
				view.Error = run.Failure.Message
				view.Hint = run.Failure.Hint
			}
		}
	}
	statusByID := map[string]string{}
	if run.Manifest != nil {
		for _, dest := range run.Manifest.Destinations {
			statusByID[dest.ID] = dest.Status
		}
	}
	for _, dest := range inspected.Site.Destinations {
		detail := dest.Kind
		if dest.Host != "" {
			detail = dest.User + "@" + dest.Host + dest.Path
		} else if dest.Bucket != "" {
			detail = dest.Bucket
		} else if dest.Path != "" {
			detail = dest.Path
		}
		if dest.Credentials != "" {
			detail += " (credentials " + dest.Credentials + ")"
		}
		view.Destinations = append(view.Destinations, destView{
			ID: dest.ID, Kind: dest.Kind, Enabled: dest.Enabled, Status: statusByID[dest.ID], Detail: detail,
		})
	}
	return view, true
}

func (s *server) progressView(siteID, runID string) progressView {
	p, ok := s.app.ReadProgress(siteID, runID)
	if !ok {
		return progressView{Phase: "Starting the backup"}
	}
	return viewProgress(p)
}

func viewProgress(p model.Progress) progressView {
	view := progressView{Phase: p.Phase, File: p.File, Known: p.PercentKnown}
	if p.PercentKnown {
		pct := int(p.Percent + 0.5)
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		view.Percent = pct
	}
	if p.FilesTotal > 0 {
		view.Count = strconv.FormatInt(p.FilesDone, 10) + " of " + strconv.FormatInt(p.FilesTotal, 10) + " files"
	}
	if p.BytesWritten > 0 {
		view.Written = formatBytes(p.BytesWritten) + " written"
	}
	return view
}

func uploadLimitNote(kib int) string {
	if kib <= 0 {
		return ""
	}
	return "Upload limited to " + strconv.Itoa(kib) + " KiB/s"
}

func formatBytes(n int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.0f KiB", float64(n)/float64(kib))
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

type backupResult struct {
	manifest model.Manifest
	err      error
}

func (s *server) backup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	outcome := make(chan backupResult, 1)
	go func() {
		manifest, err := s.app.CreateBackup(id)
		outcome <- backupResult{manifest: manifest, err: err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case result := <-outcome:
			if result.err != nil {
				s.redirectNotice(w, r, "/sites/"+id, "Backup failed. "+noticeText(result.err), true)
				return
			}
			s.redirectNotice(w, r, "/sites/"+id, backupNotice(result.manifest), false)
			return
		default:
		}
		run, err := s.app.Latest(id)
		if err == nil && run.InProgress() {
			http.Redirect(w, r, "/sites/"+id, http.StatusSeeOther)
			return
		}
		if time.Now().After(deadline) {
			http.Redirect(w, r, "/sites/"+id, http.StatusSeeOther)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func backupNotice(manifest model.Manifest) string {
	files := manifest.Captured.Files
	text := "Backup stored locally. Run " + manifest.Snapshot.RunID + ". " +
		strconv.Itoa(files.FileCount) + " files, " + strconv.FormatInt(files.ByteSize, 10) + " bytes."
	if manifest.Captured.Database != nil {
		db := manifest.Captured.Database
		text += " Database dump included (" + formatBytes(db.Bytes)
		if db.Method != "" {
			text += " via " + db.Method
		}
		text += ")."
	} else {
		text += " No database dump."
	}
	if manifest.Snapshot.DataAdded == 0 {
		text += " Those files were already in the repository."
	} else {
		text += " " + strconv.FormatInt(manifest.Snapshot.DataAdded, 10) + " of those bytes were new in the repository."
	}
	if manifest.Retention != nil && len(manifest.Retention.Removed) > 0 {
		text += " " + removedPhrase(len(manifest.Retention.Removed))
	}
	return text
}

func databaseCardNote(manifest model.Manifest) string {
	if manifest.Captured.Database == nil {
		return "No database dump"
	}
	db := manifest.Captured.Database
	note := "Database dump " + formatBytes(db.Bytes)
	if db.Method != "" {
		note += " via " + db.Method
	}
	return note
}

func databasePlanSummary(site model.Site, source model.Source) string {
	switch {
	case source.Method == "sql_file":
		return "Database file " + source.SQLFile + " will be copied into each backup"
	case site.Location.Kind == "sftp":
		return "Database " + source.Name + " will be dumped on " + site.Location.User + "@" + site.Location.Host + " and copied into the archive"
	default:
		label := source.Name
		if source.Host != "" {
			label += " on " + source.Host
		}
		if source.Method != "" && source.Method != "auto" {
			return "Database " + label + " via " + source.Method
		}
		return "Database " + label + " will be dumped into each backup"
	}
}

func removedPhrase(n int) string {
	if n == 1 {
		return "Removed 1 older snapshot."
	}
	return "Removed " + strconv.Itoa(n) + " older snapshots."
}

func (s *server) testRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.app.TestRestore(id, "", ""); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Restore check failed. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Restore check passed. The scratch copy matched this backup.", false)
}

func (s *server) destination(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	dest := model.Destination{
		Kind:     r.FormValue("kind"),
		Enabled:  r.FormValue("enabled") == "on",
		Host:     strings.TrimSpace(r.FormValue("host")),
		User:     strings.TrimSpace(r.FormValue("user")),
		Path:     strings.TrimSpace(r.FormValue("path")),
		Bucket:   strings.TrimSpace(r.FormValue("bucket")),
		Prefix:   strings.TrimSpace(r.FormValue("prefix")),
		Endpoint: strings.TrimSpace(r.FormValue("endpoint")),
	}
	if _, err := s.app.AddDestination(id, dest, r.FormValue("access_key"), r.FormValue("secret_key")); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Destination was not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Destination saved. It is not contacted until you enable it.", false)
}

func (s *server) schedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	if _, err := s.app.SetSchedule(id, strings.TrimSpace(r.FormValue("cron")), r.FormValue("enabled") == "on"); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Schedule was not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Schedule saved.", false)
}

func (s *server) adapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	saved, err := s.app.SetAdapter(id, "")
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Site type was not saved. "+noticeText(err), true)
		return
	}
	notice := "Site type is " + saved.Adapter + "."
	if db := model.DatabaseSource(saved); db != nil {
		if saved.Location.Kind == "sftp" && db.Method != "sql_file" {
			notice = "Site type is " + saved.Adapter + ". The next backup dumps the database on " + saved.Location.User + "@" + saved.Location.Host + "."
		} else {
			notice = "Site type is " + saved.Adapter + ". Database dump is enabled."
		}
	} else {
		notice = "Site type is " + saved.Adapter + ". Backups are files only."
	}
	s.redirectNotice(w, r, "/sites/"+id, notice, false)
}

func (s *server) database(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	saved, err := s.app.SetDatabase(id, app.DatabaseInput{
		Enabled:      r.FormValue("enabled") == "on",
		Name:         strings.TrimSpace(r.FormValue("name")),
		User:         strings.TrimSpace(r.FormValue("user")),
		Host:         strings.TrimSpace(r.FormValue("host")),
		Password:     r.FormValue("password"),
		PreferConfig: r.FormValue("prefer_config") == "on",
	})
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Database settings were not saved. "+noticeText(err), true)
		return
	}
	if model.DatabaseSource(saved) == nil {
		s.redirectNotice(w, r, "/sites/"+id, "Database dump turned off. The next backup stores files only.", false)
		return
	}
	notice := "Database dump enabled."
	if label := s.app.DatabasePasswordLabel(saved); label == "present" {
		notice += " Password is saved on this machine."
	} else if strings.HasPrefix(label, "from ") {
		notice += " Password will be read from " + strings.TrimPrefix(label, "from ") + "."
	} else if label == "missing" {
		notice += " Password is missing; enter it before the next backup."
	}
	s.redirectNotice(w, r, "/sites/"+id, notice, false)
}

func (s *server) location(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	kind := r.FormValue("kind")
	loc := model.Location{Kind: kind}
	if kind == "sftp" {
		loc.Host = strings.TrimSpace(r.FormValue("host"))
		loc.User = strings.TrimSpace(r.FormValue("user"))
		loc.Path = strings.TrimSpace(r.FormValue("remote_path"))
	} else {
		loc.Kind = "local"
		loc.Path = strings.TrimSpace(r.FormValue("local_path"))
	}
	saved, err := s.app.SetLocation(id, loc)
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Location was not saved. "+noticeText(err), true)
		return
	}
	if saved.Location.Kind == "sftp" {
		s.redirectNotice(w, r, "/sites/"+id, "Location saved. The next backup copies files from "+saved.Location.User+"@"+saved.Location.Host+":"+saved.Location.Path+".", false)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Location saved. Files are read from "+saved.Root+".", false)
}

func (s *server) limits(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	upload, _, err := app.ParseKiB(r.FormValue("limit_upload"))
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Speed limits were not saved. "+noticeText(err), true)
		return
	}
	download, _, err := app.ParseKiB(r.FormValue("limit_download"))
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Speed limits were not saved. "+noticeText(err), true)
		return
	}
	if _, err := s.app.SetLimits(id, &upload, &download); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Speed limits were not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Speed limits saved. Upload is "+limitPhrase(upload)+". Download is "+limitPhrase(download)+".", false)
}

func (s *server) retention(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	policy := model.Retention{}
	fields := []*int{&policy.KeepLast, &policy.KeepHourly, &policy.KeepDaily, &policy.KeepWeekly, &policy.KeepMonthly, &policy.KeepYearly}
	names := []string{"keep_last", "keep_hourly", "keep_daily", "keep_weekly", "keep_monthly", "keep_yearly"}
	for i, name := range names {
		n, err := countOrZero(r.FormValue(name))
		if err != nil {
			s.redirectNotice(w, r, "/sites/"+id, "Retention was not saved. "+noticeText(err), true)
			return
		}
		*fields[i] = n
	}
	report, err := s.app.SetRetention(id, policy)
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Retention was not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, retentionNotice(report), len(report.Warnings) > 0)
}

func countOrZero(raw string) (int, error) {
	n, set, err := app.ParseCount(raw)
	if err != nil || !set {
		return n, err
	}
	return n, nil
}

func formatCount(n int) string {
	if n < 0 {
		return "0"
	}
	return strconv.Itoa(n)
}

func retentionNotice(report app.RetentionReport) string {
	if !report.Retention.Active() {
		return "Retention saved. Every snapshot is kept."
	}
	if report.Note != "" && !report.Applied {
		return "Retention saved. " + report.Note
	}
	text := "Retention saved."
	switch n := len(report.Removed); n {
	case 0:
		text += " No older snapshots were removed."
	default:
		text += " " + removedPhrase(n)
	}
	if len(report.Warnings) > 0 {
		text += " " + report.Warnings[0]
	}
	return text
}

func formatKiB(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func limitPhrase(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return strconv.Itoa(n) + " KiB/s"
}

func (s *server) redirectNotice(w http.ResponseWriter, r *http.Request, path, notice string, bad bool) {
	q := url.Values{}
	q.Set("notice", notice)
	if bad {
		q.Set("bad", "1")
	}
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}

func noticeText(err error) string {
	if fe, ok := err.(*fail.Error); ok {
		if fe.Hint != "" {
			return fe.Message + " " + fe.Hint
		}
		return fe.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *server) render(w http.ResponseWriter, name string, view page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	view.Title = name
	if err := s.tmpl.ExecuteTemplate(w, name, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) fail(w http.ResponseWriter, err error) {
	message := err.Error()
	if fe, ok := err.(*fail.Error); ok {
		message = fe.Message
	}
	http.Error(w, message, http.StatusBadRequest)
}

const pageHTML = `
{{define "home"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="2">{{end}}
<title>Local backup</title>
<style>
  body { margin: 0; background: #f3efe6; color: #1c1915; font: 16px/1.45 system-ui, sans-serif; }
  main { max-width: 40rem; margin: 0 auto; padding: 1rem; }
  h1 { font-size: 1.6rem; margin: 0 0 0.25rem; }
  p, li { overflow-wrap: anywhere; }
  .card { background: #fffdf8; border: 1px solid #d9d2c5; border-radius: 8px; padding: 1rem; margin: 0.75rem 0; }
  label, input, select, button, textarea { display: block; width: 100%; box-sizing: border-box; margin: 0.35rem 0; font: inherit; }
  button, .button { background: #0f5c57; color: white; border: 0; border-radius: 6px; padding: 0.65rem 0.8rem; text-align: center; text-decoration: none; }
  .secondary { background: transparent; color: #0f5c57; border: 1px solid #0f5c57; }
  .ok { color: #1f6b3a; }
  .bad { color: #8c2f2f; }
  .muted { color: #5c564c; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #f6f1e7; padding: 0.75rem; }
  .check { display: flex; align-items: center; gap: 0.5rem; }
  .check input { width: auto; }
  .bar { height: 0.65rem; background: #e7e1d6; border-radius: 999px; overflow: hidden; margin: 0.4rem 0; }
  .bar > span { display: block; height: 100%; background: #0f5c57; }
  .bar.unknown > span { width: 35%; animation: indeterminate 1.1s ease-in-out infinite; }
  @keyframes indeterminate { 0% { margin-left: -35%; } 100% { margin-left: 100%; } }
</style>
</head>
<body>
<main>
<h1>Local backup</h1>
<p class="muted">Backups stay on this machine unless you enable another destination. WordPress ships as an addon. Each archive is site files, and optionally a database dump.</p>
{{if .Notice}}<p class="notice{{if .NoticeBad}} bad{{end}}" role="status">{{.Notice}}</p>{{end}}
{{if .Empty}}
  <section class="card">
    <h2>No sites yet</h2>
    <p>Register a directory on this computer. A matching addon, including WordPress, is applied automatically. Anything else is backed up as files. Nothing is uploaded.</p>
    <a class="button" href="/register">Register a site</a>
  </section>
{{else}}
  <p><a class="button" href="/register">Register a site</a></p>
  {{range .Cards}}
  <article class="card">
    <h2><a href="/sites/{{.ID}}">{{.Name}}</a></h2>
    <p class="muted">{{.Adapter}} · {{.ID}}</p>
    {{if eq .Status "running"}}
      <p class="notice" role="status">Backup in progress since {{.When}}.</p>
      {{template "progress" .Progress}}
    {{else if eq .Status "success"}}
      <p class="ok">Last backup succeeded</p>
      <p>{{.When}} · {{.Size}}</p>
      <p>{{.Database}}</p>
      <p>Restore confidence: {{.Confidence}}</p>
    {{else if eq .Status "failed"}}
      <p class="bad">Last backup failed</p>
      <p>{{.Error}}</p>
      {{if .Hint}}<p>{{.Hint}}</p>{{end}}
    {{else}}
      <p>No backup yet.</p>
      <p class="muted">{{.Database}}</p>
    {{end}}
  </article>
  {{end}}
{{end}}
<p class="muted">restic: {{.Tools}}</p>
</main>
{{template "feedback" .}}
</body>
</html>
{{end}}

{{define "register"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Register a site</title>
<style>
  body { margin: 0; background: #f3efe6; color: #1c1915; font: 16px/1.45 system-ui, sans-serif; }
  main { max-width: 40rem; margin: 0 auto; padding: 1rem; }
  .card { background: #fffdf8; border: 1px solid #d9d2c5; border-radius: 8px; padding: 1rem; margin: 0.75rem 0; }
  label, input, button { display: block; width: 100%; box-sizing: border-box; margin: 0.35rem 0; font: inherit; }
  button { background: #0f5c57; color: white; border: 0; border-radius: 6px; padding: 0.65rem 0.8rem; }
  .bad { color: #8c2f2f; }
</style>
</head>
<body>
<main>
<p><a href="/">Sites</a></p>
<h1>Register a site</h1>
{{if .FormErr}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
<form class="card" method="post" action="/register">
  <label for="name">Name</label>
  <input id="name" name="name" required placeholder="Client site">
  <label for="path">Directory on this machine</label>
  <input id="path" name="path" required placeholder="/home/you/sites/example">
  <button type="submit" data-pending="Registering…">Register</button>
</form>
</main>
{{template "feedback" .}}
</body>
</html>
{{end}}

{{define "site"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="2">{{end}}
<title>{{.Site.Name}}</title>
<style>
  body { margin: 0; background: #f3efe6; color: #1c1915; font: 16px/1.45 system-ui, sans-serif; }
  main { max-width: 40rem; margin: 0 auto; padding: 1rem; }
  h1 { font-size: 1.5rem; overflow-wrap: anywhere; }
  p, li, code { overflow-wrap: anywhere; }
  .card { background: #fffdf8; border: 1px solid #d9d2c5; border-radius: 8px; padding: 1rem; margin: 0.75rem 0; }
  label, input, select, button { display: block; width: 100%; box-sizing: border-box; margin: 0.35rem 0; font: inherit; }
  button { background: #0f5c57; color: white; border: 0; border-radius: 6px; padding: 0.65rem 0.8rem; }
  .secondary { background: transparent; color: #0f5c57; border: 1px solid #0f5c57; }
  .ok { color: #1f6b3a; }
  .bad { color: #8c2f2f; }
  .muted { color: #5c564c; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #f6f1e7; padding: 0.75rem; }
  .check { display: flex; align-items: center; gap: 0.5rem; }
  .check input { width: auto; }
  .bar { height: 0.65rem; background: #e7e1d6; border-radius: 999px; overflow: hidden; margin: 0.4rem 0; }
  .bar > span { display: block; height: 100%; background: #0f5c57; }
  .bar.unknown > span { width: 35%; animation: indeterminate 1.1s ease-in-out infinite; }
  @keyframes indeterminate { 0% { margin-left: -35%; } 100% { margin-left: 100%; } }
</style>
</head>
<body>
<main>
<p><a href="/">Sites</a></p>
<h1>{{.Site.Name}}</h1>
{{if eq .Site.LocationKind "sftp"}}
<p class="muted">{{.Site.Adapter}} · {{.Site.LocationUser}}@{{.Site.LocationHost}}:{{.Site.LocationPath}}</p>
<p class="muted">Local copy: {{.Site.Root}}</p>
{{else}}
<p class="muted">{{.Site.Adapter}} · {{.Site.Root}}</p>
{{end}}
{{if .Notice}}<p class="notice{{if .NoticeBad}} bad{{end}}" role="status">{{.Notice}}</p>{{end}}
{{if eq .Site.Status "running"}}<p class="notice" role="status">Backup in progress since {{.Site.When}}. This page refreshes until it finishes.</p>{{end}}
<section class="card">
  <h2>Where the files are</h2>
  <p class="muted">A site is read from one place. Choose a directory on this machine, or one server. Saving replaces the previous choice. SSH uses the keys already on this machine.</p>
  <form method="post" action="/sites/{{.Site.ID}}/location">
    <label for="location_kind">Location</label>
    <select id="location_kind" name="kind" data-switch>
      <option value="local" {{if ne .Site.LocationKind "sftp"}}selected{{end}}>Directory on this machine</option>
      <option value="sftp" {{if eq .Site.LocationKind "sftp"}}selected{{end}}>One server (SFTP)</option>
    </select>
    <div data-for="local">
      <label for="local_path">Directory</label>
      <input id="local_path" name="local_path" value="{{.Site.Root}}">
    </div>
    <div data-for="sftp">
      <label for="remote_host">SSH host</label>
      <input id="remote_host" name="host" value="{{.Site.LocationHost}}">
      <label for="remote_user">User</label>
      <input id="remote_user" name="user" value="{{.Site.LocationUser}}">
      <label for="remote_path">Absolute path on the server</label>
      <input id="remote_path" name="remote_path" value="{{.Site.LocationPath}}" placeholder="/var/www/example">
    </div>
    <button type="submit" data-pending="Saving location…">Save location</button>
  </form>
</section>
<section class="card">
  {{if eq .Site.Status "running"}}
    <p>Started {{.Site.When}}.</p>
    {{template "progress" .Site.Progress}}
  {{else if eq .Site.Status "success"}}
    <p class="ok">Last backup succeeded</p>
    <p>{{.Site.When}}</p>
    <p>{{.Site.Size}}</p>
    <p>{{.Site.Database}}</p>
    <p>Restore confidence: {{.Site.Confidence}}</p>
  {{else if eq .Site.Status "failed"}}
    <p class="bad">Last backup failed</p>
    <p>{{.Site.Error}}</p>
    {{if .Site.Hint}}<p>{{.Site.Hint}}</p>{{end}}
    <p class="muted">The live site was not modified.</p>
  {{else}}
    <p>No backup yet. The first run stays in the local restic repository.</p>
  {{end}}
  <form method="post" action="/sites/{{.Site.ID}}/backup">
    <button type="submit" data-pending="Backup in progress…" {{if eq .Site.Status "running"}}disabled{{end}}>{{if eq .Site.Status "running"}}Backup in progress…{{else}}Back up now{{end}}</button>
  </form>
  <form method="post" action="/sites/{{.Site.ID}}/test-restore">
    <button class="secondary" type="submit" data-pending="Testing restore…">Test restore into scratch</button>
  </form>
</section>
<section class="card">
  <h2>Database</h2>
  <p class="muted">{{.Site.DBSummary}}</p>
  {{if .Site.DBEnabled}}
    {{if .Site.DBPasswordLabel}}
      <p>Password: {{.Site.DBPasswordLabel}}</p>
    {{else if eq .Site.DBMethod "sql_file"}}
      <p class="muted">This site uses a SQL file, so a database password is not needed.</p>
    {{else}}
      <p class="bad">Password: missing</p>
    {{end}}
  {{end}}
  <form method="post" action="/sites/{{.Site.ID}}/database">
    <label class="check"><input type="checkbox" name="enabled" {{if .Site.DBEnabled}}checked{{end}} data-switch-check> Include a database dump in each backup</label>
    <div data-when-checked>
      <label for="db_name">Database name</label>
      <input id="db_name" name="name" value="{{.Site.DBName}}" autocomplete="off">
      <label for="db_user">Database user</label>
      <input id="db_user" name="user" value="{{.Site.DBUser}}" autocomplete="off">
      <label for="db_host">Database host</label>
      <input id="db_host" name="host" value="{{.Site.DBHost}}" placeholder="localhost" autocomplete="off">
      {{if ne .Site.DBMethod "sql_file"}}
      <label for="db_password">Database password</label>
      <input id="db_password" name="password" type="password" autocomplete="new-password" placeholder="Leave blank to keep the current password">
      <label class="check"><input type="checkbox" name="prefer_config"> Use password from the site config file instead of a saved password</label>
      <p class="muted">The password is stored under this tool's secrets directory on this machine. It is never shown again and is never stored in the site registry.</p>
      {{end}}
    </div>
    <button type="submit" data-pending="Saving database settings…">Save database settings</button>
  </form>
</section>
<section class="card">
  <h2>Sources</h2>
  <ul>{{range .Site.Sources}}<li>{{.}}</li>{{end}}</ul>
  <form method="post" action="/sites/{{.Site.ID}}/adapter">
    <p class="muted">Read the files again and save the matching addon. A server site dumps its database on that server unless you turn the dump off above.</p>
    <button class="secondary" type="submit" data-pending="Detecting…">Detect site type again</button>
  </form>
</section>
<section class="card">
  <h2>Destinations</h2>
  <p class="muted">The local repository is always written. A saved SFTP, S3, Backblaze B2, or other disk is where a finished archive is copied. It is not the server the site is read from. It is not contacted until you enable it.</p>
  {{if .Site.Destinations}}
    <ul>
    {{range .Site.Destinations}}
      <li>{{.Kind}} {{.ID}}{{if .Enabled}} · enabled{{else}} · saved{{end}}{{if .Status}} · last run {{.Status}}{{end}}<br><span class="muted">{{.Detail}}</span></li>
    {{end}}
    </ul>
  {{else}}
    <p>No destinations saved.</p>
  {{end}}
  <form method="post" action="/sites/{{.Site.ID}}/destinations">
    <label for="kind">Kind</label>
    <select id="kind" name="kind" data-switch>
      <option value="sftp">SFTP</option>
      <option value="s3">S3</option>
      <option value="b2">Backblaze B2</option>
      <option value="disk">Another disk</option>
    </select>
    <div data-for="sftp">
      <label for="host">SFTP host</label>
      <input id="host" name="host">
      <label for="user">SFTP user</label>
      <input id="user" name="user">
    </div>
    <div data-for="sftp disk">
      <label for="path">Path (SFTP absolute path, or another disk)</label>
      <input id="path" name="path">
    </div>
    <div data-for="s3 b2">
      <label for="bucket">Bucket</label>
      <input id="bucket" name="bucket">
      <label for="prefix">Prefix</label>
      <input id="prefix" name="prefix">
      <label for="access_key">Access key or B2 account id</label>
      <input id="access_key" name="access_key" autocomplete="off">
      <label for="secret_key">Secret key</label>
      <input id="secret_key" name="secret_key" type="password" autocomplete="off">
    </div>
    <div data-for="s3">
      <label for="endpoint">S3 endpoint</label>
      <input id="endpoint" name="endpoint" placeholder="s3.amazonaws.com">
    </div>
    <label class="check"><input type="checkbox" name="enabled"> Enable this destination</label>
    <button type="submit" data-pending="Saving destination…">Save destination</button>
  </form>
</section>
<section class="card">
  <h2>Schedule</h2>
  <p class="muted">Saving a schedule does not start a timer. A system cron must run <code>run_scheduled</code> every minute. That command runs the same backup path, with no agent in the loop.</p>
  <form method="post" action="/sites/{{.Site.ID}}/schedule">
    <label for="cron">Cron (minute hour day month weekday)</label>
    <input id="cron" name="cron" value="{{.Site.Cron}}" placeholder="15 3 * * *">
    <label class="check"><input type="checkbox" name="enabled" {{if .Site.CronOn}}checked{{end}}> Enabled</label>
    <button class="secondary" type="submit" data-pending="Saving schedule…">Save schedule</button>
  </form>
</section>
<section class="card">
  <h2>Retention</h2>
  <p class="muted">Each backup is its own snapshot. Unchanged files are stored once. After every backup, snapshots outside these rules are removed and the freed space is pruned, on this machine and on any destination that just received a copy. A snapshot stays if any rule keeps it. 0 skips that rule. Set every field to 0 to keep every snapshot.</p>
  <form method="post" action="/sites/{{.Site.ID}}/retention">
    <label for="keep_last">Newest snapshots</label>
    <input id="keep_last" name="keep_last" inputmode="numeric" value="{{.Site.KeepLast}}" placeholder="0">
    <label for="keep_hourly">Hourly</label>
    <input id="keep_hourly" name="keep_hourly" inputmode="numeric" value="{{.Site.KeepHourly}}" placeholder="0">
    <label for="keep_daily">Daily</label>
    <input id="keep_daily" name="keep_daily" inputmode="numeric" value="{{.Site.KeepDaily}}" placeholder="0">
    <label for="keep_weekly">Weekly</label>
    <input id="keep_weekly" name="keep_weekly" inputmode="numeric" value="{{.Site.KeepWeekly}}" placeholder="0">
    <label for="keep_monthly">Monthly</label>
    <input id="keep_monthly" name="keep_monthly" inputmode="numeric" value="{{.Site.KeepMonthly}}" placeholder="0">
    <label for="keep_yearly">Yearly</label>
    <input id="keep_yearly" name="keep_yearly" inputmode="numeric" value="{{.Site.KeepYearly}}" placeholder="0">
    <button class="secondary" type="submit" data-pending="Applying retention…">Save retention</button>
  </form>
</section>
<section class="card">
  <h2>Speed limits</h2>
  <p class="muted">Restic can cap repository traffic. Rates are KiB per second, the same units as <code>--limit-upload</code> and <code>--limit-download</code>. Leave a field empty for unlimited. The cap applies to backup, copies to other destinations, checks, and restores.</p>
  <form method="post" action="/sites/{{.Site.ID}}/limits">
    <label for="limit_upload">Upload limit (KiB/s)</label>
    <input id="limit_upload" name="limit_upload" inputmode="numeric" value="{{.Site.UploadKiB}}" placeholder="unlimited">
    <label for="limit_download">Download limit (KiB/s)</label>
    <input id="limit_download" name="limit_download" inputmode="numeric" value="{{.Site.DownloadKiB}}" placeholder="unlimited">
    <button class="secondary" type="submit" data-pending="Saving speed limits…">Save speed limits</button>
  </form>
</section>
<script>
(function () {
  function apply(select) {
    var kind = select.value;
    select.form.querySelectorAll("[data-for]").forEach(function (el) {
      el.hidden = el.getAttribute("data-for").split(" ").indexOf(kind) === -1;
    });
  }
  document.querySelectorAll("select[data-switch]").forEach(function (select) {
    apply(select);
    select.addEventListener("change", function () { apply(select); });
  });
  function applyCheck(input) {
    input.form.querySelectorAll("[data-when-checked]").forEach(function (el) {
      el.hidden = !input.checked;
    });
  }
  document.querySelectorAll("input[data-switch-check]").forEach(function (input) {
    applyCheck(input);
    input.addEventListener("change", function () { applyCheck(input); });
  });
})();
</script>
{{if .Site.Steps}}
<section class="card">
  <h2>Restore procedure</h2>
  <pre>{{range .Site.Steps}}{{.}}
{{end}}</pre>
</section>
{{end}}
</main>
{{template "feedback" .}}
</body>
</html>
{{end}}

{{define "progress"}}
{{if .Phase}}
<p>{{.Phase}}{{if .Known}} · {{.Percent}}%{{end}}</p>
<div class="bar{{if not .Known}} unknown{{end}}" role="progressbar" aria-valuemin="0" aria-valuemax="100"{{if .Known}} aria-valuenow="{{.Percent}}"{{end}} aria-valuetext="{{.Phase}}">
  <span{{if .Known}} style="width: {{.Percent}}%"{{end}}></span>
</div>
{{if .File}}<p class="muted">{{.File}}</p>{{end}}
{{if .Count}}<p class="muted">{{.Count}}</p>{{end}}
{{if .Written}}<p class="muted">{{.Written}}</p>{{end}}
{{if .Limit}}<p class="muted">{{.Limit}}</p>{{end}}
{{end}}
{{end}}

{{define "feedback"}}
<style>
  .notice { background: #e7f2ef; border: 2px solid #0f5c57; border-radius: 8px; padding: 0.85rem 1rem; margin: 0.75rem 0; font-weight: 600; }
  .notice.bad { background: #f8ecec; border-color: #8c2f2f; color: #8c2f2f; }
  button[disabled] { opacity: 0.65; cursor: wait; }
</style>
<script>
document.addEventListener("submit", function (event) {
  var form = event.target;
  if (!form || !form.getAttribute) return;
  var button = event.submitter || form.querySelector("button[type=submit]");
  if (!button) return;
  var pending = button.getAttribute("data-pending") || "Working…";
  var live = document.getElementById("action-status");
  if (!live) {
    live = document.createElement("p");
    live.id = "action-status";
    live.className = "notice";
    live.setAttribute("role", "status");
    form.parentNode.insertBefore(live, form);
  }
  live.textContent = pending;
  window.setTimeout(function () {
    button.disabled = true;
    button.textContent = pending;
  }, 0);
});
</script>
{{end}}
`
