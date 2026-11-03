<script setup>
import { ref } from 'vue'
import { useStatusGlyph } from './status.js'
import { demoPanes } from './demo-data.js'
import PanePreview from './PanePreview.vue'
import PickerRow from './PickerRow.vue'

const root = ref(null)
const glyph = useStatusGlyph(root)

// Each line is [indent, key, value, the part of the picker it controls].
// A null value is filled in with the accent picked from the swatches.
const config = [
  ['', 'ui:', '', null],
  ['  ', 'mode:', ' normal', 'hint'],
  ['  ', 'height:', ' "40%"', 'height'],
  ['  ', 'preview:', ' true', 'preview'],
  ['  ', 'preview_width:', ' 55', 'preview'],
  ['', 'keymap:', '', null],
  ['  ', 'normal:', '', 'hint'],
  ['    ', 'filter:', ' [f]', 'hint'],
  ['', 'theme:', '', null],
  ['  ', 'active:', null, 'active'],
]

const accents = [
  { name: 'Blue, the default', value: '#58a6ff' },
  { name: 'Cyan', value: '#39c5cf' },
  { name: 'Violet', value: '#d2a8ff' },
  { name: 'Orange', value: '#f0883e' },
]
const accent = ref(accents[0].value)

const notes = {
  hint: 'Normal mode shows the hint with the first key bound to filter.',
  height: 'Anything under 100% opens inline, right under your prompt.',
  preview: 'When space permits, the preview uses 55% of the content width.',
  active: 'The prompt, the scope, and the current tmux item.',
}

const rows = demoPanes.slice(0, 4)
const selected = rows[1]

const highlightedSetting = ref(null)

function chooseAccent(value) {
  accent.value = value
  highlightedSetting.value = 'active'
}
</script>

<template>
  <div ref="root" class="hk-panel config-demo" @mouseleave="highlightedSetting = null">
    <div class="file">
      <div class="hk-bar">~/.config/hetki/config.yaml</div>
      <div class="code">
        <template v-for="([indent, key, value, part], i) in config" :key="i">
          <button
            v-if="part"
            type="button"
            class="line"
            :class="{ lit: highlightedSetting === part }"
            :aria-describedby="highlightedSetting === part ? 'hk-config-note' : undefined"
            @mouseenter="highlightedSetting = part"
            @click="highlightedSetting = part"
            @focus="highlightedSetting = part"
            @blur="highlightedSetting = null"
          >{{ indent }}<span class="key">{{ key }}</span>{{ value ?? ` "${accent}"` }}</button>
          <div v-else class="line">{{ indent }}<span class="key">{{ key }}</span>{{ value }}</div>
        </template>
      </div>
      <div class="swatches" role="group" aria-label="Try a theme.active color">
        <span class="dim">try theme.active</span>
        <button
          v-for="color in accents"
          :key="color.value"
          type="button"
          class="swatch"
          :style="{ '--swatch': color.value }"
          :aria-label="color.name"
          :aria-pressed="accent === color.value"
          @click="chooseAccent(color.value)"
        />
      </div>
      <p id="hk-config-note" class="note" aria-live="polite">
        {{ highlightedSetting ? notes[highlightedSetting] : 'Point at a setting to see what it changes.' }}
      </p>
    </div>

    <div class="screen" :style="{ '--accent': accent, '--hk-term-active': accent }" aria-hidden="true">
      <div class="shell">
        <div><span class="prompt">~/Projects/hetki</span> $ git pull</div>
        <div class="dim">Already up to date.</div>
        <div><span class="prompt">~/Projects/hetki</span> $ hetki</div>
      </div>
      <div class="picker" :class="{ lit: highlightedSetting === 'height' }">
        <div class="header">
          <span><span class="accent" :class="{ mark: highlightedSetting === 'active' }">&gt;</span> <span class="hint" :class="{ mark: highlightedSetting === 'hint' }">press f to filter</span></span>
          <span><span class="accent scope" :class="{ mark: highlightedSetting === 'active' }">All</span>  <span class="dim">Tracked</span>  <span class="dim">4</span></span>
        </div>
        <div class="body">
          <ul class="list hk-picker-list">
            <li v-for="row in rows" :key="row.id" :class="{ mark: row.id === rows[0].id && highlightedSetting === 'active' }">
              <PickerRow :item="row" :glyph="glyph(row.status)" :current="row.id === rows[0].id" :selected="row.id === selected.id" />
            </li>
          </ul>
          <PanePreview class="preview" :class="{ lit: highlightedSetting === 'preview' }" :item="selected" :working-glyph="glyph('working')" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.config-demo {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
}

