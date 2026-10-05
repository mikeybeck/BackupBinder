// Package store is the SQLite registry. Secrets stay in mode-0600 files, not in rows.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"local-backup/internal/fail"
	"local-backup/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	Home string
}

func Open(home string) (*Store, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return nil, err
	}
	for _, dir := range []string{"secrets", "restic", "manifests", "runs", "cache", "scratch", "staging"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return nil, err
		}
	}
	dbPath := filepath.Join(home, "registry.sqlite")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dbPath)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, Home: home}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(dbPath, 0o600)
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS sites (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root TEXT NOT NULL,
  adapter TEXT NOT NULL,
  sources_json TEXT NOT NULL,
  schedule_cron TEXT,
  schedule_enabled INTEGER NOT NULL DEFAULT 0,
  schedule_last_slot TEXT,
  restore_verified_at TEXT,
  restore_run_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS destinations (
  site_id TEXT NOT NULL,
  id TEXT NOT NULL,
  kind TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  config_json TEXT NOT NULL,
  PRIMARY KEY (site_id, id),
  FOREIGN KEY (site_id) REFERENCES sites(id)
);
CREATE TABLE IF NOT EXISTS runs (
  site_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  ok INTEGER NOT NULL,
  started_at TEXT NOT NULL,
  manifest_json TEXT,
  error_json TEXT,
  PRIMARY KEY (site_id, run_id)
);`)
	if err != nil {
		return err
	}
	if err := s.addLimitColumn("limit_upload_kib", `ALTER TABLE sites ADD COLUMN limit_upload_kib INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.addLimitColumn("limit_download_kib", `ALTER TABLE sites ADD COLUMN limit_download_kib INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.addLimitColumn("location_json", `ALTER TABLE sites ADD COLUMN location_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return s.addLimitColumn("retention_json", `ALTER TABLE sites ADD COLUMN retention_json TEXT NOT NULL DEFAULT ''`)
}

func (s *Store) addLimitColumn(column, statement string) error {
	rows, err := s.DB.Query(`PRAGMA table_info(sites)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.Exec(statement)
	return err
}

func (s *Store) CreateSite(site model.Site) error {
	if _, err := s.GetSite(site.ID); err == nil {
		return fail.New("already_registered", "A site with id "+site.ID+" is already registered.")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sources, err := json.Marshal(site.Sources)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO sites (id, name, root, adapter, sources_json, schedule_cron, schedule_enabled, schedule_last_slot, restore_verified_at, restore_run_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, site.Name, site.Root, site.Adapter, string(sources), emptyNil(site.Schedule.Cron), boolInt(site.Schedule.Enabled), emptyNil(site.Schedule.LastSlot), emptyNil(site.RestoreConfidence.LastVerifiedAt), emptyNil(site.RestoreConfidence.RunID), now, now)
	if err != nil {
		return err
	}
	for _, dest := range site.Destinations {
		if err := insertDest(tx, site.ID, dest); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertDest(tx *sql.Tx, siteID string, dest model.Destination) error {
	config, err := json.Marshal(destConfigOf(dest))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO destinations (site_id, id, kind, enabled, config_json) VALUES (?, ?, ?, ?, ?)`,
		siteID, dest.ID, dest.Kind, boolInt(dest.Enabled), string(config))
	return err
}

type destConfig struct {
	Path           string `json:"path,omitempty"`
	Host           string `json:"host,omitempty"`
	User           string `json:"user,omitempty"`
	Bucket         string `json:"bucket,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	CredentialFile string `json:"credential_file,omitempty"`
}

func destConfigOf(dest model.Destination) destConfig {
	return destConfig{Path: dest.Path, Host: dest.Host, User: dest.User, Bucket: dest.Bucket, Prefix: dest.Prefix, Endpoint: dest.Endpoint, CredentialFile: dest.CredentialFile}
}

func (s *Store) AddDestination(siteID string, dest model.Destination) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertDest(tx, siteID, dest); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sites SET updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), siteID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveSchedule(siteID, cron string, enabled bool, lastSlot string) error {
	_, err := s.DB.Exec(`UPDATE sites SET schedule_cron = ?, schedule_enabled = ?, schedule_last_slot = ?, updated_at = ? WHERE id = ?`,
		emptyNil(cron), boolInt(enabled), emptyNil(lastSlot), time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) SaveRestore(siteID, verifiedAt, runID string) error {
	_, err := s.DB.Exec(`UPDATE sites SET restore_verified_at = ?, restore_run_id = ?, updated_at = ? WHERE id = ?`,
		emptyNil(verifiedAt), emptyNil(runID), time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) SaveLimits(siteID string, limits model.Limits) error {
	_, err := s.DB.Exec(`UPDATE sites SET limit_upload_kib = ?, limit_download_kib = ?, updated_at = ? WHERE id = ?`,
		limits.UploadKiB, limits.DownloadKiB, time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) SaveRetention(siteID string, policy model.Retention) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE sites SET retention_json = ?, updated_at = ? WHERE id = ?`,
		string(raw), time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) SaveAdapter(siteID, adapter string, sources []model.Source) error {
	raw, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE sites SET adapter = ?, sources_json = ?, updated_at = ? WHERE id = ?`,
		adapter, string(raw), time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) SaveLocation(siteID, root string, loc model.Location) error {
	raw, err := json.Marshal(loc)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE sites SET root = ?, location_json = ?, updated_at = ? WHERE id = ?`,
		root, string(raw), time.Now().UTC().Format(time.RFC3339), siteID)
	return err
}

