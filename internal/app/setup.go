package app

import (
	"os"
	"path/filepath"
	"strings"

	"backupbinder/internal/destination"
	"backupbinder/internal/discover"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

const defaultCron = "15 3 * * *"

var setupOrder = []string{"location", "name", "database", "destination", "schedule", "backup"}

// SetupInput is one agent or wizard turn. Backup, schedule, and remote upload run only when asked.
type SetupInput struct {
	Path       string
	Name       string
	ID         string
	Root       string
	Step       string
	Backup     bool
	NoBackup   bool
	Schedule   bool
	NoSchedule bool
	Cron       string
	Database   bool
	NoDatabase bool
	DBName     string
	DBUser     string
	DBHost     string
	DBPassword string
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
	// EnabledSet is true when the caller passed --enabled. An omitted flag must not
	// be treated as "turn this off".
	EnabledSet bool
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
	Setup              []string        `json:"setup,omitempty"`
	Config             string          `json:"config,omitempty"`
	Reused             bool            `json:"reused,omitempty"`
	Note               string          `json:"note,omitempty"`
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
	Step    string        `json:"step,omitempty"`
	Command string        `json:"command"`
	Kind    string        `json:"kind,omitempty"`
	Fields  []string      `json:"fields,omitempty"`
	Accept  string        `json:"accept,omitempty"`
	Decline string        `json:"decline,omitempty"`
	Options []SetupOption `json:"options,omitempty"`
	Cron    string        `json:"cron,omitempty"`
	Confirm bool          `json:"confirm,omitempty"`
	Reason  string        `json:"reason,omitempty"`
}

// SetupOption is one location choice. The caller sends exactly one option's fields.
type SetupOption struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields"`
	Ask    string   `json:"ask"`
}

// Setup registers a site when given a new path or server, then reports the next open decision.
// A backup, a schedule, and a remote destination run only when the input asks for them.
func (a *App) Setup(in SetupInput) (SetupResult, error) {
	in = normalizeSetup(in)
	if early, ok := setupEarly(in); ok {
		return early, nil
	}
	site, created, err := a.resolveSetup(in)
	if err != nil {
		if fe, ok := err.(*fail.Error); ok && fe.Code == "ambiguous_site" {
			return SetupResult{
				OK: true, Phase: "needs_input",
				Next: &SetupNext{Step: "location", Command: "setup", Fields: []string{"path"}, Reason: fe.Message},
			}, nil
		}
		return SetupResult{}, err
	}
	if created {
		site.Setup = append([]string{}, setupOrder...)
		if err := a.DB.SaveSetup(site.ID, site.Setup); err != nil {
			return SetupResult{}, err
		}
		if err := a.saveSiteFile(site.ID); err != nil {
			return SetupResult{}, err
		}
	}
	if err := a.confirmSetup(site, in, created); err != nil {
		return SetupResult{}, err
	}
	if !created {
		if err := rejectClosed(site, in); err != nil {
			return SetupResult{}, err
		}
	}
	before := append([]string{}, site.Setup...)
	missing, err := a.applySetup(&site, in, created)
	if err != nil {
		return SetupResult{}, err
	}
	if created || !sameSteps(before, site.Setup) {
		if err := a.DB.SaveSetup(site.ID, site.Setup); err != nil {
			return SetupResult{}, err
		}
		if err := a.saveSiteFile(site.ID); err != nil {
			return SetupResult{}, err
		}
	}
	site, err = a.DB.GetSite(site.ID)
	if err != nil {
		return SetupResult{}, err
	}
	if missing != nil {
		result := a.setupView(site)
		result.Phase = "needs_input"
		result.Next = missing
		result.Setup = site.Setup
		result.Config = a.siteFile(site.ID)
		return noteReuse(result, site, in, created), nil
	}
	return noteReuse(a.setupNext(site), site, in, created), nil
}

func normalizeSetup(in SetupInput) SetupInput {
	in.Path = strings.TrimSpace(in.Path)
	in.Name = strings.TrimSpace(in.Name)
	in.ID = strings.TrimSpace(in.ID)
	in.Root = strings.TrimSpace(in.Root)
	in.Step = strings.TrimSpace(in.Step)
	in.Cron = strings.TrimSpace(in.Cron)
	in.DBName = strings.TrimSpace(in.DBName)
	in.DBUser = strings.TrimSpace(in.DBUser)
	in.DBHost = strings.TrimSpace(in.DBHost)
	in.Remote = strings.TrimSpace(in.Remote)
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.RemotePath = strings.TrimSpace(in.RemotePath)
	in.Bucket = strings.TrimSpace(in.Bucket)
	in.Prefix = strings.TrimSpace(in.Prefix)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	return in
}

