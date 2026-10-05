package schedule

import (
	"testing"
	"time"
)

func TestEveryDayAtThree(t *testing.T) {
	at := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	ok, err := Matches("0 3 * * *", at)
	if err != nil || !ok {
		t.Fatalf("match %v %v", ok, err)
	}
	ok, err = Matches("0 3 * * *", at.Add(time.Minute))
	if err != nil || ok {
		t.Fatalf("off minute matched: %v %v", ok, err)
	}
}

func TestStepMinutes(t *testing.T) {
	for _, minute := range []int{0, 15, 30, 45} {
		ok, err := Matches("*/15 * * * *", time.Date(2026, 9, 25, 1, minute, 0, 0, time.UTC))
		if err != nil || !ok {
			t.Fatalf("minute %d: %v %v", minute, ok, err)
		}
	}
	ok, err := Matches("*/15 * * * *", time.Date(2026, 9, 25, 1, 7, 0, 0, time.UTC))
	if err != nil || ok {
		t.Fatal("7 should not match")
	}
}

func TestSundayZeroAndSeven(t *testing.T) {
	sunday := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	if sunday.Weekday() != time.Sunday {
		t.Fatal(sunday.Weekday())
	}
	for _, expr := range []string{"0 0 * * 0", "0 0 * * 7"} {
		ok, err := Matches(expr, sunday)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", expr, ok, err)
		}
	}
}

func TestDayOrWeekday(t *testing.T) {
	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if monday.Weekday() != time.Monday {
		t.Fatal(monday.Weekday())
	}
	ok, err := Matches("0 0 1 * 1", monday)
	if err != nil || !ok {
		t.Fatal("Monday should match weekday field")
	}
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	ok, err = Matches("0 0 1 * 1", first)
	if err != nil || !ok {
		t.Fatal("the 1st should match day-of-month")
	}
	wednesday := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	ok, err = Matches("0 0 1 * 1", wednesday)
	if err != nil || ok {
		t.Fatal("Sep 2 should not match")
	}
}

func TestRejectsNames(t *testing.T) {
	if _, err := Matches("0 0 * * Mon", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
