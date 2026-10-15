package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"local-backup/internal/capture"
	"local-backup/internal/destination"
	"local-backup/internal/discover"
	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/store"
)

var (
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	userPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	remotePath  = regexp.MustCompile(`^/[A-Za-z0-9._@/=+-]*$`)
)

// Register saves a discovered contract. The database password is not part of it.
func (a *App) Register(path, name, id string, idExplicit bool) (discover.Result, error) {
	result, err := discover.Site(path, name, id)
	if err != nil {
		return result, err
	}
	if !validID(result.Site.ID) {
		return result, fail.New("bad_id", "Site id must be lowercase letters, numbers, and hyphens.")
	}
	if idExplicit {
		if _, err := a.DB.GetSite(result.Site.ID); err == nil {
			return result, fail.New("already_registered", "A site with id "+result.Site.ID+" is already registered.")
		}
	} else {
		base := result.Site.ID
		for n := 2; ; n++ {
			if _, err := a.DB.GetSite(result.Site.ID); err != nil {
				break
			}
			result.Site.ID = base + "-" + itoa(n)
			if !validID(result.Site.ID) {
				return result, fail.New("bad_id", "Could not choose a site id.")
			}
		}
	}
	if err := a.DB.CreateSite(result.Site); err != nil {
		return result, err
	}
	if err := a.saveSiteFile(result.Site.ID); err != nil {
		return result, err
	}
	result.Saved = true
	return result, nil
}

// CreateBackup is the only backup path. Schedules and the UI call it directly.
func (a *App) CreateBackup(siteID string) (model.Manifest, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Manifest{}, err
	}
	if _, err := os.Stat(site.Root); err != nil {
		return model.Manifest{}, fail.New("missing_path", "The site root is missing: "+site.Root)
	}
	if err := a.checkEnabledDestinations(site); err != nil {
		return model.Manifest{}, err
	}
	runID, err := a.newRunID(site.ID)
	if err != nil {
		return model.Manifest{}, err
	}
	started := time.Now().UTC().Format(time.RFC3339)
	note, err := a.progress(site.ID, runID)
	if err != nil {
		return model.Manifest{}, err
	}
	defer note.release()
	if err := a.DB.BeginRun(site.ID, runID, started); err != nil {
		return model.Manifest{}, err
	}
	note.set(model.Progress{Phase: "Starting the backup"}, true)
	if site.Location.Kind == "sftp" {
		label := site.Location.User + "@" + site.Location.Host + ":" + site.Location.Path
		if err := syncServer(site.Location, site.Root, func(attempt int) {
			phase := "Copying files from the server"
			if attempt > 1 {
				phase = "Copying files from the server, attempt " + strconv.Itoa(attempt)
			}
			note.set(model.Progress{Phase: phase, File: label}, true)
		}); err != nil {
			return model.Manifest{}, a.failRun(site.ID, runID, started, err, nil)
		}
	}
	excludes := fileExcludes(site)
	note.set(model.Progress{Phase: "Estimating size"}, true)
	estimate, err := capture.EstimateFiles(site.Root, excludes)
	if err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, fail.Wrap("step_failed", "could not estimate backup size", err), nil)
	}
	note.set(model.Progress{
		Phase: "Estimating size", FilesTotal: int64(estimate.FileCount), BytesTotal: estimate.ByteSize,
	}, true)

	staging := filepath.Join(a.Home, "staging", site.ID, runID)
	payload := filepath.Join(staging, "payload")
	defer os.RemoveAll(staging)

	var secrets []string
	var db *model.DBCapture
	if model.DatabaseSource(site) != nil {
		phase := "Dumping the database"
		if site.Location.Kind == "sftp" && model.DatabaseSource(site).Method != "sql_file" {
			phase = "Dumping the database on the server"
		}
		note.set(model.Progress{Phase: phase}, true)
		report, found, dumpErr := capture.Database(site, filepath.Join(payload, "database", "database.sql"), filepath.Join(a.Home, "secrets"))
		secrets = found
		if dumpErr != nil {
			return model.Manifest{}, a.failRun(site.ID, runID, started, capture.Scrub(dumpErr, secrets), secrets)
		}
		db = &report
	}
	note.set(model.Progress{
		Phase: "Reading site files", FilesTotal: int64(estimate.FileCount), BytesTotal: estimate.ByteSize,
	}, true)
	files, err := capture.StageFiles(site.Root, filepath.Join(payload, "files"), excludes, func(rel string, n int) {
		note.set(model.Progress{
			Phase: "Reading site files", File: rel, FilesDone: int64(n),
			FilesTotal: int64(estimate.FileCount), BytesTotal: estimate.ByteSize,
		}, false)
	})
	if err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, fail.Wrap("step_failed", "could not read site files", err), secrets)
	}
	repo := a.repoPath(site)
	password, err := a.passwordFile(site.ID)
	if err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, err, secrets)
	}
	manifest := a.buildManifest(site, runID, started, files, db, repo, password, "", 0, nil, nil)
	if err := writePrivateJSON(filepath.Join(payload, "manifest.json"), manifest); err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, err, secrets)
	}
	restic := a.resticFor(site)
	note.set(model.Progress{Phase: "Archiving"}, true)
	if err := restic.EnsureRepo(repo, password); err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, err, secrets)
	}
	snapshotID, added, err := restic.Backup(payload, repo, password, site.ID, runID, note.onArchive(payload))
	if err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, err, secrets)
	}
	destinations, warnings := a.publish(site, repo, password, snapshotID, note)
	manifest = a.buildManifest(site, runID, started, files, db, repo, password, snapshotID, added, destinations, warnings)
	if err := writePrivateJSON(a.manifestPath(site.ID, runID), manifest); err != nil {
		return model.Manifest{}, a.failRun(site.ID, runID, started, err, secrets)
	}
	if err := a.DB.FinishRun(site.ID, runID, started, &manifest, nil); err != nil {
		return model.Manifest{}, err
	}
	report := a.retain(site, copiedTargets(site, destinations), note)
	recordRetention(&manifest, report)
	if manifest.Retention != nil || len(report.Warnings) > 0 {
		if err := writePrivateJSON(a.manifestPath(site.ID, runID), manifest); err != nil {
			manifest.Warnings = append(manifest.Warnings, "Retention ran, but the manifest file was not updated.")
		}
		if err := a.DB.UpdateManifest(site.ID, runID, manifest); err != nil {
			manifest.Warnings = append(manifest.Warnings, "Retention ran, but the registry was not updated.")
		}
	}
	return manifest, nil
}

