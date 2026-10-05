package classify

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

const callTimeout = 3 * time.Minute

// cliEnvNames are the variables every claude run gets. The Slack and HTTP tokens
// are not among them.
var cliEnvNames = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "TMPDIR",
	"CLAUDE_CONFIG_DIR", "ANTHROPIC_BASE_URL",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY", "https_proxy", "http_proxy", "no_proxy", "all_proxy",
	"NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

// CLIEnv is the environment for a claude run: cliEnvNames, CLAUDE_CODE_* and
// DISABLE_* variables, and extra, each only when set.
func CLIEnv(extra []string) []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		keep := slices.Contains(cliEnvNames, name) || strings.HasPrefix(name, "CLAUDE_CODE_") ||
			strings.HasPrefix(name, "DISABLE_") || slices.Contains(extra, name)
		return !keep
	})
}

type cliResult struct {
	IsError          bool            `json:"is_error"`
	Subtype          string          `json:"subtype"`
	StopReason       string          `json:"stop_reason"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// NewCLI runs `claude -p` with no tools, no MCP servers and no settings, so a
// message that tries to steer the model has nothing to act with.
func NewCLI(bin, model string, env []string) Ask {
	return func(ctx context.Context, system, user string, dest any) error {
		schema, err := schemaOf(dest)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "-p", "--output-format", "json", "--no-session-persistence",
			"--tools", "", "--strict-mcp-config", "--setting-sources", "",
			"--model", model, "--effort", "low", "--system-prompt", system, "--json-schema", schema)
		cmd.Dir = CLIDir()
		cmd.Env = CLIEnv(env)
		cmd.WaitDelay = 10 * time.Second
		cmd.Stdin = strings.NewReader(user)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		runErr := cmd.Run()
		if ctx.Err() != nil {
			return fmt.Errorf("claude: %w", ctx.Err())
		}
		var res cliResult
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			return fmt.Errorf("claude: %v: %s", cmp.Or(runErr, err), firstLine(stderr.String()+stdout.String()))
		}
		switch {
		case res.StopReason == "refusal":
			return ErrRefused
		case res.IsError || res.Subtype != "success":
			return fmt.Errorf("claude: %s", cmp.Or(firstLine(res.Result), res.Subtype))
		case len(res.StructuredOutput) == 0 || string(res.StructuredOutput) == "null":
			return fmt.Errorf("claude: no structured output: %s", firstLine(res.Result))
		}
		if err := json.Unmarshal(res.StructuredOutput, dest); err != nil {
			return fmt.Errorf("parse answer: %w", err)
		}
		return nil
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
