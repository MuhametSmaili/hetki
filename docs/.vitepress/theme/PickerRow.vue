<script setup>
import { statusLabels } from './status.js'

defineProps({
  item: { type: Object, required: true },
  glyph: { type: String, default: '' },
  selected: Boolean,
  current: Boolean,
})
</script>

<template>
  <div class="pane-row" :class="{ selected, current }">
    <span class="identity">{{ item.session }} <span class="separator">›</span> {{ item.window }}</span>
    <span class="path">{{ item.path }}</span>
    <span class="command">{{ item.command }}</span>
    <span class="status" :class="item.status">
      <span aria-hidden="true">{{ glyph }}</span>
      <span v-if="item.status" class="hk-sr-only">{{ statusLabels[item.status] }}</span>
    </span>
  </div>
</template>

<style scoped>
.pane-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto 2ch;
  column-gap: 1ch;
  padding: 4px 12px;
  border-left: 2px solid transparent;
  line-height: 1.65;
  white-space: nowrap;
}
.identity, .path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.identity {
  grid-column: 1;
  grid-row: 1;
}

.command {
  grid-column: 2;
  grid-row: 1;
  color: var(--hk-term-command);
}

.status {
  grid-column: 3;
  grid-row: 1;
  text-align: right;
}

.path {
  grid-column: 1 / -1;
  grid-row: 2;
  color: var(--hk-term-secondary);
  font-style: italic;
}
.separator {
  color: var(--hk-term-muted);
}

.selected {
  background: var(--hk-term-selection);
  border-left-color: var(--hk-term-active);
}

.current .identity, .current .command {
  color: var(--hk-term-active);
  font-weight: 600;
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

.unknown {
  color: var(--hk-term-muted);
}

/* Match the real picker's 56-column switch from stacked rows to columns. */
@container picker (min-width: 56ch) {
  .pane-row {
    grid-template-columns: minmax(0, 22ch) minmax(0, 1fr) auto 2ch;
  }

  .path {
    grid-column: 2;
    grid-row: 1;
  }

  .command {
    grid-column: 3;
  }

  .status {
    grid-column: 4;
  }
}
</style>
