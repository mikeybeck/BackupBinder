package app

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"backupbinder/internal/archive"
	"backupbinder/internal/discover"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
	"backupbinder/internal/schedule"
	"backupbinder/internal/store"
)

// Inspect is a read of the contract plus what the directory looks like now.
type Inspect struct {
	OK                 bool              `json:"ok"`
	Site               model.Site        `json:"site"`
	Detected           model.Versions    `json:"detected"`
	LastRun            *RunSummary       `json:"last_run"`
	Restore            *RestoreStatus    `json:"restore,omitempty"`
	RestoreConfidence  string            `json:"restore_confidence"`
	Tools              map[string]string `json:"tools"`
	ScheduleInvocation string            `json:"schedule_invocation"`
}

type RunSummary struct {
	RunID             string          `json:"run_id"`
	OK                bool            `json:"ok"`
	StartedAt         string          `json:"started_at"`
	SizeBytes         int64           `json:"size_bytes,omitempty"`
	SnapshotID        string          `json:"snapshot_id,omitempty"`
	RestoreConfidence string          `json:"restore_confidence,omitempty"`
	Error             string          `json:"error,omitempty"`
	Hint              string          `json:"hint,omitempty"`
	Progress          *model.Progress `json:"progress,omitempty"`
}

// SiteEntry is one registered site. Location is always a local directory.
// A name that looks like a hostname is only a label.
type SiteEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Root     string `json:"root"`
	Adapter  string `json:"adapter"`
	Location string `json:"location"`
	Note     string `json:"note,omitempty"`
}

// SiteList is the registry. Sites is empty when nothing is registered.
type SiteList struct {
	OK    bool        `json:"ok"`
	Sites []SiteEntry `json:"sites"`
}

func (a *App) ListSites() (SiteList, error) {
	sites, err := a.DB.ListSites()
	if err != nil {
		return SiteList{}, err
	}
	entries := make([]SiteEntry, 0, len(sites))
	for _, site := range sites {
		entries = append(entries, siteEntry(site))
	}
	return SiteList{OK: true, Sites: entries}, nil
}

func (a *App) Inspect(siteID string) (Inspect, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return Inspect{}, err
	}
	a.decorate(&site)
	detected := model.Versions{Plugins: []model.Component{}, Themes: []model.Component{}}
	if _, err := os.Stat(site.Root); err == nil {
		detected = discover.Versions(site.Root, site.Adapter)
	}
	summary := a.summary(site.ID)
	confidence := "never"
	if summary != nil && summary.OK && summary.RestoreConfidence != "" {
		confidence = summary.RestoreConfidence
	}
	var restore *RestoreStatus
	if status, ok := a.ReadRestoreStatus(site.ID); ok {
		status.PID = 0
		restore = &status
	}
	return Inspect{
		OK: true, Site: site, Detected: detected, LastRun: summary, Restore: restore,
		RestoreConfidence: confidence, Tools: a.tools(),
		ScheduleInvocation: a.scheduleLine(),
	}, nil
}

func (a *App) decorate(site *model.Site) {
	for i := range site.Destinations {
		site.Destinations[i].Credentials = credentialLabel(site.Destinations[i].CredentialFile)
	}
}

func credentialLabel(path string) string {
	if path == "" {
		return ""
	}
	if credentialPresent(path) {
		return "present"
	}
	return "missing"
}

func (a *App) tools() map[string]string {
	look := func(name string) string {
		path, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return path
	}
	return map[string]string{
		"restic":    a.Restic.Binary,
		"ssh":       look("ssh"),
		"rsync":     look("rsync"),
		"rclone":    look("rclone"),
		"mysqldump": look("mysqldump"),
		"wp":        look("wp"),
		"php":       discover.FindPHP(),
	}
}

func (a *App) ScheduleLine() string {
	return a.scheduleLine()
}

func (a *App) scheduleLine() string {
	bin := "backupbinder"
	if exe, err := os.Executable(); err == nil && exe != "" {
		bin = exe
	}
	home := ""
	if a.Home != "" {
		home = " --home " + a.Home
	}
	return "* * * * * " + bin + home + " run_scheduled"
}

