# Agent architecture

BackupBinder is built so agents can **operate** it and **extend** it. The product is the backup contract (sources, snapshot, destinations, restore), not a WordPress host. WordPress is one shipped addon.

Read this before changing the CLI, MCP layer, addons, destinations, or restore path.

## Goals

1. **Agents operate the product** — same commands as humans, structured JSON, no scraping docs or inventing hosts/paths/keys.
2. **Agents extend the product** — new site types and destination kinds as declarative JSON against a small, stable schema, not tribal Go imports.
3. **Core stays thin and trustworthy** — restic archive, SQLite registry, capture, retention, restore. Variability lives at the edges.

## Shape of the system

```
Agent → MCP (stdio) / JSON CLI → app / agent catalog → addons + destinations → restic
```

- CLI and MCP expose the **same** command catalog (`internal/agent`). Do not add an MCP-only tool or a CLI-only mutation path for the same job.
- Every command prints JSON. Failures use `{"ok": false, "error": {"code": "...", "message": "..."}}`.
- `agent` prints instructions + commands. MCP `initialize` ships those instructions. Keep them short and safety-first.
- UI (`serve`) is secondary. Agents are not expected to drive the web UI.

## Where modularity belongs

| Edge | Mechanism | Examples |
|------|-----------|----------|
| Site type (source) | `internal/addon/*.json` + optional `addons/` override dir | WordPress detect, excludes, dump commands, version rules |
| Destination kind | `internal/destination/*.json` + optional `destinations/` override dir | SFTP, S3, B2, disk, gdrive — fields, URL templates, credential env, few named Go hooks |
| Engine | Go packages | Capture, archive, retention, restore, store — not per-vendor forks |

Prefer **declarative JSON** over “run this plugin binary/Python.” Treat new Go hooks as rare and reviewed. Do not give extensions arbitrary shell or process privileges.

Freeze and version schemas carefully. Agents copy golden examples; if core silently ignores invented fields, backups will look fine and skip work.

## Operating contract (do not weaken)

- Before mutating a site: `identify` with the **exact** name/id/root the user used. Ambiguous or wrong root → stop.
- Backup, schedule, destination, retention, and database changes require matching **id and root**.
- If `access.ask_user` or `phase: needs_input`, ask the user; do not invent host, user, path, or keys.
- SFTP uses SSH keys already on the machine. Do not ask for SSH passwords.
- Never print database passwords, access keys, secret keys, or restic passwords in tool/CLI output.
- Restore only into an empty scratch directory. Refuse the live site root, parents/children of it, and the restic repo.
- `run_scheduled` must call the same path as `create_backup` with no agent prompts mid-run.
- Omit from agent/MCP surface unless explicitly designed later: arbitrary shell, raw prune/delete-repo, key export, downloading full archives into chat.

## MCP

- First-class transport today: **stdio** (`backupbinder mcp`), launched by the host next to the engine.
- MCP is a thin adapter over the catalog. It must not know Borg vs S3 vs Postgres; it calls the app layer.
- Long work may return progress via existing app mechanisms; do not block the protocol on hour-long silence without a clear run/status model.
- Remote Streamable HTTP, OAuth, resources, prompts, and live subscriptions are **optional later**. Do not expand the protocol surface until the backup/restore contract is solid and there is a real remote daemon need.

## Extension guidance for agents building features

When adding a site type or destination:

1. Copy a shipped JSON example (`wordpress.json` or an existing destination kind).
2. Keep the schema: id, labels/fields with patterns, templates/commands core already understands.
3. Add or document Go hooks only when JSON cannot express a hard safety or URL requirement.
4. Add tests that prove capture/restore (or destination URL + credential mapping) for the new kind.
5. Update the agent catalog flags/descriptions if new setup fields are required — agents and MCP share those names.

## Risks to keep in mind

- A plugin/addon that “succeeds” while silently skipping files or the database.
- Privilege creep (extensions that can read or write anything).
- API churn that breaks agent-generated JSON.
- Over-engineering transports and dashboards before restore is boring and reliable.

## Practical priority

1. Keep core + a few official addons/destinations correct.
2. Keep the v1 addon and destination contracts small and documented by example.
3. Keep MCP = CLI catalog on stdio.
4. Only then: remote MCP, richer resources/prompts, or a plugin-authoring helper.

The hard part is not modularity. It is a small interface and a restore path that stays boring.
