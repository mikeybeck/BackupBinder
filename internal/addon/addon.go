// Package addon loads site adapters from JSON. WordPress ships as wordpress.json.
// A directory named addons next to the module or the binary can replace or add adapters.
package addon

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"local-backup/internal/fail"
	"local-backup/internal/model"
)

//go:embed wordpress.json
var wordpressJSON []byte

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Addon is one site adapter. Detection, excludes, and dump commands live in the JSON.
type Addon struct {
	ID        string          `json:"id"`
	Note      string          `json:"note"`
	Ambiguous string          `json:"ambiguous,omitempty"`
	Detect    Detect          `json:"detect"`
	Excludes  []string        `json:"excludes,omitempty"`
	Database  *Database       `json:"database,omitempty"`
	Versions  VersionRules    `json:"versions"`
	Dumps     map[string]Dump `json:"dumps,omitempty"`
}

// Detect names the files that mark a site root.
type Detect struct {
	Files []string `json:"files"`
	Depth int      `json:"depth"`
	Skip  []string `json:"skip,omitempty"`
}

// Database is how a matching site proposes a database source.
type Database struct {
	Engine        string     `json:"engine"`
	Method        string     `json:"method"`
	Auto          []string   `json:"auto,omitempty"`
	DefaultPort   int        `json:"default_port,omitempty"`
	PrefixDefault string     `json:"prefix_default,omitempty"`
	SQLFileIf     *SQLFileIf `json:"sql_file_if,omitempty"`
	Config        ConfigSpec `json:"config"`
}

// SQLFileIf selects a checked-in dump when the database name matches and the file exists.
type SQLFileIf struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// ConfigSpec reads a PHP define() file. The password is not returned from ReadConnection.
type ConfigSpec struct {
	File            string `json:"file"`
	Format          string `json:"format"`
	Name            string `json:"name"`
	User            string `json:"user"`
	Host            string `json:"host"`
	Password        string `json:"password"`
	Prefix          string `json:"prefix,omitempty"`
	PasswordSource  string `json:"password_source"`
	Unreadable      string `json:"unreadable"`
	MissingPassword string `json:"missing_password"`
}

// VersionRules read an application version and component headers.
type VersionRules struct {
	App        *AppVersion     `json:"app,omitempty"`
	Components []ComponentRule `json:"components,omitempty"`
}

// AppVersion is one regular expression with a single capture group.
type AppVersion struct {
	Field   string `json:"field"`
	File    string `json:"file"`
	Pattern string `json:"pattern"`
}

// ComponentRule scans a directory of plugins or themes.
type ComponentRule struct {
	Field    string `json:"field"`
	Dir      string `json:"dir"`
	Header   string `json:"header"`
	Entry    string `json:"entry"`
	DirsOnly bool   `json:"dirs_only,omitempty"`
}

// Dump is one database capture method named by the addon.
type Dump struct {
	Engine            string                 `json:"engine"`
	Argv              []string               `json:"argv,omitempty"`
	RemoteArgv        []string               `json:"remote_argv,omitempty"`
	TimeoutSec        int                    `json:"timeout_sec,omitempty"`
	SkipIfUnreachable bool                   `json:"skip_if_unreachable,omitempty"`
	Report            string                 `json:"report,omitempty"`
	FailMessage       string                 `json:"fail_message,omitempty"`
	Hint              string                 `json:"hint,omitempty"`
	Missing           map[string]MissingTool `json:"missing,omitempty"`
	MissingMessage    string                 `json:"missing_message,omitempty"`
	MissingHint       string                 `json:"missing_hint,omitempty"`
}

