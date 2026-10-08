package classify

import (
	"math"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	d := func(m time.Month, day, h, min int) time.Time { return time.Date(2026, m, day, h, min, 0, 0, loc) }
	fri := d(10, 2, 15, 0) // Friday 2026-10-02 15:00
	for name, tc := range map[string]struct {
		w    when
		now  time.Time
		want time.Time
		err  bool
	}{
		"in two days, no time":                       {when{in: 2, unit: "days"}, fri, d(10, 4, 9, 0), false},
		"tomorrow at 17":                             {when{in: 1, unit: "days", clock: "17:00"}, fri, d(10, 3, 17, 0), false},
		"in two hours rounds up to the minute":       {when{in: 2, unit: "hours", clock: "08:00"}, fri.Add(7 * time.Second), d(10, 2, 17, 1), false},
		"in 90 minutes":                              {when{in: 90, unit: "minutes"}, fri, d(10, 2, 16, 30), false},
		"in two weeks":                               {when{in: 2, unit: "weeks"}, fri, d(10, 16, 9, 0), false},
		"in a month":                                 {when{in: 1, unit: "months"}, fri, d(11, 2, 9, 0), false},
		"31 Jan plus a month clamps":                 {when{in: 1, unit: "months"}, time.Date(2026, 1, 31, 8, 0, 0, 0, loc), d(2, 28, 9, 0), false},
		"zero distance alone":                        {when{in: 0, unit: "days"}, fri, time.Time{}, true},
		"zero distance with clock":                   {when{in: 0, unit: "minutes", clock: "16:00"}, fri, d(10, 2, 16, 0), false},
		"zero distance with day":                     {when{in: 0, unit: "weeks", weekday: "wednesday", week: "next_week"}, fri, d(10, 7, 9, 0), false},
		"negative distance":                          {when{in: -1, unit: "days"}, fri, time.Time{}, true},
		"unknown unit":                               {when{in: 1, unit: "fortnights"}, fri, time.Time{}, true},
		"wednesday":                                  {when{weekday: "wednesday"}, fri, d(10, 7, 9, 0), false},
		"friday said on friday":                      {when{weekday: "friday"}, fri, d(10, 9, 9, 0), false},
		"friday 18:00 on friday":                     {when{weekday: "friday", clock: "18:00"}, fri, d(10, 2, 18, 0), false},
		"sunday":                                     {when{weekday: "sunday"}, fri, d(10, 4, 9, 0), false},
		"monday next week":                           {when{weekday: "monday", week: "next_week"}, fri, d(10, 5, 9, 0), false},
		"sunday next week":                           {when{weekday: "sunday", week: "next_week"}, fri, d(10, 11, 9, 0), false},
		"this week sunday":                           {when{weekday: "sunday", week: "this_week"}, fri, d(10, 4, 9, 0), false},
		"this week thursday passed is next thursday": {when{weekday: "thursday", week: "this_week"}, fri, d(10, 8, 9, 0), false},
		"week without weekday":                       {when{week: "next_week"}, fri, time.Time{}, true},
		"bad week":                                   {when{weekday: "monday", week: "soon"}, fri, time.Time{}, true},
		"bad weekday":                                {when{weekday: "someday"}, fri, time.Time{}, true},
		"date this year":                             {when{month: 11, day: 14}, fri, d(11, 14, 9, 0), false},
		"date passed rolls over":                     {when{month: 10, day: 1}, fri, time.Date(2027, 10, 1, 9, 0, 0, 0, loc), false},
		"today as a date":                            {when{month: 10, day: 2, clock: "18:00"}, fri, d(10, 2, 18, 0), false},
		"30 February":                                {when{month: 2, day: 30}, fri, time.Time{}, true},
		"month 13":                                   {when{month: 13, day: 1}, fri, time.Time{}, true},
		"day without month":                          {when{day: 5}, fri, time.Time{}, true},
		"clock ahead today":                          {when{clock: "16:00"}, fri, d(10, 2, 16, 0), false},
		"clock passed is tomorrow":                   {when{clock: "14:00"}, fri, d(10, 3, 14, 0), false},
		"single digit hour":                          {when{clock: "9:30"}, fri, d(10, 3, 9, 30), false},
		"hour 24":                                    {when{clock: "24:00"}, fri, time.Time{}, true},
		"text for a time":                            {when{clock: "noon"}, fri, time.Time{}, true},
		"distance and weekday":                       {when{in: 1, unit: "days", weekday: "monday"}, fri, time.Time{}, true},
		"nothing":                                    {when{unit: "none", weekday: "none", week: "none"}, fri, time.Time{}, true},
		"overflow hours":                             {when{in: 1<<51 + 1, unit: "hours"}, fri, time.Time{}, true},
		"overflow minutes":                           {when{in: 1<<62 + 5, unit: "minutes"}, fri, time.Time{}, true},
		"overflow days":                              {when{in: 1<<57 + 5, unit: "days"}, fri, time.Time{}, true},
		"overflow weeks":                             {when{in: 1<<57 + 5, unit: "weeks"}, fri, time.Time{}, true},
		"overflow months":                            {when{in: 1<<57 + 5, unit: "months"}, fri, time.Time{}, true},
		"max int":                                    {when{in: math.MaxInt, unit: "days"}, fri, time.Time{}, true},
		"largest minutes allowed":                    {when{in: 200_000, unit: "minutes"}, fri, fri.Add(200_000 * time.Minute), false},
		"one past largest minutes":                   {when{in: 200_001, unit: "minutes"}, fri, time.Time{}, true},
		"this week today clock passed rolls":         {when{weekday: "friday", week: "this_week", clock: "08:00"}, fri, d(10, 9, 8, 0), false},
		"this week today no clock rolls":             {when{weekday: "friday", week: "this_week"}, fri, d(10, 9, 9, 0), false},
		"this week today clock ahead":                {when{weekday: "friday", week: "this_week", clock: "18:00"}, fri, d(10, 2, 18, 0), false},
		"weekday and date":                           {when{weekday: "friday", month: 10, day: 20}, fri, time.Time{}, true},
		"clock exactly at the lead is accepted":      {when{clock: "15:01"}, fri, d(10, 2, 15, 1), false},
		"clock a minute short rolls":                 {when{clock: "15:00"}, fri, d(10, 3, 15, 0), false},
	} {
		got, err := tc.w.resolve(tc.now, loc)
		if (err != nil) != tc.err || !tc.err && !got.Equal(tc.want) {
			t.Errorf("%s: got %v, %v want %v (err %v)", name, got, err, tc.want, tc.err)
		}
	}
}

