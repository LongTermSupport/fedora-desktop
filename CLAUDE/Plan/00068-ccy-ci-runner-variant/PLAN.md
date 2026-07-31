# Plan 00068: Let projects run CI workloads on ccy, without degrading desktop ccy

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

> Scaffolded as 00066; renumbered to 00068 on 2026-07-30 after `F44` independently took 00066. The
> git branch is still `plan-00066-ccy-ci-runner` and `JOURNAL/` bodies still say 00066 — both left
> alone deliberately. Design narration and the correction history live in `JOURNAL/` and in git; this
> document states current truth only.

## Overview

`ccy` (`claude-yolo`) gives every project a customised, high-function YOLO Claude Code environment on
the desktop, built from the project's own `.claude/ccy/Dockerfile`. The goal is to let the **same**
system run CI workloads — deterministic QA tooling **and** claude-powered tasks such as issue triage
and PR review — with a more locked-down posture, and with **zero** effect on desktop.

This is a **design plan**. Nothing in it has been executed, and it implements nothing.

## Ownership model

The governing decision. Everything else follows from it.

| Layer         | Owner           | Responsibility                                                                         |
| ------------- | --------------- | -------------------------------------------------------------------------------------- |
| **The VM**    | Ansible         | podman, the ccy base image, egress controls, the safe-run mechanism, the CI entrypoint |
| **The image** | **the project** | its own Dockerfile, its own tooling, its own MCP servers if it wants them              |
| **The run**   | this estate     | a hardened `podman run` of whatever the project built                                  |

## What `ccy` owes CI — two things

1. **A safe run mechanism** — an Ansible-deployed runner that hardens `podman run` for a
   project-built image: egress posture, run-ID-salted container naming, workspace mount, `tini` as
   PID 1, no writes into the checkout, arbitrary command (not hard-coded `claude`). Asserts every
   precondition and aborts before any container starts.
   → [reports/safe-run-mechanism.md](reports/safe-run-mechanism.md),
   [reports/ci-required-config.md](reports/ci-required-config.md)
2. **A CI entrypoint** — bind-mounted read-only from the VM, so no image anywhere changes. Performs
   no network I/O and no authentication; writes only the minimum local state (`hasCompletedOnboarding`,
   `bypassPermissionsModeAccepted`, `hasTrustDialogAccepted`) without which a TTY-less `claude` blocks
   on prompts. → [reports/ci-entrypoint-spec.md](reports/ci-entrypoint-spec.md)

## Goals

- Establish, with cited evidence, exactly what `ccy` lacks for unattended CI use.
- Specify both deliverables to an implementable standard.
- Keep the per-project Dockerfile seam as the way projects add CI tooling.
- Leave desktop ccy byte-identical.

## Non-Goals

- **No implementation.** No file outside this plan folder is modified.
- **No `LABEL` identity convention** — `podman build` already answers staleness.
- **No ccy-owned CI base image**, no `claude-yolo:ci`, no `Dockerfile.ci` shipped by this repo.
- **No overlay on project Dockerfiles.**
- **No registry / image distribution** — image built and consumed on the same VM.
- **No permission surface** (Decision 4).
- **No changes to `claude-yolo:latest` or to any project image.**

## Constraints

| Constraint                                                                 | Source          |
| -------------------------------------------------------------------------- | --------------- |
| Desktop ccy must not be degraded **in any way** — no context bloat, no MCP | owner, explicit |
| CI must be **more** restricted than desktop                                | owner, explicit |
| CI runs deterministic QA tools **and** claude-powered tasks                | owner, explicit |
| A project's `.claude/ccy/Dockerfile` is **not guaranteed to exist**        | owner, explicit |
| `ccy` in CI is for **trusted automation only** — not a fail-closed sandbox | Decision 4      |

## Context — what `ccy` is

Three layers. The distinction decides everything else.

