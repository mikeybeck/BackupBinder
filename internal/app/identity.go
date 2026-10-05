package app

import (
	"path/filepath"
	"strings"

	"local-backup/internal/fail"
	"local-backup/internal/model"
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
	OK      bool        `json:"ok"`
	Query   string      `json:"query"`
	Matches []SiteEntry `json:"matches"`
	Warning string      `json:"warning,omitempty"`
	Access  AccessNeed  `json:"access"`
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
	for _, site := range sites {
		if site.ID == query || site.Name == query || filepath.Clean(site.Root) == cleaned {
			matches = append(matches, siteEntry(site))
		}
	}
	result := IdentifyResult{OK: true, Query: query, Matches: matches, Access: accessFor(query, matches)}
	switch {
	case len(matches) == 0:
		result.Warning = "No registered site has this exact id, name, or root. Do not back up a different site."
	case looksLikeHost(query) && (len(matches) != 1 || matches[0].Location != "sftp"):
		result.Warning = "A name match is not a remote server. Read the root. If the user said the site is remote, do not back up this local directory."
	}
	return result, nil
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
