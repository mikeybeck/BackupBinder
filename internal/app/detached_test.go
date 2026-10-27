package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartBackupDetachedRefusesWhenBusy(t *testing.T) {
	application := openTest(t)
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
	note, err := application.progress("notes", "20261019T000000Z")
	if err != nil {
		t.Fatal(err)
	}
	defer note.release()
	if err := application.DB.BeginRun("notes", "20261019T000000Z", "2026-10-19T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.StartBackupDetached("notes"); err == nil {
		t.Fatal("expected busy")
	}
}
