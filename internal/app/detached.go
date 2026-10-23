package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"local-backup/internal/fail"
)

// StartBackupDetached runs create_backup in a new process and session so restarting
// the UI server does not cancel an in-flight backup.
func (a *App) StartBackupDetached(siteID string) error {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return err
	}
	run, err := a.Latest(siteID)
	if err == nil && run.InProgress() {
		return fail.New("backup_busy", "A backup is already running for this site.")
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return fail.New("step_failed", "Could not find the local-backup binary to start the backup.")
	}
	if err := os.MkdirAll(filepath.Join(a.Home, "runs", site.ID), 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(a.Home, "runs", site.ID, "detached.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--home", a.Home, "create_backup", "--id", site.ID, "--root", site.Root)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fail.Wrap("step_failed", "Could not start the backup process", err)
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := a.Latest(siteID)
		if err == nil && run.InProgress() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}
