package app

import (
	"path/filepath"
	"strings"

	"local-backup/internal/discover"
	"local-backup/internal/fail"
	"local-backup/internal/model"
)

const defaultCron = "15 3 * * *"

// SetupInput is one agent turn. Backup, schedule, and remote upload run only when asked.
type SetupInput struct {
	Path       string
	Name       string
	ID         string
	Root       string
	Backup     bool
	Schedule   bool
	Cron       string
	Remote     string
	Enabled    bool
	Host       string
	User       string
	RemotePath string
	Bucket     string
	Prefix     string
	Endpoint   string
	AccessKey  string
	SecretKey  string
}

// SetupResult is the next step for an agent. Secrets are not included.
type SetupResult struct {
	OK                 bool            `json:"ok"`
	SiteID             string          `json:"site_id,omitempty"`
	Phase              string          `json:"phase"`
	Site               *SiteEntry      `json:"site,omitempty"`
	Backup             *SetupBackup    `json:"backup,omitempty"`
	Schedule           *model.Schedule `json:"schedule,omitempty"`
	ScheduleInvocation string          `json:"schedule_invocation,omitempty"`
	Destinations       []SetupDest     `json:"destinations,omitempty"`
	Next               *SetupNext      `json:"next"`
}

type SetupBackup struct {
	OK    bool   `json:"ok"`
	RunID string `json:"run_id,omitempty"`
	Error string `json:"error,omitempty"`
}

type SetupDest struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Enabled     bool   `json:"enabled"`
	Credentials string `json:"credentials,omitempty"`
}

