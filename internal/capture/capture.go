// Package capture copies site files and writes a database dump into a staging tree.
package capture

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"local-backup/internal/addon"
	"local-backup/internal/discover"
	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/sqlcheck"
)

// EstimateFiles walks the site root with the same skip and exclude rules as StageFiles,
// but only counts files and bytes. It does not hard-link, copy, or hash.
func EstimateFiles(root string, excludes []string) (model.FileCapture, error) {
	report := model.FileCapture{Root: root, Excluded: []model.Excluded{}, Inventory: []model.FileRecord{}}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		reason := discover.SkipReason(rel)
		if reason == "" {
			reason = discover.ExcludeReason(rel, excludes)
		}
		if reason != "" {
			report.ExcludedCount++
			if len(report.Excluded) < 50 {
				item := rel
				if d.IsDir() {
					item += "/"
				}
				report.Excluded = append(report.Excluded, model.Excluded{Path: item, Reason: reason})
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			report.ExcludedCount++
			if len(report.Excluded) < 50 {
				report.Excluded = append(report.Excluded, model.Excluded{Path: rel, Reason: "symlink"})
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		report.Inventory = append(report.Inventory, model.FileRecord{Path: rel, Bytes: info.Size()})
		return nil
	})
	if err != nil {
		return report, err
	}
	report.FileCount = len(report.Inventory)
	for _, item := range report.Inventory {
		report.ByteSize += item.Bytes
	}
	return report, nil
}

// StageFiles hard-links (or copies) the files that belong in the archive.
// onFile, when set, is called with each kept relative path and how many files are kept so far.
func StageFiles(root, destination string, excludes []string, onFile func(rel string, n int)) (model.FileCapture, error) {
	report := model.FileCapture{Root: root, Excluded: []model.Excluded{}, Inventory: []model.FileRecord{}}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return report, err
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		reason := discover.SkipReason(rel)
		if reason == "" {
			reason = discover.ExcludeReason(rel, excludes)
		}
		if reason != "" {
			report.ExcludedCount++
			if len(report.Excluded) < 50 {
				item := rel
				if d.IsDir() {
					item += "/"
				}
				report.Excluded = append(report.Excluded, model.Excluded{Path: item, Reason: reason})
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			report.ExcludedCount++
			if len(report.Excluded) < 50 {
				report.Excluded = append(report.Excluded, model.Excluded{Path: rel, Reason: "symlink"})
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Link(path, target); err != nil {
			if copyErr := copyFile(path, target); copyErr != nil {
				return copyErr
			}
		}
		sum, err := hashFile(target)
		if err != nil {
			return err
		}
		report.Inventory = append(report.Inventory, model.FileRecord{Path: rel, Bytes: info.Size(), SHA256: sum})
		if onFile != nil {
			onFile(rel, len(report.Inventory))
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	sortInventory(report.Inventory)
	report.FileCount = len(report.Inventory)
	for _, item := range report.Inventory {
		report.ByteSize += item.Bytes
	}
	return report, nil
}

func sortInventory(items []model.FileRecord) {
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j].Path < items[j-1].Path {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Database writes database/database.sql using the site addon's dump methods.
func Database(site model.Site, output, secretDir string) (model.DBCapture, []string, error) {
	source := model.DatabaseSource(site)
	if source == nil {
		return model.DBCapture{}, nil, fail.New("step_failed", "This site has no database source.")
	}
	if source.Method == "sql_file" {
		report, err := copySQL(site.Root, source.SQLFile, output)
		return report, nil, err
	}
	matched, err := addon.ByID(site.Adapter)
	if err != nil {
		if source.Name == "" || source.User == "" {
			return model.DBCapture{}, nil, err
		}
		matched = addon.Addon{}
	}
	if site.Location.Kind == "sftp" {
		if matched.ID == "" {
			return model.DBCapture{}, nil, fail.New("step_failed", "A server database dump needs a matching site addon.")
		}
		return databaseOnServer(site, matched, output, secretDir)
	}
	methods := []string{source.Method}
	auto := source.Method == "auto" || source.Method == ""
	if auto {
		if matched.Database != nil && len(matched.Database.Auto) > 0 {
			methods = matched.Database.Auto
		} else if source.Name != "" && source.User != "" {
			methods = []string{"mysqldump"}
		} else {
			return model.DBCapture{}, nil, fail.New("step_failed", "Unknown database method "+source.Method+".")
		}
	}
	var secrets []string
	var first error
	for _, name := range methods {
		spec, ok := matched.Dump(name)
		if !ok {
			if name != "mysqldump" || source.Name == "" || source.User == "" {
				return model.DBCapture{}, secrets, fail.New("step_failed", "Unknown database method "+name+".")
			}
			spec = addon.Dump{Engine: "mysqldump", TimeoutSec: 600, FailMessage: "mysqldump failed.", Report: "mysqldump"}
		}
		if auto && spec.SkipIfUnreachable && !reachable(site, matched) {
			continue
		}
		report, found, runErr := runDump(matched, name, spec, site, output, secretDir, secrets)
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

func combineDump(first, second error) error {
	fe, ok := second.(*fail.Error)
	if !ok || fe == nil {
		fe = fail.New("step_failed", second.Error())
	}
	fe.Message = "Database capture failed. " + first.Error() + " " + fe.Message
	if earlier, ok := first.(*fail.Error); ok && earlier.Stderr != "" {
		fe.Stderr = strings.TrimSpace(earlier.Stderr + "\n" + fe.Stderr)
	}
	return fe
}

func runDump(matched addon.Addon, name string, spec addon.Dump, site model.Site, output, secretDir string, secrets []string) (model.DBCapture, []string, error) {
	switch spec.Engine {
	case "argv":
		report, err := exportArgv(matched, name, spec, site, output, secrets)
		return report, secrets, err
	case "mysqldump":
		return exportMySQL(matched, spec, site, output, secretDir, secrets)
	default:
		return model.DBCapture{}, secrets, fail.New("step_failed", "Unknown database method "+name+".")
	}
}

// resolveConnection prefers fields saved on the site source, then the addon config.
func resolveConnection(site model.Site, matched addon.Addon) (addon.Connection, error) {
	source := model.DatabaseSource(site)
	if source != nil && source.Name != "" && source.User != "" {
		host, port, socket := splitHostLabel(source.Host)
		if host == "" {
			host = "localhost"
		}
		return addon.Connection{
			Name:           source.Name,
			User:           source.User,
			Host:           host,
			Port:           port,
			Socket:         socket,
			TablePrefix:    source.TablePrefix,
			PasswordSource: source.PasswordSource,
		}, nil
	}
	return matched.ReadConnection(site.Root)
}

func resolvePassword(site model.Site, matched addon.Addon, secretDir string) (string, error) {
	source := model.DatabaseSource(site)
	if source != nil && source.PasswordSource == "saved" {
		password, err := readEnvValue(filepath.Join(secretDir, site.ID+"-database.env"), "DB_PASSWORD")
		if err != nil || password == "" {
			return "", &fail.Error{
				Code: "step_failed", Message: "Saved database credentials are missing. Enter the password on the site page.",
				Step: "database_dump",
			}
		}
		return password, nil
	}
	return matched.ReadPassword(site.Root)
}

func splitHostLabel(label string) (host string, port int, socket string) {
	host = strings.TrimSpace(label)
	if host == "" {
		return "", 0, ""
	}
	if i := strings.Index(host, ":"); i >= 0 {
		left, right := host[:i], host[i+1:]
		if strings.HasPrefix(right, "/") {
			host = left
			socket = right
			return host, 0, socket
		}
		if n, err := strconv.Atoi(right); err == nil {
			return left, n, ""
		}
	}
	return host, 0, ""
}

func readEnvValue(path, key string) (string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	prefix := key + "="
	for _, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), nil
		}
	}
	return "", nil
}

func reachable(site model.Site, matched addon.Addon) bool {
	conn, err := resolveConnection(site, matched)
	if err != nil {
		return false
	}
	if conn.Socket != "" {
		info, err := os.Stat(conn.Socket)
		return err == nil && !info.IsDir()
	}
	host := conn.Host
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	port := conn.Port
	if port == 0 {
		port = 3306
		if matched.Database != nil && matched.Database.DefaultPort != 0 {
			port = matched.Database.DefaultPort
		}
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	c, err := dialer.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func exportArgv(matched addon.Addon, name string, spec addon.Dump, site model.Site, output string, secrets []string) (model.DBCapture, error) {
	php := discover.FindPHP()
	wp, wpErr := exec.LookPath("wp")
	wpPath := ""
	if wpErr == nil {
		wpPath = wp
	}
	database := ""
	if conn, err := resolveConnection(site, matched); err == nil {
		database = conn.Name
	}
	root := site.Root
	args := make([]string, 0, len(spec.Argv))
	for _, token := range spec.Argv {
		expanded, missing := expandArg(token, map[string]string{
			"{php}": php, "{wp}": wpPath, "{output}": output, "{root}": root, "{database}": database,
		})
		if missing != "" {
			tool := spec.Missing[missing]
			message := tool.Message
			if message == "" {
				message = missing + " is not available."
			}
			return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: message, Step: "database_dump", Hint: tool.Hint}
		}
		args = append(args, expanded)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return model.DBCapture{}, err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.WaitDelay = 2 * time.Second
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	timer := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	if err != nil || !nonEmpty(output) {
		text := scrub(string(out), secrets)
		if len(text) > 4000 {
			text = text[len(text)-4000:]
		}
		code := 1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		message := spec.FailMessage
		if message == "" {
			message = name + " failed."
		}
		return model.DBCapture{}, &fail.Error{
			Code: "step_failed", Message: message, Step: "database_dump",
			Command: recordedArgs(args, output), ExitCode: fail.ExitCode(code), Stderr: text, Hint: spec.Hint,
		}
	}
	reportName := spec.Report
	if reportName == "" {
		reportName = name
	}
	return DumpReport(output, reportName)
}

func expandArg(token string, vars map[string]string) (string, string) {
	for _, key := range []string{"{php}", "{wp}", "{output}", "{root}", "{database}"} {
		if strings.Contains(token, key) && vars[key] == "" {
			return "", key
		}
		token = strings.ReplaceAll(token, key, vars[key])
	}
	return token, ""
}

func recordedArgs(args []string, output string) []string {
	var recorded []string
	for _, arg := range args {
		if arg == output || strings.HasSuffix(arg, "="+output) {
			continue
		}
		recorded = append(recorded, arg)
	}
	return recorded
}

func exportMySQL(matched addon.Addon, spec addon.Dump, site model.Site, output, secretDir string, secrets []string) (model.DBCapture, []string, error) {
	bin, err := exec.LookPath("mysqldump")
	if err != nil {
		message := spec.MissingMessage
		if message == "" {
			message = "mysqldump is not installed."
		}
		return model.DBCapture{}, secrets, &fail.Error{Code: "step_failed", Message: message, Step: "database_dump", Hint: spec.MissingHint}
	}
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
	if strings.Contains(password, "\n") {
		return model.DBCapture{}, secrets, &fail.Error{Code: "step_failed", Message: "The database password contains a newline and cannot be passed safely.", Step: "database_dump"}
	}
	secrets = append(secrets, password)
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return model.DBCapture{}, secrets, err
	}
	defaults := filepath.Join(secretDir, fmt.Sprintf("mysqldump-%d.cnf", os.Getpid()))
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
			lines = append(lines, "port="+strconv.Itoa(conn.Port))
		}
	}
	if err := writeSecret(defaults, strings.Join(lines, "\n")+"\n"); err != nil {
		return model.DBCapture{}, secrets, err
	}
	defer os.Remove(defaults)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return model.DBCapture{}, secrets, err
	}
	cmd := exec.Command(bin, "--defaults-extra-file="+defaults, "--single-transaction", "--skip-lock-tables", "--no-tablespaces", "--result-file="+output, conn.Name)
	cmd.WaitDelay = 2 * time.Second
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	timer := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	out, runErr := cmd.CombinedOutput()
	if runErr != nil || !nonEmpty(output) {
		text := scrub(string(out), secrets)
		if len(text) > 4000 {
			text = text[len(text)-4000:]
		}
		code := 1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		message := spec.FailMessage
		if message == "" {
			message = "mysqldump failed."
		}
		return model.DBCapture{}, secrets, &fail.Error{
			Code:     "step_failed",
			Message:  message,
			Step:     "database_dump",
			Command:  []string{bin, "--defaults-extra-file", "--single-transaction", "--no-tablespaces", conn.Name},
			ExitCode: fail.ExitCode(code),
			Stderr:   text,
			Hint:     spec.Hint,
		}
	}
	reportName := spec.Report
	if reportName == "" {
		reportName = "mysqldump"
	}
	report, err := DumpReport(output, reportName)
	return report, secrets, err
}

func copySQL(root, relative, output string) (model.DBCapture, error) {
	if relative == "" {
		relative = "database.sql"
	}
	source := filepath.Join(root, filepath.FromSlash(relative))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return model.DBCapture{}, err
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return model.DBCapture{}, err
	}
	if absSource != absRoot && !strings.HasPrefix(absSource, absRoot+string(filepath.Separator)) {
		return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: "The SQL file is outside the site root.", Step: "database_dump"}
	}
	if _, err := os.Stat(absSource); err != nil {
		return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: "SQL file " + relative + " is missing.", Step: "database_dump"}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return model.DBCapture{}, err
	}
	if err := copyFile(absSource, output); err != nil {
		return model.DBCapture{}, err
	}
	return DumpReport(output, "sql_file")
}

