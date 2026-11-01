# Shell completion

## Zsh

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

## Bash

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
