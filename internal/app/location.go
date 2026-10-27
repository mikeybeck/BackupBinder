package app

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backupbinder/internal/capture"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// SetLocation saves the one place this site's files are read from.
// A server replaces any previous server. The local root stays the copy on this machine.
func (a *App) SetLocation(siteID string, loc model.Location) (model.Site, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Site{}, err
	}
	switch strings.TrimSpace(loc.Kind) {
	case "", "local":
		path := strings.TrimSpace(loc.Path)
		if path == "" {
			path = site.Root
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return model.Site{}, err
		}
		info, statErr := os.Stat(abs)
		if statErr != nil || !info.IsDir() {
			return model.Site{}, fail.New("missing_path", "The directory is missing: "+abs)
		}
		site.Root = abs
		site.Location = model.Location{Kind: "local"}
	case "sftp":
		loc.Host = strings.TrimSpace(loc.Host)
		loc.User = strings.TrimSpace(loc.User)
		loc.Path = strings.TrimSpace(loc.Path)
		if loc.Host == "" || loc.User == "" || loc.Path == "" {
			return model.Site{}, fail.New("bad_location", "A server needs a host, a user, and an absolute path.")
		}
		if !hostPattern.MatchString(loc.Host) {
			return model.Site{}, fail.New("bad_location", "SSH host must be a hostname, not a shell command.")
		}
		if !userPattern.MatchString(loc.User) {
			return model.Site{}, fail.New("bad_location", "SSH user must be a plain username.")
		}
		if !remotePath.MatchString(loc.Path) {
			return model.Site{}, fail.New("bad_location", "The server path must be an absolute path without spaces.")
		}
		site.Location = model.Location{Kind: "sftp", Host: loc.Host, User: loc.User, Path: loc.Path}
	default:
		return model.Site{}, fail.New("bad_location", "Choose a directory on this machine or one server.")
	}
	if err := a.DB.SaveLocation(site.ID, site.Root, site.Location); err != nil {
		return model.Site{}, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Site{}, err
	}
	return a.DB.GetSite(site.ID)
}

const serverCopyAttempts = 4

// SizeEstimate is how large a site looks before a backup starts.
type SizeEstimate struct {
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	Source string `json:"source"`
	Note   string `json:"note,omitempty"`
}

// EstimateSource counts files and bytes at the saved location without starting a backup.
// For a server it uses an rsync dry-run with the same excludes as the copy step.
func (a *App) EstimateSource(siteID string) (SizeEstimate, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return SizeEstimate{}, err
	}
	return estimateSource(site)
}

func estimateSource(site model.Site) (SizeEstimate, error) {
	if site.Location.Kind == "sftp" {
		return estimateServer(site.Location)
	}
	excludes := fileExcludes(site)
	files, err := capture.EstimateFiles(site.Root, excludes)
	if err != nil {
		return SizeEstimate{}, fail.Wrap("step_failed", "could not estimate site size", err)
	}
	return SizeEstimate{
		Files:  files.FileCount,
		Bytes:  files.ByteSize,
		Source: "local directory",
		Note:   "Matches the exclude rules used when reading files for a backup. Database dumps are not included.",
	}, nil
}

func estimateServer(loc model.Location) (SizeEstimate, error) {
	if _, err := exec.LookPath("rsync"); err != nil {
		return SizeEstimate{}, fail.New("step_failed", "rsync is not installed, so the server size cannot be checked.")
	}
	dest, err := os.MkdirTemp("", "backupbinder-estimate-*")
	if err != nil {
		return SizeEstimate{}, err
	}
	defer os.RemoveAll(dest)
	var out bytes.Buffer
	cmd := exec.Command("rsync", rsyncArgs(loc, dest, true)...)
	capture.ApplySSHAgent(cmd)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(out.String())
		if detail == "" {
			detail = err.Error()
		}
		return SizeEstimate{}, fail.New("step_failed", "Could not measure the server path. "+firstLine(detail))
	}
	files, bytes := parseRsyncStats(out.String())
	return SizeEstimate{
		Files:  files,
		Bytes:  bytes,
		Source: loc.User + "@" + loc.Host + ":" + loc.Path,
		Note:   "Measured with the same excludes as the next backup copy. Database dumps are not included.",
	}, nil
}

