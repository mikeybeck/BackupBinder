package app

import (
	"strconv"
	"strings"

	"local-backup/internal/fail"
	"local-backup/internal/model"
)

// ParseKiB reads a KiB/s cap. An empty string is unset. Zero means unlimited.
func ParseKiB(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false, fail.New("bad_limit", "A speed limit is a whole number of KiB per second. Use 0 for unlimited.")
	}
	return n, true, nil
}

// SetLimits saves upload and download caps for one site. A nil rate is left unchanged.
func (a *App) SetLimits(siteID string, upload, download *int) (model.Limits, error) {
	site, err := a.DB.GetSite(siteID)
	if err != nil {
		return model.Limits{}, err
	}
	limits := site.Limits
	if err := applyKiB(&limits.UploadKiB, upload); err != nil {
		return model.Limits{}, err
	}
	if err := applyKiB(&limits.DownloadKiB, download); err != nil {
		return model.Limits{}, err
	}
	if err := a.DB.SaveLimits(site.ID, limits); err != nil {
		return model.Limits{}, err
	}
	return limits, nil
}

func applyKiB(dest *int, value *int) error {
	if value == nil {
		return nil
	}
	if *value < 0 {
		return fail.New("bad_limit", "A speed limit is a whole number of KiB per second. Use 0 for unlimited.")
	}
	*dest = *value
	return nil
}
