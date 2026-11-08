<script setup>
import { PhArrowRight, PhCheck, PhArrowCounterClockwise } from '@phosphor-icons/vue'
import { computed, nextTick, ref } from 'vue'

const sessions = [
  {
    id: '$3', name: 'atlas', description: 'Your application',
    windows: [
      { id: '@8', name: 'editor', pane: '%12', command: 'nvim' },
      { id: '@9', name: 'terminal', pane: '%13', command: '$ _' },
    ],
  },
  {
    id: '$4', name: 'website', description: 'Your documentation',
    windows: [
      { id: '@10', name: 'docs', pane: '%14', command: 'nvim' },
      { id: '@11', name: 'preview', pane: '%15', command: '$ _' },
    ],
  },
]
const stages = ['Action', 'Session', 'Window', 'Run']
const stage = ref(0)
const session = ref(null)
const destination = ref(null)
const controls = ref(null)
const replay = ref(null)

const title = computed(() => [
  'Choose an action.',
  'Choose a destination session.',
  `Choose a window in ${session.value?.name}.`,
  'Pane moved.',
][stage.value])

const choices = computed(() => {
  if (stage.value === 0) {
    return [{ id: 'move', name: 'Move pane to another window…', description: 'A recipe you define in actions.yaml' }]
  }

  if (stage.value === 1) return sessions

  return session.value.windows.map((window) => ({ ...window, description: `${session.value.name} / ${window.name}` }))
})

const command = computed(() => {
  if (!destination.value) return ''

  const target = `${session.value.id}:${destination.value.id}.${destination.value.pane}`
  return `tmux -S /demo/tmux.sock join-pane -d -s %7 -t '${target}'`
})

async function focusControls() {
  await nextTick()
  const target = stage.value === 3 ? replay.value : controls.value.querySelector('button')
  target.focus({ preventScroll: true })
}

async function choose(choice) {
  if (stage.value === 1) session.value = choice
  if (stage.value === 2) destination.value = choice

  stage.value += 1
  await focusControls()
}

async function back() {
  stage.value -= 1
  await focusControls()
}

async function restart() {
  stage.value = 0
  session.value = null
  destination.value = null
  await focusControls()
}

function onKey(event) {
  if (event.altKey || event.ctrlKey || event.metaKey || event.isComposing) return

  if (event.key === 'Escape' && stage.value > 0 && stage.value < 3) {
    event.preventDefault()
    event.stopPropagation()
    back()
    return
  }

  if (!['ArrowDown', 'ArrowUp'].includes(event.key) || stage.value === 3) return

  const buttons = [...controls.value.querySelectorAll('button')]
  const index = buttons.indexOf(event.target)
  if (index === -1) return

  event.preventDefault()
  event.stopPropagation()
  const direction = event.key === 'ArrowDown' ? 1 : -1
  buttons[Math.max(0, Math.min(buttons.length - 1, index + direction))].focus()
}
</script>

