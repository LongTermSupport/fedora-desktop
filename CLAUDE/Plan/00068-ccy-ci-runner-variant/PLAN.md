# Plan 00068: Make ccy fully non-interactive so CI can invoke it

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

> Scaffolded as 00066; renumbered to 00068 on 2026-07-30 after `F44` independently took 00066. The
> git branch is still `plan-00066-ccy-ci-runner` and `JOURNAL/` bodies still say 00066 — both left
> alone deliberately. Design narration lives in `JOURNAL/` and git; this document states current
> truth only.

## Overview

`ccy` is fully featured and is the CI entry point. CI does not need a new runner, a new image
mechanism, or a new credential path — it needs `ccy` to run **fully non-interactively**, and it needs
the handful of hard-coded desktop assumptions inside `ccy` made conditional.

This is a **design plan**. Nothing in it has been executed, and it implements nothing.

## Architecture

| Concern                            | Provided by                                        |
| ---------------------------------- | -------------------------------------------------- |
| The VM, config, keys, tokens       | **Ansible**                                        |
| The ccy image Dockerfile overrides | **the project**                                    |
| Invoking the workload              | **the CI runner**, firing `ccy` with specific args |

Everything below is what `ccy` must gain so that third row works unattended.

## What CI needs from `ccy`

1. **A fully non-interactive mode.** Every prompt site either takes its value from config/env or
   **fails fast and loud** with enough context to diagnose it. No spinning, no silent defaults.
2. **Conditional desktop assumptions.** `--device /dev/dri` is unconditional (`claude-yolo:2773`) and
   fatal on a headless runner — E6, confirmed by the owner's host run. GUI and SSH mounts need the
   same treatment.
3. **MCP injection** — specified, Phase 4.
4. **Egress restriction** — specified, Phase 5.
5. **The Task 7.4 capabilities** — concurrency-safe container naming, the `alpine`/`google.com`
   preflight, compose teardown on failure.

## Already solved — do not rebuild

Recorded because this plan specified replacements for all four before checking:

| Concern               | Existing mechanism                                                   |
| --------------------- | -------------------------------------------------------------------- |
| Per-project tooling   | `.claude/ccy/Dockerfile` — the seam `ccy` already has                |
| Image staleness       | `podman build` — it *is* the staleness check                         |
| Token persistence     | `~/.claude-tokens/ccy/tokens/`, `ccy --create-token`, `select_token` |
| Running the container | `ccy` itself — `--headless --prompt`, plus `CLAUDE_ARGS` passthrough |

**Working rule, adopted after four instances**: before specifying a mechanism, name the existing
thing it replaces and state why that thing cannot do the job. If that statement cannot be written
truthfully, the mechanism is not needed.

## Goals

- Establish, with cited evidence, exactly what `ccy` lacks for unattended use.
- Specify a fully non-interactive mode that fails fast and loud.
- Keep desktop `ccy` behaviour byte-identical when the new flags are absent.

## Non-Goals

- **No implementation.** No file outside this plan folder is modified.
- No separate CI runner, CI image, CI entrypoint, or CI credential path.
- No `LABEL` identity convention.
- No GitHub Actions secret for the Claude token.
- No permission surface (Decision 4).

## Constraints

| Constraint                                                                 | Source          |
| -------------------------------------------------------------------------- | --------------- |
| Desktop ccy must not be degraded **in any way** — no context bloat, no MCP | owner, explicit |
| CI must be **more** restricted than desktop                                | owner, explicit |
| CI runs deterministic QA tools **and** claude-powered tasks                | owner, explicit |
| A project's `.claude/ccy/Dockerfile` is **not guaranteed to exist**        | owner, explicit |
| `ccy` in CI is for **trusted automation only**                             | Decision 4      |

## Technical Decisions

### Decision 1 — Ansible provides the VM and its config; the project provides the image; CI fires `ccy`

**Date**: 2026-07-31. Supersedes the earlier "ccy-owned CI base image built by Ansible" direction,
which removed the project's ability to add its own CI tooling.

### Decision 2 — the `LABEL` identity convention is retired

