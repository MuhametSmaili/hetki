import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createActionFlowDemo, JOIN_ERROR, MENU, RUN, SESSIONS, WINDOWS,
} from '../.vitepress/theme/action-flow.js'

// Finish every request at once; `hold` keeps the next one pending instead.
function createFlow() {
  let held
  let hold = false
  const flow = createActionFlowDemo({
    schedule(work) {
      if (hold) {
        held = work
        return 1
      }
      work()
      return undefined
    },
    unschedule() {
      held = undefined
    },
  })
  return {
    flow,
    holdNext() {
      hold = true
    },
    finish() {
      hold = false
      held?.()
      held = undefined
    },
  }
}

const labels = (flow) => flow.visible.value.map((item) => item.label)
const press = (flow, key, extra = {}) => flow.handleKey({ key, ...extra })

function chooseLabel(flow, label) {
  const item = flow.visible.value.find((candidate) => candidate.label === label)
  assert.ok(item, `no row labeled ${label}`)
  flow.confirm(item)
}

test('screens use the Go titles, labels, and descriptions', () => {
  const { flow } = createFlow()

  assert.equal(flow.message.value, 'Actions · pane · Esc back')
  assert.deepEqual(labels(flow), ['Move pane to another window…'])

  press(flow, 'Enter')
  assert.equal(flow.screen.value.key, SESSIONS)
  assert.equal(flow.message.value, 'Move pane to another window… › Choose destination session · Esc back')
  assert.deepEqual(labels(flow), ['atlas', 'scratch', 'website'])

  chooseLabel(flow, 'website')
  assert.equal(flow.screen.value.key, WINDOWS)
  assert.deepEqual(labels(flow), ['website / 0 docs', 'website / 1 preview'])
  assert.equal(flow.visible.value[0].description, '~/Projects/website')
})

test('typing filters labels only and empty lists explain why', () => {
  const { flow } = createFlow()
  press(flow, 'Enter')
  chooseLabel(flow, 'atlas')

  // Descriptions hold the path; it must not match.
  flow.query.value = 'Projects'
  assert.deepEqual(labels(flow), [])
  assert.equal(flow.emptyText.value, 'no matching choices · ctrl+l clear filter · Esc back')
  assert.equal(press(flow, 'Enter'), true)
  assert.equal(flow.screen.value.key, WINDOWS)

  press(flow, 'l', { ctrlKey: true })
  assert.equal(flow.query.value, '')

  flow.query.value = 'term'
  assert.deepEqual(labels(flow), ['atlas / 1 terminal'])
})

test('Back restores the earlier filter and selection', () => {
  const { flow } = createFlow()
  press(flow, 'Enter')
  press(flow, 'ArrowDown')
  press(flow, 'ArrowDown')
  assert.equal(flow.selected.value.label, 'website')

  press(flow, 'Enter')
  press(flow, 'Escape')

  assert.equal(flow.screen.value.key, SESSIONS)
  assert.equal(flow.selected.value.label, 'website')
})

test('the menu forgets later screens and Escape on it closes without running', () => {
  const { flow } = createFlow()
  press(flow, 'Enter')
  flow.query.value = 'web'
  press(flow, 'Escape')
  assert.equal(flow.screen.value.key, MENU)

  press(flow, 'Enter')
  assert.equal(flow.query.value, '')

  press(flow, 'Escape')
  press(flow, 'Escape')
  assert.equal(flow.closed.value, 'cancelled')
})

test('joining a pane to its own window fails and offers a deliberate retry', () => {
  const { flow } = createFlow()
  press(flow, 'Enter')
  chooseLabel(flow, 'scratch')
  chooseLabel(flow, 'scratch / 0 dev')

  assert.equal(flow.screen.value.key, RUN)
  assert.equal(flow.message.value, JOIN_ERROR)
  assert.deepEqual(labels(flow), ['Run Move pane to another window…'])

  // Refresh reloads the screen without running the command.
  press(flow, 'r', { ctrlKey: true })
  assert.equal(flow.closed.value, null)
  assert.equal(flow.message.value, 'Move pane to another window… · Esc back')

  press(flow, 'Escape')
  assert.equal(flow.screen.value.key, WINDOWS)
  chooseLabel(flow, 'scratch / 1 logs')
  assert.equal(flow.closed.value, 'moved')
  assert.equal(flow.command.value, 'tmux -S /tmp/tmux-501/default join-pane -d -s %7 -t $5:@15.%16')
})

test('while a request runs only Ctrl+C works, and it cancels the request', () => {
  const { flow, holdNext, finish } = createFlow()
  holdNext()
  press(flow, 'Enter')

  assert.equal(flow.busy.value, true)
  assert.equal(flow.message.value, 'running action...')
  assert.deepEqual(labels(flow), ['Move pane to another window…'])
  assert.equal(press(flow, 'Escape'), true)
  assert.equal(press(flow, 'Tab'), false)

  finish()
  assert.equal(flow.screen.value.key, SESSIONS)

  holdNext()
  press(flow, 'Enter')
  press(flow, 'c', { ctrlKey: true })
  finish()
  assert.equal(flow.closed.value, 'cancelled')
  assert.equal(flow.busy.value, false)
})

test('restart reopens the menu with no earlier selections', () => {
  const { flow } = createFlow()
  press(flow, 'Enter')
  chooseLabel(flow, 'atlas')
  chooseLabel(flow, 'atlas / 0 editor')
  assert.equal(flow.closed.value, 'moved')

  flow.restart()

  assert.equal(flow.closed.value, null)
  assert.equal(flow.screen.value.key, MENU)
  assert.equal(flow.picked.session, null)
})

test('dispose cancels a pending request', () => {
  const { flow, holdNext, finish } = createFlow()
  holdNext()
  press(flow, 'Enter')

  flow.dispose()
  finish()

  assert.equal(flow.screen.value.key, MENU)
})
