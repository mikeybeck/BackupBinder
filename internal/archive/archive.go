// Package archive calls the restic binary. It does not reimplement restic.
package archive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"backupbinder/internal/destination"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// Runner is a restic binary plus its cache directory.
// LimitUpload and LimitDownload are KiB per second. Zero leaves that direction unlimited.
type Runner struct {
	Binary        string
	CacheDir      string
	LimitUpload   int
	LimitDownload int
}

// WithLimits returns a runner that caps repository traffic at the given KiB/s rates.
func (r Runner) WithLimits(uploadKiB, downloadKiB int) Runner {
	r.LimitUpload = uploadKiB
	r.LimitDownload = downloadKiB
	return r
}

type summary struct {
	MessageType string `json:"message_type"`
	SnapshotID  string `json:"snapshot_id"`
	DataAdded   int64  `json:"data_added"`
}

// Status is one progress event from restic backup --json --verbose.
type Status struct {
	File         string
	Percent      float64
	HasPercent   bool
	FilesDone    int64
	FilesTotal   int64
	HasFiles     bool
	BytesTotal   int64
	HasBytes     bool
	WrittenBytes int64
	SnapshotID   string
	DataAdded    int64
	Done         bool
}

type backupLine struct {
	MessageType  string   `json:"message_type"`
	PercentDone  float64  `json:"percent_done"`
	TotalFiles   int64    `json:"total_files"`
	FilesDone    int64    `json:"files_done"`
	TotalBytes   int64    `json:"total_bytes"`
	CurrentFiles []string `json:"current_files"`
	Action       string   `json:"action"`
	Item         string   `json:"item"`
	SnapshotID   string   `json:"snapshot_id"`
	DataAdded    int64    `json:"data_added"`
}