func (a *App) buildManifest(site model.Site, runID, started string, files model.FileCapture, db *model.DBCapture, repo, password, snapshotID string, added int64, destinations []model.DestResult, warnings []string) model.Manifest {
	if destinations == nil {
		destinations = []model.DestResult{}
	}
	size := files.ByteSize
	dbName := ""
	if db != nil {
		size += db.Bytes
	}
	if source := model.DatabaseSource(site); source != nil {
		dbName = source.Name
	}
	return model.Manifest{
		OK:        true,
		Schema:    model.Schema,
		Model:     model.ModelLine,
		Timestamp: started,
		SizeBytes: size,
		Site: model.ManifestSite{
			ID: site.ID, Name: site.Name, Adapter: site.Adapter, Root: site.Root,
			Versions: discover.Versions(site.Root, site.Adapter),
		},
		Captured:     model.Captured{Files: files, Database: db},
		Snapshot:     model.Snapshot{RunID: runID, ResticSnapshotID: snapshotID, Repository: repo, DataAdded: added},
		Destinations: destinations,
		Warnings:     warnings,
		Restore: model.Restore{
			Confidence: "never",
			Steps:      restoreSteps(site.Root, repo, password, snapshotID, db != nil, dbName, site.Limits),
		},
	}
}

func restoreSteps(root, repo, password, snapshotID string, hasDB bool, dbName string, limits model.Limits) []string {
	id := snapshotID
	if id == "" {
		id = "<snapshot id written after restic returns>"
	}
	if dbName == "" {
		dbName = "(live database)"
	}
	steps := []string{
		"Do not write into the live site root: " + root,
		"export RESTIC_PASSWORD_FILE=" + password,
		"restic" + limitFlags(limits) + " -r " + repo + " restore " + id + " --target SCRATCH",
		"SCRATCH must be an empty directory. It must not be the live site, a parent of the live site, or the restic repository.",
		"The restore contains files/ from the site.",
	}
	if hasDB {
		steps = append(steps, "Import SCRATCH/database/database.sql into a new empty database. Do not import into the live database "+dbName+".")
	}
	steps = append(steps, "Point a new web root at SCRATCH/files. Leave "+root+" unchanged.")
	return steps
}

func limitFlags(limits model.Limits) string {
	var flags string
	if limits.UploadKiB > 0 {
		flags += " --limit-upload " + strconv.Itoa(limits.UploadKiB)
	}
	if limits.DownloadKiB > 0 {
		flags += " --limit-download " + strconv.Itoa(limits.DownloadKiB)
	}
	return flags
}

