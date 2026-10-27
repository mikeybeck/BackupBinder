// Package destination loads destination kinds from JSON.
// Built-in kinds ship embedded; a destinations/ directory next to the module or binary can replace or add kinds.
package destination

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

//go:embed sftp.json
var sftpJSON []byte

//go:embed s3.json
var s3JSON []byte

//go:embed b2.json
var b2JSON []byte

//go:embed disk.json
var diskJSON []byte

//go:embed gdrive.json
var gdriveJSON []byte

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	hostPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	userPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	absPath       = regexp.MustCompile(`^/[A-Za-z0-9._@/=+-]*$`)
	bucketPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,62}$`)
	drivePath     = regexp.MustCompile(`^[A-Za-z0-9._@/=+-]+$`)
	relPath       = regexp.MustCompile(`^[A-Za-z0-9._@/=+-]*$`)
)

var shipped = []struct {
	name string
	raw  []byte
}{
	{"sftp.json", sftpJSON},
	{"s3.json", s3JSON},
	{"b2.json", b2JSON},
	{"disk.json", diskJSON},
	{"gdrive.json", gdriveJSON},
}

// Kind is one destination extension.
type Kind struct {
	ID                 string       `json:"id"`
	Label              string       `json:"label"`
	Note               string       `json:"note"`
	Offsite            bool         `json:"offsite"`
	Fields             []Field      `json:"fields"`
	Credentials        *Credentials `json:"credentials,omitempty"`
	URLTemplate        string       `json:"url"`
	URLWhenPathEmpty   string       `json:"url_when_path_empty,omitempty"`
	URLWhenPrefixEmpty string       `json:"url_when_prefix_empty,omitempty"`
	RequireTools       []string     `json:"require_tools,omitempty"`
	Hooks              []string     `json:"hooks,omitempty"`
	// RcloneArgs replaces restic's default rclone.args. Must keep "serve restic --stdio"
	// (and usually --b2-hard-delete); extra flags for Drive pacing go after that.
	RcloneArgs string `json:"rclone_args,omitempty"`
	Progress   string `json:"progress,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Field is one form/agent input on a kind.
