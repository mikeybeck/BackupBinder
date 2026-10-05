package app

import (
	"path/filepath"
	"strings"
	"testing"

	"local-backup/internal/model"
)

func TestSetLocationKeepsOneServer(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := application.SetLocation(site.ID, model.Location{Kind: "sftp", Host: "files.example", User: "backup", Path: "/var/www/site"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Location.Kind != "sftp" || saved.Location.Host != "files.example" || saved.Root != site.Root {
		t.Fatalf("%#v", saved.Location)
	}
	replaced, err := application.SetLocation(site.ID, model.Location{Kind: "sftp", Host: "other.example", User: "backup", Path: "/var/www/other"})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Location.Host != "other.example" || replaced.Location.Path != "/var/www/other" {
		t.Fatalf("%#v", replaced.Location)
	}
	if _, err := application.SetLocation(site.ID, model.Location{Kind: "sftp", Host: "files.example", User: "backup"}); err == nil {
		t.Fatal("incomplete server was accepted")
	}
	local, err := application.SetLocation(site.ID, model.Location{Kind: "local", Path: root})
	if err != nil || local.Location.Kind != "local" {
		t.Fatal(err, local.Location)
	}
}

func TestRsyncArgsCopyOneServer(t *testing.T) {
	args := strings.Join(rsyncArgs(model.Location{Host: "files.example", User: "backup", Path: "/var/www/site"}, "/tmp/copy"), " ")
	if !strings.Contains(args, "backup@files.example:/var/www/site/") || !strings.Contains(args, "/tmp/copy/") {
		t.Fatal(args)
	}
	if !strings.Contains(args, "--partial") || !strings.Contains(args, "ServerAliveInterval=30") {
		t.Fatal(args)
	}
	if strings.Contains(args, "password") {
		t.Fatal(args)
	}
}

func TestRetryableCopySkipsAuthFailures(t *testing.T) {
	if !retryableCopy("Read from remote host: Connection timed out\nBroken pipe") {
		t.Fatal("timeout should be tried again")
	}
	if retryableCopy("Permission denied (publickey)") {
		t.Fatal("auth failure")
	}
}

func TestIdentifyUsesSavedServer(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "wpfoundry.app", "wpfoundry-app", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetLocation("wpfoundry-app", model.Location{Kind: "sftp", Host: "139.162.54.72", User: "mikey", Path: "/var/www/wpfoundry.app"}); err != nil {
		t.Fatal(err)
	}
	found, err := application.Identify("wpfoundry.app")
	if err != nil {
		t.Fatal(err)
	}
	if found.Access.AskUser || found.Warning != "" || found.Matches[0].Location != "sftp" {
		t.Fatalf("%#v", found)
	}
	if !strings.Contains(found.Matches[0].Note, "mikey@139.162.54.72:/var/www/wpfoundry.app") {
		t.Fatal(found.Matches[0].Note)
	}
}
