package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/fail"
)

func TestSpeedLimitsReachRestic(t *testing.T) {
	application := openTest(t)
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Limited", "limited", true); err != nil {
		t.Fatal(err)
	}
	upload, download := 64, 32
	if _, err := application.SetLimits("limited", &upload, &download); err != nil {
		t.Fatal(err)
	}
	kept := 64
	if _, err := application.SetLimits("limited", nil, &kept); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("limited")
	if err != nil {
		t.Fatal(err)
	}
	if site.Limits.UploadKiB != 64 || site.Limits.DownloadKiB != 64 {
		t.Fatalf("partial update %#v", site.Limits)
	}
	download = 32
	if _, err := application.SetLimits("limited", nil, &download); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(application.Home, "restic-args.log")
	wrapper := filepath.Join(application.Home, "restic-wrap.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\nexec " + shellQuote(application.Restic.Binary) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	application.Restic.Binary = wrapper

	manifest, err := application.CreateBackup("limited")
	if err != nil {
		t.Fatal(err)
	}
	var sawCommand bool
	for _, step := range manifest.Restore.Steps {
		if strings.Contains(step, "restore_backup --id limited --root ") && strings.Contains(step, "Omit --scratch") {
			sawCommand = true
		}
	}
	if !sawCommand {
		t.Fatal(manifest.Restore.Steps)
	}
	if _, err := application.Verify("limited", manifest.Snapshot.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := application.TestRestore("limited", manifest.Snapshot.RunID, ""); err != nil {
		t.Fatal(err)
	}
	logged := mustRead(t, logPath)
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		if !limitsBeforeCommand(line) {
			t.Fatal(line)
		}
	}

	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	zero := 0
	if _, err := application.SetLimits("limited", &zero, &zero); err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateBackup("limited"); err != nil {
		t.Fatal(err)
	}
	cleared := mustRead(t, logPath)
	if strings.Contains(cleared, "--limit-") {
		t.Fatal(cleared)
	}
}

func TestParseKiB(t *testing.T) {
	n, set, err := ParseKiB(" 1024 ")
	if err != nil || !set || n != 1024 {
		t.Fatal(n, set, err)
	}
	n, set, err = ParseKiB("")
	if err != nil || set || n != 0 {
		t.Fatal(n, set, err)
	}
	n, set, err = ParseKiB("0")
	if err != nil || !set || n != 0 {
		t.Fatal(n, set, err)
	}
	_, _, err = ParseKiB("-1")
	if err == nil {
		t.Fatal("negative accepted")
	}
	_, _, err = ParseKiB("fast")
	fe, ok := err.(*fail.Error)
	if !ok || fe.Code != "bad_limit" {
		t.Fatal(err)
	}
}

func limitsBeforeCommand(line string) bool {
	fields := strings.Fields(line)
	upload, download, cmd := -1, -1, -1
	for i, field := range fields {
		switch field {
		case "--limit-upload":
			upload = i
		case "--limit-download":
			download = i
		case "init", "backup", "check", "ls", "restore", "copy", "forget", "prune":
			if cmd < 0 {
				cmd = i
			}
		}
	}
	return upload >= 0 && download >= 0 && upload < cmd && download < cmd
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
