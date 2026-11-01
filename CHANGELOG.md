# Changelog

## [v0.6.0] - 2026-11-01

### Before you upgrade

Replace `--name` with a workspace name. Replace `--path` (`-p`) with `--file` (`-f`).

```sh
hetki start work
hetki save work
hetki start --file ./work.yaml --dry-run
```

For `start`, `-n` means `--dry-run`. Use `-d` for `--detach`. Use the full `--force` flag.

Replace `save --all` with `save --all-sessions` (`-a`). Use `-V`, not `-v`, to print the version.

Close old pickers before you upgrade. Then upgrade and run `hetki service restart`. The status protocol has changed.

Old usage history is ignored, not deleted.

### Added

- Set picker options, keys, and colors in `~/.config/hetki/config.yaml`. See [configuration](docs/configuration.md).
- Choose the first picker mode with `--mode normal` or `--mode filter`.
- `--height` opens the picker near the shell prompt. `--stay-open` keeps it open inside tmux.
- Use `hetki start --detach` to start a workspace without attaching.
- New guides. MIT license.

### Changed

- Each visit loses half its ranking weight every seven days.
- Switching to tree view expands all branches.

### Removed

- `hetki update`. Update with your original installation method.
- Jump mode. Use filter mode or normal mode.
- OSC 133 tracking. Use OSC 7501 support or a screen profile.

### Fixed

- Protect linked windows during workspace changes. Fixes [#12](https://github.com/MuhametSmaili/hetki/issues/12).
- Switch the correct tmux client. Keep the selected window or pane when tmux indexes change.
- Show, preview, and open the same pane in tree rows.
- Recover status after lost connections. Clear status when its process is no longer active.
- Keep other captures when one pane closes or its capture is too large.

### Security

- Optional attestation checks need an authenticated `gh` 2.102.0 or newer. Otherwise, the installer checks checksums and prints a warning.

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.5.0...v0.6.0)

## [v0.5.0] - 2026-10-22

### Before you upgrade

The v0.4.0 status service is incompatible with this release. Replacing the binary does not stop it.

1. Close all old Hetki pickers.
2. Identify and stop only the old Hetki status-daemon process for each tmux server. Keep tmux and its sessions running.
3. Open the new picker to start the new service.

`hetki service stop` cannot stop the incompatible v0.4.0 service.
Source builds require Go 1.27.3 or newer. Use mise to update mise-managed stable installations.

### Added

- Use `hetki service status`, `start`, `stop`, and `restart` to control the status service. Stopping it leaves tmux sessions running.
- Use `--prune-sessions` to delete sessions absent from the selected workspace. This can delete unrelated sessions and their running processes.
- The updater accepts `GH_TOKEN` or `GITHUB_TOKEN` for GitHub API requests.
- `hetki update --head` supports active, non-linked mise installations of `go:github.com/MuhametSmaili/hetki@main`.

Check session deletions first. Replace `your_workspace` with your workspace name:

```sh
hetki start -n your_workspace --prune-sessions --dry-run
```

Remove `--dry-run` to apply the changes. Hetki cannot undo them.

### Changed

- The picker uses two-line rows in narrow terminals.
- Status updates use fewer tmux requests.

<img width="1986" height="1146" alt="Hetki picker with narrow two-line rows" src="https://github.com/user-attachments/assets/561d2623-9ca8-4b20-9fef-7bbbf4d0827f" />

### Fixed

- Tree rows show the command, path, and status from the same pane.
- The picker keeps your selection when search results refresh.
- Closed status connections no longer prevent new connections.
- Startup commands no longer repeat when Hetki cannot read a pane's directory.

**Known issue:** `--force` can delete linked windows in other sessions. See [#12](https://github.com/MuhametSmaili/hetki/issues/12).

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.4.0...v0.5.0)

## [v0.4.0] - 2026-10-17

### Before you upgrade

`start` and `save` require either `-n` for a workspace name or `-p` for a YAML file:

```sh
hetki start -n work
hetki save -n work
hetki start -p ./.hetki.yaml
hetki save -p ./.hetki.yaml
```

Replace `hetki start work` with `hetki start -n work`. Automatic local-file discovery is removed.

Relative window paths use the session root. Relative pane paths use the window directory. Explicit session roots must resolve to existing absolute directories.

The picker starts in filter mode. Press `Esc` for normal mode. Use `A` to create a session and `Ctrl+R` to refresh.
Use `R`/`X` to rename/delete sessions and `r`/`x` for windows.