// copyProgressLine matches restic copy terminal progress, e.g. "[0:12] 75.00%  3 / 4 packs copied".
var copyProgressLine = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s+(\d+)\s*/\s*(\d+)\s+packs?\s+copied`)

// ParseCopyLine reads one progress line from restic copy stdout. Percent is 0–1.
func ParseCopyLine(line string) (Status, bool) {
	line = strings.TrimSpace(line)
	m := copyProgressLine.FindStringSubmatch(line)
	if len(m) != 4 {
		return Status{}, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return Status{}, false
	}
	done, err1 := strconv.ParseInt(m[2], 10, 64)
	total, err2 := strconv.ParseInt(m[3], 10, 64)
	if err1 != nil || err2 != nil || total <= 0 {
		return Status{}, false
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return Status{
		HasPercent: true, Percent: pct / 100,
		FilesDone: done, FilesTotal: total, HasFiles: true,
		File: strconv.FormatInt(done, 10) + " of " + strconv.FormatInt(total, 10) + " packs",
	}, true
}

// ParseBackupLine reads one JSON line from restic backup. ok is false for lines that are not progress.
func ParseBackupLine(line string) (Status, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return Status{}, false
	}
	var msg backupLine
	if json.Unmarshal([]byte(line), &msg) != nil {
		return Status{}, false
	}
	switch msg.MessageType {
	case "summary":
		if msg.SnapshotID == "" {
			return Status{}, false
		}
		return Status{Done: true, SnapshotID: msg.SnapshotID, DataAdded: msg.DataAdded, HasPercent: true, Percent: 1}, true
	case "status":
		st := Status{
			HasPercent: true, Percent: msg.PercentDone,
			FilesDone: msg.FilesDone, FilesTotal: msg.TotalFiles, HasFiles: msg.TotalFiles > 0,
			BytesTotal: msg.TotalBytes, HasBytes: msg.TotalBytes > 0,
		}
		if n := len(msg.CurrentFiles); n > 0 {
			st.File = msg.CurrentFiles[n-1]
		}
		return st, true
	case "verbose_status":
		if msg.Item == "" || msg.Action == "scan_finished" || strings.HasSuffix(msg.Item, "/") {
			return Status{}, false
		}
		return Status{File: msg.Item}, true
	default:
		return Status{}, false
	}
}

// EnsureRepo initializes a local repository when it has no config yet.
func (r Runner) EnsureRepo(repo, passwordFile string) error {
	if err := os.MkdirAll(repo, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(repo + "/config"); err == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "init")
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic init failed for the local repository.", Step: "restic", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "Check that the restic binary runs and the data directory is writable."}
	}
	return nil
}

// Backup stores the current directory. onStatus receives file and percent updates. It may be nil.
func (r Runner) Backup(dir, repo, passwordFile, siteID, runID string, onStatus func(Status)) (string, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	var last summary
	var resticPID atomic.Int64
	stdout, stderr, code, err := r.runIn(ctx, dir, repo, passwordFile, nil, func(line string) {
		st, ok := ParseBackupLine(line)
		if !ok {
			return
		}
		if st.Done {
			last.SnapshotID = st.SnapshotID
			last.DataAdded = st.DataAdded
			return
		}
		if onStatus != nil {
			st.WrittenBytes = ReadWriteBytes(int(resticPID.Load()))
			onStatus(st)
		}
	}, func(pid int) { resticPID.Store(int64(pid)) }, "backup", "--json", "--verbose", "--host", "backupbinder", "--tag", "site="+siteID, "--tag", "run="+runID, ".")
	if err != nil {
		return "", 0, err
	}
	if code != 0 {
		return "", 0, &fail.Error{Code: "step_failed", Message: "restic backup failed.", Step: "restic", ExitCode: fail.ExitCode(code), Stderr: tail(stderr + stdout), Hint: "The local restic repository did not accept this snapshot."}
	}
	if last.SnapshotID == "" {
		return "", 0, &fail.Error{Code: "step_failed", Message: "restic backup did not report a snapshot id.", Step: "restic", Stderr: tail(stdout), Hint: "Run restic snapshots against the local repository."}
	}
	return last.SnapshotID, last.DataAdded, nil
}

// Check reads the repository structure and every stored data blob.
func (r Runner) Check(repo, passwordFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "check", "--read-data")
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic check failed.", Step: "verify", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The local repository is damaged or the password file does not match."}
	}
	return nil
}

// RemovedSnapshot is one snapshot restic forget dropped.
type RemovedSnapshot struct {
	ID    string
	RunID string
}

// ForgetOutcome is the local result of a retention policy. Kept is a snapshot count.
type ForgetOutcome struct {
	Removed []RemovedSnapshot
	Kept    int
}

// Forget applies a retention policy. Snapshots from one site share a repository, but each
// run is stored under its own directory, so restic's default host+path groups would keep
// every run. Grouping by host makes the policy apply to the whole site.
func (r Runner) Forget(repo, passwordFile string, extraEnv []string, siteID string, policy model.Retention, globalOpts ...string) (ForgetOutcome, error) {
	if !policy.Active() {
		return ForgetOutcome{}, nil
	}
	args := append(append([]string{}, globalOpts...), "forget", "--json", "--group-by", "host", "--host", "backupbinder", "--tag", "site="+siteID)
	args = append(args, policy.ForgetArgs()...)
	outcome, err := r.forgetOnce(repo, passwordFile, extraEnv, args...)
	if err != nil && clearStaleLock(r, repo, passwordFile, extraEnv, err, globalOpts...) {
		outcome, err = r.forgetOnce(repo, passwordFile, extraEnv, args...)
	}
	return outcome, err
}

func (r Runner) forgetOnce(repo, passwordFile string, extraEnv []string, args ...string) (ForgetOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	stdout, stderr, code, err := r.run(ctx, repo, passwordFile, extraEnv, nil, args...)
	if err != nil {
		return ForgetOutcome{}, err
	}
	if code != 0 {
		return ForgetOutcome{}, &fail.Error{Code: "step_failed", Message: "restic forget failed.", Step: "retention", ExitCode: fail.ExitCode(code), Stderr: tail(stderr + stdout), Hint: retentionLockHint(stderr + stdout)}
	}
	outcome, err := ParseForget(stdout)
	if err != nil {
		return ForgetOutcome{}, &fail.Error{Code: "step_failed", Message: "restic forget did not report which snapshots it kept.", Step: "retention", Stderr: tail(stdout), Hint: "Snapshots were not removed from the registry."}
	}
	return outcome, nil
}

// ParseForget reads the JSON array from restic forget --json.
func ParseForget(stdout string) (ForgetOutcome, error) {
	stdout = strings.TrimSpace(stdout)
	if i := strings.Index(stdout, "["); i >= 0 {
		stdout = stdout[i:]
	}
	if stdout == "" || stdout == "null" {
		return ForgetOutcome{Removed: []RemovedSnapshot{}}, nil
	}
	var groups []struct {
		Keep   []forgetSnap `json:"keep"`
		Remove []forgetSnap `json:"remove"`
	}
	if err := json.Unmarshal([]byte(stdout), &groups); err != nil {
		return ForgetOutcome{}, err
	}
	out := ForgetOutcome{Removed: []RemovedSnapshot{}}
	for _, group := range groups {
		out.Kept += len(group.Keep)
		for _, snap := range group.Remove {
			out.Removed = append(out.Removed, RemovedSnapshot{ID: snap.ID, RunID: runTag(snap.Tags)})
		}
	}
	return out, nil
}

type forgetSnap struct {
	ID   string   `json:"id"`
	Tags []string `json:"tags"`
}

func runTag(tags []string) string {
	for _, tag := range tags {
		if id, ok := strings.CutPrefix(tag, "run="); ok {
			return id
		}
	}
	return ""
}

// Prune deletes repository data that no snapshot references.
func (r Runner) Prune(repo, passwordFile string, extraEnv []string, globalOpts ...string) error {
	err := r.pruneOnce(repo, passwordFile, extraEnv, globalOpts...)
	if err != nil && clearStaleLock(r, repo, passwordFile, extraEnv, err, globalOpts...) {
		err = r.pruneOnce(repo, passwordFile, extraEnv, globalOpts...)
	}
	return err
}

func (r Runner) pruneOnce(repo, passwordFile string, extraEnv []string, globalOpts ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	args := append(append([]string{}, globalOpts...), "prune")
	_, stderr, code, err := r.run(ctx, repo, passwordFile, extraEnv, nil, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic prune failed.", Step: "retention", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: retentionLockHint(stderr)}
	}
	return nil
}

// Unlock removes restic repository locks.
func (r Runner) Unlock(repo, passwordFile string, extraEnv []string, globalOpts ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	args := append(append([]string{}, globalOpts...), "unlock")
	_, stderr, code, err := r.run(ctx, repo, passwordFile, extraEnv, nil, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic unlock failed.", Step: "retention", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "Remove the leftover lock manually if no backup is running."}
	}
	return nil
}

var lockedByPID = regexp.MustCompile(`(?i)locked by PID\s+(\d+)`)

func clearStaleLock(r Runner, repo, passwordFile string, extraEnv []string, err error, globalOpts ...string) bool {
	if !isRepoLocked(err) || !lockHolderGone(err) {
		return false
	}
	return r.Unlock(repo, passwordFile, extraEnv, globalOpts...) == nil
}

// IsRepoLocked reports whether err is a restic exclusive-lock conflict.
func IsRepoLocked(err error) bool {
	text := strings.ToLower(errText(err))
	return strings.Contains(text, "already locked") || strings.Contains(text, "repository is already locked")
}

func isRepoLocked(err error) bool { return IsRepoLocked(err) }

func lockHolderGone(err error) bool {
	m := lockedByPID.FindStringSubmatch(errText(err))
	if len(m) != 2 {
		// No PID in the message: only clear locks that look abandoned by age wording is risky.
		// Prefer unlocking when restic says the lock is old and we cannot see a holder.
		return strings.Contains(strings.ToLower(errText(err)), "ago")
	}
	pid, convErr := strconv.Atoi(m[1])
	if convErr != nil || pid <= 0 {
		return false
	}
	return !pidAlive(pid)
}

func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if fe, ok := err.(*fail.Error); ok {
		text += " " + fe.Stderr + " " + fe.Hint
	}
	return text
}

func retentionLockHint(stderr string) string {
	if isRepoLocked(&fail.Error{Stderr: stderr}) {
		if lockHolderGone(&fail.Error{Stderr: stderr}) {
			return "A leftover restic lock blocked retention. It should clear on the next run. The latest backup is still stored."
		}
		return "Another restic process still holds the repository lock. Wait for it to finish, then apply retention again. The latest backup is still stored."
	}
	return "Snapshots were not removed. The latest backup is still stored."
}

// Node is one file or directory inside a snapshot.
type Node struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	StructType string `json:"struct_type"`
}

// List returns the files and directories in one snapshot.
func (r Runner) List(repo, passwordFile, snapshotID string) ([]Node, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	stdout, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "ls", "--json", snapshotID)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, &fail.Error{Code: "step_failed", Message: "restic could not list the snapshot.", Step: "verify", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The snapshot id may be wrong, or the password file does not match the repository."}
	}
	var nodes []Node
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var node Node
		if err := json.Unmarshal([]byte(line), &node); err != nil {
			return nil, &fail.Error{Code: "step_failed", Message: "restic could not list the snapshot.", Step: "verify", Stderr: tail(line), Hint: "The snapshot listing was not JSON."}
		}
		if node.StructType != "node" {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Restore extracts a snapshot into target. target must already exist.
func (r Runner) Restore(repo, passwordFile, snapshotID, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "restore", snapshotID, "--target", target)
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic restore failed.", Step: "restore", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The archive did not restore. The live site was not written to."}
	}
	return nil
}

// Copy copies one snapshot into another repository. The destination is initialized first.
// globalOpts are restic global flags placed before the subcommand (for example -o rclone.program=...).
// onStatus receives pack-copy percent updates when restic prints progress; it may be nil.
// Google Drive query-quota failures retry a few times with backoff (limits recover per minute).
func (r Runner) Copy(sourceRepo, passwordFile, destURL, snapshotID string, extraEnv []string, onStatus func(Status), globalOpts ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.copyTimeout())
	defer cancel()
	initArgs := append(append([]string{}, globalOpts...), "init", "--copy-chunker-params", "--from-repo", sourceRepo, "--from-password-file", passwordFile)
	_, stderr, code, err := r.run(ctx, destURL, passwordFile, extraEnv, nil, initArgs...)
	if err != nil {
		return destinationTimeoutError(destURL, "initialize", err, stderr)
	}
	if code != 0 && !strings.Contains(strings.ToLower(stderr), "already") {
		return destinationCopyError(destURL, "initialize", stderr)
	}
	copyArgs := append(append([]string{}, globalOpts...), "copy", "--from-repo", sourceRepo, "--from-password-file", passwordFile, snapshotID)
	env := extraEnv
	var onLine func(string)
	var onStart func(int)
	if onStatus != nil {
		env = withProgressFPS(extraEnv)
		var resticPID atomic.Int64
		onStart = func(pid int) { resticPID.Store(int64(pid)) }
		onLine = func(line string) {
			for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == '\r' }) {
				st, ok := ParseCopyLine(part)
				if !ok {
					continue
				}
				st.WrittenBytes = ReadWriteBytes(int(resticPID.Load()))
				onStatus(st)
			}
		}
	}
	backoffs := []time.Duration{90 * time.Second, 3 * time.Minute, 5 * time.Minute}
	for attempt := 0; ; attempt++ {
		_, stderr, code, err = r.runIn(ctx, "", destURL, passwordFile, env, onLine, onStart, copyArgs...)
		if err != nil {
			return destinationTimeoutError(destURL, "copy", err, stderr)
		}
		if code == 0 {
			return nil
		}
		if attempt >= len(backoffs) || !isDriveQuotaError(strings.ToLower(stderr)) {
			return destinationCopyError(destURL, "copy", stderr)
		}
		if err := sleepCtx(ctx, backoffs[attempt]); err != nil {
			return destinationTimeoutError(destURL, "copy", &fail.Error{
				Code: "destination_failed", Message: "The rclone destination " + destURL + " copy timed out.",
			}, stderr)
		}
	}
}

// withProgressFPS asks restic to emit copy progress lines even when stdout is not a TTY.
func withProgressFPS(extra []string) []string {
	for _, item := range extra {
		if strings.HasPrefix(item, "RESTIC_PROGRESS_FPS=") {
			return extra
		}
	}
	return append(append([]string{}, extra...), "RESTIC_PROGRESS_FPS=2")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// copyTimeout is long enough for a large offsite copy even when upload is rate-limited.
// At 100 KiB/s, 2 hours only moves ~700 MiB; multi-gigabyte Drive copies need more time.
func (r Runner) copyTimeout() time.Duration {
	const min = 2 * time.Hour
	const max = 24 * time.Hour
	if r.LimitUpload <= 0 {
		return min
	}
	// Budget for about 16 GiB at the configured upload cap, plus headroom.
	seconds := (16 * 1024 * 1024) / r.LimitUpload
	d := time.Duration(seconds) * time.Second
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

func destinationTimeoutError(destURL, op string, err error, stderr string) error {
	fe, ok := err.(*fail.Error)
	if !ok || fe == nil || !strings.Contains(strings.ToLower(fe.Message), "timed out") {
		return err
	}
	message := "The rclone destination " + destURL + " " + op + " timed out."
	hint := "The local snapshot is already stored. A low upload speed limit can make Google Drive copies take many hours; raise or clear the site upload limit, then back up again."
	if detail := firstUsefulLine(stderr); detail != "" {
		message += " " + detail
	}
	return &fail.Error{Code: "destination_failed", Message: message, Step: "destination", Stderr: tail(stderr), Hint: hint}
}

func destinationCopyError(destURL, op, stderr string) error {
	message := "The destination restic repository could not be initialized."
	hint := "Check the destination URL and credentials. The local snapshot is already stored."
	if op == "copy" {
		message = "restic could not copy the snapshot to that destination."
		hint = "The local snapshot is safe. Fix the destination and back up again to copy it."
	}
	if strings.HasPrefix(destURL, "rclone:") {
		message, hint = rcloneDestinationHint(destURL, op, stderr)
	}
	return &fail.Error{
		Code: "destination_failed", Message: message, Step: "destination",
		Stderr: tail(stderr), Hint: hint,
	}
}

// rcloneDestinationHint turns restic/rclone stderr into a fixable message.
func rcloneDestinationHint(destURL, op, stderr string) (string, string) {
	remote, path := splitRcloneURL(destURL)
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "didn't find section") || (strings.Contains(lower, "config file") && strings.Contains(lower, "not found")):
		name := remote
		if name == "" {
			name = "that remote"
		}
		return "Google Drive / rclone remote " + quote(name) + " is not configured.",
			"Run rclone config and create a Google Drive remote named " + quote(name) + ". Then back up again. The local snapshot is already stored."
	case isDriveQuotaError(lower):
		return "Google Drive rate-limited the " + op + " to " + destURL + " (API queries per minute).",
			"Wait about 15 minutes, then back up again — the local snapshot is already stored. If this keeps happening, set your own Google client_id in the rclone remote (rclone config edit " + quote(remote) + ") instead of the shared rclone client."
	case strings.Contains(lower, "failed to create file system"):
		return "rclone could not open " + destURL + ".",
			"Check that rclone remote " + quote(remote) + " exists (rclone listremotes) and the folder path is correct. The local snapshot is already stored."
	case strings.Contains(lower, "token expired") || strings.Contains(lower, "unable to refresh") || strings.Contains(lower, "oauth"):
		return "The Google Drive / rclone login for " + quote(remote) + " needs to be renewed.",
			"Run rclone config reconnect " + quote(remote) + ": and authorize again. The local snapshot is already stored."
	case strings.Contains(lower, "executable file not found") || strings.Contains(lower, "rclone:") && strings.Contains(lower, "not found"):
		return "rclone is not available for this destination.",
			"Install rclone, put it on PATH, or set BACKUPBINDER_RCLONE / .tools/rclone. The local snapshot is already stored."
	default:
		detail := firstUsefulLine(stderr)
		message := "Could not " + op + " the rclone destination " + destURL + "."
		if detail != "" {
			message += " " + detail
		}
		hint := "Fix the rclone remote"
		if remote != "" {
			hint += " " + quote(remote)
		}
		if path != "" {
			hint += " and folder " + quote(path)
		}
		hint += ", then back up again. The local snapshot is already stored."
		return message, hint
	}
}

func splitRcloneURL(destURL string) (remote, path string) {
	rest := strings.TrimPrefix(destURL, "rclone:")
	remote, path, _ = strings.Cut(rest, ":")
	return remote, path
}

func isDriveQuotaError(lowerStderr string) bool {
	return strings.Contains(lowerStderr, "quota exceeded") ||
		strings.Contains(lowerStderr, "ratelimitexceeded") ||
		strings.Contains(lowerStderr, "userratelimitexceeded") ||
		strings.Contains(lowerStderr, "rate limit") ||
		(strings.Contains(lowerStderr, "403") && strings.Contains(lowerStderr, "queries"))
}

func quote(s string) string {
	if s == "" {
		return s
	}
	return "'" + s + "'"
}

func firstUsefulLine(stderr string) string {
	var fallback string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		// rclone NOTICE/INFO lines are warnings, not the failure cause.
		if strings.Contains(lower, " notice:") || strings.Contains(lower, " info:") {
			continue
		}
		if strings.Contains(lower, "fatal") || strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "critical") || strings.Contains(lower, "timed out") {
			if len(line) > 240 {
				return line[:240]
			}
			return line
		}
		if fallback == "" && strings.HasPrefix(lower, "rclone:") {
			fallback = line
		}
	}
	if len(fallback) > 240 {
		return fallback[:240]
	}
	return fallback
}

func (r Runner) run(ctx context.Context, repo, passwordFile string, extraEnv []string, onStdout func(string), args ...string) (string, string, int, error) {
	return r.runIn(ctx, "", repo, passwordFile, extraEnv, onStdout, nil, args...)
}

func (r Runner) runIn(ctx context.Context, dir, repo, passwordFile string, extraEnv []string, onStdout func(string), onStart func(int), args ...string) (string, string, int, error) {
	argv := r.argv(repo, passwordFile, args...)
	cmd := exec.CommandContext(ctx, r.Binary, argv...)
	cmd.Dir = dir
	cmd.Env = filteredEnv(extraEnv)
	cmd.WaitDelay = 2 * time.Second
	if onStdout == nil {
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return commandResult(ctx, stdout.String(), stderr.String(), cmd, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", 0, &fail.Error{Code: "step_failed", Message: "restic could not be started.", Step: "restic", Hint: err.Error()}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", 0, &fail.Error{Code: "step_failed", Message: "restic could not be started.", Step: "restic", Hint: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return commandResult(ctx, "", "", cmd, err)
	}
	if onStart != nil && cmd.Process != nil {
		onStart(cmd.Process.Pid)
	}
	var stdout tailBuf
	var stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scan := bufio.NewScanner(stdoutPipe)
		scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scan.Scan() {
			line := scan.Text()
			stdout.add(line)
			onStdout(line)
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderr, stderrPipe)
	}()
	wg.Wait()
	err = cmd.Wait()
	return commandResult(ctx, stdout.String(), stderr.String(), cmd, err)
}

func commandResult(ctx context.Context, stdout, stderr string, cmd *exec.Cmd, err error) (string, string, int, error) {
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return stdout, stderr, code, &fail.Error{Code: "step_failed", Message: "restic timed out.", Step: "restic", Stderr: tail(stderr)}
	}
	if err != nil && cmd.ProcessState == nil {
		return stdout, stderr, code, &fail.Error{Code: "step_failed", Message: "restic could not be started.", Step: "restic", Hint: err.Error()}
	}
	return stdout, stderr, code, nil
}

// tailBuf keeps the end of a stream for error reports.
type tailBuf struct {
	buf []byte
}

func (t *tailBuf) add(line string) {
	t.buf = append(t.buf, line...)
	t.buf = append(t.buf, '\n')
	if len(t.buf) > 8000 {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-8000:]...)
	}
}

func (t *tailBuf) String() string { return string(t.buf) }

// ReadWriteBytes is how many bytes this process has sent to storage.
// It is zero when the process is gone or the system does not expose that counter.
func ReadWriteBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/io")
	if err != nil {
		return 0
	}
	return parseWriteBytes(string(raw))
}

func parseWriteBytes(raw string) int64 {
	for _, line := range strings.Split(raw, "\n") {
		const prefix = "write_bytes:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(line[len(prefix):]), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// argv places restic global options, including speed limits, before the subcommand.
func (r Runner) argv(repo, passwordFile string, args ...string) []string {
	out := []string{"--cache-dir", r.CacheDir, "-r", repo, "--password-file", passwordFile}
	if r.LimitUpload > 0 {
		out = append(out, "--limit-upload", strconv.Itoa(r.LimitUpload))
	}
	if r.LimitDownload > 0 {
		out = append(out, "--limit-download", strconv.Itoa(r.LimitDownload))
	}
	return append(out, args...)
}

func filteredEnv(extra []string) []string {
	var env []string
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "RESTIC_REPOSITORY=") || strings.HasPrefix(item, "RESTIC_PASSWORD=") || strings.HasPrefix(item, "RESTIC_PASSWORD_FILE=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, extra...)
}

func tail(s string) string {
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}

// DestinationURL builds a restic repository URL. It does not open a connection.
func DestinationURL(dest model.Destination) (string, error) {
	kind, err := destination.ByID(dest.Kind)
	if err != nil {
		return "", err
	}
	return kind.URL(dest)
}
