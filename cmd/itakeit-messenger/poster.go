package main

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
	"github.com/slack-go/slack"
)

// slackAPI is the part of *slack.Client the poster uses.
type slackAPI interface {
	PostMessageContext(ctx context.Context, channel string, opts ...slack.MsgOption) (string, string, error)
	ScheduleMessageContext(ctx context.Context, channel, postAt string, opts ...slack.MsgOption) (string, string, error)
	DeleteScheduledMessageContext(ctx context.Context, p *slack.DeleteScheduledMessageParameters) (bool, error)
	UpdateMessageContext(ctx context.Context, channel, ts string, opts ...slack.MsgOption) (string, string, string, error)
	PostEphemeralContext(ctx context.Context, channel, user string, opts ...slack.MsgOption) (string, error)
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
// which is what a repeat of the same message produces, and returns "" then.
// Slack's list carries no thread, so the same user's identical reminder in two
// threads of one channel at the same minute counts as one. A failed lookup
// schedules anyway: a duplicate beats a lost reminder.
func (s slackPoster) Schedule(ctx context.Context, t messenger.Thread, at time.Time, text string) (string, error) {
	if s.scheduled(ctx, t.Channel, at, text) {
		slog.Info("reminder already scheduled", "channel", t.Channel, "at", at)
		return "", nil
	}
	_, id, err := s.api.ScheduleMessageContext(ctx, t.Channel, strconv.FormatInt(at.Unix(), 10),
		slack.MsgOptionText(text, false), slack.MsgOptionTS(t.TS), slack.MsgOptionDisableLinkUnfurl())
	return id, err
}

const deleteAction = "delete_reminder"

// Confirm posts the confirmation with a Delete button. The button carries what
// HandleAction needs, so nothing is kept in memory and it works after a restart.
func (s slackPoster) Confirm(ctx context.Context, t messenger.Thread, id, text string, users []string) error {
	value := strings.Join(append([]string{t.Channel, id}, users...), " ")
	btn := slack.NewButtonBlockElement(deleteAction, value, slack.NewTextBlockObject(slack.PlainTextType, "Delete", false, false))
	blocks := []slack.Block{section(text)}
	if len(users) > 0 {
		blocks = append(blocks, slack.NewActionBlock("", btn))
	}
	_, _, err := s.api.PostMessageContext(ctx, t.Channel, slack.MsgOptionText(text, false), slack.MsgOptionTS(t.TS), slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionBlocks(blocks...))
	return err
}

func section(text string) slack.Block {
	return slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)
}

// HandleAction deletes the scheduled reminder when a user it belongs to presses
// Delete, and rewrites the confirmation. Needs interactivity enabled in the app.
func (s slackPoster) HandleAction(ctx context.Context, cb slack.InteractionCallback) {
	if cb.Type != slack.InteractionTypeBlockActions {
		return
	}
	for _, a := range cb.ActionCallback.BlockActions {
		if a.ActionID != deleteAction {
			continue
		}
		f := strings.Fields(a.Value)
		if len(f) < 2 {
			continue
		}
		channel, id, users := f[0], f[1], f[2:]
		if !slices.Contains(users, cb.User.ID) {
			if _, err := s.api.PostEphemeralContext(ctx, channel, cb.User.ID, slack.MsgOptionText("Only the person this reminder is for, or who asked for it, can delete it.", false), slack.MsgOptionTS(cb.Message.ThreadTimestamp)); err != nil {
				slog.Warn("reminder delete: refusal not posted", "err", err)
			}
			continue
		}
		text := "Reminder deleted."
		if _, err := s.api.DeleteScheduledMessageContext(ctx, &slack.DeleteScheduledMessageParameters{Channel: channel, ScheduledMessageID: id}); err != nil {
			if !strings.Contains(err.Error(), "invalid_scheduled_message_id") {
				// Rate limit, network or scope: the reminder is still scheduled, so keep the button for a retry.
				slog.Warn("reminder delete failed", "channel", channel, "id", id, "err", err)
				if _, err := s.api.PostEphemeralContext(ctx, channel, cb.User.ID, slack.MsgOptionText("Could not delete the reminder, try again.", false), slack.MsgOptionTS(cb.Message.ThreadTimestamp)); err != nil {
					slog.Warn("reminder delete: failure not posted", "err", err)
				}
				continue
			}
			// Slack no longer knows the ID: the reminder was sent, or is due within a minute.
			slog.Info("reminder delete refused", "channel", channel, "id", id, "err", err)
			text = "Too late to delete: the reminder is due or already sent."
		}
		if _, _, _, err := s.api.UpdateMessageContext(ctx, channel, cb.Message.Timestamp, slack.MsgOptionText(text, false), slack.MsgOptionBlocks(section(text))); err != nil {
			slog.Warn("reminder delete: confirmation not updated", "err", err)
		}
	}
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
