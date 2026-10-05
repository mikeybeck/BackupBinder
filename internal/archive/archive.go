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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"local-backup/internal/fail"
	"local-backup/internal/model"
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
	}, func(pid int) { resticPID.Store(int64(pid)) }, "backup", "--json", "--verbose", "--host", "local-backup", "--tag", "site="+siteID, "--tag", "run="+runID, ".")
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

// Check verifies the repository structure.
func (r Runner) Check(repo, passwordFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "check")
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
func (r Runner) Forget(repo, passwordFile string, extraEnv []string, siteID string, policy model.Retention) (ForgetOutcome, error) {
	if !policy.Active() {
		return ForgetOutcome{}, nil
	}
	args := []string{"forget", "--json", "--group-by", "host", "--host", "local-backup", "--tag", "site=" + siteID}
	args = append(args, policy.ForgetArgs()...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	stdout, stderr, code, err := r.run(ctx, repo, passwordFile, extraEnv, nil, args...)
	if err != nil {
		return ForgetOutcome{}, err
	}
	if code != 0 {
		return ForgetOutcome{}, &fail.Error{Code: "step_failed", Message: "restic forget failed.", Step: "retention", ExitCode: fail.ExitCode(code), Stderr: tail(stderr + stdout), Hint: "Snapshots were not removed. The latest backup is still stored."}
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
func (r Runner) Prune(repo, passwordFile string, extraEnv []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, stderr, code, err := r.run(ctx, repo, passwordFile, extraEnv, nil, "prune")
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "step_failed", Message: "restic prune failed.", Step: "retention", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The snapshots are already forgotten. The next backup will try to free the space."}
	}
	return nil
}

// List returns snapshot paths that start with /.
func (r Runner) List(repo, passwordFile, snapshotID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	stdout, stderr, code, err := r.run(ctx, repo, passwordFile, nil, nil, "ls", snapshotID)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, &fail.Error{Code: "step_failed", Message: "restic could not list the snapshot.", Step: "verify", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The snapshot id may be wrong, or the password file does not match the repository."}
	}
	var paths []string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "/") {
			paths = append(paths, line)
		}
	}
	return paths, nil
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
func (r Runner) Copy(sourceRepo, passwordFile, destURL, snapshotID string, extraEnv []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, stderr, code, err := r.run(ctx, destURL, passwordFile, extraEnv, nil, "init", "--copy-chunker-params", "--from-repo", sourceRepo, "--from-password-file", passwordFile)
	if err != nil {
		return err
	}
	if code != 0 && !strings.Contains(strings.ToLower(stderr), "already") {
		return &fail.Error{Code: "destination_failed", Message: "The destination restic repository could not be initialized.", Step: "destination", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "Check the destination URL and credentials. The local snapshot is already stored."}
	}
	_, stderr, code, err = r.run(ctx, destURL, passwordFile, extraEnv, nil, "copy", "--from-repo", sourceRepo, "--from-password-file", passwordFile, snapshotID)
	if err != nil {
		return err
	}
	if code != 0 {
		return &fail.Error{Code: "destination_failed", Message: "restic could not copy the snapshot to that destination.", Step: "destination", ExitCode: fail.ExitCode(code), Stderr: tail(stderr), Hint: "The local snapshot is safe. Fix the destination and back up again to copy it."}
	}
	return nil
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
	switch dest.Kind {
	case "sftp":
		path := dest.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return "sftp:" + dest.User + "@" + dest.Host + ":" + path, nil
	case "s3":
		endpoint := strings.TrimSpace(dest.Endpoint)
		if endpoint == "" {
			endpoint = "s3.amazonaws.com"
		}
		endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
		endpoint = strings.Trim(endpoint, "/")
		bucket := strings.Trim(dest.Bucket, "/")
		base := "s3:" + endpoint + "/" + bucket
		prefix := strings.Trim(dest.Prefix, "/")
		if prefix != "" {
			return base + "/" + prefix, nil
		}
		return base, nil
	case "b2":
		prefix := strings.Trim(dest.Prefix, "/")
		if prefix != "" {
			return "b2:" + dest.Bucket + ":" + prefix, nil
		}
		return "b2:" + dest.Bucket + ":/", nil
	case "disk":
		return dest.Path, nil
	default:
		return "", fail.New("bad_destination", "Unknown destination kind "+dest.Kind+".")
	}
}
