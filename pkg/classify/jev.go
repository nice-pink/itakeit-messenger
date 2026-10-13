package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Odds are the probabilities that a message holds a task and a reminder. A question
// that was not asked is 0.
type Odds struct{ Task, Reminder float64 }

// Gate scores a message cheaply before the model is asked to write anything. task
// and reminder say which questions to ask, and taskDef is what counts as a task here.
type Gate func(ctx context.Context, text, taskDef string, task, reminder bool) (Odds, error)

const (
	jevTaskDef     = `The message asks for, or clearly needs, a concrete piece of work from someone: a request, a bug to fix, a question that needs a person to investigate or answer, a to-do. Chit-chat, thanks, announcements, status updates, answered questions and automated notices that need no action are not. A request only to be reminded of something, or to follow up later, is a reminder, not a task. A general-knowledge or social question nobody on the team needs to act on is not a task.`
	jevReminderDef = `The message asks to be reminded of something, sets a follow-up for someone, or asks to remind a person, at a specific day or time ("remind me on Friday", "ping me tomorrow at 9", "check again in two days"). A vague time such as "sometime" or "later" does not count.`
)

const jevTimeout = 15 * time.Second

// NewJev asks TypeSafe's Jev model one Noul question per output. A Noul answer is the
// probability, 0 to 1, that the statement about the message is true. The message
// goes in as state, which Jev only reads: it has no tools and returns numbers.
func NewJev(url, model, apiKey string) Gate {
	client := &http.Client{Timeout: jevTimeout}
	return func(ctx context.Context, text, taskDef string, task, reminder bool) (Odds, error) {
		qs := map[string]any{}
		if task {
			qs["task"] = map[string]any{"type": "noul", "instructions": taskDef}
		}
		if reminder {
			qs["reminder"] = map[string]any{"type": "noul", "instructions": jevReminderDef}
		}
		body, err := json.Marshal(map[string]any{"state": text, "model": model, "questions": qs})
		if err != nil {
			return Odds{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return Odds{}, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return Odds{}, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return Odds{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return Odds{}, fmt.Errorf("jev: %s: %s", resp.Status, bytes.TrimSpace(raw[:min(len(raw), 300)]))
		}
		var out struct {
			Answers map[string]struct {
				Noul *float64 `json:"noul"`
			} `json:"answers"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return Odds{}, fmt.Errorf("jev: unexpected answer: %w", err)
		}
		var o Odds
		for id, dst := range map[string]*float64{"task": &o.Task, "reminder": &o.Reminder} {
			if _, asked := qs[id]; !asked {
				continue
			}
			a := out.Answers[id]
			if a.Noul == nil || *a.Noul < 0 || *a.Noul > 1 {
				return Odds{}, fmt.Errorf("jev: no usable answer for %q", id)
			}
			*dst = *a.Noul
		}
		return o, nil
	}
}

// WithGate puts a cheap pre-filter in front of the model. A message goes to the model
// only for an output whose probability is at least its threshold: with neither
// reached the model is not called, and with one reached it is not asked for the other.
// With trust the gate's yes is final: the model is told so, only writes the task or
// reminder up, and the output stays on even if the model says otherwise (a task
// without title and summary is then an error, a reminder without a usable time is dropped).
// A failing gate lets the message through to the model for both.
func (c *Classifier) WithGate(g Gate, taskMin, reminderMin float64, trust bool) *Classifier {
	c.gate, c.taskMin, c.reminderMin, c.trust = g, taskMin, reminderMin, trust
	return c
}

// taskDef is what the gate is told a task is: the built-in definition, then the
// operator's criteria and knowledge, so it recognises the team's own kinds of task.
func (c *Classifier) taskDef() string {
	d := jevTaskDef
	if c.criteria != "" {
		d += "\n\nHow this team defines a task:\n" + strings.TrimSpace(c.criteria)
	}
	if c.knowledge != "" {
		d += "\n\nBackground about this team:\n" + c.knowledge
	}
	return d
}

// WithoutRephrase makes a gated task or reminder use the message as written instead
// of the model's wording. A message whose gate says task only is then never sent to
// the model; one that says reminder still is, because the model reads its time (Jev
// gives a probability, not a date). Without a gate this does nothing, and while the
// gate fails the model judges and words the message as usual.
func (c *Classifier) WithoutRephrase() *Classifier {
	c.raw = true
	return c
}
