<script setup>
// The picker's search bar (internal/tui/render_search.go): the prompt and query
// on the left, and the right side in the `meta` slot. The normal picker puts its
// scopes and count there; action screens put the title or status there.
defineProps({
  // Use "label" when the default slot holds the filter input, so the prompt focuses it.
  as: { type: String, default: 'span' },
  // A muted prompt, for static illustrations that are not accepting input.
  dim: Boolean,
})
</script>

<template>
  <div class="picker-header">
    <component :is="as" class="query">
      <span class="prompt" :class="{ dim }" aria-hidden="true">&gt;</span>
      <slot />
    </component>
    <slot name="meta" />
  </div>
</template>

<style scoped>
.picker-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 2ch;
  min-width: 0;
}

.query {
  display: flex;
  flex: 1;
  gap: 1ch;
  min-width: 8ch;
}

.prompt {
  color: var(--hk-term-active);
  font-weight: 600;
}

.prompt.dim {
  color: var(--hk-term-muted);
  font-weight: 400;
}
</style>