// MissingTool is the error used when a placeholder cannot be resolved.
type MissingTool struct {
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Connection is a database endpoint. Password is never a field.
type Connection struct {
	Name           string
	User           string
	Host           string
	Port           int
	Socket         string
	TablePrefix    string
	PasswordSource string
}

// Load returns the shipped addons, then any JSON files that override them by id.
func Load() ([]Addon, error) {
	shipped, err := parse(wordpressJSON, "wordpress.json")
	if err != nil {
		return nil, err
	}
	out := []Addon{shipped}
	index := map[string]int{shipped.ID: 0}
	for _, dir := range addonDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			item, err := parse(text, entry.Name())
			if err != nil {
				return nil, err
			}
			if i, ok := index[item.ID]; ok {
				out[i] = item
				continue
			}
			index[item.ID] = len(out)
			out = append(out, item)
		}
	}
	return out, nil
}

// ByID returns one loaded addon.
func ByID(id string) (Addon, error) {
	addons, err := Load()
	if err != nil {
		return Addon{}, err
	}
	for _, item := range addons {
		if item.ID == id {
			return item, nil
		}
	}
	return Addon{}, fail.New("bad_adapter", "No addon named "+id+" is installed.")
}

// Find returns the addon and site root for path. ok is false when no addon matches.
func Find(path string) (Addon, string, bool, error) {
	addons, err := Load()
	if err != nil {
		return Addon{}, "", false, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Addon{}, "", false, err
	}
	for _, item := range addons {
		if item.matches(path) {
			return item, path, true, nil
		}
	}
	found := map[string]Addon{}
	var order []string
	for _, item := range addons {
		roots, err := item.search(path)
		if err != nil {
			return Addon{}, "", false, err
		}
		for _, root := range roots {
			if _, ok := found[root]; ok {
				continue
			}
			found[root] = item
			order = append(order, root)
		}
	}
	if len(order) > 1 {
		item := found[order[0]]
		message := item.Ambiguous
		if message == "" {
			message = "That folder contains more than one site. Pass the site root you want to back up."
		}
		return Addon{}, "", false, &fail.Error{Code: "ambiguous_site", Message: message}
	}
	if len(order) == 1 {
		return found[order[0]], order[0], true, nil
	}
	return Addon{}, path, false, nil
}

// DatabaseSource proposes the database half of a contract. It does not return the password.
func (a Addon) DatabaseSource(root string) (model.Source, error) {
	if a.Database == nil {
		return model.Source{}, fail.New("step_failed", "This addon has no database source.")
	}
	conn, err := a.ReadConnection(root)
	if err != nil {
		return model.Source{}, fail.Wrap("step_failed", "could not read "+a.Database.Config.File, err)
	}
	method := a.Database.Method
	sqlFile := ""
	if a.Database.SQLFileIf != nil && conn.Name == a.Database.SQLFileIf.Name {
		candidate := a.Database.SQLFileIf.File
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(candidate))); statErr == nil {
			method = "sql_file"
			sqlFile = candidate
		}
	}
	return model.Source{
		ID:             "database",
		Kind:           "database",
		Engine:         a.Database.Engine,
		Method:         method,
		SQLFile:        sqlFile,
		Name:           conn.Name,
		User:           conn.User,
		Host:           hostLabel(conn),
		PasswordSource: conn.PasswordSource,
		TablePrefix:    conn.TablePrefix,
	}, nil
}

// ReadConnection reads the addon config file and omits the password.
func (a Addon) ReadConnection(root string) (Connection, error) {
	if a.Database == nil {
		return Connection{}, fail.New("step_failed", "This addon has no database source.")
	}
	spec := a.Database.Config
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec.File)))
	if err != nil {
		return Connection{}, err
	}
	defines := phpDefines(string(text))
	conn := Connection{
		Name: defines[spec.Name],
		User: defines[spec.User],
		Host: defines[spec.Host],
	}
	if a.Database.PrefixDefault != "" {
		conn.TablePrefix = a.Database.PrefixDefault
	}
	if _, ok := defines[spec.Password]; ok {
		conn.PasswordSource = spec.PasswordSource
	}
	if spec.Prefix != "" {
		if prefix, ok := phpAssignment(string(text), spec.Prefix); ok && prefix != "" {
			conn.TablePrefix = prefix
		}
	}
	host := conn.Host
	if i := strings.Index(host, ":"); i >= 0 {
		left, right := host[:i], host[i+1:]
		if strings.HasPrefix(right, "/") {
			conn.Host = left
			if conn.Host == "" {
				conn.Host = "localhost"
			}
			conn.Socket = right
		} else if port, err := strconv.Atoi(right); err == nil {
			conn.Host = left
			conn.Port = port
		}
	}
	return conn, nil
}

