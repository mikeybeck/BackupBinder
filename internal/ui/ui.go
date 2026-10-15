// Package ui is the localhost site list. It calls the same backup path as the CLI.
package ui

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"local-backup/internal/app"
	"local-backup/internal/destination"
	"local-backup/internal/fail"
	"local-backup/internal/model"
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
	mux.HandleFunc("POST /sites/{id}/backup", ui.backup)
	mux.HandleFunc("POST /sites/{id}/destinations", ui.destination)
	mux.HandleFunc("POST /sites/{id}/destinations/{dest}/enabled", ui.destinationEnabled)
	mux.HandleFunc("POST /sites/{id}/destinations/{dest}/remove", ui.destinationRemove)
	mux.HandleFunc("POST /sites/{id}/test-restore", ui.testRestore)
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
	Empty     bool
	Cards     []card
	Site      *siteView
	Notice    string
	NoticeBad bool
	Refresh   bool
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
	DestWarning     string
	Config          string
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
	ID        string
	Kind      string
	Enabled   bool
	Status    string
	Detail    string
	Reason    string
	Removable bool
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
	view := page{Title: "Sites", Empty: len(sites) == 0, Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1", Tools: s.app.Restic.Binary}
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
	s.render(w, "site", page{
		Title: view.Name, Site: view, Notice: r.URL.Query().Get("notice"), NoticeBad: r.URL.Query().Get("bad") == "1",
		Refresh: view.Status == "running", Tools: s.app.Restic.Binary, DestKinds: destKindViews(),
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
			item.Fields = append(item.Fields, destFieldView{Name: field.Name, Label: field.Label, Placeholder: field.Placeholder})
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
		UploadKiB: formatKiB(inspected.Site.Limits.UploadKiB), DownloadKiB: formatKiB(inspected.Site.Limits.DownloadKiB),
		KeepLast: formatCount(inspected.Site.Retention.KeepLast), KeepHourly: formatCount(inspected.Site.Retention.KeepHourly),
		KeepDaily: formatCount(inspected.Site.Retention.KeepDaily), KeepWeekly: formatCount(inspected.Site.Retention.KeepWeekly),
		KeepMonthly: formatCount(inspected.Site.Retention.KeepMonthly), KeepYearly: formatCount(inspected.Site.Retention.KeepYearly),
		LocationKind: inspected.Site.Location.Kind, LocationHost: inspected.Site.Location.Host,
		LocationUser: inspected.Site.Location.User, LocationPath: inspected.Site.Location.Path,
		Config: filepath.Join(s.app.Home, "sites", inspected.Site.ID+".json"),
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
	for _, dest := range inspected.Site.Destinations {
		detail := dest.Kind
		if kind, err := destination.ByID(dest.Kind); err == nil {
			detail = kind.DetailLabel(dest)
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
		view.Destinations = append(view.Destinations, destView{
			ID: dest.ID, Kind: dest.Kind, Enabled: dest.Enabled, Status: statusByID[dest.ID],
			Detail: detail, Reason: reasonByID[dest.ID], Removable: dest.Kind != "local_restic",
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
		view, ok := s.siteView(w, r)
		if !ok {
			return
		}
		s.render(w, "site", page{
			Title: view.Name, Site: view, FormErr: "Destination was not saved. "+noticeText(err),
			Tools: s.app.Restic.Binary, DestKinds: destKindViews(), DestDraft: destDraftFrom(r), Focus: "destinations",
		})
		return
	}
	s.redirectNotice(w, r, "/sites/"+id, "Destination saved. It is not contacted until you enable it.", false)
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
		s.redirectNotice(w, r, "/sites/"+id+"#destinations", "Destination was not updated. "+noticeText(err), true)
		return
	}
	notice := "Destination " + destID + " is saved only until you enable it."
	if enabled {
		notice = "Destination " + destID + " is enabled for the next backup."
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
	http.Redirect(w, r, setupRedirect(result), http.StatusSeeOther)
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
	http.Redirect(w, r, setupRedirect(result), http.StatusSeeOther)
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
		view.DBNote = "The site type is read after the first copy. Fill these fields to include a dump now, or leave the box unchecked for files only."
	} else {
		view.DBNote = "No database was detected. Check the box and fill the fields to include a dump, or leave it unchecked for files only."
	}
	view.Cron = site.Schedule.Cron
	if view.Cron == "" {
		view.Cron = "15 3 * * *"
	}
	view.CronOn = site.Schedule.Enabled
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

func setupRedirect(result app.SetupResult) string {
	if result.Phase == "ready" || app.NextStep(result.Setup) == "" {
		return "/sites/" + result.SiteID
	}
	return "/sites/" + result.SiteID + "/setup"
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
    <p>Choose a directory on this computer, or one server. A matching addon, including WordPress, is applied automatically. Anything else is backed up as files. Nothing is uploaded until you enable a destination.</p>
    <a class="button" href="/setup">Set up a backup</a>
  </section>
{{else}}
  <p><a class="button" href="/setup">Set up a backup</a></p>
  {{range .Cards}}
  <article class="card">
    <h2><a href="{{.Href}}">{{.Name}}</a></h2>
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
  .dest-list { list-style: none; padding: 0; margin: 0.5rem 0 1rem; }
  .dest-list > li { border-top: 1px solid #e7e1d6; padding: 0.75rem 0; }
  .dest-list > li:first-child { border-top: 0; padding-top: 0; }
  .dest-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-top: 0.5rem; }
  .dest-actions form { margin: 0; width: auto; }
  .dest-actions button { width: auto; margin: 0; padding: 0.4rem 0.7rem; font-size: 0.9rem; }
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
{{if .Site.Config}}<p class="muted">Config file: {{.Site.Config}}</p>{{end}}
{{if .FormErr}}<p class="notice bad" role="status">{{.FormErr}}</p>{{end}}
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
    {{if .Site.DestWarning}}<p class="bad">{{.Site.DestWarning}}</p>{{end}}
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
<section class="card" id="destinations">
  <h2>Destinations</h2>
  <p class="muted">The local repository is always written. A destination is where a finished archive is copied. Destination kinds are extensions (JSON under <code>destinations/</code>). A destination is not the server the site is read from. It is not contacted until you enable it.</p>
  {{if .Site.Destinations}}
    <ul class="dest-list">
    {{range .Site.Destinations}}
      <li>
        <div>{{.Kind}} {{.ID}}{{if .Enabled}} · enabled{{else}} · saved{{end}}{{if .Status}} · last run {{.Status}}{{end}}<br><span class="muted">{{.Detail}}</span>{{if .Reason}}<br><span class="bad">{{.Reason}}</span>{{end}}</div>
        {{if .Removable}}
        <div class="dest-actions">
          {{if .Enabled}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{.ID}}/enabled">
            <button class="secondary" type="submit" data-pending="Disabling…">Disable</button>
          </form>
          {{else}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{.ID}}/enabled">
            <input type="hidden" name="enabled" value="on">
            <button class="secondary" type="submit" data-pending="Enabling…">Enable</button>
          </form>
          {{end}}
          <form method="post" action="/sites/{{$.Site.ID}}/destinations/{{.ID}}/remove">
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
      <input id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}" value="{{$.DestDraft.Field $kind.ID .Name}}"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}>
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

{{define "setup"}}
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Set up a backup</title>
<style>
  body { margin: 0; background: #f3efe6; color: #1c1915; font: 16px/1.45 system-ui, sans-serif; }
  main { max-width: 40rem; margin: 0 auto; padding: 1rem; }
  h1 { font-size: 1.5rem; }
  p, code { overflow-wrap: anywhere; }
  .card { background: #fffdf8; border: 1px solid #d9d2c5; border-radius: 8px; padding: 1rem; margin: 0.75rem 0; }
  label, input, select, button { display: block; width: 100%; box-sizing: border-box; margin: 0.35rem 0; font: inherit; }
  button, .button { background: #0f5c57; color: white; border: 0; border-radius: 6px; padding: 0.65rem 0.8rem; text-align: center; text-decoration: none; }
  .secondary { background: transparent; color: #0f5c57; border: 1px solid #0f5c57; }
  .muted { color: #5c564c; }
  .bad { color: #8c2f2f; }
  .check { display: flex; align-items: center; gap: 0.5rem; }
  .check input { width: auto; }
  .actions { display: flex; gap: 0.5rem; }
  .actions button { width: auto; }
</style>
</head>
<body>
<main>
<p><a href="/">Sites</a></p>
<h1>Set up a backup</h1>
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
  <p class="muted">The local repository is always written. A destination is where a finished archive is copied. It is not the server the files are read from, and it is not contacted until you enable it.</p>
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
    <input id="dest-{{$kind.ID}}-{{.Name}}" name="{{.Name}}" value="{{$.DestDraft.Field $kind.ID .Name}}"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}>
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
  <button type="submit" data-pending="Saving destination…">Continue</button>
</form>
{{else if eq .Step "schedule"}}
<form class="card" method="post" action="/sites/{{.ID}}/setup/schedule">
  <h2>Schedule</h2>
  <p class="muted">Saving a schedule does not start a timer. A system cron must run <code>run_scheduled</code> every minute. Leave the box unchecked to keep the schedule off.</p>
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
</main>
{{template "feedback" .}}
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