<template>
  <div class="action-demo" role="group" aria-label="Try a user-defined action" aria-describedby="hk-action-help" @keydown="onKey">
    <div class="hk-panel">
      <div class="hk-bar">
        <span>hetki action --pane-id '%7'</span>
        <span class="interactive"><span aria-hidden="true">●</span> interactive demo</span>
      </div>

      <ol class="progress" aria-label="Action steps">
        <li v-for="(label, index) in stages" :key="label" :class="{ current: stage === index && stage < 3, complete: stage > index || stage === 3 }" :aria-current="stage === index ? 'step' : undefined">
          <span class="step-number"><PhCheck v-if="stage > index || stage === 3" :size="12" weight="bold" aria-hidden="true" /><template v-else>{{ index + 1 }}</template></span>
          {{ label }}
        </li>
      </ol>

      <div class="subject">
        <span class="subject-label">{{ stage === 3 ? 'Moved pane' : 'Your pane' }}</span>
        <span class="subject-command">npm run dev <span class="muted">%7</span></span>
        <span v-if="stage < 3" class="subject-origin">scratch / dev</span>
        <span v-else class="subject-origin">{{ session.name }} / {{ destination.name }}</span>
      </div>

      <div class="body">
        <div class="heading">
          <h3 aria-live="polite" aria-atomic="true">{{ title }}</h3>
          <p v-if="stage === 0">Move the pane running your development server to another window.</p>
          <p v-else-if="stage === 1">The recipe lists sessions from tmux.</p>
          <p v-else-if="stage === 2">The previous step selected {{ session.name }} ({{ session.id }}). That ID filters this window list.</p>
          <p v-else>The final choice runs the command. The action picker closes.</p>
        </div>

        <div v-if="stage < 3" ref="controls" :key="stage" class="choices">
          <button v-for="choice in choices" :key="choice.id" type="button" class="choice" @click="choose(choice)">
            <span class="choice-marker" aria-hidden="true">›</span>
            <span class="choice-copy"><span>{{ choice.name }}</span><small>{{ choice.description }}</small></span>
            <PhArrowRight :size="16" aria-hidden="true" />
          </button>
        </div>

        <div v-else class="result">
          <div class="destination-bar"><span>{{ session.name }} / {{ destination.name }}</span><span class="success">pane joined <PhCheck :size="13" aria-hidden="true" /></span></div>
          <div class="panes">
            <div class="pane existing"><span>{{ destination.command }}</span><div class="code-lines" aria-hidden="true"><i></i><i></i><i></i><i></i></div></div>
            <div class="pane moved"><span class="pane-label">your pane · %7</span><span>$ npm run dev</span><span class="server-ready">➜ server ready</span></div>
          </div>
        </div>

        <div class="controls">
          <button v-if="stage > 0 && stage < 3" type="button" class="back" @click="back">← Back</button>
          <span v-else-if="stage === 0" class="hint">Choose the action to begin <PhArrowRight :size="14" aria-hidden="true" /></span>
          <button v-else ref="replay" type="button" class="replay" @click="restart"><PhArrowCounterClockwise :size="15" aria-hidden="true" /> Try another destination</button>
          <span v-if="stage === 2" class="hint">Next choice moves the pane</span>
        </div>
      </div>

      <div class="recipe">
        <template v-if="stage < 3"><span class="recipe-label">One YAML recipe</span><span><b>select</b> session <span class="muted">→</span> <b>select</b> window <span class="muted">→</span> <b>run</b></span></template>
        <details v-else><summary>See the command with your selections</summary><code>{{ command }}</code></details>
      </div>
    </div>
    <p id="hk-action-help" class="caption">Browser illustration with sample sessions. Click or use Tab and Enter. No commands run here.</p>
  </div>
</template>

<style scoped>
.action-demo {
  min-width: 0;
}

.hk-bar {
  flex-wrap: wrap;
  gap: 4px 12px;
  white-space: normal;
}

.interactive {
  color: var(--hk-term-command);
}

.interactive span {
  margin-right: 5px;
  font-size: 9px;
}

.progress {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  margin: 0;
  padding: 20px;
  list-style: none;
}

.progress li {
  display: flex;
  align-items: center;
  gap: 7px;
  padding-bottom: 12px;
  border-bottom: 2px solid var(--hk-term-border);
  color: var(--hk-term-muted);
  font-size: 12px;
}

.progress .current {
  border-color: var(--hk-term-active);
  color: var(--hk-term-text);
}

.progress .complete {
  border-color: var(--hk-term-command);
  color: var(--hk-term-command);
}

.step-number {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: 1px solid var(--hk-term-border);
  border-radius: 50%;
  font-size: 10px;
}

.current .step-number {
  background: var(--hk-term-selection);
  border-color: var(--hk-term-active);
  color: var(--hk-term-active);
}

.subject {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 12px;
  margin: 0 20px;
  padding: 12px 14px;
  border: 1px solid var(--hk-term-border);
  border-left: 2px solid var(--hk-term-command);
  border-radius: 4px;
  background: var(--hk-term-bar);
  font-size: 12px;
}

.subject-label, .subject-origin, .muted {
  color: var(--hk-term-muted);
}

.subject-command {
  color: var(--hk-term-command);
}

