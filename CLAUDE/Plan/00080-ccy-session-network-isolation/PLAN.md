# Plan 00080: ccy session network isolation

**Status**: In Progress
**Created**: 2026-08-20
**Owner**: joseph
**Priority**: Medium

## Overview

Every CCY session launched without `--network` joins the **same** Podman bridge.
This was established rather than assumed: `podman network ls` lists `podman`
once, with a single NETWORK ID and the `bridge` driver, and a host triage run
found seven CCY sessions attached to it simultaneously (Plan 00079, F17). It is
Podman's default for a container started with no network flag, not a choice CCY
makes — but the consequence is that unrelated Claude sessions share one L2
domain by default.

Each session holds an Anthropic OAuth token, a `gh` token, mounted SSH private
keys, and a read-write project tree, and commonly runs dev servers and a
Playwright browser. Whether cross-session reachability matters therefore depends
on what those processes actually bind to, which is a **question of fact this
plan must settle rather than reason about**.

The plan is **research-gated**. It ends in one of two places, and both are
acceptable outcomes: either CCY gains a self-scoped per-session network, or the
shared default is recorded as adequate with the reasoning written down so the
question does not get re-opened from scratch. The decision gate is Phase 2.

## Goals

- Establish, from evidence, what two CCY sessions on the shared `podman` bridge
  can actually do to each other — and what they cannot
- Decide whether CCY should give each session its own network, on a threat model
  rather than on instinct
- If yes: implement it without introducing a resource leak on abnormal exit
- Record the decision either way, so this is settled rather than revisited

## Non-Goals

- **Not** hardening what a CCY session may reach on the *internet*. That is a
  separate and larger question, already open elsewhere (Plan 00068 / the CI
  runner work) and deliberately untouched here
- **Not** changing `--network <compose-net>` or `--connect`. A session that
  explicitly joins a project network is doing so on purpose; only the *default*
  is in scope
- **Not** container-to-host hardening, mount exposure, or the token model — all
  covered by `docs/ccy.md`'s existing security model
- **Not** a `podfreeze` redesign. The menu consequence is one line, downstream of
  the decision here (see D1)

## Context & Background

**From Plan 00079** (the `podfreeze` build), whose host triage produced the
finding that prompted this plan:

- **F17** — `podman` is ONE shared bridge network, not a per-container default.
  Single NETWORK ID, `bridge` driver, seven CCY sessions attached at once
- **F15** — a CCY session was attached to a ten-container app compose network,
  so the reverse also happens: a session can sit inside a project's network

Neither fact says anything yet about **reachability**, which is the thing that
actually matters and is not yet established.

**From reading the launcher** (source-grounded, no runtime claim):

- **F1** — there is no "no network" path. `NETWORK_FLAG` starts empty
  (`claude-yolo:1943`) and is only ever set to `--network <name>` for a project
  network. Every other route leaves it empty, so `podman run` is invoked with
  **no** `--network` flag and the container lands on the default `podman`
  bridge. The shared bridge is thus the fallthrough for every case that is not
  an explicit project network
- **F2** — **`--no-network` does not mean "no network"**; it means "skip the
  compose auto-detect" (`claude-yolo:2044-2047`), after which F1 applies and the
  session still joins the shared bridge. The flag's `--help` line and
  `docs/ccy.md:434` both say "Skip network auto-detection" and are correct; the
  **runtime message** — `✓ Skipping network connection (--no-network flag)` —
  is not, and reads as though the container has no network. A user reaching for
  `--no-network` to isolate a session would get the opposite of what the message
  promises. Fixing that string is in scope regardless of how the decision goes
- **F3** — a session's network is *persisted* (`load_network_preference`) and
  re-applied on the next launch, so a session that joined a project network once
  keeps doing so without the flag

## Hypotheses

Each needs a probe before it becomes a fact. None is a decision input until it
does.

- **H1** — two containers on the shared `podman` bridge can reach each other's
  TCP ports by IP
- **H2** — name resolution between them does **not** work on the *default*
  network (`aardvark-dns` is understood to serve user-created networks; the
  default is believed to differ) — so reachability, if any, is by IP
- **H3** — the processes CCY actually runs bind to `0.0.0.0` rather than
  `127.0.0.1`, making H1 exploitable rather than theoretical
- **H4** — a per-session network can be created and removed at launch/exit
  without leaking on abnormal termination (`SIGKILL`, OOM, power loss)
- **H5** — a per-session network does not change what the session can reach on
  the internet or on the host

