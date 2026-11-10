<script setup>
import ActionDemo from './.vitepress/theme/ActionDemo.vue'
</script>

# User-defined actions

An action is a list of steps in a YAML file. Each step shows a list of choices or runs a command.
Later steps can use the choices that you selected before.

An action has two types of step:

- `select`: shows a list of choices. You select one. An action can have zero or more `select` steps.
- `run`: runs a command. **Each action must end with one `run` step.**

If an action has only a `run` step, Hetki runs the command when you select the action.

## Try one action

This action moves a pane to a different window.

### 1. Create the actions file

Create `~/.config/hetki/actions.yaml`.
If you set `XDG_CONFIG_HOME`, use `$XDG_CONFIG_HOME/hetki/actions.yaml`.
This file is separate from the Hetki UI configuration.

Paste this example:

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

### 2. Open the action menu

In a tmux pane, run:

```bash
hetki action --pane-id "$TMUX_PANE"
```

`$TMUX_PANE` contains the ID of the current pane, for example `%39`.
To use a different actions file, add `--file /path/to/actions.yaml`.

### 3. Move the pane

Make sure that you have a second tmux window. Then:

1. Select **Move pane to another window…**.
2. Select a session.
3. Select a different window.

When you select the window, Hetki runs `tmux join-pane`.
The pane moves into the selected window and splits its active pane.
The `-d` argument keeps the selected window in the background.

You can also try the action here. The demo uses sample sessions and runs no commands.
To see a failed command, select **scratch**, then **scratch / 0 dev**.

<ActionDemo />

## Open actions from a tmux key

First, try the popup. In a tmux pane, run:

```bash
tmux display-popup -B -E -w 80% -h 80% "hetki action --pane-id $TMUX_PANE --height 100%"
```

The action menu opens in a popup for your current pane.

- `-B` removes the popup border. `-E` closes the popup when Hetki stops.
- `--height 100%` makes Hetki fill the popup.

To open the popup with a key, add this line to `~/.tmux.conf`:

```text
bind -N "Pane actions popup" a run-shell -C 'display-popup -B -E -w 80% -h 80% hetki action --pane-id "#{pane_id}" --height 100%'
```

`a` is an example. You can use a different key.
By default, tmux does not use `a`, `b`, `e`, `h`, `j`, `k`, `u`, `v`, or `y`.
Other keys replace a tmux key binding.

Load the changed file:

```bash
tmux source-file ~/.tmux.conf
```

Press your tmux prefix, then `a`.

`run-shell -C` replaces `#{pane_id}` with the ID of your current pane.
Without `-C`, tmux does not replace it in the popup command.

For window or session actions, use one of these lines:

```text
bind -N "Window actions popup" a run-shell -C 'display-popup -B -E -w 80% -h 80% hetki action --window "#{window_id}" --height 100%'
bind -N "Session actions popup" a run-shell -C "display-popup -B -E -w 80% -h 80% hetki action --session '#{session_id}' --height 100%"
```

All three lines use the same key. Use one line, or give each line a different key.
Keep the single quotes around `#{session_id}`. They stop tmux from changing the `$` in the session ID.

