package capture

import (
	"strings"
	"testing"

	"local-backup/internal/addon"
	"local-backup/internal/model"
)

func TestRemoteMySQLCommandOmitsPassword(t *testing.T) {
	command, err := remoteMySQLCommand("/tmp/local-backup-site-ab.cnf", "/tmp/local-backup-site-ab.sql", "wpfoundry")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(command, "secret") || strings.Contains(command, "password=") {
		t.Fatal(command)
	}
	if !strings.Contains(command, "mysqldump") || !strings.Contains(command, "'wpfoundry'") || !strings.Contains(command, "rm -f") {
		t.Fatal(command)
	}
	defaults := mysqlDefaults(addon.Connection{User: "wpfoundryuser", Host: "localhost"}, "secret-value")
	if !strings.Contains(defaults, "password=secret-value") || !strings.Contains(defaults, "host=localhost") {
		t.Fatal(defaults)
	}
	if strings.Contains(command, "secret-value") {
		t.Fatal(command)
	}
}

func TestJoinRemoteQuotesTheServerPath(t *testing.T) {
	command, err := joinRemote([]string{"wp", "db", "export", "/tmp/local-backup-site.sql", "--path=/var/www/wpfoundry.app"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "'wp' 'db' 'export' '/tmp/local-backup-site.sql' '--path=/var/www/wpfoundry.app'" {
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