func (s *Store) GetSite(id string) (model.Site, error) {
	row := s.DB.QueryRow(`SELECT id, name, root, adapter, sources_json, schedule_cron, schedule_enabled, schedule_last_slot, restore_verified_at, restore_run_id, created_at, limit_upload_kib, limit_download_kib, location_json, retention_json FROM sites WHERE id = ?`, id)
	site, err := scanSite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Site{}, fail.New("not_found", "No site is registered with id "+id+".")
	}
	if err != nil {
		return model.Site{}, err
	}
	dests, err := s.destinations(id)
	if err != nil {
		return model.Site{}, err
	}
	site.Destinations = dests
	return site, nil
}

func (s *Store) ListSites() ([]model.Site, error) {
	rows, err := s.DB.Query(`SELECT id FROM sites ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var sites []model.Site
	for _, id := range ids {
		site, err := s.GetSite(id)
		if err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	if sites == nil {
		sites = []model.Site{}
	}
	return sites, nil
}

func (s *Store) destinations(siteID string) ([]model.Destination, error) {
	rows, err := s.DB.Query(`SELECT id, kind, enabled, config_json FROM destinations WHERE site_id = ? ORDER BY id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dests []model.Destination
	for rows.Next() {
		var dest model.Destination
		var enabled int
		var raw string
		if err := rows.Scan(&dest.ID, &dest.Kind, &enabled, &raw); err != nil {
			return nil, err
		}
		dest.Enabled = enabled == 1
		var cfg destConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, err
		}
		dest.Path, dest.Host, dest.User = cfg.Path, cfg.Host, cfg.User
		dest.Bucket, dest.Prefix, dest.Endpoint = cfg.Bucket, cfg.Prefix, cfg.Endpoint
		dest.CredentialFile = cfg.CredentialFile
		dests = append(dests, dest)
	}
	if dests == nil {
		dests = []model.Destination{}
	}
	return dests, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSite(row scanner) (model.Site, error) {
	var site model.Site
	var sources, cron, slot, verified, runID, location, retention sql.NullString
	var enabled int
	err := row.Scan(&site.ID, &site.Name, &site.Root, &site.Adapter, &sources, &cron, &enabled, &slot, &verified, &runID, &site.CreatedAt, &site.Limits.UploadKiB, &site.Limits.DownloadKiB, &location, &retention)
	if err != nil {
		return model.Site{}, err
	}
	if sources.Valid {
		if err := json.Unmarshal([]byte(sources.String), &site.Sources); err != nil {
			return model.Site{}, err
		}
	}
	site.Schedule.Cron = cron.String
	site.Schedule.Enabled = enabled == 1
	site.Schedule.LastSlot = slot.String
	site.RestoreConfidence.LastVerifiedAt = verified.String
	site.RestoreConfidence.RunID = runID.String
	site.Policy = model.Policy{NeverOverwriteLive: true, RestoreTarget: "scratch_only"}
	if location.Valid && location.String != "" {
		if err := json.Unmarshal([]byte(location.String), &site.Location); err != nil {
			return model.Site{}, err
		}
	}
	if site.Location.Kind == "" {
		site.Location.Kind = "local"
	}
	site.Retention = model.DefaultRetention()
	if retention.Valid && retention.String != "" {
		site.Retention = model.Retention{}
		if err := json.Unmarshal([]byte(retention.String), &site.Retention); err != nil {
			return model.Site{}, err
		}
	}
	return site, nil
}

func (s *Store) BeginRun(siteID, runID, started string) error {
	_, err := s.DB.Exec(`INSERT INTO runs (site_id, run_id, ok, started_at) VALUES (?, ?, 0, ?)`, siteID, runID, started)
	return err
}

func (s *Store) InsertRun(siteID, runID, started string, manifest *model.Manifest, failure *fail.Error) error {
	ok, manifestJSON, errorJSON, err := runRecord(manifest, failure)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO runs (site_id, run_id, ok, started_at, manifest_json, error_json) VALUES (?, ?, ?, ?, ?, ?)`,
		siteID, runID, ok, started, manifestJSON, errorJSON)
	return err
}

func (s *Store) FinishRun(siteID, runID, started string, manifest *model.Manifest, failure *fail.Error) error {
	ok, manifestJSON, errorJSON, err := runRecord(manifest, failure)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(`UPDATE runs SET ok = ?, started_at = ?, manifest_json = ?, error_json = ? WHERE site_id = ? AND run_id = ?`,
		ok, started, manifestJSON, errorJSON, siteID, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return s.InsertRun(siteID, runID, started, manifest, failure)
	}
	return nil
}

func runRecord(manifest *model.Manifest, failure *fail.Error) (int, any, any, error) {
	ok := 0
	var manifestJSON, errorJSON any
	if manifest != nil {
		ok = 1
		raw, err := json.Marshal(manifest)
		if err != nil {
			return 0, nil, nil, err
		}
		manifestJSON = string(raw)
	}
	if failure != nil {
		raw, err := json.Marshal(failure)
		if err != nil {
			return 0, nil, nil, err
		}
		errorJSON = string(raw)
	}
	return ok, manifestJSON, errorJSON, nil
}

func (s *Store) DeleteRun(siteID, runID string) error {
	_, err := s.DB.Exec(`DELETE FROM runs WHERE site_id = ? AND run_id = ?`, siteID, runID)
	return err
}

func (s *Store) UpdateManifest(siteID, runID string, manifest model.Manifest) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE runs SET manifest_json = ? WHERE site_id = ? AND run_id = ?`, string(raw), siteID, runID)
	return err
}

