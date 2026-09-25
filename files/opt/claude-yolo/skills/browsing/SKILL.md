---
name: browsing
description: Use when you need to browse or automate the web — CCY has no default browser mode, so this skill is the decision matrix between agent-browser-headed, agent-browser-headless and agent-browser-lite-headless, plus where to get the version-matched command reference
allowed-tools: Bash
---

# Browser Automation in CCY

CCY ships **one CLI under three names, and you must pick one every time**. The bare
`agent-browser` command is deliberately blocked: it prints this decision matrix and exits
non-zero. Whatever you pick, the subcommands and flags are identical.

| Will a human watch it happen? | Needs pixels or geometry?                                 | Command                       |
| ----------------------------- | --------------------------------------------------------- | ----------------------------- |
| **yes**                       | acceptance tests, UI checks: a real window on the desktop | `agent-browser-headed`        |
| no                            | **yes**: screenshots, PDFs, geometry, layout              | `agent-browser-headless`      |
| no                            | no: parsing text or content, scraping, research           | `agent-browser-lite-headless` |

There is no `agent-browser-lite-headed` and there cannot be: Lightpanda has no renderer,
so it has nothing to show.

**Announce:** "I'm using the browsing skill with `<command>` because `<one-line reason>`,
and I will close it when I'm done." The reason matters. A headed window is the right thing
when the user asked to watch an acceptance test and the wrong thing when you are quietly
reading documentation, and saying which case you think you are in lets the user redirect
you before the window appears rather than after.

## Decide in this order

1. **Did the user ask to see it, or is the work about how a page looks and they are
   present?** Web design, UI testing, debugging a layout while the user sits there →
   `agent-browser-headed`. The window on their desktop is the point: they spot the wrong
   page, the missed cookie banner, the broken layout before you do.
2. **Otherwise, do you need pixels or geometry at all?** Screenshots, PDFs, element
   boxes, visual regression, unattended acceptance runs → `agent-browser-headless`. Same
   Chromium, no window.
3. **Otherwise** → `agent-browser-lite-headless`. Reading, extracting text or markdown,
   scraping, checking content, research. It is ~50x cheaper and nobody is disturbed.

When you are not sure whether the user wants to watch, the answer is **no**: research and
background work never get a window. Say so in your announce line so they can say
otherwise.

| Engine                              | Command                       | Cost per page fetch                  |
| ----------------------------------- | ----------------------------- | ------------------------------------ |
| **Lightpanda** — DOM + JS, no paint | `agent-browser-lite-headless` | ~379 ms, 1 process, ~25 MB RSS       |
| **Chromium** — full browser         | `agent-browser-headless`      | ~1177 ms, 15 processes, ~1345 MB RSS |
| **Chromium** — full browser, window | `agent-browser-headed`        | as headless, plus a desktop window   |

Both engines run JavaScript with the same fidelity. Measured in a CCY container,
Lightpanda matched Chromium on `fetch()`, ES modules, custom elements + shadow DOM, and a
React 18 client-side render, and returned equal or more page text on real sites. So the
choice is about **visibility and pixels**, never about whether the JavaScript will run.

Fall back freely: if a site does not behave under `agent-browser-lite-headless`, rerun the
same commands with `agent-browser-headless` — the syntax is identical.

## Close what you open — this is not optional

Every session you start is a real browser process, and with `agent-browser-headed` it is a
real window on the user's desktop. Left alone it stays there until the idle timeout fires.
CCY sets that to five minutes; upstream's default is an hour, which is how desktops end up
littered with abandoned Chrome windows from agents that finished and moved on.

The timeout is a safety net for the case where you crash. It is not your cleanup. **The
same Bash call that finishes the task ends the session**, chained with `&&`, so there is no
"later" in which to forget.

**Name your session, and pass `--session <name>` on every command.** Pick a short name
for the task (`docs`, `pricing-check`). With a name, a parallel agent cannot silently
drive your page, and you close exactly what you opened:

```bash
agent-browser-lite-headless --session docs open https://example.com \
  && agent-browser-lite-headless --session docs get text body \
  && agent-browser-lite-headless --session docs close
```

Do not use `export AGENT_BROWSER_SESSION=...`, which the upstream core skill suggests.
In Claude Code the export is gone by your next Bash call, and the calls after it would
land on another session. Put `--session` on the command itself. Do not use
`close --all` either: it also closes sessions other agents are using.

