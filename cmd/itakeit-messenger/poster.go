package main

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
	"github.com/slack-go/slack"
)

// slackAPI is the part of *slack.Client the poster uses.
type slackAPI interface {
	PostMessageContext(ctx context.Context, channel string, opts ...slack.MsgOption) (string, string, error)
	ScheduleMessageContext(ctx context.Context, channel, postAt string, opts ...slack.MsgOption) (string, string, error)
	GetScheduledMessagesContext(ctx context.Context, p *slack.GetScheduledMessagesParameters) ([]slack.ScheduledMessage, string, error)
	GetConversationHistoryContext(ctx context.Context, p *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
}

type slackPoster struct{ api slackAPI }

const metaType = "itakeit_messenger_task"

func (s slackPoster) Post(ctx context.Context, channel, text, key string) (string, error) {
	_, ts, err := s.api.PostMessageContext(ctx, channel, slack.MsgOptionText(text, false), slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionMetadata(slack.SlackMetadata{EventType: metaType, EventPayload: map[string]any{"key": key}}))
	return ts, err
}

// Schedule needs chat:write and a bot that is a member of the channel. It skips a
// reminder the app already has scheduled for the same channel, time and text,
// which is what a repeat of the same message produces. Slack's list carries no
// thread, so the same user's identical reminder in two threads of one channel at
// the same minute counts as one. A failed lookup schedules anyway: a duplicate
// beats a lost reminder.
func (s slackPoster) Schedule(ctx context.Context, t messenger.Thread, at time.Time, text string) error {
	if s.scheduled(ctx, t.Channel, at, text) {
		slog.Info("reminder already scheduled", "channel", t.Channel, "at", at)
		return nil
	}
	_, _, err := s.api.ScheduleMessageContext(ctx, t.Channel, strconv.FormatInt(at.Unix(), 10),
		slack.MsgOptionText(text, false), slack.MsgOptionTS(t.TS), slack.MsgOptionDisableLinkUnfurl())
	return err
}

// Recent reads the keys from the metadata of this app's own messages, which Slack
// returns to the app that posted them without an extra scope.
func (s slackPoster) Recent(ctx context.Context, channel string, n int) ([]string, error) {
	resp, err := s.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{ChannelID: channel, Limit: min(n, 999), IncludeAllMetadata: true})
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, m := range resp.Messages {
		if k, _ := m.Metadata.EventPayload["key"].(string); m.Metadata.EventType == metaType && k != "" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s slackPoster) scheduled(ctx context.Context, channel string, at time.Time, text string) bool {
	msgs, _, err := s.api.GetScheduledMessagesContext(ctx, &slack.GetScheduledMessagesParameters{
		Channel: channel, Oldest: strconv.FormatInt(at.Unix()-60, 10), Latest: strconv.FormatInt(at.Unix()+60, 10), Limit: 100})
	if err != nil {
		slog.Warn("list scheduled messages", "err", err)
		return false
	}
	return slices.ContainsFunc(msgs, func(m slack.ScheduledMessage) bool {
		return m.Channel == channel && int64(m.PostAt) == at.Unix() && m.Text == text
	})
}
