package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nice-pink/itakeit-messager/pkg/messager"
	"github.com/slack-go/slack"
)

type fakeSlack struct {
	listed    []slack.ScheduledMessage
	listErr   error
	scheduled int
}

func (f *fakeSlack) PostMessageContext(context.Context, string, ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}
func (f *fakeSlack) ScheduleMessageContext(context.Context, string, string, ...slack.MsgOption) (string, string, error) {
	f.scheduled++
	return "", "Q1", nil
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
	th := messager.Thread{Channel: "C1", TS: "1.1"}
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
		if err := (slackPoster{api: f}).Schedule(context.Background(), th, at, text); err != nil || f.scheduled != tc.want {
			t.Errorf("%s: scheduled %d times (want %d), err %v", name, f.scheduled, tc.want, err)
		}
	}
}
