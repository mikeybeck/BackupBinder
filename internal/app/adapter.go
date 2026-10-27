package app

import (
	"os"
	"strings"

	"backupbinder/internal/discover"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// SetAdapter re-reads the site directory and saves the matching addon.
// The schedule, server, destinations, limits, and retention stay.
// want is an addon id, "files", or empty to keep the match.
func (a *App) SetAdapter(siteID, want string) (model.Site, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Site{}, err
	}
	discovered, err := discover.Site(site.Root, site.Name, site.ID)
	if err != nil {
		return model.Site{}, err
	}
	adapter := discovered.Site.Adapter
	sources := discovered.Site.Sources
	want = strings.TrimSpace(want)
	if want == "files" {
		adapter = "files"
		sources = []model.Source{{
			ID: "files", Kind: "files", Path: ".", Method: "files_only",
			Excludes: []string{"node_modules", "cache", ".git"},
		}}
		_ = os.Remove(a.databaseSecretPath(site.ID))
	} else if want != "" && want != adapter {
		return model.Site{}, fail.New("bad_adapter", "That directory matches "+adapter+", not "+want+".")
	} else {
		// Keep a manually configured dump when rediscovery finds files only
		// (for example Laravel .env on an SFTP copy).
		if existing := model.DatabaseSource(site); existing != nil && model.DatabaseSource(model.Site{Sources: sources}) == nil {
			sources = withDatabase(sources, *existing)
		}
		sources = a.KeepSavedDatabasePassword(sources, site.ID)
	}
	if err := a.DB.SaveAdapter(site.ID, adapter, sources); err != nil {
		return model.Site{}, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Site{}, err
	}
	return a.DB.GetSite(site.ID)
}

// adoptCopiedSite re-reads a server copy when the site is still the files adapter.
// An explicit files-only choice keeps the database out. An ambiguous tree names set_adapter.
func (a *App) adoptCopiedSite(site model.Site) (model.Site, error) {
	if site.Adapter != "files" {
		return site, nil
	}
	declined := databaseDeclined(site)
	updated, err := a.SetAdapter(site.ID, "")
	if err != nil {
		if fe, ok := err.(*fail.Error); ok && fe.Code == "ambiguous_site" {
			return model.Site{}, fail.New("ambiguous_site", fe.Message+" Call set_adapter --id "+site.ID+" --root "+site.Root+" after choosing one site. Do not guess.")
		}
		return model.Site{}, err
	}
	if declined && model.DatabaseSource(updated) != nil {
		updated, err = a.SetDatabase(site.ID, DatabaseInput{Enabled: boolPtr(false)})
		if err != nil {
			return model.Site{}, err
		}
	}
	return updated, nil
}
