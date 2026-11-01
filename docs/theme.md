# Picker colors

[README](../README.md) · [Configuration](configuration.md) · [Key bindings](key-bindings.md) · [Workspaces](workspaces.md) · [Status](status.md)

Add a `theme` section to the [picker configuration](configuration.md#file-location), usually at `~/.config/hetki/config.yaml`.
If `XDG_CONFIG_HOME` is an absolute path, use `$XDG_CONFIG_HOME/hetki/config.yaml` instead.

```yaml
theme:
  accent: "#79c0ff"
  secondary: "#8b949e"
```

Use quoted `#RRGGBB` values. Set only the colors you want to change.
Remove a setting to restore its default. Open the picker again to load changes.

## Default colors

| Field          | Applies to                            | Default             |
| -------------- | ------------------------------------- | ------------------- |
| `text`         | Normal item text                      | Terminal foreground |
| `context`      | Session names and project column      | Normal text         |
| `command`      | Command labels                        | `#39c5cf`           |
| `muted`        | Counts, annotations, and dialog hints | `#6e7681`           |
| `hint`         | Search instructions                   | `#6e7681`           |
| `secondary`    | Italic paths, including selection     | `#8b949e`           |
| `emphasis`     | Search query                          | Normal text         |
| `active`       | Current tmux item, scope, and prompt  | `#58a6ff`           |
| `accent`       | Menu keys and titles                  | `#79c0ff`           |
| `border`       | Picker and dialog borders             | `#30363d`           |
| `divider`      | Separator lines                       | `#30363d`           |
| `working`      | Working spinner                       | `#58a6ff`           |
| `blocked`      | Blocked status                        | `#d29922`           |
| `idle`         | Idle status                           | `#3fb950`           |
| `success`      | Completed status                      | `#3fb950`           |
| `error`        | Failed status                         | `#ff7b72`           |
| `selection_fg` | Selected names and menu labels        | `#ffffff`           |
| `selection_bg` | Selected item background              | `#1c2d44`           |

## Notes

- Any nonempty `NO_COLOR` value disables colors, including preview colors. Selection then uses inverse text and background.
- Invalid color values show a warning and use their defaults. Other valid settings still apply.
  Invalid YAML, unknown fields, wrong value types, or duplicate keys make Hetki use all defaults.
