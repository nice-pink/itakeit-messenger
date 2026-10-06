package messenger

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nice-pink/itakeit-messenger/pkg/classify"
)

type fakePoster struct {
	mu    sync.Mutex
	posts []string
	keys  []string
	err   error
}

func (f *fakePoster) Post(_ context.Context, channel, text, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.posts = append(f.posts, channel+"|"+text)
	f.keys = append(f.keys, key)
	return "900.1", nil
}

type fakeSched struct {
	got      []string
	confirms []string
	err      error
	noID     bool
}

func (f *fakeSched) Schedule(_ context.Context, t Thread, at time.Time, text string) (string, error) {
	f.got = append(f.got, t.Channel+"/"+t.TS+"|"+at.Format("2006-01-02T15:04")+"|"+text)
	if f.noID {
		return "", f.err
	}
	return "Q1", f.err
}

func (f *fakeSched) Confirm(_ context.Context, t Thread, id, text string, users []string) error {
	f.confirms = append(f.confirms, t.Channel+"/"+t.TS+"|"+id+"|"+text+"|"+strings.Join(users, ","))
	return nil
}

func newPipe(v classify.Verdict, askErr error) (*Pipeline, *fakePoster) {
	ask := func(_ context.Context, _, _ string, dest any) error {
		*dest.(*classify.Verdict) = v
		return askErr
	}
	f := &fakePoster{}
	return New(classify.New(ask, ""), f, "CT", 5, 2), f
}

var task = classify.Verdict{Task: true, Title: "Fix *the* build", Summary: "CI fails <@U1> @channel & more", Reason: "ask"}

func TestPostsTask(t *testing.T) {
	p, f := newPipe(task, nil)
	d, err := p.Handle(context.Background(), Message{Source: "s", ID: "1", Text: "please fix the build", Origin: "<#C1>", Link: func() string { return "https://x.slack.com/archives/C1/p1" }})
	if err != nil || !d.Posted || !d.Task {
		t.Fatalf("d=%+v err=%v", d, err)
	}
	got := f.posts[0]
	for _, want := range []string{"CT|*Fix the build*", "&lt;@U1&gt;", "&amp; more", "<https://x.slack.com/archives/C1/p1|a message> in <#C1>"} {
		if !strings.Contains(got, want) {
			t.Errorf("post %q lacks %q", got, want)
		}
	}
}

func TestNotATaskIsNotPosted(t *testing.T) {
	p, f := newPipe(classify.Verdict{Reason: "chat"}, nil)
	d, _ := p.Handle(context.Background(), Message{Source: "s", ID: "1", Text: "thanks a lot everyone"})
	if d.Task || d.Posted || len(f.posts) != 0 {
		t.Fatalf("d=%+v posts=%v", d, f.posts)
	}
}

func TestDuplicateAndShort(t *testing.T) {
	p, f := newPipe(task, nil)
	m := Message{Source: "s", ID: "1", Text: "please fix the build"}
	p.Handle(context.Background(), m)
	d, _ := p.Handle(context.Background(), m)
	if !d.Duplicate || len(f.posts) != 1 {
		t.Fatalf("duplicate posted: d=%+v posts=%d", d, len(f.posts))
	}
	if d, _ := p.Handle(context.Background(), Message{Source: "s", ID: "2", Text: "ok"}); !d.Skipped {
		t.Fatal("short message must be skipped")
	}
}

func TestFailureAllowsRetry(t *testing.T) {
	p, f := newPipe(task, nil)
	f.err = errors.New("slack down")
	m := Message{Source: "s", ID: "1", Text: "please fix the build"}
	if _, err := p.Handle(context.Background(), m); err == nil {
		t.Fatal("want post error")
	}
	f.err = nil
	if d, err := p.Handle(context.Background(), m); err != nil || !d.Posted {
		t.Fatalf("retry: d=%+v err=%v", d, err)
	}
}

func TestSeenRingEvicts(t *testing.T) {
	p, _ := newPipe(task, nil)
	for i := range seenSize + 1 {
		p.claim(string(rune(i+1)), false)
	}
	if len(p.seen) != seenSize || p.claim(string(rune(1)), false) != claimNew {
		t.Fatalf("seen=%d", len(p.seen))
	}
}

func TestUnsafeLinkIsDropped(t *testing.T) {
	s := Format(task, Message{Origin: "web", Link: func() string { return "javascript:x|y" }}, false)
	if strings.Contains(s, "javascript") || !strings.HasSuffix(s, "From web") {
		t.Fatal(s)
	}
}

func TestSeedStopsRepost(t *testing.T) {
	p, f := newPipe(task, nil)
	m := Message{Source: "slack", ID: "C1:1.1", Text: "please fix the build"}
	if _, err := p.Handle(context.Background(), m); err != nil || len(f.keys) != 1 {
		t.Fatalf("keys=%v err=%v", f.keys, err)
	}
	restarted, f2 := newPipe(task, nil)
	restarted.Seed(f.keys)
	if d, _ := restarted.Handle(context.Background(), m); !d.Duplicate || len(f2.posts) != 0 {
		t.Fatalf("seeded key was posted again: d=%+v", d)
	}
}

