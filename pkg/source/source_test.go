package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nice-pink/itakeit-messenger/pkg/config"
	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
	"github.com/slack-go/slack/slackevents"
)

func TestSlackAccept(t *testing.T) {
	cfg := &config.Config{TargetChannel: "CT", Sources: config.Sources{Slack: config.Slack{Enabled: true}}}
	s := &Slack{Cfg: cfg, UserID: "UB", BotID: "BB"}
	base := slackevents.MessageEvent{User: "U1", Text: "hello there", Channel: "C1", ChannelType: "channel", TimeStamp: "1.1"}
	for name, tc := range map[string]struct {
		mod func(*slackevents.MessageEvent)
		ok  bool
	}{
		"plain":       {func(*slackevents.MessageEvent) {}, true},
		"reply":       {func(e *slackevents.MessageEvent) { e.ThreadTimeStamp = "0.9" }, false},
		"thread root": {func(e *slackevents.MessageEvent) { e.ThreadTimeStamp = "1.1" }, true},
		"file share":  {func(e *slackevents.MessageEvent) { e.SubType = "file_share" }, true},
		"edit":        {func(e *slackevents.MessageEvent) { e.SubType = "message_changed" }, false},
		"private":     {func(e *slackevents.MessageEvent) { e.ChannelType = "group" }, false},
		"target":      {func(e *slackevents.MessageEvent) { e.Channel = "CT" }, false},
		"own":         {func(e *slackevents.MessageEvent) { e.User = "UB" }, false},
		"other bot":   {func(e *slackevents.MessageEvent) { e.BotID = "B2"; e.SubType = "bot_message" }, false},
		"dm":          {func(e *slackevents.MessageEvent) { e.ChannelType = "im" }, false},
	} {
		ev := base
		tc.mod(&ev)
		if _, ok := s.Accept(&ev); ok != tc.ok {
			t.Errorf("%s: accepted=%v want %v", name, ok, tc.ok)
		}
	}
	m, _ := s.Accept(&base)
	if m.User != "U1" || m.Thread == nil || m.Thread.Channel != "C1" || m.Thread.TS != "1.1" {
		t.Errorf("reminder target: %+v", m)
	}
	cfg.Sources.Slack.IncludePrivate, cfg.Sources.Slack.IncludeBots = true, true
	for _, mod := range []func(*slackevents.MessageEvent){
		func(e *slackevents.MessageEvent) { e.ChannelType = "group" },
		func(e *slackevents.MessageEvent) { e.BotID = "B2"; e.SubType = "bot_message"; e.User = "" },
	} {
		ev := base
		mod(&ev)
		if _, ok := s.Accept(&ev); !ok {
			t.Errorf("opt-in not honoured: %+v", ev)
		}
	}
}

type fakeSink struct{ got []messenger.Message }

func (f *fakeSink) Handle(_ context.Context, m messenger.Message) (messenger.Decision, error) {
	f.got = append(f.got, m)
	return messenger.Decision{Task: true, Posted: true}, nil
}
func (f *fakeSink) Enqueue(m messenger.Message) { f.Handle(context.Background(), m) }

func TestHTTP(t *testing.T) {
	sink := &fakeSink{}
	h := (&HTTP{Token: "0123456789abcdef"}).Handler(sink)
	do := func(auth, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/messages", strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do("", `{"text":"x"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", w.Code)
	}
	if w := do("Bearer wrong", `{"text":"x"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do("Bearer 0123456789abcdef", `{"text":"  "}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty text: %d", w.Code)
	}
	w := do("Bearer 0123456789abcdef", `{"text":"deploy broke","source":"nagios","url":"https://n/1"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"posted":true`) {
		t.Fatalf("ok: %d %s", w.Code, w.Body)
	}
	m := sink.got[0]
	if m.Source != "http:nagios" || m.ID == "" || m.Link() != "https://n/1" {
		t.Fatalf("message: %+v", m)
	}
}

func TestStdinSkipsHugeLine(t *testing.T) {
	sink := &fakeSink{}
	in := "first line long enough\n" + strings.Repeat("x", 3*maxBody) + "\nlast line long enough\n"
	if err := (&Stdin{R: strings.NewReader(in)}).Run(context.Background(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.got) != 3 || len(sink.got[1].Text) != maxBody || sink.got[2].Text != "last line long enough" {
		t.Fatalf("got %d messages", len(sink.got))
	}
}

type busySink struct{ fakeSink }

func (busySink) Handle(context.Context, messenger.Message) (messenger.Decision, error) {
	return messenger.Decision{}, messenger.ErrInFlight
}

func TestHTTPInFlightIsRetryable(t *testing.T) {
	h := (&HTTP{Token: "0123456789abcdef"}).Handler(&busySink{})
	r := httptest.NewRequest("POST", "/messages", strings.NewReader(`{"text":"x"}`))
	r.Header.Set("Authorization", "Bearer 0123456789abcdef")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("code %d", w.Code)
	}
}