.file {
  display: flex;
  flex-direction: column;
}

.code {
  padding: 12px 0;
}

.line {
  display: block;
  width: 100%;
  padding: 0 16px;
  border-left: 2px solid transparent;
  font: inherit;
  line-height: 26px;
  text-align: left;
  white-space: pre;
  color: var(--hk-term-text);
}

button.line {
  cursor: default;
}

button.line:hover,
.line.lit {
  border-left-color: var(--hk-term-active);
  background: rgba(88, 166, 255, 0.08);
}

/* The shared rule scales buttons on press; a full-width line shouldn't move. */
button.line:active {
  transform: none;
}

.key {
  color: var(--hk-err);
}

.swatches {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 4px 10px 4px 16px;
  border-top: 1px solid var(--hk-term-border);
  font-size: 12px;
}

.swatches .dim {
  margin-right: auto;
}

.swatch {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: 8px;
}

.swatch::after {
  content: '';
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--swatch);
  box-shadow: 0 0 0 2px var(--hk-term-bg), 0 0 0 3px transparent;
  transition: box-shadow 0.15s ease-out;
}

.swatch:hover::after {
  box-shadow: 0 0 0 2px var(--hk-term-bg), 0 0 0 3px var(--hk-term-border);
}

.swatch[aria-pressed='true']::after {
  box-shadow: 0 0 0 2px var(--hk-term-bg), 0 0 0 3px var(--swatch);
}

.note {
  margin-top: auto;
  padding: 12px 16px;
  border-top: 1px solid var(--hk-term-border);
  color: var(--hk-term-secondary);
  font-size: 12px;
  line-height: 18px;
  min-height: 61px;
}

.screen {
  container-type: inline-size;
  container-name: config-screen;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  padding: 14px 16px;
  border-top: 1px solid var(--hk-term-border);
  font-size: 12px;
  line-height: 20px;
}

.shell {
  white-space: nowrap;
  overflow: hidden;
}

.prompt {
  color: var(--hk-term-prompt);
}

/* ui.border defaults to rounded. */
.picker {
  padding: 4px 12px 8px;
  border: 1px solid var(--hk-term-border);
  border-radius: 8px;
  transition: border-color 0.15s ease-out, box-shadow 0.15s ease-out;
}

.header {
  display: flex;
  justify-content: space-between;
  gap: 2ch;
  white-space: pre;
}

.body {
  display: grid;
  grid-template-columns: minmax(0, 45fr) minmax(0, 55fr);
}

.list {
  margin: 0;
  padding: 0;
  list-style: none;
  min-width: 0;
  padding-right: 10px;
  border-right: 1px solid var(--hk-term-border);
}

.preview {
  min-width: 0;
  margin-left: 10px;
  padding: 0 6px;
  border-radius: 4px;
  white-space: nowrap;
  overflow: hidden;
  transition: box-shadow 0.15s ease-out;
}

.accent {
  color: var(--accent);
}

.scope {
  text-decoration: underline;
  text-underline-offset: 3px;
}

.hint {
  color: var(--hk-term-muted);
  font-style: italic;
}

.dim {
  color: var(--hk-term-muted);
}

.mark {
  border-radius: 3px;
  box-shadow: 0 0 0 2px rgba(88, 166, 255, 0.55);
  background: rgba(88, 166, 255, 0.12);
}

.picker.lit {
  border-color: var(--hk-term-active);
  box-shadow: 0 0 0 3px rgba(88, 166, 255, 0.18);
}

.preview.lit {
  box-shadow: 0 0 0 2px rgba(88, 166, 255, 0.55);
}

@container (min-width: 760px) {
  .config-demo {
    grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
  }

  .screen {
    justify-content: center;
    border-top: 0;
    border-left: 1px solid var(--hk-term-border);
  }
}

/* Keep pane identities readable in this small illustrated terminal. */
@container config-screen (max-width: 64ch) {
  .body {
    grid-template-columns: minmax(0, 1fr);
  }

  .list {
    padding-right: 0;
    border-right: 0;
  }

  .preview {
    display: none;
  }
}
</style>