| Layer          | What it is                                             | Reachable from a project `Dockerfile`? |
| -------------- | ------------------------------------------------------ | -------------------------------------- |
| **Image**      | toolchain — packages, LSP servers, `gh`, `yq`          | **Yes — the seam, by design**          |
| **Entrypoint** | in-container session prep, `gh` auth, trust assertions | No — inherited (`Dockerfile:215`)      |
| **Launcher**   | credential resolution, prompts, container argv, egress | No — never enters the image            |

**The launcher is never on the CI path** — not at job time, not at provision time. Confirmed by
three independent facts: it hard-codes `claude` as the command (`claude-yolo:2792`), it passes
`--device /dev/dri` unconditionally (`:2773`, fatal on a headless runner — E6, owner-confirmed), and
~16 credential-prompt sites precede every run path.

## Technical Decisions

### Decision 1 — Ansible provides the VM, the project drives the image, we provide the run mechanism

**Context**: Phase 3 originally specified a ccy-owned CI base image built by Ansible.
**Decision**: rejected. A project must be able to add CI tooling by editing its own Dockerfile; under
an Ansible-built image it cannot, without a change to this repo and a re-provision. That contradicts
the founding steer.
**Date**: 2026-07-31

### Decision 2 — the `LABEL` identity convention is retired

**Context**: proposed so staleness could be answered from a checkout plus a container engine.
**Decision**: retired. `podman build` **is** the staleness check — it rebuilds exactly the layers
whose inputs changed and is a cache hit otherwise. The convention was only necessary while the image
was assumed to be built out of band.
**Date**: 2026-07-31

### Decision 3 — `--non-interactive` and token-by-value are desktop-only hardening

**Context**: originally scoped as CI enablers.
**Decision**: real defects worth fixing on their own merits, but not CI enablers — the launcher they
live in is not on the CI path.
**Date**: 2026-07-30

### Decision 4 — no permission surface

**Context**: `ccy` runs `claude --dangerously-skip-permissions` unconditionally (`claude-yolo:2792`).
**Decision**: `ccy`'s posture is a coherent trust model premised on the operator owning the
workspace, not a loose default a flag could tighten. Price stated: **`ccy` in CI is for trusted
automation only.**
**Open**: the owner's "CI should be more locked down" steer may reopen this for CI specifically.

### Decision 5 — egress restriction is independent of CI

**Context**: raised as a CI requirement.
**Decision**: it is a runtime property of the `podman run`, useful on the desktop too, and specified
independently of any image work.

### Decision 6 — a named CI entrypoint, selected explicitly

**Context**: three codebases hand-rolled `--entrypoint`; two got it wrong in production.
**Decision**: ship a CI entrypoint and select it explicitly. A project image is `FROM claude-yolo:latest` and therefore **inherits the desktop `ENTRYPOINT`**, so CI must override it or a
job runs `entrypoint.sh` — including `ccy.env` sourced from the branch under test.

## Tasks

### Phase 1 — Ground the unverified claims (host run, no nesting)

- [ ] 🔄 **Task 1.1**: Resolve E6 and collect the remaining host facts.
  - [x] ✅ E6 **confirmed a blocker** by the owner's host run: `EXIT 125 — stat /dev/…: no such file or directory`. A missing `--device` path is fatal to podman, so the unconditional
    `--device /dev/dri` is a guaranteed failure on any headless host.
  - [x] ✅ `probe-engine.bash`, `probe-launcher.bash`, `probe-network.bash` (group C3),
    `probe-label.bash` (group F) written, linted, wired into `triage.bash`.
  - [ ] ⬜ **Owner runs `./triage.bash` on the HOST.** Blocked on a human. Collects image
    provenance, deployed-vs-checkout drift, prompt census, and groups C and F.
  - [ ] ⬜ Record the verdict in `reports/`.
- [x] ✅ **Task 1.2**: Enumerate all 35 prompt sites → `reports/prompt-census-round2.md`.
- [x] ✅ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and how the image is built.

### Phase 2 — Design `--non-interactive` (desktop-only hardening, per Decision 3)