.subject-origin {
  margin-left: auto;
}

.body {
  display: flex;
  flex-direction: column;
  min-height: 290px;
  padding: 24px 20px 12px;
}

h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
  line-height: 1.5;
  letter-spacing: -0.02em;
}

.heading p {
  margin: 6px 0 20px;
  color: var(--hk-term-secondary);
  font-size: 12px;
  line-height: 1.7;
}

.choices {
  display: grid;
  gap: 8px;
}

button {
  font: inherit;
  color: inherit;
}

.choice {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 64px;
  padding: 12px;
  border: 1px solid var(--hk-term-border);
  border-radius: 6px;
  text-align: left;
}

.choice:hover, .choice:focus-visible {
  background: var(--hk-term-selection);
  border-color: var(--hk-term-active);
}

.choice-marker, .choice > svg {
  flex-shrink: 0;
  color: var(--hk-term-active);
}

.choice-copy {
  flex: 1;
}

.choice small {
  display: block;
  margin-top: 4px;
  color: var(--hk-term-secondary);
  font-size: 11px;
  line-height: 1.6;
}

.controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0 12px;
  margin-top: auto;
  padding-top: 12px;
  font-size: 11px;
}

.controls button, .hint {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 44px;
}

.hint {
  color: var(--hk-term-secondary);
}

.replay {
  color: var(--hk-term-command);
}

.recipe {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  padding: 14px 20px;
  border-top: 1px solid var(--hk-term-border);
  background: var(--hk-term-bar);
  color: var(--hk-term-secondary);
  font-size: 11px;
  line-height: 1.8;
}

.recipe-label {
  color: var(--hk-term-text);
}

.recipe b {
  color: var(--hk-term-command);
  font-weight: 400;
}

details {
  width: 100%;
  min-width: 0;
}

summary {
  padding-block: 6px;
  cursor: pointer;
}

summary:focus-visible {
  outline: 1px solid var(--hk-term-text);
  outline-offset: 2px;
}

.recipe code {
  display: block;
  padding-top: 10px;
  color: var(--hk-term-command);
  overflow-wrap: anywhere;
}

.result {
  overflow: hidden;
  border: 1px solid var(--hk-term-border);
  border-radius: 6px;
}

.destination-bar {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 4px 12px;
  padding: 8px 12px;
  background: var(--hk-tmux-bg);
  color: var(--hk-tmux-fg);
  font-size: 11px;
}

.success {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.panes {
  display: grid;
  grid-template-rows: 1fr 1fr;
}

.pane {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
  min-height: 64px;
  padding: 12px;
  font-size: 11px;
}

.existing {
  color: var(--hk-term-secondary);
}

.code-lines {
  display: grid;
  flex: 1;
  gap: 5px;
  max-width: 180px;
}

.code-lines i {
  height: 3px;
  border-radius: 2px;
  background: var(--hk-term-border);
}

.code-lines i:nth-child(even) {
  width: 65%;
}

.moved {
  border-top: 1px solid var(--hk-term-command);
  background: var(--hk-term-selection);
  animation: pane-arrives 0.35s ease-out;
}

.pane-label {
  width: 100%;
  color: var(--hk-term-command);
  font-size: 10px;
}

.server-ready {
  color: var(--hk-tmux-fg);
}

.caption {
  margin: 12px 4px 0;
  color: var(--vp-c-text-3);
  font-size: 12px;
  line-height: 1.6;
}

@keyframes pane-arrives {
  from {
    opacity: 0;
    transform: translateY(8px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .moved {
    animation: none;
  }
}

@media (max-width: 480px) {
  .progress {
    padding: 16px 12px;
    gap: 6px;
  }

  .progress li {
    gap: 5px;
    font-size: 10px;
  }

  .subject {
    margin-inline: 12px;
  }

  .subject-origin {
    width: 100%;
    margin-left: 0;
  }

  .body, .recipe {
    padding-inline: 12px;
  }

  .hk-bar {
    gap: 12px;
    padding-inline: 12px;
    font-size: 10px;
  }
}
</style>