func (a *App) summary(siteID string) *RunSummary {
	run, err := a.Latest(siteID)
	if err != nil || run.RunID == "" {
		return nil
	}
	item := &RunSummary{RunID: run.RunID, OK: run.OK, StartedAt: run.StartedAt}
	if run.Manifest != nil {
		item.SizeBytes = run.Manifest.SizeBytes
		item.SnapshotID = run.Manifest.Snapshot.ResticSnapshotID
		item.RestoreConfidence = run.Manifest.Restore.Confidence
	}
	if run.Failure != nil {
		item.Error = run.Failure.Message
		item.Hint = run.Failure.Hint
	}
	if run.InProgress() {
		if p, ok := a.ReadProgress(siteID, run.RunID); ok {
			item.Progress = &p
		}
	}
	return item
}

type BackupList struct {
	OK      bool         `json:"ok"`
	SiteID  string       `json:"site_id"`
	Backups []RunSummary `json:"backups"`
}

func (a *App) ListBackups(siteID string) (BackupList, error) {
	if _, err := a.DB.GetSite(siteID); err != nil {
		return BackupList{}, err
	}
	runs, err := a.DB.ListRuns(siteID)
	if err != nil {
		return BackupList{}, err
	}
	list := BackupList{OK: true, SiteID: siteID, Backups: []RunSummary{}}
	for _, run := range runs {
		if !run.OK || run.Manifest == nil {
			continue
		}
		list.Backups = append(list.Backups, RunSummary{
			RunID: run.RunID, OK: true, StartedAt: run.StartedAt,
			SizeBytes: run.Manifest.SizeBytes, SnapshotID: run.Manifest.Snapshot.ResticSnapshotID,
			RestoreConfidence: run.Manifest.Restore.Confidence,
		})
	}
	return list, nil
}

type VerifyResult struct {
	OK              bool   `json:"ok"`
	RunID           string `json:"run_id"`
	SnapshotID      string `json:"snapshot_id"`
	Readable        bool   `json:"readable"`
	ResticCheck     string `json:"restic_check"`
	ManifestPresent bool   `json:"manifest_present"`
}

func (a *App) Verify(siteID, runID string) (VerifyResult, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return VerifyResult{}, err
	}
	run, err := a.successRun(site.ID, runID)
	if err != nil {
		return VerifyResult{}, err
	}
	password, err := a.passwordFile(site.ID)
	if err != nil {
		return VerifyResult{}, err
	}
	repo := a.repoPath(site)
	restic := a.resticFor(site)
	if err := restic.Check(repo, password); err != nil {
		return VerifyResult{}, err
	}
	nodes, err := restic.List(repo, password, run.Manifest.Snapshot.ResticSnapshotID)
	if err != nil {
		return VerifyResult{}, err
	}
	if err := snapshotTree(nodes, *run.Manifest); err != nil {
		return VerifyResult{}, err
	}
	if _, err := os.Stat(a.manifestPath(site.ID, run.RunID)); err != nil {
		return VerifyResult{}, fail.New("verify_failed", "The manifest file for this run is missing.")
	}
	return VerifyResult{
		OK: true, RunID: run.RunID, SnapshotID: run.Manifest.Snapshot.ResticSnapshotID,
		Readable: true, ResticCheck: "passed", ManifestPresent: true,
	}, nil
}

