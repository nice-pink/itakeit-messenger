package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nice-pink/itakeit-messager/pkg/classify"
	"github.com/nice-pink/itakeit-messager/pkg/config"
	"github.com/nice-pink/itakeit-messager/pkg/messager"
	"github.com/nice-pink/itakeit-messager/pkg/source"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to the config file")
	debug := flag.Bool("debug", false, "log raw Socket Mode traffic")
	flag.Parse()
	if err := run(*cfgPath, *debug); err != nil {
		slog.Error("itakeit-messager stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfgPath string, debug bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	botToken, appToken := os.Getenv("MESSAGER_SLACK_BOT_TOKEN"), os.Getenv("MESSAGER_SLACK_APP_TOKEN")
	if !strings.HasPrefix(botToken, "xoxb-") {
		return errors.New("set MESSAGER_SLACK_BOT_TOKEN (xoxb-...) from the messager's own Slack app")
	}
	if cfg.Sources.Slack.Enabled && !strings.HasPrefix(appToken, "xapp-") {
		return errors.New("sources.slack needs MESSAGER_SLACK_APP_TOKEN (xapp-...)")
	}
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken), slack.OptionDebug(debug),
		slack.OptionHTTPClient(&http.Client{Timeout: 15 * time.Second}), slack.OptionRetry(3))
	auth, err := api.AuthTest()
	if err != nil {
		return err
	}

	var ask classify.Ask
	secrets := []string{botToken, appToken, os.Getenv("MESSAGER_HTTP_TOKEN"), os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("ANTHROPIC_AUTH_TOKEN")}
	if cfg.Backend == config.BackendAPI {
		ask = classify.NewAPI(cfg.Model)
	} else {
		email, err := claudeLogin(ctx, cfg.ClaudeBin, cfg.Env)
		if err != nil {
			return err
		}
		secrets = append(secrets, email, classify.CLIDir())
		secrets = append(secrets, classify.EnvSecrets(cfg.Env)...)
		ask = classify.NewCLI(cfg.ClaudeBin, cfg.Model, cfg.Env)
	}
	slog.Info("authenticated", "team", auth.Team, "user", auth.UserID, "tasks", cfg.Tasks, "reminders", cfg.Reminders, "target_channel", cfg.TargetChannel, "backend", cfg.Backend, "model", cfg.Model)

	cl := classify.New(ask, cfg.Criteria).WithSecrets(secrets...)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	refused, err := cl.Probe(probeCtx)
	cancel()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backend %s, model %q: %w (the CLI gets only an allow-listed environment: list any variable it needs in env)", cfg.Backend, cfg.Model, err)
	}
	slog.Info("model ready", "scrubbed_strings", cl.Secrets(), "probe_refused", refused)
	if !cfg.Tasks {
		cl.WithoutTasks()
	}
	if cfg.Reminders {
		loc, _ := time.LoadLocation(cfg.Timezone) // validated by config
		cl.WithReminders(loc)
	}
	p := messager.New(cl, slackPoster{api: api}, cfg.TargetChannel, cfg.MinChars, cfg.MaxParallel)
	if cfg.Reminders {
		p.Scheduler = slackPoster{api: api}
	}
	if cfg.Tasks && cfg.RecoverMessages > 0 {
		keys, err := slackPoster{api: api}.Recent(ctx, cfg.TargetChannel, cfg.RecoverMessages)
		if err != nil {
			return fmt.Errorf("read target_channel %s: %w (check the ID and that the messager's app is invited, with channels:history or groups:history)", cfg.TargetChannel, err)
		}
		p.Seed(keys)
		slog.Info("recovered posted tasks", "count", len(keys), "messages_read", cfg.RecoverMessages)
	}
	go p.Run(ctx)

	var sources []messager.Source
	if cfg.Sources.Slack.Enabled {
		sources = append(sources, &source.Slack{API: api, SM: socketmode.New(api, socketmode.OptionDebug(debug)), Cfg: cfg, UserID: auth.UserID, BotID: auth.BotID})
	}
	if cfg.Sources.HTTP.Enabled {
		sources = append(sources, &source.HTTP{Listen: cfg.Sources.HTTP.Listen, Token: os.Getenv("MESSAGER_HTTP_TOKEN")})
	}
	if cfg.Sources.Stdin {
		sources = append(sources, &source.Stdin{R: os.Stdin})
	}

	var wg sync.WaitGroup
	errc := make(chan error, len(sources))
	for _, s := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Run(ctx, p); err != nil {
				errc <- fmt.Errorf("source %s: %w", s.Name(), err)
				stop()
			}
		}()
	}
	wg.Wait()
	close(errc)
	return errors.Join(drain(errc)...)
}

func drain(c <-chan error) (errs []error) {
	for err := range c {
		errs = append(errs, err)
	}
	return
}

// claudeLogin fails at start, rather than on the first message, when the CLI is
// missing or not logged in.
func claudeLogin(ctx context.Context, bin string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "auth", "status")
	cmd.Env = classify.CLIEnv(env)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("backend claude-code: `%s auth status` failed (%w): install Claude Code and run `claude auth login`, or set CLAUDE_CODE_OAUTH_TOKEN from `claude setup-token`", bin, err)
	}
	var st struct {
		LoggedIn bool   `json:"loggedIn"`
		Email    string `json:"email"`
	}
	if json.Unmarshal(out, &st) != nil || !st.LoggedIn {
		return "", errors.New("backend claude-code: Claude Code is not logged in: run `claude auth login`, or set CLAUDE_CODE_OAUTH_TOKEN from `claude setup-token`")
	}
	return st.Email, nil
}
