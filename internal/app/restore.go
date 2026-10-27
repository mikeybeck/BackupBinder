package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backupbinder/internal/fail"
	"backupbinder/internal/model"
	"backupbinder/internal/sqlcheck"
)

// RestoreResult is the scratch restore report. The live site is not a target.
type RestoreResult struct {
	OK                 bool            `json:"ok"`
	RunID              string          `json:"run_id"`
	Scratch            string          `json:"scratch"`
	ArchiveReadable    bool            `json:"archive_readable"`
	FilesMatch         bool            `json:"files_match"`
	FileCount          int             `json:"file_count"`
	DatabaseImportable bool            `json:"database_importable"`
	DatabaseCheck      sqlcheck.Result `json:"database_check,omitempty"`
	RestoreConfidence  string          `json:"restore_confidence"`
	LiveSiteUntouched  bool            `json:"live_site_untouched"`
}

// TestRestore checks the archive in a scratch directory and records restore confidence.
func (a *App) TestRestore(siteID, runID, scratch string) (RestoreResult, error) {
	return a.restore(siteID, runID, scratch)
}

// RestoreBackup extracts into a scratch directory and runs the same checks.
func (a *App) RestoreBackup(siteID, runID, scratch string) (RestoreResult, error) {
	return a.restore(siteID, runID, scratch)
}

func (a *App) restore(siteID, runID, scratch string) (result RestoreResult, err error) {
	defer func() { a.recordRestore(siteID, result, err) }()
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return RestoreResult{}, err
	}
	run, err := a.successRun(site.ID, runID)
	if err != nil {
		return RestoreResult{}, err
	}
	manifest := *run.Manifest
	repo := a.repoPath(site)
	if scratch == "" {
		scratch = filepath.Join(a.Home, "scratch", site.ID, manifest.Snapshot.RunID+"-"+time.Now().UTC().Format("150405"))
	}
	scratch, err = filepath.Abs(scratch)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := assertScratch(site.Root, repo, scratch, a.Home, site.ID); err != nil {
		return RestoreResult{}, err
	}
	before, err := hashLive(site.Root, manifest.Captured.Files.Inventory)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return RestoreResult{}, err
	}
	password, err := a.passwordFile(site.ID)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := a.resticFor(site).Restore(repo, password, manifest.Snapshot.ResticSnapshotID, scratch); err != nil {
		return RestoreResult{}, err
	}
	if junkRestored(filepath.Join(scratch, "files")) {
		return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restore contains cache or node_modules paths that should have been skipped.", Step: "restore"}
	}
	matched, count, err := filesMatch(scratch, manifest)
	if err != nil {
		return RestoreResult{}, err
	}
	if !matched {
		return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "Restored files do not match the manifest inventory.", Step: "restore", Hint: "The scratch directory was left in place. The live site was not written."}
	}
	var assessment sqlcheck.Result
	importable := true
	if manifest.Captured.Database != nil {
		dump := filepath.Join(scratch, "database", "database.sql")
		sum, err := fileSHA(dump)
		if err != nil {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restored tree has no database dump.", Step: "restore"}
		}
		if sum != manifest.Captured.Database.SHA256 {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restored database dump does not match the manifest.", Step: "restore"}
		}
		assessment, err = assessDump(dump)
		if err != nil {
			return RestoreResult{}, err
		}
		importable = assessment.Importable
		if !importable {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The database dump is not importable. " + assessment.Reason, Step: "restore", Hint: "The scratch directory was left in place. The live database was not imported."}
		}
	}
	after, err := hashLive(site.Root, manifest.Captured.Files.Inventory)
	if err != nil {
		return RestoreResult{}, err
	}
	if !sameHashes(before, after) {
		return RestoreResult{}, fail.New("restore_refused", "The live site changed while restoring. Treat the scratch copy as untrusted until you know why.")
	}
	verified := time.Now().UTC().Format(time.RFC3339)
	manifest.Restore.Confidence = verified
	if err := a.DB.UpdateManifest(site.ID, manifest.Snapshot.RunID, manifest); err != nil {
		return RestoreResult{}, err
	}
	if err := writePrivateJSON(a.manifestPath(site.ID, manifest.Snapshot.RunID), manifest); err != nil {
		return RestoreResult{}, err
	}
	if err := a.DB.SaveRestore(site.ID, verified, manifest.Snapshot.RunID); err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{
		OK: true, RunID: manifest.Snapshot.RunID, Scratch: scratch,
		ArchiveReadable: true, FilesMatch: true, FileCount: count,
		DatabaseImportable: importable, DatabaseCheck: assessment,
		RestoreConfidence: verified, LiveSiteUntouched: true,
	}, nil
}