func TestResolveUsesTheZoneOfTheClock(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 10, 2, 23, 30, 0, 0, time.UTC) // already Saturday 01:30 in Berlin
	got, err := when{in: 1, unit: "days"}.resolve(now, loc)
	if want := time.Date(2026, 10, 4, 9, 0, 0, 0, loc); err != nil || !got.Equal(want) {
		t.Fatalf("got %v, %v want %v", got, err, want)
	}
}

func TestClearReminderLeavesSchemaValues(t *testing.T) {
	v := Verdict{Reminder: true, RemindIn: 3, RemindUnit: "days", RemindWeekday: "monday", RemindWeek: "next_week", RemindMonth: 1, RemindDay: 2, RemindTime: "09:00", RemindText: "x", RemindFor: "U1"}
	v.ClearReminder()
	if v.RemindUnit != "none" || v.RemindWeekday != "none" || v.RemindWeek != "none" || v.RemindIn != 0 || v.RemindTime != "" || v.Reminder || v.RemindText != "" || v.RemindFor != "" {
		t.Fatalf("%+v", v)
	}
}

func TestHugeDistanceIsDroppedByDue(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, loc)
	c := &Classifier{loc: loc}
	for _, v := range []Verdict{
		{RemindIn: 1<<51 + 1, RemindUnit: "hours", RemindText: "x"},
		{RemindIn: 1<<62 + 5, RemindUnit: "minutes", RemindText: "x"},
		{RemindIn: 1<<57 + 5, RemindUnit: "days", RemindText: "x"},
	} {
		if c.due(&v, now) {
			t.Errorf("%+v accepted as %v", v, v.Due)
		}
	}
}

func TestShortDistancesRoundUpAndSurviveDue(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	c := &Classifier{loc: loc}
	for name, tc := range map[string]struct {
		now  time.Time
		w    Verdict
		want time.Time
	}{
		"1 minute at :30 s":   {time.Date(2026, 10, 8, 10, 0, 30, 0, loc), Verdict{RemindIn: 1, RemindUnit: "minutes"}, time.Date(2026, 10, 8, 10, 2, 0, 0, loc)},
		"1 minute at :00 s":   {time.Date(2026, 10, 8, 10, 0, 0, 0, loc), Verdict{RemindIn: 1, RemindUnit: "minutes"}, time.Date(2026, 10, 8, 10, 1, 0, 0, loc)},
		"1 minute at :59.9 s": {time.Date(2026, 10, 8, 10, 0, 59, 900_000_000, loc), Verdict{RemindIn: 1, RemindUnit: "minutes"}, time.Date(2026, 10, 8, 10, 2, 0, 0, loc)},
		"1 hour at :30 s":     {time.Date(2026, 10, 8, 10, 0, 30, 0, loc), Verdict{RemindIn: 1, RemindUnit: "hours"}, time.Date(2026, 10, 8, 11, 1, 0, 0, loc)},
		"30 minutes at :00 s": {time.Date(2026, 10, 8, 10, 0, 0, 0, loc), Verdict{RemindIn: 30, RemindUnit: "minutes"}, time.Date(2026, 10, 8, 10, 30, 0, 0, loc)},
	} {
		v := tc.w
		v.RemindText = "x"
		if !c.due(&v, tc.now) || !v.Due.Equal(tc.want) {
			t.Errorf("%s: due=%v want %v", name, v.Due, tc.want)
		}
	}
}
