# Picker configuration

[README](../README.md) · [Key bindings](key-bindings.md) · [Theme](theme.md) · [Workspaces](workspaces.md) · [Status](status.md)

Use the configuration file for your usual settings. Use flags to change them for one run.

## File location

The picker reads `$XDG_CONFIG_HOME/hetki/config.yaml` on Linux and macOS.
If `XDG_CONFIG_HOME` is unset or relative, it reads `~/.config/hetki/config.yaml` instead.

The file is optional. Add only the settings you want to change.
Hetki reads it when the picker opens. It does not save changes made in the picker.

## Defaults

```yaml
ui:
  view: flat
  scope: all
  preview: false
  preview_width: 70
  height: "100%"
  border: rounded
```

Add [key bindings](key-bindings.md) under `keymap` and [theme colors](theme.md) under `theme`, next to `ui`.

## Picker flags

Explicit flags override file settings. File settings override built-in defaults.
These flags apply only to the picker, not to `start`, `save`, or `service`.

| Flag              | File key           | Values              | Purpose                                                                   |
| ----------------- | ------------------ | ------------------- | ------------------------------------------------------------------------- |
| `--view`          | `ui.view`          | `flat`, `tree`      | Choose a flat list or a session tree.                                     |
| `--scope`         | `ui.scope`         | `all`, `tracked`    | Show all items or items with a current status.                            |
| `--preview`       | `ui.preview`       | `true`, `false`     | Show or hide the pane preview.                                            |
| `--preview-width` | `ui.preview_width` | `1`–`99`            | Set the preview width as a percentage.                                    |
| `--height`        | `ui.height`        | Rows or `1%`–`100%` | Set the height. `100%` selects fullscreen mode.                           |
| `--border`        | `ui.border`        | `rounded`, `none`   | Set the outer border. Dialog borders do not change.                       |
| `--stay-open`     | None               | `true`, `false`     | Keep the picker open after switching. Requires tmux. Defaults to `false`. |

Boolean flags support the short form: use `--preview` instead of `--preview=true`.
Invalid flags stop startup with an error.

```bash
hetki --height 40% --border none
hetki --view tree --scope tracked --preview --preview-width 45
hetki --preview=false
hetki --stay-open
```

## Usage notes

- **Scope:** `tracked` includes items with any [current status](status.md). Your query filters within this scope.
- **Preview:** width does not enable the preview. A narrow terminal can hide it.
- **Height:** `100%` is fullscreen; other percentages and row counts use inline mode near your prompt.
- **Stay open:** your picker state stays in memory until `Ctrl+C`. Hetki does not position its pane.

## Invalid configuration

Configuration problems show a warning instead of blocking the picker:

- An invalid UI choice uses that field's default.
- Invalid key bindings use the default keymap and keep valid UI settings.
- An unreadable file, malformed YAML, wrong value type, or unknown key causes Hetki to use all defaults.

Use YAML booleans for `preview` and integers for `preview_width`. Do not quote these values.
Use one YAML document. Duplicate keys, nulls, aliases, merge keys, and explicit tags are not supported.
