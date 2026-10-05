<script lang="ts">
  import manifest from '../../slack-app-manifest.yaml?raw'
  import Code from './lib/Code.svelte'
  import Demo from './lib/Demo.svelte'
  import { agent, itakeit, repo } from './lib/site'

  const features = [
    { title: 'Reads where people write', text: 'Top-level messages from the Slack channels it is in, a POST /messages endpoint for alerts and forms, or lines on standard input. All three go through the same pipeline.' },
    { title: 'Asks Claude, with no tools', text: 'Each message long enough to matter goes to Claude with no tools, no MCP servers and no settings. A message that tries to steer the model has nothing to act with.' },
    { title: 'Posts a task that stands alone', text: 'A bold title, a short summary and a link to the original, posted to the itakeit channel. Nobody is mentioned, and model text is escaped so it cannot ping @channel.' },
    { title: 'Reminds in the thread', text: '“Remind me on Friday to send the invoice” becomes a scheduled reply under the original message, mentioning the author. Vague times create no reminder.' },
    { title: 'Counts a message once', text: 'Redelivery of the same Slack message or HTTP id is dropped. Each posted task carries a hash of its source key in invisible message metadata.' },
    { title: 'No database', text: 'On start it reads the last messages of the target channel and takes the keys of its own tasks from there, so a restart does not post a task twice.' },
    { title: 'Plugs into itakeit', text: 'It never talks to itakeit. itakeit sees a message like any other, so claims, status and reminders work, and itakeit-agent can pick the task up.' },
    { title: 'Scrubs what the model writes', text: 'The CLI login email, the Slack and HTTP tokens and the working directory are replaced with [redacted] before anything is posted.' },
  ]

  const config = `tasks: true
target_channel: C0123456789   # the channel itakeit serves (its ID)
criteria: |
  Requests to people ("can someone look at ..."), bug reports, and
  questions that need a person to investigate. Not tasks: deploy
  notices and alerts that resolved themselves.
reminders: true
timezone: Europe/Berlin
sources:
  slack:
    enabled: true
    channels: []              # empty: every channel the app is in
  http:
    enabled: false
    listen: 127.0.0.1:8080`

  const run = `docker run -d --name itakeit-messager --restart unless-stopped -e MESSAGER_SLACK_BOT_TOKEN -e MESSAGER_SLACK_APP_TOKEN -e CLAUDE_CODE_OAUTH_TOKEN -v "$PWD/config.yaml:/config/config.yaml:ro" ghcr.io/nice-pink/itakeit-messager:latest`

  const build = `git clone https://github.com/nice-pink/itakeit-messager.git && cd itakeit-messager && ./build`

  const curl = `curl -s -H "Authorization: Bearer $MESSAGER_HTTP_TOKEN" -d '{"text":"The nightly export has failed three times, can someone look?","source":"cron"}' http://127.0.0.1:8080/messages`

  const reply = `{"task":true,"posted":true,"title":"Fix the failing nightly export","reason":"..."}`

  const sources = [
    ['slack', 'Top-level messages from every channel the app is in (not the target channel), or from channels only, minus exclude. Replies, edits, DMs and other bots’ messages are ignored by default. Private channels need include_private: true.'],
    ['http', 'POST /messages with a Bearer token (16+ characters) and a JSON body of at most 64 KB. Only text is required. The call waits for the verdict. 502 means classification or posting failed, 409 means the same message is still in flight: send it again. Binds to 127.0.0.1.'],
    ['stdin', 'One message per line, handled in order. The messager exits at end of input when no other source runs.'],
  ]
</script>

<header class="nav">
  <div class="wrap row">
    <a class="brand" href="#top"><img src="./turtle-post.png" alt="" /> itakeit-messager</a>
    <nav>
      <a href="#how">How it works</a>
      <a href="#setup">Setup</a>
      <a href="#sources">Sources</a>
      <a href="#reminders">Reminders</a>
      <a href={repo}>GitHub</a>
    </nav>
  </div>
</header>

