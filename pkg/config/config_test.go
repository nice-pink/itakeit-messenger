package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	c, err := Parse([]byte("target_channel: C0123456789\nsources:\n  stdin: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend != BackendClaudeCode || c.Model != "sonnet" || c.MaxParallel != 2 || !c.Reminders || c.Timezone != "UTC" || c.Sources.HTTP.Listen != "127.0.0.1:8080" {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"name not id":       {"target_channel: general\nsources: {stdin: true}", "channel ID"},
		"no source":         {"target_channel: C0123456789", "at least one source"},
		"target listed":     {"target_channel: C0123456789\nsources: {slack: {enabled: true, channels: [C0123456789]}}", "read back"},
		"unknown key":       {"target_channel: C0123456789\nsources: {stdin: true}\nchanel: x", "chanel"},
		"nothing on":        {"target_channel: C0123456789\ntasks: false\nreminders: false\nsources: {stdin: true}", "tasks, reminders or both"},
		"http no tasks":     {"tasks: false\nsources: {http: {enabled: true}}", "only the slack source"},
		"negative min":      {"target_channel: C0123456789\nmin_chars: -5\nsources: {stdin: true}", "min_chars"},
		"recover cap":       {"target_channel: C0123456789\nrecover_messages: 5000\nsources: {stdin: true}", "999"},
		"token in env":      {"target_channel: C0123456789\nenv: [MESSENGER_SLACK_BOT_TOKEN]\nsources: {stdin: true}", "MESSENGER_SLACK_BOT_TOKEN"},
		"langdock no model": {"target_channel: C0123456789\nbackend: langdock\nsources: {stdin: true}", "model is required"},
		"bad region":        {"target_channel: C0123456789\nbackend: langdock\nmodel: m\nlangdock_region: asia\nsources: {stdin: true}", "langdock_region"},
		"bad timezone":      {"target_channel: C0123456789\ntimezone: Mars/Base\nsources: {stdin: true}", "timezone"},
		"long knowledge":    {"target_channel: C0123456789\nknowledge: " + strings.Repeat("x", MaxKnowledge+1) + "\nsources: {stdin: true}", "knowledge"},
		"bad backend":       {"target_channel: C0123456789\nbackend: gpt\nsources: {stdin: true}", "backend"},
	} {
		if _, err := Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestSlackReads(t *testing.T) {
	c := &Config{TargetChannel: "CT", Sources: Sources{Slack: Slack{Exclude: []string{"CX"}}}}
	if !c.SlackReads("CA") || c.SlackReads("CT") || c.SlackReads("CX") {
		t.Fatal("all-channels mode must skip target and excluded")
	}
	c.Sources.Slack.Channels = []string{"CA"}
	if !c.SlackReads("CA") || c.SlackReads("CB") {
		t.Fatal("list mode reads only listed channels")
	}
}

func TestRemindersOnly(t *testing.T) {
	c, err := Parse([]byte("tasks: false\nsources: {slack: {enabled: true}}"))
	if err != nil || c.Tasks || !c.Reminders {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	if _, err := Parse([]byte("tasks: false\ntarget_channel: general\nsources: {slack: {enabled: true}}")); err == nil {
		t.Fatal("a given target_channel must still be an ID")
	}
}

func TestZeroIsExplicit(t *testing.T) {
	c, err := Parse([]byte("target_channel: C0123456789\nmin_chars: 0\nrecover_messages: 0\nsources: {stdin: true}"))
	if err != nil || c.MinChars != 0 || c.RecoverMessages != 0 {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}
