# Installation

Install Hetki on Linux or macOS. On macOS, use Ventura 13 or later.
Install tmux and make sure that it is on your `PATH`.

Status tracking supports stable [tmux releases from 3.2 through 3.8](status.md#tmux-versions).
Other versions have no automatic fallback for status tracking.

## Install script

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

## Other installation methods

| Method   | Install command                                        |
| -------- | ------------------------------------------------------ |
| Homebrew | `brew install MuhametSmaili/tap/hetki`                 |
| mise     | `mise use -g go:github.com/MuhametSmaili/hetki@latest` |
| Go       | `go install github.com/MuhametSmaili/hetki@latest`     |

Go and mise installations require Go 1.27.3 or later.
For the latest unreleased main commit with Homebrew, add `--HEAD` to the install command.

You can also download a binary from the [releases page](https://github.com/MuhametSmaili/hetki/releases).


## Update Hetki

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


## Next steps

[Open the picker](getting-started.md), or [enable shell completion](shell-completion.md).
