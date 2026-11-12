# Save and start workspaces

A workspace is a YAML file that describes tmux sessions, windows, panes, and startup commands.
It does not save running process state, terminal contents, or shell history.

## Save your current setup

From inside tmux, run:

```bash
hetki save work
```

For a new workspace, this creates `work.yaml` in your [workspace directory](#choose-a-workspace).
Hetki saves the current session. If that session is already in the file, it replaces its saved structure.
Other saved sessions remain in the file.

To save all live sessions on the selected tmux server:

```bash
hetki save work --all-sessions
```

`-a` is the short form of `--all-sessions`.
This option also works outside tmux if the selected server is running.

**Caution:** saving with `--all-sessions` replaces the saved session set.
Sessions that are not running are removed from the file.
The command does not stop live sessions.

### What save keeps

Hetki records names, directories, pane structure, supported layout presets, and pane zoom.
It does not turn the current foreground program into a startup command.

When you save over an existing workspace, Hetki keeps its startup commands if it can match them to the live structure.
Otherwise, the save fails without replacing the file. Edit the workspace or save to a new file.
Hetki also refuses an invalid destination or destination symlink.
Saved layouts use readable presets, not exact pane dimensions.

## Start a workspace

Preview the planned changes:

```bash
hetki start work --dry-run
```

Apply the changes:

```bash
hetki start work
```

By default, Hetki creates missing sessions and windows with their configured panes.
It leaves existing windows unchanged. It does not rerun their startup commands.

Hetki attaches to the first listed session, or switches to it from inside tmux.
Use `--detach` (`-d`) to skip this step.
`--dry-run` (`-n`) does not apply changes or attach.

### Replace windows or remove sessions

**Caution:** these options can terminate running processes.

- `--force` replaces mismatched windows and deletes extra windows in listed sessions.
- `--prune-sessions` deletes sessions absent from the workspace on the selected tmux server, including unrelated sessions.

First preview with the same options that you intend to apply:

```bash
hetki start work --force --dry-run
hetki start work --prune-sessions --dry-run
```

These options do not ask for confirmation. A dry run does not lock tmux state.
If execution fails, earlier changes are not rolled back. Check tmux before retrying.

## Choose a workspace

Use one name or one file path:

```bash
hetki save work
hetki start work
```

Named files are in `$XDG_CONFIG_HOME/hetki/workspaces` on Linux and macOS.
If `XDG_CONFIG_HOME` is unset or relative, Hetki uses `~/.config/hetki/workspaces` instead.
Shell completion uses the same directory.

Hetki does not move existing files or search the old directory when you set `XDG_CONFIG_HOME`.
Move existing workspaces to the new directory yourself, or continue to use their file paths:

```bash
hetki start --file ~/.config/hetki/workspaces/work.yaml
```

`work` selects `work.yaml`, or an existing `work.yml` if `work.yaml` is absent.
You can also supply the extension, such as `work.yml`.

For a file elsewhere, use `--file` (`-f`):

```bash
hetki save --file ./work.yaml
hetki start --file ./work.yaml
```

A name never selects a local file.
Do not combine a name with `--file`.
Files must have a `.yaml` or `.yml` extension.

## Write a workspace

Save this example as `work.yaml`:

```yaml
sessions:
  - name: work
    root: "~"
    windows:
      - name: editor
        command: nvim
      - name: terminal
        layout: even-horizontal
        panes:
          - path: .
          - path: .
```

The example creates one session with an editor window and a two-pane terminal window.
Set `root` to an existing project directory and replace `nvim` with your editor.

**Caution:** a workspace can run shell commands. Read commands before you start a workspace you did not write.

Preview this file with `hetki start --file ./work.yaml --dry-run`.
Remove `--dry-run` to apply the changes.

### Fields

| Location  | Field      | Meaning                                                                   |
| --------- | ---------- | ------------------------------------------------------------------------- |
| Workspace | `sessions` | Required list of sessions.                                                |
| Session   | `name`     | Required, unique session name.                                            |
| Session   | `root`     | Base directory for its windows. Defaults to your current directory.       |
| Session   | `windows`  | Required list of windows.                                                 |
| Window    | `name`     | Window name. Defaults to the first pane directory's final path component. |
| Window    | `path`     | Base directory for its panes. Defaults to the session base directory.     |
| Window    | `layout`   | Optional supported tmux layout preset.                                    |
| Window    | `command`  | Startup command for the first pane.                                       |
| Window    | `panes`    | Pane list. If omitted or empty, the window has one pane.                  |
| Pane      | `path`     | Pane directory. Defaults to the window base directory.                    |
| Pane      | `command`  | Startup command for this pane.                                            |
| Pane      | `zoom`     | Zoom this pane. At most one pane per window can use `true`.               |

Do not set both `window.command` and the first pane's `command`.
Omit commands to start shells without a startup command.
Commands must be single-line text without control characters.

Supported layout presets are `even-horizontal`, `even-vertical`, `main-horizontal`, `main-vertical`, and `tiled`.
Window `index` and pane `split` or `size` fields are not supported.

### Directory rules

- Window paths use the session `root`.
- Pane paths use the declared window `path`.
- Without a session `root`, relative paths use the directory where you run Hetki, not the YAML file's directory.
- Paths support environment variables and `~` for your home directory.
- The session root must exist. Hetki does not create missing project directories.

### File limits

Workspace YAML is strict. Unknown fields, duplicate keys, and multiple documents are invalid.
Files can contain at most 1 MiB, 64 sessions, 64 windows per session, and 32 panes per window.
Names are limited to 255 bytes. Paths and commands are limited to 4,096 bytes each.