// snapshotTree checks the snapshot listing against the manifest inventory.
// The archive must contain those files, the staged manifest, and nothing else.
func snapshotTree(nodes []archive.Node, manifest model.Manifest) error {
	files := map[string]int64{}
	dirs := map[string]struct{}{}
	for _, node := range nodes {
		if node.Path == "" {
			return fail.New("verify_failed", "The snapshot listing is incomplete.")
		}
		switch node.Type {
		case "file":
			files[node.Path] = node.Size
		case "dir":
			dirs[node.Path] = struct{}{}
		default:
			return fail.New("verify_failed", "The snapshot contains an unexpected entry: "+node.Path+".")
		}
	}
	expectedFiles := map[string]int64{"/manifest.json": -1}
	expectedDirs := map[string]struct{}{"/files": {}}
	for _, item := range manifest.Captured.Files.Inventory {
		path, err := snapshotFilePath(item.Path)
		if err != nil {
			return err
		}
		expectedFiles[path] = item.Bytes
		addParents(expectedDirs, path)
	}
	if manifest.Captured.Database != nil {
		expectedFiles["/database/database.sql"] = manifest.Captured.Database.Bytes
		expectedDirs["/database"] = struct{}{}
	}
	var missing, extra, sized []string
	for path, size := range expectedFiles {
		got, ok := files[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		if size >= 0 && got != size {
			sized = append(sized, path)
		}
	}
	for path := range files {
		if _, ok := expectedFiles[path]; !ok {
			extra = append(extra, path)
		}
	}
	var missingDirs, extraDirs []string
	for path := range expectedDirs {
		if _, ok := dirs[path]; !ok {
			missingDirs = append(missingDirs, path)
		}
	}
	for path := range dirs {
		if _, ok := expectedDirs[path]; !ok {
			extraDirs = append(extraDirs, path)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	slices.Sort(sized)
	slices.Sort(missingDirs)
	slices.Sort(extraDirs)
	switch {
	case len(missing) > 0:
		return fail.New("verify_failed", "The snapshot is missing "+displayPath(missing[0])+".")
	case len(sized) > 0:
		return fail.New("verify_failed", "The snapshot size of "+displayPath(sized[0])+" does not match the manifest.")
	case len(extra) > 0:
		return fail.New("verify_failed", "The snapshot contains an unexpected file: "+extra[0]+".")
	case len(missingDirs) > 0:
		return fail.New("verify_failed", "The snapshot is missing "+displayPath(missingDirs[0])+".")
	case len(extraDirs) > 0:
		return fail.New("verify_failed", "The snapshot contains an unexpected directory: "+extraDirs[0]+".")
	}
	return nil
}

func snapshotFilePath(rel string) (string, error) {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fail.New("verify_failed", "The manifest inventory has an unsafe path.")
		}
	}
	return "/files/" + rel, nil
}

func addParents(dirs map[string]struct{}, filePath string) {
	dir := filePath
	for {
		i := strings.LastIndex(dir, "/")
		if i <= 0 {
			break
		}
		dir = dir[:i]
		dirs[dir] = struct{}{}
	}
}

func displayPath(path string) string {
	return strings.TrimPrefix(path, "/")
}

type CompareResult struct {
	OK             bool      `json:"ok"`
	Left           string    `json:"left"`
	Right          string    `json:"right"`
	Added          []string  `json:"added"`
	Removed        []string  `json:"removed"`
	Changed        []string  `json:"changed"`
	Truncated      bool      `json:"truncated"`
	FileCountDelta int       `json:"file_count_delta"`
	ByteSizeDelta  int64     `json:"byte_size_delta"`
	DatabaseBytes  [2]int64  `json:"database_bytes"`
	WordPress      [2]string `json:"wordpress"`
}

func (a *App) Compare(siteID, leftID, rightID string) (CompareResult, error) {
	if _, err := a.DB.GetSite(siteID); err != nil {
		return CompareResult{}, err
	}
	left, err := a.successRun(siteID, leftID)
	if err != nil {
		return CompareResult{}, err
	}
	right, err := a.successRun(siteID, rightID)
	if err != nil {
		return CompareResult{}, err
	}
	oldFiles := indexFiles(left.Manifest.Captured.Files.Inventory)
	newFiles := indexFiles(right.Manifest.Captured.Files.Inventory)
	result := CompareResult{
		OK: true, Left: left.RunID, Right: right.RunID,
		Added: []string{}, Removed: []string{}, Changed: []string{},
		FileCountDelta: right.Manifest.Captured.Files.FileCount - left.Manifest.Captured.Files.FileCount,
		ByteSizeDelta:  right.Manifest.Captured.Files.ByteSize - left.Manifest.Captured.Files.ByteSize,
		WordPress:      [2]string{left.Manifest.Site.Versions.WordPress, right.Manifest.Site.Versions.WordPress},
	}
	if left.Manifest.Captured.Database != nil {
		result.DatabaseBytes[0] = left.Manifest.Captured.Database.Bytes
	}
	if right.Manifest.Captured.Database != nil {
		result.DatabaseBytes[1] = right.Manifest.Captured.Database.Bytes
	}
	for path := range newFiles {
		if _, ok := oldFiles[path]; !ok {
			result.Added = appendLimited(&result.Truncated, result.Added, path)
		} else if oldFiles[path] != newFiles[path] {
			result.Changed = appendLimited(&result.Truncated, result.Changed, path)
		}
	}
	for path := range oldFiles {
		if _, ok := newFiles[path]; !ok {
			result.Removed = appendLimited(&result.Truncated, result.Removed, path)
		}
	}
	slices.Sort(result.Added)
	slices.Sort(result.Removed)
	slices.Sort(result.Changed)
	return result, nil
}

func appendLimited(truncated *bool, items []string, path string) []string {
	if len(items) >= 200 {
		*truncated = true
		return items
	}
	return append(items, path)
}

func indexFiles(items []model.FileRecord) map[string]string {
	out := map[string]string{}
	for _, item := range items {
		out[item.Path] = item.SHA256
	}
	return out
}

type ExplainResult struct {
	OK      bool        `json:"ok"`
	SiteID  string      `json:"site_id"`
	RunID   string      `json:"run_id,omitempty"`
	Failure *fail.Error `json:"failure"`
	Message string      `json:"message,omitempty"`
}

func (a *App) Explain(siteID, runID string) (ExplainResult, error) {
	if _, err := a.DB.GetSite(siteID); err != nil {
		return ExplainResult{}, err
	}
	if runID == "" {
		run, err := a.DB.LatestFailure(siteID)
		if err != nil {
			return ExplainResult{}, err
		}
		if run.RunID == "" {
			return ExplainResult{OK: true, SiteID: siteID, Message: "No failed run is recorded for this site."}, nil
		}
		return ExplainResult{OK: true, SiteID: siteID, RunID: run.RunID, Failure: run.Failure}, nil
	}
	run, err := a.run(siteID, runID)
	if err != nil {
		return ExplainResult{}, err
	}
	if run.OK {
		if run.Manifest != nil {
			var failed []string
			for _, dest := range run.Manifest.Destinations {
				if dest.Status == "failed" || dest.Status == "skipped" {
					text := dest.Kind + " destination"
					if dest.ID != "" {
						text += " " + dest.ID
					}
					text += " " + dest.Status
					if dest.Reason != "" {
						text += ": " + dest.Reason
					}
					failed = append(failed, text)
				}
			}
			if len(failed) > 0 {
				return ExplainResult{OK: true, SiteID: siteID, RunID: run.RunID, Message: "That run stored a local snapshot, but a destination did not. " + strings.Join(failed, " ")}, nil
			}
		}
		return ExplainResult{OK: true, SiteID: siteID, RunID: run.RunID, Message: "That run succeeded."}, nil
	}
	if run.InProgress() {
		return ExplainResult{OK: true, SiteID: siteID, RunID: run.RunID, Message: "That run is still in progress."}, nil
	}
	return ExplainResult{OK: true, SiteID: siteID, RunID: run.RunID, Failure: run.Failure}, nil
}

func (a *App) SetSchedule(siteID, cron string, enabled *bool) (model.Schedule, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Schedule{}, err
	}
	cron = strings.TrimSpace(cron)
	if enabled == nil && cron == "" {
		return model.Schedule{}, fail.New("bad_command", "Pass --cron or --enabled. Omit a flag to leave that saved value unchanged.")
	}
	nextCron := site.Schedule.Cron
	if cron != "" {
		nextCron = cron
	}
	nextEnabled := site.Schedule.Enabled
	if enabled != nil {
		nextEnabled = *enabled
	}
	if nextEnabled || nextCron != "" {
		if err := schedule.Check(nextCron); err != nil {
			return model.Schedule{}, err
		}
	}
	if err := a.DB.SaveSchedule(site.ID, nextCron, nextEnabled, site.Schedule.LastSlot); err != nil {
		return model.Schedule{}, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Schedule{}, err
	}
	return model.Schedule{Cron: nextCron, Enabled: nextEnabled, LastSlot: site.Schedule.LastSlot}, nil
}

