<script setup>
import { PhCheck, PhCopy } from '@phosphor-icons/vue'
import { onBeforeUnmount, ref } from 'vue'

const command = 'curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash'
const copied = ref(false)
const busy = ref(false)
const error = ref(false)
let disposed = false
let timer

async function copy() {
  busy.value = true
  copied.value = false
  error.value = false
  clearTimeout(timer)

  try {
    await navigator.clipboard.writeText(command)
    if (disposed) return
    copied.value = true
    timer = setTimeout(() => (copied.value = false), 2000)
  } catch {
    if (!disposed) error.value = true
  } finally {
    if (!disposed) busy.value = false
  }
}

onBeforeUnmount(() => {
  disposed = true
  clearTimeout(timer)
})
</script>

<template>
  <div class="install">
    <code><span class="prompt" aria-hidden="true">$ </span>{{ command }}</code>
    <button type="button" :disabled="busy" :aria-label="copied ? 'Copied' : 'Copy install command'" @click="copy">
      <PhCheck v-if="copied" :size="18" weight="bold" aria-hidden="true" />
      <PhCopy v-else :size="18" aria-hidden="true" />
    </button>
    <span class="hk-sr-only" aria-live="polite">{{ copied ? 'Copied to clipboard' : '' }}</span>
    <span v-if="error" class="copy-error" role="alert">Copy failed. Select and copy the command.</span>
  </div>
</template>

<style scoped>
/* Colors come from the --install-* tokens on the surrounding .hk-frame (style.css). */
.install {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  border: 1px solid var(--install-border);
  border-radius: 10px;
  background: var(--install-bg);
}

code {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  padding: 12px 16px;
  font-family: var(--vp-font-family-mono);
  font-size: 14px;
  line-height: 20px;
  white-space: nowrap;
  color: var(--install-text);
  scrollbar-width: none;
  mask-image: linear-gradient(to right, #000 calc(100% - 32px), transparent);
}

code::-webkit-scrollbar {
  display: none;
}

.prompt {
  color: var(--install-muted);
  user-select: none;
}

button {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 48px;
  min-height: 44px;
  border-left: 1px solid var(--install-border);
  color: var(--install-accent);
  transition: background-color 0.15s ease-out, transform 0.15s ease-out;
}

.copy-error {
  flex-basis: 100%;
  padding: 8px 16px;
  border-top: 1px solid var(--install-border);
  color: var(--install-text);
  font-size: 13px;
  line-height: 1.5;
}

button:active {
  transform: scale(0.96);
}

button:hover {
  background: var(--install-hover);
}

button:focus-visible {
  outline: 2px solid var(--install-text);
  outline-offset: -2px;
  border-radius: 0 9px 9px 0;
}
</style>
