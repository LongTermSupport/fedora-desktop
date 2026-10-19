# Plan 00209: agent bus v2 design (opus)

Branch `feat/agent-bus-v2-design` from F44 `442143f5`. Design only; no production code changed.

## Scope change

The task started as Plan 00192 Task 3.3's read-only port spec (`SPEC-agent-bus-read.md`). The
owner's direction changed it before anything was written: a v2 rebuild in Go, designed from the
requirements. No read-port spec was written; what was learnt of the read path is section 4 of the
design.

## What was produced

- Plan 00209 scaffolded by `mkplan.bash`, indexed in `CLAUDE/Plan/README.md`.
- `RESEARCH-pingbus.md`, `RESEARCH-host.md`, `RESEARCH-lxc-robustness.md`,
  `RESEARCH-go-callers.md`: four read-only digests of v1 (by four research agents), with
  `file:line` citations, placeholders only.
- `DESIGN.md`: why v2 (with the measured numbers), requirements R1-R39, shape (four Go binaries:
  `agent-busd`, `agent-bus`, `agent-bus-install`, `pingbus`; packages under `internal/bus/`), the
  read model and its latency budget, state and versioning (all v1 files kept, three new ones, a
  shared fixture corpus), privilege (sudoers rule removed, writes still need the sudo password via
  a root peer of the admin socket), concurrency (single writer, install lock), pingbus v2 (keeper
  plus worker, Go hooks and status line, no Python in members), homeserver interaction,
  migration in eight stages with rollback, test strategy, what v1 does that v2 does not carry,
  observability, owner decisions D1-D13, risks, host checks H1-H10, departures B1-B6.
- `PLAN.md`: phases (design review by Fable, owner decisions, then one phase per stage).

## Key findings

- An open starts Python 7 + C + T + H times, serially: about 22 at six checkouts, three teams and
  six people. The "six seat lists" are six checkouts, not six passes. `declare overview` runs three
  times per open and rebuilds the agent-bus zipapp in each to hash it.
- The import cost (0.36-0.42 s per start) is what is paid, not the interpreter (33 ms).
- `human devices` feeds only a count; the read needs root only because the admin token is the
  `agent-bus` user's.
- The status line runs a Python `pingbus` every second in every team session.

## Not done

- No `qa-all` (as instructed). No push, no merge.
- Fable's review (Task 1.3) and the owner's decisions (Task 1.4) are next.