func DumpReport(output, method string) (model.DBCapture, error) {
	info, err := os.Stat(output)
	if err != nil {
		return model.DBCapture{}, err
	}
	text, err := os.ReadFile(output)
	if err != nil {
		return model.DBCapture{}, err
	}
	assessment := sqlcheck.Assess(string(text))
	if !assessment.Importable {
		return model.DBCapture{}, &fail.Error{Code: "step_failed", Message: "The database dump is not importable. " + assessment.Reason, Step: "database_dump", Hint: "The dump needs CREATE TABLE statements and inserts that match those tables."}
	}
	sum, err := hashFile(output)
	if err != nil {
		return model.DBCapture{}, err
	}
	return model.DBCapture{
		Method:        method,
		Filename:      "database/database.sql",
		Bytes:         info.Size(),
		SHA256:        sum,
		Tables:        assessment.Tables,
		Importable:    true,
		ServerVersion: serverVersion(string(text)),
	}, nil
}

var serverVersionRe = regexp.MustCompile(`Server version\s+(\S+)`)

func serverVersion(text string) string {
	m := serverVersionRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

func nonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func writeSecret(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(text)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return closeErr
}

func scrub(text string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 3 {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

// Scrub removes secret values from a failure.
func Scrub(err error, secrets []string) error {
	fe, ok := err.(*fail.Error)
	if !ok || fe == nil {
		return err
	}
	fe.Message = scrub(fe.Message, secrets)
	fe.Stderr = scrub(fe.Stderr, secrets)
	fe.Hint = scrub(fe.Hint, secrets)
	for i, part := range fe.Command {
		fe.Command[i] = scrub(part, secrets)
	}
	return fe
}
