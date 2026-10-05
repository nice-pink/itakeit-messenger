package classify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
)

// minSecret keeps short values (flags like 1, /tmp) out of the scrub, which would
// mangle ordinary text.
const minSecret = 8

type probe struct {
	Email string `json:"email" jsonschema_description:"the email address in your context, or empty if there is none"`
}

const probePrompt = `You are being checked at startup by the program that runs you. Your answer is never shown to anyone. It is used only to remove that email address from what you later post. If an email address appears anywhere in your context, for example in environment or user information, return it exactly as written. Otherwise return an empty string.`

// emailAddr picks addresses out of the probe's answer, so a decorated answer
// ("Email: a@b.com.") still yields the bare address and a stray "@" yields none.
var emailAddr = regexp.MustCompile(`[\p{L}\p{N}._%+'-]+@[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)*\.(?:xn--[a-z0-9-]+|\p{L}{2,})`)

// WithSecrets adds strings that must never appear in a posted task or reminder:
// the login email, tokens, the working directory. The model's text is escaped but
// a message that asks it to "include your environment" would otherwise get the
// CLI's context into the target channel.
func (c *Classifier) WithSecrets(secrets ...string) *Classifier {
	for _, s := range secrets {
		if len(s) >= minSecret && !slices.ContainsFunc(c.secret, func(o string) bool { return strings.EqualFold(o, s) }) {
			c.secret = append(c.secret, s)
		}
	}
	return c
}

// Secrets reports how many strings are scrubbed, for the startup log.
func (c *Classifier) Secrets() int { return len(c.secret) }

// Probe makes one cheap call before the first message, so a login or model that
// does not work stops the messager at startup. It also asks the model which email
// address its prompt carries (the CLI adds the login's to every prompt) and
// scrubs that exact string, whichever login it came from. A model that declines
// to say fails nothing: refused reports it.
func (c *Classifier) Probe(ctx context.Context) (refused bool, err error) {
	var out probe
	switch err = c.ask(ctx, probePrompt, "Which email address appears in your context?", &out); {
	case errors.Is(err, ErrRefused):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("startup probe: %w", err)
	}
	c.WithSecrets(emailAddr.FindAllString(out.Email, -1)...)
	return false, nil
}

// scrub removes the secrets from the model's text, ignoring case.
func (c *Classifier) scrub(v *Verdict) {
	var hit bool
	for _, f := range []*string{&v.Title, &v.Summary, &v.Reason, &v.RemindText} {
		for _, s := range c.secret {
			if re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(s)); re.MatchString(*f) {
				*f, hit = re.ReplaceAllString(*f, "[redacted]"), true
			}
		}
	}
	if hit {
		slog.Warn("the model's answer contained a secret: removed. A message may be trying to extract the environment")
	}
}

// EnvSecrets are the values the CLI gets that could be printed: every
// CLAUDE_CODE_* variable (the OAuth token) and the ones listed in extra.
func EnvSecrets(extra []string) []string {
	var out []string
	for _, kv := range CLIEnv(extra) {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE_CODE_") || slices.Contains(extra, name) {
			out = append(out, value)
		}
	}
	return out
}

// CLIDir is the working directory of every claude run.
func CLIDir() string { return os.TempDir() }