// ReadPassword reads the database password at dump time. Callers must not persist it.
func (a Addon) ReadPassword(root string) (string, error) {
	if a.Database == nil {
		return "", fail.New("step_failed", "This addon has no database source.")
	}
	spec := a.Database.Config
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec.File)))
	if err != nil {
		return "", &fail.Error{Code: "step_failed", Message: spec.Unreadable, Step: "database_dump"}
	}
	defines := phpDefines(string(text))
	password, ok := defines[spec.Password]
	if !ok {
		return "", &fail.Error{Code: "step_failed", Message: spec.MissingPassword, Step: "database_dump"}
	}
	return password, nil
}

// FillVersions writes the versions this addon knows how to read.
func (a Addon) FillVersions(root string, out *model.Versions) {
	if out.Plugins == nil {
		out.Plugins = []model.Component{}
	}
	if out.Themes == nil {
		out.Themes = []model.Component{}
	}
	if a.Versions.App != nil && a.Versions.App.Field == "wordpress" {
		out.WordPress = appVersion(root, *a.Versions.App)
	}
	for _, rule := range a.Versions.Components {
		found := componentVersions(root, rule)
		switch rule.Field {
		case "plugins":
			out.Plugins = found
		case "themes":
			out.Themes = found
		}
	}
	if a.Database != nil {
		if conn, err := a.ReadConnection(root); err == nil {
			out.TablePrefix = conn.TablePrefix
		}
	}
}

// Dump returns one named dump method.
func (a Addon) Dump(name string) (Dump, bool) {
	item, ok := a.Dumps[name]
	return item, ok
}

func (a Addon) matches(path string) bool {
	if len(a.Detect.Files) == 0 {
		return false
	}
	for _, name := range a.Detect.Files {
		if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(name))); err != nil {
			return false
		}
	}
	return true
}

func (a Addon) search(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	skip := map[string]struct{}{}
	for _, name := range a.Detect.Skip {
		skip[name] = struct{}{}
	}
	depthLimit := a.Detect.Depth
	if depthLimit == 0 {
		depthLimit = 3
	}
	var found []string
	err = filepath.WalkDir(path, func(current string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(filepath.Separator)))
		}
		if rel != "." {
			if _, skipName := skip[d.Name()]; skipName || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
		}
		if depth > depthLimit {
			return filepath.SkipDir
		}
		if rel != "." && a.matches(current) {
			found = append(found, current)
			return filepath.SkipDir
		}
		return nil
	})
	return found, err
}

func addonDirs() []string {
	var dirs []string
	if root := moduleRoot(); root != "" {
		dirs = append(dirs, filepath.Join(root, "addons"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "addons"))
	}
	return dirs
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func hostLabel(conn Connection) string {
	if conn.Socket != "" {
		return conn.Host + ":" + conn.Socket
	}
	if conn.Port != 0 {
		return conn.Host + ":" + strconv.Itoa(conn.Port)
	}
	return conn.Host
}

func appVersion(root string, rule AppVersion) string {
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rule.File)))
	if err != nil {
		return ""
	}
	re, err := regexp.Compile(rule.Pattern)
	if err != nil {
		return ""
	}
	m := re.FindSubmatch(text)
	if m == nil || len(m) < 2 {
		return ""
	}
	return string(m[1])
}

