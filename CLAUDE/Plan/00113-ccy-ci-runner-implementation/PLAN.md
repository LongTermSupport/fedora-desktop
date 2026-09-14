# Plan 00113: ccy CI runner — implementation

**Status**: Not Started
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

Plan 00068 specified a non-interactive `ccy` a CI job can invoke, and stopped
there deliberately: its Task 4.1 is "create the implementation plan" and its
Dependencies name "the ccy CI implementation plan, not yet created". This is it.
Nothing here is new design — every decision it builds to is already settled and
cited, and **where this plan and 00068 disagree, 00068 wins**; a disagreement is
a defect in this plan, to fix here rather than to reason around.

The product is `files/var/local/claude-yolo/claude-yolo`, the launcher. Today it
negotiates with a human at roughly six credential-resolution sites, assumes a TTY,
probes for a GUI, and reports the compose block's exit status rather than the
container's. A GitHub Actions self-hosted runner can drive none of that.

Two things make this worth doing now rather than later. The restriction mechanism
stopped being a guess on 2026-08-10, when `--disallowedTools` was **measured** to
compose with `--dangerously-skip-permissions` — so the agent can keep the flag
that removes the hang risk *and* lose the tools it must not have. And Plan 00089
has already landed the token-first half of the credential story, so the
launcher's hardest dependency is in place.

**Read before starting**, in this order — none of it is restated here:
[00068 DECISIONS.md](../00068-ccy-ci-runner-variant/DECISIONS.md) ·
[reports/ci-flow.md](../00068-ccy-ci-runner-variant/reports/ci-flow.md) ·
[reports/ci-tool-surface.md](../00068-ccy-ci-runner-variant/reports/ci-tool-surface.md)

## Goals

- A CI invocation path through `ccy` that completes unattended with no TTY, no
  GUI probe, and no prompt reachable on the path CI takes.
- The **container's** exit status is the job's exit status.
- The per-event tool surface of `reports/ci-tool-surface.md` is imposed and
  **asserted at startup**, with every assertion able to fail.
- Every change is exercised by something that fails when the change is reverted.

## Non-Goals

- **No re-architecture of the interactive path.** CI is added alongside it; a
  desktop user's experience of `ccy` does not change.
- No egress allowlist. Decision 8 dropped `--egress` on measured cost, and the
  safety story moved to the tool surface (Decision 9).
- No runner provisioning, no workflow YAML, no token store. Those live in
  lts-infra; this plan ends at the launcher's contract.
- No MCP server implementation. Class B needs one server configured; which one,
  and who runs it, is lts-infra's.
- No new `ccy` subcommand where a flag or an env var does.

## Tasks

### Phase 0: Establish the facts this plan assumes

- [ ] ⬜ **Task 0.1**: HOST — run 00068's `triage.bash` and record what it
  actually reports. Its probes have never been run against the current launcher;
  §9 obligations B1–B4 and C1/C2 are all still open
- [ ] ⬜ **Task 0.2**: Instrument the CI path and **count the prompt sites**.
  `reports/ci-flow.md` says about six, and says of itself that this is "a
  derivation, not a measurement: confirm by instrumenting the CI path before
  implementing". A derivation that turns out to be fourteen changes Phase 1's shape
- [ ] ⬜ **Task 0.3**: Confirm the CLI about to run exposes every
  security-carrying flag the design uses. ccy auto-updates Claude Code daily, so
  the binary is not the one 00068 measured

### Phase 1: The launcher's non-interactive path

- [ ] ⬜ **Task 1.1**: Decide how CI announces itself — one env var, checked at
  one chokepoint, never a TTY test. A `[ -t 0 ]` heuristic silently changes
  behaviour when a human pipes something in
- [ ] ⬜ **Task 1.2**: Close every prompt site Task 0.2 measured. Each must
  **fail fast naming the missing input**, never default and continue
- [ ] ⬜ **Task 1.3**: The fixed flag set (`reports/ci-flow.md`): no
  `/dev/dri` (measured `exit 125` headless), `-i` never `-it`, no preflight
