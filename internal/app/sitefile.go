package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backupbinder/internal/destination"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
	"backupbinder/internal/schedule"
)

// siteDocument is the hand-edited contract. Passwords and cloud keys are not fields.
type siteDocument struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Root         string              `json:"root"`
	Adapter      string              `json:"adapter"`
	Sources      []model.Source      `json:"sources"`
	Location     model.Location      `json:"location"`
	Destinations []model.Destination `json:"destinations"`
	Schedule     model.Schedule      `json:"schedule"`
	Retention    *model.Retention    `json:"retention,omitempty"`
	Limits       model.Limits        `json:"limits"`
	Setup        []string            `json:"setup"`
}

func (a *App) siteFile(id string) string {
	return filepath.Join(a.Home, "sites", id+".json")
}

// RemoveSite drops the registry row, the site file, and this site's secret files.
// The local restic repository is left in place.
func (a *App) RemoveSite(siteID string) (string, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return "", err
	}
	if err := a.refuseBusy(site.ID, ""); err != nil {
		return "", err
	}
	if status, ok := a.ReadRestoreStatus(site.ID); ok && status.InProgress {
		return "", fail.New("restore_busy", "A restore is already running for this site.")
	}
	repo := a.repoPath(site)
	secrets := []string{a.databaseSecretPath(site.ID)}
	for _, dest := range site.Destinations {
		if dest.CredentialFile != "" {
			secrets = append(secrets, dest.CredentialFile)
		}
	}
	if err := os.Remove(a.siteFile(site.ID)); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := a.DB.DeleteSite(site.ID); err != nil {
		_ = a.saveSiteFile(site.ID)
		return "", err
	}
	for _, path := range secrets {
		_ = os.Remove(path)
	}
	return repo, nil
}

// saveSiteFile writes the site file and records that write as the site's updated time.
func (a *App) saveSiteFile(siteID string) error {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return err
	}
	doc := siteDocument{
		ID: site.ID, Name: site.Name, Root: site.Root, Adapter: site.Adapter,
		Sources: site.Sources, Location: site.Location, Schedule: site.Schedule,
		Retention: &site.Retention, Limits: site.Limits, Setup: site.Setup,
	}
	if doc.Sources == nil {
		doc.Sources = []model.Source{}
	}
	if doc.Setup == nil {
		doc.Setup = []string{}
	}
	doc.Destinations = make([]model.Destination, 0, len(site.Destinations))
	for _, dest := range site.Destinations {
		dest.Credentials = ""
		doc.Destinations = append(doc.Destinations, dest)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := a.siteFile(site.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return a.DB.SetUpdatedAt(site.ID, info.ModTime())
}

// ReloadSites applies site files that are newer than the registry.
func (a *App) ReloadSites() error {
	dir := filepath.Join(a.Home, "sites")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		site, err := a.DB.GetSite(id)
		if err != nil {
			if fe, ok := err.(*fail.Error); !ok || fe.Code != "not_found" {
				return err
			}
		} else if !newerFile(info.ModTime(), site.UpdatedAt) {
			continue
		}
		if err := a.importSiteFile(path, info.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

func newerFile(mod time.Time, updated string) bool {
	updated = strings.TrimSpace(updated)
	if updated == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return true
	}
	return mod.UTC().After(parsed.UTC())
}

func (a *App) importSiteFile(path string, mod time.Time) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := rejectSecrets(raw); err != nil {
		return err
	}
	var doc siteDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fail.New("bad_config", "The site file is not valid JSON.")
	}
	base := strings.TrimSuffix(filepath.Base(path), ".json")
	if doc.ID == "" || doc.ID != base {
		return fail.New("bad_config", "The site file name must be the site id plus .json.")
	}
	site := model.Site{
		ID: doc.ID, Name: doc.Name, Root: doc.Root, Adapter: doc.Adapter,
		Sources: doc.Sources, Location: doc.Location, Destinations: doc.Destinations,
		Schedule: doc.Schedule, Limits: doc.Limits, Setup: doc.Setup,
	}
	if doc.Retention != nil {
		site.Retention = *doc.Retention
	} else {
		site.Retention = model.DefaultRetention()
	}
	if site.Location.Kind == "" || site.Location.Kind == "local" {
		abs, err := filepath.Abs(strings.TrimSpace(site.Root))
		if err != nil {
			return err
		}
		site.Root = abs
		site.Location = model.Location{Kind: "local"}
	}
	if err := a.validateSiteFile(site); err != nil {
		return err
	}
	for i := range site.Destinations {
		site.Destinations[i].Credentials = ""
	}
	return a.DB.ImportSite(site, mod)
}