- [x] ✅ **Task 2.1**: Classify every prompt site → `reports/phase2-non-interactive.md`.
- [x] ✅ **Task 2.2**: Specify interaction with `--headless` and `--prompt`.
- [x] ✅ **Task 2.3**: Specify the regression guard for prompts added later.

### Phase 3 — The safe run mechanism

Tasks 3.1–3.4 specified a ccy-owned CI image and are **superseded by Decision 1**. They are recorded
as cancelled rather than deleted because `reports/fable-review-*.md` cite them.

- [x] ❌ **Task 3.1**: ~~Specify `Dockerfile.ci` as a ccy-owned base image.~~ Superseded.
- [x] ❌ **Task 3.2**: ~~Specify how a project selects the CI base.~~ Superseded — selecting a
  ccy-owned CI base put MCP on the desktop image, since a project has only one Dockerfile.
- [x] ✅ **Task 3.3**: Overlay on the project Dockerfile — **decided: no overlay.** Unchanged and
  still correct; the pressure to reverse it came from the superseded premise.
- [x] ❌ **Task 3.4**: ~~Specify how the CI image is built by Ansible, never per-job.~~ Superseded —
  wrong at the ownership level. The egress reasoning survives as a distinction between *phases of the
  job* (build needs registry egress; the workload run is locked down).
- [x] ✅ **Task 3.5**: Specify the safe run mechanism against the corrected model →
  [reports/safe-run-mechanism.md](reports/safe-run-mechanism.md). Written fresh rather than extracted
  from `claude-yolo:2770-2792`, because every reusable part is interleaved with desktop-only concerns
  and refactoring the launcher is forbidden.
- [x] ✅ **Task 3.6**: Specify the CI entrypoint's contents →
  [reports/ci-entrypoint-spec.md](reports/ci-entrypoint-spec.md). Disposition of all 18 desktop
  entrypoint behaviours.
- [x] ✅ **Task 3.7**: Establish the required CI configuration and the fail-fast preflight that
  asserts it → [reports/ci-required-config.md](reports/ci-required-config.md). 15 preconditions
  across three layers (VM / JOB / PROJECT), each with its assertion and remediation. Preflight
  collects every failure, prints them with expected/found/layer/fix plus a secret-free context block,
  and aborts with `EX_CONFIG` (78) before any container starts.

### Phase 4 — MCP injection

- [x] ✅ **Task 4.1**: Specify the interface and where config is written (**not** the symlinked
  location) → `reports/phase45-mcp-and-egress.md`.
- [x] ✅ **Task 4.2**: Tool-level restriction stays **out**.
- [x] ✅ **Task 4.3**: State how this serves the ad-hoc desktop case.

### Phase 5 — Egress restriction

- [x] ✅ **Task 5.1**: Specify `--egress`; resolve the `--network` naming collision.
- [x] ✅ **Task 5.2**: Specify the mechanism, reusing the consumer's measured result.
- [x] ✅ **Task 5.3**: State the minimum boot allowlist. Desktop entrypoint: `api.github.com`. CI
  entrypoint: **empty** — it performs no network I/O and no authentication.
- [x] ✅ **Task 5.4**: Specify the proof — three probes: allowed-through, denied **by the proxy**
  (a `403` from squid, not a timeout), and the bypass attempt dropped by the uid fence.

### Phase 6 — Audit loop

- [x] ✅ **Tasks 6.1–6.4**: Seven rounds, each on disk as `reports/fable-review-N.md` plus
  `reports/sonnet-scan-1.md`. Round 7 returned no material findings.
- [x] ✅ **Task 6.5**: One-page restatement → `reports/one-page-restatement.md`; hardware-proof list
  → `reports/hardware-proof-checklist.md`.

### Phase 7 — Corrected design

- [x] ✅ **Task 7.1**: Restate the thesis; add E10 (the `--dangerously-skip-permissions` axis).
- [x] ✅ **Task 7.2**: Re-run the prompt census with a corrected pattern.
- [ ] ⏸️ **Task 7.3**: Re-scope `--non-interactive`. Classification complete; the three fix items are
  **deferred to the implementation plan** — they edit `claude-yolo`, which this plan's Non-Goals
  forbid and which needs a `CCY_VERSION` bump.
