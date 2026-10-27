// Package schedule matches five-field cron expressions. Schedules are evaluated
// by this process when a system cron calls run_scheduled. A backup run does not
// call an agent.
package schedule

import (
	"strconv"
	"strings"
	"time"

	"backupbinder/internal/fail"
)

// Matches reports whether expr matches when. Weekday 0 and 7 are Sunday.
// When both day-of-month and weekday are restricted, either match is enough.
func Matches(expr string, when time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fail.New("bad_cron", "A schedule is five cron fields: minute hour day month weekday.")
	}
	minute, err := fieldValues(fields[0], 0, 59)
	if err != nil {
		return false, err
	}
	hour, err := fieldValues(fields[1], 0, 23)
	if err != nil {
		return false, err
	}
	dom, err := fieldValues(fields[2], 1, 31)
	if err != nil {
		return false, err
	}
	month, err := fieldValues(fields[3], 1, 12)
	if err != nil {
		return false, err
	}
	dow, err := fieldValues(fields[4], 0, 7)
	if err != nil {
		return false, err
	}
	if _, ok := minute[when.Minute()]; !ok {
		return false, nil
	}
	if _, ok := hour[when.Hour()]; !ok {
		return false, nil
	}
	if _, ok := month[int(when.Month())]; !ok {
		return false, nil
	}
	cronDow := int(when.Weekday()) // Sunday == 0, same as cron.
	domStar := fields[2] == "*"
	dowStar := fields[4] == "*"
	_, domOK := dom[when.Day()]
	_, dowOK := dow[cronDow]
	if cronDow == 0 {
		if _, ok := dow[7]; ok {
			dowOK = true
		}
	}
	switch {
	case domStar && dowStar:
		return true, nil
	case domStar:
		return dowOK, nil
	case dowStar:
		return domOK, nil
	default:
		return domOK || dowOK, nil
	}
}

// Check parses expr without comparing it to a time.
func Check(expr string) error {
	_, err := Matches(expr, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return err
}

func fieldValues(expr string, lo, hi int) (map[int]struct{}, error) {
	values := map[int]struct{}{}
	for _, part := range strings.Split(expr, ",") {
		if part == "" || strings.ContainsAny(part, " \t") {
			return nil, fail.New("bad_cron", "Bad cron field "+expr+". Use numbers, *, ranges, and lists.")
		}
		step := 1
		base := part
		if strings.Contains(part, "/") {
			left, right, _ := strings.Cut(part, "/")
			n, err := strconv.Atoi(right)
			if err != nil || n <= 0 {
				return nil, fail.New("bad_cron", "Bad cron step in "+part+".")
			}
			step = n
			base = left
		}
		start, end := lo, hi
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			left, right, _ := strings.Cut(base, "-")
			a, errA := strconv.Atoi(left)
			b, errB := strconv.Atoi(right)
			if errA != nil || errB != nil {
				return nil, fail.New("bad_cron", "Bad cron range "+part+". Names such as Mon are not accepted.")
			}
			start, end = a, b
		default:
			n, err := strconv.Atoi(base)
			if err != nil {
				return nil, fail.New("bad_cron", "Bad cron value "+part+". Use numeric fields only.")
			}
			start, end = n, n
		}
		if start > end || start < lo || end > hi {
			return nil, fail.New("bad_cron", "Cron value "+part+" is outside the allowed range.")
		}
		for n := start; n <= end; n += step {
			values[n] = struct{}{}
		}
	}
	return values, nil
}
