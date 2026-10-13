// Package config loads the messenger's YAML file.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	BackendClaudeCode = "claude-code"
	BackendAPI        = "api"
	BackendLangdock   = "langdock"
	BackendOpenAI     = "openai"
)

// DefaultOpenAIBaseURL is where backend openai calls unless openai_base_url says otherwise.
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// DefaultJevURL is TypeSafe's evaluation endpoint.
const DefaultJevURL = "https://api.typesafe.ai/v1/systemone"

// MaxKnowledge bounds knowledge, which is part of the system prompt of every call.
const MaxKnowledge = 8000

type Config struct {
	// Tasks makes the messenger post tasks to TargetChannel. Without it the messenger only
	// sets reminders and TargetChannel is optional.
	Tasks bool `yaml:"tasks"`
	// TargetChannel is the ID of the channel itakeit serves, where tasks are posted.
	TargetChannel string `yaml:"target_channel"`
	Backend       string `yaml:"backend"`
	ClaudeBin     string `yaml:"claude_bin"`
	// LangdockRegion is eu or us, the region of the Langdock workspace (backend langdock).
	LangdockRegion string `yaml:"langdock_region"`
	Model          string `yaml:"model"`
	// OpenAIBaseURL is the API root for backend openai: OpenAI by default, or a compatible
	// server such as vLLM, SGLang or Ollama.
	OpenAIBaseURL string `yaml:"openai_base_url"`
	// Criteria extends the built-in definition of a task with what counts in this team.
	Criteria string `yaml:"criteria"`
	// Knowledge is background for the model: how to treat kinds of messages (a Grafana
	// alert, a form) and how to word the task. Unlike Criteria it also applies when
	// Tasks is off.
	Knowledge string `yaml:"knowledge"`
	// ReasonFirst makes the model write its reason before the verdict. It is meant
	// for small models, costs a sentence of output per message, and a large model does
	// not need it.
	ReasonFirst bool `yaml:"reason_first"`
	// Jev puts TypeSafe's Jev model in front of the model as a cheap filter.
	Jev Jev `yaml:"jev"`
	// Env lists extra environment variables the claude CLI may see.
	Env         []string `yaml:"env"`
	MaxParallel int      `yaml:"max_parallel"`
	// MinChars skips shorter messages without asking the model.
	MinChars int `yaml:"min_chars"`
	// Reminders makes the messenger schedule a thread reply when a message asks to be
	// reminded at a time. Timezone (IANA name) is how the model reads that time.
	Reminders bool   `yaml:"reminders"`
	Timezone  string `yaml:"timezone"`
	// MentionAuthor ends a posted task with a mention of the message's author.
	MentionAuthor bool `yaml:"mention_author"`
	// BotContact is a Slack user ID that reminders, and with MentionAuthor tasks, from
	// bot messages are addressed to, as in itakeit. BotContacts overrides it per channel ID.
	BotContact  string            `yaml:"bot_contact"`
	BotContacts map[string]string `yaml:"bot_contacts"`
	// RecoverMessages is how many of the target channel's latest messages are read
	// on start to find tasks already posted, at most 999. 0 or negative disables it.
	RecoverMessages int     `yaml:"recover_messages"`
	Sources         Sources `yaml:"sources"`
}

// Jev scores each message with TypeSafe's Jev (TYPESAFE_API_KEY) and calls the model,
// to word the task or reminder, only for an output whose probability reaches its threshold.
type Jev struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
	URL     string `yaml:"url"`
	// TaskThreshold and ReminderThreshold are probabilities from 0 to 1.
	// Rephrase false posts a gated task, and a reminder's text, as the message was
	// written instead of the model's wording. A reminder still calls the model for its time.
	Rephrase bool `yaml:"rephrase"`
	// Trust makes Jev's yes final: the model only writes the task or reminder up.
	Trust             bool    `yaml:"trust"`
	TaskThreshold     float64 `yaml:"task_threshold"`
	ReminderThreshold float64 `yaml:"reminder_threshold"`
}

type Sources struct {
	Slack Slack `yaml:"slack"`
	HTTP  HTTP  `yaml:"http"`
	Stdin bool  `yaml:"stdin"`
}

type Slack struct {
	Enabled bool `yaml:"enabled"`
	// Channels limits the source to these channel IDs. Empty reads every channel the app is in.
	Channels       []string `yaml:"channels"`
	Exclude        []string `yaml:"exclude"`
	IncludePrivate bool     `yaml:"include_private"`
	IncludeBots    bool     `yaml:"include_bots"`
}

type HTTP struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

