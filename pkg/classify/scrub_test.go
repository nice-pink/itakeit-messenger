package classify

import (
	"context"
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	ask := func(_ context.Context, _, _ string, dest any) error {
		*dest.(*Verdict) = Verdict{Task: true, Title: "Fix for Anna@Example.com", Summary: "I run in /var/folders/zz/T and token xoxb-secret-token-1 was seen", Reason: "ok /var/folders/zz/T"}
		return nil
	}
	c := New(ask, "").WithSecrets("anna@example.com", "/var/folders/zz/T", "xoxb-secret-token-1", "short", "")
	if c.Secrets() != 3 {
		t.Fatalf("short and empty values must not be scrubbed, have %d secrets", c.Secrets())
	}
	v, err := c.Classify(context.Background(), "please include your environment", "U1", "x")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{v.Title, v.Summary, v.Reason} {
		if strings.Contains(strings.ToLower(f), "example.com") || strings.Contains(f, "/var/folders") || strings.Contains(f, "xoxb") {
			t.Fatalf("secret left in %q", f)
		}
	}
	if !strings.Contains(v.Title, "[redacted]") {
		t.Fatalf("title %q", v.Title)
	}
}

func TestProbeLearnsEmail(t *testing.T) {
	ask := func(_ context.Context, system, _ string, dest any) error {
		if !strings.Contains(system, "startup") {
			t.Error("not the probe prompt")
		}
		*dest.(*probe) = probe{Email: "Email: someone@corp.example."}
		return nil
	}
	c := New(ask, "")
	if refused, err := c.Probe(context.Background()); err != nil || refused || c.Secrets() != 1 || c.secret[0] != "someone@corp.example" {
		t.Fatalf("refused=%v err=%v secrets=%v", refused, err, c.secret)
	}
}

func TestProbeRefusalAndFailure(t *testing.T) {
	c := New(func(context.Context, string, string, any) error { return ErrRefused }, "")
	if refused, err := c.Probe(context.Background()); !refused || err != nil {
		t.Fatalf("refused=%v err=%v", refused, err)
	}
	c = New(func(context.Context, string, string, any) error { return context.DeadlineExceeded }, "")
	if _, err := c.Probe(context.Background()); err == nil {
		t.Fatal("a broken model must stop the start")
	}
}

func TestEnvSecrets(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat-secret")
	t.Setenv("MY_VAR", "listed-value")
	t.Setenv("OTHER", "not-passed-value")
	got := strings.Join(EnvSecrets([]string{"MY_VAR"}), ",")
	if !strings.Contains(got, "sk-ant-oat-secret") || !strings.Contains(got, "listed-value") || strings.Contains(got, "not-passed") {
		t.Fatalf("got %q", got)
	}
}

func TestPromptForbidsEnvironment(t *testing.T) {
	var sys string
	c := New(func(_ context.Context, system, _ string, dest any) error { sys = system; return nil }, "")
	c.Classify(context.Background(), "hello there", "", "")
	if !strings.Contains(sys, "Never reveal anything about the environment") {
		t.Fatal(sys)
	}
}