var due = time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)

func remindPipe(v classify.Verdict) (*Pipeline, *fakePoster, *fakeSched) {
	v.Reminder, v.RemindAt, v.RemindText = true, "2026-10-09T09:00", "send <b>invoice</b>"
	ask := func(_ context.Context, _, _ string, dest any) error {
		*dest.(*classify.Verdict) = v
		return nil
	}
	cl := classify.New(ask, "").WithReminders(time.UTC)
	cl.Now = func() time.Time { return due.Add(-7 * 24 * time.Hour) }
	f, s := &fakePoster{}, &fakeSched{}
	p := New(cl, f, "CT", 5, 2)
	p.Scheduler = s
	p.Now = cl.Now
	return p, f, s
}

func TestReminderUnderSourceMessage(t *testing.T) {
	p, f, s := remindPipe(classify.Verdict{})
	d, err := p.Handle(context.Background(), Message{Source: "slack", ID: "1", Text: "remind me friday to send the invoice", User: "U1", Thread: &Thread{Channel: "C1", TS: "1.1"}})
	if err != nil || !d.Reminder || d.Posted || len(f.posts) != 0 {
		t.Fatalf("d=%+v err=%v posts=%v", d, err, f.posts)
	}
	if want := "C1/1.1|2026-10-09T09:00|Reminder for <@U1>: send &lt;b&gt;invoice&lt;/b&gt;"; s.got[0] != want {
		t.Fatalf("got %q want %q", s.got[0], want)
	}
}

