package app

import (
	"os"
	"path/filepath"
	"strings"

	"local-backup/internal/addon"
	"local-backup/internal/discover"
	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/store"
)

// DatabaseInput is the site-page and set_database form.
// An empty password leaves the saved secret unchanged.
// PreferConfig clears a saved password and reads the site config again.
type DatabaseInput struct {
	Enabled      bool
	Name         string
	User         string
	Host         string
	Password     string
	PreferConfig bool
}

// SetDatabase chooses whether each backup includes a database dump, and optionally
// stores connection fields and a password under var/secrets. The password is never
// a field on the site contract.
func (a *App) SetDatabase(siteID string, in DatabaseInput) (model.Site, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Site{}, err
	}
	if !in.Enabled {
		if err := a.DB.SaveAdapter(site.ID, site.Adapter, withoutDatabase(site.Sources)); err != nil {
			return model.Site{}, err
		}
		_ = os.Remove(a.databaseSecretPath(site.ID))
		return a.DB.GetSite(site.ID)
	}

	db, adapter, err := a.databaseSource(site, in)
	if err != nil {
		return model.Site{}, err
	}
	if in.Name != "" {
		db.Name = strings.TrimSpace(in.Name)
	}
	if in.User != "" {
		db.User = strings.TrimSpace(in.User)
	}
	if in.Host != "" {
		db.Host = strings.TrimSpace(in.Host)
	}
	if db.Name == "" || db.User == "" || db.Host == "" {
		return model.Site{}, fail.New("bad_database", "A database dump needs a name, user, and host.")
	}
	if err := a.applyDatabasePassword(&db, site.ID, in); err != nil {
		return model.Site{}, err
	}
	if err := a.DB.SaveAdapter(site.ID, adapter, withDatabase(site.Sources, db)); err != nil {
		return model.Site{}, err
	}
	return a.DB.GetSite(site.ID)
}

func (a *App) databaseSource(site model.Site, in DatabaseInput) (model.Source, string, error) {
	if current := model.DatabaseSource(site); current != nil {
		return *current, site.Adapter, nil
	}
	discovered, err := discover.Site(site.Root, site.Name, site.ID)
	if err == nil {
		if found := model.DatabaseSource(discovered.Site); found != nil {
			adapter := site.Adapter
			if adapter == "files" && discovered.Site.Adapter != "" {
				adapter = discovered.Site.Adapter
			}
			return *found, adapter, nil
		}
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.User) == "" || strings.TrimSpace(in.Host) == "" {
		return model.Source{}, "", fail.New("bad_database", "Turn on a database dump after detecting the site type, or enter the database name, user, and host.")
	}
	return model.Source{
		ID:     "database",
		Kind:   "database",
		Engine: "mysql",
		Method: "auto",
	}, site.Adapter, nil
}

func (a *App) applyDatabasePassword(db *model.Source, siteID string, in DatabaseInput) error {
	path := a.databaseSecretPath(siteID)
	password := in.Password
	if password != "" {
		if strings.Contains(password, "\n") {
			return fail.New("bad_database", "The database password cannot contain a newline.")
		}
		if err := store.WriteSecret(path, "DB_PASSWORD="+password+"\n"); err != nil {
			return err
		}
		db.PasswordSource = "saved"
		return nil
	}
	if in.PreferConfig {
		_ = os.Remove(path)
		if db.PasswordSource == "saved" || db.PasswordSource == "" {
			db.PasswordSource = "wp-config"
		}
		return nil
	}
	if credentialPresent(path) {
		db.PasswordSource = "saved"
	}
	return nil
}

func (a *App) databaseSecretPath(siteID string) string {
	return filepath.Join(a.Home, "secrets", siteID+"-database.env")
}

// DatabasePasswordLabel is "present", "missing", or "" for inspect/UI. Never the secret.
func (a *App) DatabasePasswordLabel(site model.Site) string {
	source := model.DatabaseSource(site)
	if source == nil {
		return ""
	}
	if source.PasswordSource == "saved" {
		return credentialLabel(a.databaseSecretPath(site.ID))
	}
	if source.Method == "sql_file" {
		return ""
	}
	if source.PasswordSource != "" {
		return "from " + source.PasswordSource
	}
	matched, err := addon.ByID(site.Adapter)
	if err != nil {
		return "missing"
	}
	if _, err := matched.ReadPassword(site.Root); err != nil {
		return "missing"
	}
	return "from site config"
}

func withoutDatabase(sources []model.Source) []model.Source {
	out := make([]model.Source, 0, len(sources))
	for _, source := range sources {
		if source.Kind != "database" {
			out = append(out, source)
		}
	}
	if len(out) == 0 {
		return []model.Source{{
			ID: "files", Kind: "files", Path: ".",
			Excludes: []string{"node_modules", "cache", ".git"},
		}}
	}
	return out
}

func withDatabase(sources []model.Source, db model.Source) []model.Source {
	out := withoutDatabase(sources)
	db.ID = "database"
	db.Kind = "database"
	return append(out, db)
}

// KeepSavedDatabasePassword marks a rediscovered database source as using the saved secret.
func (a *App) KeepSavedDatabasePassword(sources []model.Source, siteID string) []model.Source {
	if !credentialPresent(a.databaseSecretPath(siteID)) {
		return sources
	}
	for i := range sources {
		if sources[i].Kind == "database" {
			sources[i].PasswordSource = "saved"
		}
	}
	return sources
}