`podman build` is the staleness check. The convention was only necessary while the image was assumed
to be built out of band. **Date**: 2026-07-31

### Decision 3 — `--non-interactive` is the CI enabler

**Retracts the earlier classification of it as "desktop-only hardening".** That followed from
assuming the launcher was not on the CI path. CI invokes `ccy`, so the launcher **is** the CI path
and its prompt sites are the blocker. **Date**: 2026-07-31

### Decision 4 — no permission surface

`ccy` runs `claude --dangerously-skip-permissions` unconditionally (`claude-yolo:2792`). Its posture
is a trust model premised on the operator owning the workspace. Price stated: **trusted automation
only.**
**Open**: the "CI should be more locked down" steer may reopen this for CI specifically.

### Decision 5 — egress restriction is independent of CI

A runtime property, useful on the desktop too, specified independently.

## Tasks

### Phase 1 — Ground the unverified claims (host run, no nesting)

- [ ] 🔄 **Task 1.1**: Resolve E6 and collect the remaining host facts.
  - [x] ✅ E6 **confirmed a blocker** by the owner's host run: `EXIT 125 — stat /dev/…: no such file or directory`. A missing `--device` path is fatal to podman.
  - [x] ✅ `probe-engine.bash`, `probe-launcher.bash`, `probe-network.bash`, `probe-label.bash`
    written, linted, wired into `triage.bash`.
  - [ ] ⬜ **Owner runs `./triage.bash` on the HOST.** Blocked on a human.
  - [ ] ⬜ Record the verdict in `reports/`.
- [x] ✅ **Task 1.2**: Enumerate all 35 prompt sites → `reports/prompt-census-round2.md`.
- [x] ✅ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and how the image is built.

### Phase 2 — `--non-interactive` (the CI enabler, per Decision 3)

- [x] ✅ **Task 2.1**: Classify every prompt site → `reports/phase2-non-interactive.md`.
- [x] ✅ **Task 2.2**: Specify interaction with `--headless` and `--prompt`.
- [x] ✅ **Task 2.3**: Specify the regression guard for prompts added later.
- [ ] ⬜ **Task 2.4**: Specify the fail-fast contract — what each site asserts, the message format,
  and the exit codes. Source material in `reports/ci-required-config.md`, which must be re-scoped
  from "a separate preflight" to "assertions inside `ccy`".

### Phase 3 — RETRACTED

Every task in this phase specified a mechanism that already exists. Kept as cancelled rather than
deleted because `reports/fable-review-*.md` cite them by number.

- [x] ❌ **Tasks 3.1, 3.2, 3.4**: ~~ccy-owned CI base image, selection, Ansible-built.~~ Superseded by
  Decision 1 — the project provides the image.
- [x] ✅ **Task 3.3**: Overlay on the project Dockerfile — **decided: no overlay.** Still correct.
- [x] ❌ **Task 3.5**: ~~A purpose-built safe run mechanism.~~ Retracted — CI invokes `ccy`.
  → `reports/safe-run-mechanism.md`
- [x] ❌ **Task 3.6**: ~~A separate CI entrypoint.~~ Retracted — `ccy` uses its own entrypoint. The
  behaviour analysis survives as hardening input. → `reports/ci-entrypoint-spec.md`
- [x] ❌ **Task 3.7**: ~~A standalone CI preflight.~~ Re-scoped into Task 2.4.
- [x] ❌ **Task 3.8**: ~~Vault + `gh secret set` credential path.~~ Retracted — ccy's host-level token
  store already solves it. → `reports/ci-credential-lifecycle.md`

### Phase 4 — MCP injection

- [x] ✅ **Task 4.1**: Interface and where config is written (**not** the symlinked location).
- [x] ✅ **Task 4.2**: Tool-level restriction stays **out**.
- [x] ✅ **Task 4.3**: How this serves the ad-hoc desktop case.

### Phase 5 — Egress restriction