func rejectSecrets(raw []byte) error {
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fail.New("bad_config", "The site file is not valid JSON.")
	}
	if secretKey(body) {
		return fail.New("bad_config", "The site file cannot contain a password, access key, or secret key. Those stay in the secrets directory.")
	}
	return nil
}

func secretKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "password", "access_key", "secret_key", "access-key", "secret-key", "db_password", "db-password":
				return true
			}
			if secretKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if secretKey(child) {
				return true
			}
		}
	}
	return false
}

func (a *App) validateSiteFile(site model.Site) error {
	if !validID(site.ID) {
		return fail.New("bad_id", "Site id must be lowercase letters, numbers, and hyphens.")
	}
	if strings.TrimSpace(site.Name) == "" {
		return fail.New("bad_config", "The site file needs a name.")
	}
	if strings.TrimSpace(site.Adapter) == "" {
		return fail.New("bad_config", "The site file needs an adapter.")
	}
	if !hasFilesSource(site) {
		return fail.New("bad_config", "The site file needs a files source.")
	}
	switch strings.TrimSpace(site.Location.Kind) {
	case "", "local":
		info, err := os.Stat(site.Root)
		if err != nil || !info.IsDir() {
			return fail.New("missing_path", "The directory is missing: "+site.Root)
		}
	case "sftp":
		if site.Location.Host == "" || site.Location.User == "" || site.Location.Path == "" {
			return fail.New("bad_location", "A server needs a host, a user, and an absolute path.")
		}
		if !hostPattern.MatchString(site.Location.Host) {
			return fail.New("bad_location", "SSH host must be a hostname, not a shell command.")
		}
		if !userPattern.MatchString(site.Location.User) {
			return fail.New("bad_location", "SSH user must be a plain username.")
		}
		if !remotePath.MatchString(site.Location.Path) {
			return fail.New("bad_location", "The server path must be an absolute path without spaces.")
		}
		if strings.TrimSpace(site.Root) == "" {
			return fail.New("bad_location", "A server site needs a local copy directory.")
		}
		if err := os.MkdirAll(site.Root, 0o755); err != nil {
			return err
		}
	default:
		return fail.New("bad_location", "Choose a directory on this machine or one server.")
	}
	if db := model.DatabaseSource(site); db != nil && db.Method != "sql_file" {
		if db.Name == "" || db.User == "" || db.Host == "" {
			return fail.New("bad_database", "A database dump needs a name, user, and host.")
		}
	}
	if site.Schedule.Enabled || site.Schedule.Cron != "" {
		if err := schedule.Check(site.Schedule.Cron); err != nil {
			return err
		}
	}
	if site.Limits.UploadKiB < 0 || site.Limits.DownloadKiB < 0 {
		return fail.New("bad_limit", "A speed limit is a whole number of KiB per second. Use 0 for unlimited.")
	}
	if err := validateRetention(site.Retention); err != nil {
		return err
	}
	for _, step := range site.Setup {
		if !knownSetupStep(step) {
			return fail.New("bad_config", "Unknown setup step "+step+".")
		}
	}
	for _, dest := range site.Destinations {
		if dest.Kind == "local_restic" || dest.Kind == "" {
			continue
		}
		kind, err := destination.ByID(dest.Kind)
		if err != nil {
			return err
		}
		if err := kind.Validate(site, a.repoPath(site), dest); err != nil {
			return err
		}
	}
	return nil
}

func hasFilesSource(site model.Site) bool {
	for _, source := range site.Sources {
		if source.Kind == "files" {
			return true
		}
	}
	return false
}

func knownSetupStep(step string) bool {
	switch step {
	case "location", "name", "database", "destination", "schedule", "backup":
		return true
	default:
		return false
	}
}
