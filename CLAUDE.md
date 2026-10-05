# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Behaviour and setup are in `README.md`; keep it in step when behaviour or config keys change.

## Commands

- Test and build: `./build` (runs `go test ./...`, then writes `bin/itakeit-messenger`). The Dockerfile runs it too.
- One test: `go test ./pkg/messenger -run TestDuplicateAndShort -v`
- Run: `MESSENGER_SLACK_BOT_TOKEN=xoxb-... MESSENGER_SLACK_APP_TOKEN=xapp-... ./bin/itakeit-messenger -config config.yaml`. `config.yaml` is gitignored; start from `config.example.yaml`.

## Architecture

Dependency direction: `cmd` -> `pkg/source` -> `pkg/messenger` -> `pkg/classify`; `pkg/config` is read by `cmd` and `pkg/source`.

- It writes to Slack only (a top-level message in `target_channel`). It never reads or calls itakeit, whose only input is the channel. It must stay a separate Slack app from itakeit and itakeit-agent, since itakeit ignores its own bot's messages.
- `messenger.Pipeline` is the one path for every source: length filter, dedupe, classify (bounded by `max_parallel`), post. Sources call `Handle` (waits for the verdict, HTTP and stdin) or `Enqueue` (queue of 1024, drops when full, Slack).
- `classify.Ask` is the model seam with two backends: `cli.go` (`claude -p` with `--tools ""`, `--strict-mcp-config`, `--setting-sources ""`, allow-listed environment that never carries the Slack or HTTP tokens) and `api.go`. The schema comes from the `Verdict` struct tags for both. Message text is untrusted: the prompt says so, the model has no tools, and everything it writes is escaped (`messenger.Escape`) before posting. Keep it that way: no tools for the classifier.
- `Origin` in a `Message` is posted unescaped (it carries `<#C123>`), so a source must escape any caller-supplied origin itself (the HTTP source does).
- The target channel is never read (`Config.SlackReads`), or a posted task would be classified again and loop.
- Dedupe keys are sha256 hex (`key`), so a source cannot put NULs or a huge ID into Slack metadata. A key is in flight while its message is processed: `Handle` returns `ErrInFlight` for a second delivery (HTTP answers 409), and a failure forgets the key, ring slot included. The seen set is in memory and refilled on start by `Pipeline.Seed` from the metadata (`itakeit_messenger_task`, payload `key`) of the app's own messages in the target channel (`slackPoster.Recent`). The channel is the only store, as in itakeit. Anything posted must go through `Poster.Post` with its key, or a restart can post it again. Tests fake `Poster` and `Ask`; Slack events are tested through `Slack.Accept`.
- Reminders: `Verdict` carries `reminder`/`remind_at`/`remind_text`; `Classifier.WithReminders(loc)` turns them on, adds the clock to the user message and validates the time (`Due`, 1 minute to 120 days ahead), dropping an unusable reminder rather than failing. `Pipeline.Scheduler` posts a thread reply from the bot (`chat.scheduleMessage`) under `Message.Thread`, which only Slack messages have. Never reply under the posted task: itakeit records the messenger's bot as that task's reporter, and a reporter's reply on a needs-info task changes its status and pings the owners. A failed reminder never fails an already posted task, because a retry would repost it. Reminder-only messages are not recorded in the channel, so `slackPoster.Schedule` skips a reminder the app already has scheduled for the same channel, time and text (`chat.scheduledMessages.list`, no scope needed, no thread in its result). A differently worded repeat after a restart still duplicates (marked `HACK:` in `Pipeline.remind`).
- `tasks` and `reminders` switch the two outputs independently (`Classifier.WithoutTasks`, `WithReminders`; `Pipeline.Scheduler` nil when reminders are off). Each changes the system prompt, so the model is never asked for what is off, and the result is also forced off in code. With tasks off, `target_channel` is optional, recovery of posted tasks is skipped, and `config` rejects http and stdin sources (no thread for a reminder).
- `knowledge` (config, at most 8000 characters) is operator-written trusted text appended to the system prompt for tasks and reminders alike, before the untrusted-message rule, so it can never override that rule. `criteria` is only for tasks.
- Model output is scrubbed of secrets (`classify/scrub.go`, ported from itakeit-agent's `Claude.scrub`/`Probe`): the CLI puts the login email and cwd in the model's context, so a message can ask for them. `main` collects the secrets (tokens, `claudeLogin` email, `CLIDir`, `EnvSecrets`) and `Probe` runs at start. Any new text field from the model must go through `scrub`.
