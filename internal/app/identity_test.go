package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentifyDoesNotSubstituteASimilarSite(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "wpfoundry.app", "wpfoundry-app", true); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(moduleRoot(t), "fixtures", "unreachable-db")
	if _, err := application.Register(other, "wpfoundry-local", "wpfoundry-local", true); err != nil {
		t.Fatal(err)
	}
	found, err := application.Identify("wpfoundry.app")
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Matches) != 1 || found.Matches[0].ID != "wpfoundry-app" {
		t.Fatalf("%#v", found.Matches)
	}
	if found.Matches[0].Location != "local_directory" || !strings.Contains(found.Warning, "remote") {
		t.Fatalf("%#v", found)
	}
	if !found.Access.AskUser {
		t.Fatal("expected the agent to ask the user for access")
	}
	var sftp []string
	for _, option := range found.Access.Options {
		if option.Kind == "sftp" {
			sftp = option.Fields
		}
	}
	if strings.Join(sftp, ",") != "host,user,remote-path" {
		t.Fatalf("sftp fields %#v", sftp)
	}
	if found.Matches[0].Root != root && !strings.HasSuffix(found.Matches[0].Root, "fixtures/wordpress-site") {
		t.Fatal(found.Matches[0].Root)
	}
	missing, err := application.Identify("https://wpfoundry.app")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing.Matches) != 0 || !strings.Contains(missing.Warning, "Do not back up") {
		t.Fatalf("%#v", missing)
	}
}

func TestConfirmSiteRejectsADifferentRoot(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.ConfirmSite("fixture-wordpress", ""); err == nil {
		t.Fatal("missing root was accepted")
	}
	if _, err := application.ConfirmSite("fixture-wordpress", site.Root+"/other"); err == nil {
		t.Fatal("different root was accepted")
	}
	got, err := application.ConfirmSite("fixture-wordpress", site.Root)
	if err != nil || got.ID != "fixture-wordpress" {
		t.Fatal(err)
	}
}
