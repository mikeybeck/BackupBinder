package app

import (
	"path/filepath"
	"strings"

	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

func siteEntry(site model.Site) SiteEntry {
	if site.Location.Kind == "sftp" {
		return SiteEntry{
			ID: site.ID, Name: site.Name, Root: site.Root, Adapter: site.Adapter,
			Location: "sftp",
			Note:     "Files are read from " + site.Location.User + "@" + site.Location.Host + ":" + site.Location.Path + ". A copy is kept at " + site.Root + ".",
		}
	}
	return SiteEntry{
		ID: site.ID, Name: site.Name, Root: site.Root, Adapter: site.Adapter,
		Location: "local_directory",
		Note:     localNameNote(site.Name, site.Root),
	}
}

func entryPtr(site model.Site) *SiteEntry {
	entry := siteEntry(site)
	return &entry
}

func localNameNote(name, root string) string {
	if !looksLikeHost(name) {
		return ""
	}
	return "The name is a label for a local directory. It is not the remote host " + name + ". The files are at " + root + "."
}

func looksLikeHost(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, ".") && !strings.ContainsAny(value, "/\\ ")
}

// ConfirmSite refuses to act unless id and root are the same registered site.
func (a *App) ConfirmSite(id, root string) (model.Site, error) {
	site, err := a.DB.GetSite(id)
	if err != nil {
		return model.Site{}, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return site, fail.New("wrong_site", "Pass --root "+site.Root+" for "+site.ID+" ("+site.Name+"). Do not substitute a similarly named site.")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return site, err
	}
	if filepath.Clean(abs) != filepath.Clean(site.Root) {
		return site, fail.New("wrong_site", "Refusing "+site.ID+" ("+site.Name+"). Its root is "+site.Root+", not "+root+".")
	}
	return site, nil
}

// AccessNeed tells the agent to ask the user how the site is reached.
type AccessNeed struct {
	AskUser bool           `json:"ask_user"`
	Reason  string         `json:"reason"`
	Choose  string         `json:"choose,omitempty"`
	Options []AccessOption `json:"options,omitempty"`
}

// AccessOption is one way to reach the site and the fields the user must supply.
type AccessOption struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields"`
	Ask    string   `json:"ask"`
}

// IdentifyResult is an exact match on id, name, or root. It does not guess.
type IdentifyResult struct {
	OK         bool        `json:"ok"`
	Query      string      `json:"query"`
	Matches    []SiteEntry `json:"matches"`
	Registered []SiteEntry `json:"registered,omitempty"`
	Warning    string      `json:"warning,omitempty"`
	Access     AccessNeed  `json:"access"`
}

// Identify returns only exact matches. A hostname is not matched to a similar local site.
func (a *App) Identify(query string) (IdentifyResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return IdentifyResult{}, fail.New("bad_id", "Pass --query.")
	}
	sites, err := a.DB.ListSites()
	if err != nil {
		return IdentifyResult{}, err
	}
	matches := make([]SiteEntry, 0)
	cleaned := filepath.Clean(query)
	absQuery := cleaned
	if abs, err := filepath.Abs(query); err == nil {
		absQuery = filepath.Clean(abs)
	}
	for _, site := range sites {
		root := filepath.Clean(site.Root)
		if site.ID == query || site.Name == query || root == cleaned || root == absQuery {
			matches = append(matches, siteEntry(site))
		}
	}
	result := IdentifyResult{OK: true, Query: query, Matches: matches}
	if len(matches) > 1 {
		options := make([]AccessOption, 0, len(matches))
		for _, match := range matches {
			options = append(options, AccessOption{
				Kind: match.ID, Fields: []string{"id"},
				Ask: "This is " + match.ID + " (" + match.Name + ") at " + match.Root + ". Call identify --query " + match.ID + " if the user names this site.",
			})
		}
		result.Warning = "More than one site matches. Ask which id. Do not back up until one id remains."
		result.Access = AccessNeed{
			AskUser: true,
			Reason:  "Ask which id. Do not ask how this machine reaches the site until one id remains.",
			Choose:  "Ask which id.",
			Options: options,
		}
		return result, nil
	}
	result.Access = accessFor(query, matches)
	switch {
	case len(matches) == 0:
		result.Registered = make([]SiteEntry, 0, len(sites))
		for _, site := range sites {
			result.Registered = append(result.Registered, siteEntry(site))
		}
		result.Warning = "No registered site has this exact id, name, or root. Do not back up a different site. None of these may be backed up until the user names that id and root."
	case looksLikeHost(query) && (len(matches) != 1 || matches[0].Location != "sftp"):
		result.Warning = "A name match is not a remote server. Read the root. If the user said the site is remote, do not back up this local directory."
	}
	return result, nil
}

// SetName changes the display name. The id stays the slug from registration.
func (a *App) SetName(siteID, name string) (model.Site, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "\n") {
		return model.Site{}, false, fail.New("bad_command", "Pass --name with the display name.")
	}
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Site{}, false, err
	}
	if name == site.Name {
		return site, true, nil
	}
	if err := a.DB.SaveName(site.ID, name); err != nil {
		return model.Site{}, false, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Site{}, false, err
	}
	site, err = a.DB.GetSite(site.ID)
	return site, false, err
}

func accessFor(query string, matches []SiteEntry) AccessNeed {
	if len(matches) == 1 && matches[0].Location == "sftp" {
		return AccessNeed{AskUser: false, Reason: "This site is read from its saved server. Back up only that server."}
	}
	if len(matches) == 1 && !looksLikeHost(query) {
		return AccessNeed{AskUser: false, Reason: "This match is a local directory. Back up only if the user confirmed this root."}
	}
	return AccessNeed{
		AskUser: true,
		Reason:  "Ask the user how this machine reaches the site. Do not invent values. Do not back up a different local directory.",
		Choose:  "Ask which one applies.",
		Options: []AccessOption{
			{Kind: "local_directory", Fields: []string{"path"}, Ask: "Ask for the absolute path of this site on this machine. Use it only if the user says these files are the site."},
			{Kind: "sftp", Fields: []string{"host", "user", "remote-path"}, Ask: "Ask for the SSH host, the user, and the absolute site root on that server. Do not ask for a password. This machine's SSH keys are used."},
		},
	}
}
