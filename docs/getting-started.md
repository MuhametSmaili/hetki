# Getting started

Hetki helps you find and switch between tmux sessions, windows, and panes.
[Install Hetki](installation.md) before you start.

## Open the picker

If you do not have any tmux sessions, create one first:

```bash
tmux new-session -d -s work
```

This creates a session named `work` without attaching to it.

Open the picker:

```bash
hetki
```

1. Type part of a session, window, or pane name.
2. Use the arrow keys to select an item.
3. Press Enter to switch to it.

Inside tmux, Hetki switches the current client.
Outside tmux, it attaches to the selected session.
By default, the picker closes after you switch.
Your sessions remain in tmux when you close Hetki.

## Find your way around

By default, the picker opens in **filter mode**. Type to filter the list.
Press Escape to enter **normal mode**. Press `/` to filter again.
Use `hetki --mode normal` to start with navigation shortcuts.

Choose a flat list or a tree of sessions, windows, and panes.
The flat list uses **frecency**: directories you use often or recently rank higher.
Press `Ctrl+T` to switch views.

| Key | Action |
| --- | --- |
| `Enter` | Open the selection. |
| `Ctrl+K` | Open the actions menu to create or manage sessions and windows. |
| `Ctrl+T` | Switch between flat and tree views. |
| `Tab` | Switch between All and Tracked items. |
| `Alt+P` | Show or hide the pane preview. |
| `Ctrl+O` | Switch to the previous session. |
| `Ctrl+C` | Quit from any mode. |

These are the default keys. See [key bindings](key-bindings.md) for all defaults and customization.

## Next steps

- [Save and start a workspace](workspaces.md) to use your tmux setup again later.
- [Configure the picker](configuration.md) for an inline layout, pane previews, or stay-open mode.
- [Read application status](status.md) to see what supported apps are doing.
- [Change the theme](theme.md) or [enable shell completion](shell-completion.md).

Use `hetki --help` or `hetki <command> --help` for command options.
