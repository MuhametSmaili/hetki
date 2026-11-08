# Installation

Install Hetki on Linux or macOS. On macOS, use Ventura 13 or later.
Install tmux and make sure that it is on your `PATH`.

Status tracking supports stable [tmux releases from 3.2 through 3.8](status.md#tmux-versions).
Other versions have no automatic fallback for status tracking.

::: warning Known tmux issue
On tmux 3.3–3.6b, setting the global `window-size` option to `manual` can crash the
tmux server when creating a session or window, closing all sessions on that server.
Upgrade to tmux 3.7 or later, or use `window-size latest`.
The default `window-size latest` setting is unaffected.
See [issue #26](https://github.com/MuhametSmaili/hetki/issues/26) for details.
:::

## Install script

Downloads a prebuilt release to `~/.local/bin`. Go is not required.

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

Make sure `~/.local/bin` is on your `PATH`.

For a manual installation, download a prebuilt binary from the
[releases page](https://github.com/MuhametSmaili/hetki/releases).

## Homebrew

Homebrew manages the required tmux and Go build dependencies for you.

```bash
brew install MuhametSmaili/tap/hetki
```

For the latest unreleased main commit, add `--HEAD` to the install command.

## Build from source

If you already use Go, install the latest release directly from source.
Building Hetki requires Go 1.27.3 or later.
An older Go installation may [download the required toolchain automatically](https://go.dev/doc/toolchain).

```bash
go install github.com/MuhametSmaili/hetki@latest
```

The executable is installed in `$GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is
unset. Make sure that directory is on your `PATH`.

## Update Hetki

Use the tool that you used to install Hetki:

| Installed with | Update with                                       |
| -------------- | ------------------------------------------------- |
| `install.sh`   | Run the install command again.                    |
| Homebrew       | `brew upgrade hetki`                              |
| Homebrew, HEAD | `brew upgrade --fetch-HEAD hetki`                  |
| Go             | `go install github.com/MuhametSmaili/hetki@latest` |

The status service stops when its binary is replaced.
The picker starts the new build the next time it opens.
The picker and `hetki service restart` also replace a service from another build.

## Next steps

[Open the picker](getting-started.md), or [enable shell completion](shell-completion.md).