func setupEarly(in SetupInput) (SetupResult, bool) {
	if in.ID != "" {
		return SetupResult{}, false
	}
	if in.Path != "" && sftpReady(in) && in.Remote == "" {
		return SetupResult{
			OK: true, Phase: "needs_input",
			Next: &SetupNext{
				Step: "location", Command: "setup", Options: locationOptions(),
				Reason: "Pass a directory or a server, not both. Send exactly one option.",
			},
		}, true
	}
	if in.Path == "" && in.Remote == "" && partialSFTP(in) {
		return SetupResult{
			OK: true, Phase: "needs_input",
			Next: &SetupNext{
				Step: "location", Command: "setup", Fields: missingSFTP(in),
				Reason: "A server needs a host, a user, and an absolute path. SSH keys already on this machine are used. Do not ask for a password.",
			},
		}, true
	}
	return SetupResult{}, false
}

func (a *App) resolveSetup(in SetupInput) (model.Site, bool, error) {
	if in.ID != "" {
		site, err := a.DB.GetSite(in.ID)
		if err == nil {
			return site, false, nil
		}
		if in.Path == "" && !(sftpReady(in) && in.Remote == "") {
			return model.Site{}, false, err
		}
	}
	if in.Path != "" {
		return a.registerLocal(in)
	}
	if sftpReady(in) && in.Remote == "" {
		return a.registerRemote(in)
	}
	return model.Site{}, false, fail.New("bad_id", "Pass --path or --id.")
}

func (a *App) registerLocal(in SetupInput) (model.Site, bool, error) {
	discovered, err := discover.Site(in.Path, in.Name, in.ID)
	if err != nil {
		return model.Site{}, false, err
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
			return model.Site{}, false, err
		}
		name = again.Site.Name
		id = again.Site.ID
		explicit = true
		root = again.Site.Root
	}
	if existing, ok, err := a.siteByRoot(root); err != nil {
		return model.Site{}, false, err
	} else if ok {
		return existing, false, nil
	}
	result, err := a.Register(in.Path, name, id, explicit)
	if err != nil {
		return model.Site{}, false, err
	}
	return result.Site, true, nil
}

func (a *App) registerRemote(in SetupInput) (model.Site, bool, error) {
	if !hostPattern.MatchString(in.Host) {
		return model.Site{}, false, fail.New("bad_location", "SSH host must be a hostname, not a shell command.")
	}
	if !userPattern.MatchString(in.User) {
		return model.Site{}, false, fail.New("bad_location", "SSH user must be a plain username.")
	}
	if !remotePath.MatchString(in.RemotePath) {
		return model.Site{}, false, fail.New("bad_location", "The server path must be an absolute path without spaces.")
	}
	name := in.Name
	if name == "" {
		name = in.Host
	}
	id := in.ID
	explicit := id != ""
	if id == "" {
		id = siteSlug(name)
	}
	if !validID(id) {
		return model.Site{}, false, fail.New("bad_id", "Site id must be lowercase letters, numbers, and hyphens.")
	}
	if !explicit {
		next, err := a.uniqueSiteID(id)
		if err != nil {
			return model.Site{}, false, err
		}
		id = next
	}
	root := filepath.Join(a.Home, "copies", id)
	if existing, ok, err := a.siteByRoot(root); err != nil {
		return model.Site{}, false, err
	} else if ok {
		return existing, false, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return model.Site{}, false, err
	}
	site := model.Site{
		ID: id, Name: name, Root: root, Adapter: "files",
		Sources: []model.Source{{
			ID: "files", Kind: "files", Path: ".",
			Excludes: []string{"node_modules", "cache", ".git"},
		}},
		Destinations: []model.Destination{{ID: "local", Kind: "local_restic", Enabled: true}},
		Retention:    model.DefaultRetention(),
		Location:     model.Location{Kind: "sftp", Host: in.Host, User: in.User, Path: in.RemotePath},
		Policy:       model.Policy{NeverOverwriteLive: true, RestoreTarget: "scratch_only"},
	}
	if err := a.DB.CreateSite(site); err != nil {
		return model.Site{}, false, err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return model.Site{}, false, err
	}
	return site, true, nil
}