type SetupNext struct {
	Command string   `json:"command"`
	Kind    string   `json:"kind,omitempty"`
	Fields  []string `json:"fields,omitempty"`
	Cron    string   `json:"cron,omitempty"`
	Confirm bool     `json:"confirm,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// Setup registers a site when given a new path, then reports the next step.
// A backup, a schedule, and a remote destination run only when the input asks for them.
func (a *App) Setup(in SetupInput) (SetupResult, error) {
	if in.Path == "" && in.ID == "" {
		return SetupResult{}, fail.New("bad_id", "Pass --path or --id.")
	}
	site, err := a.setupSite(in)
	if err != nil {
		if fe, ok := err.(*fail.Error); ok && fe.Code == "ambiguous_site" {
			return SetupResult{
				OK: true, Phase: "needs_input",
				Next: &SetupNext{Command: "setup", Fields: []string{"path"}, Reason: fe.Message},
			}, nil
		}
		return SetupResult{}, err
	}
	if in.Backup || in.Schedule || in.Remote != "" {
		if _, err := a.ConfirmSite(site.ID, in.Root); err != nil {
			return SetupResult{}, err
		}
	}
	if in.Backup {
		if _, err := a.CreateBackup(site.ID); err != nil {
			return SetupResult{}, err
		}
	}
	if in.Schedule {
		cron := in.Cron
		if cron == "" {
			cron = defaultCron
		}
		if _, err := a.SetSchedule(site.ID, cron, true); err != nil {
			return SetupResult{}, err
		}
	}
	site, err = a.DB.GetSite(site.ID)
	if err != nil {
		return SetupResult{}, err
	}
	if in.Remote != "" {
		missing := remoteMissing(in)
		if len(missing) > 0 {
			result := a.setupView(site)
			result.Phase = "needs_input"
			result.Next = &SetupNext{Command: "add_destination", Kind: in.Remote, Fields: missing, Reason: remoteReason(in.Remote)}
			return result, nil
		}
		if _, err := a.AddDestination(site.ID, model.Destination{
			Kind: in.Remote, Enabled: in.Enabled, Host: in.Host, User: in.User, Path: in.RemotePath,
			Bucket: in.Bucket, Prefix: in.Prefix, Endpoint: in.Endpoint,
		}, in.AccessKey, in.SecretKey); err != nil {
			return SetupResult{}, err
		}
		site, err = a.DB.GetSite(site.ID)
		if err != nil {
			return SetupResult{}, err
		}
	}
	return a.setupNext(site), nil
}

func (a *App) setupSite(in SetupInput) (model.Site, error) {
	if in.Path == "" {
		return a.DB.GetSite(in.ID)
	}
	discovered, err := discover.Site(in.Path, in.Name, in.ID)
	if err != nil {
		return model.Site{}, err
	}
	name, id, explicit := in.Name, in.ID, in.ID != ""
	root := discovered.Site.Root
	if !explicit && discovered.Site.ID == "public" && strings.HasSuffix(filepath.ToSlash(root), "/app/public") {
		folder := filepath.Base(filepath.Dir(filepath.Dir(root)))
		if name == "" {
			name = folder
		}
		again, err := discover.Site(in.Path, name, "")
		if err != nil {
			return model.Site{}, err
		}
		name = again.Site.Name
		id = again.Site.ID
		explicit = true
		root = again.Site.Root
	}
	if existing, ok, err := a.siteByRoot(root); err != nil {
		return model.Site{}, err
	} else if ok {
		return existing, nil
	}
	result, err := a.Register(in.Path, name, id, explicit)
	if err != nil {
		return model.Site{}, err
	}
	return result.Site, nil
}

func (a *App) siteByRoot(root string) (model.Site, bool, error) {
	sites, err := a.DB.ListSites()
	if err != nil {
		return model.Site{}, false, err
	}
	root = filepath.Clean(root)
	for _, site := range sites {
		if filepath.Clean(site.Root) == root {
			return site, true, nil
		}
	}
	return model.Site{}, false, nil
}

func (a *App) setupView(site model.Site) SetupResult {
	a.decorate(&site)
	backup := &SetupBackup{}
	if sum := a.summary(site.ID); sum != nil {
		backup.OK = sum.OK
		backup.RunID = sum.RunID
		backup.Error = sum.Error
	}
	dests := make([]SetupDest, 0, len(site.Destinations))
	for _, dest := range site.Destinations {
		dests = append(dests, SetupDest{ID: dest.ID, Kind: dest.Kind, Enabled: dest.Enabled, Credentials: dest.Credentials})
	}
	schedule := site.Schedule
	return SetupResult{
		OK: true, SiteID: site.ID, Site: entryPtr(site),
		Backup: backup, Schedule: &schedule, ScheduleInvocation: a.scheduleLine(), Destinations: dests,
	}
}

func (a *App) setupNext(site model.Site) SetupResult {
	result := a.setupView(site)
	if result.Backup == nil || !result.Backup.OK {
		reason := ""
		if result.Backup != nil {
			reason = result.Backup.Error
		}
		result.Phase = "confirm"
		result.Next = &SetupNext{Command: "create_backup", Confirm: true, Reason: reason}
		return result
	}
	if !site.Schedule.Enabled {
		result.Phase = "confirm"
		result.Next = &SetupNext{Command: "set_schedule", Cron: defaultCron, Confirm: true}
		return result
	}
	result.Phase = "ready"
	return result
}

func remoteMissing(in SetupInput) []string {
	var missing []string
	switch in.Remote {
	case "sftp":
		if in.Host == "" {
			missing = append(missing, "host")
		}
		if in.User == "" {
			missing = append(missing, "user")
		}
		if in.RemotePath == "" {
			missing = append(missing, "remote-path")
		}
	case "s3", "b2":
		if in.Bucket == "" {
			missing = append(missing, "bucket")
		}
		if in.AccessKey == "" {
			missing = append(missing, "access-key")
		}
		if in.SecretKey == "" {
			missing = append(missing, "secret-key")
		}
	case "disk":
		if in.RemotePath == "" {
			missing = append(missing, "remote-path")
		}
	default:
		missing = append(missing, "kind")
	}
	return missing
}

func remoteReason(kind string) string {
	const ask = "Ask the user for each missing field. Do not invent a value. "
	switch kind {
	case "sftp":
		return ask + "SFTP uses the SSH keys already on this machine. Do not ask for a password. Fields are host, user, and remote-path."
	case "s3", "b2":
		return ask + "Fields are bucket, access-key, and secret-key. The keys are stored and are not printed back."
	case "disk":
		return ask + "Field is remote-path. It cannot be the live site or the restic repository."
	default:
		return ask + "Ask whether the destination is sftp, s3, b2, or disk."
	}
}