func (a *App) publish(site model.Site, repo, password, snapshotID string, note *progressWriter) ([]model.DestResult, []string) {
	var results []model.DestResult
	var warnings []string
	for _, dest := range site.Destinations {
		if dest.Kind == "local_restic" {
			path := dest.Path
			if path == "" {
				path = repo
			}
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "stored", Written: true, Path: path, SnapshotID: snapshotID})
			continue
		}
		kind, kindErr := destination.ByID(dest.Kind)
		offsite := kindErr == nil && kind.Offsite
		if !dest.Enabled {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "disabled", Offsite: offsite, Reason: "Saved only. Nothing is uploaded until this destination is enabled."})
			continue
		}
		if kindErr != nil {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "failed", Reason: kindErr.Error()})
			warnings = append(warnings, kindErr.Error())
			continue
		}
		if missing := kind.Missing(dest); len(missing) > 0 {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "skipped", Offsite: offsite, Reason: "missing " + strings.Join(missing, ", ")})
			continue
		}
		if kind.NeedsCredentials() && !credentialPresent(dest.CredentialFile) {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "skipped", Offsite: offsite, Reason: "credentials file is missing"})
			continue
		}
		target, err := kind.URL(dest)
		if err != nil {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "failed", Offsite: offsite, Reason: err.Error()})
			warnings = append(warnings, err.Error())
			continue
		}
		if err := kind.BeforeCopy(dest); err != nil {
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "failed", Offsite: offsite, Reason: err.Error()})
			warnings = append(warnings, err.Error())
			continue
		}
		env := loadEnvFile(dest.CredentialFile)
		if note != nil {
			note.set(model.Progress{Phase: kind.ProgressLabel(dest)}, true)
		}
		var globalOpts []string
		if rclone, err := kind.RcloneProgram(); err == nil && rclone != "" {
			globalOpts = append(globalOpts, "-o", "rclone.program="+rclone)
		}
		if err := a.resticFor(site).Copy(repo, password, target, snapshotID, env, globalOpts...); err != nil {
			message := formatDestFailure(err)
			results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "failed", Offsite: offsite, URL: target, Reason: message})
			warnings = append(warnings, message)
			continue
		}
		results = append(results, model.DestResult{ID: dest.ID, Kind: dest.Kind, Status: "copied", Written: true, Offsite: offsite, URL: target, SnapshotID: snapshotID})
	}
	if results == nil {
		results = []model.DestResult{}
	}
	return results, warnings
}

func formatDestFailure(err error) string {
	fe, ok := err.(*fail.Error)
	if !ok || fe == nil {
		return err.Error()
	}
	text := fe.Message
	if fe.Hint != "" {
		text += " " + fe.Hint
	}
	return text
}

func missingFields(dest model.Destination) []string {
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return []string{"kind"}
	}
	return kind.Missing(dest)
}

func needsCredentials(dest model.Destination) bool {
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return false
	}
	return kind.NeedsCredentials()
}

func credentialPresent(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func loadEnvFile(path string) []string {
	if path == "" {
		return nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var env []string
	for _, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		env = append(env, line)
	}
	return env
}

// AddDestination stores a second copy setting. Disabled destinations are not contacted
// at save time, but rclone remote names are checked against local rclone config whenever
// host is set. Enabled destinations are probed for reachability when saved.
func (a *App) AddDestination(siteID string, dest model.Destination, accessKey, secretKey string) (model.Destination, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return dest, err
	}
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return dest, err
	}
	if err := kind.Validate(site, a.repoPath(site), dest); err != nil {
		return dest, err
	}
	if err := kind.CheckAccess(dest, dest.Enabled); err != nil {
		return dest, err
	}
	dest.ID = nextDestID(site, dest.Kind)
	dest.Credentials = ""
	if accessKey != "" || secretKey != "" {
		body, err := kind.CredentialBody(accessKey, secretKey)
		if err != nil {
			return dest, err
		}
		path := filepath.Join(a.Home, "secrets", site.ID+"-"+dest.ID+".env")
		if err := store.WriteSecret(path, body); err != nil {
			return dest, err
		}
		dest.CredentialFile = path
	}
	if err := a.DB.AddDestination(site.ID, dest); err != nil {
		return dest, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return dest, err
	}
	dest.Credentials = credentialLabel(dest.CredentialFile)
	return dest, nil
}

