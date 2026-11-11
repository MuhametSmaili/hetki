<script setup>
import { PhArrowRight } from '@phosphor-icons/vue'
import { withBase } from 'vitepress'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import ActionDemo from './ActionDemo.vue'
import ConfigDemo from './ConfigDemo.vue'
import InstallCommand from './InstallCommand.vue'
import StatusDemo from './StatusDemo.vue'
import WorkspaceDemo from './WorkspaceDemo.vue'

const steps = [
  { id: 'save', command: 'hetki save work', what: 'Save the current session structure.' },
  { id: 'edit', command: 'Edit work.yaml', what: 'Add startup commands by hand.' },
  { id: 'dry', command: 'hetki start work --dry-run', what: 'Preview the changes without applying them.' },
  { id: 'start', command: 'hetki start work', what: 'Create what’s missing and attach.' },
]
const step = ref('save')
const stepList = ref(null)

function onStepKey(event) {
  const keys = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }
  const index = steps.findIndex((item) => item.id === step.value)
  let next = index

  if (event.key in keys) {
    next = (index + keys[event.key] + steps.length) % steps.length
  } else if (event.key === 'Home') {
    next = 0
  } else if (event.key === 'End') {
    next = steps.length - 1
  } else {
    return
  }

  event.preventDefault()
  step.value = steps[next].id
  stepList.value.querySelectorAll('button')[next].focus()
}

const settings = [
  { key: 'ui', what: 'Inline or fullscreen, flat list or tree, preview on or off.', link: '/configuration' },
  { key: 'keymap', what: 'Customize each mode’s supported keys. Ctrl+C always quits.', link: '/key-bindings' },
  { key: 'theme', what: '18 colors, from status icons to the selected row.', link: '/theme' },
]

// Avoid animating content already visible when the page loads.
const root = ref(null)
let observer

onMounted(() => {
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) return

  const targets = [...root.value.querySelectorAll('[data-reveal]')].filter(
    (el) => el.getBoundingClientRect().top > innerHeight,
  )
  targets.forEach((el) => el.classList.add('reveal'))
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        entry.target.classList.add('revealed')
        observer.unobserve(entry.target)
      }
    },
    { rootMargin: '0px 0px -12% 0px' },
  )
  targets.forEach((el) => observer.observe(el))
})

onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="root" class="sections">
    <div class="inner">
      <section class="feature split">
        <div class="text" data-reveal>
          <h2>See which app needs&nbsp;you.</h2>
          <p>
            See when a supported app needs input, works, or becomes idle.
            Apps that report completion can also show success or error.
            Press Tab to show panes with a current status in the Tracked scope.
          </p>
          <p class="note">
            Status needs enabled app reports or a screen profile you configure.
            Ordinary terminal output alone does not provide status.
          </p>
          <a class="more" :href="withBase('/status')">How status works <PhArrowRight :size="14" weight="bold" /></a>
        </div>
        <StatusDemo class="demo" data-reveal />
      </section>

      <section class="feature split mirrored">
        <div class="text" data-reveal>
          <h2>Bring your setup&nbsp;back.</h2>
          <p>
            Save a session as YAML, add the commands you want, and start the same windows and pane layouts next time.
          </p>
          <div ref="stepList" class="steps" role="tablist" aria-label="Workspace steps" aria-orientation="vertical" @keydown="onStepKey">
            <button
              v-for="(item, i) in steps"
              :id="`hk-workspace-step-${item.id}`"
              :key="item.id"
              type="button"
              role="tab"
              :aria-selected="step === item.id"
              :tabindex="step === item.id ? 0 : -1"
              aria-controls="hk-workspace-panel"
              @click="step = item.id"
            >
              <span class="n">{{ i + 1 }}</span>
              <code>{{ item.command }}</code>
              <span class="what">{{ item.what }}</span>
            </button>
          </div>
          <a class="more" :href="withBase('/workspaces')">Save a workspace <PhArrowRight :size="14" weight="bold" /></a>
        </div>
        <WorkspaceDemo class="demo" :step="step" data-reveal />
      </section>

      <section id="custom-actions" class="feature split" aria-labelledby="hk-actions-title">
        <div class="text" data-reveal>
          <p class="eyebrow">User-defined actions</p>
          <h2 id="hk-actions-title">Add your own actions.</h2>
          <p>
            Pick a session, then one of its windows, then move your pane there.
            Write the steps in <code>actions.yaml</code>: chain selections together,
            pass their data to later steps, and finish by running a command.
          </p>
          <div class="action-launch">
            <h3>Open from your terminal or a tmux binding</h3>
            <dl>
              <div><dt>Pane</dt><dd><code>hetki action --pane "$TMUX_PANE"</code></dd></div>
              <div><dt>Window</dt><dd><code>hetki action --window '@8'</code></dd></div>
              <div><dt>Session</dt><dd><code>hetki action --session '$3'</code></dd></div>
            </dl>
            <p class="note">Use your own window or session ID in place of the examples.</p>
          </div>
          <p class="action-entry">
            The same recipes work inside Hetki. Press <kbd>U</kbd> in normal mode,
            or <kbd>Ctrl+K</kbd> then <kbd>U</kbd> from either picker mode.
            Both shortcuts are <a :href="withBase('/key-bindings#normal-mode')">configurable</a>.
          </p>
          <a class="more" :href="withBase('/actions')">Write an action <PhArrowRight :size="14" weight="bold" aria-hidden="true" /></a>
        </div>
        <div class="demo" data-reveal>
          <ActionDemo />
          <div class="action-scripts">
            <h3>Use your own scripts for the choices</h3>
            <p>Use tmux’s built-in lists, fixed items, or a script in any language that reads and writes JSON.</p>
            <dl>
              <div><dt>stdin <span aria-hidden="true">→</span></dt><dd>Hetki sends your script the original context and earlier selections as JSON.</dd></div>
              <div><dt>stdout <span aria-hidden="true">←</span></dt><dd>Your script returns choices, one JSON object per line.</dd></div>
            </dl>
            <p>
              Hetki saves the selected item for later steps. Your final command receives the context and selections on stdin too.
              <a :href="withBase('/action-reference#program-input')">See the JSON format <PhArrowRight :size="12" aria-hidden="true" /></a>
            </p>
          </div>
        </div>
      </section>

      <section class="feature split mirrored">
        <div class="text" data-reveal>
          <h2>Fits how you already&nbsp;work.</h2>
          <p>
            One file sets how the picker opens, what it shows, its keys, and its colors.
          </p>
          <ul class="settings">
            <li v-for="item in settings" :key="item.key">
              <a :href="withBase(item.link)">
                <code>{{ item.key }}</code>
                <span class="what">{{ item.what }}</span>
                <PhArrowRight class="go" :size="14" weight="bold" aria-hidden="true" />
              </a>
            </li>
          </ul>
        </div>
        <div class="demo config-wrap" data-reveal>
          <ConfigDemo />
        </div>
      </section>
    </div>

    <section data-reveal>
      <div class="closing hk-frame hk-cta">
        <h2>your sessions stay<br />in tmux</h2>
        <p>
          Hetki works with the tmux server you already run, and closing the picker never closes a session.
          Inside tmux it switches your client. Outside, it attaches.
        </p>
        <div class="cta">
          <InstallCommand class="install" />
          <a class="guide" :href="withBase('/getting-started')">
            Get started <PhArrowRight :size="16" weight="bold" aria-hidden="true" />
          </a>
        </div>
      </div>
    </section>

    <footer class="footer">
      <p><span class="brand">Hetki</span> · Released under the MIT License.</p>
      <nav aria-label="Footer">
        <a :href="withBase('/getting-started')">Guide</a>
        <a href="https://github.com/MuhametSmaili/hetki/releases">Releases</a>
        <a href="https://github.com/MuhametSmaili/hetki">GitHub</a>
      </nav>
    </footer>
  </div>
</template>

<style scoped>
.sections {
  padding: 0 var(--hk-page);
}

.inner,
.footer {
  max-width: var(--hk-max);
  margin: 0 auto;
  padding-inline: var(--hk-pad);
}

.inner {
  display: grid;
  gap: 96px;
  padding-block: 96px;
}

.feature {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 40px;
  scroll-margin-top: calc(var(--vp-nav-height) + 24px);
}

h2 {
  font-size: clamp(28px, 3.4vw, 40px);
  font-weight: 600;
  line-height: 1.1;
  letter-spacing: -0.02em;
  color: var(--vp-c-text-1);
  text-wrap: balance;
}

