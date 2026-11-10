import { computed, reactive, ref } from "vue";
import { createFilteredList, moveStep } from "./picker.js";

// The move-pane recipe from docs/actions.md, walked through the real choice screens.
// Titles and synthetic items follow internal/app/actions/flow.go, built-in labels
// follow source.go, and the header and empty states follow internal/tui/choice.go.
export const ACTION_LABEL = "Move pane to another window…";
export const SOCKET = "/tmp/tmux-501/default";
export const SUBJECT = "%7";
const BUSY_MS = 350;

// tmux lists sessions by name. Each window's path is its active pane's directory.
export const actionSessions = [
  {
    id: "$3",
    name: "atlas",
    windows: [
      { id: "@8", index: 0, name: "editor", pane: "%12", path: "~/Projects/atlas", command: "nvim" },
      { id: "@9", index: 1, name: "terminal", pane: "%13", path: "~/Projects/atlas", command: "$ _" },
    ],
  },
  {
    id: "$5",
    name: "scratch",
    windows: [
      { id: "@14", index: 0, name: "dev", pane: SUBJECT, path: "~/Projects/atlas/web", command: "npm run dev" },
      { id: "@15", index: 1, name: "logs", pane: "%16", path: "~/Projects/atlas", command: "tail -f app.log" },
    ],
  },
  {
    id: "$4",
    name: "website",
    windows: [
      { id: "@10", index: 0, name: "docs", pane: "%14", path: "~/Projects/website", command: "nvim" },
      { id: "@11", index: 1, name: "preview", pane: "%15", path: "~/Projects/website", command: "$ _" },
    ],
  },
];

export const JOIN_ERROR =
  'action command "tmux": exit status 1: source and target panes must be different';

// Screen keys match the Go flow: the menu, then "<recipe>/<step index>".
export const MENU = "actions";
export const SESSIONS = "move-pane/0";
export const WINDOWS = "move-pane/1";
export const RUN = "move-pane/2";

const previousScreen = { [SESSIONS]: MENU, [WINDOWS]: SESSIONS, [RUN]: WINDOWS };

export function windowLabel(session, window) {
  return `${session.name} / ${window.index} ${window.name}`;
}

export function joinTarget({ session, window }) {
  return session && window ? `${session.id}:${window.id}.${window.pane}` : "";
}

// Returns writable demo state; DOM focus and event cancellation belong to the view.
// `schedule` runs a request's result later, so tests can complete it at once.
export function createActionFlowDemo({
  schedule = (work) => setTimeout(work, BUSY_MS),
  unschedule = clearTimeout,
} = {}) {
  const picked = reactive({ session: null, window: null });
  const history = new Map();
  const screen = ref(screenFor(MENU));
  const busy = ref(false);
  const error = ref("");
  const closed = ref(null); // "moved" or "cancelled"
  let pending;

  // Like the Go picker, typing filters labels only.
  const { query, cursor, visible, selected, move, moveTo } = createFilteredList(
    () => screen.value.items,
    (item) => [item.label],
  );

  function screenFor(key) {
    if (key === MENU) {
      return { key, title: "Actions · pane", items: [{ id: "move-pane", label: ACTION_LABEL }] };
    }
    if (key === SESSIONS) {
      return {
        key,
        title: `${ACTION_LABEL} › Choose destination session`,
        items: actionSessions.map((session) => ({ id: session.id, label: session.name, session })),
      };
    }
    if (key === WINDOWS) {
      const session = picked.session;
      return {
        key,
        title: `${ACTION_LABEL} › Choose destination window`,
        items: session.windows.map((window) => ({
          id: `${session.id}:${window.id}`,
          label: windowLabel(session, window),
          description: window.path,
          window,
        })),
      };
    }
    // Shown only after the command fails, so the run can be retried on purpose.
    return { key, title: ACTION_LABEL, items: [{ id: "run", label: `Run ${ACTION_LABEL}` }] };
  }

  // The right side of the header: an error or busy status replaces the title.
  const message = computed(() => {
    if (error.value) return error.value;
    if (busy.value) return "running action...";
    return `${screen.value.title} · Esc back`;
  });

  const emptyText = computed(() =>
    screen.value.items.length
      ? "no matching choices · ctrl+l clear filter · Esc back"
      : "no choices available · ctrl+r refresh · Esc back",
  );

  const command = computed(
    () => `tmux -S ${SOCKET} join-pane -d -s ${SUBJECT} -t ${joinTarget(picked)}`,
  );

  // Each screen keeps its filter and selection, so Back and refresh restore them.
  // Returning to the menu forgets every later screen, as the real picker does.
  function show(key, failure = "") {
    history.set(screen.value.key, { query: query.value, id: selected.value?.id });
    const position = history.get(key) ?? { query: "" };
    if (key === MENU) {
      for (const old of [...history.keys()]) {
        if (old !== MENU) history.delete(old);
      }
    }
    screen.value = screenFor(key);
    query.value = position.query;
    moveTo(visible.value.findIndex((item) => item.id === position.id));
    error.value = failure;
  }

  // Every selection, Back, and refresh is a request. Old rows stay on screen
  // until it finishes, and only Ctrl+C is accepted meanwhile.
  function request(work) {
    if (busy.value || closed.value) return;

    busy.value = true;
    error.value = "";
    pending = schedule(() => {
      pending = undefined;
      busy.value = false;
      work();
    });
  }

  function close(outcome) {
    dispose();
    busy.value = false;
    closed.value = outcome;
  }

  // Cancels a pending request, for example when the view unmounts.
  function dispose() {
    if (pending !== undefined) unschedule(pending);
    pending = undefined;
  }

  function execute() {
    if (picked.window.pane === SUBJECT) show(RUN, JOIN_ERROR);
    else close("moved");
  }

  function confirm(item = selected.value) {
    if (!item) return;

    request(() => {
      const key = screen.value.key;
      if (key === MENU) {
        show(SESSIONS);
      } else if (key === SESSIONS) {
        picked.session = item.session;
        show(WINDOWS);
      } else {
        if (key === WINDOWS) picked.window = item.window;
        execute();
      }
    });
  }

  // Escape on the menu closes the standalone picker without running anything.
  function back() {
    request(() => {
      const key = screen.value.key;
      if (key === MENU) close("cancelled");
      else show(previousScreen[key]);
    });
  }

  function refresh() {
    request(() => show(screen.value.key));
  }

  function restart() {
    close(null);
    picked.session = null;
    picked.window = null;
    history.clear();
    screen.value = screenFor(MENU);
    query.value = "";
    cursor.value = 0;
    error.value = "";
  }

  // Returns true when the view must consume the keyboard event.
  function handleKey(event) {
    if (event.isComposing || event.metaKey || event.altKey) return false;

    if (event.ctrlKey && event.key === "c") {
      close("cancelled");
      return true;
    }
    // While busy, swallow everything except Tab so focus can still leave.
    if (busy.value) return event.key !== "Tab";

    const step = moveStep(event);
    if (step) {
      move(step);
      return true;
    }

    if (event.key === "Escape") back();
    else if (event.key === "Enter") confirm();
    else if (event.ctrlKey && event.key === "r") refresh();
    else if (event.ctrlKey && event.key === "l") query.value = "";
    else return false;

    return true;
  }

  return {
    query,
    cursor,
    visible,
    selected,
    screen,
    busy,
    error,
    closed,
    picked,
    message,
    emptyText,
    command,
    confirm,
    back,
    refresh,
    restart,
    dispose,
    handleKey,
  };
}
