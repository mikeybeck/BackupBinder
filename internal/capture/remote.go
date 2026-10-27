package capture

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"backupbinder/internal/addon"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

var (
	remoteHost = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	remoteUser = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	remoteDir  = regexp.MustCompile(`^/[A-Za-z0-9._@/=+-]*$`)
	remoteID   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func databaseOnServer(site model.Site, matched addon.Addon, output, secretDir string) (model.DBCapture, []string, error) {
	if err := validServer(site); err != nil {
		return model.DBCapture{}, nil, err
	}
	source := model.DatabaseSource(site)
	methods := []string{source.Method}
	auto := source.Method == "auto" || source.Method == ""
	if auto {
		if matched.Database != nil && len(matched.Database.Auto) > 0 {
			methods = matched.Database.Auto
		} else if source != nil && source.Name != "" && source.User != "" {
			methods = []string{"mysqldump"}
		} else {
			method := ""
			if source != nil {
				method = source.Method
			}
			return model.DBCapture{}, nil, fail.New("step_failed", "Unknown database method "+method+".")
		}
	}
	var secrets []string
	var first error
	for _, name := range methods {
		spec, ok := matched.Dump(name)
		if !ok {
			if name != "mysqldump" || source == nil || source.Name == "" || source.User == "" {
				return model.DBCapture{}, secrets, fail.New("step_failed", "Unknown database method "+name+".")
			}
			spec = addon.Dump{Engine: "mysqldump", TimeoutSec: 7200, FailMessage: "mysqldump failed.", Report: "mysqldump"}
		}
		report, found, runErr := runDumpRemote(matched, name, spec, site, output, secretDir, secrets)
		secrets = found
		if runErr == nil {
			return report, secrets, nil
		}
		if first == nil {
			first = runErr
			continue
		}
		return model.DBCapture{}, secrets, combineDump(first, runErr)
	}
	if first != nil {
		return model.DBCapture{}, secrets, first
	}
	return model.DBCapture{}, secrets, fail.New("step_failed", "Unknown database method "+source.Method+".")
}

func runDumpRemote(matched addon.Addon, name string, spec addon.Dump, site model.Site, output, secretDir string, secrets []string) (model.DBCapture, []string, error) {
	switch spec.Engine {
	case "argv":
		report, err := exportArgvRemote(matched, name, spec, site, output, secrets)
		return report, secrets, err
	case "mysqldump":
		return exportMySQLRemote(matched, spec, site, output, secretDir, secrets)
	default:
		return model.DBCapture{}, secrets, fail.New("step_failed", "Unknown database method "+name+".")
	}
}

func exportArgvRemote(matched addon.Addon, name string, spec addon.Dump, site model.Site, output string, secrets []string) (model.DBCapture, error) {
	if len(spec.RemoteArgv) == 0 {
		return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: "The " + name + " dump has no remote command.", Step: "database_dump", Hint: "Add remote_argv to the addon. The dump runs on the server."}
	}
	database := ""
	if conn, err := resolveConnection(site, matched); err == nil {
		database = conn.Name
	}
	remoteSQL, err := remoteTemp(site.ID, ".sql")
	if err != nil {
		return model.DBCapture{}, err
	}
	args := make([]string, 0, len(spec.RemoteArgv))
	for _, token := range spec.RemoteArgv {
		expanded, missing := expandArg(token, map[string]string{
			"{php}": "php", "{wp}": "wp", "{output}": remoteSQL, "{root}": site.Location.Path, "{database}": database,
		})
		if missing != "" {
			return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: missing + " is not available on the server.", Step: "database_dump"}
		}
		args = append(args, expanded)
	}
	command, err := joinRemote(args)
	if err != nil {
		return model.DBCapture{}, err
	}
	timeout := dumpTimeout(spec, 3*time.Minute)
	defer removeRemote(site.Location, remoteSQL)
	text, runErr := runSSH(site.Location, command, "", timeout)
	if runErr != nil || !copiedDump(site.Location, remoteSQL, output, timeout) {
		return model.DBCapture{}, remoteFail(name, spec, site, recordedArgs(args, remoteSQL), text, secrets)
	}
	reportName := spec.Report
	if reportName == "" {
		reportName = name
	}
	return DumpReport(output, reportName)
}