You can also open the action menu inside Hetki, for the selected row.
Press `U` in normal mode, or press `Ctrl+K`, then `U`.
When the action finishes, the normal list comes back.
To change these keys, see [key bindings](key-bindings.md#normal-mode).

## How steps share data

![The pane stays in the context. You select an action, then a session, then a window. Hetki keeps each selection under steps. The command uses the pane from the context and the selected window.](/action-flow.svg)

Hetki does the steps in order, from top to bottom.
The name after `select` is the name of the step.
Hetki keeps the selected choice under `steps.<name>`:

| Step                  | Hetki keeps the choice in | Used by                                              |
| --------------------- | ------------------------- | ---------------------------------------------------- |
| `select: session`     | `steps.session`           | The next source: `steps.session.value.session_id`    |
| `select: destination` | `steps.destination`       | The command: `steps.destination.value.pane_target`   |

For example, you select the session `$3`. Hetki keeps this choice:

```json
{
  "id": "$3",
  "label": "work",
  "value": { "session_id": "$3" }
}
```

The next step contains this line:

```yaml
session: "${steps.session.value.session_id}"
```

Hetki replaces the reference with `$3`. The next list shows only the windows in session `$3`.

### Context

The context tells the action what to act on. It does not change while the action runs.

| Field                   | Meaning                                                        | Example                       |
| ----------------------- | -------------------------------------------------------------- | ----------------------------- |
| `context.subject`       | The pane, window, or session that the action acts on.          | The pane `%39`                |
| `context.subject.cwd`   | The directory of the pane when the action menu opened. Panes only, if tmux knows it. | `/work/api` |
| `context.server.socket` | The socket of the tmux server.                                 | `/tmp/tmux-501/default`       |
| `context.invocation`    | The pane that Hetki opened in, if known.                       | The pane that shows the picker |

Your selections do not change the subject.
In the example, `${context.subject.pane_id}` is the pane to move,
and `${steps.destination.value.pane_target}` is the destination.

## Where an action appears

The `scope` field sets which subjects can use an action.
Use one or more of `pane`, `window`, and `session`:

```yaml
scope: [pane, window]
```

This action appears for panes and for windows. Each time, it acts on one subject.

| Scope     | Open the action menu           | Subject fields                              |
| --------- | ------------------------------ | ------------------------------------------- |
| `pane`    | `hetki action --pane-id '%39'` | `pane_id`, `window_id`, `session_id`, `cwd` |
| `window`  | `hetki action --window '@8'`   | `window_id`, `session_id`                   |
| `session` | `hetki action --session '$3'`  | `session_id`                                |

Some fields can be missing. See [program input](./action-reference.md#program-input).
For an action with `scope: [pane, window]`, use `context.subject.window_id`.
A window subject has no `pane_id`.

`scope` does not filter the choices in a list.
To filter a list, use the `session` or `window` field of its source.

If the action menu is empty, make sure that the `scope` of an action includes the subject.

## Where the choices come from

Each `select` step has one source:

| Source    | Choices                                | Example                                   |
| --------- | -------------------------------------- | ----------------------------------------- |
| `builtin` | Live tmux sessions, windows, or panes. | The sessions on your server.              |
| `items`   | A fixed list in the YAML file.         | Side by side or stacked.                  |
| `exec`    | A list from your script.               | Git branches, worktrees, project folders. |

Each choice has an `id` and a `label`. It can also have:

- a `description`, which shows in a second column;
- a `value`, which holds data for later steps.

When you type, Hetki searches only the labels.
For all choice fields, see the [item format](./action-reference.md#item-format).

### Built-in sources

| Name            | One choice for each | Label                      | Description                  |
| --------------- | ------------------- | -------------------------- | ---------------------------- |
| `tmux.sessions` | Session             | `work`                     | None                         |
| `tmux.windows`  | Window              | `work / 1 editor`          | Directory of the active pane |
| `tmux.panes`    | Pane                | `work / 1 editor / 0 nvim` | Directory of the pane        |

The lists use the tmux order. Each `value` contains `session_id`.
Window and pane choices also contain `window_id`, `window_target`, `pane_id`, and `pane_target`:

- `window_target` contains the session and window, for example `$3:@8`.
- `pane_target` contains the session, window, and pane, for example `$3:@8.%42`.
- For a window choice, `pane_id` is the active pane of the window.

The lists include the subject.
The `session` and `window` filters keep one ID. They cannot remove one.
To remove the subject, use an `exec` source. Otherwise, make sure that your command can handle it.

Git repositories, branches, and worktrees are not built in. Use an `exec` source for them.

### Scripts

An `exec` source runs your script to get the choices:

```yaml
- select: branch
  title: Choose a branch
  source:
    exec:
      argv: [python3, /absolute/path/to/worktrees.py, branches]
```

```text
Hetki ── stdin: JSON request ──→ script
Hetki ←─ stdout: JSON choices ── script
```

Hetki sends a JSON request to the stdin of the script.
The request contains the context and the earlier selections.
The script writes one JSON choice on each line of stdout:

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

Write errors and other messages to stderr. Hetki reads each stdout line as a choice.

When you select a choice, Hetki keeps all of it, with its `value`, in `steps.branch`.
Later scripts and the final command get it in their JSON stdin:

```python
import json
import sys

request = json.load(sys.stdin)
choice = request["steps"]["branch"]["value"]
branch_name = choice["branch"]
create_new = choice["create"]
```

For a complete example, see [program-generated lists](./action-reference.md#program-generated-lists-exec).
For commands that need the repository of the selected pane, see [working directory](./action-reference.md#working-directory).

## Use the action picker

### The screen

Each step shows a list. This is the window step of the example, at 80 columns:

```text
> █                                  Move pane to another window… › Cho...
──────────────────────────────────────────────────────────────────────────
  atlas / 0 editor    ~/Projects/atlas
  atlas / 1 terminal  ~/Projects/atlas
```

- At the left, `>` is the filter. Each screen starts in filter mode, so you can type immediately.
- At the right is the title. For a `select` step, the title is the action label, then the step `title`.
  If the step has no `title`, Hetki uses the step name.
- Each row shows the label of a choice. A description shows in a second column.
- The action menu shows your actions in the order of their IDs, not in file order.

The title uses half of the width or less.
Use a short action label and short step titles, so that the step title stays visible.

When Hetki loads choices or runs the command, the title changes to `running action...`.
When a step fails, the title changes to the error.

### Keys

| Key    | Result                                               |
| ------ | ---------------------------------------------------- |
| Type   | Filter the list by label.                            |
| Enter  | Select a choice. The last selection runs the command. |
| Escape | Go back one screen.                                  |
| Ctrl+R | Reload the choices.                                  |
| Ctrl+L | Clear the filter.                                    |
| Ctrl+C | Close the action picker and stop the command.        |

- Escape always goes back, also if you change Escape for the normal picker.
  On the action menu, Escape closes the action picker.
- When you go back, Hetki loads the choices again. If it can, it keeps your filter and selected row.
- Going back and reloading never run the command again.
- While Hetki loads choices or runs a command, only Ctrl+C works.
- Ctrl+R reloads the choices, not `actions.yaml`. After you change `actions.yaml`, open the action menu again.

For the other keys, see [action choice screens](key-bindings.md#action-choice-screens).

### Empty lists

| Message                                                | Meaning                                     |
| ------------------------------------------------------ | ------------------------------------------- |
| `no choices available · ctrl+r refresh · Esc back`     | The source gave no choices.                 |
| `no matching choices · ctrl+l clear filter · Esc back` | No choice matches your filter.              |
| `could not load choices · ctrl+r retry · Esc back`     | The source failed. The title shows the error. |

The key names in the messages follow your key bindings.

### Command results

When the command succeeds, the action picker closes. Hetki does not show the command output.

- From `hetki action`, your shell or tmux popup comes back.
- From inside Hetki, the normal list comes back.

When the command fails, the list shows one row:

```text
> █                                  action command "tmux": exit status...
──────────────────────────────────────────────────────────────────────────
  Run Move pane to another window…
```

The title shows the error, cut to fit. At 80 columns, you usually see only the start of it.
To see more of the error, use a wider terminal or popup.

Press Enter to run the command again, or Escape to go back.
A failed command does not undo its changes. When you run it again, it can make the same changes again.

For other results, limits, and command rules, see the [action reference](./action-reference.md).
