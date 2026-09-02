---
name: child-claude
description: Use when you need a SEPARATE claude process rather than a subagent — a fresh context window, a headless pipeline step, or a long job in another directory. Explains ccy-claude, which attaches this session's credential to a child, and when the Agent tool is the better answer instead.
allowed-tools: Bash
---

# Spawning a child Claude Code process

This project opted in to child-claude mode, so `ccy-claude` is on your `PATH`. It runs
`claude` with this session's own credential attached.

**Announce:** "I'm using the child-claude skill to run a separate claude process."

## Read this part first: usually you want the Agent tool instead

A child process is not a better subagent. It is a **different thing**, and it is worse at
the job the `Agent` tool already does well: it cannot see your conversation, returns only
text on stdout, cannot be messaged mid-flight, and pays a full session's startup.

| You want                                                 | Use          |
| -------------------------------------------------------- | ------------ |
| Parallel investigation, a second opinion, a broad search | `Agent` tool |
| Work that should report back and stay steerable          | `Agent` tool |
| A genuinely fresh context window, unrelated to this one  | `ccy-claude` |
| A headless step inside a shell pipeline or script        | `ccy-claude` |
| A long job in a different repository or worktree         | `ccy-claude` |
| Something that must outlive this turn as its own process | `ccy-claude` |

If you cannot say which row you are in, you are in the first one. Use `Agent`.

## Running one

```bash
ccy-claude -p "summarise CHANGELOG.md in three bullets" --model haiku < /dev/null
```

**The `< /dev/null` is not optional.** Without it the child waits several seconds for stdin
that never comes, then warns and proceeds anyway. In a script, or anywhere without a
terminal, that reads as a hang.

### The trap that will actually catch you: the child inherits this project

A child started in `/workspace` loads the whole project harness — `CLAUDE.md`, the hooks
daemon and its handlers, session-start hooks, project skills and settings. That context can
swamp a short prompt completely.

It is not theoretical. The first real run of this feature's own functional test asked a
child to reply with a one-word sentinel. The child authenticated perfectly and then replied
about this repository's stop-hook rules, never producing the sentinel at all. Nothing was
broken; the project's own instructions simply outweighed the question.

So choose the working directory deliberately:

```bash
# A self-contained question — run it somewhere neutral.
( cd "$(mktemp -d)" && ccy-claude -p "..." --model haiku < /dev/null )

# Work ON this project — run it here, and expect the project's rules to apply.
ccy-claude -p "..." --model haiku < /dev/null
```

If a child returns something confidently unrelated to what you asked, check the working
directory before you suspect the prompt.

Arguments reach `claude` **verbatim**. The wrapper adds nothing: no model, no settings file,
and deliberately no `--dangerously-skip-permissions`. If a child needs a permission mode,
pass it yourself and mean it. The wrapper will not widen a child's authority on your behalf.

## What it costs, and what it will refuse

- **Quota.** The child spends the same subscription as this session. Ten children is ten
  sessions' worth of usage. Prefer one child with a larger prompt over a fan-out.
- **Depth is bounded.** A child may not spawn its own child. The wrapper refuses past
  `CCY_CHILD_CLAUDE_MAX_DEPTH`, default 1, so a recursive tree cannot run away. A child
  reporting `refusing to spawn — already at depth` is the guard working, not a bug.
- **Transcripts persist on the host.** The child writes session state under `/root/.claude`,
  a symlink to `/workspace/.claude/ccy`, inside the project directory mounted from the
  user's real filesystem. Whatever the child prints is recorded there in plaintext.

## The one rule

**Never print, echo, or log the credential.** Do not run `env`, `set -x`, `bash -x`, or any
tracing over a command that touches `CLAUDE_CODE_OAUTH_TOKEN`. The wrapper is written so the
value reaches no stream, no argument and no file — a shell trace defeats every one of those
protections from outside the script, and the output lands in a transcript on the host mount.

That is not a hypothetical. It happened during this feature's own development, which is why
it is the one rule stated here.

## If the command is missing

`ccy-claude: command not found` means the mode is off for this project. It is enabled with
one line in the project's tracked `ccy.env`:

```bash
# /workspace/.claude/ccy/ccy.env
export CCY_CHILD_CLAUDE=1
```

The container must then be restarted, because the tooling is installed at startup.

Do not try to recreate the wrapper by hand. Reading the token into a shell variable yourself
is exactly the exposure the wrapper exists to avoid.
