# Hetki

A keyboard-first tmux workspace manager for Linux and macOS.

> **Alpha:** expect breaking changes. Hetki currently supports tmux only, and its behavior, appearance, and keybindings are not configurable yet.

<img width="867" height="564" alt="image" src="https://github.com/user-attachments/assets/6902d842-42b4-4ec0-9c06-7867c5c0a649" />

## What it does

- Browse and switch between tmux sessions, windows, and individual panes in an interactive TUI.
- Save the current session, or all sessions, as a strict YAML workspace.
- Start and reconcile saved workspaces.

## Installation

Hetki requires tmux and, on macOS, Ventura 13 or later.

### Using Go

```bash
go install github.com/MSmaili/hetki@latest
```

### Using curl

```bash
curl -fsSL https://raw.githubusercontent.com/MSmaili/hetki/main/install.sh | bash
```

## Updating

```bash
hetki update          # Latest stable release
hetki update --head   # Latest pushed commit on main (requires Go; unreleased code)
```

`--head` pins and verifies the current `main` commit before building. Repeat it to
get newer pushed commits; local unpushed changes are not included.
Release updates exclude prereleases. Use `--version vX.Y.Z` to return to a specific
stable release.

## Quick start

```bash
hetki                 # Open the TUI
hetki save -n work    # Save the currently attached tmux session
hetki start work      # Start or reconcile the saved workspace
hetki switch dev      # Switch directly to a session
```

Run `hetki --help` or `hetki <command> --help` for the current CLI options.

The default flat list shows one row per pane, with its directory in the middle
and foreground command aligned at the far right. Pane IDs appear only to
distinguish otherwise identical rows. `Tab` toggles the session/window tree.
Same-directory panes stay distinct; Enter opens the selected pane, while
rename/delete still act on its window. Names are snapshot observations,
updated on refresh—not live status.
On macOS, single-process Node jobs can expose a short name through
argv-zero/process title; uncertain or oversized process data keeps the raw
command. Linux uses tmux's own argv-zero-based command observation without
additional process scanning. There is no application catalogue or package-path
matching; ordinary Node jobs and failed lookups keep the raw command.

## Safety

`hetki start --force` reconciles windows only inside sessions declared by the selected workspace; it never removes unrelated sessions. Saves use atomic replacement and refuse to overwrite a destination that changed while it was being saved.
