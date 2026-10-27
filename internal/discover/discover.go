// Package discover reads a directory and proposes a backup contract.
// Site types come from addons. Database passwords are read from the config file
// the addon names, or from a saved secret — never stored on the site contract.
package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"backupbinder/internal/addon"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

var (
	skipDirs = map[string]struct{}{
		"node_modules": {}, "cache": {}, ".cache": {}, "bower_components": {},
		"__pycache__": {}, ".git": {}, ".sass-cache": {}, ".parcel-cache": {}, "coverage": {},
	}
	junkFiles = map[string]struct{}{".DS_Store": {}, "Thumbs.db": {}, "desktop.ini": {}}
)

// SkipReason reports why a relative path is left out of every file capture.
func SkipReason(rel string) string {
	rel = filepath.ToSlash(rel)
	for _, part := range strings.Split(rel, "/") {
		if part == "" {
			continue
		}
		if _, ok := skipDirs[part]; ok {
			if part == "cache" || part == ".cache" {
				return "cache"
			}
			return strings.TrimPrefix(part, ".")
		}
		if _, ok := junkFiles[part]; ok {
			return "junk"
		}
	}
	return ""
}

// ExcludeReason reports why an addon exclude leaves a relative path out.
func ExcludeReason(rel string, excludes []string) string {
	rel = filepath.ToSlash(rel)
	for _, raw := range excludes {
		pattern := filepath.ToSlash(strings.TrimSpace(raw))
		if pattern == "" {
			continue
		}
		if !strings.Contains(pattern, "/") {
			for _, part := range strings.Split(rel, "/") {
				if part == pattern {
					return excludeLabel(pattern)
				}
			}
			continue
		}
		if rel == pattern || strings.HasPrefix(rel, pattern+"/") || strings.HasSuffix(rel, "/"+pattern) {
			return excludeLabel(pattern)
		}
	}
	return ""
}

func excludeLabel(pattern string) string {
	base := pattern
	if i := strings.LastIndex(pattern, "/"); i >= 0 {
		base = pattern[i+1:]
	}
	if base == "debug.log" {
		return "debug-log"
	}
	return strings.TrimPrefix(base, ".")
}

// FindPHP returns a PHP binary, or empty.
func FindPHP() string {
	if override := os.Getenv("BACKUPBINDER_PHP"); override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override
		}
	}
	if found, err := exec.LookPath("php"); err == nil {
		return found
	}
	bases := []string{
		filepath.Join(os.Getenv("HOME"), ".config", "Local", "lightning-services"),
		"/opt/Local/resources/extraResources/lightning-services",
	}
	var candidates []string
	for _, base := range bases {
		matches, _ := filepath.Glob(filepath.Join(base, "php-*", "bin", "linux", "bin", "php"))
		candidates = append(candidates, matches...)
		matches, _ = filepath.Glob(filepath.Join(base, "php-*", "bin", "php"))
		candidates = append(candidates, matches...)
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[len(candidates)-1]
}

func phpVersion(binary string) string {
	cmd := exec.Command(binary, "-r", "echo PHP_VERSION;")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Result is a proposed contract. Saved is false until register_site.
type Result struct {
	OK       bool           `json:"ok"`
	Saved    bool           `json:"saved"`
	Site     model.Site     `json:"site"`
	Detected model.Versions `json:"detected"`
	Note     string         `json:"note"`
}

// Site proposes a contract for path. It does not write the registry.
func Site(path, name, id string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) && looksLikeHostPath(path) {
			return Result{}, fail.New("missing_path", path+" does not exist. A hostname is not a directory. Call setup --host "+path+" --user USER --remote-path PATH. Do not create a directory with that name.")
		}
		return Result{}, fail.New("missing_path", path+" does not exist.")
	}
	if !info.IsDir() {
		return Result{}, fail.New("missing_path", path+" is not a directory.")
	}
	matched, root, ok, err := addon.Find(path)
	if err != nil {
		if fe, isFail := err.(*fail.Error); isFail && fe.Code == "ambiguous_site" {
			return Result{}, err
		}
		return Result{}, fail.Wrap("missing_path", "could not read the path", err)
	}
	if !ok {
		root = path
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	if name == "" {
		name = filepath.Base(root)
	}
	if id == "" {
		id = slug(name)
	}
	adapter := "files"
	note := "No addon matched this directory. The contract is a file source only."
	excludes := []string{"node_modules", "cache", ".git"}
	var database *model.Source
	if ok {
		adapter = matched.ID
		if matched.Note != "" {
			note = matched.Note
		}
		if len(matched.Excludes) > 0 {
			excludes = matched.Excludes
		}
		if matched.Database != nil {
			source, err := matched.DatabaseSource(root)
			if err != nil {
				return Result{}, err
			}
			database = &source
		}
	}
	sources := []model.Source{{
		ID:       "files",
		Kind:     "files",
		Path:     ".",
		Excludes: excludes,
	}}
	if database != nil {
		sources = append(sources, *database)
	}
	site := model.Site{
		ID:                id,
		Name:              name,
		Root:              root,
		Adapter:           adapter,
		Sources:           sources,
		Schedule:          model.Schedule{},
		RestoreConfidence: model.RestoreConfidence{},
		Policy:            model.Policy{NeverOverwriteLive: true, RestoreTarget: "scratch_only"},
		Retention:         model.DefaultRetention(),
		Destinations: []model.Destination{{
			ID: "local", Kind: "local_restic", Enabled: true,
		}},
	}
	return Result{OK: true, Saved: false, Site: site, Detected: Versions(root, adapter), Note: note}, nil
}

// Versions reads versions for the site's addon. It does not read the database password.
func Versions(root, adapter string) model.Versions {
	out := model.Versions{Plugins: []model.Component{}, Themes: []model.Component{}}
	if php := FindPHP(); php != "" {
		out.PHPCLI = phpVersion(php)
	}
	if adapter == "" || adapter == "files" {
		return out
	}
	matched, err := addon.ByID(adapter)
	if err != nil {
		return out
	}
	matched.FillVersions(root, &out)
	return out
}

func slug(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "site"
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

func looksLikeHostPath(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, ".") && !strings.ContainsAny(value, "/\\ ")
}
