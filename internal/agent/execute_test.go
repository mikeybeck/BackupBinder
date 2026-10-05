package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/app"
)

func TestExecuteReadsAgentArgumentTypes(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
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
	site, err := application.DB.GetSite("notes")
	if err != nil {
		t.Fatal(err)
	}
	scheduled, err := Execute(application, "set_schedule", map[string]any{
		"id": "notes", "root": site.Root, "cron": "15 3 * * *", "enabled": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(scheduled)
	if !strings.Contains(string(raw), `"enabled":true`) {
		t.Fatal(string(raw))
	}
	limited, err := Execute(application, "set_limits", map[string]any{
		"id": "notes", "root": site.Root, "limit-upload": float64(256), "limit-download": "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(limited)
	if !strings.Contains(string(raw), `"upload_kib":256`) || !strings.Contains(string(raw), `"download_kib":0`) {
		t.Fatal(string(raw))
	}
	if _, err := Execute(application, "create_backup", map[string]any{"id": "notes", "root": site.Root + "-other"}); err == nil {
		t.Fatal("a different root was accepted")
	}
}
