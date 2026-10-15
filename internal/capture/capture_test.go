package capture

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStageFilesSkipsJunkSymlinksAndExcludes(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep", "sub")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("hello")
	if err := os.WriteFile(filepath.Join(keep, "note.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logs", "a.log"), []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(keep, "note.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateFiles(root, []string{"logs"})
	if err != nil {
		t.Fatal(err)
	}

	var seen []string
	report, err := StageFiles(root, t.TempDir(), []string{"logs"}, func(rel string, n int) {
		seen = append(seen, rel)
		if n != len(seen) {
			t.Fatalf("count %d len %d", n, len(seen))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.FileCount != 1 || report.ByteSize != int64(len(body)) {
		t.Fatalf("%#v", report)
	}
	if estimate.FileCount != report.FileCount || estimate.ByteSize != report.ByteSize {
		t.Fatalf("estimate %#v stage %#v", estimate, report)
	}
	if len(report.Inventory) != 1 || report.Inventory[0].Path != "keep/sub/note.txt" {
		t.Fatalf("%#v", report.Inventory)
	}
	sum := sha256.Sum256(body)
	if report.Inventory[0].SHA256 != fmt.Sprintf("%x", sum) || report.Inventory[0].Bytes != int64(len(body)) {
		t.Fatalf("%#v", report.Inventory[0])
	}
	if len(seen) != 1 || seen[0] != "keep/sub/note.txt" {
		t.Fatalf("%#v", seen)
	}
	reasons := map[string]string{}
	for _, item := range report.Excluded {
		reasons[item.Path] = item.Reason
	}
	if reasons["node_modules/"] != "node_modules" || reasons[".DS_Store"] != "junk" || reasons["logs/"] != "logs" || reasons["link.txt"] != "symlink" {
		t.Fatalf("%#v", reasons)
	}
	if _, ok := reasons["keep/sub/note.txt"]; ok {
		t.Fatal("kept file was also excluded")
	}
}
