<script setup>
import { computed, ref } from 'vue'
import { demoPanes } from './demo-data.js'
import { statusLabels, useStatusGlyph } from './status.js'
import PickerHeader from './PickerHeader.vue'
import PickerRow from './PickerRow.vue'

const root = ref(null)
const glyph = useStatusGlyph(root)
const tracked = ref(true)
const spotlight = ref(null)
const visible = computed(() => demoPanes.filter((item) => !tracked.value || item.status))
const shown = (status) => demoPanes.some((item) => item.status === status)

function toggle(status) {
  spotlight.value = spotlight.value === status ? null : status
}
</script>

<template>
  <div ref="root" class="hk-panel status-demo">
    <PickerHeader dim class="bar">
      <template #meta>
        <div class="scopes" role="group" aria-label="Status example scope">
          <button type="button" :aria-pressed="!tracked" @click="tracked = false">All</button>
          <button type="button" :aria-pressed="tracked" @click="tracked = true">Tracked</button>
          <span class="muted" aria-live="polite">{{ visible.length }}</span>
        </div>
      </template>
    </PickerHeader>
    <ul class="hk-picker-list">
      <li v-for="item in visible" :key="item.id" :class="{ lit: spotlight && item.status === spotlight }">
        <PickerRow :item="item" :glyph="glyph(item.status)" />
      </li>
    </ul>
    <div class="legend" role="group" aria-label="Highlight rows by status">
      <component
        :is="shown(status) ? 'button' : 'span'"
        v-for="(label, status) in statusLabels"
        :key="status"
        :type="shown(status) ? 'button' : undefined"
        :aria-pressed="shown(status) ? spotlight === status : undefined"
        class="key"
        @click="shown(status) && toggle(status)"
      >
        <span class="glyph" :class="status" aria-hidden="true">{{ glyph(status) }}</span>
        {{ label }}
      </component>
    </div>
    <p class="caption">Sample apps with OSC 7501 reporting enabled. No status does not mean idle.</p>
  </div>
</template>

<style scoped>
.status-demo {
  container-type: inline-size;
  container-name: status;
}

.status-demo .bar {
  padding: 4px 16px;
  border-bottom: 1px solid var(--hk-term-border);
}

.scopes {
  display: flex;
  align-items: center;
  gap: 2ch;
}

.scopes button {
  min-height: 40px;
  color: var(--hk-term-muted);
  font: inherit;
}

.scopes button[aria-pressed='true'] {
  color: var(--hk-term-active);
  text-decoration: underline;
  text-underline-offset: 3px;
}

ul {
  min-height: calc(7 * (3.3em + 8px) + 8px);
  margin: 0;
  padding: 4px 0;
  list-style: none;
}

li {
  border-radius: 2px;
}

li.lit {
  outline: 1px solid var(--hk-term-active);
  outline-offset: -1px;
  background: var(--hk-term-selection);
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 8px;
  padding: 8px 10px;
  border-top: 1px solid var(--hk-term-border);
  color: var(--hk-term-secondary);
  font-size: 12px;
}

.key {
  display: inline-flex;
  align-items: center;
  gap: 1ch;
  min-height: 40px;
  padding: 0 6px;
  border-radius: 6px;
  color: inherit;
  font: inherit;
}

button.key[aria-pressed='true'] {
  background: var(--hk-term-bar);
  color: var(--hk-term-text);
}

.glyph {
  width: 1.5ch;
  text-align: center;
}

.caption {
  margin: 0;
  padding: 4px 16px 12px;
  color: var(--hk-term-secondary);
  font-size: 12px;
  line-height: 1.6;
}

.muted, .unknown {
  color: var(--hk-term-muted);
}

.working {
  color: var(--hk-term-active);
}

.blocked {
  color: var(--hk-warn);
}

.idle, .success {
  color: var(--hk-ok);
}

.error {
  color: var(--hk-err);
}

@container status (min-width: 56ch) {
  ul {
    min-height: calc(7 * (1.65em + 8px) + 8px);
  }
}
</style>
