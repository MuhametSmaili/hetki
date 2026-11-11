<script setup>
import { PhCheck } from '@phosphor-icons/vue'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import {
  createActionFlowDemo, joinTarget, windowLabel,
  MENU, SESSIONS, WINDOWS, SUBJECT,
} from './action-flow.js'
import ChoiceRow from './ChoiceRow.vue'
import PickerHeader from './PickerHeader.vue'

const input = ref(null)
const replayButton = ref(null)
const flow = createActionFlowDemo()
const { query, cursor, visible, selected, screen, busy, error, closed, picked, message, emptyText, command } = flow

// The Go picker sizes the label column to the widest label on the screen.
const labelWidth = computed(() => `${Math.max(0, ...screen.value.items.map((item) => item.label.length))}ch`)

// A note under the picker: the YAML behind this screen and what it shows.
const guide = computed(() => {
  const key = screen.value.key
  if (key === MENU) {
    return {
      yaml: 'scope: [pane]',
      note: `The menu lists each recipe whose scope includes the subject, pane ${SUBJECT}. Rows show the recipe's label.`,
    }
  }
  if (key === SESSIONS) {
    return {
      yaml: 'select: session    builtin: tmux.sessions',
      note: 'Every session on the server, labeled by name. Typing filters labels only.',
    }
  }
  if (key === WINDOWS) {
    const session = picked.session
    const tip = session.name === 'scratch' ? ` scratch / 0 dev holds pane ${SUBJECT} itself; try it to see a failed run.` : ''
    return {
      yaml: `session: "\${steps.session.value.session_id}"  →  "${session.id}"`,
      note: `Your ${session.name} choice filters this list. Rows show session / index name, then the active pane's directory.${tip}`,
    }
  }
  return {
    yaml: `run: tmux … join-pane -d -s ${SUBJECT} -t ${joinTarget(picked)}`,
    note: error.value
      ? 'tmux rejected the target: it is your own pane. Enter retries the command; Esc goes back to the windows.'
      : 'Enter runs the command again. Refresh reloads this screen without running it.',
  }
})

// Move focus with the screen: the replay button after closing, the filter after reopening.
watch(closed, async (outcome) => {
  await nextTick()
  const target = outcome ? replayButton.value : input.value
  target?.focus({ preventScroll: true })
})

function onKey(event) {
  if (!flow.handleKey(event)) return

  event.preventDefault()
  event.stopPropagation()
}

function choose(index) {
  if (busy.value) return

  cursor.value = index
  flow.confirm(visible.value[index])
}

onBeforeUnmount(flow.dispose)
</script>

<template>
  <div class="action-demo">
    <div class="hk-panel" role="group" aria-label="Browser demo of hetki action" aria-describedby="hk-action-help">
      <div class="hk-bar">
        <span><span class="dim">$ </span>hetki action --pane '{{ SUBJECT }}'</span>
        <span class="dim">pane {{ SUBJECT }} · scratch / 0 dev</span>
      </div>

      <div class="screen">
        <div v-if="!closed" class="picker">
          <PickerHeader as="label" class="header">
            <input
              ref="input"
              v-model="query"
              role="combobox"
              aria-label="Filter choices"
              aria-controls="hk-action-list"
              aria-expanded="true"
              aria-autocomplete="list"
              :aria-activedescendant="selected ? `hk-action-${cursor}` : undefined"
              :readonly="busy"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              @keydown="onKey"
            />
            <template #meta>
              <span class="meta" :title="message" aria-live="polite">{{ message }}</span>
            </template>
          </PickerHeader>

          <ul
            id="hk-action-list"
            class="choices"
            role="listbox"
            :aria-label="screen.title"
            :aria-busy="busy"
            :style="{ '--hk-choice-label': labelWidth }"
          >
            <li
              v-for="(item, index) in visible"
              :id="`hk-action-${index}`"
              :key="item.id"
              role="option"
              :aria-selected="index === cursor"
              @mousedown.prevent
              @click="choose(index)"
            >
              <ChoiceRow :item="item" :selected="index === cursor" />
            </li>
            <li v-if="!visible.length" class="empty" role="presentation">{{ emptyText }}</li>
          </ul>
        </div>

        <div v-else class="closed">
          <template v-if="closed === 'moved'">
            <p class="closed-note" role="status">
              The picker closed. Pane {{ SUBJECT }} now runs in {{ windowLabel(picked.session, picked.window) }}.
            </p>
            <div class="result">
              <div class="destination-bar">
                <span>{{ windowLabel(picked.session, picked.window) }} <span class="dim-tmux">· in the background</span></span>
                <span class="success">pane joined <PhCheck :size="13" aria-hidden="true" /></span>
              </div>
              <div class="panes">
                <div class="pane existing"><span class="pane-label">{{ picked.window.pane }}</span><span>{{ picked.window.command }}</span></div>
                <div class="pane moved"><span class="pane-label">your pane · {{ SUBJECT }}</span><span>$ npm run dev</span><span class="ready">➜ server ready</span></div>
              </div>
            </div>
            <details>
              <summary>The command Hetki ran</summary>
              <code>{{ command }}</code>
            </details>
          </template>
          <p v-else class="closed-note" role="status">The picker closed. Nothing ran.</p>
          <button ref="replayButton" type="button" class="replay" @click="flow.restart()">Open the picker again</button>
        </div>
      </div>

      <div v-if="!closed" class="guide">
        <code>{{ guide.yaml }}</code>
        <p>{{ guide.note }}</p>
        <button type="button" class="back" :disabled="busy" @click="flow.back()">
          Esc · {{ screen.key === MENU ? 'close the picker' : 'go back' }}
        </button>
      </div>
    </div>
    <p id="hk-action-help" class="caption">
      Browser demo with sample sessions. Type to filter labels, ↑↓ to move, Enter or click to select,
      Esc to go back, Ctrl+R to reload, Ctrl+C to close. Nothing runs here.
    </p>
  </div>