- [x] ✅ **Task 5.1**: Specify `--egress`; resolve the `--network` naming collision.
- [x] ✅ **Task 5.2**: Specify the mechanism.
- [x] ✅ **Task 5.3**: Minimum boot allowlist — `api.github.com` under the desktop entrypoint.
- [x] ✅ **Task 5.4**: Specify the proof — allowed-through, denied **by the proxy**, and the bypass
  attempt dropped by the uid fence.

### Phase 6 — Audit loop

- [x] ✅ **Tasks 6.1–6.5**: Seven rounds on disk in `reports/`; one-page restatement; hardware-proof
  list.

### Phase 7 — Corrected design

- [x] ✅ **Task 7.1**: Restate the thesis; add E10.
- [x] ✅ **Task 7.2**: Re-run the prompt census with a corrected pattern.
- [ ] ⬜ **Task 7.3**: Re-scope `--non-interactive` per C4/C9. **Un-deferred by Decision 3 — this is
  the core work.** Classification complete; the three fix items (five spin paths, 32 abort sites,
  version bump) are implementation and belong to the implementation plan.
- [x] ✅ **Task 7.4**: Capabilities → `reports/task74-capabilities.md`. Concurrency-safe naming (C7),
  `--no-network` for CI (C8), compose teardown (C10), and **CI must not write `.claude/ccy/` into the
  checkout** — that path is read *and executed* (`entrypoint.sh:269-274`).
- [x] ✅ **Task 7.5**: Re-order per C11.

## Open decisions — owner

1. **Does "more locked down" reopen Decision 4** for CI specifically?
2. **`ccy.env` sourcing** (`entrypoint.sh:269-274`) executes shell from the checkout. Acceptable
   under trusted-automation-only, or gated off in non-interactive mode?
3. **The `/root/.claude` → `/workspace` symlink** (`:185-195`) puts session state in the job
   checkout. Same question.

## Proof obligations — outstanding

Nothing in this plan has been executed. Full list: `reports/hardware-proof-checklist.md`.

| ID    | Claim                                                | How it gets settled                 |
| ----- | ---------------------------------------------------- | ----------------------------------- |
| E1    | Task 1.1's host facts                                | `triage.bash` — owner run           |
| C3    | `--network pasta:…` / `--network <name>` exclusivity | `probe-network.bash` — same run     |
| B1–B4 | Spin-vs-abort behaviour of the launcher              | interactive; needs real quota       |
| C1/C2 | pasta port-forwarding and loopback exposure          | needs a host listener; borrowed     |
| F1–F4 | Label-reader behaviour                               | **not blockers** (Decision 2)       |
| G1    | `--entrypoint` drops `tini`                          | **moot** — `ccy` never overrides it |

## Dependencies

- **Blocked on**: the owner's host run of `triage.bash`; `lts-infra` not checked out, which makes a
  class of cross-repo citations unverifiable.
- **Blocks**: the ccy CI implementation plan, not yet created.

## Success Criteria

- [x] ✅ Every claim is cited to a `file:line` or explicitly marked unverified.
- [x] ✅ A reader can say what happens to an existing `.claude/ccy/Dockerfile`: **nothing**.
- [x] ✅ The audit loop ran to a quiet round, every round on disk.
- [x] ✅ Desktop ccy is provably unaffected when the new flags are absent.
- [ ] ⬜ The fully non-interactive contract is specified site by site (Task 2.4).
- [ ] ⬜ Task 1.1's host facts are answered by a run, not by inference.

## Risks & Mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                         |
| --------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------ |
| A mechanism is specified that already exists                    | H      | H           | **Materialised 4×.** Apply the working rule above before designing |
| A design defect survives because reviews audit self-consistency | H      | H           | **Materialised 3×.** Audit against the owner's steer               |
| Non-interactive mode changes desktop behaviour                  | H      | M           | Flags absent ⇒ byte-identical; Task 2.3's regression guard         |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Run-ID-salted names; never `get_next_container_name` (C7)          |

## Delivery & Milestones

- `89fb5a8` — credential lifecycle (since retracted)
- `9a53608` — PLAN.md crystallised
- `6ece34d` — the project drives the image
- Blow-by-blow: `JOURNAL/`
