# Picker key bindings

By default, the picker opens in **filter mode**. Type to filter the list.
Press Escape to enter **normal mode**. Press `/` to filter again.
Use `hetki --mode normal` to start with navigation shortcuts.
User-defined actions use the [choice-screen rules](#action-choice-screens) below.

`Ctrl+C` always quits from any mode. You cannot change this key.

## Change a binding

Add `keymap` to your [configuration file](configuration.md#file-location).
Place it next to `ui`, not inside it:

```yaml
ui:
  view: tree
keymap:
  normal:
    toggle_preview: [p]
  filter:
    toggle_preview: ["alt+v"]
```

Each entry replaces the full key list for one action in one mode.
Omitted actions keep their defaults.
Modes are independent. A normal-mode binding does not change a menu binding.

For example, this configuration swaps the normal-mode rename and delete keys:

```yaml
keymap:
  normal:
    rename_window: [x]
    delete_window: [r]
```

Change both actions together to avoid a conflict with a default key.
The menu still uses `r` to rename and `x` to delete.

### Key format and validation

- Supply 1 to 16 keys for each action. Empty lists do not disable actions.
- Use one key per string, not a sequence such as `gg`.
- Use literal printable characters: `p`, `G`, or `?`. Use `G`, not `shift+g`.
- Prefix modifiers in this order: `ctrl+`, `alt+`, `shift+`.
- Use named keys shown in the tables. You can also use `space`, `home`, `end`, `insert`, and `f1` through `f24`.
- Do not assign one key to two actions in the same mode. Other modes can reuse it.
- Do not assign `ctrl+c`. It is reserved for quitting.

A terminal can intercept a key or send the same code for different keys.
A valid binding works only if your terminal sends that key to Hetki.

Invalid modes, unsupported actions, invalid key syntax, or conflicts cause a warning.
Hetki uses the entire default keymap in that case and keeps valid UI settings.
A structural error causes Hetki to ignore the entire configuration file.
For example, use a key list, not a single string.

## Default bindings

The action names below are the names to use in YAML.
An action is available only in the modes that list it.

### Normal mode

| Action              | Default keys          | Purpose                             |
| ------------------- | --------------------- | ----------------------------------- |
| `quit`              | `q`                   | Close the picker.                   |
| `move_up`           | `up`, `k`, `ctrl+p`   | Move up.                            |
| `move_down`         | `down`, `j`, `ctrl+n` | Move down.                          |
| `move_top`          | `g`                   | Move to the first item.             |
| `move_bottom`       | `G`                   | Move to the last item.              |
| `page_up`           | `pgup`, `u`           | Move up one page.                   |
| `page_down`         | `pgdown`, `d`         | Move down one page.                 |
| `filter`            | `/`                   | Enter filter mode.                  |
| `next_match`        | `n`                   | Select the next match.              |
| `previous_match`    | `N`                   | Select the previous match.          |
| `clear_filter`      | `ctrl+l`              | Clear the filter.                   |
| `context_menu`      | `ctrl+k`              | Open the actions menu.              |
| `create_session`    | `A`                   | Create a session.                   |
| `create_window`     | `a`                   | Create a window.                    |
| `rename_window`     | `r`                   | Rename the selected window.         |
| `rename_session`    | `R`                   | Rename the selected session.        |
| `delete_window`     | `x`                   | Delete the selected window.         |
| `delete_session`    | `X`                   | Delete the selected session.        |
| `expand`            | `right`, `l`          | Expand a tree item.                 |
| `collapse`          | `left`, `h`           | Collapse a tree item.               |
| `expand_all`        | `L`                   | Expand all tree items.              |
| `collapse_all`      | `H`                   | Collapse all tree items.            |
| `refresh`           | `ctrl+r`              | Refresh the list.                   |
| `toggle_projection` | `ctrl+t`              | Switch between flat and tree views. |
| `toggle_scope`      | `tab`                 | Switch between All and Tracked.     |
| `toggle_preview`    | `alt+p`               | Show or hide the pane preview.      |
| `open`              | `enter`, `ctrl+y`     | Open the selection.                 |
| `last_session`      | `ctrl+o`              | Switch to the previous session.     |

### Filter mode

| Action              | Default keys          | Purpose                             |
| ------------------- | --------------------- | ----------------------------------- |
| `cancel`            | `esc`                 | Enter normal mode.                  |
| `confirm`           | `enter`, `ctrl+y`     | Open the selection.                 |
| `move_up`           | `up`, `ctrl+p`        | Move up.                            |
| `move_down`         | `down`, `ctrl+n`      | Move down.                          |
| `page_up`           | `pgup`                | Move up one page.                   |
| `page_down`         | `pgdown`              | Move down one page.                 |
| `backspace`         | `backspace`, `delete` | Delete the preceding character.     |
| `delete_word`       | `ctrl+w`              | Delete the preceding word.          |
| `delete_to_start`   | `ctrl+u`              | Delete text back to the start.      |
| `clear_filter`      | `ctrl+l`              | Clear the filter.                   |
| `context_menu`      | `ctrl+k`              | Open the actions menu.              |
| `toggle_projection` | `ctrl+t`              | Switch between flat and tree views. |
| `toggle_scope`      | `tab`                 | Switch between All and Tracked.     |
| `toggle_preview`    | `alt+p`               | Show or hide the pane preview.      |
| `last_session`      | `ctrl+o`              | Switch to the previous session.     |

### Prompt mode

Prompt mode accepts text for actions such as creating or renaming an item.
Use `prompt` as the YAML mode name.

| Action            | Default keys          | Purpose                         |
| ----------------- | --------------------- | ------------------------------- |
| `cancel`          | `esc`                 | Cancel the prompt.              |
| `confirm`         | `enter`, `ctrl+y`     | Submit the text.                |
| `backspace`       | `backspace`, `delete` | Delete the preceding character. |
| `delete_word`     | `ctrl+w`              | Delete the preceding word.      |
| `delete_to_start` | `ctrl+u`              | Delete text back to the start.  |

### Confirmation mode

Use `confirm` as the YAML mode name.

| Action    | Default keys                | Purpose             |
| --------- | --------------------------- | ------------------- |
| `cancel`  | `esc`, `n`, `N`             | Cancel the action.  |
| `confirm` | `enter`, `ctrl+y`, `y`, `Y` | Confirm the action. |

### Menu mode

Open the actions menu with `Ctrl+K` by default.
You can select an entry and confirm it, or use its shortcut.
Available entries depend on the selected item.

Press `U` to open **User actions…**. To change this shortcut:

```yaml
keymap:
  menu:
    user_actions: [u]
```

| Action              | Default keys          | Purpose                             |
| ------------------- | --------------------- | ----------------------------------- |
| `open`              | `o`                   | Open the selected item.             |
| `user_actions`      | `U`                   | Open user-defined actions.          |
| `rename_window`     | `r`                   | Rename the selected window.         |
| `rename_session`    | `R`                   | Rename the selected session.        |
| `create_window`     | `a`                   | Create a window.                    |
| `create_session`    | `A`                   | Create a session.                   |
| `delete_window`     | `x`                   | Delete the selected window.         |
| `delete_session`    | `X`                   | Delete the selected session.        |
| `refresh`           | `ctrl+r`              | Refresh the list.                   |
| `toggle_projection` | `ctrl+t`              | Switch between flat and tree views. |
| `cancel`            | `esc`                 | Close the menu.                     |
| `confirm`           | `enter`, `ctrl+y`     | Run the selected menu entry.        |
| `move_up`           | `up`, `k`, `ctrl+p`   | Move up.                            |
| `move_down`         | `down`, `j`, `ctrl+n` | Move down.                          |

### Action choice screens

These rules apply after you open **User actions…** or run `hetki action`.
Choice screens always start in filter mode, even when your configuration uses `mode: normal`.
They differ from the normal session/window picker:

- `Escape` always goes Back. It does not leave filter mode and takes precedence over keymap overrides.
- The configured `cancel` and `collapse` actions also go Back.
- The configured `confirm` and `open` actions select a choice.
- Movement, paging, filtering, text-editing, and quit bindings use the current mode's keymap.
- `clear_filter` clears the query and stays in filter mode, so you can type another query immediately.
- The configured `refresh` action reloads choices. Unbound `Ctrl+R` also reloads them.
  If you bind `Ctrl+R` to another action in the current mode, that binding takes precedence.
- Navigation commands such as rename, delete, preview, scope, and view switching are unavailable.
- While a program runs, only `Ctrl+C` is available.

Back and refresh never execute the final command again.
On a completed-command diagnostic screen, Back closes the flow and refresh keeps the diagnostic.
