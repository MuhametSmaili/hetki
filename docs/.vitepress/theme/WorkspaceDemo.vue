<script setup>
import { computed, ref } from 'vue'

const props = defineProps({ step: { type: String, required: true } })
const highlightedWindow = ref(null)
const selectedWindow = ref('editor') // start attaches to the first listed window

// `hetki save work` writes names, paths, layouts, and panes (yaml.v3 output).
// It never records running programs, so the `+` lines are commands added by hand.
const yaml = [
  ['sessions:'],
  ['    - name: hetki'],
  ['      windows:'],
  ['        - name: editor'],
  ['          path: ~/Projects/hetki'],
  ['          command: nvim', true],
  ['        - name: dev'],
  ['          path: ~/Projects/hetki/docs'],
  ['          layout: main-vertical'],
  ['          panes:'],
  ['            - path: ~/Projects/hetki/docs'],
  ['              command: npm run dev', true],
  ['            - path: ~/Projects/hetki'],
  ['              command: make test', true],
  ['            - path: ~/Projects/hetki'],
]

// What `hetki start work --dry-run` prints for that file when no `hetki`
// session exists, with ~ expanded to /home/you (internal/backend/tmux).
const plan = [
  ['new-session -d -s hetki -n editor -c /home/you/Projects/hetki -P -F #{window_id}|#{pane_id}', 'editor'],
  ['send-keys -l -t <new-pane:hetki:editor:0> -- nvim ; send-keys -t <new-pane:hetki:editor:0> Enter', 'editor'],
  ['new-window -t hetki: -n dev -c /home/you/Projects/hetki/docs -P -F #{window_id}|#{pane_id}', 'dev'],
  ['split-window -t <new-window:hetki:dev> -c /home/you/Projects/hetki -P -F #{pane_id}', 'dev'],
  ['split-window -t <new-window:hetki:dev> -c /home/you/Projects/hetki -P -F #{pane_id}', 'dev'],
  ['select-layout -t <new-window:hetki:dev> main-vertical', 'dev'],
  ['send-keys -l -t <new-pane:hetki:dev:0> -- npm run dev ; send-keys -t <new-pane:hetki:dev:0> Enter', 'dev'],
  ['send-keys -l -t <new-pane:hetki:dev:1> -- make test ; send-keys -t <new-pane:hetki:dev:1> Enter', 'dev'],
]

const shownYaml = computed(() => yaml.filter((line) => props.step === 'edit' || !line[1]))

const titles = {
  save: 'hetki save work',
  edit: 'Edit work.yaml — add startup commands',
  dry: 'hetki start work --dry-run',
  start: 'hetki start work',
}

function tokens(line) {
  let subcommand = true
  return line.split(' ').map((text) => {
    let tone = ''
    if (subcommand) tone = 'sub'
    else if (text === ';' || text.startsWith('-') || text.startsWith('#{')) tone = 'flag'
    else if (text.startsWith('<')) tone = 'ref'
    subcommand = text === ';'
    return [text, tone]
  })
}

// Splits "  - name: hetki" into indent, key, and value.
function yamlParts(text) {
  const match = text.match(/^(\s*-?\s*)([a-z_]+:)(.*)$/)
  return match ? match.slice(1) : [text, '', '']
}
</script>

<template>
  <div id="hk-workspace-panel" class="hk-panel workspace-demo" role="tabpanel" :aria-labelledby="`hk-workspace-step-${step}`" tabindex="0">
    <div class="hk-bar">
      <span><span v-if="step !== 'edit'" class="dim">$ </span>{{ titles[step] }}</span>
      <span v-if="step === 'dry' && highlightedWindow" class="dim">builds {{ highlightedWindow }}</span>
    </div>

    <div v-if="step === 'save' || step === 'edit'" class="body">
      <div v-if="step === 'save'" class="success">Saved to /home/you/.config/hetki/workspaces/work.yaml</div>
      <p class="explanation">{{ step === 'save' ? 'A new save records structure, not running commands.' : 'Add the startup commands you want. These lines are manual edits, not output from save.' }}</p>
      <div class="file">
        <div class="file-name">
          <span>work.yaml</span>
          <span :class="{ added: step === 'edit' }">{{ step === 'edit' ? '+ 3 lines added by hand' : 'As saved: no commands' }}</span>
        </div>
        <div class="code">
          <div v-for="([text, added], i) in shownYaml" :key="i" class="row" :class="{ added }">
            <span class="gutter">{{ added ? '+' : '' }}</span><span class="dim">{{ yamlParts(text)[0] }}</span><span class="key">{{ yamlParts(text)[1] }}</span><span>{{ yamlParts(text)[2] }}</span>
          </div>
        </div>
      </div>
      <p v-if="step === 'save'" class="caption">On later saves, Hetki preserves existing authored commands when it can safely match them to the live structure.</p>
    </div>

    <div v-else-if="step === 'dry'" class="body output" @mouseleave="highlightedWindow = null">
      <p class="explanation">Example after the manual edits, with no existing hetki session. A dry run does not apply changes.</p>
      <div class="dim">Workspace: /home/you/.config/hetki/workspaces/work.yaml</div>
      <div class="info">Dry run - actions to execute:</div>
      <div v-for="([line, owner], i) in plan" :key="i" class="cmd" :class="{ lit: owner === highlightedWindow }" @mouseenter="highlightedWindow = owner">
        <span class="dim">tmux</span> <template v-for="([text, tone], j) in tokens(line)" :key="j"><span :class="tone">{{ text }}</span>{{ ' ' }}</template>
      </div>
    </div>

    <div v-else class="body session">
      <div v-if="selectedWindow === 'editor'" class="panes">
        <div class="pane">
          <span><span class="dim">50 </span><span class="key">func</span> <span class="fn">handleTogglePreview</span>(m model, _ list.ItemID) (tea.Model, tea.Cmd) {</span>
          <span><span class="dim">51 </span>{{ '    ' }}m.preview.enabled = !m.preview.enabled</span>
          <span><span class="dim">52 </span>{{ '    ' }}<span class="key">return</span> m.<span class="fn">reflow</span>(), nil</span>
          <span><span class="dim">53 </span>}</span>
          <span class="spacer"></span>
          <span class="dim">-- NORMAL --{{ '  ' }}internal/tui/preview.go</span>
        </div>
      </div>
      <div v-else class="panes main-vertical">
        <div class="pane main">
          <span><span class="prompt">~/Projects/hetki/docs</span> $ npm run dev</span>
          <span class="dim">&gt; vitepress dev --host 127.0.0.1</span>
          <span><span class="ok">➜</span>{{ '  ' }}Local: http://127.0.0.1:5173/</span>
        </div>
        <div class="pane">
          <span><span class="prompt">~/Projects/hetki</span> $ make test</span>
          <span><span class="ok">--- PASS</span>: TestPreviewToggleModeOwnership</span>
          <span><span class="ok">ok</span>{{ '  ' }}github.com/MuhametSmaili/hetki/internal/tui{{ '  ' }}(cached)</span>
        </div>
        <div class="pane">
          <span><span class="prompt">~/Projects/hetki</span> $ ▌</span>
        </div>
      </div>
      <div class="tmux" role="group" aria-label="Sample windows in session hetki">
        <span class="session-name">[hetki]</span>
        <button
          v-for="(name, i) in ['editor', 'dev']"
          :key="name"
          type="button"
          :aria-pressed="selectedWindow === name"
          @click="selectedWindow = name"
        >{{ i }}:{{ name }}{{ selectedWindow === name ? '*' : '-' }}</button>
      </div>
      <p class="caption">
        Hetki creates missing windows, then attaches to <span class="strong">hetki</span>, or switches to it from inside tmux.
        Windows that already exist stay as they are.
      </p>
    </div>
  </div>
