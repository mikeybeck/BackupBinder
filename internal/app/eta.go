package app

import (
	"fmt"
	"strings"
	"time"

	"backupbinder/internal/model"
)

const longRunThreshold = 30 * time.Minute

// TimingEstimate is a best-effort remaining-time guess for the UI.
type TimingEstimate struct {
	ETASeconds int64  `json:"eta_seconds,omitempty"`
	ETA        string `json:"eta,omitempty"`
	Notice     string `json:"notice,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// EstimateTiming combines progress, elapsed time, and restic speed limits.
// Server rsync copies ignore restic limits; those phases use observed percent only.
func EstimateTiming(p model.Progress, startedAt time.Time, uploadKiB, downloadKiB int, now time.Time) TimingEstimate {
	p = normalizeProgress(p)
	if now.IsZero() {
		now = time.Now()
	}
	var candidates []time.Duration
	var detail string

	if p.PercentKnown && p.Percent >= 2 && p.Percent < 100 && !startedAt.IsZero() {
		elapsed := now.Sub(startedAt)
		if elapsed >= 10*time.Second {
			remaining := time.Duration(float64(elapsed) * (100 - p.Percent) / p.Percent)
			candidates = append(candidates, remaining)
			detail = "Based on progress so far"
		}
	}

	if rate := phaseRateKiB(p.Phase, uploadKiB, downloadKiB); rate > 0 {
		limitDetail := "Based on upload limited to " + fmt.Sprintf("%d KiB/s", rate)
		remaining := remainingBytes(p)
		if remaining > 0 {
			secs := remaining / int64(rate*1024)
			if secs < 1 {
				secs = 1
			}
			candidates = append(candidates, time.Duration(secs)*time.Second)
			if detail == "" {
				detail = limitDetail
			} else {
				detail += "; " + limitDetail
			}
		} else if p.BytesTotal > 0 && (!p.PercentKnown || p.Percent < 2) {
			// Early in a capped phase: warn from full payload size before progress moves.
			secs := p.BytesTotal / int64(rate*1024)
			if secs < 1 {
				secs = 1
			}
			full := time.Duration(secs) * time.Second
			if full >= longRunThreshold {
				return TimingEstimate{
					ETASeconds: int64(full / time.Second),
					ETA:        formatETA(full),
					Notice:     longRunNotice(full),
					Detail:     limitDetail + " and about " + formatDataSize(p.BytesTotal),
				}
			}
		}
	}

	if len(candidates) == 0 {
		return TimingEstimate{}
	}
	eta := candidates[0]
	for _, c := range candidates[1:] {
		if c > eta {
			eta = c
		}
	}
	if eta < 5*time.Second {
		return TimingEstimate{Detail: detail}
	}
	out := TimingEstimate{
		ETASeconds: int64(eta / time.Second),
		ETA:        formatETA(eta),
		Detail:     detail,
	}
	if eta >= longRunThreshold {
		out.Notice = longRunNotice(eta)
	}
	return out
}

func phaseRateKiB(phase string, uploadKiB, downloadKiB int) int {
	lower := strings.ToLower(phase)
	switch {
	case strings.Contains(lower, "server"):
		// rsync does not use restic --limit-*; do not pretend the download cap applies.
		return 0
	case strings.Contains(lower, "reading site"), strings.Contains(lower, "estimating"), strings.Contains(lower, "dumping"):
		return 0
	case strings.Contains(lower, "archiv"), strings.Contains(lower, "saving"),
		strings.Contains(lower, "copying to"), strings.Contains(lower, "google"),
		strings.Contains(lower, "destination"), strings.Contains(lower, "s3"),
		strings.Contains(lower, "b2"), strings.Contains(lower, "sftp"),
		strings.Contains(lower, "disk"):
		return uploadKiB
	default:
		return uploadKiB
	}
}

func remainingBytes(p model.Progress) int64 {
	if p.BytesTotal <= 0 {
		return 0
	}
	if p.BytesWritten > 0 && p.BytesWritten < p.BytesTotal {
		return p.BytesTotal - p.BytesWritten
	}
	if p.PercentKnown && p.Percent > 0 && p.Percent < 100 {
		left := int64(float64(p.BytesTotal) * (100 - p.Percent) / 100)
		if left < 1 {
			return 1
		}
		return left
	}
	if !p.PercentKnown || p.Percent < 1 {
		return p.BytesTotal
	}
	return 0
}

func longRunNotice(eta time.Duration) string {
	return "This may take a while (" + formatETA(eta) + "). You can leave this page; the backup keeps running in the background."
}

func formatETA(d time.Duration) string {
	if d < time.Minute {
		secs := int(d.Seconds() + 0.5)
		if secs < 1 {
			secs = 1
		}
		return fmt.Sprintf("about %d seconds", secs)
	}
	if d < time.Hour {
		mins := int(d.Minutes() + 0.5)
		if mins < 1 {
			mins = 1
		}
		if mins == 1 {
			return "about 1 minute"
		}
		return fmt.Sprintf("about %d minutes", mins)
	}
	hours := d.Hours()
	if hours < 10 {
		return fmt.Sprintf("about %.1f hours", hours)
	}
	return fmt.Sprintf("about %.0f hours", hours)
}

func formatDataSize(n int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.0f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
