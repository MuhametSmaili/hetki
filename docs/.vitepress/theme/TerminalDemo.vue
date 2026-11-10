<script setup>
import { computed, nextTick, ref } from 'vue'
import { demoPanes } from './demo-data.js'
import { createPickerDemo } from './picker.js'
import { useStatusGlyph } from './status.js'
import PanePreview from './PanePreview.vue'
import PickerHeader from './PickerHeader.vue'
import PickerRow from './PickerRow.vue'

const root = ref(null)
const input = ref(null)
const exitTarget = ref(null)
const replayButton = ref(null)
const glyph = useStatusGlyph(root)
const {
  query,
  mode,
  tracked,
  cursor,
  visible,
  selected,
  activeId,
  opened,
  preview,
  replay,
  handleKey,
} = createPickerDemo(demoPanes)
const current = computed(() => demoPanes.find((item) => item.id === activeId.value))

function leave() {
  exitTarget.value.focus()
}

async function onKey(event) {
  if (event.ctrlKey && event.key === 'c') {
    event.preventDefault()
    event.stopPropagation()
    leave()
    return
  }

  if (!handleKey(event)) return

  event.preventDefault()
  event.stopPropagation()

  if (opened.value) {
    await nextTick()
    replayButton.value.focus({ preventScroll: true })
  }
}

function selectPane(index) {
  cursor.value = index
  input.value.focus({ preventScroll: true })
}

async function restart() {
  replay()
  await nextTick()
  input.value.focus({ preventScroll: true })
}
</script>

<template>
  <div ref="root" class="terminal-demo">
    <div class="window" role="group" aria-label="Browser demonstration of the Hetki picker" aria-describedby="hk-demo-help">
      <div class="titlebar">
        <span>hetki{{ preview ? ' --preview' : '' }}</span>
        <button v-if="!opened" type="button" @click="leave">Leave demo</button>
      </div>
      <template v-if="!opened">
        <PickerHeader as="label" class="bar">
          <input
            ref="input"
            v-model="query"
            role="combobox"
            aria-label="Filter panes"
            aria-controls="hetki-demo-list"
            aria-expanded="true"
            aria-autocomplete="list"
            :aria-activedescendant="selected ? `hk-demo-${selected.id}` : undefined"
            :readonly="mode === 'normal'"
            :placeholder="mode === 'normal' ? 'press / to filter' : ''"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            @keydown="onKey"
          />
          <template #meta>
            <div class="scopes" role="group" aria-label="Demo scope">
              <button type="button" :aria-pressed="!tracked" @click="tracked = false">All</button>
              <button type="button" :aria-pressed="tracked" @click="tracked = true">Tracked</button>
              <span class="muted" aria-live="polite">{{ visible.length }}</span>
            </div>
          </template>
        </PickerHeader>
        <div class="body" :class="{ 'with-preview': preview }">
          <ul id="hetki-demo-list" class="hk-picker-list" role="listbox" aria-label="Sample tmux panes">
            <li
              v-for="(item, index) in visible"
              :id="`hk-demo-${item.id}`"
              :key="item.id"
              role="option"
              :aria-selected="index === cursor"
              @click="selectPane(index)"
            >
              <PickerRow :item="item" :glyph="glyph(item.status)" :selected="index === cursor" :current="item.id === activeId" />
            </li>
            <li v-if="!visible.length" class="empty muted" role="presentation">No matches</li>
          </ul>
          <PanePreview v-if="preview" class="preview" :item="selected" :working-glyph="glyph('working')" aria-label="Sample selected pane contents" />
        </div>
        <div class="hints muted">
          <span>{{ mode === 'filter' ? 'Filter' : 'Normal' }} mode</span>
          <span>↑↓ move · Enter open · {{ mode === 'filter' ? 'Esc normal mode' : '/ filter' }} · Tab scope · Alt+P preview</span>
        </div>
      </template>
      <div v-else class="destination">
        <p class="opened" role="status">Switched to {{ opened.session }} › {{ opened.window }}. The picker closed.</p>
        <PanePreview :item="opened" :working-glyph="glyph('working')" />
        <button ref="replayButton" type="button" class="replay" @click="restart">Open the picker again</button>
      </div>
      <div class="tmux" aria-hidden="true">[{{ current.session }}] 0:{{ current.window }}*</div>
    </div>
    <p id="hk-demo-help" ref="exitTarget" class="demo-help" tabindex="-1">
      Browser demo with fictional panes and simplified search ranking. Nothing runs here.
      Preview appears when space permits. Shift+Tab reaches Leave demo. Ctrl+C leaves the demo.
    </p>
  </div>
</template>

<style scoped>
.window {
  display: flex;
  flex-direction: column;
  min-height: 530px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 12px;
  background: var(--hk-term-bg);
  box-shadow: var(--hk-window-shadow);
  color: var(--hk-term-text);
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  line-height: 1.8;
  text-align: left;
  font-variant-numeric: tabular-nums;
}
.titlebar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 14px;
  border-bottom: 1px solid var(--hk-term-border);
  background: var(--hk-term-bar);
  color: var(--hk-term-muted);
  font-size: 12px;
}
button {
  color: inherit;
  font: inherit;
  min-height: 40px;
}

button:focus-visible, .demo-help:focus-visible {
  outline: 2px solid var(--hk-term-active);
  outline-offset: 2px;
  border-radius: 3px;
}

.body {
  flex: 1;
  min-height: 350px;
}

.body.with-preview {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
}

.window .bar {
  gap: 12px;
  padding: 4px 14px;
  border-bottom: 1px solid var(--hk-term-border);
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
input::placeholder {
  color: var(--hk-term-muted);
  font-style: italic;
}

input:focus {
  outline: none;
}

.window:has(input:focus-visible) {
  border-color: var(--hk-term-active);
}

.scopes {
  display: flex;
  align-items: center;
  gap: 1.5ch;
}

.scopes button {
  color: var(--hk-term-muted);
}

.scopes button[aria-pressed='true'] {
  color: var(--hk-term-active);
  text-decoration: underline;
  text-underline-offset: 3px;
}

ul {
  margin: 0;
  padding: 4px 0;
  list-style: none;
}

li[role='option'] {
  cursor: pointer;
}

.empty {
  padding: 12px 14px;
}

.preview {
  border-left: 1px solid var(--hk-term-border);
}

.hints {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 2px 16px;
  padding: 8px 14px;
  border-top: 1px solid var(--hk-term-border);
  font-size: 12px;
}

.destination {
  flex: 1;
  min-height: 350px;
  padding: 16px 0;
}

.opened {
  padding: 0 18px;
  color: var(--hk-term-active);
}

.replay {
  margin: 16px 18px 0;
  padding: 0 12px;
  border: 1px solid var(--hk-term-border);
  border-radius: 6px;
}

.tmux {
  padding: 0 10px;
  background: var(--hk-tmux-bg);
  color: var(--hk-tmux-fg);
  font-size: 12px;
  line-height: 24px;
}

.muted {
  color: var(--hk-term-muted);
}

.demo-help {
  margin: 16px 0 0;
  color: var(--frame-muted);
  font-size: 13px;
  line-height: 1.6;
}

@media (max-width: 767px) {
  .body.with-preview {
    display: block;
  }

  .preview {
    display: none;
  }

  input {
    font-size: 16px;
  }

  .window .bar {
    gap: 8px;
  }

  .scopes {
    gap: 1ch;
  }
}
</style>
