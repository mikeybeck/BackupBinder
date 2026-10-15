# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Anyone who wants to back up their files with absolute minimum hassle. They may use the localhost UI, the JSON CLI, or an agent over MCP; they are not expected to learn restic, retention jargon, or destination plumbing to get a trustworthy archive.

## Product Purpose

Local backup keeps a restic archive of a site on this machine. A site has sources, a snapshot, destinations, and a restore procedure. The product is that backup contract for any directory of files — not a WordPress host and not tied to any one CMS.

Success means both: the operator trusts they can restore, and an agent can set up and run backups without inventing hosts, paths, or secrets.

## Positioning

Same backup contract for humans and agents (JSON CLI and MCP share one catalog). Core stays thin (restic, SQLite registry, capture, retention, restore). Site types and destination kinds are declarative JSON extensions at the edges. Restore is deliberately boring and safe: only into an empty scratch directory, never the live site root or the repository.

## Operating Context

- Data under `var/` (or `--home` / `LOCAL_BACKUP_HOME`); secrets in `var/secrets/` mode `0600`
- Restic (and rclone for Google Drive) as separate binaries on PATH or under `.tools/`
- Schedules are cron fields on the site; system cron must call `run_scheduled`
- UI via `serve` on localhost only; agents are not expected to drive the web UI
- Site contract also lives in `var/sites/<id>.json`; passwords and keys are rejected from that file

## Capabilities and Constraints

- Default capture is files. If no addon matches the directory (or the operator chooses `--adapter files`), the contract is a file source only — no database, no CMS config.
- Site types are addons: JSON that names detect markers, excludes, optional database config/dump methods, and version rules. WordPress ships embedded as `internal/addon/wordpress.json`. Anyone can add or override an addon by placing JSON in `addons/` next to the module or the binary (same id replaces; new id adds). Destinations work the same way under `destinations/` / `internal/destination/*.json`.
- Addon-specific details (e.g. WordPress `wp-config.php`, `DB_PASSWORD`, `wp db export` / `mysqldump`) apply only when that addon matched. Future UI/docs must not imply every site has a PHP config or database password.
- When an addon defines a database, the dump password is read from the config file that addon names, or from a secret under `var/secrets/` when saved there; it is never stored on the site contract or printed. Remote dumps run on the server when the site is read over SFTP.
- Destinations: shipped kinds include SFTP, S3, B2, disk, gdrive; primary local restic repo is not an extension; a second destination is saved until enabled.
- Mutations require exact `identify` then matching `id` and `root`
- Never invent access fields; SFTP uses existing SSH keys (no SSH passwords)
- Never print database passwords, access keys, secret keys, or restic passwords
- Omit from agent/MCP unless designed later: arbitrary shell, raw prune/delete-repo, key export, downloading full archives into chat
- Undecided for design: whether marketing or docs surfaces exist beyond the product binary and README — none confirmed

## Evidence on Hand

- Product and agent contracts: `README.md`, `AGENTS.md`
- Runnable localhost UI: `internal/ui/ui.go` (embedded HTML)
- Shipped WordPress addon + fixture: `internal/addon/wordpress.json`, `fixtures/wordpress-site` (example only; not the product default)
- No testimonials, case studies, press, logos, or marketing claims on hand — future work must not fabricate them

## Product Principles

1. Minimum hassle for a trustworthy backup and restore — complexity stays in the engine, not on the operator.
2. One contract for humans and agents; never invent access or print secrets.
3. Thin core, declarative edges; WordPress is one shipped addon, not the product. Extensions must not silently skip files or the database.
4. Restore stays boring and safe: scratch only, refuse the live root and the repo.
5. Prefer a smaller interface and a correct restore path over more transports, dashboards, or modularity.
