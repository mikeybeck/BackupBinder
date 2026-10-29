package capture

import (
	"strings"
	"testing"

	"backupbinder/internal/addon"
	"backupbinder/internal/model"
)

func TestRemoteMySQLCommandOmitsPassword(t *testing.T) {
	command, err := remoteMySQLCommand("/tmp/backupbinder-site-ab.cnf", "/tmp/backupbinder-site-ab.sql", "wpfoundry")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(command, "secret") || strings.Contains(command, "password=") {
		t.Fatal(command)
	}
	if !strings.Contains(command, "mysqldump") || !strings.Contains(command, "'wpfoundry'") || !strings.Contains(command, "rm -f") {
		t.Fatal(command)
	}
	if !strings.Contains(command, "--defaults-file=") || strings.Contains(command, "--defaults-extra-file=") {
		t.Fatal(command)
	}
	if !strings.Contains(command, `trap "rm -f '/tmp/backupbinder-site-ab.cnf'" EXIT`) {
		t.Fatal(command)
	}
	defaults := mysqlDefaults(addon.Connection{User: "wpfoundryuser", Host: "localhost"}, "secret-value")
	if !strings.Contains(defaults, "password=secret-value") || !strings.Contains(defaults, "host=localhost") {
		t.Fatal(defaults)
	}
	if strings.Contains(command, "secret-value") {
		t.Fatal(command)
	}
	stream, err := remoteMySQLStreamCommand("/tmp/backupbinder-site-ab.cnf", "wpfoundry")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stream, "result-file") || strings.Contains(stream, "password=") {
		t.Fatal(stream)
	}
	if !strings.Contains(stream, "mysqldump") || !strings.Contains(stream, "'wpfoundry'") {
		t.Fatal(stream)
	}
	if !strings.Contains(stream, `trap "rm -f '/tmp/backupbinder-site-ab.cnf'" EXIT`) {
		t.Fatal(stream)
	}
}

func TestJoinRemoteQuotesTheServerPath(t *testing.T) {
	command, err := joinRemote([]string{"wp", "db", "export", "/tmp/backupbinder-site.sql", "--path=/var/www/wpfoundry.app"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "'wp' 'db' 'export' '/tmp/backupbinder-site.sql' '--path=/var/www/wpfoundry.app'" {
		t.Fatal(command)
	}
	if _, err := quoteShell("line\nbreak"); err == nil {
		t.Fatal("newline was accepted")
	}
}

func TestValidServerRejectsAShellHost(t *testing.T) {
	err := validServer(model.Site{Location: model.Location{Kind: "sftp", Host: "files.example;rm", User: "backup", Path: "/var/www/site"}})
	if err == nil {
		t.Fatal("shell host was accepted")
	}
}
