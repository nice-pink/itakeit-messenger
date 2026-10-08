package classify

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// when is the time phrase of a reminder as the model copies it from the message. The
// model never computes a date: small models get calendar arithmetic wrong ("next
// Wednesday" three days off), so it only fills these fields and resolve does the
// arithmetic. The fields are not posted anywhere, so they are not scrubbed.
type when struct {
	in         int
	unit       string // none, minutes, hours, days, weeks, months
	weekday    string // none, monday ... sunday
	week       string // none, this_week, next_week
	month, day int    // 0 when the message names no calendar date
	clock      string // HH:MM, empty when the message names no time of day
}

// defaultHour is the time of a reminder whose message names a day but no time.
const defaultHour = 9

// maxIn bounds a distance per unit before any arithmetic, a little past maxLead (120
// days): the schema cannot bound an integer, and a huge one from a hostile message
// would overflow time.Duration or AddDate into a time inside the accepted window.
var maxIn = map[string]int{"minutes": 200_000, "hours": 3_000, "days": 400, "weeks": 60, "months": 14}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

func none(s string) bool { return s == "" || s == "none" }

// resolve turns the phrase into an instant in loc, relative to now. The rules:
//
//   - a distance in minutes or hours is exact from now and ignores any clock time;
//     days, weeks and months are calendar days (a month clamps to the month's end)
//     at the stated time, or defaultHour;
//   - a weekday alone is the next such day after today, counted as today only when the
//     time on it is still ahead ("Friday 18:00" said on Friday morning); next_week is
//     that weekday in the following Monday to Sunday week, this_week the same day in the
//     current one, or the next such day when it is already past;
//   - a month and day is the next such date, this year or next;
//   - a clock time alone is today if still ahead, else tomorrow.
//
// A phrase that mixes a distance, a weekday and a date, names no day or time, or holds
// an impossible or absurd value is an error, and the caller drops the reminder.
func (w when) resolve(now time.Time, loc *time.Location) (time.Time, error) {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	hour, minute, hasClock, err := parseClock(w.clock)
	if err != nil {
		return time.Time{}, err
	}
	at := func(d time.Time) time.Time {
		h, m := defaultHour, 0
		if hasClock {
			h, m = hour, minute
		}
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
	}
	if w.in < 0 {
		return time.Time{}, fmt.Errorf("distance %d %s", w.in, w.unit)
	}
	// ahead is what due accepts: not earlier than minLead from now.
	ahead := func(t time.Time) bool { return !t.Before(now.Add(minLead)) }
	// A unit without an amount is the model filling a field it should leave at none, which
	// a constrained decoder makes it do; it is no distance, not an error.
	rel, wd, wk, date := !none(w.unit) && w.in > 0, !none(w.weekday), !none(w.week), w.month != 0 || w.day != 0
	switch {
	case rel && (wd || wk || date), wd && date:
		return time.Time{}, errors.New("a distance, a weekday and a date cannot be combined")
	case rel:
		if limit, ok := maxIn[w.unit]; !ok || w.in > limit {
			return time.Time{}, fmt.Errorf("distance %d %s", w.in, w.unit)
		}
		switch w.unit {
		case "minutes":
			return ceilMinute(now.Add(time.Duration(w.in) * time.Minute)), nil
		case "hours":
			return ceilMinute(now.Add(time.Duration(w.in) * time.Hour)), nil
		case "days":
			return at(today.AddDate(0, 0, w.in)), nil
		case "weeks":
			return at(today.AddDate(0, 0, 7*w.in)), nil
		case "months":
			return at(addMonths(today, w.in)), nil
		}
		return time.Time{}, fmt.Errorf("unit %q", w.unit)
	case wd:
		target, ok := weekdays[strings.ToLower(w.weekday)]
		if !ok {
			return time.Time{}, fmt.Errorf("weekday %q", w.weekday)
		}
		monday := today.AddDate(0, 0, -isoOffset(today.Weekday()))
		switch w.week {
		case "next_week":
			return at(monday.AddDate(0, 0, 7+isoOffset(target))), nil
		case "this_week":
			// A reminder cannot be in the past, so a weekday already gone this week is
			// the next one. Models also write this_week for a plain "Montag".
			d := monday.AddDate(0, 0, isoOffset(target))
			if d.Before(today) || !ahead(at(d)) {
				d = d.AddDate(0, 0, 7)
			}
			return at(d), nil
		case "", "none":
		default:
			return time.Time{}, fmt.Errorf("week %q", w.week)
		}
		d := today.AddDate(0, 0, (int(target)-int(today.Weekday())+7)%7)
		if !ahead(at(d)) {
			d = d.AddDate(0, 0, 7)
		}
		return at(d), nil
	case wk:
		return time.Time{}, errors.New("a week without a weekday")
	case date:
		if w.month < 1 || w.month > 12 || w.day < 1 || w.day > 31 {
			return time.Time{}, fmt.Errorf("date %d-%d", w.month, w.day)
		}
		for _, year := range []int{today.Year(), today.Year() + 1} {
			d := time.Date(year, time.Month(w.month), w.day, 0, 0, 0, 0, loc)
			if d.Month() == time.Month(w.month) && !d.Before(today) {
				return at(d), nil
			}
		}
		return time.Time{}, fmt.Errorf("no such date %d-%d", w.month, w.day)
	case hasClock:
		if t := at(today); ahead(t) {
			return t, nil
		}
		return at(today.AddDate(0, 0, 1)), nil
	}
	return time.Time{}, errors.New("no day or time")
}

// ceilMinute rounds up to a whole minute, never down: rounding "in 1 minute" down from
// 10:00:30 gives 10:01:00, which is under the lead due requires and drops the reminder.
func ceilMinute(t time.Time) time.Time {
	if r := t.Truncate(time.Minute); r.Before(t) {
		return r.Add(time.Minute)
	}
	return t
}

// isoOffset counts days since Monday.
func isoOffset(d time.Weekday) int { return (int(d) + 6) % 7 }

// addMonths adds n months, clamping to the last day of the month (31 Jan + 1 month is
// 28 Feb, not 3 Mar).
func addMonths(d time.Time, n int) time.Time {
	t := d.AddDate(0, n, 0)
	if t.Day() != d.Day() {
		t = time.Date(t.Year(), t.Month(), 0, 0, 0, 0, 0, d.Location())
	}
	return t
}

func parseClock(s string) (h, m int, ok bool, err error) {
	if s = strings.TrimSpace(s); s == "" {
		return 0, 0, false, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, false, fmt.Errorf("time %q: use HH:MM", s)
	}
	return t.Hour(), t.Minute(), true, nil
}
