package app

import (
	"path/filepath"
	"testing"

	"backupbinder/internal/model"
)

func TestSetAdapterKeepsScheduleAndServer(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetSchedule("fixture-wordpress", "15 3 * * *", boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	files, err := application.SetAdapter("fixture-wordpress", "files")
	if err != nil {
		t.Fatal(err)
	}
	if files.Adapter != "files" || len(files.Sources) != 1 || files.Schedule.Cron != "15 3 * * *" || !files.Schedule.Enabled {
		t.Fatalf("%#v", files)
	}
	wordpress, err := application.SetAdapter("fixture-wordpress", "")
	if err != nil {
		t.Fatal(err)
	}
	if wordpress.Adapter != "wordpress" || len(wordpress.Sources) != 2 || wordpress.Sources[1].Kind != "database" {
		t.Fatalf("%#v", wordpress.Sources)
	}
	if wordpress.Schedule.Cron != "15 3 * * *" || !wordpress.Schedule.Enabled {
		t.Fatalf("%#v", wordpress.Schedule)
	}
	if _, err := application.SetAdapter("fixture-wordpress", "nope"); err == nil {
		t.Fatal("unknown adapter was accepted")
	}
}

func TestAdoptCopiedSite(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	plain := site.Sources
	for i := range plain {
		if plain[i].Kind == "database" {
			plain = append(plain[:i], plain[i+1:]...)
			break
		}
	}
	if err := application.DB.SaveAdapter(site.ID, "files", plain); err != nil {
		t.Fatal(err)
	}
	site, err = application.DB.GetSite("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	detected, err := application.adoptCopiedSite(site)
	if err != nil {
		t.Fatal(err)
	}
	if detected.Adapter != "wordpress" || model.DatabaseSource(detected) == nil {
		t.Fatalf("%s %#v", detected.Adapter, detected.Sources)
	}
	declined, err := application.SetDatabase("fixture-wordpress", DatabaseInput{Enabled: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.DB.SaveAdapter(declined.ID, "files", declined.Sources); err != nil {
		t.Fatal(err)
	}
	declined, err = application.DB.GetSite("fixture-wordpress")
	if err != nil {
		t.Fatal(err)
	}
	again, err := application.adoptCopiedSite(declined)
	if err != nil {
		t.Fatal(err)
	}
	if again.Adapter != "wordpress" || model.DatabaseSource(again) != nil {
		t.Fatalf("declined database %#v", again.Sources)
	}
}

func TestAdoptCopiedSiteKeepsManualDatabase(t *testing.T) {
	application := openTest(t)
	root := t.TempDir()
	if _, err := application.Register(root, "Notes", "notes", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetDatabase("notes", DatabaseInput{
		Enabled: boolPtr(true), Name: "notes", User: "notes", Host: "127.0.0.1", Password: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	site, err := application.DB.GetSite("notes")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := application.adoptCopiedSite(site)
	if err != nil {
		t.Fatal(err)
	}
	db := model.DatabaseSource(kept)
	if kept.Adapter != "files" || db == nil || db.Name != "notes" || db.User != "notes" || db.Host != "127.0.0.1" {
		t.Fatalf("%s %#v", kept.Adapter, kept.Sources)
	}
	if db.PasswordSource != "saved" {
		t.Fatalf("password source %#v", db)
	}
}
