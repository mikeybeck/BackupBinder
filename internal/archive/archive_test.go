package archive

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

func TestDestinationURL(t *testing.T) {
	sftp, err := DestinationURL(model.Destination{Kind: "sftp", User: "backup", Host: "files.example", Path: "/repos/site"})
	if err != nil || sftp != "sftp:backup@files.example:/repos/site" {
		t.Fatal(sftp, err)
	}
	s3, err := DestinationURL(model.Destination{Kind: "s3", Bucket: "site-backups", Prefix: "a"})
	if err != nil || s3 != "s3:s3.amazonaws.com/site-backups/a" {
		t.Fatal(s3, err)
	}
	b2, err := DestinationURL(model.Destination{Kind: "b2", Bucket: "bucket", Prefix: "site"})
	if err != nil || b2 != "b2:bucket:site" {
		t.Fatal(b2, err)
	}
	drive, err := DestinationURL(model.Destination{Kind: "gdrive", Host: "gdrive", Path: "backupbinder/site"})
	if err != nil || drive != "rclone:gdrive:backupbinder/site" {
		t.Fatal(drive, err)
	}
}

func TestRcloneDestinationHintMissingRemote(t *testing.T) {
	err := destinationCopyError("rclone:gdrive:backupbinder/site", "initialize", `Fatal: create repository at rclone:gdrive:backupbinder/site failed: error listing files: didn't find section in config file ("gdrive")`)
	fe, ok := err.(*fail.Error)
	if !ok {
		t.Fatal(err)
	}
	if !strings.Contains(fe.Message, "gdrive") || !strings.Contains(fe.Message, "not configured") {
		t.Fatal(fe.Message)
	}
	if !strings.Contains(fe.Hint, "rclone config") || !strings.Contains(fe.Hint, "'gdrive'") {
		t.Fatal(fe.Hint)
	}
}

func TestRcloneDestinationHintQuota(t *testing.T) {
	stderr := `rclone: 2026/10/19 16:18:30 ERROR : data/8e/8e0b: Post request put error: googleapi: Error 403: Quota exceeded for quota metric 'Queries' and limit 'Previous quota: Requests per minute'
Fatal: copy to rclone:gdrive:backupbinder/wpfoundry.app failed`
	err := destinationCopyError("rclone:gdrive:backupbinder/wpfoundry.app", "copy", stderr)
	fe, ok := err.(*fail.Error)
	if !ok {
		t.Fatal(err)
	}
	if !strings.Contains(fe.Message, "rate-limited") {
		t.Fatal(fe.Message)
	}
	if !strings.Contains(fe.Hint, "15 minutes") || !strings.Contains(fe.Hint, "client_id") {
		t.Fatal(fe.Hint)
	}
}

func TestFirstUsefulLineSkipsRcloneNotice(t *testing.T) {
	stderr := "rclone: 2026/10/19 14:50:49 NOTICE: gdrive: shared client_id\nFatal: copy to rclone:gdrive:site failed: context deadline exceeded\n"
	got := firstUsefulLine(stderr)
	if !strings.Contains(got, "Fatal") || strings.Contains(got, "NOTICE") {
		t.Fatal(got)
	}
}

func TestCopyTimeoutScalesWithUploadLimit(t *testing.T) {
	unlimited := Runner{}.copyTimeout()
	if unlimited != 2*time.Hour {
		t.Fatal(unlimited)
	}
	limited := Runner{LimitUpload: 100}.copyTimeout()
	// 16 GiB at 100 KiB/s is well above 2 hours.
	if limited <= 2*time.Hour || limited > 24*time.Hour {
		t.Fatal(limited)
	}
}

func TestLockHolderGone(t *testing.T) {
	stderr := "unable to create lock in backend: repository is already locked by PID 3645610 on mikey-think by mikey (UID 1000, GID 1000)\nlock was created at 2026-10-20 10:47:33 (4h7m3.237511364s ago)\nstorage ID 57c68fd3"
	err := &fail.Error{Message: "restic forget failed.", Stderr: stderr}
	if !IsRepoLocked(err) {
		t.Fatal("expected locked")
	}
	if !lockHolderGone(err) {
		t.Fatal("expected dead PID to clear")
	}
	live := &fail.Error{Stderr: "repository is already locked by PID " + strconv.Itoa(os.Getpid()) + " on host"}
	if lockHolderGone(live) {
		t.Fatal("live PID should not clear")
	}
	hint := retentionLockHint(stderr)
	if !strings.Contains(hint, "leftover") {
		t.Fatal(hint)
	}
}