</template>

<style scoped>
.action-demo {
  min-width: 0;
}

.dim {
  color: var(--hk-term-muted);
}

.screen {
  padding: 14px 16px;
  font-size: 12px;
  line-height: 20px;
}

/* ui.border defaults to rounded; the picker opens inline below the prompt. */
.picker {
  padding: 4px 12px 8px;
  border: 1px solid var(--hk-term-border);
  border-radius: 8px;
}

.picker:has(input:focus-visible) {
  border-color: var(--hk-term-active);
}

/* The header rule sits under the prompt and title. */
.picker .header {
  gap: 1ch;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--hk-term-border);
  white-space: pre;
}

input {
  flex: 1;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--hk-term-text);
  font: inherit;
  caret-color: var(--hk-term-active);
}

input:focus {
  outline: none;
}

/* The real header cuts this text to half the list width. */
.meta {
  max-width: 50%;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--hk-term-text);
}

.choices {
  min-height: calc(3 * (1.65em + 8px) + 4px);
  margin: 0;
  padding: 4px 0 0;
  list-style: none;
}

li[role='option'] {
  cursor: pointer;
}

.empty {
  padding: 4px 12px;
  color: var(--hk-term-muted);
}

.guide {
  display: grid;
  gap: 4px;
  padding: 12px 16px 8px;
  border-top: 1px solid var(--hk-term-border);
  background: var(--hk-term-bar);
  font-size: 12px;
  line-height: 18px;
}

.guide code {
  color: var(--hk-term-command);
  overflow-wrap: anywhere;
}

.guide p {
  margin: 0;
  color: var(--hk-term-secondary);
  min-height: 36px;
}

button {
  color: inherit;
  font: inherit;
}

.back {
  justify-self: start;
  min-height: 40px;
  color: var(--hk-term-secondary);
}

.back:hover:not(:disabled) {
  color: var(--hk-term-text);
}

.back:disabled {
  opacity: 0.5;
}

.closed {
  display: grid;
  gap: 12px;
  min-height: 160px;
}

.closed-note {
  margin: 0;
  color: var(--hk-term-active);
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
  padding: 4px 10px;
  background: var(--hk-tmux-bg);
  color: var(--hk-tmux-fg);
}

.dim-tmux {
  opacity: 0.7;
}

.success {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

/* join-pane splits the destination's active pane top and bottom by default. */
.panes {
  display: grid;
  grid-template-rows: 1fr 1fr;
}

.pane {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px 16px;
  min-height: 56px;
  padding: 8px 10px;
}

.pane-label {
  width: 100%;
  color: var(--hk-term-muted);
  font-size: 11px;
}

.existing {
  color: var(--hk-term-secondary);
}

.moved {
  border-top: 1px solid var(--hk-term-command);
  background: var(--hk-term-selection);
  animation: pane-arrives 0.35s ease-out;
}

.moved .pane-label {
  color: var(--hk-term-command);
}

.ready {
  color: var(--hk-tmux-fg);
}

details {
  min-width: 0;
  color: var(--hk-term-secondary);
}

summary {
  padding-block: 6px;
  cursor: pointer;
}

summary:focus-visible {
  outline: 1px solid var(--hk-term-text);
  outline-offset: 2px;
}

details code {
  display: block;
  padding-top: 6px;
  color: var(--hk-term-command);
  overflow-wrap: anywhere;
}

.replay {
  justify-self: start;
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--hk-term-border);
  border-radius: 6px;
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
    transform: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .moved {
    animation: none;
  }
}

@media (max-width: 480px) {
  .screen, .guide {
    padding-inline: 12px;
  }

  .picker {
    padding-inline: 8px;
  }

  /* Keep iOS from zooming into the filter. */
  input {
    font-size: 16px;
  }
}
</style>