.text p {
  max-width: 30rem;
  font-size: 17px;
  line-height: 1.6;
  color: var(--vp-c-text-2);
  text-wrap: pretty;
}

.text h2 + p,
.text p + p {
  margin-top: 16px;
}

.text .note {
  font-size: 15px;
  color: var(--vp-c-text-3);
}

.text .eyebrow {
  margin-bottom: 14px;
  color: var(--vp-c-brand-1);
  font-family: var(--vp-font-family-mono);
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.action-entry kbd {
  padding: 2px 5px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 4px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-2);
  font-family: var(--vp-font-family-mono);
  font-size: 12px;
  white-space: nowrap;
}

.text code {
  font-family: var(--vp-font-family-mono);
  font-size: 0.85em;
}

.action-launch {
  margin-top: 24px;
  padding-block: 16px;
  border-block: 1px solid var(--vp-c-divider);
}

.action-launch h3, .action-scripts h3 {
  font-size: 15px;
  font-weight: 500;
  color: var(--vp-c-text-1);
}

.action-launch dl, .action-scripts dl {
  display: grid;
  gap: 10px;
  margin: 14px 0;
}

.action-launch dl > div, .action-scripts dl > div {
  display: grid;
  grid-template-columns: 7ch minmax(0, 1fr);
  gap: 12px;
  font-size: 12px;
  line-height: 1.7;
}

.action-launch dt, .action-scripts dt {
  color: var(--vp-c-text-3);
}

.action-launch dd, .action-scripts dd {
  margin: 0;
  color: var(--vp-c-text-2);
}

.action-launch code {
  font-size: 12px;
  overflow-wrap: anywhere;
}

.action-launch .note {
  font-size: 12px;
}

.text .action-entry {
  margin-top: 20px;
  font-size: 15px;
}

.action-entry a, .action-scripts a {
  color: var(--vp-c-text-1);
  text-decoration: underline;
  text-underline-offset: 3px;
}

.action-scripts {
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid var(--vp-c-divider);
}

.action-scripts p {
  margin: 8px 0 0;
  font-size: 13px;
  line-height: 1.7;
  color: var(--vp-c-text-2);
}

.action-scripts dt {
  font-family: var(--vp-font-family-mono);
  color: var(--vp-c-brand-1);
  white-space: nowrap;
}

.action-scripts dl > div {
  grid-template-columns: 64px minmax(0, 1fr);
}

