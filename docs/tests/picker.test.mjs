import assert from 'node:assert/strict'
import test from 'node:test'
import { createPickerDemo } from '../.vitepress/theme/picker.js'

const panes = [
  { id: 'editor', session: 'work', window: 'editor', path: '~/work', command: 'nvim' },
  { id: 'agent', session: 'api', window: 'agent', path: '~/api', command: 'pi', status: 'working' },
  { id: 'shell', session: 'notes', window: 'shell', path: '~/notes', command: '' },
]

test('normal-mode keys navigate without consuming filter text or browser modifiers', () => {
  const picker = createPickerDemo(panes)

  assert.equal(picker.handleKey({ key: 'j' }), false)
  assert.equal(picker.handleKey({ key: '/' }), false)
  assert.equal(picker.handleKey({ key: 'Tab', shiftKey: true }), false)
  assert.equal(picker.handleKey({ key: 'Escape', isComposing: true }), false)

  picker.handleKey({ key: 'Escape' })
  picker.handleKey({ key: 'j' })

  assert.equal(picker.selected.value.id, 'agent')

  picker.handleKey({ key: 'G' })
  assert.equal(picker.selected.value.id, 'shell')

  picker.handleKey({ key: 'g' })
  assert.equal(picker.selected.value.id, 'editor')
  assert.equal(picker.handleKey({ key: '/', metaKey: true }), false)
  assert.equal(picker.mode.value, 'normal')
})

test('preview can be toggled without changing the selection', () => {
  const picker = createPickerDemo(panes)

  picker.handleKey({ key: 'p', altKey: true })

  assert.equal(picker.preview.value, false)
  assert.equal(picker.selected.value.id, 'editor')
})

test('opening closes the picker and replay keeps the destination current', () => {
  const picker = createPickerDemo(panes)
  picker.handleKey({ key: 'ArrowDown' })

  picker.handleKey({ key: 'Enter' })

  assert.equal(picker.opened.value.id, 'agent')
  assert.equal(picker.activeId.value, 'agent')

  picker.replay()

  assert.equal(picker.opened.value, null)
  assert.equal(picker.selected.value.id, 'agent')
  assert.equal(picker.activeId.value, 'agent')
})

test('scope changes retain the selected pane when it remains visible', () => {
  const picker = createPickerDemo([...panes, { ...panes[1], id: 'review' }])
  picker.handleKey({ key: 'ArrowDown' })

  picker.handleKey({ key: 'Tab' })

  assert.equal(picker.selected.value.id, 'agent')
})

test('filtering searches directories and scope changes keep selection valid', () => {
  const picker = createPickerDemo(panes)
  picker.query.value = '~/nt'

  assert.equal(picker.selected.value.id, 'shell')

  picker.query.value = ''
  picker.handleKey({ key: 'ArrowDown' })
  picker.handleKey({ key: 'ArrowDown' })
  picker.handleKey({ key: 'Tab' })

  assert.deepEqual(picker.visible.value.map((pane) => pane.id), ['agent'])
  assert.equal(picker.selected.value.id, 'agent')

  picker.query.value = 'missing'
  picker.handleKey({ key: 'ArrowDown' })
  picker.handleKey({ key: 'Enter' })

  assert.equal(picker.selected.value, undefined)
  assert.equal(picker.opened.value, null)
})

test('movement stops at the first and last panes', () => {
  const picker = createPickerDemo(panes)

  picker.handleKey({ key: 'ArrowUp' })

  assert.equal(picker.selected.value.id, 'editor')

  for (let i = 0; i < panes.length + 1; i++) picker.handleKey({ key: 'ArrowDown' })

  assert.equal(picker.selected.value.id, 'shell')
})

test('Escape retains the query and slash returns to filtering', () => {
  const picker = createPickerDemo(panes)
  picker.query.value = 'api'

  const escaped = picker.handleKey({ key: 'Escape' })

  assert.equal(escaped, true)
  assert.equal(picker.mode.value, 'normal')
  assert.equal(picker.query.value, 'api')

  assert.equal(picker.handleKey({ key: '/' }), true)
  assert.equal(picker.mode.value, 'filter')
  assert.equal(picker.query.value, 'api')
})