func componentVersions(root string, rule ComponentRule) []model.Component {
	dir := filepath.Join(root, filepath.FromSlash(rule.Dir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []model.Component{}
	}
	var found []model.Component
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || skipComponent(entry.Name()) {
			continue
		}
		if rule.DirsOnly && !entry.IsDir() {
			continue
		}
		target := componentFile(dir, entry, rule)
		if target == "" {
			continue
		}
		name, version, ok := readHeader(target, rule.Header)
		if !ok || name == "" {
			continue
		}
		rel, _ := filepath.Rel(root, target)
		found = append(found, model.Component{Name: name, Version: version, Path: filepath.ToSlash(rel)})
	}
	if found == nil {
		return []model.Component{}
	}
	return found
}

func skipComponent(name string) bool {
	switch name {
	case "node_modules", "cache", ".cache", "bower_components", "__pycache__", ".git":
		return true
	default:
		return false
	}
}

func componentFile(dir string, entry os.DirEntry, rule ComponentRule) string {
	full := filepath.Join(dir, entry.Name())
	if !entry.IsDir() {
		if strings.HasSuffix(entry.Name(), ".php") && strings.Contains(rule.Entry, "{name}") {
			return full
		}
		return ""
	}
	entryName := strings.ReplaceAll(rule.Entry, "{name}", entry.Name())
	candidate := filepath.Join(full, filepath.FromSlash(entryName))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	if !strings.Contains(rule.Entry, "{name}") {
		return ""
	}
	children, _ := os.ReadDir(full)
	for _, child := range children {
		if strings.HasSuffix(child.Name(), ".php") && !child.IsDir() {
			return filepath.Join(full, child.Name())
		}
	}
	return ""
}

