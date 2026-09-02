---
name: browsing
description: Use when you need to browse or automate the web — teaches which of CCY's two browser engines to use (lightweight Lightpanda vs full Chromium) and where to get the version-matched command reference
allowed-tools: Bash
---

# Browser Automation in CCY

CCY ships **one CLI, `agent-browser`, driving two different browser engines**. Choosing
the right one is the whole job of this skill; the command syntax is identical either way.

**Announce:** "I'm using the browsing skill to automate the browser, and I will close it
when I'm done."

## Close what you open — this is not optional

Every session you start is a real browser process, and with the headed engine it is a real
window on the user's desktop. Left alone it stays there until the idle timeout fires. CCY
sets that to five minutes; upstream's default is an hour, which is how desktops end up
littered with abandoned Chrome windows from agents that finished and moved on.

The timeout is a safety net for the case where you crash. It is not your cleanup. **The
same Bash call that finishes the task ends the session**, chained with `&&`, so there is no
"later" in which to forget:

```bash
agent-browser open https://example.com && agent-browser get text body && agent-browser close --all
```

Use `close --all` rather than a bare `close`: it reaps every session, including one a
previous command of yours left behind. `agent-browser-lite` has its own daemon and needs
its own `agent-browser-lite close --all`. Before you report a task finished, run
`agent-browser session list`; if it names anything, you are not finished.

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

## Which engine? Ask two questions, in this order

### 1. Does the user want or need to SEE this happening?

Chromium here is **headed** — the window appears on the user's desktop through Wayland
forwarding. That visibility is a **feature, not overhead**. If the user is sitting there
and the work is about a web page, letting them watch is often the most valuable thing the
browser does: they spot the wrong page, the missed cookie banner, or the broken layout
before you do.

So ask it explicitly, and default to *yes* when the user is clearly present and engaged
with the page itself. Being able to see what you are doing is worth far more than the
extra second.

Answer **yes** → `agent-browser` (headed is already the default; do not pass `--headed false`).

### 2. If not, does the task need pixels or geometry at all?

Answer **no** → `agent-browser-lite`. Answer **yes, but nobody is watching** →
`agent-browser --headed false`.

| Situation                                                               | Use                            |
| ----------------------------------------------------------------------- | ------------------------------ |
| Web design, UI testing, debugging a layout — **and the user is around** | `agent-browser` (headed)       |
| Reading, extracting text/markdown, scraping, checking a page's content  | `agent-browser-lite`           |
| Screenshots/PDFs/geometry, unattended (batch runs, no one watching)     | `agent-browser --headed false` |

| Engine                              | Command              | Cost per page fetch                  |
| ----------------------------------- | -------------------- | ------------------------------------ |
| **Lightpanda** — DOM + JS, no paint | `agent-browser-lite` | ~379 ms, 1 process, ~25 MB RSS       |
| **Chromium** — full browser         | `agent-browser`      | ~1177 ms, 15 processes, ~1345 MB RSS |

Both run JavaScript with the same fidelity. Measured in a CCY container, Lightpanda
matched Chromium on `fetch()`, ES modules, custom elements + shadow DOM, and a React 18
client-side render, and returned equal or more page text on real sites. So the choice is
about **visibility and pixels**, never about whether the JavaScript will run.

Fall back freely: if `agent-browser-lite` gets something wrong, rerun it with
`agent-browser` — the syntax is identical.

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
  dedicated config file, and puts it on its own daemon namespace. Every subcommand and
  flag behaves the same.
- The two engines therefore keep **separate browser sessions**. Interleave them in any
  order without closing anything — but remember that a page you opened with one engine
  is not open in the other, so re-`open` the URL after switching.
- Chromium here is **headed** against the host's Wayland socket, so a real window appears
  on the user's desktop. Treat that as a benefit to reach for when the user is present,
  not a cost to avoid — `--headed false` is for unattended work.
- Lightpanda is always headless; it has no window to show and cannot be made to have one.
- If you are unsure whether the user wants to watch, say which engine you are using and
  why in one short line, so they can redirect you before the work is done rather than
  after.
