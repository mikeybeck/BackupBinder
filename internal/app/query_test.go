package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-backup/internal/fail"
	"local-backup/internal/model"
)

func TestExplainReportsRunState(t *testing.T) {
	application := openTest(t)
	registerNotes(t, application)

	none, err := application.Explain("notes", "")
	if err != nil || none.Message != "No failed run is recorded for this site." {
		t.Fatalf("%#v %v", none, err)
	}
	if err := application.DB.BeginRun("notes", "run-1", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	live, err := application.Explain("notes", "run-1")
	if err != nil || live.Message != "That run is still in progress." {
		t.Fatalf("%#v %v", live, err)
	}
	manifest := &model.Manifest{OK: true, Snapshot: model.Snapshot{RunID: "run-2", ResticSnapshotID: "abc"}}
	if err := application.DB.InsertRun("notes", "run-2", "2026-01-02T00:00:00Z", manifest, nil); err != nil {
		t.Fatal(err)
	}
	ok, err := application.Explain("notes", "run-2")
	if err != nil || ok.Message != "That run succeeded." || ok.Failure != nil {
		t.Fatalf("%#v %v", ok, err)
	}
	destFail := &model.Manifest{
		OK: true, Snapshot: model.Snapshot{RunID: "run-2b", ResticSnapshotID: "abc"},
		Destinations: []model.DestResult{{
			ID: "gdrive-1", Kind: "gdrive", Status: "failed",
			Reason: "Google Drive / rclone remote 'gdrive' is not configured. Run rclone config.",
		}},
	}
	if err := application.DB.InsertRun("notes", "run-2b", "2026-01-02T12:00:00Z", destFail, nil); err != nil {
		t.Fatal(err)
	}
	partial, err := application.Explain("notes", "run-2b")
	if err != nil || !strings.Contains(partial.Message, "destination did not") || !strings.Contains(partial.Message, "rclone config") {
		t.Fatalf("%#v %v", partial, err)
	}
	failure := &fail.Error{Code: "step_failed", Message: "disk full", Step: "archive"}
	if err := application.DB.InsertRun("notes", "run-3", "2026-01-03T00:00:00Z", nil, failure); err != nil {
		t.Fatal(err)
	}
	latest, err := application.Explain("notes", "")
	if err != nil || latest.RunID != "run-3" || latest.Failure == nil || latest.Failure.Step != "archive" {
		t.Fatalf("%#v %v", latest, err)
	}
}

func TestCompareFindsAddedRemovedAndChanged(t *testing.T) {
	application := openTest(t)
	registerNotes(t, application)
	left := manifestWithFiles("left", []model.FileRecord{
		{Path: "a.txt", SHA256: "aaa"},
		{Path: "b.txt", SHA256: "bbb"},
	}, 10, "6.7")
	left.Captured.Files.ByteSize = 100
	rightFiles := []model.FileRecord{{Path: "b.txt", SHA256: "changed"}, {Path: "c.txt", SHA256: "ccc"}}
	right := manifestWithFiles("right", rightFiles, 40, "6.8")
	right.Captured.Files.ByteSize = 130
	if err := application.DB.InsertRun("notes", "left", "2026-01-01T00:00:00Z", left, nil); err != nil {
		t.Fatal(err)
	}
	if err := application.DB.InsertRun("notes", "right", "2026-01-02T00:00:00Z", right, nil); err != nil {
		t.Fatal(err)
	}
	compared, err := application.Compare("notes", "left", "right")
	if err != nil {
		t.Fatal(err)
	}
	if len(compared.Added) != 1 || compared.Added[0] != "c.txt" {
		t.Fatalf("added %#v", compared.Added)
	}
	if len(compared.Removed) != 1 || compared.Removed[0] != "a.txt" {
		t.Fatalf("removed %#v", compared.Removed)
	}
	if len(compared.Changed) != 1 || compared.Changed[0] != "b.txt" {
		t.Fatalf("changed %#v", compared.Changed)
	}
	if compared.Truncated || compared.FileCountDelta != 0 || compared.ByteSizeDelta != 30 {
		t.Fatalf("%#v", compared)
	}
	if compared.DatabaseBytes != [2]int64{10, 40} || compared.WordPress != [2]string{"6.7", "6.8"} {
		t.Fatalf("%#v", compared)
	}

	many := manifestWithFiles("many", nil, 0, "")
	for i := 0; i < 201; i++ {
		many.Captured.Files.Inventory = append(many.Captured.Files.Inventory, model.FileRecord{Path: fileName(i), SHA256: "x"})
	}
	if err := application.DB.InsertRun("notes", "many", "2026-01-03T00:00:00Z", many, nil); err != nil {
		t.Fatal(err)
	}
	truncated, err := application.Compare("notes", "left", "many")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.Truncated || len(truncated.Added) != 200 {
		t.Fatalf("added %d truncated %v", len(truncated.Added), truncated.Truncated)
	}
}

func TestRunScheduledSkipsDisabledAndNotDue(t *testing.T) {
	application := openTest(t)
	registerNotes(t, application)
	other := filepath.Join(application.Home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(other, "Other", "other", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetSchedule("other", "15 3 * * *", true); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 25, 4, 0, 0, 0, time.UTC)
	result, err := application.RunScheduled(when)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ran) != 0 || len(result.Skipped) != 2 {
		t.Fatalf("%#v", result)
	}
	reasons := map[string]string{}
	for _, item := range result.Skipped {
		reasons[item.ID] = item.Reason
	}
	if reasons["notes"] != "disabled" || reasons["other"] != "not_due" {
		t.Fatalf("%#v", reasons)
	}
	listed, err := application.ListBackups("notes")
	if err != nil || len(listed.Backups) != 0 {
		t.Fatalf("%#v %v", listed, err)
	}
}

func registerNotes(t *testing.T, application *App) {
	t.Helper()
	dir := filepath.Join(application.Home, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Register(dir, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
}

func manifestWithFiles(runID string, files []model.FileRecord, dbBytes int64, wordpress string) *model.Manifest {
	return &model.Manifest{
		OK: true,
		Site: model.ManifestSite{Versions: model.Versions{
			WordPress: wordpress, Plugins: []model.Component{}, Themes: []model.Component{},
		}},
		Captured: model.Captured{
			Files:    model.FileCapture{Inventory: files, FileCount: len(files), ByteSize: int64(len(files))},
			Database: &model.DBCapture{Bytes: dbBytes},
		},
		Snapshot: model.Snapshot{RunID: runID, ResticSnapshotID: "snap-" + runID},
	}
}

func fileName(n int) string {
	return "f-" + pad3(n) + ".txt"
}

func pad3(n int) string {
	raw := ""
	for i := 0; i < 3; i++ {
		raw = string(rune('0'+n%10)) + raw
		n /= 10
	}
	return raw
}
