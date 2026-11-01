---
layout: home
title: Your tmux sessions, within reach
description: Find sessions, preview panes, track application activity, and save repeatable tmux workspaces with Hetki.
hero:
  name: Hetki
  text: Find your place in tmux.
  tagline: Search sessions, preview panes, and save your setup for later. A keyboard-first tool for Linux and macOS.
  image:
    src: /picker.png
    alt: Hetki picker showing tmux windows and application status indicators
  actions:
    - theme: brand
      text: Install Hetki
      link: /installation
    - theme: alt
      text: Get started
      link: /getting-started
features:
  - title: Find the right session
    details: Filter sessions, windows, and panes. Choose a flat list ranked by recent and frequent use, or explore a session tree.
    link: /getting-started
    linkText: Meet the picker
  - title: See what needs you
    details: Preview a pane before switching. See when supported apps are working, need input, or finish a task.
    link: /status
    linkText: Understand application status
  - title: Bring your setup back
    details: Save sessions as YAML workspaces. Start the same windows, pane layouts, and commands the next time you need them.
    link: /workspaces
    linkText: Save a workspace
---

## Your sessions stay in tmux

Hetki works with your existing tmux server.
Open the picker when you need it. Close it without closing your sessions.
Inside tmux, Hetki switches your current client. Outside tmux, it attaches to the session you select.

Workspaces save session structure, not running processes or terminal contents.
Application status comes from supported reports or configured screen profiles, not arbitrary terminal output.

## Start with one command

With [tmux installed](installation.md), install Hetki:

```bash
curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
```

Run `hetki`, type a name, and press Enter.
See [installation](installation.md) for system requirements, Homebrew, Go, mise, and updates.

## Keep your workflow

Use an inline picker near your shell prompt or keep it fullscreen.
Change the keys, colors, and layout without changing how you use tmux.

[Configure the picker](configuration.md) · [Change key bindings](key-bindings.md) · [Choose colors](theme.md)
