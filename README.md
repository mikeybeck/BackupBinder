# BackupBinder

BackupBinder keeps a restic archive of a site on this machine. A site has sources, a snapshot, destinations, and a restore procedure. Site types are addons. WordPress ships as `internal/addon/wordpress.json`. The product is the backup contract, not a WordPress host. The current binary is still named `local-backup`.

Marketing site (static): open [`site/index.html`](site/index.html), or https://backupbinder.pages.dev. Deploy notes in [`site/README.md`](site/README.md).

Database passwords are never stored in the site contract or registry. When an addon defines a database, the password is read from the config file that addon names (WordPress: `wp-config.php`), or from a secret under `var/secrets/` when you save one there. Files-only sites have no database password. The registry is SQLite. Restic is a separate binary. Destination kinds are JSON extensions (shipped under `internal/destination/`; override or add via `destinations/` next to the module or binary). A second destination is saved until you enable it. With nothing enabled, the run never leaves this computer. Google Drive uses restic's rclone backend: put rclone on `PATH` or in `.tools/rclone`, create a remote with `rclone config` (it must appear in `rclone listremotes`), then pass that remote name and a folder path. Saving an enabled Google Drive destination, or starting a backup that uses one, checks the remote before any local snapshot work.

## Run

From this directory:

```sh
go build -o bin/local-backup ./cmd/local-backup
```

Data goes in `var/` (gitignored), or pass `--home` / `LOCAL_BACKUP_HOME`. Restic is `.tools/restic` when that file exists, otherwise `restic` on `PATH` or `LOCAL_BACKUP_RESTIC`. Rclone (for Google Drive) is `.tools/rclone`, `PATH`, or `LOCAL_BACKUP_RCLONE`.

```sh
./bin/local-backup agent
./bin/local-backup setup --path fixtures/wordpress-site
./bin/local-backup setup --host files.example --user backup --remote-path /var/www/site --name Example
./bin/local-backup discover_site --path fixtures/wordpress-site
./bin/local-backup register_site --path fixtures/wordpress-site --name "Fixture WordPress" --id fixture-wordpress
./bin/local-backup list_sites
./bin/local-backup inspect_site --id fixture-wordpress
./bin/local-backup set_schedule --id fixture-wordpress --cron "15 3 * * *" --enabled
./bin/local-backup set_location --id fixture-wordpress --root fixtures/wordpress-site --kind sftp --host files.example --user backup --remote-path /var/www/site
./bin/local-backup set_limits --id fixture-wordpress --root fixtures/wordpress-site --limit-upload 1024 --limit-download 1024
./bin/local-backup set_retention --id fixture-wordpress --root fixtures/wordpress-site --keep-daily 14 --keep-weekly 8
./bin/local-backup apply_retention --id fixture-wordpress --root fixtures/wordpress-site
./bin/local-backup add_destination --id fixture-wordpress --kind sftp --host files.example --user backup --path /repos/site
./bin/local-backup add_destination --id fixture-wordpress --root fixtures/wordpress-site --kind gdrive --host gdrive --path local-backup/fixture-wordpress
./bin/local-backup set_destination --id fixture-wordpress --root fixtures/wordpress-site --destination gdrive-1 --enabled
./bin/local-backup remove_destination --id fixture-wordpress --root fixtures/wordpress-site --destination gdrive-1
./bin/local-backup identify --query "Fixture WordPress"
./bin/local-backup create_backup --id fixture-wordpress --root fixtures/wordpress-site
./bin/local-backup verify_backup --id fixture-wordpress
./bin/local-backup list_backups --id fixture-wordpress
./bin/local-backup compare_backups --id fixture-wordpress --left RUN --right RUN
./bin/local-backup explain_failure --id fixture-wordpress
./bin/local-backup test_restore --id fixture-wordpress
./bin/local-backup restore_backup --id fixture-wordpress --scratch /tmp/empty-scratch
./bin/local-backup serve --host 127.0.0.1 --port 47831
./bin/local-backup mcp
```

Every command prints JSON. Failures look like `{"ok": false, "error": {"code": "...", "message": "..."}}`.

## Setup

Point an agent at a directory or a server and it walks the decisions that change a backup. `setup --path DIR` starts from a directory on this machine. `setup --host HOST --user USER --remote-path PATH` starts from a server and keeps the local copy under `var/copies/`. Each response has `phase` and `next`. Ask only what `next` describes, then call `setup` again with the answer and that site's `--id` and `--root`. `--no-database`, `--remote none`, `--no-schedule`, and `--no-backup` decline that step. Stop when `phase` is `ready`.

The same steps are in the browser. `serve`, then open the UI and choose **Set up a backup**. Each page has every field for that decision. Back returns to the previous page with the saved values. Retention and speed limits stay on the site page.

