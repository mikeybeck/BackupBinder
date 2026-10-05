package archive

import (
	"strings"
	"testing"

	"local-backup/internal/model"
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