func (a *App) confirmSetup(site model.Site, in SetupInput, created bool) error {
	editingLocation := !created && (in.Step == "location" || (hasStep(site, "location") && (in.Path != "" || (in.Remote == "" && sftpReady(in)))))
	if !editingLocation && !in.Backup && !in.Schedule && !in.NoSchedule && !in.Database && !in.NoDatabase && in.Remote == "" {
		return nil
	}
	_, err := a.ConfirmSite(site.ID, in.Root)
	return err
}

func (a *App) applySetup(site *model.Site, in SetupInput, created bool) (*SetupNext, error) {
	if err := a.settleLocation(site, in, created); err != nil {
		return nil, err
	}
	if err := a.settleName(site, in, created); err != nil {
		return nil, err
	}
	if next, err := a.settleDatabase(site, in); err != nil || next != nil {
		return next, err
	}
	var missing *SetupNext
	if next, err := a.settleDestination(site, in); err != nil {
		return nil, err
	} else if next != nil {
		missing = next
	}
	if err := a.settleSchedule(site, in); err != nil {
		return nil, err
	}
	if err := a.settleBackup(site, in); err != nil {
		return nil, err
	}
	return missing, nil
}

func (a *App) settleLocation(site *model.Site, in SetupInput, created bool) error {
	if in.Remote != "" {
		if created && in.Path != "" {
			site.Setup = dropStep(site.Setup, "location")
		}
		return nil
	}
	if in.Step == "location" && in.Path == "" && !sftpReady(in) {
		return fail.New("bad_location", "Choose a directory on this machine, or a host, user, and absolute path.")
	}
	if in.Path == "" && !sftpReady(in) {
		return nil
	}
	if !created && in.Step != "location" && !hasStep(*site, "location") {
		return nil
	}
	if in.Path != "" && sftpReady(in) {
		return fail.New("bad_location", "Pass a directory or a server, not both.")
	}
	if created {
		site.Setup = dropStep(site.Setup, "location")
		return nil
	}
	var loc model.Location
	if in.Path != "" {
		loc = model.Location{Kind: "local", Path: in.Path}
	} else {
		loc = model.Location{Kind: "sftp", Host: in.Host, User: in.User, Path: in.RemotePath}
	}
	pending := dropStep(site.Setup, "location")
	saved, err := a.SetLocation(site.ID, loc)
	if err != nil {
		return err
	}
	saved.Setup = pending
	*site = saved
	return nil
}

func (a *App) settleName(site *model.Site, in SetupInput, created bool) error {
	if in.Step == "name" && in.Name == "" {
		return fail.New("bad_config", "The site needs a name.")
	}
	if in.Name == "" {
		return nil
	}
	if !created && in.Step != "name" && !hasStep(*site, "name") {
		return nil
	}
	pending := dropStep(site.Setup, "name")
	if in.Name == site.Name {
		site.Setup = pending
		return nil
	}
	if err := a.DB.SaveName(site.ID, in.Name); err != nil {
		return err
	}
	if err := a.saveSiteFile(site.ID); err != nil {
		return err
	}
	saved, err := a.DB.GetSite(site.ID)
	if err != nil {
		return err
	}
	saved.Setup = pending
	*site = saved
	return nil
}

func (a *App) settleDatabase(site *model.Site, in SetupInput) (*SetupNext, error) {
	if !in.Database && !in.NoDatabase {
		return nil, nil
	}
	if in.Database && in.NoDatabase {
		return nil, fail.New("bad_database", "Pass --database or --no-database, not both.")
	}
	if in.Step != "database" && !hasStep(*site, "database") {
		return nil, nil
	}
	pending := dropStep(site.Setup, "database")
	if in.NoDatabase {
		saved, err := a.SetDatabase(site.ID, DatabaseInput{Enabled: boolPtr(false)})
		if err != nil {
			return nil, err
		}
		saved.Setup = pending
		*site = saved
		return nil, nil
	}
	name, user, host := in.DBName, in.DBUser, in.DBHost
	if db := model.DatabaseSource(*site); db != nil {
		if name == "" {
			name = db.Name
		}
		if user == "" {
			user = db.User
		}
		if host == "" {
			host = db.Host
		}
	}
	if name == "" || user == "" || host == "" {
		fields := make([]string, 0, 3)
		if name == "" {
			fields = append(fields, "db-name")
		}
		if user == "" {
			fields = append(fields, "db-user")
		}
		if host == "" {
			fields = append(fields, "db-host")
		}
		return withSite(*site, SetupNext{
			Step: "database", Command: "setup", Fields: fields,
			Reason: "A database dump needs a name, user, and host. Leave the password blank when the site config already has one. Do not print the password.",
		}), nil
	}
	saved, err := a.SetDatabase(site.ID, DatabaseInput{
		Enabled: boolPtr(true), Name: in.DBName, User: in.DBUser, Host: in.DBHost, Password: in.DBPassword,
	})
	if err != nil {
		return nil, err
	}
	saved.Setup = pending
	*site = saved
	return nil, nil
}

