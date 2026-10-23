package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"local-backup/internal/archive"
	"local-backup/internal/fail"
	"local-backup/internal/model"
	"local-backup/internal/store"
)

type progressWriter struct {
	app    *App
	siteID string
	runID  string
	lock   *os.File
	mu     sync.Mutex
	last   time.Time
	cur    model.Progress
}

func (a *App) progress(siteID, runID string) (*progressWriter, error) {
	lock, err := a.holdRun(siteID, runID)
	if err != nil {
		return nil, err
	}
	a.track(siteID, runID)
	return &progressWriter{app: a, siteID: siteID, runID: runID, lock: lock}, nil
}

func (a *App) track(siteID, runID string) {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	if a.live == nil {
		a.live = map[string]struct{}{}
	}
	a.live[siteID+"/"+runID] = struct{}{}
}

func (a *App) untrack(siteID, runID string) {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	delete(a.live, siteID+"/"+runID)
}

func (a *App) ownsRun(siteID, runID string) bool {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	_, ok := a.live[siteID+"/"+runID]
	return ok
}

func (a *App) holdRun(siteID, runID string) (*os.File, error) {
	path := a.lockPath(siteID, runID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// runIsLive reports whether this process, or another one, is still inside the run.
// A lock left behind by a dead process does not count.
func (a *App) runIsLive(siteID, runID string) bool {
	if a.ownsRun(siteID, runID) {
		return true
	}
	f, err := os.Open(a.lockPath(siteID, runID))
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// settleRun marks an in-progress run as interrupted when no process is still backing it up.
func (a *App) settleRun(run store.Run) (store.Run, error) {
	if !run.InProgress() || a.runIsLive(run.SiteID, run.RunID) {
		return run, nil
	}
	fe := &fail.Error{
		Code:    "interrupted",
		Message: "The backup stopped before it finished.",
		Step:    "backup",
		Hint:    "The live site was not modified. This usually means the backup process exited early (for example the UI server was restarted while a backup ran inside it). Start the backup again.",
		RunID:   run.RunID,
	}
	if err := a.DB.FinishRun(run.SiteID, run.RunID, run.StartedAt, nil, fe); err != nil {
		return run, err
	}
	_ = os.Remove(a.progressPath(run.SiteID, run.RunID))
	_ = os.Remove(a.lockPath(run.SiteID, run.RunID))
	return a.DB.GetRun(run.SiteID, run.RunID)
}

func (w *progressWriter) set(p model.Progress, force bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p = normalizeProgress(p)
	w.cur = p
	if !force && time.Since(w.last) < 200*time.Millisecond {
		return
	}
	w.last = time.Now()
	_ = w.app.writeProgress(w.siteID, w.runID, p)
}

// normalizeProgress fills Percent from file or byte counters when restic has not
// supplied one yet, so the UI can show a percentage for every measurable phase.
func normalizeProgress(p model.Progress) model.Progress {
	switch {
	case p.PercentKnown:
		// keep restic/rsync value
	case p.FilesTotal > 0:
		p.PercentKnown = true
		p.Percent = float64(p.FilesDone) * 100 / float64(p.FilesTotal)
	case p.BytesTotal > 0 && p.BytesWritten > 0:
		if ratio, ok := savePercent(p.BytesWritten, p.BytesTotal); ok {
			p.PercentKnown = true
			p.Percent = ratio
		}
	}
	if p.PercentKnown {
		if p.Percent < 0 {
			p.Percent = 0
		}
		if p.Percent > 100 {
			p.Percent = 100
		}
	}
	return p
}

func (w *progressWriter) clear() {
	_ = os.Remove(w.app.progressPath(w.siteID, w.runID))
}

func (w *progressWriter) release() {
	w.app.untrack(w.siteID, w.runID)
	if w.lock != nil {
		_ = syscall.Flock(int(w.lock.Fd()), syscall.LOCK_UN)
		_ = w.lock.Close()
		_ = os.Remove(w.app.lockPath(w.siteID, w.runID))
		w.lock = nil
	}
	w.clear()
}

func (w *progressWriter) onCopy(phase string, bytesHint int64) func(archive.Status) {
	return func(st archive.Status) {
		p := model.Progress{Phase: phase, BytesTotal: bytesHint}
		if st.File != "" {
			p.File = st.File
		}
		if st.WrittenBytes > 0 {
			p.BytesWritten = st.WrittenBytes
		}
		if st.HasPercent {
			p.PercentKnown = true
			pct := st.Percent * 100
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			p.Percent = pct
		}
		w.set(p, false)
	}
}

func (w *progressWriter) onArchive(payload string) func(archive.Status) {
	return func(st archive.Status) {
		w.mu.Lock()
		p := w.cur
		w.mu.Unlock()
		saving := p.Phase == "Saving the archive"
		p.Phase = "Archiving"
		if st.File != "" {
			p.File = archiveFile(st.File, payload)
		}
		if st.HasBytes {
			p.BytesTotal = st.BytesTotal
		}
		if st.WrittenBytes > 0 {
			p.BytesWritten = st.WrittenBytes
		}
		if st.HasPercent {
			p.PercentKnown = true
			pct := st.Percent * 100
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			p.Percent = pct
			if pct >= 100 && st.File == "" {
				// Restic's percent is files read. The repository write continues after that.
				p.Phase = "Saving the archive"
				if ratio, ok := savePercent(p.BytesWritten, p.BytesTotal); ok {
					p.PercentKnown = true
					p.Percent = ratio
				} else if p.BytesTotal > 0 {
					// Keep a high water mark so the bar does not go indeterminate mid-save.
					p.PercentKnown = true
					p.Percent = 99
				} else {
					p.PercentKnown = false
				}
			}
		} else if saving && st.File == "" {
			p.Phase = "Saving the archive"
			if ratio, ok := savePercent(p.BytesWritten, p.BytesTotal); ok {
				p.PercentKnown = true
				p.Percent = ratio
			}
		}
		if st.HasFiles {
			p.FilesDone = st.FilesDone
			p.FilesTotal = st.FilesTotal
		}
		w.set(p, false)
	}
}

func (a *App) progressPath(siteID, runID string) string {
	return filepath.Join(a.Home, "runs", siteID, runID+".progress.json")
}

func (a *App) lockPath(siteID, runID string) string {
	return filepath.Join(a.Home, "runs", siteID, runID+".lock")
}

func (a *App) writeProgress(siteID, runID string, p model.Progress) error {
	return writePrivateJSON(a.progressPath(siteID, runID), p)
}

// ReadProgress returns the latest step for a run that is still going.
func (a *App) ReadProgress(siteID, runID string) (model.Progress, bool) {
	raw, err := os.ReadFile(a.progressPath(siteID, runID))
	if err != nil {
		return model.Progress{}, false
	}
	var p model.Progress
	if json.Unmarshal(raw, &p) != nil || p.Phase == "" {
		return model.Progress{}, false
	}
	return p, true
}

// savePercent is bytes written compared with the size of the files.
// It stays under 100 until the backup command finishes.
func savePercent(written, total int64) (float64, bool) {
	if written <= 0 || total <= 0 {
		return 0, false
	}
	ratio := float64(written) * 100 / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 99 {
		ratio = 99
	}
	return ratio, true
}

func archiveFile(path, payload string) string {
	path = filepath.Clean(path)
	payload = filepath.Clean(payload)
	rel := path
	prefix := payload + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		rel = strings.TrimPrefix(path, prefix)
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimLeft(rel, "/")
	return strings.TrimPrefix(rel, "files/")
}
