import { computed, ref, toValue, watch } from "vue";

// Search is illustrative: the Go picker also ranks text matches and frecency.
// Every word must match one field as an in-order subsequence.
export function matchesQuery(fields, query) {
  const haystacks = fields.map((text) => text.toLowerCase());
  const words = query.toLowerCase().trim().split(/\s+/);

  return words.every((word) =>
    haystacks.some((field) => {
      let next = 0;
      for (const character of field) {
        if (character === word[next]) {
          next++;
        }
      }

      return next === word.length;
    }),
  );
}

// Movement shared by every picker mode: arrows and Ctrl+P/Ctrl+N.
// Returns -1 or 1, or 0 when the event is not a movement key.
export function moveStep(event) {
  if (event.key === "ArrowUp") return -1;
  if (event.key === "ArrowDown") return 1;
  if (event.ctrlKey && event.key === "p") return -1;
  if (event.ctrlKey && event.key === "n") return 1;
  return 0;
}

// The filter and cursor shared by every picker demo. `items` is a ref or getter,
// so a demo can swap the list; `fields` returns the searchable text of one item.
// When the list changes, the cursor stays on the same item if it is still visible.
export function createFilteredList(items, fields) {
  const query = ref("");
  const cursor = ref(0);
  const visible = computed(() =>
    toValue(items).filter((item) => matchesQuery(fields(item), query.value)),
  );
  const selected = computed(() => visible.value[cursor.value]);

  watch(
    visible,
    (rows, previous) => {
      const id = previous[cursor.value]?.id;
      const retained = rows.findIndex((item) => item.id === id);
      cursor.value =
        retained >= 0
          ? retained
          : Math.min(cursor.value, Math.max(0, rows.length - 1));
    },
    { flush: "sync" },
  );

  function move(step) {
    cursor.value = Math.max(
      0,
      Math.min(visible.value.length - 1, cursor.value + step),
    );
  }

  function moveTo(index) {
    cursor.value = Math.max(0, Math.min(visible.value.length - 1, index));
  }

  return { query, cursor, visible, selected, move, moveTo };
}

// Returns writable demo state; DOM focus and event cancellation belong to the view.
// handleKey returns true when the view must consume the keyboard event.
export function createPickerDemo(items) {
  const mode = ref("filter");
  const tracked = ref(false);
  const activeId = ref(items[0]?.id);
  const opened = ref(null);
  const preview = ref(true);
  const { query, cursor, visible, selected, move, moveTo } = createFilteredList(
    () => items.filter((item) => !tracked.value || item.status),
    (item) => [item.session, item.window, item.path, item.command],
  );

  function openSelection() {
    const item = selected.value;
    if (!item) return;

    activeId.value = item.id;
    opened.value = item;
  }

  function replay() {
    opened.value = null;
    mode.value = "filter";
  }

  function handleNavigationKey(event) {
    const step = moveStep(event);
    if (step) {
      move(step);
      return true;
    }

    if (mode.value !== "normal" || event.ctrlKey || event.altKey) return false;

    switch (event.key) {
      case "k":
      case "j":
        move(event.key === "k" ? -1 : 1);
        return true;
      case "g":
      case "G":
        moveTo(event.key === "g" ? 0 : visible.value.length - 1);
        return true;
      default:
        return false;
    }
  }

  function handleKey(event) {
    if (event.isComposing || event.metaKey) return false;

    if (event.key === "Enter") {
      openSelection();
      return true;
    }

    if (event.altKey && event.key.toLowerCase() === "p") {
      preview.value = !preview.value;
      return true;
    }

    if (event.key === "Escape") {
      mode.value = "normal";
      return true;
    }

    if (event.key === "Tab" && !event.shiftKey) {
      tracked.value = !tracked.value;
      return true;
    }

    if (mode.value === "normal" && event.key === "/") {
      mode.value = "filter";
      return true;
    }

    return handleNavigationKey(event);
  }

  return {
    query,
    mode,
    tracked,
    cursor,
    visible,
    selected,
    activeId,
    opened,
    preview,
    replay,
    handleKey,
  };
}