func (a *App) settleDestination(site *model.Site, in SetupInput) (*SetupNext, error) {
	if in.Remote == "" {
		return nil, nil
	}
	if in.Step != "destination" && !hasStep(*site, "destination") {
		return nil, nil
	}
	if in.Remote == "none" {
		site.Setup = dropStep(site.Setup, "destination")
		return nil, nil
	}
	if missing := remoteMissing(in); len(missing) > 0 {
		next := SetupNext{Step: "destination", Command: "setup", Fields: missing, Reason: remoteReason(in.Remote)}
		if _, err := destination.ByID(in.Remote); err == nil {
			next.Kind = in.Remote
		}
		return withSite(*site, next), nil
	}
	pending := dropStep(site.Setup, "destination")
	if _, err := a.AddDestination(site.ID, model.Destination{
		Kind: in.Remote, Enabled: in.Enabled, Host: in.Host, User: in.User, Path: in.RemotePath,
		Bucket: in.Bucket, Prefix: in.Prefix, Endpoint: in.Endpoint,
	}, in.AccessKey, in.SecretKey); err != nil {
		return nil, err
	}
	saved, err := a.DB.GetSite(site.ID)
	if err != nil {
		return nil, err
	}
	saved.Setup = pending
	*site = saved
	return nil, nil
}

func (a *App) settleSchedule(site *model.Site, in SetupInput) error {
	if !in.Schedule && !in.NoSchedule {
		return nil
	}
	if in.Schedule && in.NoSchedule {
		return fail.New("bad_cron", "Pass --schedule or --no-schedule, not both.")
	}
	if in.Step != "schedule" && !hasStep(*site, "schedule") {
		return nil
	}
	cron := in.Cron
	if in.Schedule && cron == "" {
		cron = defaultCron
	}
	if in.NoSchedule && cron == "" {
		cron = site.Schedule.Cron
	}
	pending := dropStep(site.Setup, "schedule")
	enabled := in.Schedule
	if _, err := a.SetSchedule(site.ID, cron, &enabled); err != nil {
		return err
	}
	saved, err := a.DB.GetSite(site.ID)
	if err != nil {
		return err
	}
	saved.Setup = pending
	*site = saved
	return nil
}

func (a *App) settleBackup(site *model.Site, in SetupInput) error {
	if !in.Backup && !in.NoBackup {
		return nil
	}
	if in.Backup && in.NoBackup {
		return fail.New("bad_command", "Pass --backup or --no-backup, not both.")
	}
	if in.Step != "backup" && !hasStep(*site, "backup") {
		return nil
	}
	if in.Backup {
		if _, err := a.CreateBackup(site.ID); err != nil {
			return err
		}
	}
	site.Setup = dropStep(site.Setup, "backup")
	return nil
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
		Setup: site.Setup, Config: a.siteFile(site.ID),
	}
}

func (a *App) setupNext(site model.Site) SetupResult {
	result := a.setupView(site)
	switch nextStep(site.Setup) {
	case "":
		result.Phase = "ready"
	case "location":
		result.Phase = "needs_input"
		result.Next = withSite(site, SetupNext{
			Step: "location", Command: "setup", Options: locationOptions(),
			Reason: "Send exactly one option. Pass --path for a directory on this machine, or --host, --user, and --remote-path for a server. Do not invent values.",
		})
	case "name":
		result.Phase = "confirm"
		result.Next = withSite(site, SetupNext{
			Step: "name", Command: "setup", Fields: []string{"name"}, Accept: "name", Confirm: true,
			Reason: "The proposed name is " + site.Name + ". Pass --name " + site.Name + " to keep it, or --name with a different name to change it.",
		})
	case "database":
		result.Phase = "confirm"
		result.Next = withSite(site, SetupNext{
			Step: "database", Command: "setup", Fields: []string{"database"}, Accept: "database", Decline: "no-database", Confirm: true,
			Reason: databasePrompt(a, site),
		})
	case "destination":
		result.Phase = "confirm"
		result.Next = withSite(site, SetupNext{
			Step: "destination", Command: "setup", Fields: []string{"remote"}, Confirm: true,
			Reason: destinationConfirmReason(site),
		})
	case "schedule":
		result.Phase = "confirm"
		result.Next = withSite(site, SetupNext{
			Step: "schedule", Command: "setup", Fields: []string{"schedule"}, Accept: "schedule", Decline: "no-schedule", Cron: defaultCron, Confirm: true,
			Reason: "Pass --schedule to enable " + defaultCron + ", or --no-schedule to leave the schedule off. A system cron must run this every minute. It is not installed: " + a.scheduleLine() + ".",
		})
	case "backup":
		result.Phase = "confirm"
		result.Next = withSite(site, SetupNext{
			Step: "backup", Command: "setup", Fields: []string{"backup"}, Accept: "backup", Decline: "no-backup", Confirm: true,
			Reason: "Pass --backup to take the first backup, or --no-backup to finish without one.",
		})
	}
	return result
}

