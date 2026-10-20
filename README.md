# Hetki

A keyboard-first tmux workspace manager for Linux and macOS.

> **Alpha:** expect breaking changes. Hetki supports tmux only. Picker appearance and keybindings are not configurable yet.

<img width="867" height="564" alt="image" src="https://github.com/user-attachments/assets/6902d842-42b4-4ec0-9c06-7867c5c0a649" />

## What it does

- Browse tmux sessions, windows, and individual panes in flat or tree views.
- Preview the selected pane and jump to the previous session.
- Show pane status from supported terminal reports and optional screen profiles.
- Save sessions as strict YAML, with pane zoom, layout presets, and safely matched startup commands.
- Start saved workspaces or preview changes with `--dry-run`.

## Installation

Hetki requires tmux and, on macOS, Ventura 13 or later. Native status supports explicitly recognized stable tmux releases from 3.2 through 3.8.

Prebuilt binaries for Linux and macOS on amd64 and arm64 are available on the [releases page](https://github.com/MuhametSmaili/hetki/releases), with checksums and build attestations.

### Using Go

Source builds require Go 1.27.2 or newer.

```bash
go install github.com/MuhametSmaili/hetki@latest
```

### Using curl

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

## Updating

```bash
hetki update          # Latest stable release
hetki update --head   # Latest pushed commit on main (requires Go; unreleased code)
```

`--head` pins and verifies the latest pushed `main` commit before building. It does not include local unpushed changes. It requires Go and cannot combine with `--version`.

Release updates exclude prereleases. Use `hetki update --version v0.4.0` to select that exact stable release, including a downgrade or reinstall.

The built-in updater supports Go-bin installations and binaries in `~/.local/bin` or `~/bin`.

For mise, `--head` also supports an active, non-linked `go:github.com/MuhametSmaili/hetki@main` installation. It updates that binary in place and leaves mise configuration unchanged. The previous binary remains available for rollback until the new binary passes verification. Mise discovery requires `mise` on `PATH`.

Other mise selections require an explicit switch to `main` through mise first. Use mise for release updates and linked installations.

## Quick start

Replace `your_workspace` and `./path/to/your_workspace.yaml` with your workspace name and YAML file path.

```bash
hetki                            # Open the picker
hetki save -n your_workspace     # Save the current session from inside tmux
hetki start -n your_workspace    # Start or reconcile the saved workspace
```

Both `start` and `save` require exactly one selector:

- `--name` (`-n`) selects a file in `~/.config/hetki/workspaces`.
- `--path` (`-p`) selects an explicit `.yaml` or `.yml` file, not a directory.

```bash
hetki save -p ./path/to/your_workspace.yaml --all
hetki start -p ./path/to/your_workspace.yaml --dry-run
hetki start -p ./path/to/your_workspace.yaml
```

A workspace name selects a file, not session ownership. Session names come from YAML. Hetki does not automatically discover a local workspace.

Without `--all`, save updates the current session and retains other saved sessions. With `--all`, save replaces the file's session set with all live sessions.

Window paths resolve against the session root. Pane paths resolve against the window directory. Omitted paths inherit their parent directory. Without a session root, the base is the caller's directory, not the YAML file's directory.

Run `hetki --help` or `hetki <command> --help` for CLI options.

### Picker shortcuts

The picker starts in filter mode. Press `Esc` for normal mode.

| Key                      | Action                                   |
| ------------------------ | ---------------------------------------- |
| `Ctrl+T`                 | Toggle flat/tree view                    |
| `Tab`                    | Toggle All/Tracked scope                 |
| `Alt+P`                  | Toggle pane preview                      |
| `Ctrl+O`                 | Jump to the previous session, marked `↶` |
| `Enter`                  | Open the selected destination            |
| `;` in normal mode       | Enter jump mode                          |
| `Ctrl+R` in normal mode  | Refresh                                  |
| `A` / `a` in normal mode | Create a session/window                  |
| `R` / `r` in normal mode | Rename a session/window                  |
| `X` / `x` in normal mode | Delete a session/window                  |

Flat rows show individual panes, including panes that share a directory. Tree rows group windows by session and show the command, path, and status from the same pane: the highest-priority status wins, ties keep the previous pane, and idle windows use the active pane. Redundant default-shell labels are hidden.

Command names are snapshots, updated on refresh. Previews capture the visible screen, without scrollback. They are separate from pane status.

On macOS, single-process Node jobs can expose a short process title. Uncertain or oversized readings retain the raw command. Linux uses tmux's command observation without additional process scans. There is no application catalogue.

### Pane status

Hetki starts a shared status daemon for each tmux server. Supported application reports and optional screen profiles supply status. OSC133 tracks shell lifecycle, not generic command completion.

Indicators distinguish working, needs-input, idle, unknown, success, and failure where evidence supports them. Tracked scope shows status-tracked panes, not workspace-owned sessions.

The daemon attaches one tmux control client per session, which never resizes your windows. These clients appear as `hetki-status-*` in `tmux list-clients`. tmux forwards each pane's raw output to it. Hetki scans that output only for OSC 7501 and OSC 133 sequences, and stores or logs none of it. Screen profiles read the visible screen with `capture-pane`, apply their rules, and discard the text. This gives the daemon no more access than any process running as your user already has through tmux.

Optional YAML profiles live in `$XDG_CONFIG_HOME/hetki/status.d`, or `~/.config/hetki/status.d` by default. Profiles load at daemon startup and require a daemon restart after changes.

The [Kiro 2.28 profile](integrations/pane-status/profiles/kiro-2.28.yaml) provides a screen-based example. It targets the documented English UI and does not infer completion.

### Status service

The picker starts the status daemon automatically. These commands let you manage it without opening the picker:

| Command                 | What it does                                             |
| ----------------------- | -------------------------------------------------------- |
| `hetki service status`  | Shows the daemon's status. Does not start it.            |
| `hetki service start`   | Starts the daemon. Reuses an already-running daemon.     |
| `hetki service stop`    | Stops the daemon. Leaves tmux and your sessions running. |
| `hetki service restart` | Stops, then starts the daemon. Starts it if not running. |

Inside tmux, commands affect the current server. Outside tmux, they affect the default server. That server must already exist.

The daemon keeps running after the command exits. When its tmux server exits, the daemon stops.

Stop is not a permanent disable. Running `hetki service start` or opening the picker starts it again. An already-open picker can also restart it automatically.

If you start during shutdown, Hetki waits briefly for the old daemon to finish. If the wait times out, the command reports an error.

Restart checks profiles before stopping, so invalid profiles leave the current daemon running. If another picker or command starts a compatible replacement, restart reuses it. A failure after stopping can leave the service stopped.

Older daemons can reject `stop` and `restart`. Hetki does not force-kill them.

## Safety

Normal start adds missing resources. `--force` also replaces mismatched windows and removes extra windows inside declared sessions. It can terminate their processes. Earlier actions remain applied after a failure.

> **Caution:** With linked windows, `--force` can destroy windows and sessions outside the workspace. See [#12](https://github.com/MuhametSmaili/hetki/issues/12).

Saves use atomic replacement and reject a destination that changed during the save. Existing authored commands survive only when their live-pane matches are unambiguous. Hetki does not reconstruct commands from running processes.

Startup commands run when Hetki creates resources. Matching starts do not replay them. Command submission does not guarantee command success.

Saved layout presets approximate custom geometry. Changes to named presets alone are not yet enforced on existing windows.

Upstream tmux 3.4 can crash when deleting zoomed windows. Use a patched tmux version. See [tmux/tmux#3857](https://github.com/tmux/tmux/issues/3857).

### Upgrading from v0.3.0

Replace the example workspace name and YAML file path with your own.

- Replace `hetki start your_workspace` with `hetki start -n your_workspace`.
- Replace local discovery and `hetki save .` with an explicit file selector, such as `-p ./path/to/your_workspace.yaml`.
- Use the picker instead of the removed `hetki switch` command.
- Remove `update --source` and `--pre`. `--head` selects unreleased main code, not a stable source release.
- Use the Go module path `github.com/MuhametSmaili/hetki`.

See the [v0.4.0 release notes](https://github.com/MuhametSmaili/hetki/releases/tag/v0.4.0) for the full changelog and migration details.