- [ ] ⬜ **Task 1.4**: Propagate the container's exit status, not the compose
  block's (defect 4). **The test must fail before the fix** — a container exiting
  non-zero while the job reports success is the whole point
- [ ] ⬜ **Task 1.5**: The unattended-launch hygiene fixes, four cited defects
  ([DECISIONS.md §7](../00068-ccy-ci-runner-variant/DECISIONS.md)). The
  container-naming race is primarily fixed by serialising jobs on the runner
  (lts-infra Plan 00030 Task 2.8) — do not re-solve it here
- [ ] ⬜ **Task 1.6**: CCY version bump (mandatory when
  `files/var/local/claude-yolo/claude-yolo` changes) and QA

### Phase 2: The tool surface

- [ ] ⬜ **Task 2.1**: One list per class, and every layer derived from it —
  flag string, startup assertion and documentation generated from one place,
  never three hand-kept copies
- [ ] ⬜ **Task 2.2**: The four startup assertions of
  [reports/ci-tool-surface.md](../00068-ccy-ci-runner-variant/reports/ci-tool-surface.md).
  Including assertion 3, **assert `Bash` is PRESENT for class A** — the one an
  implementer will skip as redundant, and the only one that catches an
  over-tightened denylist. Without it a class-A job whose `Bash` went missing
  would skip the suite and pass green
- [ ] ⬜ **Task 2.3**: MCP by `--mcp-config` + `--strict-mcp-config`, at server
  granularity. **No `mcp__*` name in any denylist** — measured downstream to fail
  open on a typo
- [ ] ⬜ **Task 2.4**: Assert every assertion can fail, by breaking each one on
  purpose once and recording the failure. A restriction nobody checks is a
  comment

### Phase 3: Acceptance

- [ ] ⬜ **Task 3.1**: A plan-local `acceptance.bash` that drives the CI path end
  to end and distinguishes a **harness** failure from a **product** failure, as
  Plan 00110's does
- [ ] ⬜ **Task 3.2**: Prove no prompt is reachable — run the CI path with stdin
  closed and assert it neither hangs nor silently defaults
- [ ] ⬜ **Task 3.3**: QA, then `qa-reviewer` over the full diff

## Success Criteria

- [ ] The CI path completes with stdin closed and no TTY, and its exit status is
  the container's
- [ ] Each of the four startup assertions has been **observed failing** when
  deliberately broken — not merely observed passing
- [ ] A class-A job whose `Bash` is missing FAILS rather than skipping the suite
  and passing
- [ ] Every prompt site measured in Task 0.2 either fails fast naming its missing
  input, or is unreachable on the CI path — no third outcome
- [ ] The interactive desktop path is unchanged, demonstrated not asserted
- [ ] `./scripts/qa-all.bash` passes; the CCY version is bumped

## Open Questions For The Owner

Carried from 00068 and unanswered there; each changes work in this plan.

- [ ] ⬜ **The class-A token's scopes.** `reports/ci-tool-surface.md` records
  that `Bash` is a write vector, so removing `Edit`/`Write` does not make a
  class-A job read-only — what stops it pushing is the token. If that token
  carries push scope, the tool surface is decorative. Property of lts-infra's
  token store, unreadable from this repo
- [ ] ⬜ **`ccy.env` sourcing** and **the `/root/.claude` → `/workspace` symlink**
  — DECISIONS.md §8 items 2 and 3, both open owner questions

## Dependencies

- **Specified by**: Plan 00068 — its DECISIONS.md is this plan's input, and wins
  any disagreement
- **Builds on**: Plan 00089 (token-first `ssh-handling.bash`), landed
- **Coordinates with**: lts-infra Plan 00030 (the workflow and runner side; job
  serialisation fixes the container-naming race)
- **Do not duplicate**: Plan 00080 (ccy network isolation), 00092 (child-claude
  spawn mode), 00093 (version gate) — different subsystems

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00113-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