func withSite(site model.Site, next SetupNext) *SetupNext {
	if site.ID == "" {
		return &next
	}
	fields := []string{"id", "root"}
	for _, field := range next.Fields {
		if field != "id" && field != "root" {
			fields = append(fields, field)
		}
	}
	next.Fields = fields
	if next.Reason != "" && !strings.Contains(next.Reason, "--root "+site.Root) {
		next.Reason = strings.TrimSpace(next.Reason) + " Include --id " + site.ID + " and --root " + site.Root + "."
	}
	return &next
}

func locationOptions() []SetupOption {
	return []SetupOption{
		{Kind: "local_directory", Fields: []string{"path"}, Ask: "Ask for the directory on this machine. Send this option only."},
		{Kind: "sftp", Fields: []string{"host", "user", "remote-path"}, Ask: "Ask for the SSH host, the user, and the absolute path on the server. Do not ask for a password. Send this option only."},
	}
}

func destinationConfirmReason(site model.Site) string {
	reason := "Finished archives stay on this machine unless you add a destination. Pass --remote none to keep them here, or --remote with one of " + strings.ReplaceAll(destination.IDs(), "|", ", ") + ". Upload happens only when you also pass --enabled."
	for _, dest := range site.Destinations {
		if dest.Kind == "local_restic" {
			continue
		}
		if dest.Enabled {
			reason += " " + dest.ID + " is saved and enabled."
			continue
		}
		reason += " " + dest.ID + " is saved but disabled. Call set_destination --id " + site.ID + " --root " + site.Root + " --destination " + dest.ID + " --enabled true to upload."
	}
	return reason
}

func noteReuse(result SetupResult, site model.Site, in SetupInput, created bool) SetupResult {
	if created || in.Path == "" || site.Root == "" {
		return result
	}
	if filepath.Clean(in.Path) != filepath.Clean(site.Root) {
		return result
	}
	result.Reused = true
	if in.ID != "" && in.ID != site.ID {
		result.Note = "The requested id " + in.ID + " was not registered. This directory is already " + site.ID + " at " + site.Root + "."
	}
	return result
}

func rejectClosed(site model.Site, in SetupInput) error {
	if in.Step != "" {
		return nil
	}
	id, root := site.ID, site.Root
	if !hasStep(site, "location") && locationChange(site, in) {
		return fail.New("bad_command", "The location is already saved. Call set_location --id "+id+" --root "+root+" --kind local --path DIR, or --kind sftp --host HOST --user USER --remote-path PATH.")
	}
	if !hasStep(site, "name") && in.Name != "" && in.Name != site.Name {
		return fail.New("bad_command", "The name is already "+site.Name+". Call set_name --id "+id+" --root "+root+" --name NAME. The id stays "+id+".")
	}
	if !hasStep(site, "database") && databaseChange(in) {
		return fail.New("bad_command", "The database setting is already saved. Call set_database --id "+id+" --root "+root+" --enabled true --name NAME --user USER --host HOST, or --enabled false for files only. Do not print the password.")
	}
	if !hasStep(site, "destination") && in.Remote != "" {
		return fail.New("bad_command", "The destination step is already closed. Call add_destination --id "+id+" --root "+root+" --kind "+in.Remote+".")
	}
	if !hasStep(site, "destination") && in.EnabledSet && in.Remote == "" {
		return fail.New("bad_command", "Upload is changed with set_destination. Call inspect_site --id "+id+", then set_destination --id "+id+" --root "+root+" --destination DEST --enabled true.")
	}
	if !hasStep(site, "schedule") && (in.Schedule || in.NoSchedule || in.Cron != "") {
		return fail.New("bad_command", "The schedule is already saved. Call set_schedule --id "+id+" --root "+root+" --cron EXPR --enabled true, or --enabled false to turn it off.")
	}
	if !hasStep(site, "backup") && (in.Backup || in.NoBackup) {
		return fail.New("bad_command", "Setup already finished the backup step. Call create_backup --id "+id+" --root "+root+" to take another backup.")
	}
	return nil
}