func readHeader(path, nameLabel string) (string, string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	text := string(buf[:n])
	name := headerValue(text, nameLabel)
	version := headerValue(text, "Version")
	if name == "" && version == "" {
		return "", "", false
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return name, version, true
}

func headerValue(text, label string) string {
	prefix := strings.ToLower(label) + ":"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func parse(text []byte, name string) (Addon, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.DisallowUnknownFields()
	var item Addon
	if err := dec.Decode(&item); err != nil {
		return Addon{}, fail.Wrap("bad_addon", name+" is not an addon", err)
	}
	if err := item.validate(); err != nil {
		return Addon{}, fail.Wrap("bad_addon", name+" is not an addon", err)
	}
	return item, nil
}

func (a Addon) validate() error {
	if !idPattern.MatchString(a.ID) {
		return fail.New("bad_addon", "Addon id must be lowercase letters, numbers, and hyphens.")
	}
	if len(a.Detect.Files) == 0 {
		return fail.New("bad_addon", "An addon needs at least one detect file.")
	}
	for _, name := range a.Detect.Files {
		if err := relativePath(name); err != nil {
			return err
		}
	}
	for _, name := range a.Excludes {
		if err := relativePath(name); err != nil {
			return err
		}
	}
	if a.Database != nil {
		if err := a.Database.validate(a.Dumps); err != nil {
			return err
		}
	}
	if a.Versions.App != nil {
		if a.Versions.App.Field != "wordpress" {
			return fail.New("bad_addon", "Version field must be wordpress.")
		}
		if err := relativePath(a.Versions.App.File); err != nil {
			return err
		}
		re, err := regexp.Compile(a.Versions.App.Pattern)
		if err != nil || re.NumSubexp() != 1 {
			return fail.New("bad_addon", "Version pattern needs one capture group.")
		}
	}
	for _, rule := range a.Versions.Components {
		if rule.Field != "plugins" && rule.Field != "themes" {
			return fail.New("bad_addon", "Component field must be plugins or themes.")
		}
		if err := relativePath(rule.Dir); err != nil {
			return err
		}
		if strings.Contains(rule.Entry, "..") || filepath.IsAbs(rule.Entry) {
			return fail.New("bad_addon", "Component entry must stay inside the site.")
		}
	}
	for name, dump := range a.Dumps {
		if err := dump.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (d Database) validate(dumps map[string]Dump) error {
	if d.Config.Format != "php_define" {
		return fail.New("bad_addon", "Database config format must be php_define.")
	}
	if err := relativePath(d.Config.File); err != nil {
		return err
	}
	if d.Config.Name == "" || d.Config.User == "" || d.Config.Host == "" || d.Config.Password == "" {
		return fail.New("bad_addon", "Database config needs name, user, host, and password keys.")
	}
	if d.SQLFileIf != nil {
		if err := relativePath(d.SQLFileIf.File); err != nil {
			return err
		}
	}
	switch d.Method {
	case "auto":
		if len(d.Auto) == 0 {
			return fail.New("bad_addon", "An auto database method needs a list of dump methods.")
		}
		for _, name := range d.Auto {
			if _, ok := dumps[name]; !ok {
				return fail.New("bad_addon", "Unknown dump method "+name+".")
			}
		}
	case "sql_file":
	default:
		if _, ok := dumps[d.Method]; !ok {
			return fail.New("bad_addon", "Unknown dump method "+d.Method+".")
		}
	}
	return nil
}

func (d Dump) validate(name string) error {
	switch d.Engine {
	case "mysqldump":
		for _, arg := range d.RemoteArgv {
			if err := argvToken(arg); err != nil {
				return err
			}
		}
		return nil
	case "argv":
		if len(d.Argv) == 0 {
			return fail.New("bad_addon", "Dump "+name+" needs an argument list.")
		}
		for _, arg := range append(append([]string{}, d.Argv...), d.RemoteArgv...) {
			if err := argvToken(arg); err != nil {
				return err
			}
		}
		return nil
	default:
		return fail.New("bad_addon", "Dump engine must be argv or mysqldump.")
	}
}

func argvToken(arg string) error {
	if arg == "" {
		return fail.New("bad_addon", "A dump argument is empty.")
	}
	for _, r := range arg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:@={}[]+", r):
			continue
		default:
			return fail.New("bad_addon", "Dump argument "+arg+" has a character the addon runner will not pass.")
		}
	}
	expanded := arg
	for _, key := range []string{"{php}", "{wp}", "{output}", "{root}", "{database}"} {
		expanded = strings.ReplaceAll(expanded, key, "")
	}
	if strings.Contains(expanded, "{") || strings.Contains(expanded, "}") {
		return fail.New("bad_addon", "Dump argument "+arg+" has an unknown placeholder.")
	}
	return nil
}

func relativePath(name string) error {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "..") {
		return fail.New("bad_addon", "Addon paths must stay inside the site.")
	}
	return nil
}

func phpDefines(text string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], "define")
		if j < 0 {
			break
		}
		i += j + len("define")
		i = skipSpace(text, i)
		if i >= len(text) || text[i] != '(' {
			continue
		}
		i = skipSpace(text, i+1)
		key, next, ok := phpString(text, i)
		if !ok {
			continue
		}
		i = skipSpace(text, next)
		if i >= len(text) || text[i] != ',' {
			continue
		}
		i = skipSpace(text, i+1)
		val, next, ok := phpString(text, i)
		if !ok {
			continue
		}
		if _, exists := out[key]; !exists {
			out[key] = val
		}
		i = next
	}
	return out
}

func phpAssignment(text, name string) (string, bool) {
	needle := "$" + name
	i := strings.Index(text, needle)
	if i < 0 {
		return "", false
	}
	i = skipSpace(text, i+len(needle))
	if i >= len(text) || text[i] != '=' {
		return "", false
	}
	i = skipSpace(text, i+1)
	value, _, ok := phpString(text, i)
	return value, ok
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func phpString(s string, i int) (string, int, bool) {
	if i >= len(s) || (s[i] != '\'' && s[i] != '"') {
		return "", i, false
	}
	quote := s[i]
	i++
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == quote {
			return b.String(), i + 1, true
		}
		b.WriteByte(c)
		i++
	}
	return "", i, false
}
