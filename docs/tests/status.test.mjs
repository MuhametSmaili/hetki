import assert from 'node:assert/strict'
import test from 'node:test'
import { createRenderer, ref } from 'vue'
import { useStatusGlyph } from '../.vitepress/theme/status.js'

// Exercise Vue's real lifecycle without adding a DOM implementation.
function mountStatusDemo(t) {
  const motion = Object.assign(new EventTarget(), { matches: false })
  const document = Object.assign(new EventTarget(), { hidden: false })
  const timers = new Map()
  const originals = new Map()
  let notify
  let glyph
  let app
  let nextTimer = 0

  const browser = {
    document,
    matchMedia: () => motion,
    IntersectionObserver: class {
      constructor(callback) {
        notify = callback
      }
      observe() {}
      disconnect() {}
    },
    setInterval(callback) {
      const id = ++nextTimer
      timers.set(id, callback)
      return id
    },
    clearInterval(id) {
      timers.delete(id)
    },
  }

  t.after(() => {
    app?.unmount()
    for (const [name, descriptor] of originals) {
      if (descriptor) {
        Object.defineProperty(globalThis, name, descriptor)
      } else {
        delete globalThis[name]
      }
    }
  })

  for (const [name, value] of Object.entries(browser)) {
    originals.set(name, Object.getOwnPropertyDescriptor(globalThis, name))
    Object.defineProperty(globalThis, name, { configurable: true, writable: true, value })
  }

  const renderer = createRenderer({
    createComment: () => ({}),
    insert() {},
    remove() {},
    parentNode: () => null,
    nextSibling: () => null,
  })
  app = renderer.createApp({
    setup() {
      glyph = useStatusGlyph(ref({}))
      return () => null
    },
  })
  app.mount({})

  return {
    timers,
    glyph,
    unmount: () => app.unmount(),
    intersect(visible) {
      notify([{ isIntersecting: visible }])
    },
    reduceMotion(reduced) {
      motion.matches = reduced
      motion.dispatchEvent(new Event('change'))
    },
    hidePage(hidden) {
      document.hidden = hidden
      document.dispatchEvent(new Event('visibilitychange'))
    },
  }
}

test('visible working status advances with only one animation timer', (t) => {
  const demo = mountStatusDemo(t)

  demo.intersect(true)
  demo.intersect(true)
  for (const tick of demo.timers.values()) tick()

  assert.equal(demo.timers.size, 1)
  assert.equal(demo.glyph('working'), '⠙')
})

const pauseCases = [
  {
    name: 'offscreen',
    pause: (demo) => demo.intersect(false),
    resume: (demo) => demo.intersect(true),
  },
  {
    name: 'reduced motion',
    pause: (demo) => demo.reduceMotion(true),
    resume: (demo) => demo.reduceMotion(false),
  },
  {
    name: 'hidden page',
    pause: (demo) => demo.hidePage(true),
    resume: (demo) => demo.hidePage(false),
  },
]

for (const scenario of pauseCases) {
  test(`animation pauses and resumes for ${scenario.name}`, (t) => {
    const demo = mountStatusDemo(t)
    demo.intersect(true)

    scenario.pause(demo)

    assert.equal(demo.timers.size, 0)

    scenario.resume(demo)

    assert.equal(demo.timers.size, 1)

    demo.unmount()

    assert.equal(demo.timers.size, 0)
  })
}

test('a queued visibility notification cannot restart animation after unmount', (t) => {
  const demo = mountStatusDemo(t)
  demo.unmount()

  demo.intersect(true)

  assert.equal(demo.timers.size, 0)
})