func exportMySQLRemote(matched addon.Addon, spec addon.Dump, site model.Site, output, secretDir string, secrets []string) (model.DBCapture, []string, error) {
	conn, err := resolveConnection(site, matched)
	if err != nil {
		message := "The database config could not be read."
		if matched.Database != nil && matched.Database.Config.Unreadable != "" {
			message = matched.Database.Config.Unreadable
		}
		return model.DBCapture{}, secrets, &fail.Error{Code: "step_failed", Message: message, Step: "database_dump"}
	}
	password, err := resolvePassword(site, matched, secretDir)
	if err != nil {
		return model.DBCapture{}, secrets, err
	}
	if strings.Contains(password, "\n") || strings.Contains(conn.User, "\n") || strings.Contains(conn.Name, "\n") {
		return model.DBCapture{}, secrets, &fail.Error{Code: "step_failed", Message: "The database password contains a newline and cannot be passed safely.", Step: "database_dump"}
	}
	secrets = append(secrets, password)
	remoteCNF, err := remoteTemp(site.ID, ".cnf")
	if err != nil {
		return model.DBCapture{}, secrets, err
	}
	command, err := remoteMySQLStreamCommand(remoteCNF, conn.Name)
	if err != nil {
		return model.DBCapture{}, secrets, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return model.DBCapture{}, secrets, err
	}
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return model.DBCapture{}, secrets, err
	}
	timeout := dumpTimeout(spec, 2*time.Hour)
	defer removeRemote(site.Location, remoteCNF)
	text, runErr := runSSHStdout(site.Location, command, mysqlDefaults(conn, password), output, timeout)
	if runErr != nil || !nonEmpty(output) {
		_ = os.Remove(output)
		message := spec.FailMessage
		if message == "" {
			message = "mysqldump failed."
		}
		return model.DBCapture{}, secrets, &fail.Error{
			Code: "step_failed", Message: message, Step: "database_dump",
			Command: []string{"ssh", site.Location.User + "@" + site.Location.Host, "mysqldump", "--defaults-extra-file", "--single-transaction", "--no-tablespaces", conn.Name},
			Stderr:  clip(scrub(text, secrets)), Hint: remoteHint(site, spec.Hint),
		}
	}
	reportName := spec.Report
	if reportName == "" {
		reportName = "mysqldump"
	}
	report, err := DumpReport(output, reportName)
	return report, secrets, err
}

func remoteMySQLCommand(cnf, sql, name string) (string, error) {
	cnfQ, err := quoteShell(cnf)
	if err != nil {
		return "", err
	}
	sqlQ, err := quoteShell(sql)
	if err != nil {
		return "", err
	}
	nameQ, err := quoteShell(name)
	if err != nil {
		return "", err
	}
	return remoteMySQLPrefix(cnfQ) + "mysqldump --defaults-extra-file=" + cnfQ + " --single-transaction --skip-lock-tables --no-tablespaces --result-file=" + sqlQ + " " + nameQ + "; status=$?; rm -f " + cnfQ + "; exit $status", nil
}

// remoteMySQLStreamCommand writes the dump to stdout so SSH can stream it locally.
func remoteMySQLStreamCommand(cnf, name string) (string, error) {
	cnfQ, err := quoteShell(cnf)
	if err != nil {
		return "", err
	}
	nameQ, err := quoteShell(name)
	if err != nil {
		return "", err
	}
	return remoteMySQLPrefix(cnfQ) + "mysqldump --defaults-extra-file=" + cnfQ + " --single-transaction --skip-lock-tables --no-tablespaces " + nameQ + "; status=$?; rm -f " + cnfQ + "; exit $status", nil
}

// remoteMySQLPrefix writes the defaults file and removes it when the remote shell exits,
// including when SSH dies and the shell receives SIGHUP.
func remoteMySQLPrefix(cnfQ string) string {
	return "umask 077; trap \"rm -f " + cnfQ + "\" EXIT; cat > " + cnfQ + " && "
}

