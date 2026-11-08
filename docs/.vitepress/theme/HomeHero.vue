<script setup>
import { PhArrowRight } from '@phosphor-icons/vue'
import { withBase } from 'vitepress'
import InstallCommand from './InstallCommand.vue'
import TerminalDemo from './TerminalDemo.vue'
</script>

<template>
  <section class="hero">
    <div class="hk-frame">
      <div class="copy">
        <h1>find your place<br />in tmux<span class="cursor" aria-hidden="true"></span></h1>
        <div class="side">
          <p class="tagline">
            Search tmux panes, preview before switching, and save layouts as YAML workspaces.
            A keyboard-first tool for Linux and macOS.
          </p>
          <InstallCommand class="install" />
          <div class="links">
            <a class="guide" :href="withBase('/getting-started')">
              Get started
              <PhArrowRight :size="16" weight="bold" aria-hidden="true" />
            </a>
            <a class="more" :href="withBase('/installation')">Other installation methods</a>
          </div>
        </div>
      </div>
      <div class="stage">
        <TerminalDemo class="terminal" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.hero {
  padding: calc(var(--vp-nav-height) + 8px) var(--hk-page) 0;
  margin-top: calc(var(--vp-nav-height) * -1);
}

.copy {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 24px;
  padding: 48px var(--hk-pad) 40px;
  color: var(--frame-text);
}

h1 {
  font-family: var(--vp-font-family-mono);
  font-size: clamp(32px, 5.4vw, 64px);
  font-weight: 500;
  line-height: 1.1;
  letter-spacing: -0.04em;
}

.cursor {
  display: inline-block;
  width: 0.62em;
  height: 0.82em;
  margin-left: 0.12em;
  vertical-align: -0.1em;
  background: var(--frame-cursor);
  animation: blink 1.06s steps(1) infinite;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cursor {
    animation: none;
  }
}

.tagline {
  font-size: 17px;
  line-height: 1.6;
  color: var(--frame-muted);
  text-wrap: pretty;
}

.install {
  margin-top: 24px;
}

.links {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0 24px;
  margin-top: 12px;
}

.links a {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  font-size: 15px;
  font-weight: 500;
  text-decoration: none;
  transition: color 0.15s ease-out;
}

.links a:focus-visible {
  outline: 2px solid var(--frame-text);
  outline-offset: 2px;
  border-radius: 6px;
}

.guide {
  gap: 6px;
  color: var(--frame-text);
}

.guide svg {
  transition: transform 0.15s ease-out;
}

.guide:hover svg {
  transform: translateX(2px);
}

.more {
  color: var(--frame-faint);
}

.more:hover {
  color: var(--frame-text);
}

/* Keep the playable demo and its keyboard feedback inside the frame. */
.stage {
  padding: 0 var(--hk-pad) 32px;
}

@media (min-width: 768px) {
  .copy {
    gap: 32px;
    padding-top: 72px;
    padding-bottom: 56px;
  }

  .tagline,
  .install {
    max-width: 30rem;
  }

}

@media (min-width: 1100px) {
  .copy {
    grid-template-columns: minmax(0, 1fr) 28rem;
    align-items: end;
    gap: 64px;
    padding-top: 104px;
    padding-bottom: 64px;
  }

  /* Trim the line box so the headline's last baseline meets the links row. */
  h1 {
    margin-bottom: 6px;
  }

}
</style>