<main id="top">
  <section class="hero wrap">
    <div class="pitch">
      <img class="turtle" src="./turtle-post.png" alt="pixel turtle in a postal cap carrying an envelope" width="1268" height="1240" />
      <h1>The tasks in your messages. <span>Posted to itakeit.</span></h1>
      <p class="lead"><b>itakeit-messager</b> reads messages from Slack channels, HTTP and standard input, asks Claude whether each contains a task, and posts the tasks to the channel <a href={itakeit}>itakeit</a> serves. It also sets reminders in the thread when a message asks for one. Nobody has to copy a request into the task channel by hand.</p>
      <div class="cta">
        <a class="btn" href="#setup">Set it up</a>
        <a class="btn ghost" href={repo}>View on GitHub</a>
      </div>
      <p class="small">Self-hosted. One Docker container, your Claude login, no database.</p>
    </div>
    <div class="demo">
      <Demo />
      <p class="small center">Try it: pick a message and let the messager read it.</p>
    </div>
  </section>

  <section id="how" class="band">
    <div class="wrap">
      <h2>How it works</h2>
      <p class="sub">A separate Slack app, so itakeit treats its posts like those of any person.</p>
      <div class="grid">
        {#each features as f (f.title)}
          <article class="card">
            <h3>{f.title}</h3>
            <p>{f.text}</p>
          </article>
        {/each}
      </div>
    </div>
  </section>

  <section id="setup" class="wrap setup">
    <h2>Setup</h2>
    <p class="sub">About ten minutes. You need a channel running <a href={itakeit}>itakeit</a>, a Claude login, and a machine that runs Docker.</p>

    <ol class="steps">
      <li>
        <h3>Create a Slack app</h3>
        <p>It must be a different app from itakeit, which ignores its own bot’s messages, and from <a href={agent}>itakeit-agent</a>, which holds tool credentials. Open <a href="https://api.slack.com/apps">api.slack.com/apps</a>, choose <b>Create New App</b>, then <b>From an app manifest</b>, and paste this manifest.</p>
        <details>
          <summary>Show slack-app-manifest.yaml</summary>
          <Code code={manifest.trim()} label="slack-app-manifest.yaml" />
        </details>
        <p>It requests <code>channels:history</code>, <code>groups:history</code> and <code>chat:write</code>, and subscribes to channel messages. Socket Mode, so no request URL.</p>
      </li>
      <li>
        <h3>Get the two tokens</h3>
        <p>Under <b>Basic Information → App-Level Tokens</b>, generate a token with <code>connections:write</code>: that <code>xapp-…</code> token is <code>MESSAGER_SLACK_APP_TOKEN</code>, needed for the Slack source only. Under <b>Install App</b>, install the app and copy the <code>xoxb-…</code> Bot User OAuth Token: <code>MESSAGER_SLACK_BOT_TOKEN</code>.</p>
      </li>
      <li>
        <h3>Invite it to the channels</h3>
        <p>Invite it to the target channel and to every channel it should read.</p>
      </li>
      <li>
        <h3>Write config.yaml</h3>
        <p>Start from <a href="{repo}/blob/main/config.example.yaml">config.example.yaml</a>. <code>target_channel</code> is the ID of the itakeit channel. <code>criteria</code> sharpens what counts as a task for your team. <code>tasks</code> and <code>reminders</code> switch the two outputs independently; at least one must be on.</p>
        <Code code={config} label="config.yaml" />
      </li>
      <li>
        <h3>Log in to Claude</h3>
        <p>Run <code>claude auth login</code>, or <code>claude setup-token</code> once and keep the token as <code>CLAUDE_CODE_OAUTH_TOKEN</code>. To bill the API instead, set <code>backend: api</code> and pass <code>ANTHROPIC_API_KEY</code>. The messager checks the login at start and refuses to run without it.</p>
      </li>
      <li>
        <h3>Run the container</h3>
        <p>Export the tokens, then start the published image from the directory with <code>config.yaml</code>.</p>
        <Code code={'export MESSAGER_SLACK_BOT_TOKEN=xoxb-... MESSAGER_SLACK_APP_TOKEN=xapp-... CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...'} label="shell" />
        <Code code={run} label="shell" />
        <p>Or build the binary yourself and run <code>bin/itakeit-messager -config config.yaml</code>:</p>
        <Code code={build} label="shell" />
      </li>
      <li>
        <h3>Check it</h3>
        <p>Post a request in a channel it reads, such as “can someone look at the failing export?”. A task appears in the target channel. A setup problem (login, config, a channel it cannot read) stops it at startup with an error that names the fix.</p>
      </li>
    </ol>

    <div class="note">
      <b>Private channels leak a summary.</b> A task from a private channel shows its summary and a link in the target channel, which people who cannot see the original may read. That is why private channels are off unless <code>include_private: true</code>.
    </div>
  </section>

  <section id="sources" class="band">
    <div class="wrap">
      <h2>Sources</h2>
      <p class="sub">Every source feeds the same pipeline: length filter, dedupe, Claude, post.</p>
      <div class="split">
        <div>
          <p class="scroll-hint">Scroll the table sideways →</p>
          <div class="table">
            <table>
              <thead><tr><th>Source</th><th>Reads</th></tr></thead>
              <tbody>
                {#each sources as [name, text] (name)}
                  <tr><td>{name}</td><td>{text}</td></tr>
                {/each}
              </tbody>
            </table>
          </div>
          <p class="cost"><b>Cost.</b> Each message that passes <code>min_chars</code> is one model call at low effort with a cached system prompt. Reading every channel of a busy workspace adds up: list <code>channels</code>, or use a small <code>model</code>.</p>
        </div>
        <div>
          <p>Send a message from a script, a monitoring hook or a form:</p>
          <Code code={curl} label="shell" />
          <p>The answer is the decision:</p>
          <Code code={reply} label="response" />
        </div>
      </div>
    </div>
  </section>

  <section id="reminders" class="wrap setup">
    <h2>Reminders</h2>
    <p class="sub">On by default. Independent of tasks: a message can be a task, a reminder, both or neither.</p>
    <div class="points">
      <p><b>A scheduled reply.</b> A message such as “remind me on Friday to send the invoice” gets a reply under it at that time, from the messager’s bot, mentioning the author: <code>Reminder for @anna: send the invoice</code>. Slack’s own reminders API is retired and cannot remind other users, so a scheduled reply is used.</p>
      <p><b>It never guesses.</b> Relative times resolve against the current time in <code>timezone</code>. A vague or missing time creates no reminder, as does a time in the past or more than 120 days ahead, which is Slack’s limit. A date without a time means 09:00.</p>
      <p><b>Slack messages only.</b> An HTTP or stdin message has no Slack message to reply under. A reply under the task the messager posted would come from the user itakeit records as the task’s reporter, which clears a needs-info status and pings the owners.</p>
      <p><b>No cancelling.</b> It skips a reminder its app already has scheduled for the same channel, time and text, so a restart does not remind twice. A differently worded repeat still does. If the original message is deleted or the work is done early, the reminder still posts.</p>
      <p><b>Reminders only.</b> With <code>tasks: false</code> nothing is posted to a channel, <code>target_channel</code> is optional, and only the Slack source is allowed.</p>
    </div>
  </section>

  <section class="band">
    <div class="wrap">
      <h2>Limits</h2>
      <div class="points">
        <p><b>Queue loss.</b> Slack events are acknowledged at once and queued in memory (1024). On shutdown the queue is not drained, and Slack does not redeliver.</p>
        <p><b>Recovery covers tasks only.</b> Messages judged not to be a task are not recorded, so nothing replays them. If more than <code>recover_messages</code> (default 200) messages were posted since a task, a redelivery of its source would post it again.</p>
        <p><b>Raw IDs.</b> Mentions and links reach the model and the task as Slack’s escaped markup, so a task can show a raw user ID.</p>
        <p><b>Scrubbing is best effort.</b> Values shorter than 8 characters are not scrubbed, and a model that declines the start-up probe leaves the login email unscrubbed.</p>
      </div>
    </div>
  </section>
</main>

<footer>
  <div class="wrap row">
    <span><img src="./turtle-post.png" alt="" /> itakeit-messager</span>
    <div>Needs <a href={itakeit}>itakeit</a>. Pairs with <a href={agent}>itakeit-agent</a>.</div>
    <div>built by <a href="https://nice.pink">nice-pink</a></div>
  </div>
</footer>

<style>
  .wrap { max-width: 1120px; margin: 0 auto; padding: 0 16px; }
  .row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
  .small { font-size: 0.85rem; color: var(--muted); }
  .center { text-align: center; }

  .nav { position: sticky; top: 0; z-index: 10; background: color-mix(in srgb, var(--bg) 92%, transparent); backdrop-filter: blur(6px); border-bottom: 2px solid var(--ink); }
  .nav .row { min-height: 60px; }
  .brand { display: flex; align-items: center; gap: 0.5rem; font: 700 1.15rem var(--mono); color: var(--ink); text-decoration: none; }
  .brand img, footer img { width: auto; height: 34px; image-rendering: pixelated; vertical-align: middle; }
  nav { display: flex; gap: 1.1rem; flex-wrap: wrap; }
  nav a { color: var(--ink); text-decoration: none; font-weight: 600; font-size: 0.95rem; }
  nav a:hover { color: var(--green); }

  .hero { display: grid; grid-template-columns: 1fr 1.05fr; gap: 3rem; align-items: center; padding-top: 3rem; padding-bottom: 4rem; }
  .turtle { width: 170px; height: auto; image-rendering: pixelated; margin: 0 0 1rem -8px; }
  h1 { font: 800 clamp(2.2rem, 5vw, 3.5rem)/1.05 var(--sans); letter-spacing: -0.03em; margin: 0 0 1rem; }
  h1 span { color: var(--green); display: block; }
  .lead { font-size: 1.15rem; color: var(--muted); margin: 0 0 1.5rem; max-width: 34rem; }
  .cta { display: flex; gap: 0.8rem; flex-wrap: wrap; margin-bottom: 0.8rem; }
  .btn { display: inline-block; padding: 0.75rem 1.3rem; font-weight: 700; text-decoration: none; background: var(--green-dark); color: #fff; border: 2px solid var(--ink); box-shadow: 4px 4px 0 var(--ink); transition: transform 0.1s, box-shadow 0.1s; }
  .btn:hover { transform: translate(-2px, -2px); box-shadow: 6px 6px 0 var(--ink); }
  .btn:active { transform: translate(2px, 2px); box-shadow: 2px 2px 0 var(--ink); }
  .btn.ghost { background: var(--panel); color: var(--ink); }
  .demo { min-width: 0; }

  h2 { font: 800 clamp(1.8rem, 4vw, 2.5rem)/1.1 var(--sans); letter-spacing: -0.02em; margin: 0 0 0.4rem; }
  .sub { color: var(--muted); margin: 0 0 2rem; }
  section { scroll-margin-top: 70px; }

  .band { background: var(--green-soft); border-top: 2px solid var(--ink); border-bottom: 2px solid var(--ink); padding: 4rem 0; }
  .grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 1.2rem; }
  .card { background: var(--panel); border: 2px solid var(--ink); box-shadow: 4px 4px 0 var(--ink); padding: 1.2rem 1.3rem; }
  .card h3 { margin: 0.4rem 0 0.3rem; font-size: 1.1rem; }
  .card p { margin: 0; color: var(--muted); font-size: 0.95rem; }

  .setup { padding-top: 4rem; padding-bottom: 4rem; }
  .steps { list-style: none; counter-reset: step; padding: 0; margin: 0; max-width: 820px; }
  .steps > li { counter-increment: step; position: relative; padding: 0 0 1.5rem 3.6rem; border-left: 2px dashed var(--line); margin-left: 1.2rem; min-width: 0; }
  .steps > li:last-child { border-left-color: transparent; }
  .steps > li::before { content: counter(step); position: absolute; left: -1.25rem; top: -0.2rem; width: 2.5rem; height: 2.5rem; display: grid; place-items: center; font: 700 1.1rem var(--mono); background: var(--green); color: #fff; border: 2px solid var(--ink); box-shadow: 3px 3px 0 var(--ink); }
  .steps h3 { margin: 0 0 0.4rem; font-size: 1.2rem; }
  .steps p { margin: 0 0 0.6rem; }
  details { margin: 0.4rem 0 0.8rem; }
  summary { cursor: pointer; font-weight: 600; color: var(--green-dark); }
  .note { max-width: 820px; background: #fff3e6; border: 2px solid var(--ink); border-left: 8px solid var(--orange); padding: 1rem 1.2rem; }

  .split { display: grid; grid-template-columns: 1fr 1fr; gap: 2rem; align-items: start; }
  .split > :global(.code) { margin-top: 0; }
  .points { max-width: 820px; }
  .points p { margin: 0 0 1rem; }
  .cost { margin: 1.2rem 0 0; }

  .table { overflow-x: auto; background: var(--panel); border: 2px solid var(--ink); box-shadow: 4px 4px 0 var(--ink); }
  table { width: 100%; border-collapse: collapse; font-size: 0.95rem; }
  th, td { text-align: left; padding: 0.65rem 0.9rem; border-bottom: 1px solid var(--line); vertical-align: top; }
  th { background: var(--ink); color: #fff; font-weight: 600; }
  td:first-child { font-family: var(--mono); font-size: 0.85rem; white-space: nowrap; color: var(--green-dark); }
  tr:last-child td { border-bottom: 0; }

  footer { border-top: 2px solid var(--ink); padding: 1.2rem 0; font-size: 0.9rem; }
  footer span { font: 700 1rem var(--mono); }

  @media (max-width: 1000px) {
    .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 860px) {
    .hero, .split { grid-template-columns: 1fr; gap: 2rem; }
    .hero { padding-top: 1.5rem; }
    .turtle { width: 120px; }
    nav { gap: 0.8rem; }
    nav a { font-size: 0.85rem; }
  }
  .scroll-hint { display: none; margin: 0 0 0.4rem; font-size: 0.8rem; color: var(--muted); }
  @media (max-width: 520px) {
    .scroll-hint { display: block; }
    td:first-child { white-space: normal; }
    nav a:not(:last-child):not([href="#setup"]) { display: none; }
    .steps > li { padding-left: 2.2rem; margin-left: 1.2rem; }
    .grid { grid-template-columns: minmax(0, 1fr); }
  }
</style>
