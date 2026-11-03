import { defineConfig } from "vitepress";

const base = process.env.DOCS_BASE || "/";

export default defineConfig({
  title: "Hetki",
  description:
    "A keyboard-first tmux picker and workspace tool for Linux and macOS.",
  lang: "en-US",
  base,
  appearance: "dark",
  head: [
    ["meta", { name: "theme-color", content: "#0b0b0c" }],
    ["link", { rel: "preconnect", href: "https://fonts.googleapis.com" }],
    [
      "link",
      { rel: "preconnect", href: "https://fonts.gstatic.com", crossorigin: "" },
    ],
    [
      "link",
      {
        rel: "stylesheet",
        href: "https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:ital,wght@0,400;0,500;0,600;1,400&family=IBM+Plex+Sans:wght@400;500;600&display=swap",
      },
    ],
    ["meta", { property: "og:title", content: "Hetki" }],
    [
      "meta",
      {
        property: "og:description",
        content:
          "Find your place in tmux. A keyboard-first picker and workspace tool.",
      },
    ],
    [
      "meta",
      { property: "og:image", content: "https://hetki.smaili.dev/picker.png" },
    ],
    ["meta", { name: "twitter:card", content: "summary_large_image" }],
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
  },
});
