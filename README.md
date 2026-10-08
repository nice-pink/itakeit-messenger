# itakeit-messenger

Reads messages from several sources, asks Claude whether each contains a task, and posts the tasks as top-level messages to the Slack channel [itakeit](https://github.com/nice-pink/itakeit) serves. itakeit then handles them like any message a person posted: reactions claim them and set their status, and [itakeit-agent](https://github.com/nice-pink/itakeit-agent) can pick them up. The messenger never talks to itakeit directly.

It is a separate app from both. It needs its own Slack app because itakeit ignores its own bot's messages, and it is not part of itakeit-agent because the agent holds tool credentials and works only in itakeit's channels, while the messenger reads many channels and listens on HTTP. Keep those trust boundaries apart.

## What it does

1. A source delivers a message: a top-level message in a Slack channel the app is in, a `POST /messages`, or a line on standard input.
2. Messages shorter than `min_chars` are skipped. Every other message goes to Claude, with no tools, no MCP servers and no settings, so a message that tries to steer the model has nothing to act with.
3. Claude answers with `task`, `title`, `summary` and `reason` (with `reason_first: true` the reason is written before the verdict, meant for small models; it costs a sentence of output and a large model does not need it). A task is a concrete piece of work someone asks for or that clearly needs doing. `criteria` in the config sharpens that for your team. `knowledge` adds background about your tools, for example that a Grafana alert is a task titled "Check incident: <alert name>".
4. For a task the messenger posts `*title*`, the summary, and `From <link|a message> in <#channel>` to `target_channel`. The summary stands alone, since the original is not copied. The author is not mentioned, so posting notifies nobody, and model text is escaped so it cannot ping `@channel` or groups. With `mention_author: true` (default false) the task ends with `by @anna`, which pings the author. For a message from a bot it is `bot_contact` (or the channel's `bot_contacts` entry), and nobody is mentioned without one. HTTP and stdin messages have no Slack user to mention. `bot_contact` and `bot_contacts` thus serve reminders and, when on, task mentions.

5. If the message asks to be reminded at a specific time ("remind me on Friday to send the invoice") and `reminders` is on (default), the messenger schedules a thread reply under the original message at that time, posted by the messenger's bot and mentioning who it is for: `Reminder for @anna: send the invoice`. "Me" is the author of the message. "Remind @bob ..." is for bob, and only a user the message mentions is accepted as a target; anything else falls back to the author. For a message from a bot or integration (`sources.slack.include_bots`) the reminder addresses `bot_contact`, or the channel's entry in `bot_contacts` (channel ID to Slack user ID, the same keys as in itakeit), and mentions nobody when neither is set. Right after scheduling, the messenger replies in the same thread (it mentions the target only when that is not the author, so the target is told at once) with `Reminder scheduled for Fri 10:00: send the invoice` and a Delete button. This is independent of the task: a message can be a task, a reminder, both or neither. The model only copies the time phrase into fields (a distance such as "in two days", a weekday, a calendar date, a clock time) and the messenger works out the date against the current time in `timezone` (default UTC), because small models get calendar arithmetic wrong. A weekday alone is its next occurrence ("Friday" said on Friday morning with a time still ahead means today), "Friday next week" is that day in the following Monday to Sunday week, a day without a time means 09:00, and a distance in minutes or hours counts from now. It never guesses: a vague or missing time creates no reminder, as does a time in the past or more than 120 days ahead (Slack's limit for scheduled messages). Only Slack messages get reminders: an HTTP or stdin message has no Slack message to reply under, and a reply under the task the messenger posted would come from the user itakeit records as the task's reporter, which clears a needs-info status and pings the owners. A reminder whose time passes while the model is still working is dropped. If scheduling fails after the task was posted, it is logged and the task stands.

Slack's own reminders API (`reminders.add`) is retired and cannot remind other users, which is why a scheduled reply is used. Before scheduling, it lists the reminders its app already has scheduled in that channel and skips one with the same time (to the second) and exact text, so a repeat of the same message after a restart does not remind twice. Slack's list carries no thread, so a repeat that the model words differently still schedules twice, and the same user's identical reminder in two threads of one channel at the same time counts as one. Delete cancels the scheduled reply (`chat.deleteScheduledMessage`) and rewrites the confirmation. Only the person the reminder is for and the author of the message can press it. Slack refuses once the reminder is due within a minute or already sent, and the confirmation then says so. The button carries the channel and scheduled message ID, so nothing is stored and it works after a restart. It needs Interactivity enabled in the Slack app (in the manifest; an existing app needs it switched on under Interactivity & Shortcuts, no URL with Socket Mode). If the original message is deleted or the work is done early, the reminder still posts unless someone deletes it.

A message is classified once: redelivery of the same Slack message or the same HTTP `id` is dropped. Each posted task carries a hash of its source key in Slack message metadata, invisible to people. On start the messenger reads the last `recover_messages` (default 200) messages of `target_channel` and takes the keys of its own tasks from there, so a restart does not post a task twice and no database is needed. This covers tasks only: messages judged not to be one are not recorded, and nothing replays them. If more than `recover_messages` messages were posted since a task, the task is forgotten. The messenger refuses to start when it cannot read the channel.

## Setup

1. Create a Slack app from `slack-app-manifest.yaml`, generate an app-level token with `connections:write` (`MESSENGER_SLACK_APP_TOKEN`, only needed for the Slack source) and install it (`MESSENGER_SLACK_BOT_TOKEN`).
2. Invite it to `target_channel` and to every channel it should read. It must be a different app from itakeit and itakeit-agent.
3. Copy `config.example.yaml` to `config.yaml`, set `target_channel` and enable sources. `tasks` (default on) and `reminders` (default on) choose what the messenger creates. With `tasks: false` it only sets reminders: `target_channel` is optional, nothing is posted to it, and only the `slack` source is allowed, since a reminder is a reply under a Slack message. At least one of the two must be on.
4. Log in to Claude: `claude auth login`, or `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token` (`backend: claude-code`), or `ANTHROPIC_API_KEY` (`backend: api`), or `LANGDOCK_API_KEY` with `backend: langdock`, `model` set to a model ID of your workspace and `langdock_region: eu` or `us` (Langdock's OpenAI-compatible endpoint, any model), or `OPENAI_API_KEY` with `backend: openai` and `model` set to an OpenAI model ID (`openai_base_url` points it at a compatible server such as vLLM, SGLang or Ollama running Qwen, where the key is optional; vLLM needs `--reasoning-parser qwen3` for Qwen3, and a leading `<think>` block is dropped anyway). The messenger checks the CLI login at start and refuses to run without it.
5. Run it: `./build && MESSENGER_SLACK_BOT_TOKEN=xoxb-... MESSENGER_SLACK_APP_TOKEN=xapp-... ./bin/itakeit-messenger -config config.yaml`, or the image:

```
docker run -d --name itakeit-messenger --restart unless-stopped -e MESSENGER_SLACK_BOT_TOKEN -e MESSENGER_SLACK_APP_TOKEN -e CLAUDE_CODE_OAUTH_TOKEN -v "$PWD/config.yaml:/config/config.yaml:ro" ghcr.io/nice-pink/itakeit-messenger:latest
```

`latest` follows `main`. Release tags `vX.Y.Z` also publish `X.Y.Z` and `X.Y`, and every build publishes `sha-<short>`, for linux/amd64 and linux/arm64. With `backend: langdock` run `ghcr.io/nice-pink/itakeit-messenger-langdock` (same tags, or build `Dockerfile.langdock`) instead, and with `backend: openai` run `ghcr.io/nice-pink/itakeit-messenger-openai` (or build `Dockerfile.openai`). It has no Claude Code CLI and no Node, so it is small, and a config with `backend: claude-code` fails its start probe there. Pass `LANGDOCK_API_KEY` or `OPENAI_API_KEY` instead of `CLAUDE_CODE_OAUTH_TOKEN`.

`examples/` has a Docker Compose file and a Kubernetes kustomization, both with a read-only root filesystem and writable `/tmp` and `/home/node`, which the Claude CLI needs on every call.

| Scope | Used for |
|---|---|
| `channels:history`, `groups:history` | receiving messages of channels it is in |
| `chat:write` | posting tasks to the target channel, scheduling reminder replies, confirming them and deleting them |

Reading a private channel needs `include_private: true`. The summary and a link to the message then appear in the target channel, which is visible to people who cannot see the original.

## Sources

- `slack`: top-level messages, from every channel the app is in (not the target channel), or from `channels` only, minus `exclude`. A message with a file attached is read. Replies, edits, DMs and, by default, other bots' messages are ignored. Only the message text is read, not the blocks or attachments of bot messages, so an alert whose content is not in its text is too short to pass `min_chars`.
- `http`: `POST /messages` with `Authorization: Bearer $MESSENGER_HTTP_TOKEN` (16+ characters) and a JSON body of at most 64 KB. Only `text` is required. `source` names the sender, `url` is linked in the task, `author` is shown to the model, and `id` makes retries idempotent (default: a hash of the text). The call waits for the verdict and answers `{"task":true,"posted":true,"title":"...","reason":"..."}`; `502` means classification or posting failed, and `409` means the same message is still being processed. In both cases send it again. It binds to `127.0.0.1` by default, which a container does not publish even with `-p`: set `listen: 0.0.0.0:8080` there. Put a reverse proxy with TLS in front before exposing it put a reverse proxy with TLS in front of it before exposing it.
- `stdin`: one message per line, handled in order. The messenger exits at end of input when no other source runs.

Adding a source means implementing `messenger.Source` (`Name`, `Run`) and handing messages to the `Sink`.

```
curl -s -H "Authorization: Bearer $MESSENGER_HTTP_TOKEN" -d '{"text":"The nightly export has failed three times, can someone look?","source":"cron"}' http://127.0.0.1:8080/messages
```

## Cost

Each message that passes `min_chars` is one model call, at low effort and with a cached system prompt. Reading every channel of a busy workspace adds up: list `channels`, or use a small `model`.

## Limits

- Slack events are acknowledged at once and queued in memory (1024). On shutdown the queue is not drained: messages still waiting are lost, and Slack does not redeliver them.
- Everything the model writes is escaped, so it cannot ping, and scrubbed of the CLI's login email, the Slack and HTTP tokens, the Claude login token, `env` values and the CLI's working directory (replaced by `[redacted]`, with a warning in the log). The email is learned at start from `claude auth status` and from a probe call that asks the model what email its context carries (also the check that the login and model work). A model that declines the probe, or an `ANTHROPIC_API_KEY` login with no email, leaves that one string unscrubbed: the start log shows `scrubbed_strings`. Values shorter than 8 characters are not scrubbed. The prompt also tells the model not to reveal its environment.
- Mentions and links in the source text reach the model and the task as Slack's escaped markup (`&lt;@U0123&gt;`), so a task can show a raw user ID.

## Homepage

`homepage/` is a Svelte site prerendered at build time (`cd homepage && npm ci && npm run build`, output in `homepage/dist`). It is served at `itakeit-messenger.nice.pink`, set in `src/lib/site.ts`, `index.html` and `public/`. It is outside the Docker image.

## License

GPL-3.0, see [LICENSE](LICENSE).
