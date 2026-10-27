// Package model is the backup contract: site, sources, snapshot, destinations, restore.
package model

import "strconv"

// Site is the saved contract. Database passwords are never fields on Site.
type Site struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Root              string            `json:"root"`
	Adapter           string            `json:"adapter"`
	Sources           []Source          `json:"sources"`
	Destinations      []Destination     `json:"destinations"`
	Schedule          Schedule          `json:"schedule"`
	RestoreConfidence RestoreConfidence `json:"restore_confidence"`
	CreatedAt         string            `json:"created_at,omitempty"`
	Policy            Policy            `json:"policy"`
	Limits            Limits            `json:"limits"`
	Retention         Retention         `json:"retention"`
	Location          Location          `json:"location"`
	// Setup is the decisions still open: location, name, database, destination, schedule, backup.
	// An empty list means this site is not in the guided walk.
	Setup     []string `json:"setup,omitempty"`
	UpdatedAt string   `json:"-"`
}

// Location is the one place a site's files are read from.
// Kind is "local" or "sftp". An empty kind means the local root.
type Location struct {
	Kind string `json:"kind"`
	Host string `json:"host,omitempty"`
	User string `json:"user,omitempty"`
	Path string `json:"path,omitempty"`
}

// Limits caps restic repository traffic. Zero is unlimited. Rates are KiB per second.
type Limits struct {
	UploadKiB   int `json:"upload_kib"`
	DownloadKiB int `json:"download_kib"`
}

// Retention is how many snapshots to keep. Zero skips that rule.
// When every count is zero, snapshots are kept until a count is set.
// The rules overlap: a snapshot stays if any rule keeps it.
type Retention struct {
	KeepLast    int `json:"keep_last"`
	KeepHourly  int `json:"keep_hourly"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
	KeepYearly  int `json:"keep_yearly"`
}

// DefaultRetention is the policy used when a site has none saved.
// The newest three snapshots always stay. The time buckets match a Time Machine shape:
// the last day of hours, two weeks of days, eight weeks, a year of months, and two years.
func DefaultRetention() Retention {
	return Retention{KeepLast: 3, KeepHourly: 24, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12, KeepYearly: 2}
}

// Active reports whether any rule would remove a snapshot.
func (r Retention) Active() bool {
	return r.KeepLast > 0 || r.KeepHourly > 0 || r.KeepDaily > 0 || r.KeepWeekly > 0 || r.KeepMonthly > 0 || r.KeepYearly > 0
}

// ForgetArgs is the restic forget keep flags for this policy. Zero counts are omitted.
func (r Retention) ForgetArgs() []string {
	args := make([]string, 0, 12)
	add := func(flag string, n int) {
		if n > 0 {
			args = append(args, flag, strconv.Itoa(n))
		}
	}
	add("--keep-last", r.KeepLast)
	add("--keep-hourly", r.KeepHourly)
	add("--keep-daily", r.KeepDaily)
	add("--keep-weekly", r.KeepWeekly)
	add("--keep-monthly", r.KeepMonthly)
	add("--keep-yearly", r.KeepYearly)
	return args
}

type Policy struct {
	NeverOverwriteLive bool   `json:"never_overwrite_live"`
	RestoreTarget      string `json:"restore_target"`
}

type Schedule struct {
	Cron     string `json:"cron"`
	Enabled  bool   `json:"enabled"`
	LastSlot string `json:"last_slot,omitempty"`
}

type RestoreConfidence struct {
	LastVerifiedAt string `json:"last_verified_at"`
	RunID          string `json:"run_id,omitempty"`
}

type Source struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Path           string   `json:"path,omitempty"`
	Excludes       []string `json:"excludes,omitempty"`
	Engine         string   `json:"engine,omitempty"`
	Method         string   `json:"method,omitempty"`
	SQLFile        string   `json:"sql_file,omitempty"`
	Name           string   `json:"name,omitempty"`
	User           string   `json:"user,omitempty"`
	Host           string   `json:"host,omitempty"`
	PasswordSource string   `json:"password_source,omitempty"`
	TablePrefix    string   `json:"table_prefix,omitempty"`
}

type Destination struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Enabled        bool   `json:"enabled"`
	Path           string `json:"path,omitempty"`
	Host           string `json:"host,omitempty"`
	User           string `json:"user,omitempty"`
	Bucket         string `json:"bucket,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	CredentialFile string `json:"credential_file,omitempty"`
	Credentials    string `json:"credentials,omitempty"`
}