func locationChange(site model.Site, in SetupInput) bool {
	if in.Remote != "" {
		return false
	}
	if in.Host != "" || in.User != "" || in.RemotePath != "" {
		return true
	}
	if in.Path == "" {
		return false
	}
	return filepath.Clean(in.Path) != filepath.Clean(site.Root)
}

func databaseChange(in SetupInput) bool {
	return in.Database || in.NoDatabase || in.DBName != "" || in.DBUser != "" || in.DBHost != "" || in.DBPassword != ""
}

func databasePrompt(a *App, site model.Site) string {
	db := model.DatabaseSource(site)
	if db == nil && site.Location.Kind == "sftp" {
		return "No database was detected yet. Pass --database with --db-name, --db-user, and --db-host to include a dump, or --no-database for files only. A server copy is read on the first backup."
	}
	if db == nil {
		return "No database was detected. Pass --database with --db-name, --db-user, and --db-host to include a dump, or --no-database for files only."
	}
	text := "A database dump is planned for " + db.Name + " as " + db.User + " at " + db.Host + "."
	if label := a.DatabasePasswordLabel(site); label != "" {
		text += " Password: " + label + "."
	}
	return text + " Pass --database to keep it, or --no-database for files only."
}

func remoteMissing(in SetupInput) []string {
	kind, err := destination.ByID(in.Remote)
	if err != nil {
		return []string{"remote"}
	}
	return kind.SetupMissing(destination.SetupFields{
		Host: in.Host, User: in.User, RemotePath: in.RemotePath,
		Bucket: in.Bucket, Prefix: in.Prefix, Endpoint: in.Endpoint,
		AccessKey: in.AccessKey, SecretKey: in.SecretKey,
	})
}

func remoteReason(kindID string) string {
	kind, err := destination.ByID(kindID)
	if err != nil {
		return "Ask the user for each missing field. Do not invent a value. Ask whether the destination is " + strings.ReplaceAll(destination.IDs(), "|", ", ") + "."
	}
	return kind.SetupReason()
}

func (a *App) uniqueSiteID(base string) (string, error) {
	id := base
	for n := 2; ; n++ {
		_, err := a.DB.GetSite(id)
		if err != nil {
			if fe, ok := err.(*fail.Error); ok && fe.Code == "not_found" {
				return id, nil
			}
			return "", err
		}
		id = base + "-" + itoa(n)
		if !validID(id) {
			return "", fail.New("bad_id", "Could not choose a site id.")
		}
	}
}

func sftpReady(in SetupInput) bool {
	return in.Host != "" && in.User != "" && in.RemotePath != ""
}

func partialSFTP(in SetupInput) bool {
	n := 0
	if in.Host != "" {
		n++
	}
	if in.User != "" {
		n++
	}
	if in.RemotePath != "" {
		n++
	}
	return n > 0 && n < 3
}

func missingSFTP(in SetupInput) []string {
	var fields []string
	if in.Host == "" {
		fields = append(fields, "host")
	}
	if in.User == "" {
		fields = append(fields, "user")
	}
	if in.RemotePath == "" {
		fields = append(fields, "remote-path")
	}
	return fields
}

func hasStep(site model.Site, step string) bool {
	for _, item := range site.Setup {
		if item == step {
			return true
		}
	}
	return false
}

func dropStep(steps []string, step string) []string {
	out := make([]string, 0, len(steps))
	for _, item := range steps {
		if item != step {
			out = append(out, item)
		}
	}
	return out
}

// NextStep is the first guided decision still open.
func NextStep(steps []string) string {
	return nextStep(steps)
}

func nextStep(steps []string) string {
	open := map[string]bool{}
	for _, step := range steps {
		open[step] = true
	}
	for _, step := range setupOrder {
		if open[step] {
			return step
		}
	}
	return ""
}

func sameSteps(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func siteSlug(name string) string {
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