var channelID = regexp.MustCompile(`^[CG][A-Z0-9]{8,}$`)
var userID = regexp.MustCompile(`^[UW][A-Z0-9]{6,}$`)

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Config, error) {
	c := Config{Tasks: true, Reminders: true, MinChars: 12, RecoverMessages: 200, MaxParallel: 2,
		Jev: Jev{Rephrase: true, Model: "jev-latest", URL: DefaultJevURL, TaskThreshold: 0.3, ReminderThreshold: 0.3}}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.Backend = cmp.Or(c.Backend, BackendClaudeCode)
	c.ClaudeBin = cmp.Or(c.ClaudeBin, "claude")
	c.LangdockRegion = cmp.Or(c.LangdockRegion, "eu")
	c.OpenAIBaseURL = cmp.Or(c.OpenAIBaseURL, DefaultOpenAIBaseURL)
	c.Timezone = cmp.Or(c.Timezone, "UTC")
	c.Sources.HTTP.Listen = cmp.Or(c.Sources.HTTP.Listen, "127.0.0.1:8080")
	if c.Model == "" {
		c.Model = map[string]string{BackendClaudeCode: "sonnet", BackendAPI: "claude-sonnet-5-5", BackendLangdock: "", BackendOpenAI: ""}[c.Backend]
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	var errs []error
	if (c.Tasks || c.TargetChannel != "") && !channelID.MatchString(c.TargetChannel) {
		errs = append(errs, errors.New("target_channel must be a channel ID such as C0123456789, not a name"))
	}
	if !c.Tasks && !c.Reminders {
		errs = append(errs, errors.New("enable tasks, reminders or both"))
	}
	if !c.Tasks && (c.Sources.HTTP.Enabled || c.Sources.Stdin) {
		errs = append(errs, errors.New("with tasks off only the slack source works: reminders are replies under a Slack message, and http and stdin messages have none"))
	}
	if !slices.Contains([]string{BackendClaudeCode, BackendAPI, BackendLangdock, BackendOpenAI}, c.Backend) {
		errs = append(errs, fmt.Errorf("backend %q: use %s, %s, %s or %s", c.Backend, BackendClaudeCode, BackendAPI, BackendLangdock, BackendOpenAI))
	}
	if c.Backend == BackendLangdock && c.Model == "" {
		errs = append(errs, errors.New("model is required for backend langdock: use a model ID from GET /openai/{region}/v1/models"))
	}
	if c.Backend == BackendOpenAI && c.Model == "" {
		errs = append(errs, errors.New("model is required for backend openai: use a model ID from GET /v1/models"))
	}
	if u, err := url.Parse(c.OpenAIBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("openai_base_url %q: use an http(s) URL such as %s", c.OpenAIBaseURL, DefaultOpenAIBaseURL))
	}
	if c.LangdockRegion != "eu" && c.LangdockRegion != "us" {
		errs = append(errs, fmt.Errorf("langdock_region %q: use eu or us", c.LangdockRegion))
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("timezone %q: use an IANA name such as Europe/Berlin", c.Timezone))
	}
	if n := utf8.RuneCountInString(c.Knowledge); n > MaxKnowledge {
		errs = append(errs, fmt.Errorf("knowledge is %d characters, at most %d: it is sent with every message", n, MaxKnowledge))
	}
	for name, v := range map[string]float64{"task_threshold": c.Jev.TaskThreshold, "reminder_threshold": c.Jev.ReminderThreshold} {
		if v < 0 || v > 1 {
			errs = append(errs, fmt.Errorf("jev.%s %v: use a probability from 0 to 1", name, v))
		}
	}
	if !c.Jev.Rephrase && !c.Jev.Enabled {
		errs = append(errs, errors.New("jev.rephrase: false needs jev.enabled: without the gate nothing decides what is a task"))
	}
	if u, err := url.Parse(c.Jev.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("jev.url %q: use an http(s) URL such as %s", c.Jev.URL, DefaultJevURL))
	}
	if c.MinChars < 0 {
		errs = append(errs, errors.New("min_chars must not be negative"))
	}
	if c.RecoverMessages > 999 {
		errs = append(errs, errors.New("recover_messages is at most 999: Slack returns one page"))
	}
	for _, name := range c.Env {
		if strings.HasPrefix(name, "MESSENGER_") || name == "ANTHROPIC_API_KEY" || name == "ANTHROPIC_AUTH_TOKEN" || name == "LANGDOCK_API_KEY" || name == "OPENAI_API_KEY" || name == "TYPESAFE_API_KEY" {
			errs = append(errs, fmt.Errorf("env: %s must not reach the claude CLI (it holds a token, or would make the CLI bill the API instead of using its login)", name))
		}
	}
	if c.MaxParallel < 1 {
		errs = append(errs, errors.New("max_parallel must be at least 1"))
	}
	s := c.Sources
	if !s.Slack.Enabled && !s.HTTP.Enabled && !s.Stdin {
		errs = append(errs, errors.New("enable at least one source under sources"))
	}
	for _, id := range slices.Concat(s.Slack.Channels, s.Slack.Exclude) {
		if !channelID.MatchString(id) {
			errs = append(errs, fmt.Errorf("sources.slack: %q is not a channel ID", id))
		}
	}
	if slices.Contains(s.Slack.Channels, c.TargetChannel) {
		errs = append(errs, errors.New("sources.slack.channels contains target_channel: tasks would be read back and posted again"))
	}
	c.BotContact = strings.TrimSpace(c.BotContact)
	if c.BotContact != "" && !userID.MatchString(c.BotContact) {
		errs = append(errs, fmt.Errorf("bot_contact %q is not a Slack user ID (like U0123456789, from the profile's more menu -> Copy member ID)", c.BotContact))
	}
	for ch, u := range c.BotContacts {
		if !channelID.MatchString(ch) {
			errs = append(errs, fmt.Errorf("bot_contacts key %q is not a channel ID (like C0123456789)", ch))
		}
		if u = strings.TrimSpace(u); !userID.MatchString(u) {
			errs = append(errs, fmt.Errorf("bot_contacts[%s] %q is not a Slack user ID (like U0123456789)", ch, u))
		}
		c.BotContacts[ch] = u
	}
	return errors.Join(errs...)
}

// BotContactFor is the user a reminder from a bot message in channel is addressed
// to: its bot_contacts entry, else bot_contact, else "" (nobody is mentioned).
func (c *Config) BotContactFor(channel string) string {
	if u := c.BotContacts[channel]; u != "" {
		return u
	}
	return c.BotContact
}

// SlackReads reports whether the Slack source reads channel. The target channel
// is never read, whatever the lists say.
func (c *Config) SlackReads(channel string) bool {
	s := c.Sources.Slack
	switch {
	case channel == c.TargetChannel, slices.Contains(s.Exclude, channel):
		return false
	case len(s.Channels) > 0:
		return slices.Contains(s.Channels, channel)
	}
	return true
}
