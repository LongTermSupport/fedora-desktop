# Explain the Status Line

Answers "what does this icon mean, and what does its current value mean?"
for every segment the status line can render — the field report this shipped
from was literally "🧹 1 stale — i have forgotten what this means" (Plan
00369).

## Usage

A routed skill subcommand — a human types this. On a self-install the
wrapper is `bin/hooks-daemon`; a client install uses
`.claude/hooks-daemon/bin/hooks-daemon`:

```claude-code
/hooks-daemon status-line-explained
/hooks-daemon status-line-explained --format json
```

Equivalent CLI verbs, which an agent can run directly:

```bash
.claude/hooks-daemon/bin/hooks-daemon status-line-explained
.claude/hooks-daemon/bin/hooks-daemon explain-status-line   # alias
```

## Output

For each status-line handler currently ENABLED in this project, in
status-line priority order:

```
Current Time  [current_time]
  🕐
  What it is: The local wall-clock time, refreshed on every status-line render.
  How to read it: 24-hour HH:MM, no seconds. Always shown; no colour coding.
  Right now: Currently shows 14:32.
```

Handlers disabled by config appear afterward, under a `Not enabled:` heading,
one line each. `--format json` emits the same information as a JSON array —
one object per handler, each carrying `config_key`, `enabled`, `priority`,
`glyphs`, `name`, `what_it_is`, `how_to_read`, `current_value`, and `error`
(non-null only if that handler's explanation could not be computed).

## What "reference" means at the top of the text output

The printed icon line is a REFERENCE — every enabled segment's glyph(s)
joined together — not a byte-for-byte replay of what your terminal shows
right now. Several segments (model name and context %, the working-directory
diff, the multithread count, an active downgrade) only have a real value
inside a LIVE Claude Code session render, which this command — run from a
plain shell, outside any render — cannot see. Where that applies, `current_value`
says so plainly ("not shown now — requires a live session") rather than
guessing.

## Why this exists

`explain-rule`/`explain-handler` (see [rule-explain.md](rule-explain.md))
answer this question for anything with a *blocking rule* — but a status-line
segment is advisory-only and declares no `Rule`, so neither of those commands
can find it. Every status-line handler now implements a separate,
self-describing `explain_segment()` method (never called from the render hot
path, and never writes anything to disk) purely for this command to read.

## Implementation

Fully dynamic, like `explain-rule`/`explain-handler`: it discovers every
status-line handler directly from the handlers package (no running daemon
required), resolves each one's enabled/priority state from this project's
`.claude/hooks-daemon.yaml`, and calls its `explain_segment()`. A handler
whose explanation fails to compute is reported inline rather than hiding
every other handler's explanation.
