package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSplitHome(t *testing.T) {
	home, rest := splitHome([]string{"--home", "/tmp/var", "list_sites"})
	if home != "/tmp/var" || len(rest) != 1 || rest[0] != "list_sites" {
		t.Fatalf("home %q rest %#v", home, rest)
	}
	home, rest = splitHome([]string{"--home=/tmp/var", "agent"})
	if home != "/tmp/var" || len(rest) != 1 || rest[0] != "agent" {
		t.Fatalf("home %q rest %#v", home, rest)
	}
	home, rest = splitHome([]string{"list_sites", "--home", "/tmp/other"})
	if home != "/tmp/other" || len(rest) != 1 || rest[0] != "list_sites" {
		t.Fatalf("home %q rest %#v", home, rest)
	}
}

func TestCLIJSON(t *testing.T) {
	if code := run("", "nope", nil); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	out := captureStdout(t, func() {
		if code := run("", "discover_site", nil); code != 1 {
			t.Fatalf("exit %d", code)
		}
	})
	var missing struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &missing); err != nil {
		t.Fatal(err, out)
	}
	if missing.OK || missing.Error.Code != "bad_command" || !strings.Contains(missing.Error.Message, "--path") {
		t.Fatal(out)
	}

	home := t.TempDir()
	out = captureStdout(t, func() {
		if code := run(home, "list_sites", nil); code != 0 {
			t.Fatalf("exit %d", code)
		}
	})
	var listed struct {
		OK    bool  `json:"ok"`
		Sites []any `json:"sites"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err, out)
	}
	if !listed.OK || len(listed.Sites) != 0 {
		t.Fatal(out)
	}

	out = captureStdout(t, func() {
		if code := run(home, "serve", []string{"--host", "0.0.0.0"}); code != 1 {
			t.Fatalf("exit %d", code)
		}
	})
	if !strings.Contains(out, "bad_address") || !strings.Contains(out, "localhost") {
		t.Fatal(out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	fn()
	writer.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
