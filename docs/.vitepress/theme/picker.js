import { computed, ref, watch } from "vue";

// Search is illustrative: the Go picker also ranks text matches and frecency.
function matchesQuery(item, query) {
  const fields = [item.session, item.window, item.path, item.command].map(
    (text) => text.toLowerCase(),
  );
  const words = query.toLowerCase().trim().split(/\s+/);

  return words.every((word) =>
    fields.some((field) => {
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

// Returns writable demo state; DOM focus and event cancellation belong to the view.
// handleKey returns true when the view must consume the keyboard event.
export function createPickerDemo(items) {
  const query = ref("");
  const mode = ref("filter");
  const tracked = ref(false);
  const cursor = ref(0);
  const activeId = ref(items[0]?.id);
  const opened = ref(null);
  const preview = ref(true);
  const visible = computed(() =>
    items.filter(
      (item) =>
        (!tracked.value || item.status) && matchesQuery(item, query.value),
    ),
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
    const normal = mode.value === "normal" && !event.ctrlKey && !event.altKey;

    switch (event.key) {
      case "ArrowUp":
        move(-1);
        return true;
      case "ArrowDown":
        move(1);
        return true;
      case "p":
      case "n":
        if (!event.ctrlKey) return false;

        move(event.key === "p" ? -1 : 1);
        return true;
      case "k":
      case "j":
        if (!normal) return false;

        move(event.key === "k" ? -1 : 1);
        return true;
      case "g":
      case "G":
        if (!normal) return false;

        cursor.value =
          event.key === "g" ? 0 : Math.max(0, visible.value.length - 1);
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
