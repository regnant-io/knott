// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

func TestNextRunInterval(t *testing.T) {
	from := mustParse(t, "2026-01-01 10:00:00")
	next, err := NextRun("interval", "300", from)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(from.Add(5 * time.Minute)) {
		t.Fatalf("interval: got %v want %v", next, from.Add(5*time.Minute))
	}
	if _, err := NextRun("interval", "0", from); err == nil {
		t.Fatal("expected error for zero interval")
	}
	if _, err := NextRun("interval", "abc", from); err == nil {
		t.Fatal("expected error for non-numeric interval")
	}
}

func TestNextRunDaily(t *testing.T) {
	// before the time today → fires today
	from := mustParse(t, "2026-01-01 08:00:00")
	next, _ := NextRun("daily", "09:30", from)
	want := mustParse(t, "2026-01-01 09:30:00")
	if !next.Equal(want) {
		t.Fatalf("daily before: got %v want %v", next, want)
	}
	// after the time today → fires tomorrow
	from = mustParse(t, "2026-01-01 10:00:00")
	next, _ = NextRun("daily", "09:30", from)
	want = mustParse(t, "2026-01-02 09:30:00")
	if !next.Equal(want) {
		t.Fatalf("daily after: got %v want %v", next, want)
	}
	if _, err := NextRun("daily", "25:00", from); err == nil {
		t.Fatal("expected error for invalid hour")
	}
}

func TestNextRunCronEveryFiveMinutes(t *testing.T) {
	from := mustParse(t, "2026-01-01 10:02:30")
	next, err := NextRun("cron", "*/5 * * * *", from)
	if err != nil {
		t.Fatal(err)
	}
	want := mustParse(t, "2026-01-01 10:05:00")
	if !next.Equal(want) {
		t.Fatalf("cron */5: got %v want %v", next, want)
	}
}

func TestNextRunCronDailyAt(t *testing.T) {
	from := mustParse(t, "2026-01-01 10:00:00")
	next, err := NextRun("cron", "30 9 * * *", from)
	if err != nil {
		t.Fatal(err)
	}
	want := mustParse(t, "2026-01-02 09:30:00")
	if !next.Equal(want) {
		t.Fatalf("cron daily: got %v want %v", next, want)
	}
}

func TestNextRunCronWeekday(t *testing.T) {
	// 2026-01-01 is a Thursday. Next Monday 09:00 is 2026-01-05.
	from := mustParse(t, "2026-01-01 10:00:00")
	next, err := NextRun("cron", "0 9 * * 1", from)
	if err != nil {
		t.Fatal(err)
	}
	want := mustParse(t, "2026-01-05 09:00:00")
	if !next.Equal(want) {
		t.Fatalf("cron weekday: got %v want %v", next, want)
	}
}

func TestCronInvalid(t *testing.T) {
	from := time.Now()
	for _, bad := range []string{"* * * *", "60 * * * *", "* 24 * * *", "abc * * * *", ""} {
		if _, err := NextRun("cron", bad, from); err == nil {
			t.Fatalf("expected error for cron %q", bad)
		}
	}
}

func TestCronRangeAndList(t *testing.T) {
	from := mustParse(t, "2026-01-01 10:00:00")
	// At minute 0, only on hours 9 and 17.
	next, err := NextRun("cron", "0 9,17 * * *", from)
	if err != nil {
		t.Fatal(err)
	}
	want := mustParse(t, "2026-01-01 17:00:00")
	if !next.Equal(want) {
		t.Fatalf("cron list: got %v want %v", next, want)
	}
}