**Each of the three commands has its own daemon**, so close in every mode you used.
Before you report a task finished, run `<command> session list` for every mode you used.
If one still lists your session, you are not finished.

## One session per command, and a second is refused

Every session name is a separate browser, about 14 Chromium processes each. So CCY lets
each of the three commands have **one session open at a time**. A command that would
start a second exits 3 with `refused`, and names the session that is already open:

- **It is yours and you still need it:** reuse it with `--session <that name>`. This
  is also what happens if you forget `--session` on one command.
- **It is yours and you are done with it:** `<command> --session <that name> close`,
  then re-run.
- **You did not open it:** another agent working in parallel did. Do not close it. Wait
  for it, or use a different mode if the task allows.

The browser commands also refuse (exit 2) a copy of a flag they set themselves, such as
`--headed` or `--namespace`: a second copy would override the mode you chose.

A project that genuinely needs two sessions side by side, such as a two-user flow, sets
`CCY_BROWSER_MAX_SESSIONS` in its `.claude/ccy/ccy.env`. Close each one when done.

## Get the command reference from the CLI itself

Do **not** learn the commands from a copy in this repo. `agent-browser` ships its own
skills, always matched to the installed version:

```bash
agent-browser-headless skills get core --full   # full command reference + patterns
agent-browser-headless skills list              # electron, slack, dogfood, ...
```

Read that before running anything. Its examples are written as `agent-browser ...`;
substitute the command you chose, because the bare name exits 2 here. Do not "fix" that
by reinstalling the npm package: the block is deliberate. This file deliberately does not
restate the reference — a hand-maintained copy drifts, and an earlier version of this
skill taught a
`agent-browser run "navigate …; extract text"` syntax that no longer exists at all.

## The trap: Lightpanda fails silently

Lightpanda has **no layout or paint pipeline**. It does not error when you ask for pixels
— it returns success and gives you something useless:

```bash
$ agent-browser-lite-headless screenshot /tmp/page.png
✓ Screenshot saved to /tmp/page.png      # exit 0 ...
# ... and the PNG says "Lightpanda has no graphical rendering engine"

$ agent-browser-lite-headless get box body
height: 100000000                        # exit 0, fabricated geometry
```

**Exit status will not warn you.** If the task involves pixels or geometry, choose a
Chromium command up front — do not check the return code and assume it worked.

## Quick reference

Identical for all three commands. The browser persists between invocations via a daemon,
so chain with `&&`, and name the session on every command:

```bash
# Read a page's rendered content (cheap engine, no window)
agent-browser-lite-headless --session docs open https://example.com \
  && agent-browser-lite-headless --session docs get text body

# NOTE: `read <url>` does NOT render — it is an HTTP fetch plus text extraction.
# For anything JavaScript-dependent, use `open` then `get text`.
agent-browser-lite-headless --session docs read https://example.com  # static/markdown only

# Inspect and interact while the user watches
agent-browser-headed --session ui open https://example.com && agent-browser-headed --session ui snapshot -i
agent-browser-headed --session ui click @e2
agent-browser-headed --session ui fill @e3 "user@example.com"

# Screenshot with nobody watching — Chromium, no window
agent-browser-headless --session shot open https://example.com \
  && agent-browser-headless --session shot screenshot /tmp/page.png

# Finish up — close each session you opened, in its own mode
agent-browser-headed --session ui close
agent-browser-headless --session shot close
agent-browser-lite-headless --session docs close
```

## Notes

- All three are passthrough wrappers around the same binary. `agent-browser-headed` and
  `agent-browser-headless` differ only in the headed flag; `agent-browser-lite-headless`
  selects the Lightpanda engine via a dedicated config file. Every subcommand and flag
  behaves the same. The additions are the session cap and the refused duplicate flags
  above.
- Each wrapper runs on its own daemon namespace, so the three keep **separate browser
  sessions**. Interleave them in any order without closing anything — but a page you
  opened with one is not open in the other, so re-`open` the URL after switching.
- `agent-browser-headed` renders against the host's Wayland socket, so a real window
  appears on the user's desktop. That is a feature when the user asked to watch and an
  intrusion when they did not.
- Lightpanda is always headless; it has no window to show and cannot be made to have one.