.action-scripts a {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.more {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 44px;
  margin-top: 8px;
  color: var(--vp-c-text-1);
  font-size: 15px;
  font-weight: 500;
  text-decoration: none;
}

.more svg,
.guide svg {
  transition: transform 0.15s ease-out, color 0.15s ease-out;
}

.more svg {
  color: var(--vp-c-text-3);
}

.more:hover svg {
  color: var(--vp-c-text-1);
  transform: translateX(2px);
}

.more:focus-visible,
.footer a:focus-visible {
  outline: 2px solid var(--vp-c-text-1);
  outline-offset: 2px;
  border-radius: 4px;
}

.steps {
  display: grid;
  margin-top: 24px;
  border-top: 1px solid var(--vp-c-divider);
}

.steps button {
  display: grid;
  /* The number column hugs its digit so the gap after it matches the left padding. */
  grid-template-columns: max-content minmax(0, 1fr);
  column-gap: 12px;
  row-gap: 2px;
  padding: 12px;
  border-bottom: 1px solid var(--vp-c-divider);
  border-left: 2px solid transparent;
  text-align: left;
  font: inherit;
  color: inherit;
  transition: background-color 0.15s ease-out, border-color 0.15s ease-out;
}

.steps button:hover {
  background: var(--vp-c-bg-soft);
}

.steps button[aria-selected='true'] {
  border-left-color: var(--vp-c-text-1);
  background: var(--vp-c-bg-soft);
}

.steps button:focus-visible {
  outline: 2px solid var(--vp-c-text-1);
  outline-offset: -2px;
}

.steps .n {
  grid-row: span 2;
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  line-height: 20px;
  color: var(--vp-c-text-3);
}

.steps code {
  font-family: var(--vp-font-family-mono);
  font-size: 14px;
  line-height: 20px;
  color: var(--vp-c-text-2);
  overflow-wrap: anywhere;
}

.steps [aria-selected='true'] code {
  color: var(--vp-c-text-1);
}

.steps .what {
  font-size: 14px;
  line-height: 1.5;
  color: var(--vp-c-text-3);
}

.steps + .more {
  margin-top: 16px;
}

.settings {
  margin: 24px 0 0;
  padding: 0;
  border-top: 1px solid var(--vp-c-divider);
  list-style: none;
}

.settings a {
  display: grid;
  grid-template-columns: 8ch minmax(0, 1fr) auto;
  align-items: baseline;
  gap: 16px;
  padding: 14px 4px 14px 0;
  border-bottom: 1px solid var(--vp-c-divider);
  color: var(--vp-c-text-2);
  font-size: 15px;
  line-height: 1.5;
  text-decoration: none;
  transition: color 0.15s ease-out;
}

.settings code {
  font-family: var(--vp-font-family-mono);
  font-size: 14px;
  color: var(--vp-c-text-1);
}

.settings .go {
  color: var(--vp-c-text-3);
  transition: transform 0.15s ease-out, color 0.15s ease-out;
}

.settings a:hover {
  color: var(--vp-c-text-1);
}

.settings a:hover .go {
  color: var(--vp-c-text-1);
  transform: translateX(2px);
}

.settings a:focus-visible {
  outline: 2px solid var(--vp-c-text-1);
  outline-offset: -2px;
  border-radius: 4px;
}

.config-wrap {
  container-type: inline-size;
}

.closing {
  padding: 64px var(--hk-pad);
  color: var(--frame-text);
  text-align: center;
}

.closing h2 {
  font-family: var(--vp-font-family-mono);
  font-size: clamp(28px, 4vw, 48px);
  font-weight: 500;
  line-height: 1.1;
  letter-spacing: -0.04em;
  color: inherit;
  text-wrap: balance;
}

.closing p {
  max-width: 34rem;
  margin: 20px auto 0;
  font-size: 17px;
  line-height: 1.6;
  color: var(--frame-muted);
  text-wrap: pretty;
}

.cta {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  margin-top: 32px;
}

.cta .install {
  width: 100%;
  max-width: 30rem;
  text-align: left;
}

.guide {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 44px;
  color: var(--frame-text);
  font-size: 15px;
  font-weight: 500;
  text-decoration: none;
}

.guide:hover svg {
  transform: translateX(2px);
}

.guide:focus-visible {
  outline: 2px solid var(--frame-text);
  outline-offset: 2px;
  border-radius: 6px;
}

.footer {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 8px 24px;
  padding-top: 24px;
  padding-bottom: calc(32px + env(safe-area-inset-bottom));
  font-size: 14px;
  color: var(--vp-c-text-3);
}

.brand {
  font-family: var(--vp-font-family-mono);
  font-weight: 600;
  color: var(--vp-c-text-2);
}

.footer nav {
  display: flex;
  gap: 24px;
}

.footer a {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  color: var(--vp-c-text-2);
  text-decoration: none;
  transition: color 0.15s ease-out;
}

.footer a:hover {
  color: var(--vp-c-text-1);
}

.reveal {
  opacity: 0;
  transform: translateY(12px);
  transition:
    opacity 0.6s cubic-bezier(0.22, 1, 0.36, 1),
    transform 0.6s cubic-bezier(0.22, 1, 0.36, 1);
}

.reveal.demo {
  transition-delay: 0.08s;
}

.reveal.revealed {
  opacity: 1;
  transform: none;
}

@media (prefers-reduced-motion: reduce) {
  .reveal {
    opacity: 1;
    transform: none;
    transition: none;
  }
}

@media (min-width: 768px) {
  .inner {
    gap: 128px;
    padding-block: 128px;
  }

  .closing {
    padding-block: 72px;
  }
}

@media (min-width: 1100px) {
  .inner {
    gap: 160px;
    padding-block: 144px 160px;
  }

  .split {
    grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
    align-items: center;
    gap: 64px;
  }

  .mirrored {
    grid-template-columns: minmax(0, 7fr) minmax(0, 5fr);
  }

  .mirrored .text {
    grid-column: 2;
    grid-row: 1;
  }

  .mirrored .demo {
    grid-column: 1;
    grid-row: 1;
  }

  .closing {
    padding-block: 88px;
  }
}
</style>
