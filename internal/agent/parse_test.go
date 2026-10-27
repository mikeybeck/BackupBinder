package agent

import (
	"strings"
	"testing"

	"backupbinder/internal/fail"
)

func TestParseFlags(t *testing.T) {
	args, err := Parse("set_schedule", []string{"--id", "demo", "--root", "/tmp/site", "--cron", "15 3 * * *", "--enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if args["id"] != "demo" || args["root"] != "/tmp/site" || args["cron"] != "15 3 * * *" || args["enabled"] != true {
		t.Fatalf("%#v", args)
	}
	if _, err := Parse("set_schedule", []string{"--nope"}); err == nil {
		t.Fatal("unknown flag was accepted")
	}
	if _, err := Parse("missing", nil); err == nil {
		t.Fatal("unknown command was accepted")
	}
}

func TestExecuteRejectsIncompleteCalls(t *testing.T) {
	for _, name := range []string{"serve", "mcp", "nope"} {
		if _, err := Execute(nil, name, map[string]any{}); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	_, err := Execute(nil, "create_backup", map[string]any{})
	fe, ok := err.(*fail.Error)
	if !ok || fe.Code != "bad_id" {
		t.Fatal(err)
	}
	_, err = Execute(nil, "compare_backups", map[string]any{"id": "demo", "left": "a"})
	fe, ok = err.(*fail.Error)
	if !ok || fe.Code != "bad_command" || !strings.Contains(fe.Message, "--right") {
		t.Fatal(err)
	}
	catalog, err := Execute(nil, "agent", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := catalog.(Catalog)
	if !ok || !doc.OK || len(doc.Commands) == 0 {
		t.Fatalf("%#v", catalog)
	}
}
