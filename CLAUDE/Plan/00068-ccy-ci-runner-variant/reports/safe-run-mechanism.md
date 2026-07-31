# Plan 00068 — RETRACTED: there is no separate run mechanism. CI invokes `ccy` with args.

**This document specified a purpose-built CI runner that constructs its own hardened `podman run`.
It is retracted in full.** `ccy` is fully featured and is the CI entry point:

```
ccy --headless --prompt "…"      # already exists: claude-yolo:154-155, :198, :503-507
```

Arbitrary Claude args already pass through (`CLAUDE_ARGS`, `claude-yolo:2687`).

## Why the retracted reasoning was wrong

It argued against reusing `claude-yolo:2770-2792` on four grounds. Every one of them is a **defect in
`ccy` to be fixed**, not a reason to build a second runner:

| Retracted objection                                | What it actually is                                                               |
| -------------------------------------------------- | --------------------------------------------------------------------------------- |
| `--device /dev/dri` is unconditional               | **E6** — a confirmed bug. Fix: make it conditional, as the GUI mounts already are |
| ~16 credential prompt sites precede every run path | **Phase 2 / Task 7.3** — exactly what `--non-interactive` is for                  |
| The command is hard-coded to `claude`              | Not a gap: `--headless --prompt` and `CLAUDE_ARGS` already cover it               |
| GUI/SSH mounts are desktop-only                    | Conditional-mount work, the same shape as E6                                      |

Building a parallel runner would have duplicated image resolution, token selection, network handling,
compose lifecycle and container naming — every one of which `ccy` already does — and left the real
defects unfixed in the launcher that desktop keeps using.

## What survives

The **analysis** of which behaviours are unsafe for CI is still valid input to hardening `ccy`. It is
not a specification for a separate component:

- `entrypoint.sh:269-274` sources `ccy.env` **from the checkout** — arbitrary shell from the branch
  under test. Real hardening question for CI use.
- `entrypoint.sh:185-195` symlinks `/root/.claude` into `/workspace`, so session state lands in the
  job checkout.
- Container naming (`get_next_container_name`, no lock; `claude-yolo:2747` force-removes a running
  container) — **C7**, already tracked in Task 7.4.
- `tini` is PID 1 via `Dockerfile:215` and stays that way precisely because `ccy` does **not**
  override `--entrypoint`. **G1 is moot again**: nothing overrides it, so nothing drops `tini`.

## The pattern — fourth instance

| #   | I specified                               | What already existed                      |
| --- | ----------------------------------------- | ----------------------------------------- |
| 1   | A `LABEL` convention for image staleness  | `podman build`                            |
| 2   | Ansible-built CI images                   | the project Dockerfile seam               |
| 3   | Vault + `gh secret set` + expiry metadata | ccy's host-level token store              |
| 4   | A purpose-built CI run mechanism          | `ccy` — fully featured, invoked with args |

The rule that would have caught all four: **before specifying a mechanism, name the existing thing it
replaces and state why that thing cannot do the job.** In all four cases no such statement could have
been written truthfully — the existing thing either already did the job, or had a fixable defect that
was being routed around instead of repaired.
