package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"local-backup/internal/fail"
	"local-backup/internal/model"
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

func syncServer(loc model.Location, dest string, onAttempt func(int)) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return fail.New("step_failed", "rsync is not installed, so the server files cannot be copied.")
	}
	var stderr bytes.Buffer
	for attempt := 1; attempt <= serverCopyAttempts; attempt++ {
		if onAttempt != nil {
			onAttempt(attempt)
		}
		stderr.Reset()
		cmd := exec.Command("rsync", rsyncArgs(loc, dest)...)
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			return nil
		}
		if attempt == serverCopyAttempts || !retryableCopy(stderr.String()) {
			return copyFailed(stderr.String())
		}
		time.Sleep(5 * time.Second)
	}
	return copyFailed(stderr.String())
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

func rsyncArgs(loc model.Location, dest string) []string {
	remote := loc.User + "@" + loc.Host + ":" + loc.Path
	if !strings.HasSuffix(remote, "/") {
		remote += "/"
	}
	if !strings.HasSuffix(dest, string(os.PathSeparator)) && !strings.HasSuffix(dest, "/") {
		dest += string(os.PathSeparator)
	}
	return []string{
		"-a",
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
	}
}
