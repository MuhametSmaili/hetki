<script setup>
defineProps({
  item: { type: Object, default: null },
  workingGlyph: { type: String, default: '⠋' },
})
</script>

<template>
  <div class="pane-preview">
    <template v-if="item">
      <div v-for="(line, i) in item.preview" :key="i" class="line">
        <template v-for="([text, tone], j) in line" :key="j">
          <span v-if="text === null" :class="tone" aria-hidden="true">{{ workingGlyph }}</span>
          <span v-else :class="tone">{{ text }}</span>
        </template>
      </div>
    </template>
    <span v-else class="dim">No pane selected.</span>
  </div>
</template>

<style scoped>
.pane-preview {
  min-width: 0;
  padding: 12px 18px;
  overflow: hidden;
  white-space: pre;
  line-height: 1.8;
}
.line {
  min-height: 1.8em;
}

.dim {
  color: var(--hk-term-muted);
}

.active {
  color: var(--hk-term-active);
}

.ok {
  color: var(--hk-ok);
}

.warn {
  color: var(--hk-warn);
}

.err, .key {
  color: var(--hk-err);
}

</style>
