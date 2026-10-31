# Picker colors

Add a `theme` section to `~/.config/hetki/config.yaml`.
If `XDG_CONFIG_HOME` is an absolute path, use `$XDG_CONFIG_HOME/hetki/config.yaml` instead.

```yaml
theme:
  accent: "#89b4fa"
  secondary: "#a4a1b7"
```

Use quoted `#RRGGBB` values. Set only the colors you want to change.
Remove a setting to restore its default. Open the picker again to load changes.

## Default colors

| Field          | Applies to                            | Default             |
| -------------- | ------------------------------------- | ------------------- |
| `text`         | Normal item text                      | Terminal foreground |
| `context`      | Session names and project column      | Normal text         |
| `command`      | Command labels                        | `#9ccfd8`           |
| `muted`        | Counts, annotations, and dialog hints | `#87839f`           |
| `hint`         | Search instructions                   | `#87839f`           |
| `secondary`    | Italic paths, including selection     | `#a4a1b7`           |
| `emphasis`     | Search query                          | Normal text         |
| `active`       | Current tmux item, scope, and prompt  | `#cba6f7`           |
| `accent`       | Menu keys and titles                  | `#89b4fa`           |
| `border`       | Picker and dialog borders             | `#6e6a86`           |
| `divider`      | Separator lines                       | `#6e6a86`           |
| `working`      | Working spinner                       | `#89b4fa`           |
| `blocked`      | Blocked status                        | `#f6c177`           |
| `idle`         | Idle status                           | `#a6da95`           |
| `success`      | Completed status                      | `#a6da95`           |
| `error`        | Failed status                         | `#eb6f92`           |
| `selection_fg` | Selected names and menu labels        | `#e0def4`           |
| `selection_bg` | Selected item background              | `#39374a`           |

## Notes

- Changing `accent` also sets `border` and `working`, unless you set them separately.
- Session names follow window text by default. Set `context` to give them a separate color.
- Selection uses `selection_bg`. The active item keeps its `active` text color.
  Other labels use `selection_fg`, unless you set `context` for session names.
  Paths, commands, counts, and status indicators keep their own colors.
- The main background and pane-preview colors stay unchanged. There is no automatic light/dark mode.
  Exact hex colors need true-color support in your terminal and tmux.
- Any nonempty `NO_COLOR` value disables colors, including preview colors. Selection then uses inverse text and background.
- Invalid color values show a warning and use their defaults. Other valid settings still apply.
  Invalid YAML, unknown fields, wrong value types, or duplicate keys make Hetki use all defaults.
