<script lang="ts">
  type Mode = 'task' | 'reminder' | 'neither'
  type Step = { verdict?: string; post?: { title: string; text: string; from: string }; reminder?: string }

  // One scripted run per kind of message: the messager reads it, Claude
  // answers, and the messager posts the task, schedules the reminder, or does nothing.
  const scripts: Record<Mode, { channel: string; text: string; steps: Step[] }> = {
    task: {
      channel: '# backend',
      text: 'The nightly export has failed three times this week. Can someone look at it before Monday?',
      steps: [
        { verdict: 'task: yes. "Fix the failing nightly export"' },
        { post: { title: 'Fix the failing nightly export', text: 'The nightly export failed three times this week. Someone needs to find the cause before Monday.', from: 'a message in #backend' } },
      ],
    },
    reminder: {
      channel: '# finance',
      text: 'Remind me on Friday at 10 to send the invoice to Acme.',
      steps: [
        { verdict: 'task: no. reminder: Friday 10:00, "send the invoice to Acme"' },
        { reminder: 'Fri 10:00 · Reminder for @Maya: send the invoice to Acme' },
      ],
    },
    neither: {
      channel: '# general',
      text: 'Lunch at 12? The new place on the corner has a table for six.',
      steps: [{ verdict: 'task: no. reminder: none. Nothing is posted.' }],
    },
  }

  const modes: { key: Mode; label: string; note: string }[] = [
    { key: 'task', label: 'task', note: 'A concrete piece of work someone asks for. It is posted to the itakeit channel as a task.' },
    { key: 'reminder', label: 'reminder', note: 'A request to be reminded at a specific time. It is scheduled as a reply in the thread.' },
    { key: 'neither', label: 'neither', note: 'Everything else is dropped after the model call. No record, no ping.' },
  ]

  let mode = $state<Mode>('task')
  let step = $state(0)

  let script = $derived(scripts[mode])
  let shown = $derived(script.steps.slice(0, step))
  let done = $derived(step === script.steps.length)

  function pick(m: Mode) {
    mode = m
    step = 0
  }
  function next() {
    if (step < script.steps.length) step++
  }
</script>

