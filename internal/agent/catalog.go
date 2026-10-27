// Package agent is the JSON catalog and command dispatch shipped with the binary.
package agent

import (
	"strings"
	"time"

	"backupbinder/internal/app"
	"backupbinder/internal/destination"
	"backupbinder/internal/discover"
	"backupbinder/internal/fail"
	"backupbinder/internal/model"
)

// Flag is one CLI flag. The same name is the MCP argument name.
type Flag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Placeholder string `json:"placeholder,omitempty"`
	Bool        bool   `json:"bool,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Command is one binary command.
type Command struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Flags   []Flag `json:"flags,omitempty"`
	Tool    bool   `json:"tool"`
}

// Catalog is the document an agent reads before calling setup.
type Catalog struct {
	OK           bool      `json:"ok"`
	Instructions string    `json:"instructions"`
	Commands     []Command `json:"commands"`
}

const instructions = "A new site is only setup until phase is ready. An existing site is identify, then inspect_site. Call set_*, add_destination, create_backup, or restore_backup only when the user asked for that change. Before backing up an existing site by name, call identify with the exact name the user used. If access.ask_user is true, ask the user how this machine reaches the site, then ask only for that option's fields. Do not back up a different local directory. If the user points at a directory or a server, call setup with that location, then ask only what next describes and call setup again. When next.options is set, ask which one applies and send exactly one option. Do not send every field from every option. --path is a directory on this machine. A server is --host, --user, and --remote-path. Do not invent host, user, path, or keys. Do not pass a directory and a server together. SFTP uses the SSH keys already on this machine. Do not ask for an SSH password. When next.accept and next.decline are set, pass one of those flags on the next command. --no-database, --remote none, --no-schedule, and --no-backup decline that step. Include --id and --root from the result whenever the site exists. Stop when phase is ready. A backup, schedule change, destination change, retention change, or database setting change also requires that site's id and root. If a tool returns phase needs_input, ask the user for each field in next.fields and call the tool again with their answers. Never print database passwords, access keys, or secret keys. Run doctor to see whether restic, rclone, rsync, and ssh are installed. doctor runs even when restic is missing. create_backup blocks until the backup finishes unless --detach is set; then poll inspect_site last_run.progress and last_run.ok. run_scheduled is the cron command and blocks until due sites finish. The schedule line is not installed for you. test_restore and restore_backup block until the restore finishes unless --detach is set; then poll inspect_site restore."

// Commands is the stable list. Tool is false for commands that do not return to the caller.
var Commands = []Command{
	{Name: "agent", Summary: "Print this catalog.", Tool: true},
	{Name: "doctor", Summary: "Report the data home, restic, rclone, rsync, ssh, and the MCP config. Runs even when restic is missing. Does not install anything.", Tool: true},
	{Name: "setup", Summary: "Walk through backup setup. --path is a directory on this machine. A server is --host, --user, and --remote-path. Each response says which decision is next. --remote-path is also the remote directory for a destination.", Tool: true, Flags: []Flag{
		{Name: "path", Placeholder: "DIR", Description: "site directory"},
		{Name: "name", Placeholder: "NAME", Description: "display name"},
		{Name: "id", Placeholder: "ID", Description: "site id"},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify; required before backup, schedule, or destination changes"},
		{Name: "backup", Bool: true, Description: "take a local backup now"},
		{Name: "schedule", Bool: true, Description: "enable the schedule"},
		{Name: "cron", Placeholder: "EXPR", Description: "five cron fields; default is 15 3 * * *"},
		{Name: "remote", Placeholder: "KIND", Description: "destination kind, or none to keep archives on this machine"},
		{Name: "enabled", Bool: true, Description: "upload to the remote on the next backup"},
		{Name: "database", Bool: true, Description: "include a database dump"},
		{Name: "no-database", Bool: true, Description: "store files only"},
		{Name: "db-name", Placeholder: "NAME", Description: "database name"},
		{Name: "db-user", Placeholder: "USER", Description: "database user"},
		{Name: "db-host", Placeholder: "HOST", Description: "database host"},
		{Name: "db-password", Placeholder: "SECRET", Description: "database password; saved under secrets; not printed back"},
		{Name: "no-schedule", Bool: true, Description: "leave the schedule off"},
		{Name: "no-backup", Bool: true, Description: "finish setup without a backup"},
		{Name: "host", Placeholder: "HOST", Description: "SFTP host, or destination host/remote name"},
		{Name: "user", Placeholder: "USER", Description: "SFTP user"},
		{Name: "remote-path", Placeholder: "PATH", Description: "SFTP path, disk path, or destination folder path"},
		{Name: "bucket", Placeholder: "BUCKET", Description: "bucket"},
		{Name: "prefix", Placeholder: "PREFIX", Description: "key prefix"},
		{Name: "endpoint", Placeholder: "HOST", Description: "S3 endpoint hostname"},
		{Name: "access-key", Placeholder: "KEY", Description: "S3 access key or B2 account id; not printed back"},
		{Name: "secret-key", Placeholder: "SECRET", Description: "S3 secret or B2 application key; not printed back"},
	}},
	{Name: "discover_site", Summary: "Propose a contract and write nothing.", Tool: true, Flags: []Flag{
		{Name: "path", Placeholder: "DIR", Description: "site directory", Required: true},
	}},
	{Name: "register_site", Summary: "Save a discovered contract and continue in setup until phase is ready.", Tool: true, Flags: []Flag{
		{Name: "path", Placeholder: "DIR", Description: "site directory", Required: true},
		{Name: "name", Placeholder: "NAME", Description: "display name"},
		{Name: "id", Placeholder: "ID", Description: "site id"},
	}},
	{Name: "list_sites", Summary: "List registered sites with id, name, root, and location.", Tool: true},
	{Name: "identify", Summary: "Exact match on id, name, or root. If access.ask_user is true, ask the user which access applies and then ask only for that option's fields. Do not invent values.", Tool: true, Flags: []Flag{
		{Name: "query", Placeholder: "NAME", Description: "exact site id, display name, or root", Required: true},
	}},
	{Name: "inspect_site", Summary: "Read one site, its last run, restore status, and the cron line. Check root before acting.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
	}},
	{Name: "set_name", Summary: "Change one saved setting. Set the display name for the site whose id and root both match. The id does not change.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "name", Placeholder: "NAME", Description: "display name", Required: true},
	}},
	{Name: "set_schedule", Summary: "Change one saved setting. Save a five-field cron schedule for the site whose id and root both match. Omit --cron or --enabled to leave that saved value unchanged.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "cron", Placeholder: "EXPR", Description: "five cron fields. Omit to leave the saved cron unchanged."},
		{Name: "enabled", Bool: true, Description: "run this schedule from run_scheduled. Omit to leave the saved value unchanged."},
	}},
	{Name: "set_location", Summary: "Change one saved setting. Choose the one place this site's files are read from. local reads --path on this machine. sftp copies one server into the local root before each backup. SSH keys already on this machine are used.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "kind", Placeholder: "local|sftp", Description: "where the files are read from", Required: true},
		{Name: "path", Placeholder: "DIR", Description: "local directory when kind is local"},
		{Name: "host", Placeholder: "HOST", Description: "SSH host when kind is sftp"},
		{Name: "user", Placeholder: "USER", Description: "SSH user when kind is sftp"},
		{Name: "remote-path", Placeholder: "PATH", Description: "absolute path on the server when kind is sftp"},
	}},
	{Name: "set_adapter", Summary: "Change one saved setting. Re-read the site directory and save the matching addon. Keeps the schedule, the server, and destinations. Omit --adapter to use the match. --adapter files stores a file source only.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "adapter", Placeholder: "NAME", Description: "addon id, or files. Omit to use the addon that matches the directory."},
	}},
	{Name: "set_database", Summary: "Change one saved setting. Choose whether each backup includes a database dump. Optionally set name, user, host, and a password saved under secrets. The password is never printed back.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "enabled", Bool: true, Description: "include a database dump. Omit to leave the saved choice. Pass false for files only."},
		{Name: "name", Placeholder: "NAME", Description: "database name"},
		{Name: "user", Placeholder: "USER", Description: "database user"},
		{Name: "host", Placeholder: "HOST", Description: "database host, host:port, or host:/socket"},
		{Name: "password", Placeholder: "SECRET", Description: "database password; saved under secrets; not printed back"},
		{Name: "prefer-config", Bool: true, Description: "clear a saved password and read the site config file instead"},
	}},
	{Name: "set_limits", Summary: "Change one saved setting. Cap restic repository traffic for the site whose id and root both match. Rates are KiB per second. 0 is unlimited. An omitted rate is left unchanged.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "limit-upload", Placeholder: "KIB", Description: "max upload rate in KiB/s; 0 is unlimited"},
		{Name: "limit-download", Placeholder: "KIB", Description: "max download rate in KiB/s; 0 is unlimited"},
	}},
	{Name: "set_retention", Summary: "Change one saved setting. Set how many snapshots to keep for the site whose id and root both match. 0 skips that rule. An omitted count is left unchanged. All zeros keeps every snapshot. Saved rules apply immediately, and again after every backup.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "keep-last", Placeholder: "N", Description: "newest snapshots to keep, at any time"},
		{Name: "keep-hourly", Placeholder: "N", Description: "hourly snapshots to keep"},
		{Name: "keep-daily", Placeholder: "N", Description: "daily snapshots to keep"},
		{Name: "keep-weekly", Placeholder: "N", Description: "weekly snapshots to keep"},
		{Name: "keep-monthly", Placeholder: "N", Description: "monthly snapshots to keep"},
		{Name: "keep-yearly", Placeholder: "N", Description: "yearly snapshots to keep"},
	}},
	{Name: "apply_retention", Summary: "Change one saved setting. Forget and prune snapshots outside the saved rules, on this machine and on enabled destinations.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
	}},
	{Name: "add_destination", Summary: "Change one saved setting. Save a destination for the site whose id and root both match. Rclone remote names are checked against local rclone config when set. Enabled destinations are probed when saved; disabled ones are not contacted until enabled.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "kind", Placeholder: "KIND", Description: "destination kind", Required: true},
		{Name: "enabled", Bool: true, Description: "upload on the next backup. Omit to save the destination disabled."},
		{Name: "host", Placeholder: "HOST", Description: "SFTP host, or destination host/remote name"},
		{Name: "user", Placeholder: "USER", Description: "SFTP user"},
		{Name: "path", Placeholder: "PATH", Description: "SFTP path, disk path, or destination folder path"},
		{Name: "remote-path", Placeholder: "PATH", Description: "Same folder as --path. Use either flag."},
		{Name: "bucket", Placeholder: "BUCKET", Description: "bucket"},
		{Name: "prefix", Placeholder: "PREFIX", Description: "key prefix"},
		{Name: "endpoint", Placeholder: "HOST", Description: "S3 endpoint hostname"},
		{Name: "access-key", Placeholder: "KEY", Description: "S3 access key or B2 account id; not printed back"},
		{Name: "secret-key", Placeholder: "SECRET", Description: "S3 secret or B2 application key; not printed back"},
	}},
	{Name: "set_destination", Summary: "Change one saved setting. Update a saved destination for the site whose id and root both match. Omitted fields stay as stored. The destination id does not change. Enabling probes reachability. The local restic repository cannot be edited or disabled.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "destination", Placeholder: "DEST", Description: "destination id from inspect_site", Required: true},
		{Name: "enabled", Bool: true, Description: "upload on the next backup. Omit to leave the saved value unchanged."},
		{Name: "host", Placeholder: "HOST", Description: "SFTP host, or destination host/remote name"},
		{Name: "user", Placeholder: "USER", Description: "SFTP user"},
		{Name: "path", Placeholder: "PATH", Description: "SFTP path, disk path, or destination folder path"},
		{Name: "remote-path", Placeholder: "PATH", Description: "Same folder as --path. Use either flag."},
		{Name: "bucket", Placeholder: "BUCKET", Description: "bucket"},
		{Name: "prefix", Placeholder: "PREFIX", Description: "key prefix"},
		{Name: "endpoint", Placeholder: "HOST", Description: "S3 endpoint hostname"},
		{Name: "access-key", Placeholder: "KEY", Description: "S3 access key or B2 account id; not printed back"},
		{Name: "secret-key", Placeholder: "SECRET", Description: "S3 secret or B2 application key; not printed back"},
	}},
	{Name: "remove_site", Summary: "Change one saved setting. Remove a site whose id and root both match. Drops the registry row and this site's secret files. Leaves the local restic repository in place.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
	}},
	{Name: "remove_destination", Summary: "Change one saved setting. Remove a saved destination for the site whose id and root both match. The local restic repository cannot be removed.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "destination", Placeholder: "DEST", Description: "destination id from inspect_site", Required: true},
	}},
	{Name: "create_backup", Summary: "Back up the site whose id and root both match. Refuses a different root. Without --detach the call blocks until the manifest is ready. With --detach, poll inspect_site last_run.progress and last_run.ok.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "detach", Bool: true, Description: "Return immediately with a run id. Poll inspect_site last_run.progress and last_run.ok."},
	}},
	{Name: "verify_backup", Summary: "Read the stored data and check that the snapshot file list matches the manifest.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "run", Placeholder: "RUN", Description: "run id"},
	}},
	{Name: "list_backups", Summary: "List backup runs.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
	}},
	{Name: "compare_backups", Summary: "Compare two runs.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "left", Placeholder: "RUN", Description: "run id", Required: true},
		{Name: "right", Placeholder: "RUN", Description: "run id", Required: true},
	}},
	{Name: "explain_failure", Summary: "Explain the latest failure, or one run.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "run", Placeholder: "RUN", Description: "run id"},
	}},
	{Name: "test_restore", Summary: "Restore into an empty scratch directory, compare it to the manifest, and record confidence. Requires the exact root. Omit --scratch to create a new empty directory. Without --detach the call blocks until the check finishes. With --detach, poll inspect_site restore.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "run", Placeholder: "RUN", Description: "run id"},
		{Name: "scratch", Placeholder: "DIR", Description: "empty directory outside the live site. Omit to create one under the data home."},
		{Name: "detach", Bool: true, Description: "Return immediately. Poll inspect_site restore.in_progress, restore.ok, restore.scratch, and restore.error."},
	}},
	{Name: "restore_backup", Summary: "Restore into an empty scratch directory. Requires the exact root. Omit --scratch to create a new empty directory. Without --detach the call blocks until the restore finishes. With --detach, poll inspect_site restore.", Tool: true, Flags: []Flag{
		{Name: "id", Placeholder: "ID", Description: "site id", Required: true},
		{Name: "root", Placeholder: "DIR", Description: "exact root from identify", Required: true},
		{Name: "run", Placeholder: "RUN", Description: "run id"},
		{Name: "scratch", Placeholder: "DIR", Description: "empty directory outside the live site. Omit to create one under the data home."},
		{Name: "detach", Bool: true, Description: "Return immediately. Poll inspect_site restore.in_progress, restore.ok, restore.scratch, and restore.error."},
	}},
	{Name: "run_scheduled", Summary: "Run sites whose schedule is due. A system cron calls this. The call blocks until every due site finishes. It does not install the cron line.", Tool: true},
	{Name: "serve", Summary: "Serve the localhost UI. This command does not return.", Flags: []Flag{
		{Name: "host", Placeholder: "127.0.0.1", Description: "loopback address"},
		{Name: "port", Placeholder: "47831", Description: "port"},
	}},
	{Name: "mcp", Summary: "Speak MCP over stdio so an agent can call these commands as tools."},
}

// CatalogDocument is the JSON from `backupbinder agent`.
func CatalogDocument() Catalog {
	ids := destination.IDs()
	commands := make([]Command, len(Commands))
	for i, cmd := range Commands {
		commands[i] = cmd
		if len(cmd.Flags) == 0 {
			continue
		}
		flags := make([]Flag, len(cmd.Flags))
		copy(flags, cmd.Flags)
		for j := range flags {
			switch {
			case flags[j].Name == "remote" && flags[j].Placeholder == "KIND":
				flags[j].Placeholder = "none|" + ids
			case flags[j].Name == "kind" && flags[j].Placeholder == "KIND":
				flags[j].Placeholder = ids
			}
			if note := destination.AppliesTo(flags[j].Name); note != "" {
				flags[j].Description += " " + note
			}
		}
		commands[i].Flags = flags
	}
	return Catalog{OK: true, Instructions: instructions, Commands: commands}
}

// Usage is the text printed when the command is missing.
func Usage() string {
	var b strings.Builder
	b.WriteString("backupbinder is a local site backup tool. Commands print JSON.\n\n")
	for _, cmd := range Commands {
		b.WriteString(cmd.usageLine())
		b.WriteByte('\n')
	}
	b.WriteString("\nData lives in ./var or --home / BACKUPBINDER_HOME.\n")
	b.WriteString("Run backupbinder agent for the machine-readable catalog.\n")
	return b.String()
}

func (c Command) usageLine() string {
	var b strings.Builder
	b.WriteString("  backupbinder ")
	b.WriteString(c.Name)
	for _, f := range c.Flags {
		b.WriteByte(' ')
		if !f.Required {
			b.WriteByte('[')
		}
		b.WriteString("--")
		b.WriteString(f.Name)
		if !f.Bool {
			b.WriteByte(' ')
			b.WriteString(f.Placeholder)
		}
		if !f.Required {
			b.WriteByte(']')
		}
	}
	return b.String()
}

// Lookup returns the command description.
func Lookup(name string) (Command, bool) {
	for _, cmd := range Commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return Command{}, false
}

// Tools are the commands an MCP client can call.
func Tools() []Command {
	var out []Command
	for _, cmd := range Commands {
		if cmd.Tool {
			out = append(out, cmd)
		}
	}
	return out
}

// Execute runs one command. args keys are flag names.
func Execute(application *app.App, name string, args map[string]any) (any, error) {
	cmd, ok := Lookup(name)
	if !ok || name == "mcp" || name == "serve" {
		return nil, fail.New("bad_command", "Unknown command. Run backupbinder agent.")
	}
	if application != nil {
		if err := application.ReloadSites(); err != nil {
			return nil, err
		}
	}
	for _, f := range cmd.Flags {
		if f.Required && !f.Bool && strings.TrimSpace(asString(args, f.Name)) == "" {
			if f.Name == "id" {
				return nil, fail.New("bad_id", "Pass --id.")
			}
			return nil, fail.New("bad_command", "Pass --"+f.Name+".")
		}
	}
	switch name {
	case "agent":
		return CatalogDocument(), nil
	case "doctor":
		home := ""
		if application != nil {
			home = application.Home
		}
		return app.Doctor(home), nil
	case "discover_site":
		return discover.Site(asString(args, "path"), "", "")
	case "register_site":
		id := asString(args, "id")
		return application.Setup(app.SetupInput{Path: asString(args, "path"), Name: asString(args, "name"), ID: id})
	case "list_sites":
		return application.ListSites()
	case "identify":
		return application.Identify(asString(args, "query"))
	case "inspect_site":
		return application.Inspect(asString(args, "id"))
	case "set_name":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		site, unchanged, err := application.SetName(asString(args, "id"), asString(args, "name"))
		if err != nil {
			return nil, err
		}
		out := map[string]any{"ok": true, "id": site.ID, "name": site.Name, "root": site.Root}
		if unchanged {
			out["unchanged"] = true
		}
		return out, nil
	case "set_schedule":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		schedule, err := application.SetSchedule(asString(args, "id"), asString(args, "cron"), asBoolPtr(args, "enabled"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "schedule": schedule}, nil
	case "set_location":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		kind := asString(args, "kind")
		loc := model.Location{Kind: kind}
		if kind == "sftp" {
			loc.Host = asString(args, "host")
			loc.User = asString(args, "user")
			loc.Path = asString(args, "remote-path")
		} else {
			loc.Path = asString(args, "path")
		}
		site, err := application.SetLocation(asString(args, "id"), loc)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "location": site.Location, "root": site.Root}, nil
	case "set_adapter":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		site, err := application.SetAdapter(asString(args, "id"), asString(args, "adapter"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "adapter": site.Adapter, "sources": site.Sources}, nil
	case "set_database":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		site, err := application.SetDatabase(asString(args, "id"), app.DatabaseInput{
			Enabled:      asBoolPtr(args, "enabled"),
			Name:         asString(args, "name"),
			User:         asString(args, "user"),
			Host:         asString(args, "host"),
			Password:     asString(args, "password"),
			PreferConfig: asBool(args, "prefer-config"),
		})
		if err != nil {
			return nil, err
		}
		db := model.DatabaseSource(site)
		out := map[string]any{"ok": true, "enabled": db != nil, "sources": site.Sources}
		if db != nil {
			out["database"] = map[string]any{
				"name": db.Name, "user": db.User, "host": db.Host, "method": db.Method,
				"password": application.DatabasePasswordLabel(site),
			}
		}
		return out, nil
	case "set_limits":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		upload, uploadSet, err := app.ParseKiB(asString(args, "limit-upload"))
		if err != nil {
			return nil, err
		}
		download, downloadSet, err := app.ParseKiB(asString(args, "limit-download"))
		if err != nil {
			return nil, err
		}
		if !uploadSet && !downloadSet {
			return nil, fail.New("bad_limit", "Pass --limit-upload or --limit-download, in KiB per second. Use 0 for unlimited.")
		}
		var uploadPtr, downloadPtr *int
		if uploadSet {
			uploadPtr = &upload
		}
		if downloadSet {
			downloadPtr = &download
		}
		limits, err := application.SetLimits(asString(args, "id"), uploadPtr, downloadPtr)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "limits": limits}, nil
	case "set_retention":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		site, err := application.DB.GetSite(asString(args, "id"))
		if err != nil {
			return nil, err
		}
		policy, set, err := retentionPolicy(site.Retention, args)
		if err != nil {
			return nil, err
		}
		if !set {
			return nil, fail.New("bad_retention", "Pass a keep count. Use 0 to skip that rule. Set every count to 0 to keep every snapshot.")
		}
		return application.SetRetention(site.ID, policy)
	case "apply_retention":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		return application.ApplyRetention(asString(args, "id"))
	case "add_destination":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		path := asString(args, "path")
		if path == "" {
			path = asString(args, "remote-path")
		}
		if asString(args, "path") != "" && asString(args, "remote-path") != "" && asString(args, "path") != asString(args, "remote-path") {
			return nil, fail.New("bad_destination", "Pass --path or --remote-path, not two different folders.")
		}
		dest, err := application.AddDestination(asString(args, "id"), model.Destination{
			Kind: asString(args, "kind"), Enabled: asBool(args, "enabled"),
			Host: asString(args, "host"), User: asString(args, "user"), Path: path,
			Bucket: asString(args, "bucket"), Prefix: asString(args, "prefix"), Endpoint: asString(args, "endpoint"),
		}, asString(args, "access-key"), asString(args, "secret-key"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "destination": dest}, nil
	case "set_destination":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		patch, changed, err := destinationPatch(args)
		if err != nil {
			return nil, err
		}
		if !changed {
			site, err := application.DB.GetSite(asString(args, "id"))
			if err != nil {
				return nil, err
			}
			for _, dest := range site.Destinations {
				if dest.ID == asString(args, "destination") {
					return map[string]any{"ok": true, "destination": dest, "unchanged": true, "note": "Pass a field to change, or --enabled true or --enabled false. Omitted values were left as stored."}, nil
				}
			}
			return nil, fail.New("bad_destination", "Destination "+asString(args, "destination")+" was not found on this site.")
		}
		dest, err := application.SetDestination(asString(args, "id"), asString(args, "destination"), patch)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "destination": dest}, nil
	case "remove_site":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		repo, err := application.RemoveSite(asString(args, "id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ok": true, "removed": asString(args, "id"), "id": asString(args, "id"), "repo": repo,
			"note": "The local restic repository was left at " + repo + ". A later site with this id uses that repository.",
		}, nil
	case "remove_destination":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		if err := application.RemoveDestination(asString(args, "id"), asString(args, "destination")); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "removed": asString(args, "destination")}, nil
	case "create_backup":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		if asBool(args, "detach") {
			runID, err := application.StartBackupDetached(asString(args, "id"))
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"ok": true, "run_id": runID, "in_progress": true,
				"next": "Poll inspect_site --id " + asString(args, "id") + ". Read last_run.progress while the backup runs and last_run.ok when it finishes.",
			}, nil
		}
		return application.CreateBackup(asString(args, "id"))
	case "verify_backup":
		return application.Verify(asString(args, "id"), asString(args, "run"))
	case "list_backups":
		return application.ListBackups(asString(args, "id"))
	case "compare_backups":
		return application.Compare(asString(args, "id"), asString(args, "left"), asString(args, "right"))
	case "explain_failure":
		return application.Explain(asString(args, "id"), asString(args, "run"))
	case "test_restore":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		if asBool(args, "detach") {
			return detachRestore(application, "test_restore", args)
		}
		return application.TestRestore(asString(args, "id"), asString(args, "run"), asString(args, "scratch"))
	case "restore_backup":
		if _, err := application.ConfirmSite(asString(args, "id"), asString(args, "root")); err != nil {
			return nil, err
		}
		if asBool(args, "detach") {
			return detachRestore(application, "restore_backup", args)
		}
		return application.RestoreBackup(asString(args, "id"), asString(args, "run"), asString(args, "scratch"))
	case "run_scheduled":
		return application.RunScheduled(time.Time{})
	case "setup":
		return application.Setup(app.SetupInput{
			Path: asString(args, "path"), Name: asString(args, "name"), ID: asString(args, "id"),
			Root:       asString(args, "root"),
			Backup:     asBool(args, "backup"),
			NoBackup:   asBool(args, "no-backup"),
			Schedule:   asBool(args, "schedule"),
			NoSchedule: asBool(args, "no-schedule"),
			Cron:       asString(args, "cron"),
			Database:   asBool(args, "database"),
			NoDatabase: asBool(args, "no-database"),
			DBName:     asString(args, "db-name"),
			DBUser:     asString(args, "db-user"),
			DBHost:     asString(args, "db-host"),
			DBPassword: asString(args, "db-password"),
			Remote:     asString(args, "remote"),
			Enabled:    asBool(args, "enabled"),
			EnabledSet: argSet(args, "enabled"),
			Host:       asString(args, "host"),
			User:       asString(args, "user"),
			RemotePath: asString(args, "remote-path"),
			Bucket:     asString(args, "bucket"),
			Prefix:     asString(args, "prefix"),
			Endpoint:   asString(args, "endpoint"),
			AccessKey:  asString(args, "access-key"),
			SecretKey:  asString(args, "secret-key"),
		})
	default:
		return nil, fail.New("bad_command", "Unknown command. Run backupbinder agent.")
	}
}

func destinationPatch(args map[string]any) (app.DestinationPatch, bool, error) {
	if argSet(args, "path") && argSet(args, "remote-path") && asString(args, "path") != asString(args, "remote-path") {
		return app.DestinationPatch{}, false, fail.New("bad_destination", "Pass --path or --remote-path, not two different folders.")
	}
	patch := app.DestinationPatch{Enabled: asBoolPtr(args, "enabled")}
	changed := patch.Enabled != nil
	assign := func(flag string, dest **string) {
		if !argSet(args, flag) {
			return
		}
		value := asString(args, flag)
		*dest = &value
		changed = true
	}
	if argSet(args, "path") {
		assign("path", &patch.Path)
	} else {
		assign("remote-path", &patch.Path)
	}
	assign("host", &patch.Host)
	assign("user", &patch.User)
	assign("bucket", &patch.Bucket)
	assign("prefix", &patch.Prefix)
	assign("endpoint", &patch.Endpoint)
	if argSet(args, "access-key") || argSet(args, "secret-key") {
		patch.KeysSet = true
		patch.AccessKey = asString(args, "access-key")
		patch.SecretKey = asString(args, "secret-key")
		changed = true
	}
	return patch, changed, nil
}

func detachRestore(application *app.App, command string, args map[string]any) (any, error) {
	if err := application.StartRestoreDetached(asString(args, "id"), command, asString(args, "run"), asString(args, "scratch")); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "in_progress": true,
		"next": "Poll inspect_site --id " + asString(args, "id") + ". Read restore.in_progress while it runs, then restore.ok, restore.scratch, and restore.error.",
	}, nil
}

func retentionPolicy(current model.Retention, args map[string]any) (model.Retention, bool, error) {
	set := false
	apply := func(dest *int, name string) error {
		n, ok, err := app.ParseCount(asString(args, name))
		if err != nil || !ok {
			return err
		}
		*dest = n
		set = true
		return nil
	}
	if err := apply(&current.KeepLast, "keep-last"); err != nil {
		return model.Retention{}, false, err
	}
	if err := apply(&current.KeepHourly, "keep-hourly"); err != nil {
		return model.Retention{}, false, err
	}
	if err := apply(&current.KeepDaily, "keep-daily"); err != nil {
		return model.Retention{}, false, err
	}
	if err := apply(&current.KeepWeekly, "keep-weekly"); err != nil {
		return model.Retention{}, false, err
	}
	if err := apply(&current.KeepMonthly, "keep-monthly"); err != nil {
		return model.Retention{}, false, err
	}
	if err := apply(&current.KeepYearly, "keep-yearly"); err != nil {
		return model.Retention{}, false, err
	}
	return current, set, nil
}