func boolPtr(v bool) *bool {
	return &v
}

type Scheduled struct {
	OK      bool          `json:"ok"`
	Slot    string        `json:"slot"`
	Ran     []ScheduledID `json:"ran"`
	Skipped []ScheduledID `json:"skipped"`
}

type ScheduledID struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok,omitempty"`
	RunID  string `json:"run_id,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RunScheduled runs due contracts. A system cron invokes this; the backup itself does not.
func (a *App) RunScheduled(now time.Time) (Scheduled, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Minute)
	sites, err := a.DB.ListSites()
	if err != nil {
		return Scheduled{}, err
	}
	result := Scheduled{OK: true, Slot: now.Format("2006-01-02T15:04"), Ran: []ScheduledID{}, Skipped: []ScheduledID{}}
	for _, site := range sites {
		if !site.Schedule.Enabled || site.Schedule.Cron == "" {
			result.Skipped = append(result.Skipped, ScheduledID{ID: site.ID, Reason: "disabled"})
			continue
		}
		due, err := schedule.Matches(site.Schedule.Cron, now)
		if err != nil {
			result.Skipped = append(result.Skipped, ScheduledID{ID: site.ID, Reason: err.Error()})
			continue
		}
		if !due {
			result.Skipped = append(result.Skipped, ScheduledID{ID: site.ID, Reason: "not_due"})
			continue
		}
		if site.Schedule.LastSlot == result.Slot {
			result.Skipped = append(result.Skipped, ScheduledID{ID: site.ID, Reason: "already_ran"})
			continue
		}
		if err := a.DB.SaveSchedule(site.ID, site.Schedule.Cron, true, result.Slot); err != nil {
			return Scheduled{}, err
		}
		manifest, err := a.CreateBackup(site.ID)
		if err != nil {
			result.Ran = append(result.Ran, ScheduledID{ID: site.ID, Error: err.Error()})
			continue
		}
		result.Ran = append(result.Ran, ScheduledID{ID: site.ID, OK: true, RunID: manifest.Snapshot.RunID})
	}
	return result, nil
}

// HasSuccessfulBackup reports whether test_restore can run without a new backup.
func (a *App) HasSuccessfulBackup(siteID string) bool {
	_, err := a.successRun(siteID, "")
	return err == nil
}

func (a *App) successRun(siteID, runID string) (store.Run, error) {
	if runID == "" {
		runs, err := a.DB.ListRuns(siteID)
		if err != nil {
			return store.Run{}, err
		}
		for _, run := range runs {
			if run.OK && run.Manifest != nil && run.Manifest.Snapshot.ResticSnapshotID != "" {
				return run, nil
			}
		}
		return store.Run{}, fail.New("not_found", "This site has no successful backup.")
	}
	run, err := a.run(siteID, runID)
	if err != nil {
		return store.Run{}, err
	}
	if !run.OK || run.Manifest == nil {
		return store.Run{}, fail.New("not_found", "Run "+runID+" is not a successful backup.")
	}
	return run, nil
}

func (a *App) run(siteID, runID string) (store.Run, error) {
	run, err := a.DB.GetRun(siteID, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Run{}, fail.New("not_found", "No run "+runID+" is recorded for this site.")
	}
	return run, err
}

func (a *App) Latest(siteID string) (store.Run, error) {
	run, err := a.DB.LatestRun(siteID)
	if err != nil || run.RunID == "" {
		return run, err
	}
	return a.settleRun(run)
}
