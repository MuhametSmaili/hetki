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
