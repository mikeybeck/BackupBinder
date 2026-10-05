package app

import (
	"path/filepath"
	"testing"
)

func TestSetAdapterKeepsScheduleAndServer(t *testing.T) {
	application := openTest(t)
	root := filepath.Join(moduleRoot(t), "fixtures", "wordpress-site")
	if _, err := application.Register(root, "Fixture", "fixture-wordpress", true); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetSchedule("fixture-wordpress", "15 3 * * *", true); err != nil {
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