**H4 is the one that decides feasibility.** CCY runs `--rm`, which handles the
container but says nothing about a network created alongside it.

## Technical Decisions

### D1: the `podfreeze` menu consequence is downstream, not a fix here

**Context**: the initial framing was *"podfreeze should probably skip the podman
network for grouping"*.

**Decision**: do not change `podfreeze` until this plan resolves. Skipping the
row treats a symptom in the wrong file — the row is low-signal *because* the
default groups by accident, and it remains a legitimate group for non-CCY
containers either way.

**Consequence of each outcome**: if per-session networks land, the `podman` row
dissolves for CCY on its own and no `podfreeze` change is needed. If the shared
default stands, the row is worth re-labelling (it means "did not join a project
network"), which is a one-line change made *then*, with the reason known.
**Date**: 2026-08-20

## Tasks

### Phase 1: Research (no code changes)

- [x] ✅ **Task 1.1**: Dispatch a research sub-agent to establish A) what the
  shared bridge permits, B) what CCY does today, C) the options and their crash
  behaviour, D) the `podfreeze` consequence — writing tagged findings to
  `research/findings.md` and rendering **no** verdict
- [ ] 🔄 **Task 1.2**: Write `triage.bash` from the probe list the research
  returns — read-only, HOST-run, logging to this plan's `logs/`
- [ ] ⬜ **Task 1.3**: User runs `triage.bash` on the HOST; convert confirmed
  hypotheses into numbered facts, and record what was refuted

### Phase 2: Decision gate

- [ ] ⬜ **Task 2.1**: Write the threat model from the facts — concretely what
  cross-session reachability gains and does not gain. If the honest answer is
  "little in practice", say so rather than inflating it
- [ ] ⬜ **Task 2.2**: **DECISION**: per-session network, or keep the shared
  default. Record it with the reasoning, including what would change the answer

### Phase 3: Implement (only if Task 2.2 says so)

- [ ] ⬜ **Task 3.1**: Per-session network creation + attach in the launcher,
  with a cleanup path that survives abnormal exit — the leak is the risk, not
  the create
- [ ] ⬜ **Task 3.2**: Confirm `--network`, `--no-network`, `--connect` and the
  compose auto-detect still behave; `CCY_VERSION` minor bump + changelog
- [ ] ⬜ **Task 3.3**: `deploy.bash` + `acceptance.bash` for this plan; QA;
  `qa-reviewer`

### Phase 4: Close out

- [ ] ⬜ **Task 4.0**: Fix the `--no-network` runtime message (F2). Unconditional
  — it misdescribes what the flag does whichever way Task 2.2 goes, and it
  misdescribes it in the direction a user reaching for isolation would be misled
  by
- [ ] ⬜ **Task 4.1**: Apply the D1 consequence to `podfreeze`, whichever way it
  went
- [ ] ⬜ **Task 4.2**: Update `docs/ccy.md`'s networking + security sections;
  mark plan Complete and move to `Completed/`

## Dependencies

- Depends on: Plan 00079 (produced F15/F17; its `podfreeze` is the consumer)
- Blocks: nothing

## Success Criteria

- [ ] H1–H5 are settled by a HOST `triage.bash` run, not by argument
- [ ] A written threat model that a reader can disagree with on the evidence
- [ ] A recorded decision, including the conditions that would reverse it
- [ ] If implemented: a session network cannot outlive its session, proven by
  killing one with `SIGKILL` and re-checking `podman network ls`
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` verdict PASS

## Risks & Mitigations

| Risk                                                   | Impact | Probability | Mitigation                                                                                    |
| ------------------------------------------------------ | ------ | ----------- | --------------------------------------------------------------------------------------------- |
| Per-session networks leak on abnormal exit             | H      | M           | H4 is a gating hypothesis; a sweep-on-launch reaper is the fallback if `--rm` cannot cover it |
| Subnet pool or interface-name exhaustion after N leaks | M      | L           | Establish the pool size and network cap as a fact before implementing                         |
| Change breaks `--connect` or compose attach            | H      | M           | Both are explicitly out of scope for the default change and are acceptance-tested             |
| Research asserts runtime behaviour it cannot verify    | H      | M           | Sub-agent is forbidden a verdict and must tag every claim; host probes settle facts           |
| Threat model inflated to justify the work              | M      | M           | "Little in practice" is an accepted outcome; Task 2.2 may decide to change nothing            |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00080-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created; research dispatched
