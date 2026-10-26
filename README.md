# Hetki

Navigate tmux sessions and save your setup to use again later.

A keyboard-first tool for Linux and macOS.

- Find sessions, windows, and panes in a searchable picker.
- Preview panes and see activity from supported apps.
- Save tmux sessions as YAML workspaces and start them again.

> **Alpha:** expect breaking changes.

<img width="867" height="564" alt="image" src="https://github.com/user-attachments/assets/6902d842-42b4-4ec0-9c06-7867c5c0a649" />

## Install

Requires tmux. On macOS, requires Ventura 13 or later.

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

Or use a package manager:

| Method   | Install command                                        |
| -------- | ------------------------------------------------------ |
| Homebrew | `brew install MuhametSmaili/tap/hetki`                 |
| mise     | `mise use -g go:github.com/MuhametSmaili/hetki@latest` |
| Go       | `go install github.com/MuhametSmaili/hetki@latest`     |

Go and mise installations require Go 1.27.3 or newer.
For the latest unreleased main commit with Homebrew, add `--HEAD` to its install command.

Prebuilt binaries are available on the [releases page](https://github.com/MuhametSmaili/hetki/releases).

### Update

Update Hetki with the tool you installed it with:

| Installed with | Update with                                                   |
| -------------- | ------------------------------------------------------------- |
| `install.sh`   | Run the install command again.                                |
| Homebrew       | `brew upgrade hetki`                                          |
| Homebrew, HEAD | `brew upgrade --fetch-HEAD hetki`                             |
| Go             | `go install github.com/MuhametSmaili/hetki@latest`            |
| mise           | `mise upgrade go:github.com/MuhametSmaili/hetki`              |
| mise, `@main`  | `mise install --force go:github.com/MuhametSmaili/hetki@main` |

The status service stops when its binary is replaced. The picker starts the new build the next time it opens.
If a service from another build still runs, the picker and `hetki service restart` replace it.

<details>
<summary>Load completion for Hetki</summary>

### Zsh

Generate the completion file once:

```zsh
mkdir -p ~/.local/share/zsh/site-functions
hetki completion zsh > ~/.local/share/zsh/site-functions/_hetki
```

In `~/.zshrc`, add this line before your shell's completion setup:

```zsh
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
```

Zsh's completion system must be enabled. Most shell frameworks handle this automatically.
Otherwise, add these lines after `fpath`:

```zsh
autoload -Uz compinit
compinit
```

### Bash

Requires Bash 4.2 or newer with `bash-completion` 2.x enabled. Generate its user-local completion file once:

```bash
dir="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
mkdir -p "$dir"
hetki completion bash > "$dir/hetki"
```

If you set `BASH_COMPLETION_USER_DIR`, use one of those directories with a `completions` subdirectory instead.

Restart your shell after setup. Regenerate the completion file after upgrading Hetki.
These setups load completion functions on demand. Hetki reads workspace names only during completion.

</details>

## Commands

| Command                          | What it does                                          |
| -------------------------------- | ----------------------------------------------------- |
| `hetki`                          | Open the picker.                                      |
| `hetki save work`                | Save the current session from inside tmux.            |
| `hetki start work`               | Start a saved workspace.                              |
| `hetki start work --detach`      | Start without attaching or switching sessions.        |
| `hetki start work --dry-run`     | Preview workspace changes without applying them.      |
| `hetki start --file ./work.yaml` | Start a workspace from a YAML file.                   |
| `hetki save work --all-sessions` | Replace the saved session set with all live sessions. |
| `hetki service status`           | Show status service information.                      |
| `hetki service start`            | Start the status service.                             |
| `hetki service stop`             | Stop the status service, not tmux.                    |
| `hetki service restart`          | Restart the status service.                           |

Named workspace files live in `~/.config/hetki/workspaces` by default.
Use exactly one selector: a workspace name such as `work`, or `--file ./work.yaml` (`-f ./work.yaml`).
Names never select local files. A name combined with `--file` is an error.

The picker starts its status service automatically. Use `hetki --help` or `hetki <command> --help` for more options.

**Caution:** `--force` can replace or delete windows in sessions listed in the workspace.
`--prune-sessions` can delete unrelated sessions on the selected tmux server.
Both can terminate processes. Use `--dry-run` first to see result.

## Configuration

The picker reads an optional `$XDG_CONFIG_HOME/hetki/config.yaml` on Linux and macOS.
If `XDG_CONFIG_HOME` is unset or relative, it uses `~/.config/hetki/config.yaml` instead.

```yaml
ui:
  view: tree
  scope: tracked
  preview: false
  preview_width: 45
  height: "40%"
  border: none
```

| Key                | Valid values                         | Default   |
| ------------------ | ------------------------------------ | --------- |
| `ui.view`          | `flat`, `tree`                       | `flat`    |
| `ui.scope`         | `all`, `tracked`                     | `all`     |
| `ui.preview`       | `false`, `true`                      | `false`   |
| `ui.preview_width` | Integer from 1–99                    | `70`      |
| `ui.height`        | Positive integer rows or `1%`–`100%` | `100%`    |
| `ui.border`        | `rounded`, `none`                    | `rounded` |

Omit keys to use their defaults. A missing or empty valid file uses defaults without a warning.
The picker reads the file once when it opens. Runtime changes remain temporary: Hetki does not create or update this file or save picker choices.

`ui.preview_width` sets the preview percentage of the available content width, excluding the frame and separator.
With `preview: false`, the preview reserves no space. In this example, `Alt+P` shows the preview at 45%.
The layout clamps both columns to their minimum widths. A narrow terminal hides the preview without changing the requested visibility or percentage.

An invalid view, scope, border or height choice uses the default for that field and keeps other valid choices.
An integer outside 1–99, including an integer too large to decode, warns and uses width 70 only. Other valid choices remain active.
`ui.preview` requires an unquoted YAML boolean (`true` or `false`).
`ui.preview_width` requires an unquoted YAML integer, not a quoted number, percentage string, boolean, float, or null.
A wrong value type discards the entire configuration.
Malformed YAML or an invalid file structure discards the entire configuration and uses built-in defaults.
An unreadable or nonregular file, or a configuration-path error, also uses built-in defaults.
These problems show a warning in the picker instead of blocking startup. Other commands and help are unaffected.

The format is strict: one YAML document, at most 64 KiB and 16 levels deep. Symlinks to regular files are supported.
Unknown or duplicate keys, nulls, aliases, merge keys, explicit tags, and wrong value types are invalid.

### Inline picker and command overrides

The inline picker opens near the shell prompt without a separate full-screen buffer.
Earlier shell output stays in the main buffer, but the terminal can scroll to make space.
The height includes the header and outer border. Inline mode reserves one terminal row when possible.
Percentages use the entire terminal height, not the empty space below the cursor. Hetki rounds down to whole rows, with at least one row.
Percentages recompute when the terminal size changes. The renderer scrolls to make room instead of switching above or below the prompt.
Very short pickers hide header rows to keep the selected item visible.
The `none` border option removes the outer frame, not dialog borders.

Explicit command flags override the configuration for one invocation. Omitted flags leave file preferences unchanged.
The order is built-in defaults, then configuration, then explicit flags. Hetki does not write these choices to the configuration file.

```bash
hetki --height 12 --border none
hetki --height 40%
hetki --height 100%
hetki --border rounded
hetki --preview
hetki --preview=false
hetki --scope tracked
hetki --scope all
hetki --view tree --scope tracked --preview
hetki --preview --preview-width 45
```

`--preview` (or `--preview=true`) initially shows the pane preview. Use `--preview=false` to initially hide it, even when `ui.preview` is `true`.
Omit the flag to use the configured value or built-in default. `Alt+P` can still toggle the preview during the run.
The flag does not change `ui.preview_width` or force the preview to fit a narrow terminal.

`--scope all` selects all items regardless of status. `--scope tracked` selects items with any current status, including idle items and non-AI commands.
The flag overrides `ui.scope` for the initial item set. The text query matches within that set, and `Tab` still switches between All and Tracked during the run.
This is not a working-only filter or a text query expression.

`--view flat` or `--view tree` overrides `ui.view` for the initial projection. `Ctrl+T` can still switch views during the run.
`--preview-width N` overrides `ui.preview_width`. Use an integer percentage from 1–99 without a `%` suffix.
Setting the width does not enable the preview. Existing column minimums and narrow-terminal hiding still apply.

`100%` selects fullscreen mode in a separate screen buffer. It is also the built-in default.
Other percentages and positive row counts select inline mode. `100` rows is inline, not fullscreen.
The file accepts an integer or string for `ui.height`, such as `12` or `"40%"`.
Decimal rows and integer percentages are supported. Fractional percentages, negative heights and adaptive `~` heights are not supported.
Invalid explicit flags stop startup with an error.
These flags apply only to the picker, not other commands.

Inline mode does not add shell keybindings or preserve an unfinished shell command.
A tmux popup binding still opens a popup. Run Hetki inside the pane to use inline mode there.

## Picker default key-bindings

Type to filter. Press `Esc` for normal mode.

| Key      | What it does                                 |
| -------- | -------------------------------------------- |
| `Enter`  | Open the selection.                          |
| `Ctrl+K` | Open the actions menu.                       |
| `Ctrl+T` | Switch between flat and tree views.          |
| `Tab`    | Show all items or only status-tracked items. |
| `Alt+P`  | Toggle the pane preview.                     |
| `Ctrl+O` | Jump to the previous session.                |
| `Ctrl+C` | Quit.                                        |