The Go module path has changed. Source builds require Go 1.27.2 or newer:

```sh
go install github.com/MuhametSmaili/hetki@v0.4.0
```

Use mise to update mise-managed installations.

### Added

- Find and select individual panes in the picker.
- Press `Ctrl+T` for flat or tree views. Press `Tab` for all panes or only status-tracked panes.
- Press `Alt+P` for a pane preview. Press `Ctrl+O` to return to the previous session.
- A shared service collects pane status. Status requires a supported app, shell integration, or a configured screen profile.
- Use `hetki update --head` to build the latest pushed `main` commit. This requires Go.

### Changed

- Saving keeps existing startup commands when Hetki can match the panes. It also saves pane zoom and layout presets.
- `hetki start --force` creates replacement windows before deleting old windows.

### Removed

- The `hetki switch` command. Use the picker instead.
- The updater's `--source` and `--pre` flags.

**Known issue:** `--force` can delete linked windows in other sessions. Hetki cannot undo partial changes. See [#12](https://github.com/MuhametSmaili/hetki/issues/12).

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.3.0...v0.4.0)

## [v0.3.0] - 2026-09-04

### Before you upgrade

- `hetki` now opens the picker. Use subcommands for other actions.
- The `list` command is removed. Use the picker instead.
- Workspace YAML validation is stricter. Correct any errors that Hetki reports.

This is an alpha release. Do not use it for critical work.

### Added

- Find workspaces, sessions, and windows by name or path in the new interactive picker.
- Frequently used items appear first.
- Use jump labels to select items, or open a row's actions menu.

<img width="940" height="511" alt="Hetki interactive picker" src="https://github.com/user-attachments/assets/0982704c-95db-49d9-9d52-76f9abae84f2" />

### Fixed

- Saving stops if another process has changed the workspace file. The newer file stays intact.

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.2.0...v0.3.0)

## [v0.2.0] - 2026-02-08

### Before you upgrade

- The command is now `hetki`, previously `muxie`. Update scripts and shortcuts.
- Rename local workspace files from `.muxie.yaml` to `.hetki.yaml`. Keep the extension if you use `.yml` or `.json`.
- Workspace `sessions` now uses a list. Each session needs a `name` and `windows`, with an optional `root` directory.

### Added

- Use `hetki switch` to select a session, window, or pane.
- Use `~` in paths for your home directory.
- Use the verbose flag for more detailed logs.

### Changed

- Errors no longer print the full command usage.
- Starting a workspace inside tmux switches the current client to the target session.

### Fixed

- Start a workspace when tmux is not running.
- Allow windows with duplicate names.
- Save the current session, or all sessions with `--all`, into an existing workspace.

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.1.2...v0.2.0)

## [v0.1.2] - 2026-01-18

### Before you upgrade

The updater now requires a Go installation. Updates for script-installed binaries are no longer supported in this version.

### Added

- Use `muxie list sessions --marker` to mark the current session or window. Supply the marker text after the flag.
- Use `muxie update --dry-run` to show the update command without running it.

### Security

- The updater no longer downloads and runs an installation script. It uses `go install` instead.

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.1.1...v0.1.2)

## [v0.1.1] - 2026-01-18

### Before you upgrade

- The command is now `muxie`, previously `tms`. Update scripts and shortcuts.
- Rename local workspace files from `.tms.yaml` to `.muxie.yaml`. Keep the extension if you use `.yml` or `.json`.

### Added

- Use `muxie update --source` to update through Go.

[All changes](https://github.com/MuhametSmaili/hetki/compare/v0.1.0...v0.1.1)

## [v0.1.0] - 2026-01-18

First release, under the command name `tms`.

### Added

- Start tmux sessions, windows, and panes from YAML or JSON workspace files.
- Save running sessions as workspaces.
- List workspaces and running sessions.
- Set window layouts, pane zoom, working directories, and startup commands.
- Use `--dry-run` to inspect a startup plan and `--force` to replace existing windows.
- Complete workspace names in the shell.

[All changes](https://github.com/MuhametSmaili/hetki/commits/v0.1.0)

[v0.6.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.6.0
[v0.5.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.5.0
[v0.4.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.4.0
[v0.3.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.3.0
[v0.2.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.2.0
[v0.1.2]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.1.2
[v0.1.1]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.1.1
[v0.1.0]: https://github.com/MuhametSmaili/hetki/releases/tag/v0.1.0
