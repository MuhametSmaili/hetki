<script setup>
// One row on an action choice screen (internal/tui/render_list.go, flat rows):
// the label, then an optional italic description in an aligned second column.
// The list sets --hk-choice-label to its widest label, as the Go picker does.
defineProps({
  item: { type: Object, required: true },
  selected: Boolean,
})
</script>

<template>
  <div class="choice-row" :class="{ selected }">
    <span class="label">{{ item.label }}</span>
    <span v-if="item.description" class="description">{{ item.description }}</span>
  </div>
</template>

<style scoped>
/* The label column keeps at least 40% of the row for the description.
   Without --hk-choice-label from the list, labels get that whole 60%. */
.choice-row {
  display: grid;
  grid-template-columns: minmax(0, min(var(--hk-choice-label, 60%), 60%)) minmax(0, 1fr);
  column-gap: 2ch;
  padding: 4px 12px;
  border-left: 2px solid transparent;
  line-height: 1.65;
  white-space: nowrap;
}

.label, .description {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.description {
  color: var(--hk-term-secondary);
  font-style: italic;
}

/* Matches PickerRow's selection so every demo highlights rows the same way. */
.selected {
  background: var(--hk-term-selection);
  border-left-color: var(--hk-term-active);
}
</style>
