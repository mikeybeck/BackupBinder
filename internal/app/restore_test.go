package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupbinder/internal/model"
)

func TestAssertScratchRefusesTheLiveTree(t *testing.T) {
	root := t.TempDir()
	repo := t.TempDir()
	outside := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(root)

	refused := []string{root, parent, child, repo, filepath.Join(repo, "data")}
	home := t.TempDir()
	check := func(path string) error {
		return assertScratch(root, repo, path, home, "site")
	}
	for _, path := range refused {
		if err := check(path); err == nil || !strings.Contains(err.Error(), "Omit --scratch") {
			t.Fatalf("accepted %s: %v", path, err)
		}
	}
	sibling := root + "-other"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })
	if err := check(sibling); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(outside, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := check(file); err == nil {
		t.Fatal("a file was accepted as scratch")
	}
	busy := filepath.Join(outside, "busy")
	if err := os.Mkdir(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := check(busy); err == nil || !strings.Contains(err.Error(), "Omit --scratch") {
		t.Fatal(err)
	}
	empty := filepath.Join(outside, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := check(empty); err != nil {
		t.Fatal(err)
	}
	if err := check(filepath.Join(outside, "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestJunkRestored(t *testing.T) {
	root := t.TempDir()
	if junkRestored(root) {
		t.Fatal("empty tree")
	}
	cache := filepath.Join(root, "wp-content", "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if !junkRestored(root) {
		t.Fatal("cache directory was accepted")
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !junkRestored(root) {
		t.Fatal("node_modules was accepted")
	}
}

func TestFilesMatchRejectsADifferentHash(t *testing.T) {
	scratch := t.TempDir()
	path := filepath.Join(scratch, "files", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := model.Manifest{Captured: model.Captured{Files: model.FileCapture{Inventory: []model.FileRecord{{
		Path: "note.txt", Bytes: 5, SHA256: "not-the-hash",
	}}}}}
	ok, count, err := filesMatch(scratch, manifest)
	if err != nil || ok || count != 0 {
		t.Fatalf("ok=%v count=%d err=%v", ok, count, err)
	}
}

func TestFilesMatchRejectsAnExtraFile(t *testing.T) {
	scratch := t.TempDir()
	path := filepath.Join(scratch, "files", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "files", "extra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.Manifest{Captured: model.Captured{Files: model.FileCapture{Inventory: []model.FileRecord{{
		Path: "note.txt", Bytes: 5, SHA256: sum,
	}}}}}
	ok, _, err := filesMatch(scratch, manifest)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := os.Remove(filepath.Join(scratch, "files", "extra.txt")); err != nil {
		t.Fatal(err)
	}
	ok, count, err := filesMatch(scratch, manifest)
	if err != nil || !ok || count != 1 {
		t.Fatalf("ok=%v count=%d err=%v", ok, count, err)
	}
}

func TestHashLiveIgnoresAFileAlreadyGone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	inventory := []model.FileRecord{{Path: "keep.txt"}, {Path: "gone.txt"}}
	before, err := hashLive(root, inventory)
	if err != nil {
		t.Fatal(err)
	}
	after, err := hashLive(root, inventory)
	if err != nil || !sameHashes(before, after) {
		t.Fatalf("stable missing file: %v %#v %#v", err, before, after)
	}
	if err := os.Remove(filepath.Join(root, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	changed, err := hashLive(root, inventory)
	if err != nil || sameHashes(before, changed) {
		t.Fatalf("a file removed during the check was ignored: %v", err)
	}
}
