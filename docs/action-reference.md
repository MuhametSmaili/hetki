# Action reference

Start with the [action guide](./actions.md) for a complete example.

## Recipe fields

Store recipes in `~/.config/hetki/actions.yaml`, or `$XDG_CONFIG_HOME/hetki/actions.yaml`.
The file must exist. Hetki reads it when you open the action menu.

| Field        | Purpose                                                                     |
| ------------ | --------------------------------------------------------------------------- |
| `version: 1` | Select the supported file format.                                           |
| `actions`    | Map action IDs to recipes. The menu sorts by ID.                            |
| `label`      | Text shown in the action menu.                                              |
| `scope`      | One or more of `pane`, `window`, `session`.                                 |
| `steps`      | An ordered list of selections, followed by one command.                     |
| `select`     | Name used to store this selection in `steps.NAME`.                          |
| `title`      | Optional heading for a selection screen.                                    |
| `source`     | Where this step gets its choices.                                           |
| `run`        | Required once, as the last step. Contains `argv` and an optional `timeout`. |

Action IDs and selection names must start with a letter.
Use letters, digits, `_`, and `-`; the maximum length is 64 characters.
Selection names must be unique within a recipe.

**Every recipe must end with exactly one `run` step.**
All preceding steps must be selections. A selection-only recipe is invalid.
If there are no selection steps, selecting the action runs the command immediately.

## Sources

Each `source` must contain one of `builtin`, `items`, or `exec`.

### Built-in lists

