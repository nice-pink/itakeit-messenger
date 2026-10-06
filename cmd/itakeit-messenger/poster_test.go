package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
	"github.com/slack-go/slack"
)

type fakeSlack struct {
	listed    []slack.ScheduledMessage
	listErr   error
	scheduled int
	posted    []slack.MsgOption
	deleted   []string
	deleteErr error
	updated   int
	ephemeral int
}

func (f *fakeSlack) PostMessageContext(_ context.Context, _ string, o ...slack.MsgOption) (string, string, error) {
	f.posted = o
	return "", "", nil
}
func (f *fakeSlack) ScheduleMessageContext(context.Context, string, string, ...slack.MsgOption) (string, string, error) {
	f.scheduled++
	return "", "Q1", nil
}
func (f *fakeSlack) DeleteScheduledMessageContext(_ context.Context, p *slack.DeleteScheduledMessageParameters) (bool, error) {
	f.deleted = append(f.deleted, p.Channel+"/"+p.ScheduledMessageID)
	return f.deleteErr == nil, f.deleteErr
}
func (f *fakeSlack) UpdateMessageContext(context.Context, string, string, ...slack.MsgOption) (string, string, string, error) {
	f.updated++
	return "", "", "", nil
}
func (f *fakeSlack) PostEphemeralContext(context.Context, string, string, ...slack.MsgOption) (string, error) {
	f.ephemeral++
	return "", nil
}
func (f *fakeSlack) GetScheduledMessagesContext(context.Context, *slack.GetScheduledMessagesParameters) ([]slack.ScheduledMessage, string, error) {
	return f.listed, "", f.listErr
}
func (f *fakeSlack) GetConversationHistoryContext(context.Context, *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	return nil, nil
}

func TestScheduleSkipsExistingReminder(t *testing.T) {
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	text := "Reminder for <@U1>: send the invoice"
	th := messenger.Thread{Channel: "C1", TS: "1.1"}
	for name, tc := range map[string]struct {
		listed  []slack.ScheduledMessage
		listErr error
		want    int
	}{
		"same":          {[]slack.ScheduledMessage{{Channel: "C1", PostAt: int(at.Unix()), Text: text}}, nil, 0},
		"other text":    {[]slack.ScheduledMessage{{Channel: "C1", PostAt: int(at.Unix()), Text: "Reminder for <@U2>: send the invoice"}}, nil, 1},
		"other minute":  {[]slack.ScheduledMessage{{Channel: "C1", PostAt: int(at.Unix()) + 30, Text: text}}, nil, 1},
		"other channel": {[]slack.ScheduledMessage{{Channel: "C2", PostAt: int(at.Unix()), Text: text}}, nil, 1},
		"none":          {nil, nil, 1},
		"lookup failed": {nil, errors.New("ratelimited"), 1},
	} {
		f := &fakeSlack{listed: tc.listed, listErr: tc.listErr}
		if _, err := (slackPoster{api: f}).Schedule(context.Background(), th, at, text); err != nil || f.scheduled != tc.want {
			t.Errorf("%s: scheduled %d times (want %d), err %v", name, f.scheduled, tc.want, err)
		}
	}
}

func press(user string) slack.InteractionCallback {
	return slack.InteractionCallback{
		Type:           slack.InteractionTypeBlockActions,
		User:           slack.User{ID: user},
		ActionCallback: slack.ActionCallbacks{BlockActions: []*slack.BlockAction{{ActionID: deleteAction, Value: "C1 Q1 U1 U2"}}},
	}
}

func TestDeleteButton(t *testing.T) {
	for name, tc := range map[string]struct {
		user      string
		deleteErr error
		deleted   int
		updated   int
		ephemeral int
	}{
		"target":      {"U1", nil, 1, 1, 0},
		"author":      {"U2", nil, 1, 1, 0},
		"bystander":   {"U3", nil, 0, 0, 1},
		"already due": {"U1", errors.New("invalid_scheduled_message_id"), 1, 1, 0},
		"slack down":  {"U1", errors.New("ratelimited"), 1, 0, 1},
	} {
		f := &fakeSlack{deleteErr: tc.deleteErr}
		(slackPoster{api: f}).HandleAction(context.Background(), press(tc.user))
		if len(f.deleted) != tc.deleted || f.updated != tc.updated || f.ephemeral != tc.ephemeral {
			t.Errorf("%s: deleted %v updated %d ephemeral %d", name, f.deleted, f.updated, f.ephemeral)
		}
	}
}

func TestHandleActionIgnoresOtherCallbacks(t *testing.T) {
	f := &fakeSlack{}
	h := slackPoster{api: f}
	other := press("U1")
	other.ActionCallback.BlockActions[0].ActionID = "something_else"
	short := press("U1")
	short.ActionCallback.BlockActions[0].Value = "C1"
	notAction := press("U1")
	notAction.Type = slack.InteractionTypeViewSubmission
	for _, cb := range []slack.InteractionCallback{other, short, notAction} {
		h.HandleAction(context.Background(), cb)
	}
	if len(f.deleted) != 0 || f.updated != 0 || f.ephemeral != 0 {
		t.Fatalf("acted on a callback that is not ours: %+v", f)
	}
}

func TestConfirmButtonRoundTrip(t *testing.T) {
	f := &fakeSlack{}
	th := messenger.Thread{Channel: "C1", TS: "1.1"}
	if err := (slackPoster{api: f}).Confirm(context.Background(), th, "Q9", "Reminder scheduled", []string{"U1", "U2"}); err != nil || len(f.posted) == 0 {
		t.Fatalf("err %v posted %d", err, len(f.posted))
	}
	cb := press("U2")
	cb.ActionCallback.BlockActions[0].Value = strings.Join([]string{"C1", "Q9", "U1", "U2"}, " ")
	(slackPoster{api: f}).HandleAction(context.Background(), cb)
	if len(f.deleted) != 1 || f.deleted[0] != "C1/Q9" {
		t.Fatalf("deleted %v", f.deleted)
	}
}
