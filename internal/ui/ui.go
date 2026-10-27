// Package ui is the localhost site list. It calls the same backup path as the CLI.
package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backupbinder/internal/app"
	"backupbinder/internal/destination"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// Handler serves the local UI. Bind it to 127.0.0.1.
func Handler(application *app.App) http.Handler {
	mux := http.NewServeMux()
	ui := &server{app: application, tmpl: template.Must(template.New("page").Parse(pageHTML))}
	mux.HandleFunc("GET /", ui.home)
	mux.HandleFunc("GET /register", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /setup", ui.setupForm)
	mux.HandleFunc("POST /setup", ui.setupCreate)
	mux.HandleFunc("GET /sites/{id}/setup", ui.setupShow)
	mux.HandleFunc("GET /sites/{id}/setup/{step}", ui.setupShow)
	mux.HandleFunc("POST /sites/{id}/setup/{step}", ui.setupSave)
	mux.HandleFunc("GET /sites/{id}", ui.site)
	mux.HandleFunc("POST /sites/{id}/name", ui.rename)
	mux.HandleFunc("POST /sites/{id}/remove", ui.removeSite)
	mux.HandleFunc("GET /sites/{id}/progress", ui.progressJSON)
	mux.HandleFunc("POST /sites/{id}/backup", ui.backup)
	mux.HandleFunc("POST /sites/{id}/destinations", ui.destination)
	mux.HandleFunc("POST /sites/{id}/destinations/{dest}", ui.destinationUpdate)
	mux.HandleFunc("POST /sites/{id}/destinations/{dest}/enabled", ui.destinationEnabled)
	mux.HandleFunc("POST /sites/{id}/destinations/{dest}/remove", ui.destinationRemove)
	mux.HandleFunc("POST /sites/{id}/test-restore", ui.testRestore)
	mux.HandleFunc("GET /sites/{id}/restore-status", ui.restoreStatus)
	mux.HandleFunc("POST /sites/{id}/schedule", ui.schedule)
	mux.HandleFunc("POST /sites/{id}/limits", ui.limits)
	mux.HandleFunc("POST /sites/{id}/retention", ui.retention)
	mux.HandleFunc("POST /sites/{id}/location", ui.location)
	mux.HandleFunc("POST /sites/{id}/adapter", ui.adapter)
	mux.HandleFunc("POST /sites/{id}/database", ui.database)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := application.ReloadSites(); err != nil {
			message := err.Error()
			if fe, ok := err.(*fail.Error); ok {
				message = fe.Message
			}
			http.Error(w, message, http.StatusBadRequest)
			return
		}
		mux.ServeHTTP(w, r)
	})
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
	Href       string
	Progress   progressView
}

type page struct {
	Title     string
	Nav       string // "setup" or "sites" — binder tab
	Empty     bool
	Cards     []card
	Site      *siteView
	Notice    string
	NoticeBad bool
	Refresh   bool
	Watch     bool
	FormErr   string
	Tools     string
	DestKinds []destKindView
	DestDraft *destDraft
	Focus     string
	Wizard    *wizardView
}

// destDraft holds submitted destination fields so a failed save can re-render them.
type destDraft struct {
	Kind     string
	Enabled  bool
	Host     string
	User     string
	Path     string
	Bucket   string
	Prefix   string
	Endpoint string
}

func (d *destDraft) Selected(kind string) bool {
	return d != nil && d.Kind == kind
}

func (d *destDraft) Field(kind, name string) string {
	if d == nil || d.Kind != kind {
		return ""
	}
	switch name {
	case "host":
		return d.Host
	case "user":
		return d.User
	case "path":
		return d.Path
	case "bucket":
		return d.Bucket
	case "prefix":
		return d.Prefix
	case "endpoint":
		return d.Endpoint
	default:
		return ""
	}
}

func destDraftFrom(r *http.Request) *destDraft {
	return &destDraft{
		Kind:     strings.TrimSpace(r.FormValue("kind")),
		Enabled:  r.FormValue("enabled") == "on",
		Host:     strings.TrimSpace(r.FormValue("host")),
		User:     strings.TrimSpace(r.FormValue("user")),
		Path:     strings.TrimSpace(r.FormValue("path")),
		Bucket:   strings.TrimSpace(r.FormValue("bucket")),
		Prefix:   strings.TrimSpace(r.FormValue("prefix")),
		Endpoint: strings.TrimSpace(r.FormValue("endpoint")),
	}
}

type destKindView struct {
	ID             string
	Label          string
	Note           string
	Fields         []destFieldView
	HasCredentials bool
	AccessLabel    string
	SecretLabel    string
}

type destFieldView struct {
	Name        string
	Label       string
	Placeholder string
	Current     string
	Required    bool
	Choices     []string
}

// Value prefers a draft value, otherwise the sole choice when there is only one.
func (f destFieldView) Value(draft string) string {
	if draft != "" {
		return draft
	}
	if len(f.Choices) == 1 {
		return f.Choices[0]
	}
	return ""
}

// Options returns Choices plus a draft value that is not already listed.
func (f destFieldView) Options(draft string) []string {
	if draft == "" {
		return f.Choices
	}
	for _, choice := range f.Choices {
		if choice == draft {
			return f.Choices
		}
	}
	return append([]string{draft}, f.Choices...)
}

type backupRow struct {
	When       string
	Size       string
	Confidence string
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
	Backups         []backupRow
	Cron            string
	CronOn          bool
	ScheduleLine    string
	RestoreRunning  bool
	RestoreScratch  string
	RestoreError    string
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
	CanCheckRestore bool
	Progress        progressView
	DBEnabled       bool
	DBName          string
	DBUser          string
	DBHost          string
	DBMethod        string
	DBPasswordLabel string
	DBSummary       string
	DestWarning     string
	Config          string
	LocalRepo       string
}

type progressView struct {
	Phase   string
	File    string
	Percent int
	Known   bool
	Count   string
	Written string
	Limit   string
	ETA     string
	Detail  string
	Notice  string
}

type destView struct {
	ID             string
	Kind           string
	Enabled        bool
	Status         string
	Detail         string
	Reason         string
	Removable      bool
	Fields         []destFieldView
	HasCredentials bool
	AccessLabel    string
	SecretLabel    string
}

