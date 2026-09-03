// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NextRun computes the next time a schedule should fire, strictly after `from`.
//
// Supported kinds:
//   - "interval": expr is a number of seconds (e.g. "300" → every 5 minutes)
//   - "daily":    expr is "HH:MM" 24h local time (e.g. "09:30")
//   - "cron":     expr is a 5-field cron string "min hour dom month dow"
//     Each field supports: *, a value, a list (1,2,3), a range (1-5),
//     and step values (*/5 or 10-30/5).
func NextRun(kind, expr string, from time.Time) (time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "interval":
		secs, err := strconv.Atoi(strings.TrimSpace(expr))
		if err != nil || secs <= 0 {
			return time.Time{}, fmt.Errorf("invalid interval seconds %q", expr)
		}
		return from.Add(time.Duration(secs) * time.Second), nil

	case "daily":
		hh, mm, err := parseHHMM(expr)
		if err != nil {
			return time.Time{}, err
		}
		next := time.Date(from.Year(), from.Month(), from.Day(), hh, mm, 0, 0, from.Location())
		if !next.After(from) {
			next = next.Add(24 * time.Hour)
		}
		return next, nil

	case "cron":
		return nextCron(expr, from)

	default:
		return time.Time{}, fmt.Errorf("unknown schedule kind %q", kind)
	}
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid daily time %q (want HH:MM)", s)
	}
	hh, err1 := strconv.Atoi(parts[0])
	mm, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("invalid daily time %q", s)
	}
	return hh, mm, nil
}

// nextCron evaluates a standard 5-field cron expression by scanning forward
// minute-by-minute (bounded to ~366 days) for the next matching minute.
func nextCron(expr string, from time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron must have 5 fields, got %d in %q", len(fields), expr)
	}
	minSet, err := cronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron minute: %w", err)
	}
	hourSet, err := cronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron hour: %w", err)
	}
	domSet, err := cronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron day-of-month: %w", err)
	}
	monthSet, err := cronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron month: %w", err)
	}
	dowSet, err := cronField(fields[4], 0, 6) // 0 = Sunday
	if err != nil {
		return time.Time{}, fmt.Errorf("cron day-of-week: %w", err)
	}
	domRestricted := fields[2] != "*"
	dowRestricted := fields[4] != "*"

	// Start at the next whole minute after `from`.
	t := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if minSet[t.Minute()] && hourSet[t.Hour()] && monthSet[int(t.Month())] {
			domMatch := domSet[t.Day()]
			dowMatch := dowSet[int(t.Weekday())]
			// Standard cron OR semantics when both day fields are restricted.
			var dayOK bool
			switch {
			case domRestricted && dowRestricted:
				dayOK = domMatch || dowMatch
			case domRestricted:
				dayOK = domMatch
			case dowRestricted:
				dayOK = dowMatch
			default:
				dayOK = true
			}
			if dayOK {
				return t, nil
			}
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching time found for cron %q within a year", expr)
}

// cronField parses one cron field into a set of allowed integer values.
func cronField(field string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		rangePart := part
		if slash := strings.Index(part, "/"); slash >= 0 {
			s, err := strconv.Atoi(part[slash+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step in %q", part)
			}
			step = s
			rangePart = part[:slash]
		}

		lo, hi := min, max
		if rangePart != "*" {
			if dash := strings.Index(rangePart, "-"); dash >= 0 {
				a, err1 := strconv.Atoi(rangePart[:dash])
				b, err2 := strconv.Atoi(rangePart[dash+1:])
				if err1 != nil || err2 != nil {
					return nil, fmt.Errorf("invalid range %q", rangePart)
				}
				lo, hi = a, b
			} else {
				v, err := strconv.Atoi(rangePart)
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", rangePart)
				}
				lo, hi = v, v
			}
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("field value out of range [%d-%d] in %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty field %q", field)
	}
	return out, nil
}

// DescribeSchedule returns a short human-readable description for the UI/logs.
func DescribeSchedule(kind, expr string) string {
	switch strings.ToLower(kind) {
	case "interval":
		if secs, err := strconv.Atoi(strings.TrimSpace(expr)); err == nil {
			d := time.Duration(secs) * time.Second
			return "Every " + d.String()
		}
		return "Every " + expr + "s"
	case "daily":
		return "Daily at " + expr
	case "cron":
		return "Cron: " + expr
	}
	return kind + " " + expr
}
