import { defineConfig } from "vitepress";

const base = process.env.DOCS_BASE || "/";

export default defineConfig({
  title: "Hetki",
  description:
    "A keyboard-first tmux picker and workspace tool for Linux and macOS.",
  lang: "en-US",
  base,
  head: [
    ["meta", { name: "theme-color", content: "#24273a" }],
    [
      "link",
      { rel: "icon", type: "image/svg+xml", href: `${base}favicon.svg` },
    ],
  ],
  themeConfig: {
    nav: [
      { text: "Guide", link: "/getting-started" },
      { text: "Reference", link: "/configuration" },
    ],
    sidebar: [
      {
        text: "Start here",
        items: [
          { text: "Introduction", link: "/" },
          { text: "Installation", link: "/installation" },
          { text: "Getting started", link: "/getting-started" },
        ],
      },
      {
        text: "Guides",
        items: [
          { text: "Workspaces", link: "/workspaces" },
          { text: "Application status", link: "/status" },
          { text: "Shell completion", link: "/shell-completion" },
        ],
      },
      {
        text: "Reference",
        items: [
          { text: "Configuration", link: "/configuration" },
          { text: "Key bindings", link: "/key-bindings" },
          { text: "Theme", link: "/theme" },
        ],
      },
    ],
    search: { provider: "local" },
    socialLinks: [
      { icon: "github", link: "https://github.com/MuhametSmaili/hetki" },
    ],
    outline: { level: [2, 3] },
    footer: {
      message: "Released under the MIT License.",
      copyright: "Hetki · Your tmux companion.",
    },
  },
});