The built-in names are `tmux.sessions`, `tmux.windows`, and `tmux.panes`.
See the [complete field table](./actions.md#built-in-sources).

To show windows from the subject's session:

```yaml
source:
  builtin: tmux.windows
  session: "${context.subject.session_id}"
```

To show panes from a selected window:

```yaml
source:
  builtin: tmux.panes
  session: "${steps.destination.value.session_id}"
  window: "${steps.destination.value.window_id}"
```

Without filters, Hetki lists choices from the whole server.
Filters use exact IDs, not names.

| Built-in source | `session` filter | `window` filter |
| --------------- | ---------------- | --------------- |
| `tmux.sessions` | Supported        | Rejected        |
| `tmux.windows`  | Supported        | Supported       |
| `tmux.panes`    | Supported        | Supported       |

Hetki rejects a `window` filter on `tmux.sessions` when it loads the recipe.

### Fixed lists: `items`

Put the choices directly in the recipe:

```yaml
- select: layout
  source:
    items:
      - id: horizontal
        label: Side by side
        value: { flag: "-h" }
      - id: vertical
        label: Stacked
        value: { flag: "-v" }
```

The next step can read `${steps.layout.id}` or `${steps.layout.value.flag}`.

Fixed `value` objects must be valid JSON data. Hetki checks this when it loads the recipe.
It rejects values that cannot be encoded as JSON, such as `.inf` and `.nan`.
Integer references preserve signed and unsigned 64-bit values without conversion to floating point.

### Program-generated lists: `exec`

A **provider** is a program that prints choices.
Use any language that can read and write JSON.

| Channel | Direction        | Contents                                                |
| ------- | ---------------- | ------------------------------------------------------- |
| stdin   | Hetki → provider | One JSON request with context and completed selections. |
| stdout  | Provider → Hetki | One JSON item per line.                                 |
| stderr  | Provider → Hetki | Diagnostics if the provider fails.                      |

`source.exec.argv` selects the program and its arguments.
It does not define the returned item values. Your program defines those values.

```yaml
- select: project
  source:
    exec:
      argv: [python3, /absolute/path/to/projects.py]
      timeout: 5s
```

Save this example provider as `projects.py`:

```python
import json
import hashlib
import sys
from pathlib import Path

request = json.load(sys.stdin)
root = Path.home() / "Projects"

for path in sorted(root.iterdir()):
    if path.is_dir():
        print(json.dumps({
            "id": hashlib.sha256(str(path).encode()).hexdigest()[:16],
            "label": path.name,
            "description": str(path),
            "value": {"path": str(path)},
        }))
```

Set `root` to a directory that exists on your machine.

The provider receives the [JSON request](#program-input) on stdin.
It must print **one complete JSON object per line**, not a JSON array:

```text
{"id":"api","label":"API","value":{"path":"/work/api"}}
{"id":"web","label":"Website","value":{"path":"/work/web"}}
```

| Item field    | Required? | Purpose                                         |
| ------------- | --------- | ----------------------------------------------- |
| `id`          | Yes       | Unique ID within this list. Maximum: 256 bytes. |
| `label`       | Yes       | Text shown in the picker.                       |
| `description` | No        | Additional display text.                        |
| `value`       | No        | A JSON object with data for later steps.        |

Use `value` for custom fields. Other top-level item fields are rejected.
Do not print a `source` object, a recipe, a JSON array, or log messages to stdout.

Hetki reads stdout while the program runs, but does not display items as they arrive.
It waits for a successful exit, validates the complete list, then displays it.
The provider must finish within its timeout. Empty stdout produces an empty list.

Hetki validates two things at different times:

| When                        | What Hetki checks                                                                                                |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| When loading YAML           | Exactly one source type per selection, valid command arguments and timeout, and one final `run`.                 |
| After the provider finishes | Valid JSON records, accepted item fields and types, nonempty IDs and labels, unique IDs, and list/record limits. |

If the program fails or any record is invalid, Hetki shows an error and no partial list.
Hetki checks the item format. Your program validates the meaning of custom `value` fields.

Typing filters the loaded list; it does not restart the provider.
Ctrl+R runs the provider again.

Use the selected project in the final command:

```yaml
- run:
    argv:
      - tmux
      - -S
      - "${context.server.socket}"
      - new-window
      - -t
      - "${context.subject.session_id}:"
      - -c
      - "${steps.project.value.path}"
```

### Pass parameters to your provider

`value` holds item data. `argv` passes arguments to a program.
They are separate fields.

Built-in sources accept the filters listed above.
Omit a filter to include all matching objects on the current server.

For a custom source, use `exec`. Define its parameters in your program:

```yaml
source:
  exec:
    argv:
      - python3
      - /absolute/path/to/list-branches.py
      - --repo
      - "${steps.project.value.path}"
```

This example needs an earlier `select: project` step whose items contain `value.path`.
If you select `/work/api`, the program receives `--repo` and `/work/api` as two arguments.

Read the parameter in `list-branches.py`:

```python
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--repo", required=True)
args = parser.parse_args()

repo = args.repo  # /work/api
```

Your program decides which parameters are required and what omitted parameters mean.
Hetki does not define them for you.
Custom parameter names belong in `exec.argv`, not beside `source.exec`.
You cannot register a new `builtin` name through YAML.

The program also receives context and completed selections through JSON stdin.
Use arguments for explicit parameters, or read the selected data directly from that JSON.

### Program input

Providers and final commands receive the same JSON format on stdin:

```json
{
  "version": 1,
  "context": {
    "server": { "socket": "/tmp/tmux-501/default" },
    "subject": {
      "kind": "pane",
      "session_id": "$3",
      "window_id": "@8",
      "pane_id": "%39"
    },
    "invocation": { "session_id": "$3", "pane_id": "%7" }
  },
  "steps": {
    "project": {
      "id": "api",
      "label": "API",
      "value": { "path": "/work/api" }
    }
  }
}
```

Only completed selections appear in `steps`.
Before the first selection, `steps` is `{}`.
Selecting an item stores its whole object in memory under the name from `select`.
The next program receives that object through JSON stdin; no extra save instruction is needed.
When you go back, Hetki removes the selections after that point.

Read values in Python with:

```python
original_pane = request["context"]["subject"]["pane_id"]
project_path = request["steps"]["project"]["value"]["path"]
```

`invocation` is absent outside tmux.
The subject can differ from the invocation when you act on another pane.
For a bare pane/window ID with multiple session links, `subject.session_id` is absent.

## References and commands

Hetki replaces `${...}` references in command arguments and source filters.
It does not replace references in titles, labels, or fixed item values.

| Reference                       | Value                                              |
| ------------------------------- | -------------------------------------------------- |
| `${context.server.socket}`      | tmux server socket path.                           |
| `${context.subject.kind}`       | `pane`, `window`, or `session`.                    |
| `${context.subject.pane_id}`    | Original pane ID, if present.                      |
| `${context.subject.window_id}`  | Original window ID, if present.                    |
| `${context.subject.session_id}` | Original session ID, if present.                   |
| `${steps.NAME.id}`              | Selected item ID.                                  |
| `${steps.NAME.label}`           | Selected item label.                               |
| `${steps.NAME.value.FIELD}`     | Selected data; nested object fields are supported. |

Other request fields, including `invocation` and `description`, are available through JSON stdin only.
Missing fields, `null`, objects, and arrays cannot be used as argument references.
Empty strings, `false`, and `0` are valid values.

`argv` is a list: first the program, then each argument.
Spaces inside one argument stay inside that argument.
Hetki does not expand `~`, shell variables, or wildcards.

For shell features, call a shell explicitly. Pass selected values as separate arguments:

```yaml
run:
  argv:
    - sh
    - -c
    - 'printf "%s\n" "$1" | my-command'
    - action
    - "${steps.project.value.path}"
```

The final `run` receives JSON on stdin, including all completed selections.
Its stdout is captured; it does not need to contain JSON and does not create another list.
Hetki waits for the command to finish. Exit code `0` completes the command.
With complete output and successful cleanup, the flow closes immediately.
If output capture or cleanup fails after exit code `0`, Hetki shows a diagnostic
and a **Close completed action** item. Enter or Escape closes the flow.
Refresh keeps this screen; it does not execute the command again.

For example, a background child can keep an output pipe open after its parent exits.
Hetki waits up to 200 ms for these pipes, then stops capture and the owned process group.
This is an incomplete-output error for a provider. For a final command that exited
with code `0`, it is a completion diagnostic, not an invitation to retry.

A command that fails to start, exits nonzero, or is killed can be retried explicitly.
Failure does not undo earlier changes. Retry can repeat them.
Commands cannot take over the picker's terminal for an interactive editor or shell.
Use `tmux new-window` or `tmux split-window` to open an interactive program elsewhere.

## Entry points

Use an exact tmux ID with one subject flag:

```bash
hetki action --pane-id '%39'
hetki action --window '@8'
hetki action --session '$3'
```

Optional flags: `--file`, `--height`, and `--border`.
The current tmux environment selects the server; outside tmux, Hetki uses the default server.

In Hetki's `Ctrl+K` menu, tree session/window rows use their respective scopes.
Pane children and flat rows use pane scope.
Use flat view to act on the pane in a compact, single-pane window row.

## Limits and failure behavior

| Setting                                   | Limit                                                                  |
| ----------------------------------------- | ---------------------------------------------------------------------- |
| Recipe file                               | 1 MiB; one YAML document; no aliases or unknown fields.                |
| Actions                                   | 128 per file.                                                          |
| Steps                                     | 32 per action, including the final command.                            |
| Arguments                                 | 128 per command.                                                       |
| Expanded argument or native source filter | 64 KiB each.                                                           |
| Expanded arguments combined               | 256 KiB per command, including the executable name.                    |
| Timeout                                   | Default: 30 seconds. Set `timeout` to a positive duration up to `10m`. |
| Provider stdout                           | 4 MiB total; 10,000 items; records below 64 KiB.                       |
| Command stderr                            | 8 KiB.                                                                 |
| Final command stdout / JSON stdin         | 4 MiB each.                                                            |

Empty lists stay open. Failed providers do not expose partial lists.
Expansion limits count UTF-8 bytes, including literal text.
Hetki checks the budget before it appends each part of an expanded value.
The operating system can impose additional command-size limits.
Invalid recipes produce an error; Hetki does not substitute default actions.
Owned command process groups are stopped on cancellation or command exit.

Before the final command, Hetki checks the tmux server, original subject, and built-in selections again.
Your program must validate its own custom values.
External changes can still occur between the check and the command.
Linked windows share their panes; changing one affects every link.

Version 1 supports ordered selection steps and one final command.
It does not support text-input steps, conditional branches, or interactive terminal handoff.