type wizardView struct {
	Step            string
	ID              string
	Name            string
	Adapter         string
	Root            string
	LocationKind    string
	Host            string
	User            string
	RemotePath      string
	LocalPath       string
	DBEnabled       bool
	DBName          string
	DBUser          string
	DBHost          string
	DBNote          string
	DBPasswordLabel string
	Cron            string
	CronOn          bool
	ScheduleLine    string
	Back            string
	Config          string
	Edit            string
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	sites, err := s.app.DB.ListSites()
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(sites) == 0 {
		target := "/setup"
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	view := page{Title: "Sites", Nav: "sites", Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1", Tools: s.app.Restic.Binary}
	for _, site := range sites {
		item := s.card(site)
		if item.Status == "running" {
			view.Refresh = true
		}
		if len(site.Setup) > 0 {
			item.Href = "/sites/" + site.ID + "/setup"
		} else {
			item.Href = "/sites/" + site.ID
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
	s.render(w, "register", page{Title: "Register a site", Nav: "setup", Tools: s.app.Restic.Binary})
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
	if r.URL.Query().Get("edit") != "1" {
		if site, err := s.app.DB.GetSite(r.PathValue("id")); err == nil && len(site.Setup) > 0 {
			http.Redirect(w, r, "/sites/"+site.ID+"/setup", http.StatusSeeOther)
			return
		}
	}
	view, ok := s.siteView(w, r)
	if !ok {
		return
	}
	watch := r.URL.Query().Get("watch") == "1" || view.Status == "running"
	s.render(w, "site", page{
		Title: view.Name, Site: view, Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1",
		Refresh: view.Status == "running" && !watch, Watch: watch,
		Tools: s.app.Restic.Binary, DestKinds: destKindViews(),
	})
}

func destKindViews() []destKindView {
	kinds, err := destination.Load()
	if err != nil {
		return nil
	}
	var out []destKindView
	for _, kind := range kinds {
		item := destKindView{ID: kind.ID, Label: kind.Label, Note: kind.Note}
		for _, field := range kind.Fields {
			item.Fields = append(item.Fields, destFieldView{
				Name: field.Name, Label: field.Label, Placeholder: field.Placeholder, Required: field.Required,
				Choices: destination.FieldChoices(field.Choices),
			})
		}
		if kind.Credentials != nil {
			item.HasCredentials = true
			item.AccessLabel = kind.Credentials.AccessLabel
			item.SecretLabel = kind.Credentials.SecretLabel
		}
		out = append(out, item)
	}
	return out
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
		ScheduleLine: inspected.ScheduleInvocation,
		UploadKiB:    formatKiB(inspected.Site.Limits.UploadKiB), DownloadKiB: formatKiB(inspected.Site.Limits.DownloadKiB),
		KeepLast: formatCount(inspected.Site.Retention.KeepLast), KeepHourly: formatCount(inspected.Site.Retention.KeepHourly),
		KeepDaily: formatCount(inspected.Site.Retention.KeepDaily), KeepWeekly: formatCount(inspected.Site.Retention.KeepWeekly),
		KeepMonthly: formatCount(inspected.Site.Retention.KeepMonthly), KeepYearly: formatCount(inspected.Site.Retention.KeepYearly),
		LocationKind: inspected.Site.Location.Kind, LocationHost: inspected.Site.Location.Host,
		LocationUser: inspected.Site.Location.User, LocationPath: inspected.Site.Location.Path,
		Config:    filepath.Join(s.app.Home, "sites", inspected.Site.ID+".json"),
		LocalRepo: filepath.Join(s.app.Home, "restic", inspected.Site.ID),
	}
	if view.LocationKind == "" {
		view.LocationKind = "local"
	}
	for _, dest := range inspected.Site.Destinations {
		if dest.Kind == "local_restic" && dest.Path != "" {
			view.LocalRepo = dest.Path
			break
		}
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
	view.CanCheckRestore = s.app.HasSuccessfulBackup(id)
	if listed, err := s.app.ListBackups(id); err == nil {
		for _, backup := range listed.Backups {
			confidence := backup.RestoreConfidence
			if confidence == "" {
				confidence = "never"
			}
			view.Backups = append(view.Backups, backupRow{
				When:       backup.StartedAt,
				Size:       strconv.FormatInt(backup.SizeBytes, 10) + " bytes",
				Confidence: confidence,
			})
		}
	}
	statusByID := map[string]string{}
	reasonByID := map[string]string{}
	if run.Manifest != nil {
		for _, dest := range run.Manifest.Destinations {
			statusByID[dest.ID] = dest.Status
			if dest.Reason != "" && (dest.Status == "failed" || dest.Status == "skipped") {
				reasonByID[dest.ID] = dest.Reason
			}
		}
		if len(run.Manifest.Warnings) > 0 && run.OK {
			view.DestWarning = strings.Join(run.Manifest.Warnings, " ")
		}
	}
	kinds := destKindViews()
	for _, dest := range inspected.Site.Destinations {
		detail := dest.Kind
		item := destView{
			ID: dest.ID, Kind: dest.Kind, Enabled: dest.Enabled, Status: statusByID[dest.ID],
			Reason: reasonByID[dest.ID], Removable: dest.Kind != "local_restic",
		}
		if kind, err := destination.ByID(dest.Kind); err == nil {
			detail = kind.DetailLabel(dest)
			for _, viewKind := range kinds {
				if viewKind.ID != dest.Kind {
					continue
				}
				for _, field := range viewKind.Fields {
					field.Current = savedField(dest, field.Name)
					item.Fields = append(item.Fields, field)
				}
				item.HasCredentials = viewKind.HasCredentials
				item.AccessLabel = viewKind.AccessLabel
				item.SecretLabel = viewKind.SecretLabel
			}
		} else if dest.Host != "" {
			detail = dest.User + "@" + dest.Host + dest.Path
		} else if dest.Bucket != "" {
			detail = dest.Bucket
		} else if dest.Path != "" {
			detail = dest.Path
		}
		if dest.Credentials != "" {
			detail += " (credentials " + dest.Credentials + ")"
		}
		item.Detail = detail
		view.Destinations = append(view.Destinations, item)
	}
	if status, ok := s.app.ReadRestoreStatus(id); ok {
		view.RestoreRunning = status.InProgress
		view.RestoreScratch = status.Scratch
		view.RestoreError = status.Error
	}
	return view, true
}

func savedField(dest model.Destination, name string) string {
	switch name {
	case "host":
		return dest.Host
	case "user":
		return dest.User
	case "path":
		return dest.Path
	case "bucket":
		return dest.Bucket
	case "prefix":
		return dest.Prefix
	case "endpoint":
		return dest.Endpoint
	default:
		return ""
	}
}

func (s *server) progressView(siteID, runID string) progressView {
	p, ok := s.app.ReadProgress(siteID, runID)
	if !ok {
		return progressView{Phase: "Starting the backup"}
	}
	view := viewProgress(p)
	run, err := s.app.Latest(siteID)
	started := time.Time{}
	if err == nil {
		started, _ = time.Parse(time.RFC3339, run.StartedAt)
	}
	upload, download := 0, 0
	if site, err := s.app.DB.GetSite(siteID); err == nil {
		upload, download = site.Limits.UploadKiB, site.Limits.DownloadKiB
		view.Limit = uploadLimitNote(site.Limits.UploadKiB)
	}
	timing := app.EstimateTiming(p, started, upload, download, time.Now())
	view.ETA = timing.ETA
	view.Detail = timing.Detail
	view.Notice = timing.Notice
	return view
}

func viewProgress(p model.Progress) progressView {
	view := progressView{Phase: p.Phase, File: p.File}
	switch {
	case p.PercentKnown:
		view.Known = true
		view.Percent = int(p.Percent + 0.5)
	case p.FilesTotal > 0:
		view.Known = true
		view.Percent = int(float64(p.FilesDone)*100/float64(p.FilesTotal) + 0.5)
	case p.BytesTotal > 0 && p.BytesWritten > 0:
		view.Known = true
		view.Percent = int(float64(p.BytesWritten)*100/float64(p.BytesTotal) + 0.5)
		if view.Percent > 99 {
			view.Percent = 99
		}
	}
	if view.Known {
		if view.Percent < 0 {
			view.Percent = 0
		}
		if view.Percent > 100 {
			view.Percent = 100
		}
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

func (s *server) backup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.app.StartBackupDetached(id); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Backup failed. "+noticeText(err), true)
		return
	}
	http.Redirect(w, r, "/sites/"+id+"?watch=1#backup-progress", http.StatusSeeOther)
}

func (s *server) progressJSON(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.app.Latest(id)
	if err != nil || run.RunID == "" {
		writeProgressJSON(w, map[string]any{"ok": true, "running": false, "status": "none"})
		return
	}
	if run.InProgress() {
		view := s.progressView(id, run.RunID)
		writeProgressJSON(w, map[string]any{
			"ok": true, "running": true, "status": "running",
			"started_at":  run.StartedAt,
			"phase":       view.Phase,
			"percent":     view.Percent,
			"known":       view.Known,
			"file":        view.File,
			"count":       view.Count,
			"written":     view.Written,
			"limit":       view.Limit,
			"eta":         view.ETA,
			"eta_detail":  view.Detail,
			"long_notice": view.Notice,
		})
		return
	}
	status := "failed"
	notice := ""
	if run.OK && run.Manifest != nil {
		status = "success"
		notice = backupNotice(*run.Manifest)
	} else if run.Failure != nil {
		notice = "Backup failed. " + noticeText(run.Failure)
	}
	writeProgressJSON(w, map[string]any{
		"ok": true, "running": false, "status": status, "notice": notice,
	})
}

func writeProgressJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
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
	if err := s.app.StartRestoreDetached(id, "test_restore", "", ""); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Restore check failed. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Restore check started. The scratch folder appears here when it finishes.", false)
}

func (s *server) restoreStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.app.DB.GetSite(id); err != nil {
		http.NotFound(w, r)
		return
	}
	status, ok := s.app.ReadRestoreStatus(id)
	if !ok {
		status = app.RestoreStatus{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
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
		view, ok := s.siteView(w, r)
		if !ok {
			return
		}
		s.render(w, "site", page{
			Title: view.Name, Site: view, FormErr: "Destination was not saved. " + noticeText(err),
			Tools: s.app.Restic.Binary, DestKinds: destKindViews(), DestDraft: destDraftFrom(r), Focus: "destinations",
		})
		return
	}
	notice := "Destination saved. It is not contacted until you enable it."
	if dest.Enabled {
		notice = "Destination saved and checked. It will receive a copy on the next backup."
	}
	s.redirectNotice(w, r, "/sites/"+id+"#destinations", notice, false)
}

func (s *server) destinationUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	destID := r.PathValue("dest")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	site, err := s.app.DB.GetSite(id)
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not saved. "+noticeText(err), true)
		return
	}
	var current model.Destination
	found := false
	for _, dest := range site.Destinations {
		if dest.ID == destID {
			current = dest
			found = true
			break
		}
	}
	if !found {
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not saved. Destination "+destID+" was not found on this site.", true)
		return
	}
	kind, err := destination.ByID(current.Kind)
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not saved. "+noticeText(err), true)
		return
	}
	enabled := r.FormValue("enabled") == "on"
	patch := app.DestinationPatch{Enabled: &enabled}
	for _, field := range kind.Fields {
		patch.SetField(field.Name, strings.TrimSpace(r.FormValue(field.Name)))
	}
	if r.FormValue("access_key") != "" || r.FormValue("secret_key") != "" {
		patch.KeysSet = true
		patch.AccessKey = r.FormValue("access_key")
		patch.SecretKey = r.FormValue("secret_key")
	}
	if _, err := s.app.SetDestination(id, destID, patch); err != nil {
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination "+destID+" saved.", false)
}

func (s *server) rename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	saved, unchanged, err := s.app.SetName(id, r.FormValue("name"))
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Name was not saved. "+noticeText(err), true)
		return
	}
	if unchanged {
		s.redirectNotice(w, r, "/sites/"+id, "Name is already "+saved.Name+".", false)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Name saved. The id stays "+saved.ID+".", false)
}