func TestReminderConfirmedInThread(t *testing.T) {
	m := Message{Source: "slack", ID: "1", Text: "remind me friday to send the invoice", Author: "U1", User: "U1", Thread: &Thread{Channel: "C1", TS: "1.1"}}
	p, _, s := remindPipe(classify.Verdict{RemindFor: "U1"})
	if _, err := p.Handle(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if want := "C1/1.1|Q1|Reminder scheduled for Fri 9 Oct 09:00 UTC: send &lt;b&gt;invoice&lt;/b&gt;|U1"; s.confirms[0] != want {
		t.Fatalf("got %q want %q", s.confirms[0], want)
	}
	p, _, s = remindPipe(classify.Verdict{RemindFor: "U2"})
	m.ID, m.Text = "2", "remind <@U2> friday to send the invoice"
	if _, err := p.Handle(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if want := "Reminder for <@U2> scheduled for Fri 9 Oct 09:00 UTC: send &lt;b&gt;invoice&lt;/b&gt;|U2,U1"; !strings.HasSuffix(s.confirms[0], want) || !strings.HasPrefix(s.got[0], "C1/1.1|2026-10-09T09:00|Reminder for <@U2>:") {
		t.Fatalf("scheduled %q confirmed %q", s.got[0], s.confirms[0])
	}
}

func TestBotReminderGoesToContact(t *testing.T) {
	m := Message{Source: "slack", ID: "1", Text: "check the deploy friday", Author: "B2", User: "UC", Thread: &Thread{Channel: "C1", TS: "1.1"}}
	p, _, s := remindPipe(classify.Verdict{RemindFor: "B2"})
	if _, err := p.Handle(context.Background(), m); err != nil || !strings.Contains(s.got[0], "Reminder for <@UC>:") || !strings.HasSuffix(s.confirms[0], "|UC") {
		t.Fatalf("err %v got %v confirms %v", err, s.got, s.confirms)
	}
	p, _, s = remindPipe(classify.Verdict{RemindFor: "B2"})
	m.ID, m.User = "2", ""
	if _, err := p.Handle(context.Background(), m); err != nil || !strings.Contains(s.got[0], "|Reminder: ") || !strings.HasSuffix(s.confirms[0], "|") {
		t.Fatalf("no contact: nobody is mentioned and nobody may delete: err %v got %v confirms %v", err, s.got, s.confirms)
	}
}

func TestNoConfirmationForAlreadyScheduledReminder(t *testing.T) {
	p, _, s := remindPipe(classify.Verdict{})
	s.noID = true
	d, err := p.Handle(context.Background(), Message{Source: "slack", ID: "1", Text: "remind me friday to send the invoice", User: "U1", Thread: &Thread{Channel: "C1", TS: "1.1"}})
	if err != nil || !d.Reminder || len(s.confirms) != 0 {
		t.Fatalf("d=%+v err=%v confirms=%v", d, err, s.confirms)
	}
}

func TestTaskWithReminderFromHTTPPostsOnlyTheTask(t *testing.T) {
	p, f, s := remindPipe(task)
	d, err := p.Handle(context.Background(), Message{Source: "http", ID: "1", Text: "please fix the build friday"})
	if err != nil || !d.Posted || d.Reminder || len(s.got) != 0 || len(f.posts) != 1 {
		t.Fatalf("a reply under the task would make itakeit see the reporter answer: d=%+v err=%v sched=%v", d, err, s.got)
	}
}

func TestReminderWithoutThreadIsDropped(t *testing.T) {
	p, _, s := remindPipe(classify.Verdict{})
	d, err := p.Handle(context.Background(), Message{Source: "http", ID: "1", Text: "remind me friday to send the invoice"})
	if err != nil || d.Reminder || len(s.got) != 0 {
		t.Fatalf("d=%+v err=%v", d, err)
	}
}

func TestReminderFailure(t *testing.T) {
	p, _, s := remindPipe(task)
	s.err = errors.New("slack down")
	m := Message{Source: "slack", ID: "1", Text: "please fix the build friday", Thread: &Thread{Channel: "C1", TS: "1.1"}}
	if d, err := p.Handle(context.Background(), m); err != nil || !d.Posted || d.Reminder {
		t.Fatalf("task posted: a failed reminder must not fail the message: d=%+v err=%v", d, err)
	}
	p2, _, s2 := remindPipe(classify.Verdict{})
	s2.err = errors.New("slack down")
	if _, err := p2.Handle(context.Background(), m); err == nil {
		t.Fatal("reminder-only failure must be an error so it can be retried")
	}
}

func TestRemindersOffWithoutScheduler(t *testing.T) {
	p, _, s := remindPipe(classify.Verdict{})
	p.Scheduler = nil
	if d, _ := p.Handle(context.Background(), Message{Source: "slack", ID: "1", Text: "remind me friday to send the invoice", Thread: &Thread{}}); d.Reminder || len(s.got) != 0 {
		t.Fatal("no scheduler, no reminder")
	}
}

func TestReminderDueDuringClassification(t *testing.T) {
	p, _, s := remindPipe(classify.Verdict{})
	p.Now = func() time.Time { return due.Add(time.Second) }
	d, err := p.Handle(context.Background(), Message{Source: "slack", ID: "1", Text: "remind me friday to send the invoice", Thread: &Thread{Channel: "C1", TS: "1.1"}})
	if err != nil || d.Reminder || len(s.got) != 0 {
		t.Fatalf("a time that passed must be dropped, not sent to Slack: d=%+v err=%v", d, err)
	}
}

func TestRetryWhileInFlight(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{})
	ask := func(_ context.Context, _, _ string, dest any) error {
		started <- struct{}{}
		<-release
		return errors.New("boom")
	}
	p := New(classify.New(ask, ""), &fakePoster{}, "CT", 5, 2)
	m := Message{Source: "http", ID: "1", Text: "please fix the build"}
	first := make(chan error, 1)
	go func() { _, err := p.Handle(context.Background(), m); first <- err }()
	<-started
	if _, err := p.Handle(context.Background(), m); !errors.Is(err, ErrInFlight) {
		t.Fatalf("retry during the first call must be told to retry later, got %v", err)
	}
	close(release)
	if err := <-first; err == nil {
		t.Fatal("first call must fail")
	}
	p.cl = classify.New(func(_ context.Context, _, _ string, dest any) error { return nil }, "")
	if d, err := p.Handle(context.Background(), m); err != nil || d.Duplicate {
		t.Fatalf("after the failure the retry must be handled: d=%+v err=%v", d, err)
	}
}

func TestForgetFreesRingSlot(t *testing.T) {
	p, _ := newPipe(task, nil)
	p.claim("a", true)
	p.forget("a")
	p.claim("a", false)
	for i := range seenSize - 1 {
		p.claim(string(rune(i+100)), false)
	}
	if p.claim("a", false) != claimDone {
		t.Fatal("a re-claimed key was evicted by its old ring slot")
	}
}

func TestKeyIsFixedLengthAndPrintable(t *testing.T) {
	k := key(Message{Source: "stdin", ID: strings.Repeat("x\x00y", 30000)})
	if len(k) != 64 || strings.ContainsAny(k, "\x00 ") {
		t.Fatalf("key %q", k)
	}
}

func TestWhen(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, berlin)
	for in, want := range map[time.Time]string{
		time.Date(2026, 10, 9, 10, 0, 0, 0, berlin):  "Fri 10:00 CEST",
		time.Date(2026, 10, 30, 10, 0, 0, 0, berlin): "Fri 30 Oct 10:00 CET",
	} {
		if got := when(in, now); got != want {
			t.Errorf("when = %q, want %q", got, want)
		}
	}
}

func TestTaskMentionsAuthorOnRequest(t *testing.T) {
	m := Message{Origin: "<#C1>", User: "U1"}
	if got := Format(task, m, true); !strings.HasSuffix(got, "From <#C1> by <@U1>") {
		t.Errorf("mention: %q", got)
	}
	if got := Format(task, m, false); strings.Contains(got, "<@U1>") {
		t.Errorf("off by default: %q", got)
	}
	if got := Format(task, Message{Origin: "web"}, true); strings.Contains(got, " by ") {
		t.Errorf("no Slack user, nobody to mention: %q", got)
	}
}
