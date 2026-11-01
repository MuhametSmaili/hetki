# Hetki

Navigate tmux sessions and save your setup to use again later.

A keyboard-first tool for Linux and macOS.

- Find sessions, windows, and panes in a searchable picker.
- Preview panes and see activity from supported apps.
- Save tmux sessions as YAML workspaces and start them again.

<img width="2088" height="1248" alt="SCR-20261101-rfyq" src="https://github.com/user-attachments/assets/702112e8-c40b-4b16-94a9-da55fda67733" />


## How Hetki works

Hetki works with your tmux server. Your sessions remain in tmux when you close Hetki.

| Term               | Meaning                                                                            |
| ------------------ | ---------------------------------------------------------------------------------- |
| **Picker**         | The terminal interface for finding and selecting items on the current tmux server. |
| **Workspace**      | A YAML file that describes sessions, windows, panes, and startup commands.         |
| **Status service** | A background process that tracks application activity in tmux panes (OSC 7501).    |
| **Tracked**        | An item with any [current status](docs/status.md).                                 |

A workspace saves session structure, not running process state or terminal contents.
The picker starts the status service automatically.

## Install and open the picker

Install tmux and make sure that it is on your `PATH`.
On macOS, use Ventura 13 or later.
Status tracking supports stable [tmux releases from 3.2 through 3.8](docs/status.md#tmux-versions).

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

1. Run `hetki` in a terminal.
2. Type part of a session, window, or pane name.
3. Use the arrow keys to select an item.
4. Press Enter to switch to it.

Inside tmux, Hetki switches the current client. Outside tmux, it attaches to the selected session.
By default, the picker closes after you switch.

<details>
<summary>Other installation methods</summary>

| Method   | Install command                                        |
| -------- | ------------------------------------------------------ |
| Homebrew | `brew install MuhametSmaili/tap/hetki`                 |
| mise     | `mise use -g go:github.com/MuhametSmaili/hetki@latest` |
| Go       | `go install github.com/MuhametSmaili/hetki@latest`     |

Go and mise installations require Go 1.27.3 or later.
For the latest unreleased main commit with Homebrew, add `--HEAD` to the install command.

You can also download a binary from the [releases page](https://github.com/MuhametSmaili/hetki/releases).

</details>

<details>
<summary>Update Hetki</summary>

Use the tool that you used to install Hetki:

| Installed with | Update with                                                   |
| -------------- | ------------------------------------------------------------- |
| `install.sh`   | Run the install command again.                                |
| Homebrew       | `brew upgrade hetki`                                          |
| Homebrew, HEAD | `brew upgrade --fetch-HEAD hetki`                             |
| Go             | `go install github.com/MuhametSmaili/hetki@latest`            |
| mise           | `mise upgrade go:github.com/MuhametSmaili/hetki`              |
| mise, `@main`  | `mise install --force go:github.com/MuhametSmaili/hetki@main` |

The status service stops when its binary is replaced.
The picker starts the new build the next time it opens.
The picker and `hetki service restart` also replace a service from another build.

</details>

<details>
<summary>Set up shell completion</summary>

### Zsh

Generate the completion file:

```zsh
mkdir -p ~/.local/share/zsh/site-functions
hetki completion zsh > ~/.local/share/zsh/site-functions/_hetki
```

In `~/.zshrc`, add this line before your shell's completion setup:

```zsh
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
```

Most shell frameworks enable Zsh completion.
If yours does not, add these lines after `fpath`:

```zsh
autoload -Uz compinit
compinit
```

### Bash

Use Bash 4.2 or later with `bash-completion` 2.x enabled.
Generate the completion file:

```bash
dir="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
mkdir -p "$dir"
hetki completion bash > "$dir/hetki"
```

If you set `BASH_COMPLETION_USER_DIR`, use one of those directories with a `completions` subdirectory instead.

Restart your shell after setup. Generate the file again after you update Hetki.
These setups load completion functions on demand.
Hetki reads workspace names only when you request completion.

</details>

## Everyday use

### Find and switch

Choose a flat list or a tree of sessions, windows, and panes.
The flat list uses **frecency**: directories you use often or recently rank higher.
Press `Ctrl+T` to switch views.

The picker opens in filter mode. Type to filter the list.
Press Escape to enter normal mode. Press `/` to filter again.

| Key      | Action                                |
| -------- | ------------------------------------- |
| `Enter`  | Open the selection.                   |
| `Ctrl+K` | Open the actions menu.                |
| `Ctrl+T` | Switch between flat and tree views.   |
| `Tab`    | Switch between All and Tracked items. |
| `Alt+P`  | Show or hide the pane preview.        |
| `Ctrl+O` | Switch to the previous session.       |
| `Ctrl+C` | Quit from any mode.                   |

These are the default keys. You can [change keys for each mode](docs/key-bindings.md).

<details>
<summary>Open an inline picker or keep it open after switching</summary>

Open the picker near your shell prompt:

```bash
hetki --height 40% --border none
```

Earlier shell output stays in the main terminal buffer.
The terminal can scroll to make space.
Use `--height 100%` for fullscreen mode, which is the default.

Inside tmux, keep the picker open after you switch:

```bash
hetki --stay-open
```

Your filter, selection, view, and preview stay in memory.
`Ctrl+O` also keeps the picker open. Press `Ctrl+C` to quit.
Hetki does not position the picker pane or save these choices for the next run.

</details>

### Save and start a workspace

From inside tmux, save the current session:

```bash
hetki save work
```

For a new workspace, this creates `~/.config/hetki/workspaces/work.yaml`.
If the workspace already exists, Hetki updates it and keeps other saved sessions.

Preview the changes before you start the workspace:

```bash
hetki start work --dry-run
```

Then start it:

```bash
hetki start work
```

By default, `start` adds missing sessions and windows without changing existing windows.

**Caution:** `--force` and `--prune-sessions` can replace or delete items and stop running processes.
Preview destructive changes with `--dry-run`.

Hetki attaches to the first listed session, or switches to it from inside tmux.
Use `--detach` (`-d`) to skip this step.

See the [workspace guide](docs/workspaces.md) for YAML examples and save behavior.

## Customize the picker

Create `~/.config/hetki/config.yaml` and copy only the options you want to change.
If you use an absolute `XDG_CONFIG_HOME`, put the file at `$XDG_CONFIG_HOME/hetki/config.yaml` instead.

```yaml
ui:
  view: tree
  scope: tracked
  preview: false
  preview_width: 45
  height: "40%"
  border: none
```

Command flags override the file for one run:

```bash
hetki --view tree --scope tracked --preview --preview-width 45
```

Hetki reads the file when the picker opens. It does not write your picker choices to the file.

- [Configuration](docs/configuration.md): defaults, flags, layout, and file rules.
- [Key bindings](docs/key-bindings.md): change keys for each mode or see all defaults.
- [Theme](docs/theme.md): customize picker colors and selection.

## Application status and help

Status shows activity from apps that report it or match a screen profile.
Ordinary terminal output alone does not mean that an app is working.

| Command                 | Action                                                                  |
| ----------------------- | ----------------------------------------------------------------------- |
| `hetki service status`  | Show service information and the log path without starting the service. |
| `hetki service start`   | Start the status service.                                               |
| `hetki service stop`    | Stop the status service, not tmux.                                      |
| `hetki service restart` | Restart the status service.                                             |

An open picker can start a stopped service again.
See the [status guide](docs/status.md) for app support, profiles, and troubleshooting.

Use `hetki --help` or `hetki <command> --help` for command options.
