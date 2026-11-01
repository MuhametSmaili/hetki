# Application status

The status service tracks application activity in tmux panes.
The picker starts it automatically and uses the same service across picker runs.
Closing the picker does not stop the service.

## Read the indicators

| Indicator        | Meaning                          |
| ---------------- | -------------------------------- |
| Animated spinner | The app is working.              |
| `?`              | The app needs input or approval. |
| `○`              | The app is idle.                 |
| `✓`              | The app reported success.        |
| `×`              | The app reported an error.       |
| `·`              | The current activity is unknown. |

Outcomes are temporary. They can change or disappear when the app sends new reports.

## Where status comes from

By default, Hetki reads status reports that apps send through the OSC 7501 terminal protocol.
The app must support and enable these reports.
A screen profile can instead match text in an app's interface. Profiles depend on the app version and language.

No status does not mean that an app is idle. Ordinary terminal output alone is not activity evidence.
After a restart, status can remain missing until the app sends another report.

The **Tracked** scope includes items with any current status.

## Tmux versions

Status tracking uses tmux's native protocol and checks the server version.
It supports the stable releases below. Other releases have no automatic fallback for status tracking.

<details>
<summary>Exact tmux releases</summary>

- `3.2`, `3.2a`, `3.3`, `3.3a`, `3.4`, `3.5`, `3.5a`.
- `3.6`, `3.6a`, `3.6b`, `3.7`, `3.7a`, `3.7b`, `3.7c`, `3.8`.

</details>

## Manage the service

Commands use the tmux server selected by your environment.
Run them from the same tmux environment as the picker you want to inspect.

| Command                 | Behavior                                                                                    |
| ----------------------- | ------------------------------------------------------------------------------------------- |
| `hetki service status`  | Show the tmux socket, service state, build, and log path. Do not start tmux or the service. |
| `hetki service start`   | Start the service or reuse the running service from the same build.                         |
| `hetki service stop`    | Stop the service without stopping tmux sessions.                                            |
| `hetki service restart` | Restart the service, or start it if absent.                                                 |

Start, stop, and restart require a running tmux server.
The service stops when its tmux server exits or its executable is replaced.
An open picker starts the service again when needed.

<details>
<summary>Service recovery details</summary>

- The picker, start, and restart replace a service from another Hetki build.
- Cancellation or a timeout does not undo an accepted shutdown request.
- A restart failure after shutdown can leave the service stopped.
- Invalid profiles leave the existing service running during a restart.

</details>

## Troubleshoot missing status

1. Press `Tab` to select All items with the default bindings.
2. Run `hetki service status` in the same tmux environment.
3. Check the reported tmux socket and build.
4. Check the reported log path for service errors.
5. Check that the app sends OSC 7501 reports or has a matching screen profile.

If the service is absent, run `hetki service start`.
If you changed profiles or need to replace a running build, run `hetki service restart`.
Then cause a new app status change and check the picker again.

A restart does not guarantee that missing or stale status is fixed.
When you report a problem, include the Hetki, tmux, and app versions.
Add service status and relevant log lines, but remove private paths and terminal contents.

## Add a screen profile

Use a screen profile for an app that does not report status through OSC 7501.
A profile matches an exact command name and uses screen matching instead of protocol reports for that command.

Create a `.yaml` or `.yml` file in `~/.config/hetki/status.d`.
If `XDG_CONFIG_HOME` is absolute, use `$XDG_CONFIG_HOME/hetki/status.d` instead.
A relative `XDG_CONFIG_HOME` is an error for status profiles.

```yaml
version: 2
id: my-app
match:
  command: my-app
source:
  type: screen
  screen:
    poll_interval: 2s
    region:
      last_nonblank_rows: 12
    rules:
      - state: working
        all:
          - contains: "Working..."
      - state: needs-input
        any:
          - contains: "Approve?"
          - contains: "Continue?"
      - state: idle
        all:
          - regex: "(?m)^Ready$"
```

Change `match.command` to the app's exact command name.
`last_nonblank_rows` selects how many rows from the bottom of the pane to inspect.
Rules can report `working`, `needs-input`, or `idle`:

- Every `all` matcher must match.
- At least one `any` matcher must match when present.
- Every `none` matcher must fail to match.
- Matchers use literal `contains` text or a `regex` pattern.

If no rule matches, or matching rules report conflicting states, the status is unknown.
Screen capture runs only for commands matched by configured profiles.
With no profiles, Hetki does not capture pane contents or incur screen-capture overhead.

Profiles are read when the service starts. Run `hetki service restart` after adding or changing them.
A missing profile directory is valid.
An invalid profile or duplicate profile ID or command rejects the entire profile set.

The [Kiro CLI 2.28.0 profile](https://github.com/MuhametSmaili/hetki/blob/main/integrations/pane-status/profiles/kiro-2.28.yaml) is one example for its default English interface and key bindings.
Current Kiro CLI versions support OSC 7501 and do not need this profile.

Hetki has no built-in logic for Kiro, AI tools, or other specific apps.
It listens for OSC 7501 reports and evaluates only the command-matching profiles you provide.
You are responsible for adding profiles to `status.d` and keeping their matchers up to date.
For example, if an app changes its `Working...` label, update the corresponding rule and restart the service.
Profiles do not detect app versions or languages automatically.

<details>
<summary>Profile file limits</summary>

Profile files must be regular `.yaml` or `.yml` files, not symlinks.
Each file can be at most 32 KiB. At most 64 profiles and 256 directory entries are accepted.

</details>