func (s *server) removeSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	repo, err := s.app.RemoveSite(id)
	if err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Site was not removed. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/", "Site removed. The local restic repository was left at "+repo+".", false)
}

func (s *server) destinationEnabled(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	destID := r.PathValue("dest")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	enabled := r.FormValue("enabled") == "on"
	if _, err := s.app.SetDestinationEnabled(id, destID, enabled); err != nil {
		view, ok := s.siteView(w, r)
		if !ok {
			return
		}
		s.render(w, "site", page{
			Title: view.Name, Site: view, FormErr: "Destination was not updated. " + noticeText(err),
			Tools: s.app.Restic.Binary, DestKinds: destKindViews(), Focus: "destinations",
		})
		return
	}
	notice := "Destination " + destID + " is saved only until you enable it."
	if enabled {
		notice = "Destination " + destID + " is enabled and checked for the next backup."
	}
	s.redirectNotice(w, r, "/sites/"+id+"#destinations", notice, false)
}

func (s *server) destinationRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	destID := r.PathValue("dest")
	if err := s.app.RemoveDestination(id, destID); err != nil {
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not removed. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination "+destID+" removed.", false)
}

func (s *server) schedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	enabled := r.FormValue("enabled") == "on"
	if _, err := s.app.SetSchedule(id, strings.TrimSpace(r.FormValue("cron")), &enabled); err != nil {
		s.redirectNotice(w, r, "/sites/"+id, "Schedule was not saved. "+noticeText(err), true)
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Schedule saved. It is not installed. Add this cron line: "+s.app.ScheduleLine(), false)
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
	enabled := r.FormValue("enabled") == "on"
	saved, err := s.app.SetDatabase(id, app.DatabaseInput{
		Enabled:      &enabled,
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
	notice := locationSavedNotice(saved)
	if est, estErr := s.app.EstimateSource(saved.ID); estErr == nil {
		notice += " " + sizeEstimateNotice(est)
	} else {
		notice += " Size could not be checked yet. " + noticeText(estErr)
	}
	s.redirectNotice(w, r, "/sites/"+id, notice, false)
}

func locationSavedNotice(site model.Site) string {
	if site.Location.Kind == "sftp" {
		return "Location saved. The next backup copies files from " + site.Location.User + "@" + site.Location.Host + ":" + site.Location.Path + "."
	}
	return "Location saved. Files are read from " + site.Root + "."
}

func sizeEstimateNotice(est app.SizeEstimate) string {
	text := "Likely backup size: about " + strconv.Itoa(est.Files) + " files, " + formatBytes(est.Bytes) + "."
	if est.Note != "" {
		text += " " + est.Note
	}
	return text
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
	frag := ""
	if i := strings.Index(path, "#"); i >= 0 {
		frag = path[i:]
		path = path[:i]
	}
	q := url.Values{}
	q.Set("notice", notice)
	if bad {
		q.Set("bad", "1")
	}
	http.Redirect(w, r, path+"?"+q.Encode()+frag, http.StatusSeeOther)
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
	if view.Nav == "" {
		switch name {
		case "setup", "register":
			view.Nav = "setup"
		default:
			view.Nav = "sites"
		}
	}
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

func (s *server) setupForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "setup", page{Wizard: &wizardView{Step: "location", LocationKind: "local"}, DestKinds: destKindViews()})
}

func (s *server) setupCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	result, err := s.app.Setup(formSetup(r, "location", app.SetupInput{}))
	if err != nil || result.SiteID == "" {
		message := noticeText(err)
		if message == "" && result.Next != nil {
			message = result.Next.Reason
		}
		s.render(w, "setup", page{
			FormErr: message,
			Wizard: &wizardView{
				Step: "location", LocationKind: formKind(r), LocalPath: r.FormValue("local_path"),
				Host: r.FormValue("host"), User: r.FormValue("user"), RemotePath: r.FormValue("remote_path"),
			},
			DestKinds: destKindViews(),
		})
		return
	}
	http.Redirect(w, r, setupRedirect(result, s.setupSizeNotice(result.SiteID)), http.StatusSeeOther)
}

func (s *server) setupShow(w http.ResponseWriter, r *http.Request) {
	site, ok := s.wizardSite(w, r)
	if !ok {
		return
	}
	step := r.PathValue("step")
	if step == "" {
		step = app.NextStep(site.Setup)
	}
	if step == "" {
		http.Redirect(w, r, "/sites/"+site.ID, http.StatusSeeOther)
		return
	}
	if !wizardStep(step) {
		http.NotFound(w, r)
		return
	}
	s.render(w, "setup", page{
		Wizard: s.wizard(site, step), DestKinds: destKindViews(),
		Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1",
	})
}

func (s *server) setupSave(w http.ResponseWriter, r *http.Request) {
	site, ok := s.wizardSite(w, r)
	if !ok {
		return
	}
	step := r.PathValue("step")
	if !wizardStep(step) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	if step == "backup" && r.FormValue("skip") != "1" {
		if _, err := s.app.Setup(app.SetupInput{ID: site.ID, Root: site.Root, Step: "backup", NoBackup: true}); err != nil {
			s.render(w, "setup", page{FormErr: noticeText(err), Wizard: s.wizard(site, step), DestKinds: destKindViews()})
			return
		}
		s.backup(w, r)
		return
	}
	result, err := s.app.Setup(formSetup(r, step, app.SetupInput{ID: site.ID, Root: site.Root}))
	if err != nil || result.Phase == "needs_input" {
		message := noticeText(err)
		if message == "" && result.Next != nil {
			message = result.Next.Reason
		}
		current, loadErr := s.app.DB.GetSite(site.ID)
		if loadErr != nil {
			current = site
		}
		wiz := s.wizard(current, step)
		var draft *destDraft
		focus := ""
		if step == "destination" {
			draft = destDraftFrom(r)
			focus = "destinations"
		}
		s.render(w, "setup", page{
			FormErr: message, Wizard: wiz, DestKinds: destKindViews(), DestDraft: draft, Focus: focus,
		})
		return
	}
	notice := ""
	if step == "location" {
		notice = s.setupSizeNotice(result.SiteID)
	}
	http.Redirect(w, r, setupRedirect(result, notice), http.StatusSeeOther)
}

func (s *server) setupSizeNotice(siteID string) string {
	if siteID == "" {
		return ""
	}
	est, err := s.app.EstimateSource(siteID)
	if err != nil {
		return "Size could not be checked yet. " + noticeText(err)
	}
	return sizeEstimateNotice(est)
}

func (s *server) wizardSite(w http.ResponseWriter, r *http.Request) (model.Site, bool) {
	site, err := s.app.DB.GetSite(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return model.Site{}, false
	}
	return site, true
}

