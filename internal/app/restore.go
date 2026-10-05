package app

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/sqlcheck"
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

func (a *App) restore(siteID, runID, scratch string) (RestoreResult, error) {
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
	if err := assertScratch(site.Root, repo, scratch); err != nil {
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
		text, err := os.ReadFile(dump)
		if err != nil {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restored tree has no database dump.", Step: "restore"}
		}
		sum := sha256.Sum256(text)
		if fmt.Sprintf("%x", sum) != manifest.Captured.Database.SHA256 {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restored database dump does not match the manifest.", Step: "restore"}
		}
		assessment = sqlcheck.Assess(string(text))
		importable = assessment.Importable
		if !importable {
			return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The database dump is not importable. " + assessment.Reason, Step: "restore", Hint: "The scratch directory was left in place. The live database was not imported."}
		}
	}
	if junkRestored(filepath.Join(scratch, "files")) {
		return RestoreResult{}, &fail.Error{Code: "restore_mismatch", Message: "The restore contains cache or node_modules paths that should have been skipped.", Step: "restore"}
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

func assertScratch(siteRoot, repo, scratch string) error {
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
	if related(scratch, siteRoot) {
		return fail.New("restore_refused", "Refusing to restore into the live site or a directory that contains it ("+siteRoot+").")
	}
	if related(scratch, repo) {
		return fail.New("restore_refused", "Refusing to restore into the restic repository.")
	}
	info, err := os.Stat(scratch)
	if err == nil {
		if !info.IsDir() {
			return fail.New("restore_refused", "The scratch path exists and is not a directory.")
		}
		entries, err := os.ReadDir(scratch)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fail.New("restore_refused", "The scratch directory is not empty.")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
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
	for _, item := range manifest.Captured.Files.Inventory {
		path := filepath.Join(scratch, "files", filepath.FromSlash(item.Path))
		sum, err := fileSHA(path)
		if err != nil {
			return false, 0, nil
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() != item.Bytes || sum != item.SHA256 {
			return false, 0, nil
		}
	}
	return true, len(manifest.Captured.Files.Inventory), nil
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
