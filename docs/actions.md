# User-defined actions

Define an action in YAML by chaining selection steps, then running a command.
Each step can use data from earlier choices. Use built-in tmux lists, fixed choices,
or your own scripts to supply the items.

Scripts receive the original context and earlier selections as JSON on stdin.
They return choices as one JSON object per line on stdout. Hetki keeps the selected
item for later steps and sends the context and selections to the final command on stdin too.
See [stdin and stdout](#external-scripts-stdin-and-stdout) for an example.

For example: **Move pane → choose session → choose window → move the pane.**

Each recipe has two types of step:

- `select`: get choices and wait for you to select one. You can have zero or more selection steps.
- `run`: execute a command. **Exactly one `run` is required, and it must be the last step.**

An action with only `run` executes as soon as you select the action.

Open your actions in either place:

- **Inside Hetki:** highlight a row, press `Ctrl+K`, then `U` for **User actions…**.
  In normal mode, press `U` directly.
  The highlighted row is the subject. After the action finishes, you return to the normal list.
- **From a terminal or tmux binding:** run `hetki action` with a pane, window, or session ID.
  This opens a standalone picker that closes when the action finishes.

Both use the same recipes. The [action reference](./action-reference.md) lists all fields and limits.
To change `U`, set `keymap.normal.user_actions` or `keymap.menu.user_actions`
for the corresponding mode in your [key bindings](key-bindings.md).

From a terminal inside tmux, open actions for the current pane with:

```bash
hetki action --pane-id "$TMUX_PANE"
```

You can also target a window or session. Replace these example IDs with your own:

```bash
hetki action --window '@8'
hetki action --session '$3'
```

## Try one action

### 1. Create the file

Create `~/.config/hetki/actions.yaml`.
If you set `XDG_CONFIG_HOME`, use `$XDG_CONFIG_HOME/hetki/actions.yaml` instead.
This is a separate file from Hetki's UI configuration.

Paste this complete example:

```yaml
version: 1
actions:
  move-pane:
    label: Move pane to another window…
    scope: [pane]
    steps:
      - select: session
        title: Choose destination session
        source:
          builtin: tmux.sessions

      - select: destination
        title: Choose destination window
        source:
          builtin: tmux.windows
          session: "${steps.session.value.session_id}"

      - run:
          argv:
            - tmux
            - -S
            - "${context.server.socket}"
            - join-pane
            - -d
            - -s
            - "${context.subject.pane_id}"
            - -t
            - "${steps.destination.value.pane_target}"
```

### 2. Open it from a tmux pane

```bash
hetki action --pane-id "$TMUX_PANE"
```

`$TMUX_PANE` contains the current pane ID, such as `%39`.
To use another file, add `--file /path/to/actions.yaml`.
For this example, have another tmux window available as the destination.

### 3. Move the pane

1. Select **Move pane to another window…**.
2. Select a session.
3. Select a different window.

The last selection runs `tmux join-pane` immediately.
The original pane moves into the selected window.
It splits the destination's active pane.
The `-d` argument keeps the destination window in the background.

## How does the flow work?

![The original pane stays in context. Choose an action, then a session, then a window. Each selection is saved under steps. The command uses the original pane and the selected window.](/action-flow.svg)

Hetki reads the steps from top to bottom.
`select` opens a list. `run` executes the final command.

The name after `select` tells Hetki where to store the selected item.
Hetki keeps the whole item in memory while the flow is open. It does not save selections to disk.

| Step                  | Where Hetki stores the selection | What uses it next                                        |
| --------------------- | -------------------------------- | -------------------------------------------------------- |
| `select: session`     | `steps.session`                  | The next source reads `steps.session.value.session_id`.  |
| `select: destination` | `steps.destination`              | The command reads `steps.destination.value.pane_target`. |

For example, you select session `$3`. Hetki stores this item:

```json
{
  "id": "$3",
  "label": "work",
  "value": { "session_id": "$3" }
}
```

The next step contains:

```yaml
session: "${steps.session.value.session_id}"
```

Hetki replaces that reference with `$3`.
The next list shows only windows in session `$3`.
You do not need a pipe or a temporary file to pass the selection.

## What is context?

**Context describes what you started with. Selections describe what you chose later.**

| Field                   | Meaning                                          | Example                            |
| ----------------------- | ------------------------------------------------ | ---------------------------------- |
| `context.subject`       | The pane, window, or session you want to act on. | The original pane `%39`.           |
| `context.subject.cwd`   | Pane directory when the flow opened, if known.   | `/work/api`                       |
| `context.server.socket` | The address of its tmux server.                  | `/tmp/tmux-501/default`            |
| `context.invocation`    | Where Hetki was started, when known.             | The pane that contains the picker. |
| `steps.NAME`            | The item selected in the step named `NAME`.      | The destination window.            |

The subject stays the same through the whole flow.
Selecting a destination does not replace the original pane.
For pane subjects, `cwd` is a snapshot from tmux. It is absent when unknown and
for window/session subjects. It does not change when you refresh choices.

Use `${context.subject.pane_id}` for the original pane.
Use `${steps.destination.value.pane_target}` for the destination.

## Can an action have two scopes?

Yes. List all the scopes that can use the action:

```yaml
scope: [pane, window]
```

This means **pane OR window**. Each invocation still has one subject.
All three scopes are supported:

| Scope | Command to open its actions | Subject fields you can use |
| --- | --- | --- |
| `pane` | `hetki action --pane-id '%39'` | `pane_id`, `window_id`, optional `cwd` |
| `window` | `hetki action --window '@8'` | `window_id` |
| `session` | `hetki action --session '$3'` | `session_id` |

Pane and window subjects also have `session_id` when the session is unambiguous.
Linked windows can belong to more than one session.

**Scope controls which actions appear. It does not filter a source's choices.**
Use a source's `session` or `window` field to filter its list.

For a `[pane, window]` action, use `context.subject.window_id` to target the window.
Do not use `pane_id`: a window subject does not have that field.

## Where do the choices come from?

Use exactly one source type in each selection step:

| Source    | Use it for                       | Example                                   |
| --------- | -------------------------------- | ----------------------------------------- |
| `builtin` | Live tmux objects.               | Sessions, windows, panes.                 |
| `items`   | A fixed list in YAML.            | Side by side or stacked.                  |
| `exec`    | A list produced by your program. | Git branches, worktrees, project folders. |

Each choice has an `id` and a `label`. It can also have a `description` for
secondary display text and a `value` object for data used by later steps.
**Typing searches labels only**, not descriptions or values.
See the [item format](./action-reference.md#item-format) for the shared fields.

### Built-in sources

These are **all three** built-in sources:

| Name            | Choices                                         | Fields in each item's `value`                                        |
| --------------- | ----------------------------------------------- | -------------------------------------------------------------------- |
| `tmux.sessions` | Sessions on the current server.                 | `session_id`                                                         |
| `tmux.windows`  | Windows, listed under their sessions.           | `session_id`, `window_id`, `window_target`, `pane_id`, `pane_target` |
| `tmux.panes`    | Panes, listed under their windows and sessions. | `session_id`, `window_id`, `window_target`, `pane_id`, `pane_target` |

For a window choice, `pane_id` identifies its active pane when the list loads.
`window_target` includes the session, for example `$3:@8`.
`pane_target` includes all three IDs, for example `$3:@8.%42`.

Git repositories, branches, and worktrees are **not built in**.
Use `exec` to supply those choices. No Hetki code change is needed.

### External scripts: stdin and stdout

This step tells Hetki **which program supplies the choices**:

```yaml
- select: branch
  title: Choose a branch
  source:
    exec:
      argv: [python3, /absolute/path/to/worktrees.py, branches]
```

`branches` is an argument for your script. It has no special meaning to Hetki.
The script receives context and previous selections, then prints choices:

```text
Hetki ── stdin: JSON request ──→ script
Hetki ←─ stdout: JSON items ─── script
```

Hetki connects both streams automatically. Your script reads the request from
stdin and writes one JSON item per line to stdout:

```python
import json
import sys

request = json.load(sys.stdin)
print(json.dumps({
    "id": "scratch",
    "label": "New scratch branch",
    "value": {"branch": "try/example", "create": True},
}))
```

**The output contains items, not a `source` definition or another recipe.**
Your script defines `branch` and `create` inside `value`. Hetki keeps those fields as data.
Write diagnostics to stderr so they do not get parsed as choices.

1. Hetki starts the script and sends the JSON request to its stdin.
2. Hetki reads stdout and waits for the script to exit successfully.
3. Hetki validates the items, then displays their labels and optional descriptions.
4. You select a choice. Hetki stores the whole item in `steps.branch`.
5. Later providers and the final command receive that selection in their JSON stdin.

The final command can read it with:

```python
import json
import sys

request = json.load(sys.stdin)
choice = request["steps"]["branch"]["value"]
branch_name = choice["branch"]
create_new = choice["create"]
```

The YAML still defines what comes next: another `select`, or the final `run`.
Hetki waits for `run` to finish. Its stdout is captured, not used as a new choice list.

For a complete recipe and provider, see [program-generated lists](./action-reference.md#program-generated-lists-exec).
For commands that need the selected pane's repository, see [working directory](./action-reference.md#working-directory).

## Open the menu from tmux

Add this binding to `~/.tmux.conf`:

```text
bind -N "Pane actions popup" o run-shell -C 'display-popup -B -E -w 80% -h 80% hetki action --pane-id "#{pane_id}" --height 100%'
```

Reload the file:

```bash
tmux source-file ~/.tmux.conf
```

Press your tmux prefix, then `o`.
**`run-shell -C` replaces `#{pane_id}` with the pane you were in when you pressed the key.**
You do not need to find the ID yourself.

Here, `run-shell -C` runs a tmux command after it expands the format fields.
Putting `#{pane_id}` directly in the popup command does not expand it.

`-B` removes the popup border. `-E` closes the popup when Hetki exits.
`--height 100%` makes Hetki fill the popup.

For window or session actions, use these bindings instead:

```text
bind -N "Window actions popup" o run-shell -C 'display-popup -B -E -w 80% -h 80% hetki action --window "#{window_id}" --height 100%'
bind -N "Session actions popup" o run-shell -C "display-popup -B -E -w 80% -h 80% hetki action --session '#{session_id}' --height 100%"
```

Choose one binding for `o`, or assign different keys.
Keep the inner single quotes around the session ID. They protect its `$` during command parsing.

For the regular session/window switcher, run `hetki` without `action`:

```text
bind -N "Session switcher popup" o display-popup -B -E -w 80% -h 80% 'hetki --height 100%'
```

## Keys and completion

These are the default keys:

| Input | Result |
| --- | --- |
| Type | Filter the current list by label. |
| Enter | Select an item. The final selection runs the command. |
| Escape | Go back one screen. |
| Ctrl+R | Reload the choices. |
| Ctrl+L | Clear the filter and keep typing. |
| Ctrl+C | Close the picker and cancel its running command. |

Back restores the previous search and selection when possible.
Returning to a selection step reloads its choices. Escape from the action menu closes the flow.
Back and refresh do not run the final command again.
While a program runs, only Ctrl+C is available.
Escape always goes Back, even if you remap it for the normal picker.
Other supported keys use the current mode's bindings.
See [action choice screens](key-bindings.md#action-choice-screens) for the exceptions.

### Empty lists and loading errors

If the list is empty, the message explains why:

- **No choices available:** the source returned an empty list. Refresh or go Back.
- **No matching choices:** choices loaded, but none match your filter. Edit or clear the filter.
- **Could not load choices:** the source failed. The diagnostic stays visible; retry loading or go Back.

The recovery hints follow your key bindings. An empty list never submits a choice.
If the action menu itself is empty, check that a recipe's `scope` matches the subject.

Ctrl+R reloads the current choices, not `actions.yaml`.
Close and reopen the action menu after editing recipes.

### Command results

Success closes the action picker. A command failure shows a **Run** item for retry.
If the command exits with code `0` but output capture or cleanup fails,
Hetki shows **Close completed action** and a diagnostic. Enter or Escape closes it.
Back and refresh cannot retry that completed command.
Failure or cancellation does not undo changes that the command already made.
Retry can repeat those changes.

For program input, command rules, and limits, see the [action reference](./action-reference.md).