func (s *server) wizard(site model.Site, step string) *wizardView {
	view := &wizardView{
		Step: step, ID: site.ID, Name: site.Name, Adapter: site.Adapter, Root: site.Root,
		LocationKind: site.Location.Kind, Host: site.Location.Host, User: site.Location.User,
		Config: filepath.Join(s.app.Home, "sites", site.ID+".json"),
		Edit:   "/sites/" + site.ID + "?edit=1",
		Back:   previousStep(step),
	}
	if view.LocationKind == "" || view.LocationKind == "local" {
		view.LocationKind = "local"
		view.LocalPath = site.Root
	} else {
		view.RemotePath = site.Location.Path
	}
	if db := model.DatabaseSource(site); db != nil {
		view.DBEnabled = true
		view.DBName, view.DBUser, view.DBHost = db.Name, db.User, db.Host
		view.DBPasswordLabel = s.app.DatabasePasswordLabel(site)
		view.DBNote = "A dump of " + db.Name + " as " + db.User + " at " + db.Host + " is planned."
		if view.DBPasswordLabel != "" {
			view.DBNote += " Password: " + view.DBPasswordLabel + "."
		}
	} else if site.Location.Kind == "sftp" {
		view.DBNote = "No database was detected yet. Fill these fields to include a dump now, or leave the box unchecked for files only. The first backup reads the server copy."
	} else {
		view.DBNote = "No database was detected. Check the box and fill the fields to include a dump, or leave it unchecked for files only."
	}
	view.Cron = site.Schedule.Cron
	if view.Cron == "" {
		view.Cron = "15 3 * * *"
	}
	view.CronOn = site.Schedule.Enabled
	view.ScheduleLine = s.app.ScheduleLine()
	return view
}

func formSetup(r *http.Request, step string, in app.SetupInput) app.SetupInput {
	in.Step = step
	switch step {
	case "location":
		if r.FormValue("kind") == "sftp" {
			in.Host = strings.TrimSpace(r.FormValue("host"))
			in.User = strings.TrimSpace(r.FormValue("user"))
			in.RemotePath = strings.TrimSpace(r.FormValue("remote_path"))
		} else {
			in.Path = strings.TrimSpace(r.FormValue("local_path"))
		}
	case "name":
		in.Name = strings.TrimSpace(r.FormValue("name"))
	case "database":
		if r.FormValue("enabled") == "on" {
			in.Database = true
			in.DBName = strings.TrimSpace(r.FormValue("name"))
			in.DBUser = strings.TrimSpace(r.FormValue("user"))
			in.DBHost = strings.TrimSpace(r.FormValue("host"))
			in.DBPassword = r.FormValue("password")
		} else {
			in.NoDatabase = true
		}
	case "destination":
		kind := strings.TrimSpace(r.FormValue("kind"))
		if kind == "" || kind == "none" {
			in.Remote = "none"
		} else {
			in.Remote = kind
			in.Enabled = r.FormValue("enabled") == "on"
			in.Host = strings.TrimSpace(r.FormValue("host"))
			in.User = strings.TrimSpace(r.FormValue("user"))
			in.RemotePath = strings.TrimSpace(r.FormValue("path"))
			in.Bucket = strings.TrimSpace(r.FormValue("bucket"))
			in.Prefix = strings.TrimSpace(r.FormValue("prefix"))
			in.Endpoint = strings.TrimSpace(r.FormValue("endpoint"))
			in.AccessKey = r.FormValue("access_key")
			in.SecretKey = r.FormValue("secret_key")
		}
	case "schedule":
		in.Cron = strings.TrimSpace(r.FormValue("cron"))
		if r.FormValue("enabled") == "on" {
			in.Schedule = true
		} else {
			in.NoSchedule = true
		}
	case "backup":
		in.NoBackup = true
	}
	return in
}

func formKind(r *http.Request) string {
	if r.FormValue("kind") == "sftp" {
		return "sftp"
	}
	return "local"
}

func setupRedirect(result app.SetupResult, notice string) string {
	path := "/sites/" + result.SiteID + "/setup"
	if result.Phase == "ready" || app.NextStep(result.Setup) == "" {
		path = "/sites/" + result.SiteID
	}
	if notice == "" {
		return path
	}
	return path + "?notice=" + url.QueryEscape(notice)
}

func wizardStep(step string) bool {
	switch step {
	case "location", "name", "database", "destination", "schedule", "backup":
		return true
	default:
		return false
	}
}

func previousStep(step string) string {
	order := []string{"location", "name", "database", "destination", "schedule", "backup"}
	for i, item := range order {
		if item == step && i > 0 {
			return order[i-1]
		}
	}
	return ""
}