</template>

<style scoped>
.workspace-demo {
  display: flex;
  flex-direction: column;
  min-height: 540px;
}

.body {
  flex: 1;
  padding: 14px 16px;
  font-size: 12px;
  line-height: 20px;
}

.success {
  color: var(--hk-ok);
  overflow-wrap: anywhere;
}

.file {
  margin-top: 14px;
  overflow: hidden;
  border: 1px solid var(--hk-term-border);
  border-radius: 8px;
}

.file-name {
  display: flex;
  justify-content: space-between;
  gap: 2ch;
  padding: 6px 12px;
  border-bottom: 1px solid var(--hk-term-border);
  color: var(--hk-term-secondary);
}

.code {
  padding: 8px 0;
  overflow-x: auto;
  scrollbar-width: none;
}

.code::-webkit-scrollbar {
  display: none;
}

.row {
  padding-right: 12px;
  white-space: pre;
}

.gutter {
  display: inline-block;
  width: 3ch;
  color: var(--hk-ok);
  text-align: center;
}

.row.added {
  background: rgba(63, 185, 80, 0.1);
}

.added {
  color: var(--hk-ok);
}

.key {
  color: var(--hk-err);
}

.info {
  margin-bottom: 4px;
  color: var(--hk-term-command);
}

.sub {
  color: var(--hk-term-command);
}

.ref {
  color: var(--hk-term-active);
}

.dim {
  color: var(--hk-term-muted);
}

.flag {
  color: var(--hk-term-muted);
}

/* Align wrapped commands after the tmux prefix. */
.cmd {
  padding: 2px 0 2px 2ch;
  text-indent: -2ch;
  overflow-wrap: anywhere;
}

.cmd.lit {
  background: var(--hk-term-selection);
  box-shadow: inset 2px 0 var(--hk-term-active);
}

.session {
  display: flex;
  flex-direction: column;
}

.session .panes {
  flex: 1;
  display: grid;
  gap: 1px;
  min-height: 250px;
  overflow: hidden;
  border: 1px solid var(--hk-term-border);
  border-bottom: 0;
  border-radius: 6px 6px 0 0;
  background: var(--hk-term-border);
}

.main-vertical {
  grid-template-columns: 3fr 2fr;
  grid-template-rows: 1fr 1fr;
}

.main-vertical .main {
  grid-row: span 2;
}

.pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 8px 10px;
  background: var(--hk-term-bg);
  white-space: pre;
  overflow: hidden;
}

.spacer {
  flex: 1;
}

.fn {
  color: var(--hk-syntax-fn);
}

.ok {
  color: var(--hk-ok);
}

.prompt {
  color: var(--hk-term-prompt);
}

.tmux {
  display: flex;
  gap: 1ch;
  padding: 0 10px;
  border: 1px solid var(--hk-term-border);
  border-top: 0;
  border-radius: 0 0 6px 6px;
  background: var(--hk-tmux-bg);
  color: var(--hk-tmux-fg);
  line-height: 26px;
}

.session-name {
  font-weight: 600;
}

.tmux button {
  position: relative;
  color: inherit;
  font: inherit;
  opacity: 0.7;
}

.tmux button::before {
  content: '';
  position: absolute;
  inset: -8px -4px;
}

.tmux button[aria-pressed='true'] {
  opacity: 1;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.caption {
  margin: 14px 0 0;
  color: var(--hk-term-secondary);
}

.strong {
  color: var(--hk-term-text);
}

.explanation {
  margin: 0 0 12px;
  color: var(--hk-term-secondary);
}

.tmux button {
  min-height: 40px;
}

@media (max-width: 640px) {
  .workspace-demo {
    min-height: 0;
  }

  .session .panes {
    min-height: 180px;
  }
}
</style>