var (
	rsyncFileCount = regexp.MustCompile(`(?i)Number of files:\s*([\d,]+)`)
	rsyncTotalSize = regexp.MustCompile(`(?i)Total file size:\s*([\d,]+)\s*bytes`)
)

func parseRsyncStats(text string) (files int, bytes int64) {
	if m := rsyncFileCount.FindStringSubmatch(text); len(m) == 2 {
		files, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	}
	if m := rsyncTotalSize.FindStringSubmatch(text); len(m) == 2 {
		bytes, _ = strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	}
	return files, bytes
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 240 {
				return line[:240]
			}
			return line
		}
	}
	return text
}

func syncServer(loc model.Location, dest string, onAttempt func(int), onProgress func(percent int, detail string)) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return fail.New("step_failed", "rsync is not installed, so the server files cannot be copied.")
	}
	var stderr bytes.Buffer
	for attempt := 1; attempt <= serverCopyAttempts; attempt++ {
		if onAttempt != nil {
			onAttempt(attempt)
		}
		stderr.Reset()
		cmd := exec.Command("rsync", rsyncArgs(loc, dest, false)...)
		capture.ApplySSHAgent(cmd)
		// rsync --info=progress2 writes progress to stdout (not stderr) on current rsync.
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = readRsyncProgress(stdout, onProgress)
		waitErr := cmd.Wait()
		if waitErr == nil {
			return nil
		}
		if attempt == serverCopyAttempts || !retryableCopy(stderr.String()) {
			return copyFailed(stderr.String())
		}
		time.Sleep(5 * time.Second)
	}
	return copyFailed(stderr.String())
}

var rsyncPercent = regexp.MustCompile(`(\d+)%`)

func readRsyncProgress(r io.Reader, onProgress func(percent int, detail string)) string {
	var collected strings.Builder
	scanner := bufio.NewScanner(r)
	scanner.Split(scanRsyncLines)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		collected.WriteString(line)
		collected.WriteByte('\n')
		if onProgress == nil {
			continue
		}
		if m := rsyncPercent.FindStringSubmatch(line); len(m) == 2 {
			pct, _ := strconv.Atoi(m[1])
			if pct >= 0 && pct <= 100 {
				onProgress(pct, line)
			}
		}
	}
	return collected.String()
}

// scanRsyncLines splits on newline or carriage return so --info=progress2 updates are seen.
func scanRsyncLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func retryableCopy(stderr string) bool {
	text := strings.ToLower(stderr)
	for _, part := range []string{"permission denied", "no such file", "authentication", "host key verification"} {
		if strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func copyFailed(stderr string) *fail.Error {
	hint := strings.TrimSpace(stderr)
	if len(hint) > 500 {
		hint = hint[len(hint)-500:]
	}
	if hint == "" {
		hint = "Check the host, user, path, and that this machine's SSH key can log in. No password is asked."
	}
	hint += " Files already copied are kept. Start the backup again."
	return &fail.Error{Code: "step_failed", Message: "The server files could not be copied.", Step: "files", Hint: hint}
}

func rsyncArgs(loc model.Location, dest string, dryRun bool) []string {
	remote := loc.User + "@" + loc.Host + ":" + loc.Path
	if !strings.HasSuffix(remote, "/") {
		remote += "/"
	}
	if !strings.HasSuffix(dest, string(os.PathSeparator)) && !strings.HasSuffix(dest, "/") {
		dest += string(os.PathSeparator)
	}
	args := []string{"-a"}
	if dryRun {
		args = append(args, "--dry-run", "--stats")
	} else {
		// progress2 is on stdout; outbuf=N keeps percent updates flowing through pipes.
		args = append(args, "--info=progress2", "--outbuf=N")
	}
	args = append(args,
		"--exclude", "node_modules",
		"--exclude", "cache",
		"--exclude", ".cache",
		"--exclude", ".git",
		"--exclude", "bower_components",
		"--exclude", "__pycache__",
		"--exclude", ".sass-cache",
		"--exclude", ".parcel-cache",
		"--exclude", "coverage",
		"--exclude", "wp-content/upgrade",
		"--exclude", "wp-content/debug.log",
		"--partial",
		"--timeout=120",
		"-e", "ssh -o BatchMode=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=4",
		remote,
		dest,
	)
	return args
}
