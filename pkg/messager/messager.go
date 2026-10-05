// Package messager turns messages from any source into task messages in the
// channel itakeit serves.
package messager

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nice-pink/itakeit-messager/pkg/classify"
)

// Message is one input text. ID must be unique within Source and stable across
// redelivery, since it is the deduplication key.
type Message struct {
	Source string
	ID     string
	Text   string
	Author string
	// Origin is posted as written: a Slack reference such as <#C0123> or text the
	// source has already passed through Escape.
	Origin string
	// User is the Slack user ID a reminder mentions. Empty for bots and other sources.
	User string
	// Thread is the Slack message a reminder is posted under. Without it the
	// message's reminder is dropped.
	Thread *Thread
	// Link is resolved only for messages that turn out to be tasks. May be nil.
	Link func() string
}

var (
	errNoThread = errors.New("no Slack message to reply under")
	errTooLate  = errors.New("due time passed while classifying")
)

type Thread struct{ Channel, TS string }

// Decision is what happened to a message.
type Decision struct {
	Task      bool   `json:"task"`
	Posted    bool   `json:"posted"`
	Reminder  bool   `json:"reminder,omitempty"`
	RemindAt  string `json:"remind_at,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
	Title     string `json:"title,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Poster is the Slack call the pipeline needs. Post stores key with the message,
// out of sight, so Recent can return it after a restart.
type Poster interface {
	Post(ctx context.Context, channel, text, key string) (ts string, err error)
}

// Scheduler posts text as a reply under thread at the given time.
type Scheduler interface {
	Schedule(ctx context.Context, t Thread, at time.Time, text string) error
}

// Recent lists the keys of the last n tasks this app posted to channel.
type Recent interface {
	Recent(ctx context.Context, channel string, n int) ([]string, error)
}

// Source reads messages until ctx ends and hands each to sink.
type Source interface {
	Name() string
	Run(ctx context.Context, sink Sink) error
}

// Sink takes a message from a source. Handle waits for the verdict. Enqueue
// returns at once and drops the message when the queue is full.
type Sink interface {
	Handle(ctx context.Context, m Message) (Decision, error)
	Enqueue(m Message)
}

type Pipeline struct {
	cl      *classify.Classifier
	poster  Poster
	channel string
	minLen  int
	sem     chan struct{}
	queue   chan Message
	ctx     context.Context
	// Scheduler is nil when reminders are off.
	Scheduler Scheduler
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]*entry
	ring []string
	next int
}

// entry is a remembered key: its ring slot, and whether the message is still
// being processed.
type entry struct {
	slot     int
	inflight bool
}

// ErrInFlight is returned for a message whose first delivery is still being
// processed. The caller may retry: if the first one fails, the retry is handled.
var ErrInFlight = errors.New("the same message is still being processed")

const (
	queueSize = 1024
	seenSize  = 4096
)

func New(cl *classify.Classifier, poster Poster, channel string, minChars, parallel int) *Pipeline {
	return &Pipeline{cl: cl, poster: poster, channel: channel, minLen: minChars,
		sem: make(chan struct{}, parallel), queue: make(chan Message, queueSize),
		seen: map[string]*entry{}, ring: make([]string, seenSize), Now: time.Now}
}

// Run starts the queue workers and blocks until ctx ends.
func (p *Pipeline) Run(ctx context.Context) {
	p.ctx = ctx
	for range cap(p.sem) {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case m := <-p.queue:
					if _, err := p.Handle(ctx, m); errors.Is(err, ErrInFlight) {
						slog.Debug("same message still processing, dropped", "source", m.Source, "id", m.ID)
					} else if err != nil {
						slog.Warn("message failed", "source", m.Source, "id", m.ID, "err", err)
					}
				}
			}
		}()
	}
	<-ctx.Done()
}

func (p *Pipeline) Enqueue(m Message) {
	select {
	case p.queue <- m:
	default:
		slog.Error("queue full, dropping message", "source", m.Source, "id", m.ID)
	}
}

// Handle classifies m and posts it when it is a task. A message is remembered
// once classified, so redelivery cannot post it twice. A failure forgets it
// again, so a retry is possible.
// The seen set lives in memory and is refilled on start from the tasks already in
// the target channel (Seed), so the channel stays the only store. It covers
// tasks, not messages judged not to be one: nothing replays those.
func (p *Pipeline) Handle(ctx context.Context, m Message) (Decision, error) {
	if utf8.RuneCountInString(strings.TrimSpace(m.Text)) < p.minLen {
		return Decision{Skipped: true}, nil
	}
	k := key(m)
	switch p.claim(k, true) {
	case claimDone:
		return Decision{Duplicate: true}, nil
	case claimBusy:
		return Decision{}, ErrInFlight
	}
	d, err := p.process(ctx, m)
	if err != nil {
		p.forget(k)
	} else {
		p.finish(k)
	}
	return d, err
}