func TestParseCopyLine(t *testing.T) {
	st, ok := ParseCopyLine(`[0:12] 75.00%  3 / 4 packs copied`)
	if !ok || !st.HasPercent || st.Percent < 0.74 || st.Percent > 0.76 || st.FilesDone != 3 || st.FilesTotal != 4 {
		t.Fatalf("%#v", st)
	}
	if st.File != "3 of 4 packs" {
		t.Fatal(st.File)
	}
	if _, ok := ParseCopyLine(`snapshot abc saved`); ok {
		t.Fatal("expected non-progress")
	}
}

func TestParseBackupLine(t *testing.T) {
	status, ok := ParseBackupLine(`{"message_type":"status","percent_done":0.881,"total_files":8,"files_done":4,"total_bytes":9000,"current_files":["/tmp/src/photo-7.bin","/tmp/src/photo-8.bin"]}`)
	if !ok || !status.HasPercent || status.Percent < 0.88 || status.File != "/tmp/src/photo-8.bin" || status.FilesDone != 4 || status.FilesTotal != 8 || !status.HasBytes || status.BytesTotal != 9000 {
		t.Fatalf("%#v", status)
	}
	file, ok := ParseBackupLine(`{"message_type":"verbose_status","action":"new","item":"/tmp/src/photo-1.bin"}`)
	if !ok || file.File != "/tmp/src/photo-1.bin" || file.HasPercent {
		t.Fatalf("%#v", file)
	}
	if _, ok := ParseBackupLine(`{"message_type":"verbose_status","action":"scan_finished","item":""}`); ok {
		t.Fatal("scan_finished")
	}
	done, ok := ParseBackupLine(`{"message_type":"summary","snapshot_id":"abc","data_added":12}`)
	if !ok || !done.Done || done.SnapshotID != "abc" || done.DataAdded != 12 {
		t.Fatalf("%#v", done)
	}
}

func TestParseWriteBytes(t *testing.T) {
	raw := "rchar: 10\nwchar: 20\nread_bytes: 0\nwrite_bytes: 1048576\ncancelled_write_bytes: 0\n"
	if parseWriteBytes(raw) != 1048576 {
		t.Fatal(parseWriteBytes(raw))
	}
	if parseWriteBytes("write_bytes: no\n") != 0 {
		t.Fatal("bad counter")
	}
	if ReadWriteBytes(0) != 0 || ReadWriteBytes(1<<30) != 0 {
		t.Fatal("missing process")
	}
}

func TestParseForget(t *testing.T) {
	out, err := ParseForget(`[{"keep":[{"id":"aaa","tags":["site=demo","run=new"]}],"remove":[{"id":"bbb","tags":["run=old"]},{"id":"ccc","tags":["site=demo"]}],"reasons":[]},{"keep":[],"remove":null}]`)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kept != 1 || len(out.Removed) != 2 || out.Removed[0].RunID != "old" || out.Removed[0].ID != "bbb" || out.Removed[1].RunID != "" {
		t.Fatalf("%#v", out)
	}
	empty, err := ParseForget(" \n")
	if err != nil || len(empty.Removed) != 0 {
		t.Fatal(err, empty)
	}
	if _, err := ParseForget("not-json"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestArgvIncludesSpeedLimits(t *testing.T) {
	limited := Runner{CacheDir: "/cache", LimitUpload: 1024, LimitDownload: 256}
	got := strings.Join(limited.argv("/repo", "/secret", "backup", "."), " ")
	want := "--cache-dir /cache -r /repo --password-file /secret --limit-upload 1024 --limit-download 256 backup ."
	if got != want {
		t.Fatal(got)
	}
	plain := strings.Join(Runner{CacheDir: "/cache"}.argv("/repo", "/secret", "restore", "snap", "--target", "/scratch"), " ")
	if strings.Contains(plain, "limit") {
		t.Fatal(plain)
	}
	if !strings.Contains(plain, "--password-file /secret restore snap --target /scratch") {
		t.Fatal(plain)
	}
}
