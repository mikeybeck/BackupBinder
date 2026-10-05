package app

import (
	"os"
	"path/filepath"
	"testing"

	"local-backup/internal/model"
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
	for _, path := range refused {
		if err := assertScratch(root, repo, path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	sibling := root + "-other"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })
	if err := assertScratch(root, repo, sibling); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(outside, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := assertScratch(root, repo, file); err == nil {
		t.Fatal("a file was accepted as scratch")
	}
	busy := filepath.Join(outside, "busy")
	if err := os.Mkdir(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := assertScratch(root, repo, busy); err == nil {
		t.Fatal("a non-empty directory was accepted")
	}
	empty := filepath.Join(outside, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := assertScratch(root, repo, empty); err != nil {
		t.Fatal(err)
	}
	if err := assertScratch(root, repo, filepath.Join(outside, "missing")); err != nil {
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
	manifest := model.Manifest{Captured: model.Captured{Files: model.FileCapture{Inventory: []model.FileRecord{{
		Path: "note.txt", Bytes: 5, SHA256: "not-the-hash",
	}}}}}
	ok, count, err := filesMatch(scratch, manifest)
	if err != nil || ok || count != 0 {
		t.Fatalf("ok=%v count=%d err=%v", ok, count, err)
	}
}
