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

Or install with Go 1.27.3 or newer:

```bash
go install github.com/MuhametSmaili/hetki@latest
```

Prebuilt binaries are available on the [releases page](https://github.com/MuhametSmaili/hetki/releases).

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
| `hetki update`                   | Update to the latest stable release.                  |
| `hetki update --head`            | Update to the latest pushed main commit. Requires Go. |
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
```

| Key                | Valid values      | Default |
| ------------------ | ----------------- | ------- |
| `ui.view`          | `flat`, `tree`    | `flat`  |
| `ui.scope`         | `all`, `tracked`  | `all`   |
| `ui.preview`       | `false`, `true`   | `false` |
| `ui.preview_width` | Integer from 1–99 | `70`    |

Omit keys to use their defaults. A missing or empty valid file uses defaults without a warning.
The picker reads the file once when it opens. Runtime changes remain temporary: Hetki does not create or update this file or save picker choices.

`ui.preview_width` sets the preview percentage of the available content width, excluding the frame and separator.
With `preview: false`, the preview reserves no space. In this example, `Alt+P` shows the preview at 45%.
The layout clamps both columns to their minimum widths. A narrow terminal hides the preview without changing the requested visibility or percentage.

An invalid `ui.view` or `ui.scope` string choice uses the default for that field and keeps other valid choices.
An integer outside 1–99, including an integer too large to decode, warns and uses width 70 only. Other valid choices remain active.
`ui.preview` requires an unquoted YAML boolean (`true` or `false`).
`ui.preview_width` requires an unquoted YAML integer, not a quoted number, percentage string, boolean, float, or null.
A wrong value type discards the entire configuration.
Malformed YAML or an invalid file structure discards the entire configuration and uses built-in defaults.
An unreadable or nonregular file, or a configuration-path error, also uses built-in defaults.
These problems show a warning in the picker instead of blocking startup. Other commands and help are unaffected.

The format is strict: one YAML document, at most 64 KiB and 16 levels deep. Symlinks to regular files are supported.
Unknown or duplicate keys, nulls, aliases, merge keys, explicit tags, and wrong value types are invalid.

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
