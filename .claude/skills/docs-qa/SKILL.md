---
name: docs-qa
description: Audit this project's documentation for SSoT violations — conflicting truths, duplicated facts, stale pointers, and verbose comment blocks acting as documentation. Trigger on requests like "audit the docs", "check for conflicting truths", "docs drift", "is this documentation duplicated anywhere".
argument-hint: "[topic or plan folder to focus on]"
disable-model-invocation: false
user-invocable: true
allowed-tools: Task, Bash, Read, Grep, Glob
---

# Documentation QA

This skill is a **shim**. It carries no documentation body of its own — it
exists to dispatch the `hooks-daemon-docs-qa` agent, which does the real
work. See `CLAUDE/DocumentationStrategy.md` for the ruleset it enforces.

## What to do

Dispatch the `hooks-daemon-docs-qa` agent (`Task` tool,
`subagent_type: "hooks-daemon-docs-qa"`) with:

- the scope the user asked for (a topic, a plan folder, or "the whole
  corpus" if unstated)
- a pointer to run the deterministic checks first:
  `hooks-daemon docs-qa --sweep --json`
- a pointer to the comment-block finder for its Decision-7 hunt:
  `hooks-daemon find-comment-blocks <source-root> --json`

```claude-code
/docs-qa                    # full-corpus audit
/docs-qa CLAUDE/Plan/00284   # scoped to one plan folder's topic
```

The agent is **read-only** — it delivers findings **inline, as its final
report**, and never edits anything or writes a report file on its own
initiative. If you want the findings persisted (e.g. to
`untracked/reports/`), name that target explicitly when dispatching it.
Review its report and act on the findings yourself (or ask it to be
dispatched again after you have made changes, to re-verify).

## If the agent is not deployed

`hooks-daemon-docs-qa` ships opt-in (`agents.docs_qa.enabled: false` by
default). If the `Task` dispatch fails because the agent is not found, tell
the user to enable it in `.claude/hooks-daemon.yaml`:

```yaml
agents:
  docs_qa:
    enabled: true
```

then run `hooks-daemon restart` (or the next daemon upgrade/install run) to
deploy it into `.claude/agents/hooks-daemon-docs-qa.md`.

## Bundled finder scripts

`scripts/` bundles thin wrappers over the deterministic CLI, for a human or
the agent to run directly without recalling the exact daemon subcommand:

- `scripts/sweep.sh` — `hooks-daemon docs-qa --sweep --json`
- `scripts/find-comment-blocks.sh <path>...` — `hooks-daemon find-comment-blocks <path>... --json`

Both are finders: they LIST candidates, they never judge content and never
gate a tool call. Judgement is the agent's job (or the reviewing human's).
