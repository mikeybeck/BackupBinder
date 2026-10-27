package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backupbinder/internal/archive"
	"backupbinder/internal/model"
)

func TestArchiveFileStripsPayload(t *testing.T) {
	payload := filepath.Join("/tmp", "staging", "payload")
	got := archiveFile(filepath.Join(payload, "files", "wp-content", "note.txt"), payload)
	if got != "wp-content/note.txt" {
		t.Fatal(got)
	}
	if archiveFile("files/index.php", payload) != "index.php" {
		t.Fatal(archiveFile("files/index.php", payload))
	}
	if archiveFile("database/database.sql", payload) != "database/database.sql" {
		t.Fatal(archiveFile("database/database.sql", payload))
	}
	if archiveFile("/files/photo-6.bin", payload) != "photo-6.bin" {
		t.Fatal(archiveFile("/files/photo-6.bin", payload))
	}
}

func TestNormalizeProgressFromFiles(t *testing.T) {
	got := normalizeProgress(model.Progress{Phase: "Reading site files", FilesDone: 25, FilesTotal: 100})
	if !got.PercentKnown || got.Percent != 25 {
		t.Fatalf("%#v", got)
	}
}

func TestSavePercent(t *testing.T) {
	ratio, ok := savePercent(50, 200)
	if !ok || ratio != 25 {
		t.Fatal(ratio, ok)
	}
	ratio, ok = savePercent(500, 100)
	if !ok || ratio != 99 {
		t.Fatal(ratio, ok)
	}
	if _, ok := savePercent(0, 100); ok {
		t.Fatal("zero written")
	}
}

func TestSavingArchiveUsesBytesWritten(t *testing.T) {
	application := openTest(t)
	note, err := application.progress("demo", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	defer note.release()
	on := note.onArchive("/tmp/payload")
	on(archiveStatus(1, "", 40*1024*1024, 200*1024*1024))
	got, ok := application.ReadProgress("demo", "run-1")
	if !ok || got.Phase != "Saving the archive" || !got.PercentKnown || got.Percent != 20 || got.BytesWritten != 40*1024*1024 {
		t.Fatalf("%#v %v", got, ok)
	}
	time.Sleep(250 * time.Millisecond)
	on(archiveStatus(0, "", 0, 0))
	got, ok = application.ReadProgress("demo", "run-1")
	if !ok || got.Phase != "Saving the archive" || got.Percent != 20 {
		t.Fatalf("%#v %v", got, ok)
	}
}

func archiveStatus(percent float64, file string, written, total int64) archive.Status {
	st := archive.Status{File: file, WrittenBytes: written}
	if percent > 0 {
		st.HasPercent = true
		st.Percent = percent
	}
	if total > 0 {
		st.HasBytes = true
		st.BytesTotal = total
	}
	return st
}

func TestProgressRoundTrip(t *testing.T) {
	application := openTest(t)
	note, err := application.progress("demo", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	note.set(model.Progress{Phase: "Archiving", File: "note.txt", PercentKnown: true, Percent: 42, FilesDone: 4, FilesTotal: 10}, true)
	got, ok := application.ReadProgress("demo", "run-1")
	if !ok || got.Phase != "Archiving" || got.File != "note.txt" || got.Percent != 42 || got.FilesTotal != 10 {
		t.Fatalf("%#v %v", got, ok)
	}
	note.release()
	if _, err := os.Stat(application.progressPath("demo", "run-1")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestAbandonedRunIsNotStillRunning(t *testing.T) {
	application := openTest(t)
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	started := "2026-09-30T00:00:00Z"
	if err := application.DB.BeginRun("notes", "20260930T000000Z", started); err != nil {
		t.Fatal(err)
	}
	run, err := application.Latest("notes")
	if err != nil {
		t.Fatal(err)
	}
	if run.InProgress() || run.Failure == nil || run.Failure.Code != "interrupted" {
		t.Fatalf("%#v", run.Failure)
	}
	if !strings.Contains(run.Failure.Message, "stopped before it finished") {
		t.Fatal(run.Failure.Message)
	}
	again, err := application.Latest("notes")
	if err != nil || again.InProgress() || again.Failure.Code != "interrupted" {
		t.Fatal(err, again.Failure)
	}

	note, err := application.progress("notes", "20260930T000001Z")
	if err != nil {
		t.Fatal(err)
	}
	defer note.release()
	if err := application.DB.BeginRun("notes", "20260930T000001Z", started); err != nil {
		t.Fatal(err)
	}
	live, err := application.Latest("notes")
	if err != nil || !live.InProgress() || live.RunID != "20260930T000001Z" {
		t.Fatalf("%#v %v", live, err)
	}
}