const pageHTML = `
{{define "styles"}}
:root {
  --paper: #f2efe8;
  --ink: #1e2430;
  --rail: #1e2430;
  --rail-muted: #c8cdd6;
  --accent: #0f5c57;
  --accent-hover: #0c4a46;
  --brass: #c4a574;
  --ok: #1f6b3a;
  --bad: #8c2f2f;
  --muted: #5c6570;
  --card: #ffffff;
  --line: #ddd6c8;
  --focus: #1a4f8b;
}
* { box-sizing: border-box; }
body {
  margin: 0; min-height: 100vh;
  background: var(--paper); color: var(--ink);
  font: 16px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif;
}
a { color: var(--accent); }
a:focus-visible, button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, summary:focus-visible {
  outline: 2px solid var(--focus); outline-offset: 2px;
}
::selection { background: #c5e4df; color: var(--ink); }
* { scrollbar-width: thin; scrollbar-color: var(--brass) var(--paper); }
*::-webkit-scrollbar { width: 10px; height: 10px; }
*::-webkit-scrollbar-track { background: var(--paper); }
*::-webkit-scrollbar-thumb { background: var(--brass); border-radius: 6px; border: 2px solid var(--paper); }
.shell { display: grid; grid-template-columns: 13.5rem 1fr; min-height: 100vh; }
.rail {
  background: var(--rail); color: #f2efe8;
  padding: 1.35rem 0.85rem 1.25rem;
  display: flex; flex-direction: column; gap: 0.3rem;
  position: sticky; top: 0; height: 100vh;
  box-shadow: inset -6px 0 0 var(--brass);
}
.brand { font-weight: 650; font-size: 1.05rem; padding: 0.15rem 0.7rem 1.25rem; letter-spacing: -0.01em; }
.brand small { display: block; font-weight: 400; font-size: 0.75rem; color: var(--rail-muted); margin-top: 0.2rem; }
.tab {
  display: block; padding: 0.65rem 0.75rem; border-radius: 6px 0 0 6px;
  color: var(--rail-muted); text-decoration: none; font-weight: 500;
  border-left: 3px solid transparent;
}
.tab:hover { color: #fff; }
.tab.active {
  background: var(--paper); color: var(--ink); font-weight: 650;
  border-left-color: var(--brass);
  box-shadow: inset 0 1px 0 #fff8, 0 1px 0 rgba(30, 36, 48, 0.06);
}
.tab-cta {
  margin-top: auto; display: block; text-align: center; text-decoration: none;
  background: var(--accent); color: #fff; font-weight: 650;
  padding: 0.75rem 0.85rem; border-radius: 6px;
  box-shadow: inset 0 0 0 1px var(--brass);
}
.tab-cta:hover { background: var(--accent-hover); color: #fff; }
.content { padding: 1.75rem 2rem 2.5rem; max-width: 44rem; }
h1 { font-size: 1.65rem; font-weight: 650; margin: 0 0 0.4rem; letter-spacing: -0.02em; overflow-wrap: anywhere; }
h2 { font-size: 1.05rem; font-weight: 650; margin: 0 0 0.55rem; }
h3 { font-size: 0.9rem; font-weight: 650; margin: 0.85rem 0 0.4rem; }
.lede { color: var(--muted); max-width: 38rem; margin: 0 0 1.35rem; }
.muted { color: var(--muted); }
.ok { color: var(--ok); }
.bad { color: var(--bad); }
p, li, code { overflow-wrap: anywhere; }
.card {
  background: var(--card); border: 1px solid var(--line); border-radius: 8px;
  padding: 1.15rem 1.25rem; margin: 0.85rem 0;
  box-shadow: 0 1px 0 rgba(30, 36, 48, 0.04);
}
.card h2 {
  padding-bottom: 0.35rem;
  border-bottom: 1px solid var(--brass);
  margin-bottom: 0.65rem;
}
.hint { margin-top: 1rem; font-size: 0.88rem; color: var(--muted); }
label, input, select, button, textarea { display: block; width: 100%; font: inherit; margin: 0.35rem 0; }
input, select, textarea {
  border: 1px solid #cfc7b8; border-radius: 6px; padding: 0.55rem 0.65rem;
  background: #fff; color: var(--ink);
}
button, .button {
  background: var(--accent); color: #fff; border: 0; border-radius: 6px;
  padding: 0.65rem 0.9rem; text-align: center; text-decoration: none; font-weight: 600; cursor: pointer;
}
button:hover, .button:hover { background: var(--accent-hover); }
.secondary { background: transparent; color: var(--accent); border: 1px solid var(--accent); }
.secondary:hover { background: #e7f2ef; }
.check { display: flex; align-items: center; gap: 0.5rem; width: auto; }
.check input { width: auto; margin: 0; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #ebe6dc; padding: 0.75rem; border-radius: 6px; }
.bar {
  height: 0.85rem; background: #e3ddd2; border-radius: 3px; overflow: hidden; margin: 0.45rem 0;
}
.bar > span { display: block; height: 100%; background: var(--accent); min-width: 2px; transition: width 180ms ease-out; }
.bar.unknown > span { width: 35%; animation: indeterminate 1.1s ease-in-out infinite; }
@keyframes indeterminate { 0% { margin-left: -35%; } 100% { margin-left: 100%; } }
.progress-pct {
  font-size: 1.75rem; font-weight: 650; letter-spacing: -0.02em; margin: 0.25rem 0 0.15rem;
  color: var(--accent);
}
.site-list { list-style: none; padding: 0; margin: 0; }
.site-list > li { border-bottom: 1px solid var(--line); }
.site-list > li:first-child { border-top: 1px solid var(--line); }
.site-list details { padding: 0.85rem 0; }
.site-list summary {
  cursor: pointer; list-style: none; display: grid;
  grid-template-columns: 1fr auto; gap: 0.75rem; align-items: baseline;
}
.site-list summary::-webkit-details-marker { display: none; }
.site-list summary .name { font-weight: 650; color: var(--ink); text-decoration: none; }
.site-list summary .name:hover { color: var(--accent); }
.site-list .meta { font-size: 0.9rem; color: var(--muted); margin-top: 0.2rem; }
.site-list .trust {
  font-size: 0.8rem; font-weight: 650; letter-spacing: 0.03em;
  border: 1px solid var(--ok); color: var(--ok);
  padding: 0.15rem 0.45rem; border-radius: 3px; white-space: nowrap;
}
.site-list .trust.never { border-color: var(--muted); color: var(--muted); }
.site-list .trust.bad { border-color: var(--bad); color: var(--bad); }
.site-list .body { margin-top: 0.75rem; }
.site-list .body .button { display: inline-block; width: auto; margin-top: 0.5rem; }
.dest-list { list-style: none; padding: 0; margin: 0.5rem 0 1rem; }
.dest-list > li { border-top: 1px solid var(--line); padding: 0.75rem 0; }
.dest-list > li:first-child { border-top: 0; padding-top: 0; }
.dest-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-top: 0.5rem; }
.dest-actions form { margin: 0; width: auto; }
.dest-actions button { width: auto; margin: 0; padding: 0.4rem 0.7rem; font-size: 0.9rem; }
.actions { display: flex; flex-wrap: wrap; gap: 0.5rem; }
.actions button, .actions .button { width: auto; }
.tools { margin-top: 2rem; font-size: 0.85rem; color: var(--muted); }
@media (max-width: 720px) {
  .shell { grid-template-columns: 1fr; }
  .rail {
    position: static; height: auto; padding: 0.85rem 1rem 1rem;
    display: grid; grid-template-columns: 1fr auto; gap: 0.55rem 0.75rem; align-items: center;
    box-shadow: inset 0 -4px 0 var(--brass);
  }
  .brand { padding: 0; grid-column: 1; }
  .brand small { display: none; }
  .nav-tabs { grid-column: 1 / -1; display: flex; gap: 0.35rem; }
  .tab {
    display: inline-block; border-radius: 6px 6px 0 0; padding: 0.5rem 0.75rem;
    border-left: 0; border-bottom: 3px solid transparent;
  }
  .tab.active {
    background: var(--paper); color: var(--ink);
    border-bottom-color: var(--brass);
    box-shadow: none;
  }
  .tab-cta { margin-top: 0; grid-column: 2; grid-row: 1; padding: 0.5rem 0.7rem; font-size: 0.9rem; }
  .content { padding: 1.25rem 1rem 2rem; }
}
{{end}}

{{define "shell-start"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="2">{{end}}
<title>{{if eq .Nav "setup"}}Set up a backup{{else if .Site}}{{.Site.Name}}{{else}}BackupBinder{{end}}</title>
<style>{{template "styles" .}}</style>
</head>
<body>
<div class="shell">
<aside class="rail" aria-label="BackupBinder">
  <div class="brand">BackupBinder<small>on this machine</small></div>
  <div class="nav-tabs">
    <a class="tab{{if eq .Nav "setup"}} active{{end}}" href="/setup"{{if eq .Nav "setup"}} aria-current="page"{{end}}>Setup</a>
    <a class="tab{{if eq .Nav "sites"}} active{{end}}" href="/"{{if eq .Nav "sites"}} aria-current="page"{{end}}>Sites</a>
  </div>
  <a class="tab-cta" href="/setup">Set up a backup</a>
</aside>
<div class="content">
{{end}}

{{define "shell-end"}}
</div>
</div>
{{template "feedback" .}}
</body>
</html>
{{end}}

{{define "home"}}
{{template "shell-start" .}}
<h1>Sites</h1>
<p class="lede">Last run and restore confidence live here. Local-only is a complete backup until you enable another destination. Site types are addons; unmatched paths stay files-only.</p>
{{if .Notice}}<p class="notice{{if .NoticeBad}} bad{{end}}" role="status">{{.Notice}}</p>{{end}}
<ul class="site-list">
{{range $i, $c := .Cards}}
  <li>
    <details{{if or (eq $i 0) (eq $c.Status "running")}} open{{end}}>
      <summary>
        <div>
          <span class="name">{{$c.Name}}</span>
          <div class="meta">{{$c.Adapter}} · {{$c.ID}}</div>
        </div>
        {{if eq $c.Status "success"}}
          <span class="trust{{if eq $c.Confidence "never"}} never{{end}}">Restore {{$c.Confidence}}</span>
        {{else if eq $c.Status "failed"}}
          <span class="trust bad">Failed</span>
        {{else if eq $c.Status "running"}}
          <span class="trust never">Running</span>
        {{else}}
          <span class="trust never">No backup yet</span>
        {{end}}
      </summary>
      <div class="body">
        {{if eq $c.Status "running"}}
          <p class="notice" role="status">Backup in progress since {{$c.When}}.</p>
          {{template "progress" $c.Progress}}
        {{else if eq $c.Status "success"}}
          <p class="ok">Last backup succeeded</p>
          <p>{{$c.When}} · {{$c.Size}}</p>
          <p>{{$c.Database}}</p>
          <p>Restore confidence: {{$c.Confidence}}</p>
        {{else if eq $c.Status "failed"}}
          <p class="bad">Last backup failed</p>
          <p>{{$c.Error}}</p>
          {{if $c.Hint}}<p>{{$c.Hint}}</p>{{end}}
        {{else}}
          <p>No backup yet.</p>
          <p class="muted">{{$c.Database}}</p>
        {{end}}
        <p><a class="button" href="{{$c.Href}}">Open site</a></p>
      </div>
    </details>
  </li>
{{end}}
</ul>
<p class="tools">restic: {{.Tools}}</p>
{{template "shell-end" .}}
{{end}}

{{define "register"}}
{{template "shell-start" .}}
<h1>Register a site</h1>
<p class="lede">Prefer <a href="/setup">Set up a backup</a> — it walks the same decisions with fewer dead ends.</p>
{{if .FormErr}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
<form class="card" method="post" action="/register">
  <label for="name">Name</label>
  <input id="name" name="name" required placeholder="Client site">
  <label for="path">Directory on this machine</label>
  <input id="path" name="path" required placeholder="/home/you/sites/example">
  <button type="submit" data-pending="Registering…">Register</button>
</form>
{{template "shell-end" .}}
{{end}}
{{define "site"}}
{{template "shell-start" .}}
<h1>{{.Site.Name}}</h1>
{{if eq .Site.LocationKind "sftp"}}
<p class="muted">{{.Site.Adapter}} · {{.Site.LocationUser}}@{{.Site.LocationHost}}:{{.Site.LocationPath}}</p>
<p class="muted">Local copy: {{.Site.Root}}</p>
{{else}}
<p class="muted">{{.Site.Adapter}} · {{.Site.Root}}</p>
{{end}}
{{if .Site.Config}}<p class="muted">Config file: {{.Site.Config}}</p>{{end}}
<form method="post" action="/sites/{{.Site.ID}}/name">
  <label for="site_name">Name</label>
  <input id="site_name" name="name" value="{{.Site.Name}}" required>
  <p class="muted">The name is a label. The id stays {{.Site.ID}}.</p>
  <button class="secondary" type="submit" data-pending="Saving name…">Save name</button>
</form>
{{if and .FormErr (ne .Focus "destinations")}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
{{if .Notice}}<p class="notice{{if .NoticeBad}} bad{{end}}" role="status">{{.Notice}}</p>{{end}}
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
<section class="card" id="backup-progress"{{if or .Watch (eq .Site.Status "running")}} data-progress-url="/sites/{{.Site.ID}}/progress"{{end}}>
  <h2>Backup</h2>
  <div data-progress-panel{{if and (ne .Site.Status "running") (not .Watch)}} hidden{{end}}>
    <p class="notice" role="status" data-progress-status>Backup in progress{{if .Site.When}} since {{.Site.When}}{{end}}.</p>
    <p class="notice" role="status" data-progress-long-notice{{if not .Site.Progress.Notice}} hidden{{end}}>{{.Site.Progress.Notice}}</p>
    {{template "progress" .Site.Progress}}
  </div>
  <div data-progress-result{{if eq .Site.Status "running"}} hidden{{end}}>
  {{if eq .Site.Status "success"}}
    <p class="ok">Last backup succeeded</p>
    <p>{{.Site.When}}</p>
    <p>{{.Site.Size}}</p>
    <p>{{.Site.Database}}</p>
    <p>Restore confidence: {{.Site.Confidence}}{{if eq .Site.Confidence "never"}}. Check restore proves a scratch copy matches this backup. It has not been run.{{end}}</p>
    {{if .Site.DestWarning}}<p class="bad">{{.Site.DestWarning}}</p>{{end}}
  {{else if eq .Site.Status "failed"}}
    <p class="bad">Last backup failed</p>
    <p>{{.Site.Error}}</p>
    {{if .Site.Hint}}<p>{{.Site.Hint}}</p>{{end}}
    <p class="muted">The live site was not modified.</p>
  {{else if ne .Site.Status "running"}}
    <p>No backup yet. The first run stays in the local restic repository.</p>
  {{end}}
  </div>
  <h3>Stored backups on this machine</h3>
  <p class="muted">Local restic repository: <code>{{.Site.LocalRepo}}</code>. Destinations below are separate copies of a finished backup, not this list.</p>
  {{if .Site.Backups}}
    <ul>
    {{range .Site.Backups}}
      <li>{{.When}} · {{.Size}} · Restore {{.Confidence}}</li>
    {{end}}
    </ul>
  {{else}}
    <p class="muted">None yet in the local repository.</p>
  {{end}}
  <form method="post" action="/sites/{{.Site.ID}}/backup">
    <button type="submit" data-pending="Starting backup…" {{if or .Watch (eq .Site.Status "running")}}disabled{{end}} data-progress-button>{{if or .Watch (eq .Site.Status "running")}}Backup in progress…{{else}}Back up now{{end}}</button>
  </form>
  {{if .Site.CanCheckRestore}}
  <div id="restore-check"{{if .Site.RestoreRunning}} data-restore-url="/sites/{{.Site.ID}}/restore-status"{{end}}>
  <p data-restore-status class="muted">{{if .Site.RestoreRunning}}Restore check in progress.{{else if .Site.RestoreError}}{{.Site.RestoreError}}{{else if .Site.RestoreScratch}}Scratch copy: {{.Site.RestoreScratch}}{{end}}</p>
  <form method="post" action="/sites/{{.Site.ID}}/test-restore">
    <button class="secondary" type="submit" data-pending="Starting restore check…" {{if or (eq .Site.Status "running") .Site.RestoreRunning}}disabled{{end}}>Check restore</button>
  </form>
  </div>
  <p class="muted">Uses the last successful backup. Does not run a new backup. Restores into an empty scratch folder, checks every file against the manifest hashes, checks the database dump when one was captured, and confirms the live site was not modified.</p>
  {{else}}
  <p class="muted">After the first successful backup, Check restore can verify that archive without backing up again.</p>
  {{end}}
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
<section class="card" id="destinations">
  <h2>Destinations</h2>
  <p class="muted">The local repository is always written. A destination is where a finished archive is copied. Destination kinds are extensions (JSON under <code>destinations/</code>). A destination is not the server the site is read from. Saving checks required fields and that the destination is reachable enough to use.</p>
  {{if and .FormErr (eq .Focus "destinations")}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
  {{if .Site.Destinations}}
    <ul class="dest-list">
    {{range $dest := .Site.Destinations}}
      <li>
        <div>{{$dest.Kind}} {{$dest.ID}}{{if $dest.Enabled}} · enabled{{else}} · saved, not uploaded{{end}}{{if $dest.Status}} · last run {{$dest.Status}}{{end}}<br><span class="muted">{{$dest.Detail}}</span>{{if not $dest.Enabled}}<br><span class="bad">Not uploaded until you enable it.</span>{{end}}{{if $dest.Reason}}<br><span class="bad">{{$dest.Reason}}</span>{{end}}</div>
        {{if $dest.Removable}}
        <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{$dest.ID}}">
          {{range $dest.Fields}}
          <label for="edit-{{$dest.ID}}-{{.Name}}">{{.Label}}</label>
          {{if .Choices}}
          <select id="edit-{{$dest.ID}}-{{.Name}}" name="{{.Name}}"{{if .Required}} required{{end}}>
            <option value="">Choose…</option>
            {{$val := .Current}}
            {{range .Options $val}}<option value="{{.}}"{{if eq $val .}} selected{{end}}>{{.}}</option>{{end}}
          </select>
          {{else}}
          <input id="edit-{{$dest.ID}}-{{.Name}}" name="{{.Name}}" value="{{.Current}}"{{if .Required}} required{{end}}>
          {{end}}
          {{end}}
          {{if $dest.HasCredentials}}
          <label for="edit-{{$dest.ID}}-access">{{$dest.AccessLabel}}</label>
          <input id="edit-{{$dest.ID}}-access" name="access_key" autocomplete="off" placeholder="Leave blank to keep the current key">
          <label for="edit-{{$dest.ID}}-secret">{{$dest.SecretLabel}}</label>
          <input id="edit-{{$dest.ID}}-secret" name="secret_key" type="password" autocomplete="off" placeholder="Leave blank to keep the current key">
          {{end}}
          <label class="check"><input type="checkbox" name="enabled"{{if $dest.Enabled}} checked{{end}}> Enable this destination</label>
          <button class="secondary" type="submit" data-pending="Saving destination…">Save destination</button>
        </form>
        <div class="dest-actions">
          {{if $dest.Enabled}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{$dest.ID}}/enabled">
            <button class="secondary" type="submit" data-pending="Disabling…">Disable</button>
          </form>
          {{else}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{$dest.ID}}/enabled">
            <input type="hidden" name="enabled" value="on">
            <button class="secondary" type="submit" data-pending="Enabling…">Enable</button>
          </form>
          {{end}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{$dest.ID}}/remove">
            <button class="secondary" type="submit" data-pending="Removing…">Remove</button>
          </form>
        </div>
        {{end}}
      </li>
    {{end}}
    </ul>
  {{else}}
    <p>No destinations saved.</p>
  {{end}}
  <form method="post" action="/sites/{{.Site.ID}}/destinations">
    <label for="kind">Kind</label>
    <select id="kind" name="kind" data-switch>
      {{range $.DestKinds}}<option value="{{.ID}}"{{if $.DestDraft.Selected .ID}} selected{{end}}>{{.Label}}</option>{{end}}
    </select>
    {{range $kind := $.DestKinds}}
    <div data-for="{{$kind.ID}}">
      {{if $kind.Note}}<p class="muted">{{$kind.Note}}</p>{{end}}
      {{range $kind.Fields}}
      <label for="dest-{{$kind.ID}}-{{.Name}}">{{.Label}}</label>
      {{if .Choices}}
      <select id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}"{{if .Required}} required{{end}}>
        <option value="">Choose a remote…</option>
        {{$draft := $.DestDraft.Field $kind.ID .Name}}
        {{$val := .Value $draft}}
        {{range .Options $draft}}<option value="{{.}}"{{if eq $val .}} selected{{end}}>{{.}}</option>{{end}}
      </select>
      {{else}}
      <input id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}" value="{{$.DestDraft.Field $kind.ID .Name}}"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}>
      {{if eq .Name "host"}}{{if eq $kind.ID "gdrive"}}<p class="muted">No Google Drive remotes found. Run <code>rclone config</code> and create a drive remote, then reload this page.</p>{{end}}{{end}}
      {{end}}
      {{end}}
      {{if $kind.HasCredentials}}
      <label for="dest-{{$kind.ID}}-access">{{$kind.AccessLabel}}</label>
      <input id="dest-{{$kind.ID}}-access" name="access_key" autocomplete="off">
      <label for="dest-{{$kind.ID}}-secret">{{$kind.SecretLabel}}</label>
      <input id="dest-{{$kind.ID}}-secret" name="secret_key" type="password" autocomplete="off">
      {{end}}
    </div>
    {{end}}
    <label class="check"><input type="checkbox" name="enabled"{{if and .DestDraft .DestDraft.Enabled}} checked{{end}}> Enable this destination</label>
    <p class="muted">Leave this unchecked and the copy is saved but not used.</p>
    <button type="submit" data-pending="Saving destination…">Save destination</button>
  </form>
</section>
<section class="card">
  <h2>Schedule</h2>
  <p class="muted">Saving a schedule does not start a timer. A system cron must run <code>run_scheduled</code> every minute. That command runs the same backup path, with no agent in the loop. This line is not installed:</p>
  <p><code>{{.Site.ScheduleLine}}</code></p>
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
      var show = el.getAttribute("data-for").split(" ").indexOf(kind) !== -1;
      el.hidden = !show;
      el.querySelectorAll("input, select, textarea").forEach(function (input) {
        input.disabled = !show;
      });
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
  {{if eq .Focus "destinations"}}
  var destSection = document.getElementById("destinations");
  if (destSection) {
    destSection.scrollIntoView({ block: "start" });
    var destFocus = destSection.querySelector("select[data-switch]") || destSection.querySelector("input:not([disabled]):not([type=hidden])");
    if (destFocus) { destFocus.focus(); }
  }
  {{end}}
  var progressRoot = document.getElementById("backup-progress");
  var progressURL = progressRoot && progressRoot.getAttribute("data-progress-url");
  if (progressURL) {
    progressRoot.scrollIntoView({ block: "start" });
    function setText(el, text) {
      if (!el) return;
      if (!text) { el.hidden = true; el.textContent = ""; return; }
      el.hidden = false;
      el.textContent = text;
    }
    function renderProgress(data) {
      var panel = progressRoot.querySelector("[data-progress-panel]");
      var result = progressRoot.querySelector("[data-progress-result]");
      var button = progressRoot.querySelector("[data-progress-button]");
      if (data.running) {
        if (panel) panel.hidden = false;
        if (result) result.hidden = true;
        if (button) { button.disabled = true; button.textContent = "Backup in progress…"; }
        setText(progressRoot.querySelector("[data-progress-phase]"), data.phase || "Starting the backup");
        setText(progressRoot.querySelector("[data-progress-status]"), data.started_at ? "Backup in progress since " + data.started_at + "." : "Backup in progress.");
        var pctEl = progressRoot.querySelector("[data-progress-pct]");
        if (pctEl) {
          if (data.known) {
            pctEl.hidden = false;
            pctEl.textContent = data.percent + "%";
          } else {
            pctEl.hidden = true;
            pctEl.textContent = "";
          }
        }
        setText(progressRoot.querySelector("[data-progress-long-notice]"), data.long_notice || "");
        setText(progressRoot.querySelector("[data-progress-eta]"), data.eta ? ("Estimated time left: " + data.eta) : "");
        setText(progressRoot.querySelector("[data-progress-eta-detail]"), data.eta_detail || "");
        setText(progressRoot.querySelector("[data-progress-file]"), data.file || "");
        setText(progressRoot.querySelector("[data-progress-count]"), data.count || "");
        setText(progressRoot.querySelector("[data-progress-written]"), data.written || "");
        setText(progressRoot.querySelector("[data-progress-limit]"), data.limit || "");
        var bar = progressRoot.querySelector("[data-progress-bar]");
        var fill = progressRoot.querySelector("[data-progress-fill]");
        if (bar && fill) {
          if (data.known) {
            bar.classList.remove("unknown");
            bar.setAttribute("aria-valuenow", String(data.percent));
            fill.style.width = data.percent + "%";
          } else {
            bar.classList.add("unknown");
            bar.removeAttribute("aria-valuenow");
            fill.style.width = "";
          }
          bar.setAttribute("aria-valuetext", (data.phase || "Starting the backup") + (data.known ? " " + data.percent + "%" : ""));
        }
        return true;
      }
      window.location.replace("/sites/{{.Site.ID}}");
      return false;
    }
    function tick() {
      fetch(progressURL, { headers: { "Accept": "application/json" }, cache: "no-store" })
        .then(function (res) { return res.json(); })
        .then(function (data) {
          if (renderProgress(data)) {
            window.setTimeout(tick, 1000);
          }
        })
        .catch(function () { window.setTimeout(tick, 2000); });
    }
    tick();
  }
  var restoreRoot = document.getElementById("restore-check");
  var restoreURL = restoreRoot && restoreRoot.getAttribute("data-restore-url");
  if (restoreURL) {
    function restoreTick() {
      fetch(restoreURL, { headers: { "Accept": "application/json" }, cache: "no-store" })
        .then(function (res) { return res.json(); })
        .then(function (data) {
          var line = restoreRoot.querySelector("[data-restore-status]");
          if (!line) return;
          if (data.in_progress) {
            line.textContent = "Restore check in progress.";
            window.setTimeout(restoreTick, 1000);
            return;
          }
          if (data.error) {
            line.textContent = data.error;
            return;
          }
          if (data.scratch) {
            line.textContent = "Restore check passed. Scratch copy: " + data.scratch;
            return;
          }
          line.textContent = "Restore check finished.";
        })
        .catch(function () { window.setTimeout(restoreTick, 2000); });
    }
    restoreTick();
  }
})();
</script>
<section class="card">
  <h2>Remove this site</h2>
  <p class="muted">This drops the site from the registry. The local restic repository stays at <code>{{.Site.LocalRepo}}</code>.</p>
  <form method="post" action="/sites/{{.Site.ID}}/remove">
    <button class="secondary" type="submit" data-pending="Removing site…">Remove this site</button>
  </form>
</section>
{{if .Site.Steps}}
<section class="card">
  <h2>Restore procedure</h2>
  <pre>{{range .Site.Steps}}{{.}}
{{end}}</pre>
</section>
{{end}}

{{template "shell-end" .}}
{{end}}
{{define "setup"}}
{{template "shell-start" .}}
<h1>Start a backup</h1>
<p class="lede">Choose a directory on this machine or one server. Matching site types apply automatically; anything else is files-only. Staying local is fine — destinations are optional.</p>
{{if .FormErr}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
{{if .Notice}}<p class="notice{{if .NoticeBad}} bad{{end}}" role="status">{{.Notice}}</p>{{end}}
{{with .Wizard}}
{{if .Back}}<p><a href="/sites/{{.ID}}/setup/{{.Back}}">Back</a></p>{{end}}
{{if eq .Step "location"}}
<form class="card" method="post" action="{{if .ID}}/sites/{{.ID}}/setup/location{{else}}/setup{{end}}">
  <h2>Where the files are</h2>
  <p class="muted">A site is read from one place. Choose a directory on this machine, or one server. SSH uses the keys already on this machine. A password is not asked.</p>
  <label for="location_kind">Location</label>
  <select id="location_kind" name="kind" data-switch>
    <option value="local" {{if ne .LocationKind "sftp"}}selected{{end}}>Directory on this machine</option>
    <option value="sftp" {{if eq .LocationKind "sftp"}}selected{{end}}>One server (SFTP)</option>
  </select>
  <div data-for="local">
    <label for="local_path">Directory</label>
    <input id="local_path" name="local_path" value="{{.LocalPath}}" placeholder="/home/you/sites/example">
  </div>
  <div data-for="sftp">
    <label for="remote_host">SSH host</label>
    <input id="remote_host" name="host" value="{{.Host}}">
    <label for="remote_user">User</label>
    <input id="remote_user" name="user" value="{{.User}}">
    <label for="remote_path">Absolute path on the server</label>
    <input id="remote_path" name="remote_path" value="{{.RemotePath}}" placeholder="/var/www/example">
  </div>
  <button type="submit" data-pending="Saving location…">Continue</button>
  <p class="hint">Continuing measures how large the site looks (for a server, an rsync dry-run) so you see the likely backup size before the first run.</p>
</form>
{{else if eq .Step "name"}}
<form class="card" method="post" action="/sites/{{.ID}}/setup/name">
  <h2>Name</h2>
  <p class="muted">Detected as {{.Adapter}}. The name is a label. The files stay at {{.Root}}.</p>
  <label for="site_name">Name</label>
  <input id="site_name" name="name" required value="{{.Name}}">
  <button type="submit" data-pending="Saving name…">Continue</button>
</form>
{{else if eq .Step "database"}}
<form class="card" method="post" action="/sites/{{.ID}}/setup/database">
  <h2>Database</h2>
  <p class="muted">{{.DBNote}}</p>
  <label class="check"><input type="checkbox" name="enabled" {{if .DBEnabled}}checked{{end}} data-switch-check> Include a database dump in each backup</label>
  <div data-when-checked>
    <label for="db_name">Database name</label>
    <input id="db_name" name="name" value="{{.DBName}}" autocomplete="off">
    <label for="db_user">Database user</label>
    <input id="db_user" name="user" value="{{.DBUser}}" autocomplete="off">
    <label for="db_host">Database host</label>
    <input id="db_host" name="host" value="{{.DBHost}}" placeholder="localhost" autocomplete="off">
    <label for="db_password">Database password</label>
    <input id="db_password" name="password" type="password" autocomplete="new-password" placeholder="Leave blank to keep the current password">
    <p class="muted">The password is stored under this tool's secrets directory. It is never shown again.</p>
  </div>
  <button type="submit" data-pending="Saving database settings…">Continue</button>
</form>
{{else if eq .Step "destination"}}
<form class="card" id="destinations" method="post" action="/sites/{{.ID}}/setup/destination">
  <h2>Where copies go</h2>
  <p class="muted">The local repository is always written. A destination is where a finished archive is copied. It is not the server the files are read from. Saving checks required fields and that the destination is reachable enough to use.</p>
  <label for="kind">Kind</label>
  <select id="kind" name="kind" data-switch>
    <option value="none"{{if or (not $.DestDraft) (eq $.DestDraft.Kind "") (eq $.DestDraft.Kind "none")}} selected{{end}}>This machine only</option>
    {{range $.DestKinds}}<option value="{{.ID}}"{{if $.DestDraft.Selected .ID}} selected{{end}}>{{.Label}}</option>{{end}}
  </select>
  {{range $kind := $.DestKinds}}
  <div data-for="{{$kind.ID}}">
    {{if $kind.Note}}<p class="muted">{{$kind.Note}}</p>{{end}}
    {{range $kind.Fields}}
    <label for="dest-{{$kind.ID}}-{{.Name}}">{{.Label}}</label>
    {{if .Choices}}
    <select id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}"{{if .Required}} required{{end}}>
      <option value="">Choose a remote…</option>
      {{$draft := $.DestDraft.Field $kind.ID .Name}}
      {{$val := .Value $draft}}
      {{range .Options $draft}}<option value="{{.}}"{{if eq $val .}} selected{{end}}>{{.}}</option>{{end}}
    </select>
    {{else}}
    <input id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}" value="{{$.DestDraft.Field $kind.ID .Name}}"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}>
    {{if eq .Name "host"}}{{if eq $kind.ID "gdrive"}}<p class="muted">No Google Drive remotes found. Run <code>rclone config</code> and create a drive remote, then reload this page.</p>{{end}}{{end}}
    {{end}}
    {{end}}
    {{if $kind.HasCredentials}}
    <label for="dest-{{$kind.ID}}-access">{{$kind.AccessLabel}}</label>
    <input id="dest-{{$kind.ID}}-access" name="access_key" autocomplete="off">
    <label for="dest-{{$kind.ID}}-secret">{{$kind.SecretLabel}}</label>
    <input id="dest-{{$kind.ID}}-secret" name="secret_key" type="password" autocomplete="off">
    {{end}}
  </div>
  {{end}}
  <label class="check"><input type="checkbox" name="enabled"{{if and $.DestDraft $.DestDraft.Enabled}} checked{{end}}> Enable this destination</label>
  <p class="muted">Leave this unchecked and the copy is saved but not used.</p>
  <button type="submit" data-pending="Saving destination…">Continue</button>
</form>
{{else if eq .Step "schedule"}}
<form class="card" method="post" action="/sites/{{.ID}}/setup/schedule">
  <h2>Schedule</h2>
  <p class="muted">Saving a schedule does not start a timer. A system cron must run <code>run_scheduled</code> every minute. Leave the box unchecked to keep the schedule off. This line is not installed:</p>
  <p><code>{{.ScheduleLine}}</code></p>
  <label for="cron">Cron (minute hour day month weekday)</label>
  <input id="cron" name="cron" value="{{.Cron}}">
  <label class="check"><input type="checkbox" name="enabled" {{if .CronOn}}checked{{end}}> Enabled</label>
  <button type="submit" data-pending="Saving schedule…">Continue</button>
</form>
{{else if eq .Step "backup"}}
<form class="card" method="post" action="/sites/{{.ID}}/setup/backup">
  <h2>First backup</h2>
  <p class="muted">The first backup stays in the local restic repository. Nothing is uploaded unless you enabled a destination.</p>
  <div class="actions">
    <button type="submit" data-pending="Backup in progress…">Back up now</button>
    <button class="secondary" type="submit" name="skip" value="1">Skip for now</button>
  </div>
</form>
{{end}}
{{if .Config}}
<p class="muted">Config file: {{.Config}}</p>
<p class="muted"><a href="{{.Edit}}">Retention and speed limits are on the site page.</a></p>
{{end}}
{{end}}

<script>
(function () {
  function apply(select) {
    var kind = select.value;
    select.form.querySelectorAll("[data-for]").forEach(function (el) {
      var show = el.getAttribute("data-for").split(" ").indexOf(kind) !== -1;
      el.hidden = !show;
      el.querySelectorAll("input, select, textarea").forEach(function (input) {
        input.disabled = !show;
      });
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
  {{if eq .Focus "destinations"}}
  var destSection = document.getElementById("destinations");
  if (destSection) {
    destSection.scrollIntoView({ block: "start" });
    var destFocus = destSection.querySelector("select[data-switch]") || destSection.querySelector("input:not([disabled]):not([type=hidden])");
    if (destFocus) { destFocus.focus(); }
  }
  {{end}}
})();
</script>
{{template "shell-end" .}}
{{end}}
{{define "progress"}}
<p data-progress-phase>{{if .Phase}}{{.Phase}}{{else}}Starting the backup{{end}}</p>
<p class="progress-pct" data-progress-pct{{if not .Known}} hidden{{end}}>{{if .Known}}{{.Percent}}%{{end}}</p>
<div class="bar{{if not .Known}} unknown{{end}}" role="progressbar" aria-valuemin="0" aria-valuemax="100"{{if .Known}} aria-valuenow="{{.Percent}}"{{end}} aria-valuetext="{{if .Phase}}{{.Phase}}{{else}}Starting the backup{{end}}" data-progress-bar>
  <span data-progress-fill{{if .Known}} style="width: {{.Percent}}%"{{end}}></span>
</div>
<p data-progress-eta{{if not .ETA}} hidden{{end}}>{{if .ETA}}Estimated time left: {{.ETA}}{{end}}</p>
<p class="muted" data-progress-eta-detail{{if not .Detail}} hidden{{end}}>{{.Detail}}</p>
<p class="muted" data-progress-file{{if not .File}} hidden{{end}}>{{.File}}</p>
<p class="muted" data-progress-count{{if not .Count}} hidden{{end}}>{{.Count}}</p>
<p class="muted" data-progress-written{{if not .Written}} hidden{{end}}>{{.Written}}</p>
<p class="muted" data-progress-limit{{if not .Limit}} hidden{{end}}>{{.Limit}}</p>
{{end}}

{{define "feedback"}}
<style>
  .notice { background: #e7f2ef; border: 2px solid var(--accent, #0f5c57); border-radius: 8px; padding: 0.85rem 1rem; margin: 0.75rem 0; font-weight: 600; }
  .notice.bad { background: #f8ecec; border-color: var(--bad, #8c2f2f); color: var(--bad, #8c2f2f); }
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