func (p *Pipeline) process(ctx context.Context, m Message) (Decision, error) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return Decision{}, ctx.Err()
	}
	v, err := p.cl.Classify(ctx, m.Text, m.Author, m.Origin)
	if err != nil {
		return Decision{}, fmt.Errorf("classify: %w", err)
	}
	remind := v.Reminder && p.Scheduler != nil
	d := Decision{Task: v.Task, Title: v.Title, Reason: v.Reason}
	slog.Info("classified", "source", m.Source, "id", m.ID, "task", v.Task, "reminder", remind, "reason", v.Reason)
	if v.Task {
		if _, err = p.poster.Post(ctx, p.channel, Format(v, m), key(m)); err != nil {
			return d, fmt.Errorf("post task: %w", err)
		}
		d.Posted = true
	}
	if remind {
		err := p.remind(ctx, m, v)
		d.Reminder = err == nil
		if d.Reminder {
			d.RemindAt = v.Due.Format(time.RFC3339)
		}
		switch {
		case err == nil:
		case errors.Is(err, errNoThread), errors.Is(err, errTooLate):
			slog.Info("reminder dropped", "source", m.Source, "id", m.ID, "why", err)
		case d.Posted:
			// The task is out and its key is stored: failing here would repost it on retry.
			slog.Warn("reminder not scheduled", "source", m.Source, "id", m.ID, "err", err)
		default:
			return d, fmt.Errorf("schedule reminder: %w", err)
		}
	}
	return d, nil
}

// remind schedules the reply under the source Slack message. Other sources have
// none: a reply under the task posted for them would come from the same bot user
// that itakeit records as the task's reporter, and a reporter's reply on a
// needs-info task clears that status and pings the owners.
// A reminder-only message is not recorded in the channel, so the seen set does not
// survive a restart for it. The Slack scheduler skips a reminder already scheduled
// for the same channel, time and text, which covers a repeat that the model words
// the same. HACK: a repeat after a restart that the model words differently still
// schedules twice. Nothing replays handled messages today, which keeps this rare.
func (p *Pipeline) remind(ctx context.Context, m Message, v classify.Verdict) error {
	if m.Thread == nil || m.Thread.TS == "" {
		return errNoThread
	}
	// The classifier checked the time before a model call that can take minutes.
	if v.Due.Sub(p.Now()) < 10*time.Second {
		return errTooLate
	}
	text := "Reminder"
	if m.User != "" {
		text += " for <@" + m.User + ">"
	}
	return p.Scheduler.Schedule(ctx, *m.Thread, v.Due, text+": "+Escape(v.RemindText))
}

// key is what the channel remembers about a posted task: a hash, so it is
// fixed length and printable whatever the source put in the ID.
func key(m Message) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(m.Source+"\x00"+m.ID)))
}

// Seed marks keys as handled, oldest last as Slack lists them, so the newest
// stay longest in the ring.
func (p *Pipeline) Seed(keys []string) {
	for _, k := range slices.Backward(keys) {
		p.claim(k, false)
	}
}

type claimed int

const (
	claimNew claimed = iota
	claimBusy
	claimDone
)

func (p *Pipeline) claim(key string, inflight bool) claimed {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.seen[key]; ok {
		if e.inflight {
			return claimBusy
		}
		return claimDone
	}
	if old := p.ring[p.next]; old != "" {
		delete(p.seen, old)
	}
	p.ring[p.next] = key
	p.seen[key] = &entry{slot: p.next, inflight: inflight}
	p.next = (p.next + 1) % len(p.ring)
	return claimNew
}

func (p *Pipeline) finish(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.seen[key]; ok {
		e.inflight = false
	}
}

// forget frees the key's ring slot too, or the slot would later evict the key
// when it is claimed again.
func (p *Pipeline) forget(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.seen[key]; ok {
		p.ring[e.slot] = ""
		delete(p.seen, key)
	}
}

// Format is the task message. Model text is escaped so it cannot ping @channel
// or user groups, and the author is not mentioned, so posting never notifies them.
func Format(v classify.Verdict, m Message) string {
	title := strings.NewReplacer("*", "", "\n", " ").Replace(Escape(v.Title))
	var b strings.Builder
	fmt.Fprintf(&b, "*%s*\n%s\n\n", title, Escape(v.Summary))
	from := m.Origin
	if from == "" {
		from = m.Source
	}
	if m.Link != nil {
		if l := m.Link(); safeURL(l) {
			fmt.Fprintf(&b, "From <%s|a message> in %s", l, from)
			return b.String()
		}
	}
	fmt.Fprintf(&b, "From %s", from)
	return b.String()
}

// Escape makes text render as written in Slack.
func Escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func safeURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && !strings.ContainsAny(s, " <>|")
}