type Run struct {
	SiteID    string
	RunID     string
	OK        bool
	StartedAt string
	Manifest  *model.Manifest
	Failure   *fail.Error
}

// InProgress is a run that has been started and has not finished.
func (r Run) InProgress() bool {
	return r.RunID != "" && !r.OK && r.Manifest == nil && r.Failure == nil
}

func (s *Store) RunExists(siteID, runID string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM runs WHERE site_id = ? AND run_id = ?`, siteID, runID).Scan(&n)
	return n > 0, err
}

func (s *Store) GetRun(siteID, runID string) (Run, error) {
	row := s.DB.QueryRow(`SELECT site_id, run_id, ok, started_at, manifest_json, error_json FROM runs WHERE site_id = ? AND run_id = ?`, siteID, runID)
	return scanRun(row)
}

func (s *Store) LatestRun(siteID string) (Run, error) {
	row := s.DB.QueryRow(`SELECT site_id, run_id, ok, started_at, manifest_json, error_json FROM runs WHERE site_id = ? ORDER BY run_id DESC LIMIT 1`, siteID)
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, nil
	}
	return run, err
}

func (s *Store) ListRuns(siteID string) ([]Run, error) {
	rows, err := s.DB.Query(`SELECT site_id, run_id, ok, started_at, manifest_json, error_json FROM runs WHERE site_id = ? ORDER BY run_id DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if runs == nil {
		runs = []Run{}
	}
	return runs, rows.Err()
}

func (s *Store) LatestFailure(siteID string) (Run, error) {
	row := s.DB.QueryRow(`SELECT site_id, run_id, ok, started_at, manifest_json, error_json FROM runs WHERE site_id = ? AND ok = 0 AND error_json IS NOT NULL ORDER BY run_id DESC LIMIT 1`, siteID)
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, nil
	}
	return run, err
}

func scanRun(row scanner) (Run, error) {
	var run Run
	var ok int
	var manifest, failure sql.NullString
	err := row.Scan(&run.SiteID, &run.RunID, &ok, &run.StartedAt, &manifest, &failure)
	if err != nil {
		return Run{}, err
	}
	run.OK = ok == 1
	if manifest.Valid && manifest.String != "" {
		var decoded model.Manifest
		if err := json.Unmarshal([]byte(manifest.String), &decoded); err != nil {
			return Run{}, err
		}
		run.Manifest = &decoded
	}
	if failure.Valid && failure.String != "" {
		var decoded fail.Error
		if err := json.Unmarshal([]byte(failure.String), &decoded); err != nil {
			return Run{}, err
		}
		run.Failure = &decoded
	}
	return run, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func emptyNil(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// WriteSecret writes mode 0600 and returns the path.
func WriteSecret(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(text)
	closeErr := f.Close()
	_ = os.Chmod(path, 0o600)
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
