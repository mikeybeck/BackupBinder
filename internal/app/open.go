package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"

	"backupbinder/internal/archive"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
	"backupbinder/internal/store"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// App is the process: registry, restic, and the backup path the CLI and UI share.
type App struct {
	Home     string
	RepoRoot string
	DB       *store.Store
	Restic   archive.Runner
	liveMu   sync.Mutex
	live     map[string]struct{}
}

func Open(home string) (*App, error) {
	if home == "" {
		home = DefaultHome()
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(abs)
	if err != nil {
		return nil, err
	}
	binary, err := FindRestic()
	if err != nil {
		db.Close()
		return nil, err
	}
	application := &App{
		Home:     abs,
		RepoRoot: findModuleRoot(),
		DB:       db,
		Restic:   archive.Runner{Binary: binary, CacheDir: filepath.Join(abs, "cache", "restic")},
	}
	if err := application.ReloadSites(); err != nil {
		db.Close()
		return nil, err
	}
	return application, nil
}

func (a *App) resticFor(site model.Site) archive.Runner {
	return a.Restic.WithLimits(site.Limits.UploadKiB, site.Limits.DownloadKiB)
}

func (a *App) Close() error {
	if a.DB == nil {
		return nil
	}
	return a.DB.Close()
}

func DefaultHome() string {
	if value := os.Getenv("BACKUPBINDER_HOME"); value != "" {
		return value
	}
	if root := findModuleRoot(); root != "" {
		return filepath.Join(root, "var")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "var"
	}
	return filepath.Join(cwd, "var")
}

func FindRestic() (string, error) {
	if override := os.Getenv("BACKUPBINDER_RESTIC"); override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override, nil
		}
		return "", fail.New("restic_missing", "BACKUPBINDER_RESTIC does not point at an executable.")
	}
	if root := findModuleRoot(); root != "" {
		candidate := filepath.Join(root, ".tools", "restic")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath("restic"); err == nil {
		return found, nil
	}
	return "", fail.New("restic_missing", "restic is not installed. Put it on PATH, or set BACKUPBINDER_RESTIC. This checkout can use .tools/restic.")
}

func findModuleRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		if root := walkForGoMod(cwd); root != "" {
			return root
		}
	}
	if exe, err := os.Executable(); err == nil {
		if root := walkForGoMod(filepath.Dir(exe)); root != "" {
			return root
		}
	}
	return ""
}

func walkForGoMod(start string) string {
	dir := start
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

func validID(id string) bool {
	return idPattern.MatchString(id)
}
