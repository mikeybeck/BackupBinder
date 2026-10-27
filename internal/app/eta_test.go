package app

import (
	"strings"
	"testing"
	"time"

	"backupbinder/internal/model"
)

func TestEstimateTimingFromUploadLimit(t *testing.T) {
	// 1 GiB at 100 KiB/s ≈ 2.9 hours.
	p := model.Progress{Phase: "Copying to Google Drive (gdrive)", BytesTotal: 1 << 30}
	got := EstimateTiming(p, time.Time{}, 100, 0, time.Now())
	if got.ETASeconds < 2*3600 || got.Notice == "" || !strings.Contains(got.Detail, "100 KiB/s") {
		t.Fatalf("%#v", got)
	}
}

func TestEstimateTimingFromObservedPercent(t *testing.T) {
	started := time.Now().Add(-20 * time.Minute)
	p := model.Progress{Phase: "Copying files from the server", PercentKnown: true, Percent: 25}
	got := EstimateTiming(p, started, 100, 100, time.Now())
	// 20 min for 25% → ~60 min remaining.
	if got.ETASeconds < 50*60 || got.ETASeconds > 80*60 {
		t.Fatalf("%#v", got)
	}
	if !strings.Contains(got.Detail, "progress") {
		t.Fatal(got.Detail)
	}
	// rsync must not claim the restic download cap.
	if strings.Contains(got.Detail, "download limited") {
		t.Fatal(got.Detail)
	}
}

func TestEstimateTimingIgnoresLimitForLocalRead(t *testing.T) {
	p := model.Progress{Phase: "Reading site files", BytesTotal: 1 << 30, FilesDone: 10, FilesTotal: 100}
	got := EstimateTiming(p, time.Time{}, 100, 0, time.Now())
	if got.Notice != "" && strings.Contains(got.Detail, "upload limited") {
		t.Fatalf("local read should not use upload cap: %#v", got)
	}
}

func TestFormatETA(t *testing.T) {
	if formatETA(45*time.Second) != "about 45 seconds" {
		t.Fatal(formatETA(45 * time.Second))
	}
	if formatETA(5*time.Minute) != "about 5 minutes" {
		t.Fatal(formatETA(5 * time.Minute))
	}
}
