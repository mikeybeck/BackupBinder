package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// StartBackupDetached runs create_backup in a new process and session so restarting
// the UI server does not cancel an in-flight backup. It returns the run id to poll.
func (a *App) StartBackupDetached(siteID string) (string, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return "", err
	}
	run, err := a.Latest(siteID)
	if err == nil && run.InProgress() {
		return "", fail.New("backup_busy", "A backup is already running for this site.")
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "", fail.New("step_failed", "Could not find the backupbinder binary to start the backup.")
	}
	if err := os.MkdirAll(filepath.Join(a.Home, "runs", site.ID), 0o700); err != nil {
		return "", err
	}
	runID, err := a.newRunID(site.ID)
	if err != nil {
		return "", err
	}
	started := time.Now().UTC().Format(time.RFC3339)
	if err := a.DB.BeginRun(site.ID, runID, started); err != nil {
		return "", err
	}
	_ = a.writeProgress(site.ID, runID, model.Progress{Phase: "Starting the backup"})
	logPath := filepath.Join(a.Home, "runs", site.ID, "detached.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe, "--home", a.Home, "create_backup", "--id", site.ID, "--root", site.Root)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(withoutEnv(os.Environ(), "BACKUPBINDER_RUN_ID="), "BACKUPBINDER_RUN_ID="+runID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return "", fail.Wrap("step_failed", "Could not start the backup process", err)
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	return runID, nil
}

func withoutEnv(env []string, prefix string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
			continue
		}
		out = append(out, item)
	}
	return out
}

// StartRestoreDetached runs test_restore or restore_backup in a new session.
// command is one of those two names. runID and scratch are optional.
func (a *App) StartRestoreDetached(siteID, command, runID, scratch string) error {
	if command != "test_restore" && command != "restore_backup" {
		return fail.New("bad_command", "Detach runs test_restore or restore_backup.")
	}
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return err
	}
	if status, ok := a.ReadRestoreStatus(siteID); ok && status.InProgress {
		return fail.New("restore_busy", "A restore is already running for this site.")
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return fail.New("step_failed", "Could not find the backupbinder binary to start the restore.")
	}
	if err := os.MkdirAll(filepath.Join(a.Home, "runs", site.ID), 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(a.Home, "runs", site.ID, "restore.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	args := []string{"--home", a.Home, command, "--id", site.ID, "--root", site.Root}
	if runID != "" {
		args = append(args, "--run", runID)
	}
	if scratch != "" {
		args = append(args, "--scratch", scratch)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fail.Wrap("step_failed", "Could not start the restore check", err)
	}
	if cmd.Process != nil {
		_ = a.writeRestoreStatus(site.ID, RestoreStatus{InProgress: true, PID: cmd.Process.Pid})
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil || err == syscall.EPERM {
		return true
	}
	return false
}
