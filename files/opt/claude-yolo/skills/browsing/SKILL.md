---
name: browsing
description: Use when you need to browse or automate the web — teaches which of CCY's two browser engines to use (lightweight Lightpanda vs full Chromium) and where to get the version-matched command reference
allowed-tools: Bash
---

# Browser Automation in CCY

CCY ships **one CLI, `agent-browser`, driving two different browser engines**. Choosing
the right one is the whole job of this skill; the command syntax is identical either way.

**Announce:** "I'm using the browsing skill to automate the browser."

## Get the command reference from the CLI itself

Do **not** learn the commands from a copy in this repo. `agent-browser` ships its own
skills, always matched to the installed version:

```bash
agent-browser skills get core --full   # full command reference + patterns
agent-browser skills list              # electron, slack, dogfood, ...
```

Read that before running anything. This file deliberately does not restate it — a
hand-maintained copy drifts, and the previous version of this skill taught a
`agent-browser run "navigate …; extract text"` syntax that no longer exists at all.

## Which engine?

| Engine                              | Command              | Cost per page fetch                  |
| ----------------------------------- | -------------------- | ------------------------------------ |
| **Lightpanda** — DOM + JS, no paint | `agent-browser-lite` | ~379 ms, 1 process, ~25 MB RSS       |
| **Chromium** — full browser         | `agent-browser`      | ~1177 ms, 15 processes, ~1345 MB RSS |

Both run JavaScript with the same fidelity. Measured in a CCY container, Lightpanda
matched Chromium on `fetch()`, ES modules, custom elements + shadow DOM, and a React 18
client-side render, and returned equal or more page text on real sites.

### Use `agent-browser-lite` (Lightpanda) for

- Reading a page, extracting text or markdown
- Scraping, including JavaScript-rendered SPAs
- Checking whether a page contains something
- Anything where you want the page's **content**

### Use `agent-browser` (Chromium) for

- **Screenshots, PDFs, visual checks** — anything about pixels
- **Element geometry** (`get box`), layout, or CSS-computed positions
- Clicking through a flow where visual state matters
- Anything `agent-browser-lite` got wrong (fall back freely — same syntax)

## The trap: Lightpanda fails silently

Lightpanda has **no layout or paint pipeline**. It does not error when you ask for pixels
— it returns success and gives you something useless:

```bash
$ agent-browser-lite screenshot /tmp/page.png
✓ Screenshot saved to /tmp/page.png      # exit 0 ...
# ... and the PNG says "Lightpanda has no graphical rendering engine"

$ agent-browser-lite get box body
height: 100000000                        # exit 0, fabricated geometry
```

**Exit status will not warn you.** If the task involves pixels or geometry, choose
`agent-browser` up front — do not check the return code and assume it worked.

## Quick reference

Identical for both commands; swap `agent-browser` for `agent-browser-lite` to go cheap.
The browser persists between invocations via a daemon, so chain with `&&`:

```bash
# Read a page's rendered content (cheap engine)
agent-browser-lite open https://example.com && agent-browser-lite get text body

# NOTE: `read <url>` does NOT render — it is an HTTP fetch plus text extraction.
# For anything JavaScript-dependent, use `open` then `get text`.
agent-browser-lite read https://example.com    # fine for static/markdown docs only

# Inspect and interact (full engine)
agent-browser open https://example.com && agent-browser snapshot -i
agent-browser click @e2
agent-browser fill @e3 "user@example.com"

# Screenshot — Chromium only
agent-browser open https://example.com && agent-browser screenshot /tmp/page.png

# Finish up
agent-browser close --all
```

## Notes

- `agent-browser-lite` is a passthrough wrapper: it selects the Lightpanda engine via a
  dedicated config file and changes nothing else. Every subcommand and flag behaves the
  same.
- Chromium here is **headed** against the host's Wayland socket, so a real window can
  appear. Lightpanda is always headless.