<div class="wrapper">
  <div class="tabs" role="group" aria-label="Kind of message">
    {#each modes as m (m.key)}
      <button type="button" aria-pressed={mode === m.key} class:on={mode === m.key} onclick={() => pick(m.key)}>{m.label}</button>
    {/each}
  </div>
  <p class="note">{modes.find((m) => m.key === mode)?.note}</p>

  <div class="slack" role="region" aria-label="Interactive example of the messager reading a Slack message">
    <div class="head"><span>{script.channel}</span><button type="button" class="reset" onclick={() => pick(mode)}>reset</button></div>

    <div class="msg">
      <div class="avatar a1">M</div>
      <div class="body">
        <div class="meta"><b>Maya</b> <span>10:42</span></div>
        <p>{script.text}</p>

        <div class="thread" aria-live="polite">
          {#each shown as s, i (i)}
            {#if s.verdict}
              <p class="tool"><span>the messager asks Claude, with no tools:</span> <code>{s.verdict}</code></p>
            {/if}
            {#if s.reminder}
              <div class="msg">
                <div class="avatar agent"><img src="./turtle-parrot.png" alt="" /></div>
                <div class="body">
                  <div class="meta"><b>itakeit-messager</b> <span class="app">APP</span> <span>scheduled</span></div>
                  <p>{s.reminder}</p>
                </div>
              </div>
            {/if}
          {/each}
        </div>

        {#each shown as s, i (i)}
          {#if s.post}
            <div class="target">
              <div class="meta"><b># itakeit</b> <span>top-level message</span></div>
              <div class="msg inner">
                <div class="avatar agent"><img src="./turtle-parrot.png" alt="" /></div>
                <div class="body">
                  <div class="meta"><b>itakeit-messager</b> <span class="app">APP</span></div>
                  <p><b>{s.post.title}</b><br />{s.post.text}<br /><span class="from">From {s.post.from}</span></p>
                </div>
              </div>
              <p class="small">itakeit picks it up from here, like any message a person posted.</p>
            </div>
          {/if}
        {/each}
      </div>
    </div>

    <div class="foot">
      {#if !done}
        <button type="button" class="step" onclick={next}>{step === 0 ? '▶ Let the messager read it' : '▶ Next step'}</button>
      {:else}
        <span class="small">Done. <button type="button" class="link" onclick={() => pick(mode)}>Run it again</button></span>
      {/if}
    </div>
  </div>
</div>

<style>
  .tabs { display: flex; gap: 0.4rem; margin-bottom: 0.5rem; }
  .tabs button { font: 700 0.85rem var(--mono); padding: 0.35rem 0.8rem; border: 2px solid var(--ink); background: var(--panel); color: var(--ink); cursor: pointer; box-shadow: 3px 3px 0 var(--ink); }
  .tabs button.on { background: var(--green-dark); color: #fff; }
  .note { margin: 0 0 0.8rem; font-size: 0.85rem; color: var(--muted); min-height: 2.6em; }
  .slack { background: #fff; color: #1d1c1d; border: 2px solid var(--ink); box-shadow: 6px 6px 0 var(--ink); font-size: 0.92rem; text-align: left; min-width: 0; }
  .head { display: flex; justify-content: space-between; align-items: center; padding: 0.55rem 0.9rem; border-bottom: 1px solid #e3e3e3; font-weight: 700; }
  .reset { font: 0.75rem var(--mono); background: none; border: 1px solid #ccc; padding: 0.2rem 0.5rem; cursor: pointer; color: #555; }
  .msg { display: flex; gap: 0.6rem; padding: 0.7rem 0.9rem 0.2rem; }
  .msg.inner { padding: 0.3rem 0 0.1rem; }
  .body { min-width: 0; flex: 1; }
  .body p { margin: 0.15rem 0 0.35rem; }
  .avatar { flex: none; width: 36px; height: 36px; display: grid; place-items: center; font-weight: 700; color: #fff; }
  .a1 { background: #a84474; }
  .agent { background: var(--green-soft); border: 1px solid #b8dcc9; overflow: hidden; }
  .agent img { width: 34px; height: 34px; object-fit: contain; }
  .meta { font-size: 0.85rem; }
  .meta span { color: #666; font-size: 0.75rem; margin-left: 0.25rem; }
  .meta .app { background: #eee; padding: 0 0.25rem; font-size: 0.65rem; font-weight: 700; }
  .thread { border-left: 2px solid #e3e3e3; margin: 0.3rem 0 0.6rem; min-height: 0.2rem; }
  .thread .msg { padding: 0.4rem 0.6rem 0.1rem; }
  .tool { margin: 0.2rem 0.6rem 0.2rem 0.6rem; font-size: 0.78rem; color: #616061; overflow-wrap: anywhere; }
  .tool code { background: #f4f4f4; padding: 0 0.25rem; font-family: var(--mono); }
  .target { border: 1px solid #e3e3e3; background: #fbfbfb; padding: 0.5rem 0.7rem 0.3rem; margin: 0.3rem 0 0.6rem; }
  .from { color: #616061; font-size: 0.8rem; }
  .small { font-size: 0.8rem; color: #616061; }
  .foot { border-top: 1px dashed #ccc; padding: 0.6rem 0.9rem 0.7rem; background: #fbfaf5; min-height: 3.2rem; }
  .step { font: 700 0.85rem var(--sans); background: var(--green-dark); color: #fff; border: 2px solid var(--ink); padding: 0.4rem 0.8rem; cursor: pointer; box-shadow: 3px 3px 0 var(--ink); }
  .link { background: none; border: 0; padding: 0; color: var(--green-dark); text-decoration: underline; cursor: pointer; font: inherit; }
</style>
