package app

import (
	"os"
	"strconv"
	"strings"

	"local-backup/internal/archive"
	"local-backup/internal/destination"
	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/store"
)

// RetentionReport is the result of saving or applying snapshot rules.
type RetentionReport struct {
	OK        bool            `json:"ok"`
	Retention model.Retention `json:"retention"`
	Removed   []string        `json:"removed"`
	Kept      int             `json:"kept"`
	Applied   bool            `json:"applied"`
	Note      string          `json:"note,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// ParseCount reads a retention count. An empty string is unset. Zero skips that rule.
func ParseCount(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false, fail.New("bad_retention", "A retention count is a whole number. Use 0 to skip that rule.")
	}
	return n, true, nil
}

// SetRetention saves snapshot rules and applies them when a repository already exists.
func (a *App) SetRetention(siteID string, policy model.Retention) (RetentionReport, error) {
	if err := validateRetention(policy); err != nil {
		return RetentionReport{}, err
	}
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return RetentionReport{}, err
	}
	if err := a.DB.SaveRetention(site.ID, policy); err != nil {
		return RetentionReport{}, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return RetentionReport{}, err
	}
	site.Retention = policy
	return a.retain(site, enabledTargets(site), nil), nil
}

// ApplyRetention forgets and prunes using the saved rules, locally and on enabled destinations.
func (a *App) ApplyRetention(siteID string) (RetentionReport, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return RetentionReport{}, err
	}
	return a.retain(site, enabledTargets(site), nil), nil
}

func validateRetention(policy model.Retention) error {
	for _, n := range []int{policy.KeepLast, policy.KeepHourly, policy.KeepDaily, policy.KeepWeekly, policy.KeepMonthly, policy.KeepYearly} {
		if n < 0 {
			return fail.New("bad_retention", "A retention count is a whole number. Use 0 to skip that rule.")
		}
	}
	return nil
}

type repoTarget struct {
	Label      string
	Repo       string
	Env        []string
	GlobalOpts []string
}

func (a *App) retain(site model.Site, targets []repoTarget, note *progressWriter) RetentionReport {
	report := RetentionReport{OK: true, Retention: site.Retention, Removed: []string{}}
	if !site.Retention.Active() {
		report.Note = "Every snapshot is kept."
		return report
	}
	if note != nil {
		note.set(model.Progress{Phase: "Applying retention"}, true)
	}
	repo := a.repoPath(site)
	localReady := repoReady(repo)
	if !localReady && len(targets) == 0 {
		report.Note = "These rules apply on the next backup."
		return report
	}
	password, err := a.passwordFile(site.ID)
	if err != nil {
		report.Warnings = append(report.Warnings, warnText(err))
		return report
	}
	restic := a.resticFor(site)
	if !localReady {
		report.Note = "These rules apply on the next backup."
	} else if outcome, err := restic.Forget(repo, password, nil, site.ID, site.Retention); err != nil {
		report.Warnings = append(report.Warnings, "Local retention was not applied. "+warnText(err))
	} else {
		report.Applied = true
		report.Kept = outcome.Kept
		if err := restic.Prune(repo, password, nil); err != nil {
			if len(outcome.Removed) > 0 {
				report.Warnings = append(report.Warnings, "Older snapshots were removed, but the local repository was not pruned. The next backup will try again.")
			} else {
				report.Warnings = append(report.Warnings, "The local repository was not pruned. The next backup will try again.")
			}
		}
		removed, warnings := a.dropForgotten(site, outcome.Removed)
		report.Removed = removed
		report.Warnings = append(report.Warnings, warnings...)
	}
	for _, target := range targets {
		report.Warnings = append(report.Warnings, a.retainCopy(restic, password, site, target, report.Applied)...)
	}
	if report.Removed == nil {
		report.Removed = []string{}
	}
	return report
}

func (a *App) retainCopy(restic archive.Runner, password string, site model.Site, target repoTarget, localApplied bool) []string {
	if target.Repo == "" {
		return nil
	}
	if !strings.Contains(target.Repo, ":") && !repoReady(target.Repo) {
		return nil
	}
	if _, err := restic.Forget(target.Repo, password, target.Env, site.ID, site.Retention, target.GlobalOpts...); err != nil {
		if missingRepo(err) {
			return nil
		}
		text := "Retention was not applied to " + target.Label + "."
		if localApplied {
			text += " The local snapshots were still updated."
		}
		return []string{text + " " + warnText(err)}
	}
	if err := restic.Prune(target.Repo, password, target.Env, target.GlobalOpts...); err != nil && !missingRepo(err) {
		return []string{"Retention removed snapshots on " + target.Label + ", but that copy was not pruned. The next backup will try again."}
	}
	return nil
}

func (a *App) dropForgotten(site model.Site, removed []archive.RemovedSnapshot) ([]string, []string) {
	if len(removed) == 0 {
		return []string{}, nil
	}
	runs, err := a.DB.ListRuns(site.ID)
	if err != nil {
		return []string{}, []string{"Snapshots were removed, but the backup list could not be updated."}
	}
	var dropped []string
	var warnings []string
	cleared := false
	for _, snap := range removed {
		runID := forgottenRun(runs, snap)
		if runID == "" {
			continue
		}
		if err := a.DB.DeleteRun(site.ID, runID); err != nil {
			warnings = append(warnings, "Snapshot "+runID+" was removed, but its record could not be deleted.")
			continue
		}
		if err := os.Remove(a.manifestPath(site.ID, runID)); err != nil && !os.IsNotExist(err) {
			warnings = append(warnings, "Snapshot "+runID+" was removed, but its manifest file could not be deleted.")
		}
		dropped = append(dropped, runID)
		if !cleared && site.RestoreConfidence.RunID == runID {
			if err := a.DB.SaveRestore(site.ID, "", ""); err != nil {
				warnings = append(warnings, "The removed snapshot was the last restore check, and that record could not be cleared.")
			}
			cleared = true
		}
	}
	if dropped == nil {
		dropped = []string{}
	}
	return dropped, warnings
}

func forgottenRun(runs []store.Run, snap archive.RemovedSnapshot) string {
	if snap.RunID != "" {
		for _, run := range runs {
			if run.RunID == snap.RunID && !run.InProgress() {
				return run.RunID
			}
		}
	}
	for _, run := range runs {
		if run.InProgress() || run.Manifest == nil {
			continue
		}
		if sameSnapshot(run.Manifest.Snapshot.ResticSnapshotID, snap.ID) {
			return run.RunID
		}
	}
	return ""
}

func sameSnapshot(stored, removed string) bool {
	if stored == "" || removed == "" {
		return false
	}
	return stored == removed || strings.HasPrefix(stored, removed) || strings.HasPrefix(removed, stored)
}

func repoReady(path string) bool {
	info, err := os.Stat(path + "/config")
	return err == nil && !info.IsDir()
}

func missingRepo(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	if fe, ok := err.(*fail.Error); ok {
		text += " " + fe.Stderr + " " + fe.Hint
	}
	text = strings.ToLower(text)
	return strings.Contains(text, "does not exist") || strings.Contains(text, "repository not found")
}

func warnText(err error) string {
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

func enabledTargets(site model.Site) []repoTarget {
	var targets []repoTarget
	for _, dest := range site.Destinations {
		if dest.Kind == "local_restic" || !dest.Enabled {
			continue
		}
		if len(missingFields(dest)) > 0 || (needsCredentials(dest) && !credentialPresent(dest.CredentialFile)) {
			continue
		}
		url, err := archive.DestinationURL(dest)
		if err != nil {
			continue
		}
		targets = append(targets, repoTarget{
			Label: destLabel(dest), Repo: url, Env: loadEnvFile(dest.CredentialFile),
			GlobalOpts: destGlobalOpts(dest),
		})
	}
	return targets
}

func copiedTargets(site model.Site, results []model.DestResult) []repoTarget {
	byID := map[string]model.Destination{}
	for _, dest := range site.Destinations {
		byID[dest.ID] = dest
	}
	var targets []repoTarget
	for _, result := range results {
		if result.Status != "copied" || result.URL == "" {
			continue
		}
		dest := byID[result.ID]
		targets = append(targets, repoTarget{
			Label: destLabel(dest), Repo: result.URL, Env: loadEnvFile(dest.CredentialFile),
			GlobalOpts: destGlobalOpts(dest),
		})
	}
	return targets
}

func destGlobalOpts(dest model.Destination) []string {
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return nil
	}
	rclone, err := kind.RcloneProgram()
	if err != nil || rclone == "" {
		return nil
	}
	return []string{"-o", "rclone.program=" + rclone}
}

func destLabel(dest model.Destination) string {
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return "the " + dest.Kind + " copy"
	}
	return "the " + kind.Label + " copy"
}

func recordRetention(manifest *model.Manifest, report RetentionReport) {
	if report.Applied {
		removed := report.Removed
		if removed == nil {
			removed = []string{}
		}
		manifest.Retention = &model.RetentionApplied{Removed: removed, Kept: report.Kept}
	}
	if len(report.Warnings) == 0 {
		return
	}
	manifest.Warnings = append(manifest.Warnings, report.Warnings...)
}