func assertScratch(siteRoot, repo, scratch, home, siteID string) error {
	siteRoot, err := filepath.Abs(siteRoot)
	if err != nil {
		return err
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		return err
	}
	scratch, err = filepath.Abs(scratch)
	if err != nil {
		return err
	}
	hint := " Omit --scratch. A new empty directory is created under " + filepath.Join(home, "scratch", siteID) + "."
	if related(scratch, siteRoot) {
		return fail.New("restore_refused", "Refusing to restore into the live site or a directory that contains it ("+siteRoot+")."+hint)
	}
	if related(scratch, repo) {
		return fail.New("restore_refused", "Refusing to restore into the restic repository."+hint)
	}
	info, err := os.Stat(scratch)
	if err == nil {
		if !info.IsDir() {
			return fail.New("restore_refused", "The scratch path exists and is not a directory."+hint)
		}
		entries, err := os.ReadDir(scratch)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fail.New("restore_refused", "The scratch directory is not empty."+hint)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) recordRestore(siteID string, result RestoreResult, err error) {
	if siteID == "" {
		return
	}
	status := RestoreStatus{OK: err == nil && result.OK, Scratch: result.Scratch}
	if err != nil {
		status.OK = false
		status.Error = err.Error()
		if fe, ok := err.(*fail.Error); ok {
			status.Error = fe.Message
		}
	}
	_ = a.writeRestoreStatus(siteID, status)
}

// RestoreStatus is the latest scratch restore for the site page.
type RestoreStatus struct {
	InProgress bool   `json:"in_progress"`
	OK         bool   `json:"ok"`
	PID        int    `json:"pid,omitempty"`
	Scratch    string `json:"scratch,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (a *App) restoreStatusPath(siteID string) string {
	return filepath.Join(a.Home, "runs", siteID, "restore.json")
}

func (a *App) writeRestoreStatus(siteID string, status RestoreStatus) error {
	if err := os.MkdirAll(filepath.Dir(a.restoreStatusPath(siteID)), 0o700); err != nil {
		return err
	}
	return writePrivateJSON(a.restoreStatusPath(siteID), status)
}

// ReadRestoreStatus returns the latest restore check, if one was recorded.
func (a *App) ReadRestoreStatus(siteID string) (RestoreStatus, bool) {
	raw, err := os.ReadFile(a.restoreStatusPath(siteID))
	if err != nil {
		return RestoreStatus{}, false
	}
	var status RestoreStatus
	if json.Unmarshal(raw, &status) != nil {
		return RestoreStatus{}, false
	}
	if status.InProgress && !processAlive(status.PID) {
		status.InProgress = false
		if status.Error == "" {
			status.Error = "The restore check stopped before it finished."
		}
	}
	return status, true
}

func related(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(a, b+sep) || strings.HasPrefix(b, a+sep)
}

func filesMatch(scratch string, manifest model.Manifest) (bool, int, error) {
	if err := scratchLayout(scratch, manifest); err != nil {
		if errors.Is(err, errMismatch) || os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	expected := map[string]model.FileRecord{}
	for _, item := range manifest.Captured.Files.Inventory {
		expected[item.Path] = item
	}
	found := 0
	err := filepath.WalkDir(filepath.Join(scratch, "files"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(filepath.Join(scratch, "files"), path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !inventoryDir(rel, expected) {
				return errMismatch
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return errMismatch
		}
		item, ok := expected[rel]
		if !ok {
			return errMismatch
		}
		sum, err := fileSHA(path)
		if err != nil {
			return errMismatch
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() != item.Bytes || sum != item.SHA256 {
			return errMismatch
		}
		found++
		return nil
	})
	if errors.Is(err, errMismatch) || (err != nil && os.IsNotExist(err)) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if found != len(expected) {
		return false, 0, nil
	}
	return true, found, nil
}

var errMismatch = errors.New("mismatch")

func scratchLayout(scratch string, manifest model.Manifest) error {
	entries, err := os.ReadDir(scratch)
	if err != nil {
		return err
	}
	allowed := map[string]bool{"files": true, "manifest.json": true}
	if manifest.Captured.Database != nil {
		allowed["database"] = true
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return errMismatch
		}
	}
	info, err := os.Stat(filepath.Join(scratch, "manifest.json"))
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			return errMismatch
		}
		return err
	}
	filesInfo, err := os.Stat(filepath.Join(scratch, "files"))
	if err != nil {
		return err
	}
	if !filesInfo.IsDir() {
		return errMismatch
	}
	if manifest.Captured.Database == nil {
		return nil
	}
	dbEntries, err := os.ReadDir(filepath.Join(scratch, "database"))
	if err != nil {
		return err
	}
	if len(dbEntries) != 1 || dbEntries[0].Name() != "database.sql" || dbEntries[0].IsDir() {
		return errMismatch
	}
	return nil
}

func inventoryDir(rel string, expected map[string]model.FileRecord) bool {
	prefix := rel + "/"
	for path := range expected {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func assessDump(path string) (sqlcheck.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return sqlcheck.Result{}, err
	}
	defer f.Close()
	return sqlcheck.AssessReader(f), nil
}

func junkRestored(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "node_modules" || name == "cache" || name == ".cache" {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func hashLive(root string, inventory []model.FileRecord) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range inventory {
		sum, err := fileSHA(filepath.Join(root, filepath.FromSlash(item.Path)))
		if err != nil {
			if os.IsNotExist(err) {
				out[item.Path] = ""
				continue
			}
			return nil, err
		}
		out[item.Path] = sum
	}
	return out, nil
}

func sameHashes(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