type Field struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Required    bool   `json:"required"`
	Pattern     string `json:"pattern"`
	Setup       string `json:"setup,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// Choices is a named option source for the UI (for example rclone_drive).
	Choices string `json:"choices,omitempty"`
}

// Credentials maps access/secret form values into an env file.
type Credentials struct {
	AccessEnv       string `json:"access_env"`
	SecretEnv       string `json:"secret_env"`
	AccessLabel     string `json:"access_label"`
	SecretLabel     string `json:"secret_label"`
	RequiredAtSetup bool   `json:"required_at_setup"`
}

// SetupFields are values collected during setup / agent turns.
type SetupFields struct {
	Host       string
	User       string
	RemotePath string
	Bucket     string
	Prefix     string
	Endpoint   string
	AccessKey  string
	SecretKey  string
}

// Load returns shipped kinds, then any JSON files that override them by id.
func Load() ([]Kind, error) {
	var out []Kind
	index := map[string]int{}
	for _, item := range shipped {
		kind, err := parse(item.raw, item.name)
		if err != nil {
			return nil, err
		}
		index[kind.ID] = len(out)
		out = append(out, kind)
	}
	for _, dir := range kindDirs() {
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
			kind, err := parse(text, entry.Name())
			if err != nil {
				return nil, err
			}
			if i, ok := index[kind.ID]; ok {
				out[i] = kind
				continue
			}
			index[kind.ID] = len(out)
			out = append(out, kind)
		}
	}
	return out, nil
}

// ByID returns one loaded kind.
func ByID(id string) (Kind, error) {
	kinds, err := Load()
	if err != nil {
		return Kind{}, err
	}
	for _, kind := range kinds {
		if kind.ID == id {
			return kind, nil
		}
	}
	return Kind{}, fail.New("bad_destination", "Destination kind must be one of: "+IDs()+".")
}

// IDs returns the loaded kind ids joined for catalog placeholders.
func IDs() string {
	kinds, err := Load()
	if err != nil || len(kinds) == 0 {
		return "sftp|s3|b2|disk|gdrive"
	}
	ids := make([]string, len(kinds))
	for i, kind := range kinds {
		ids[i] = kind.ID
	}
	return strings.Join(ids, "|")
}

// AppliesTo describes which destination kinds use an agent flag.
func AppliesTo(flag string) string {
	kinds, err := Load()
	if err != nil {
		return ""
	}
	var ids []string
	for _, kind := range kinds {
		if flagApplies(kind, flag) {
			ids = append(ids, kind.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "Applies when the destination kind is " + strings.Join(ids, ", ") + "."
}

func flagApplies(kind Kind, flag string) bool {
	for _, field := range kind.Fields {
		if field.Name == flag || field.Setup == flag {
			return true
		}
	}
	if kind.Credentials != nil && (flag == "access-key" || flag == "secret-key") {
		return true
	}
	return false
}

// Unexpected reports fields the caller set that this kind does not declare.
func (k Kind) Unexpected(dest model.Destination) error {
	known := map[string]bool{}
	for _, field := range k.Fields {
		known[field.Name] = true
	}
	var extra []string
	for _, name := range []string{"host", "user", "path", "bucket", "prefix", "endpoint"} {
		if fieldValue(dest, name) != "" && !known[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return fail.New("bad_destination", "This destination does not use "+strings.Join(extra, ", ")+".")
}

// Validate checks required fields, field patterns, hooks, and required tools.
func (k Kind) Validate(site model.Site, repo string, dest model.Destination) error {
	for _, field := range k.Fields {
		value := fieldValue(dest, field.Name)
		if value == "" {
			if field.Required {
				label := field.Label
				if label == "" {
					label = field.Name
				}
				return fail.New("bad_destination", label+" is required.")
			}
			continue
		}
		if err := matchPattern(field, value); err != nil {
			return err
		}
	}
	for _, hook := range k.Hooks {
		if hook == "disk_safety" {
			if dest.Path == "" {
				continue
			}
			if !filepath.IsAbs(dest.Path) {
				return fail.New("bad_destination", "A disk destination must be an absolute path.")
			}
			if related(dest.Path, site.Root) || related(dest.Path, repo) {
				return fail.New("bad_destination", "The disk destination cannot be the live site or the primary restic repository.")
			}
		}
	}
	for _, tool := range k.RequireTools {
		if _, err := FindTool(tool); err != nil {
			return err
		}
	}
	return nil
}

// Missing returns required field names that are empty on dest.
func (k Kind) Missing(dest model.Destination) []string {
	var missing []string
	for _, field := range k.Fields {
		if field.Required && fieldValue(dest, field.Name) == "" {
			missing = append(missing, field.Name)
		}
	}
	return missing
}

// SetupMissing returns agent field names still needed for setup.
func (k Kind) SetupMissing(in SetupFields) []string {
	var missing []string
	for _, field := range k.Fields {
		if !field.Required {
			continue
		}
		name := field.Setup
		if name == "" {
			name = field.Name
		}
		if setupValue(in, name) == "" {
			missing = append(missing, name)
		}
	}
	if k.Credentials != nil && k.Credentials.RequiredAtSetup {
		if in.AccessKey == "" {
			missing = append(missing, "access-key")
		}
		if in.SecretKey == "" {
			missing = append(missing, "secret-key")
		}
	}
	return missing
}

// SetupReason is the agent prompt when fields are missing.
func (k Kind) SetupReason() string {
	ask := "Ask the user for each missing field. Do not invent a value. "
	if k.Note != "" {
		return ask + k.Note
	}
	return ask + "Ask whether the destination is " + strings.ReplaceAll(IDs(), "|", ", ") + "."
}

// NeedsCredentials reports whether a credential env file is required at copy time.
func (k Kind) NeedsCredentials() bool {
	return k.Credentials != nil
}

// CredentialBody writes the env file body for access/secret, or rejects them.
func (k Kind) CredentialBody(access, secret string) (string, error) {
	if strings.ContainsAny(access, "\n") || strings.ContainsAny(secret, "\n") {
		return "", fail.New("bad_destination", "Credentials cannot contain newlines.")
	}
	if k.Credentials == nil {
		return "", fail.New("bad_destination", "This destination does not take a secret in the form.")
	}
	return k.Credentials.AccessEnv + "=" + access + "\n" + k.Credentials.SecretEnv + "=" + secret + "\n", nil
}

// URL builds a restic repository URL. It does not open a connection.
func (k Kind) URL(dest model.Destination) (string, error) {
	pathTrim := strings.Trim(dest.Path, "/")
	prefixTrim := strings.Trim(dest.Prefix, "/")
	tmpl := k.URLTemplate
	if pathTrim == "" && k.URLWhenPathEmpty != "" {
		tmpl = k.URLWhenPathEmpty
	} else if prefixTrim == "" && k.URLWhenPrefixEmpty != "" {
		tmpl = k.URLWhenPrefixEmpty
	}
	if tmpl == "" {
		return "", fail.New("bad_destination", "Destination kind "+k.ID+" has no URL template.")
	}
	return expand(tmpl, dest), nil
}

// ProgressLabel is the backup progress phase for this destination.
func (k Kind) ProgressLabel(dest model.Destination) string {
	if k.Progress != "" {
		return expand(k.Progress, dest)
	}
	return "Copying to " + k.Label
}

// DetailLabel is the short UI detail line.
func (k Kind) DetailLabel(dest model.Destination) string {
	if k.Detail != "" {
		return expand(k.Detail, dest)
	}
	return k.ID
}

// HasHook reports whether the kind declares a named hook.
func (k Kind) HasHook(name string) bool {
	for _, hook := range k.Hooks {
		if hook == name {
			return true
		}
	}
	return false
}

// BeforeCopy runs mkdir_before_copy when declared.
func (k Kind) BeforeCopy(dest model.Destination) error {
	if !k.HasHook("mkdir_before_copy") {
		return nil
	}
	if dest.Path == "" {
		return nil
	}
	return os.MkdirAll(dest.Path, 0o700)
}

// CheckAccess verifies destination readiness. When contact is false, only local
// config is checked (for example rclone listremotes). When contact is true, a
// cheap reachability probe runs as well.
func (k Kind) CheckAccess(dest model.Destination, contact bool) error {
	if !k.HasHook("rclone_remote") {
		return nil
	}
	if dest.Host == "" {
		return fail.New("bad_destination", "Google Drive remote is required.")
	}
	program, err := FindTool("rclone")
	if err != nil {
		return err
	}
	if err := checkRcloneRemoteConfigured(program, dest.Host); err != nil {
		return err
	}
	if !contact {
		return nil
	}
	return probeRcloneRemote(program, dest.Host)
}

// RcloneProgram returns the rclone binary when this kind needs it.
func (k Kind) RcloneProgram() (string, error) {
	for _, tool := range k.RequireTools {
		if tool == "rclone" {
			return FindTool("rclone")
		}
	}
	return "", nil
}

// RcloneGlobalOpts returns restic -o flags for rclone backends (program path and args).
func (k Kind) RcloneGlobalOpts() []string {
	var opts []string
	if program, err := k.RcloneProgram(); err == nil && program != "" {
		opts = append(opts, "-o", "rclone.program="+program)
	}
	if args := strings.TrimSpace(k.RcloneArgs); args != "" {
		opts = append(opts, "-o", "rclone.args="+args)
	}
	return opts
}

func checkRcloneRemoteConfigured(program, name string) error {
	remotes, err := listRcloneRemotes(program, "")
	if err != nil {
		return err
	}
	want := strings.TrimSuffix(strings.TrimSpace(name), ":")
	for _, remote := range remotes {
		if remote == want {
			return nil
		}
	}
	return fail.New("bad_destination",
		"Google Drive / rclone remote "+quoteRemote(name)+" is not configured. Run rclone config and create a Google Drive remote named "+quoteRemote(name)+".")
}

// ListRcloneRemotes returns configured rclone remote names without a trailing colon.
// When backendType is set (for example "drive"), only that backend is returned.
func ListRcloneRemotes(backendType string) ([]string, error) {
	program, err := FindTool("rclone")
	if err != nil {
		return nil, err
	}
	return listRcloneRemotes(program, backendType)
}

// FieldChoices resolves a field's Choices token into selectable values for the UI.
func FieldChoices(token string) []string {
	switch token {
	case "rclone_drive":
		remotes, err := ListRcloneRemotes("drive")
		if err != nil {
			return nil
		}
		return remotes
	case "rclone_remotes":
		remotes, err := ListRcloneRemotes("")
		if err != nil {
			return nil
		}
		return remotes
	default:
		return nil
	}
}

// ApplyDefaults fills empty destination fields when a safe default exists.
// For Google Drive, a blank host becomes the only configured drive remote.
func (k Kind) ApplyDefaults(dest *model.Destination) {
	if dest == nil || dest.Host != "" || !k.HasHook("rclone_remote") {
		return
	}
	remotes, err := ListRcloneRemotes("drive")
	if err != nil || len(remotes) != 1 {
		return
	}
	dest.Host = remotes[0]
}

func listRcloneRemotes(program, backendType string) ([]string, error) {
	args := []string{"listremotes"}
	if backendType != "" {
		args = append(args, "--type", backendType, "--exact")
	}
	out, err := exec.Command(program, args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fail.New("bad_destination", "Could not list rclone remotes. "+detail)
	}
	return parseRcloneRemotes(string(out)), nil
}

func parseRcloneRemotes(listremotesOut string) []string {
	var remotes []string
	for _, line := range strings.Split(listremotesOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Plain listremotes prints "name:". --long prints "name: type …".
		name := strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		if name == "" {
			continue
		}
		remotes = append(remotes, name)
	}
	return remotes
}

func rcloneRemoteListed(listremotesOut, name string) bool {
	want := strings.TrimSuffix(strings.TrimSpace(name), ":")
	for _, remote := range parseRcloneRemotes(listremotesOut) {
		if remote == want {
			return true
		}
	}
	return false
}

// rcloneProbeTimeout bounds the preflight `rclone about` call.
// On the shared Google Drive client_id, about often takes longer than 30s
// and still succeeds. A 30s cutoff made create_backup refuse a remote the copy can use.
var rcloneProbeTimeout = 2 * time.Minute

func probeRcloneRemote(program, name string) error {
	// Probe the remote root only so a missing folder path is not a false failure;
	// restic creates the repository path on first copy.
	target := strings.TrimSuffix(name, ":") + ":"
	ctx, cancel := context.WithTimeout(context.Background(), rcloneProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "about", target)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	stderr := string(out)
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "didn't find section") || (strings.Contains(lower, "config file") && strings.Contains(lower, "not found")):
		return fail.New("bad_destination",
			"Google Drive / rclone remote "+quoteRemote(name)+" is not configured. Run rclone config and create a Google Drive remote named "+quoteRemote(name)+".")
	case strings.Contains(lower, "token expired") || strings.Contains(lower, "unable to refresh") || strings.Contains(lower, "oauth"):
		return fail.New("bad_destination",
			"The Google Drive / rclone login for "+quoteRemote(name)+" needs to be renewed. Run rclone config reconnect "+quoteRemote(name)+": and authorize again.")
	case ctx.Err() != nil:
		return fail.New("bad_destination",
			"rclone could not reach remote "+quoteRemote(name)+" in time. Check the network and that the remote works (rclone about "+quoteRemote(name)+":).")
	default:
		detail := firstRcloneLine(stderr)
		msg := "rclone could not open remote " + quoteRemote(name) + "."
		if detail != "" {
			msg += " " + detail
		}
		msg += " Check that rclone remote " + quoteRemote(name) + " exists (rclone listremotes) and the folder path is correct."
		return fail.New("bad_destination", msg)
	}
}

func quoteRemote(s string) string {
	if s == "" {
		return s
	}
	return "'" + s + "'"
}

func firstRcloneLine(stderr string) string {
	var fallback string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, " notice:") || strings.Contains(lower, " info:") {
			continue
		}
		if strings.Contains(lower, "fatal") || strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "critical") {
			if len(line) > 240 {
				return line[:240]
			}
			return line
		}
		if fallback == "" && strings.HasPrefix(lower, "rclone:") {
			fallback = line
		}
	}
	if len(fallback) > 240 {
		return fallback[:240]
	}
	return fallback
}

// FindTool resolves a helper binary: BACKUPBINDER_<TOOL>, .tools/<tool>, then PATH.
func FindTool(name string) (string, error) {
	envKey := "BACKUPBINDER_" + strings.ToUpper(name)
	if override := os.Getenv(envKey); override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override, nil
		}
		return "", fail.New("bad_destination", envKey+" does not point at an executable.")
	}
	if root := moduleRoot(); root != "" {
		candidate := filepath.Join(root, ".tools", name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found, nil
	}
	hint := "Put " + name + " on PATH"
	if name == "rclone" {
		hint += ", or set BACKUPBINDER_RCLONE. This checkout can use .tools/rclone. Create a Google Drive remote with rclone config"
	}
	return "", fail.New("bad_destination", name+" is not installed. "+hint+".")
}

func parse(raw []byte, name string) (Kind, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var kind Kind
	if err := dec.Decode(&kind); err != nil {
		return Kind{}, fail.Wrap("bad_destination", name+" is not a destination kind", err)
	}
	if err := validateKind(kind); err != nil {
		return Kind{}, err
	}
	return kind, nil
}

func validateKind(k Kind) error {
	if !idPattern.MatchString(k.ID) {
		return fail.New("bad_destination", "Destination kind id must be lowercase letters, numbers, and hyphens.")
	}
	if strings.TrimSpace(k.Label) == "" {
		return fail.New("bad_destination", "Destination kind "+k.ID+" needs a label.")
	}
	if strings.TrimSpace(k.URLTemplate) == "" {
		return fail.New("bad_destination", "Destination kind "+k.ID+" needs a url template.")
	}
	allowedFields := map[string]bool{"host": true, "user": true, "path": true, "bucket": true, "prefix": true, "endpoint": true}
	allowedPatterns := map[string]bool{
		"hostname": true, "username": true, "abs_path": true, "bucket": true,
		"endpoint": true, "drive_path": true, "rel_path": true,
	}
	for _, field := range k.Fields {
		if !allowedFields[field.Name] {
			return fail.New("bad_destination", "Unknown destination field "+field.Name+".")
		}
		if field.Pattern != "" && !allowedPatterns[field.Pattern] {
			return fail.New("bad_destination", "Unknown field pattern "+field.Pattern+".")
		}
	}
	for _, hook := range k.Hooks {
		if hook != "disk_safety" && hook != "mkdir_before_copy" && hook != "rclone_remote" {
			return fail.New("bad_destination", "Unknown destination hook "+hook+".")
		}
	}
	for _, tool := range k.RequireTools {
		if tool != "rclone" {
			return fail.New("bad_destination", "Unknown destination tool "+tool+".")
		}
	}
	if k.Credentials != nil {
		if k.Credentials.AccessEnv == "" || k.Credentials.SecretEnv == "" {
			return fail.New("bad_destination", "Credentials need access_env and secret_env.")
		}
	}
	return nil
}

func fieldValue(dest model.Destination, name string) string {
	switch name {
	case "host":
		return dest.Host
	case "user":
		return dest.User
	case "path":
		return dest.Path
	case "bucket":
		return dest.Bucket
	case "prefix":
		return dest.Prefix
	case "endpoint":
		return dest.Endpoint
	default:
		return ""
	}
}

func setupValue(in SetupFields, name string) string {
	switch name {
	case "host":
		return in.Host
	case "user":
		return in.User
	case "remote-path", "path":
		return in.RemotePath
	case "bucket":
		return in.Bucket
	case "prefix":
		return in.Prefix
	case "endpoint":
		return in.Endpoint
	case "access-key":
		return in.AccessKey
	case "secret-key":
		return in.SecretKey
	default:
		return ""
	}
}

func matchPattern(field Field, value string) error {
	switch field.Pattern {
	case "hostname":
		if !hostPattern.MatchString(value) {
			return fail.New("bad_destination", field.Label+" must be a hostname, not a shell command.")
		}
	case "username":
		if !userPattern.MatchString(value) {
			return fail.New("bad_destination", field.Label+" must be a plain username.")
		}
	case "abs_path":
		if !filepath.IsAbs(value) || !absPath.MatchString(value) {
			return fail.New("bad_destination", field.Label+" must be an absolute path without spaces.")
		}
	case "bucket":
		if !bucketPattern.MatchString(value) {
			return fail.New("bad_destination", "Bucket name contains characters this tool will not send.")
		}
	case "endpoint":
		endpoint := strings.Trim(strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://"), "/")
		if !hostPattern.MatchString(endpoint) {
			return fail.New("bad_destination", "S3 endpoint must be a hostname.")
		}
	case "drive_path":
		path := strings.Trim(value, "/")
		if path != "" && !drivePath.MatchString(path) {
			return fail.New("bad_destination", field.Label+" must be a folder path without spaces.")
		}
	case "rel_path":
		if value != "" && !relPath.MatchString(strings.Trim(value, "/")) && !relPath.MatchString(value) {
			return fail.New("bad_destination", field.Label+" contains characters this tool will not send.")
		}
	}
	return nil
}

func expand(tmpl string, dest model.Destination) string {
	endpoint := strings.TrimSpace(dest.Endpoint)
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
	}
	endpoint = strings.Trim(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), "/")
	pathAbs := dest.Path
	if pathAbs != "" && !strings.HasPrefix(pathAbs, "/") {
		pathAbs = "/" + pathAbs
	}
	replacer := strings.NewReplacer(
		"{host}", dest.Host,
		"{user}", dest.User,
		"{path}", dest.Path,
		"{path_abs}", pathAbs,
		"{path_trim}", strings.Trim(dest.Path, "/"),
		"{bucket}", strings.Trim(dest.Bucket, "/"),
		"{prefix}", dest.Prefix,
		"{prefix_trim}", strings.Trim(dest.Prefix, "/"),
		"{endpoint}", dest.Endpoint,
		"{endpoint_host}", endpoint,
	)
	return replacer.Replace(tmpl)
}

func related(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(a+sep, b+sep) || strings.HasPrefix(b+sep, a+sep)
}

func kindDirs() []string {
	var dirs []string
	if root := moduleRoot(); root != "" {
		dirs = append(dirs, filepath.Join(root, "destinations"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "destinations"))
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