func mysqlDefaults(conn addon.Connection, password string) string {
	lines := []string{"[client]", "user=" + conn.User, "password=" + password}
	if conn.Socket != "" {
		lines = append(lines, "socket="+conn.Socket)
	} else {
		host := conn.Host
		if host == "" {
			host = "127.0.0.1"
		}
		lines = append(lines, "host="+host)
		if conn.Port != 0 {
			lines = append(lines, "port="+fmt.Sprintf("%d", conn.Port))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func copiedDump(loc model.Location, remotePath, localPath string, timeout time.Duration) bool {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return false
	}
	if _, err := exec.LookPath("scp"); err != nil {
		return false
	}
	args := append(sshBase(), loc.User+"@"+loc.Host+":"+remotePath, localPath)
	cmd := exec.Command("scp", args...)
	ApplySSHAgent(cmd)
	cmd.WaitDelay = 2 * time.Second
	timer := time.AfterFunc(timeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	if err := cmd.Run(); err != nil {
		return false
	}
	return nonEmpty(localPath)
}

func removeRemote(loc model.Location, paths ...string) {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		item, err := quoteShell(path)
		if err != nil {
			continue
		}
		quoted = append(quoted, item)
	}
	if len(quoted) == 0 {
		return
	}
	_, _ = runSSH(loc, "rm -f "+strings.Join(quoted, " "), "", 30*time.Second)
}

func runSSH(loc model.Location, remoteCmd, stdin string, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return "", fail.New("step_failed", "ssh is not installed.")
	}
	args := append(sshBase(), loc.User+"@"+loc.Host, remoteCmd)
	cmd := exec.Command("ssh", args...)
	ApplySSHAgent(cmd)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.WaitDelay = 2 * time.Second
	timer := time.AfterFunc(timeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runSSHStdout streams remote stdout into a local file and returns scrubbed stderr.
func runSSHStdout(loc model.Location, remoteCmd, stdin, output string, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return "", fail.New("step_failed", "ssh is not installed.")
	}
	outFile, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer outFile.Close()
	args := append(sshBase(), loc.User+"@"+loc.Host, remoteCmd)
	cmd := exec.Command("ssh", args...)
	ApplySSHAgent(cmd)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = outFile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	timer := time.AfterFunc(timeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	runErr := cmd.Run()
	return stderr.String(), runErr
}

func remoteFail(name string, spec addon.Dump, site model.Site, command []string, stderr string, secrets []string) error {
	message := spec.FailMessage
	if message == "" {
		message = name + " failed."
	}
	return &fail.Error{
		Code: "step_failed", Message: message, Step: "database_dump",
		Command: append([]string{"ssh", site.Location.User + "@" + site.Location.Host}, command...),
		Stderr:  clip(scrub(stderr, secrets)), Hint: remoteHint(site, spec.Hint),
	}
}

func remoteHint(site model.Site, hint string) string {
	where := "The dump ran on " + site.Location.User + "@" + site.Location.Host + "."
	if hint == "" {
		return where
	}
	return where + " " + hint
}

func joinRemote(args []string) (string, error) {
	quoted := make([]string, len(args))
	for i, arg := range args {
		item, err := quoteShell(arg)
		if err != nil {
			return "", err
		}
		quoted[i] = item
	}
	return strings.Join(quoted, " "), nil
}

func quoteShell(s string) (string, error) {
	if strings.ContainsAny(s, "\n\r") || strings.ContainsRune(s, 0) {
		return "", &fail.Error{Code: "step_failed", Message: "A remote dump argument contains a newline.", Step: "database_dump"}
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

func remoteTemp(siteID, suffix string) (string, error) {
	if !remoteID.MatchString(siteID) {
		return "", fail.New("bad_id", "Site id must be lowercase letters, numbers, and hyphens.")
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "/tmp/backupbinder-" + siteID + "-" + hex.EncodeToString(buf) + suffix, nil
}

func validServer(site model.Site) error {
	loc := site.Location
	if !remoteHost.MatchString(loc.Host) || !remoteUser.MatchString(loc.User) || !remoteDir.MatchString(loc.Path) {
		return &fail.Error{Code: "bad_location", Message: "The server host, user, or path cannot be used for a remote dump.", Step: "database_dump"}
	}
	return nil
}

func dumpTimeout(spec addon.Dump, fallback time.Duration) time.Duration {
	if spec.TimeoutSec > 0 {
		return time.Duration(spec.TimeoutSec) * time.Second
	}
	return fallback
}

func sshBase() []string {
	return []string{"-o", "BatchMode=yes", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=4"}
}

func clip(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 4000 {
		return text[len(text)-4000:]
	}
	return text
}
