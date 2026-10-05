package source

import (
	"context"
	"log/slog"

	"github.com/nice-pink/itakeit-messenger/pkg/config"
	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Slack reads top-level messages of the channels the app is in over Socket Mode.
// Replies, edits and deletions are ignored.
type Slack struct {
	API    *slack.Client
	SM     *socketmode.Client
	Cfg    *config.Config
	UserID string
	BotID  string
}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) Run(ctx context.Context, sink messenger.Sink) error {
	errc := make(chan error, 1)
	go func() { errc <- s.SM.RunContext(ctx) }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errc:
			return err
		case evt := <-s.SM.Events:
			switch evt.Type {
			case socketmode.EventTypeConnected:
				slog.Info("connected to slack")
			case socketmode.EventTypeConnectionError:
				slog.Warn("slack connection error, retrying", "data", evt.Data)
			case socketmode.EventTypeEventsAPI:
				if evt.Request != nil {
					s.SM.Ack(*evt.Request)
				}
				e, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				if ev, ok := e.InnerEvent.Data.(*slackevents.MessageEvent); ok {
					if m, ok := s.Accept(ev); ok {
						sink.Enqueue(m)
					}
				}
			}
		}
	}
}

// Accept maps an event to a message, or reports that it is not read.
func (s *Slack) Accept(ev *slackevents.MessageEvent) (messenger.Message, bool) {
	sl := s.Cfg.Sources.Slack
	switch {
	case ev.SubType != "" && ev.SubType != "file_share" && !(ev.SubType == "bot_message" && sl.IncludeBots),
		ev.BotID != "" && !sl.IncludeBots,
		ev.BotID == s.BotID && s.BotID != "", ev.User == s.UserID && s.UserID != "",
		ev.ThreadTimeStamp != "" && ev.ThreadTimeStamp != ev.TimeStamp,
		ev.ChannelType == "group" && !sl.IncludePrivate,
		ev.ChannelType != "channel" && ev.ChannelType != "group",
		!s.Cfg.SlackReads(ev.Channel):
		return messenger.Message{}, false
	}
	author := ev.User
	if author == "" {
		author = ev.BotID
	}
	thread := &messenger.Thread{Channel: ev.Channel, TS: ev.TimeStamp}
	ch, ts := ev.Channel, ev.TimeStamp
	return messenger.Message{
		Source: "slack",
		ID:     ch + ":" + ts,
		Text:   ev.Text,
		Author: author,
		User:   ev.User,
		Thread: thread,
		Origin: "<#" + ch + ">",
		Link: func() string {
			l, err := s.API.GetPermalink(&slack.PermalinkParameters{Channel: ch, Ts: ts})
			if err != nil {
				slog.Warn("permalink", "channel", ch, "ts", ts, "err", err)
			}
			return l
		},
	}, true
}