type Versions struct {
	WordPress   string      `json:"wordpress,omitempty"`
	PHPCLI      string      `json:"php_cli,omitempty"`
	Plugins     []Component `json:"plugins"`
	Themes      []Component `json:"themes"`
	TablePrefix string      `json:"table_prefix,omitempty"`
}

type Component struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type FileRecord struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Excluded struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Manifest struct {
	OK           bool              `json:"ok"`
	Schema       string            `json:"schema"`
	Model        string            `json:"model"`
	Site         ManifestSite      `json:"site"`
	Captured     Captured          `json:"captured"`
	SizeBytes    int64             `json:"size_bytes"`
	Timestamp    string            `json:"timestamp"`
	Snapshot     Snapshot          `json:"snapshot"`
	Destinations []DestResult      `json:"destinations"`
	Restore      Restore           `json:"restore"`
	Retention    *RetentionApplied `json:"retention,omitempty"`
	Warnings     []string          `json:"warnings,omitempty"`
}

// RetentionApplied is what the policy did to the local repository after a run.
type RetentionApplied struct {
	Removed []string `json:"removed,omitempty"`
	Kept    int      `json:"kept"`
}

type ManifestSite struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Adapter  string   `json:"adapter"`
	Root     string   `json:"root"`
	Versions Versions `json:"versions"`
}

type Captured struct {
	Files    FileCapture `json:"files"`
	Database *DBCapture  `json:"database,omitempty"`
}

type FileCapture struct {
	Root          string       `json:"root"`
	FileCount     int          `json:"file_count"`
	ByteSize      int64        `json:"byte_size"`
	ExcludedCount int          `json:"excluded_count"`
	Excluded      []Excluded   `json:"excluded"`
	Inventory     []FileRecord `json:"inventory"`
}

type DBCapture struct {
	Method        string   `json:"method"`
	Filename      string   `json:"filename"`
	Bytes         int64    `json:"bytes"`
	SHA256        string   `json:"sha256"`
	Tables        []string `json:"tables"`
	Importable    bool     `json:"importable"`
	ServerVersion string   `json:"server_version,omitempty"`
}

type Snapshot struct {
	RunID            string `json:"run_id"`
	ResticSnapshotID string `json:"restic_snapshot_id,omitempty"`
	Repository       string `json:"repository"`
	DataAdded        int64  `json:"restic_data_added,omitempty"`
}

type DestResult struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Written    bool   `json:"written"`
	Offsite    bool   `json:"offsite"`
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type Restore struct {
	Confidence string   `json:"confidence"`
	Steps      []string `json:"steps"`
}

// Progress is the latest step of a run that has not finished.
// Percent is 0–100 and is set only when PercentKnown is true.
type Progress struct {
	Phase        string  `json:"phase"`
	File         string  `json:"file,omitempty"`
	PercentKnown bool    `json:"percent_known,omitempty"`
	Percent      float64 `json:"percent,omitempty"`
	FilesDone    int64   `json:"files_done,omitempty"`
	FilesTotal   int64   `json:"files_total,omitempty"`
	BytesWritten int64   `json:"bytes_written,omitempty"`
	BytesTotal   int64   `json:"bytes_total,omitempty"`
}

const Schema = "backupbinder.manifest/v1"
const ModelLine = "site -> sources -> snapshot -> destinations -> restore"

func DatabaseSource(site Site) *Source {
	for i := range site.Sources {
		if site.Sources[i].Kind == "database" {
			return &site.Sources[i]
		}
	}
	return nil
}
