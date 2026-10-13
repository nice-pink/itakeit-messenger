package classify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func gated(o Odds, err error, calls *int, system *string) (*Classifier, Ask) {
	ask := func(_ context.Context, sys, _ string, dest any) error {
		*calls++
		*system = sys
		*dest.(*Verdict) = Verdict{Task: true, Title: "t", Summary: "s", Reminder: true, RemindWeekday: "friday", RemindText: "x"}
		return nil
	}
	c := New(ask, "Bugs count.").WithReminders(time.UTC).WithGate(func(context.Context, string, string, bool, bool) (Odds, error) { return o, err }, 0.5, 0.5, false)
	c.Now = func() time.Time { return time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC) }
	return c, ask
}

func TestGate(t *testing.T) {
	for name, tc := range map[string]struct {
		odds                     Odds
		err                      error
		calls                    int
		task, reminder, taskText bool
	}{
		"below both":    {Odds{0.1, 0.2}, nil, 0, false, false, false},
		"task only":     {Odds{0.9, 0.2}, nil, 1, true, false, true},
		"reminder only": {Odds{0.1, 0.9}, nil, 1, false, true, false},
		"at threshold":  {Odds{0.5, 0.5}, nil, 1, true, true, true},
		"gate down":     {Odds{}, errors.New("boom"), 1, true, true, true},
	} {
		var calls int
		var system string
		c, _ := gated(tc.odds, tc.err, &calls, &system)
		v, err := c.Classify(context.Background(), "x", "U1", "o")
		if err != nil || calls != tc.calls || v.Task != tc.task || v.Reminder != tc.reminder {
			t.Errorf("%s: calls=%d v=%+v err=%v", name, calls, v, err)
		}
		if tc.calls > 0 && (strings.Contains(system, "Bugs count.") != tc.taskText || strings.Contains(system, "remind_weekday") != tc.reminder && tc.err == nil) {
			t.Errorf("%s: prompt does not match the outputs asked for: %q", name, system)
		}
		if tc.calls == 0 && !strings.Contains(v.Reason, "gate") {
			t.Errorf("%s: reason %q", name, v.Reason)
		}
	}
}

func TestJev(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"answers":{"task":{"type":"noul","noul":0.8},"reminder":{"type":"noul","noul":0.1}}}`))
	}))
	defer srv.Close()
	g := NewJev(srv.URL, "jev-latest", "k")
	o, err := g(context.Background(), "please fix the build", "def", true, true)
	if err != nil || o != (Odds{0.8, 0.1}) || auth != "Bearer k" || got["state"] != "please fix the build" || got["model"] != "jev-latest" {
		t.Fatalf("o=%+v err=%v auth=%q body=%v", o, err, auth, got)
	}
	if qs := got["questions"].(map[string]any); len(qs) != 2 {
		t.Fatalf("questions %v", qs)
	}
	if _, err := g(context.Background(), "x", "def", true, true); err != nil {
		t.Fatal(err)
	}
	o, err = g(context.Background(), "x", "def", false, true)
	if qs := got["questions"].(map[string]any); len(qs) != 1 || err != nil || o.Task != 0 {
		t.Fatalf("only the reminder: %v %+v %v", qs, o, err)
	}
}

func TestJevErrors(t *testing.T) {
	for name, body := range map[string]string{"status": "", "missing": `{"answers":{}}`, "range": `{"answers":{"task":{"noul":1.5}}}`, "junk": "x"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if name == "status" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(body))
		}))
		if _, err := NewJev(srv.URL, "m", "k")(context.Background(), "x", "d", true, false); err == nil {
			t.Errorf("%s: want error", name)
		}
		srv.Close()
	}
}

func TestGateTrust(t *testing.T) {
	var system string
	ask := func(_ context.Context, sys, _ string, dest any) error {
		system = sys
		*dest.(*Verdict) = Verdict{Title: "t", Summary: "s", RemindWeekday: "friday", RemindText: "x"}
		return nil
	}
	gate := func(context.Context, string, string, bool, bool) (Odds, error) { return Odds{0.9, 0.9}, nil }
	c := New(ask, "").WithReminders(time.UTC).WithGate(gate, 0.5, 0.5, true)
	c.Now = func() time.Time { return time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC) }
	v, err := c.Classify(context.Background(), "x", "U1", "o")
	if err != nil || !v.Task || !v.Reminder || !strings.Contains(system, "already decided") || strings.Contains(system, "You decide whether") {
		t.Fatalf("v=%+v err=%v system=%q", v, err, system)
	}
	c = New(ask, "").WithGate(gate, 0.5, 0.5, true)
	if v, _ := c.Classify(context.Background(), "x", "U1", "o"); !v.Task || v.Reminder {
		t.Fatalf("v=%+v", v)
	}
	c = New(func(context.Context, string, string, any) error { return nil }, "").WithGate(gate, 0.5, 0.5, true)
	if _, err := c.Classify(context.Background(), "x", "U1", "o"); err == nil {
		t.Fatal("a trusted task without title and summary must be an error")
	}
}

func TestGateRaw(t *testing.T) {
	var calls int
	var system string
	ask := func(_ context.Context, sys, _ string, dest any) error {
		calls++
		system = sys
		*dest.(*Verdict) = Verdict{Task: true, Title: "model", Summary: "model", Reminder: true, RemindWeekday: "friday", RemindText: "model text"}
		return nil
	}
	text := "Fix the build\nCI fails on main since the last merge."
	mk := func(o Odds) *Classifier {
		c := New(ask, "").WithReminders(time.UTC).WithoutRephrase().WithGate(func(context.Context, string, string, bool, bool) (Odds, error) { return o, nil }, 0.5, 0.5, false)
		c.Now = func() time.Time { return time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC) }
		return c
	}
	v, err := mk(Odds{0.9, 0.1}).Classify(context.Background(), text, "U1", "o")
	if err != nil || calls != 0 || !v.Task || v.Title != "Fix the build" || v.Summary != text || v.Reminder {
		t.Fatalf("task only: calls=%d v=%+v err=%v", calls, v, err)
	}
	v, err = mk(Odds{0.9, 0.9}).Classify(context.Background(), text, "U1", "o")
	if err != nil || calls != 1 || !v.Task || v.Title != "Fix the build" || !v.Reminder || v.RemindText != text || !strings.Contains(system, "always set task to false") {
		t.Fatalf("both: calls=%d v=%+v err=%v system=%q", calls, v, err, system)
	}
	v, err = mk(Odds{0.1, 0.9}).Classify(context.Background(), text, "U1", "o")
	if err != nil || calls != 2 || v.Task || !v.Reminder || v.RemindText != text {
		t.Fatalf("reminder only: calls=%d v=%+v err=%v", calls, v, err)
	}
	long := strings.Repeat("a", 200) + "\n" + strings.Repeat("b", 3000)
	if v := rawTaskVerdict(long); len([]rune(v.Title)) != maxTitleRunes || len([]rune(v.Summary)) != maxSummaryRunes {
		t.Fatalf("not cut: %d %d", len([]rune(v.Title)), len([]rune(v.Summary)))
	}
}