The browser and the agent write `var/sites/<id>.json`. Edit that file to change the contract by hand. A newer file replaces the registry row the next time the program opens, and when `serve` or `mcp` handles a request. Passwords, access keys, and secret keys are rejected in that file. They stay in `var/secrets/`.

## Agents

`agent` prints the command catalog. `identify --query NAME` matches only an exact id, name, or root. When `access.ask_user` is true, ask the user how this machine reaches the site and then ask only for that option's fields. A hostname is not a local directory. `create_backup`, `set_schedule`, `add_destination`, `set_database`, `test_restore`, and `restore_backup` refuse to run unless `--root` is that site's root. `setup` registers a new site, or returns the site that already uses that directory. A backup, a schedule, and a remote upload wait until the caller asks, and those actions also require `--root`. Access keys and secret keys are not printed back.

`mcp` speaks MCP on stdio. A Cursor user can add:

```json
{
  "mcpServers": {
    "local-backup": {
      "command": "/path/to/local-backup",
      "args": ["--home", "/path/to/var", "mcp"]
    }
  }
}
```

The tools are the same commands. `serve` is not a tool because it does not return.

The UI is at [http://127.0.0.1:47831/](http://127.0.0.1:47831/). It binds to localhost only. Set up a backup from a directory or a server, then read the last run, destinations, and restore confidence. The site page shows the path of `var/sites/<id>.json`. While a backup is running, the page shows the current step, the file being handled, and restic's percent complete. While the archive is saving, the bar follows bytes written into the repository.

## What a run stores

Each successful run writes `var/manifests/<site>/<run>.json` and a row in `var/registry.sqlite`. The manifest names the site, the files (and database, when captured), versions when they can be read, the size, the time, where the snapshot was written, the restore commands, and restore confidence (`never`, or the time a scratch restore last passed). Runs that fall outside the retention rules are deleted from the registry and from the restic repository.

Capture is files, and optionally a database dump when the site's addon (or a manual database setting) includes one. The WordPress addon tries `wp db export`, then `mysqldump`. When the site is read from a server, that dump runs on the server and is copied back. `localhost` in the server's config is the database on that server. The fixture site in `fixtures/wordpress-site` uses its `database.sql` because it is not a live server. Cache, `node_modules`, and similar junk are left out.

`set_adapter` reads the site directory again and saves the matching addon on an existing site. The schedule, the server, and destinations stay. `--adapter files` stores a file source only.

Another JSON file in `addons/` next to this module, or next to the binary, overrides an addon with the same id or adds a site type. The file names the marker files, the excludes, how to read versions, and which dump commands to run. A database password stays in the config file that addon names, or under `var/secrets/` when saved there — never in the site JSON.

Destination kinds work the same way under `destinations/`: each JSON file names the kind id, form fields, restic URL template, optional credential env vars, required helper tools, and Go hooks (`disk_safety`, `mkdir_before_copy`). The primary local restic repository is not an extension.

`test_restore` and `restore_backup` extract into an empty scratch directory. They refuse the live site root, anything inside it, a parent of it, and the restic repository.

## Schedules

A schedule is five cron fields stored on the site. It does not run by itself. Point system cron at the binary:

```sh
* * * * * /path/to/local-backup --home /path/to/var run_scheduled
```

`run_scheduled` calls the same `create_backup` path. Nothing in that path asks an agent what to do.

## Where the files are

Each site is read from one place. The site page chooses a directory on this machine, or one SFTP server. Saving a server replaces the previous server. The next backup copies that server into the local directory, then archives that copy. SSH uses the keys already on this machine. A destination is a separate choice: it is where a finished archive is copied, and it is not the server the files are read from. For `gdrive`, `--host` is the rclone remote name and `--path` is the folder in Drive.

## Speed limits

A site can cap restic repository traffic. Rates are KiB per second, matching restic `--limit-upload` and `--limit-download`. `0` or a blank field means that direction stays unlimited. The cap applies to backup, copies to other destinations, checks, and restores, including a scheduled run. Set it with `set_limits` or on the site page.

## Retention

Each successful run is a restic snapshot. Unchanged file data is stored once. A site keeps the newest 3 snapshots, 24 hourly, 14 daily, 8 weekly, 12 monthly, and 2 yearly until you change that. A snapshot stays if any of those rules keeps it. After every backup, snapshots outside the rules are forgotten and the freed space is pruned, in the local repository and on each destination that just received a copy. `apply_retention` does the same for every enabled destination.

`0` skips that one rule. Set every count to `0` to keep every snapshot. An omitted `set_retention` flag leaves that count unchanged. Saving the rules applies them immediately when a repository already exists. The same form is on the site page.

## Keys

`var/secrets/` holds the restic password and any destination credentials, mode `0600`. Do not commit `var/` or `.tools/`. The fixture password in `fixtures/wordpress-site/wp-config.php` is fake.

## License

Local backup is free software under the [GNU General Public License v3.0 or later](LICENSE).
