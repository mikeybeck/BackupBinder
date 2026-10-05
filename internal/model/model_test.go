package model

import (
	"strings"
	"testing"
)

func TestRetentionForgetArgs(t *testing.T) {
	policy := DefaultRetention()
	if !policy.Active() {
		t.Fatal("default policy is inactive")
	}
	args := strings.Join(policy.ForgetArgs(), " ")
	if args != "--keep-last 3 --keep-hourly 24 --keep-daily 14 --keep-weekly 8 --keep-monthly 12 --keep-yearly 2" {
		t.Fatal(args)
	}
	if (Retention{}).Active() || len((Retention{}).ForgetArgs()) != 0 {
		t.Fatal("all-zero policy still forgets snapshots")
	}
	daily := Retention{KeepDaily: 7}.ForgetArgs()
	if len(daily) != 2 || daily[0] != "--keep-daily" || daily[1] != "7" {
		t.Fatalf("%#v", daily)
	}
}

func TestDatabaseSource(t *testing.T) {
	site := Site{Sources: []Source{{Kind: "files"}, {Kind: "database", Name: "app"}}}
	got := DatabaseSource(site)
	if got == nil || got.Name != "app" {
		t.Fatalf("%#v", got)
	}
	if DatabaseSource(Site{}) != nil {
		t.Fatal("files-only site has a database")
	}
}