// SetDestinationEnabled turns upload on or off for one saved destination.
// Enabling re-checks access. The local restic repository cannot be disabled.
func (a *App) SetDestinationEnabled(siteID, destID string, enabled bool) (model.Destination, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Destination{}, err
	}
	dest, err := findDestination(site, destID)
	if err != nil {
		return model.Destination{}, err
	}
	if dest.Kind == "local_restic" {
		return model.Destination{}, fail.New("bad_destination", "The local restic repository stays enabled.")
	}
	if enabled {
		kind, err := destination.ByID(dest.Kind)
		if err != nil {
			return model.Destination{}, err
		}
		if err := kind.Validate(site, a.repoPath(site), dest); err != nil {
			return model.Destination{}, err
		}
		if err := kind.CheckAccess(dest, true); err != nil {
			return model.Destination{}, err
		}
	}
	dest.Enabled = enabled
	if err := a.DB.UpdateDestination(site.ID, dest); err != nil {
		return model.Destination{}, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Destination{}, err
	}
	dest.Credentials = credentialLabel(dest.CredentialFile)
	return dest, nil
}

// RemoveDestination deletes a saved destination. The local restic repository cannot be removed.
func (a *App) RemoveDestination(siteID, destID string) error {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return err
	}
	dest, err := findDestination(site, destID)
	if err != nil {
		return err
	}
	if dest.Kind == "local_restic" {
		return fail.New("bad_destination", "The local restic repository cannot be removed.")
	}
	if err := a.DB.RemoveDestination(site.ID, destID); err != nil {
		return err
	}
	if dest.CredentialFile != "" {
		_ = os.Remove(dest.CredentialFile)
	}
	return a.saveSiteFile(site.ID)
}

func findDestination(site model.Site, destID string) (model.Destination, error) {
	for _, dest := range site.Destinations {
		if dest.ID == destID {
			return dest, nil
		}
	}
	return model.Destination{}, fail.New("bad_destination", "Destination "+destID+" was not found on this site.")
}

// checkEnabledDestinations probes every enabled destination before a backup run starts.
func (a *App) checkEnabledDestinations(site model.Site) error {
	repo := a.repoPath(site)
	for _, dest := range site.Destinations {
		if !dest.Enabled || dest.Kind == "local_restic" {
			continue
		}
		kind, err := destination.ByID(dest.Kind)
		if err != nil {
			return err
		}
		if err := kind.Validate(site, repo, dest); err != nil {
			return err
		}
		if missing := kind.Missing(dest); len(missing) > 0 {
			continue
		}
		if kind.NeedsCredentials() && dest.CredentialFile == "" {
			continue
		}
		if err := kind.CheckAccess(dest, true); err != nil {
			return err
		}
	}
	return nil
}

func nextDestID(site model.Site, kind string) string {
	n := 1
	for _, dest := range site.Destinations {
		if dest.Kind == kind {
			n++
		}
	}
	return kind + "-" + itoa(n)
}

func (a *App) passwordFile(siteID string) (string, error) {
	path := filepath.Join(a.Home, "secrets", siteID+".restic-password")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	if err := store.WriteSecret(path, base64.RawURLEncoding.EncodeToString(buf)+"\n"); err != nil {
		return "", err
	}
	return path, nil
}

func fileExcludes(site model.Site) []string {
	for _, source := range site.Sources {
		if source.Kind == "files" {
			return source.Excludes
		}
	}
	return nil
}

func (a *App) repoPath(site model.Site) string {
	for _, dest := range site.Destinations {
		if dest.Kind == "local_restic" && dest.Path != "" {
			return dest.Path
		}
	}
	return filepath.Join(a.Home, "restic", site.ID)
}

func (a *App) newRunID(siteID string) (string, error) {
	base := time.Now().UTC().Format("20060102T150405Z")
	id := base
	for n := 2; ; n++ {
		exists, err := a.DB.RunExists(siteID, id)
		if err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
		id = base + "-" + itoa(n)
	}
}

func (a *App) manifestPath(siteID, runID string) string {
	return filepath.Join(a.Home, "manifests", siteID, runID+".json")
}

func (a *App) failRun(siteID, runID, started string, err error, secrets []string) error {
	err = capture.Scrub(err, secrets)
	fe, ok := err.(*fail.Error)
	if !ok || fe == nil {
		fe = fail.Wrap("step_failed", "backup failed", err)
	}
	fe.RunID = runID
	_ = a.DB.FinishRun(siteID, runID, started, nil, fe)
	_ = writePrivateJSON(filepath.Join(a.Home, "runs", siteID, runID+".json"), map[string]any{"ok": false, "error": fe})
	return fe
}

func writePrivateJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
