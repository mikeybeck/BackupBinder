package app

import (
	"os"
	"strings"

	"local-backup/internal/discover"
	"local-backup/internal/fail"
	"local-backup/internal/model"
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
			ID: "files", Kind: "files", Path: ".",
			Excludes: []string{"node_modules", "cache", ".git"},
		}}
		_ = os.Remove(a.databaseSecretPath(site.ID))
	} else if want != "" && want != adapter {
		return model.Site{}, fail.New("bad_adapter", "That directory matches "+adapter+", not "+want+".")
	} else {
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