- [x] ✅ **Task 7.4**: Capabilities → `reports/task74-capabilities.md`. Token from environment (C5);
  run-ID-salted container names (C7); `--no-network` for CI (C8); `trap`-based compose teardown (C10);
  self-hosted-only image distribution (C6); and **CI must not write `.claude/ccy/` into the checkout**
  — decided on evidence, since that path is read *and executed* (`entrypoint.sh:269-274`).
- [x] ✅ **Task 7.5**: Re-order per C11.

## Open decisions — owner

1. **Confirm the ownership model** in Decision 1. Everything in Phase 3 now rests on it.
2. **Does "more locked down" reopen Decision 4** for CI specifically?
3. **Where the CI entrypoint lives** — recommended: bind-mounted from the VM, which keeps desktop
   byte-identical and lets the mechanism own the `podman run` argv.

## Proof obligations — outstanding

Nothing in this plan has been executed. Full list: `reports/hardware-proof-checklist.md`.

| ID    | Claim                                                             | How it gets settled                    |
| ----- | ----------------------------------------------------------------- | -------------------------------------- |
| E1    | Task 1.1's host facts                                             | `triage.bash` — owner run              |
| C3    | `--network pasta:…` and `--network <name>` are mutually exclusive | `probe-network.bash` — same run        |
| G1    | `--entrypoint` replaces the whole vector, dropping `tini`         | cheap, read-only; not yet wired        |
| G2/G3 | `claude` blocks without the onboarding / trust flags              | interactive, with the owner            |
| B1–B4 | Spin-vs-abort behaviour of the launcher                           | interactive; needs real quota          |
| C1/C2 | pasta port-forwarding and loopback exposure                       | needs a host listener; borrowed        |
| F1–F4 | Label-reader behaviour                                            | **no longer CI blockers** (Decision 2) |

## Dependencies

- **Blocked on**: the owner's host run of `triage.bash`; `lts-infra` not being checked out, which
  makes a class of cross-repo citations unverifiable (`reports/cross-repo-citation-status.md`).
- **Blocks**: the ccy CI **implementation** plan, not yet created.

## Success Criteria

- [x] ✅ Every claim is cited to a `file:line` or explicitly marked unverified.
- [x] ✅ Both deliverables have an implementable specification.
- [x] ✅ A reader can say what happens to an existing `.claude/ccy/Dockerfile`: **nothing**.
- [x] ✅ The audit loop ran to a quiet round, every round on disk.
- [x] ✅ Desktop ccy is provably unaffected — no image and no launcher change is proposed.
- [ ] ⬜ Task 1.1's host facts are answered by a run, not by inference.
- [ ] 🚫 **No source file outside this plan folder has been modified.** Mis-specified at birth: it
  reads as a deliverable but is a Non-Goal restated. Retired with this plan.

## Risks & Mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                               |
| --------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------ |
| A design defect survives because reviews audit self-consistency | H      | H           | **Materialised 3×.** Audit against the owner's steer, not the document   |
| CI writes into the job checkout and pollutes desktop            | H      | M           | No `/workspace` symlink, no `ccy.env` sourcing, no `.claude/ccy/` writes |
| `tini` dropped by the entrypoint override                       | M      | H           | Mechanism sets `--entrypoint /usr/bin/tini`; G1 measures it              |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Run-ID-salted names; never `get_next_container_name` (C7)                |
| Specifications rot before implementation                        | M      | M           | Implementation plan created as soon as the host run lands                |

## Delivery & Milestones

- `dddeb0c` — Phase 3 redone as the safe run mechanism
- `6ece34d` — D33: the project drives the image
- `e9acdf3` — D32: CI rebuild/relaunch unconfirmable
- `93bec3f` — D29: the CI entrypoint specified
- Blow-by-blow: `JOURNAL/`
