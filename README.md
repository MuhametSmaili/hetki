# Hetki

A keyboard-first tmux workspace manager for Linux and macOS.

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

| Command                                   | What it does                                          |
| ----------------------------------------- | ----------------------------------------------------- |
| `hetki`                                   | Open the picker.                                      |
| `hetki save -n your_workspace`            | Save the current session from inside tmux.            |
| `hetki start -n your_workspace`           | Start a saved workspace.                              |
| `hetki start -n your_workspace --dry-run` | Preview workspace changes without applying them.      |
| `hetki update`                            | Update to the latest stable release.                  |
| `hetki update --head`                     | Update to the latest pushed main commit. Requires Go. |
| `hetki service status`                    | Check the status service.                             |
| `hetki service start`                     | Start the status service.                             |
| `hetki service stop`                      | Stop the status service, not tmux.                    |
| `hetki service restart`                   | Restart the status service.                           |

Replace `your_workspace` with your workspace name. Named workspace files live in `~/.config/hetki/workspaces` by default.

For an explicit file, use `-p ./path/to/your_workspace.yaml` instead of `-n your_workspace`.

The picker starts its status service automatically. Use `hetki --help` or `hetki <command> --help` for more options.

**Caution:** `--force` and `--prune-sessions` can delete windows or sessions and terminate their processes. Use `--dry-run` first.

## Picker keys

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
