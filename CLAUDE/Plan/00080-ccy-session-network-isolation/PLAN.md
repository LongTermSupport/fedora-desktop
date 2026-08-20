# Plan 00080: ccy session network isolation

**Status**: In Progress
**Created**: 2026-08-20
**Owner**: joseph
**Priority**: Medium

## Overview

Every CCY session launched without `--network` joins the **same** Podman bridge.
This was established rather than assumed: `podman network ls` lists `podman`
once, with a single NETWORK ID and the `bridge` driver, and a host triage run
found seven CCY sessions attached to it simultaneously (Plan 00079, F17).

That turns out to be **CCY's own choice, not Podman's default** (F1/F2). Rootless
Podman defaults to *pasta*, which has no virtual network and isolates containers
from each other; CCY explicitly overrides it with `--network podman` so that
`ccy --connect` can attach a running session to a project network later. So the
question is not "can we add isolation" — the isolated default already exists and
was **traded away for `--connect`**. The real question is whether that trade can
be undone without losing what it bought.

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

- **F1** — **the shared bridge is a CCY decision, not a Podman default.** When
  the engine is Podman, `--no-network` was not given, and nothing else selected
  a network, `claude-yolo:2677-2680` *explicitly* sets
  `NETWORK_FLAG="--network podman"`. Rootless Podman's own default is **pasta**,
  which gives no virtual network and therefore isolates containers from each
  other. So the isolated default already exists — CCY opts out of it
- **F2** — **it was traded away to make `ccy --connect` work.** Commit
  `ea7ba129` (CCY 2.5.2 → 2.6.0) added that line because `--connect` failed with
  `"pasta" is not supported: invalid network mode`: pasta cannot join additional
  networks after container start, so the launcher moved to the default bridge to
  keep later `podman network connect` possible. The shared L2 domain is a
  **side-effect of preserving `--connect`**, not a judgement about isolation.
  Any option that removes the bridge must answer for `--connect`
- **F2b** — consequently **`--no-network` really does isolate**: it leaves
  `NETWORK_FLAG` empty, the `elif` at 2677 is skipped, and the session gets
  pasta. The runtime message `✓ Skipping network connection` is defensible after
  all

> **Correction.** F1 and F2 previously said the opposite — that CCY passes no
> `--network` flag and lands on the bridge as a fallthrough, and that
> `--no-network` fails to isolate. Both were wrong. The cause is worth recording
> because it is this repo's own defect class: the assignment list was read from
> a `grep … | head -n 30` whose output was **truncated at exactly 30 lines**, and
> the decisive assignment is the eleventh and last. A truncated result was read
> as exhaustive.

- **F3** — a session's network is *persisted* (`load_network_preference`) and
  re-applied on the next launch, so a session that joined a project network once
  keeps doing so without the flag

**From inside a live CCY session**, read passively from `/proc/net/*` (no
`podman` needed, nothing probed, no peer touched):

- **F4** — the container has one interface `eth0`, a default route via the
  bridge gateway, and an **on-link route for the whole default subnet**
  (a `/16`, mask `0000FFFF`). So every other container on that bridge is
  reachable at L3 **without traversing the gateway** — they are neighbours on
  one flat segment, which is what "shared L2 domain" means concretely.
  `[SOURCE: /proc/net/route, /proc/net/tcp in a live session]`
- **F5** — this session has **zero listening TCP sockets**: no `st 0A` rows in
  either `/proc/net/tcp` or `/proc/net/tcp6`; every socket is an ESTABLISHED
  outbound connection to `:443`. So an **idle** Claude session exposes no TCP
  surface at all. **This is a snapshot of one idle session and is NOT the
  general claim** — a dev server, or `agent-browser`, would add listeners, and
  which address they bind is exactly H3. Read as "the floor is zero", not as
  "there is never anything to reach"
- **F6** — the ARP table holds **only the gateway**, so this session has never
  exchanged L2 frames with a peer container. Consistent with no cross-talk
  happening in practice; says **nothing** about whether it is possible, which is
  the question H1 asks

**F4 + F5 together are the shape of the answer**: the path is open by
construction, and whether anything is listening at the end of it is the
variable. That makes H3 the fact worth spending effort on, not H1.

## Hypotheses

Each needs a probe before it becomes a fact. None is a decision input until it
does.

- **H1** — two containers on the shared `podman` bridge can reach each other's
  TCP ports by IP. **Confirmed on documentation** ("within a bridge network,
  containers can initiate communications with each other"); still needs a host
  probe (P6) because this machine's firewall state is not derivable from docs
- **H2** — name resolution does **not** work on the *default* network.
  **Confirmed twice**: Podman's docs say the default `podman` network "does not
  support dns resolution", and the repo already encodes it —
  `ensure_network_dns()` returns early for that network
  (`network-management.bash:751`). So cross-session reach, where it exists, is
  **by IP only**
- **H3** — the processes CCY actually runs bind to `0.0.0.0` rather than
  `127.0.0.1`. **This is now the decisive unknown.** Nothing in the repo
  evidences it either way, and a live session read from `/proc` while idle had
  **zero** listeners (F5)
- **H4** — a per-session network can be created and removed without leaking on
  abnormal termination (`SIGKILL`, OOM, power loss)
- **H5** — a per-session network reaches the internet and the host identically

**Two hypotheses decide this plan, and neither is H1.** H3 decides whether there
is a problem at all: F4 established that the *path* is open by construction, so
what matters is whether anything is listening at the end of it. H4 decides
whether the obvious fix is viable, since `--rm` covers the container and says
nothing about a network created alongside it.

**A third, found by the research and not anticipated here**: whether this host
is on netavark ≥ 2.0 / Podman ≥ 6.0, where bridge networks became *strictly
isolated by default*. Below that version a per-session network would look
isolated **without being isolated** unless `--opt isolate=` is passed explicitly
— an appearance of a fix, which is worse than no fix. `triage.bash` P1 answers
it in one command.

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

### D2: the option space, reframed by F1/F2

Eight options are laid out with evidence in `research/findings.md` §5. Three are
ruled out **on evidence** rather than left hanging: `--network none` and an
`--internal` network both remove egress, so Claude Code cannot reach the API;
and `isolate=strict` on the shared network is a *cross-network* control that
does nothing to traffic **inside** one network, so it does not address this
problem at all.

That leaves the real choice, and F1/F2 make it a narrower one than it looked:

- **Option 4 — delete the override.** Reverts to pasta, which isolates by
  design. Costs nothing to build; it is a *deletion*. **Breaks `ccy --connect`**,
  which is the exact bug the override was added to fix.
- **Option 2 — a network per session.** Keeps a bridge, so `--connect` still
  works, but with one member. Costs a lifecycle, and inherits the two traps
  above (pre-6.0 isolation, and DNS).
- **Option 5 — a network per project.** Sessions of one project already share
  tokens, SSH key and working tree, so isolating them from each other buys
  little; fewer objects to leak, but "when is the last one out" is harder.

**Option 2 is "keep what F2 bought, drop what it cost".** Which is right turns
on a question the repo does not record: **how often `ccy --connect` is actually
used.** If it is rare, Option 4 is free isolation and less code.
**Date**: 2026-08-20

## Tasks

### Phase 1: Research (no code changes)

- [x] ✅ **Task 1.1**: Dispatch a research sub-agent to establish A) what the
  shared bridge permits, B) what CCY does today, C) the options and their crash
  behaviour, D) the `podfreeze` consequence — writing tagged findings to
  `research/findings.md` and rendering **no** verdict
- [x] ✅ **Task 1.2**: `triage.bash` written from the research's probe list —
  passive by default (P1–P5), active probes behind `--reachability` (P6–P12),
  logging to this plan's `logs/`
- [ ] 🔄 **Task 1.3**: User runs `triage.bash` on the HOST; convert confirmed
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

- [ ] ⬜ **Task 4.0**: Document that `--no-network` is the *isolating* mode
  (F2b) — `docs/ccy.md` and the `--help` line both describe it only as "skip
  auto-detection", which undersells it: it is currently the one way to run a
  session that no other container can reach
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

| Risk                                                     | Impact | Probability | Mitigation                                                                                                                                                                                                                                                                                  |
| -------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Per-session networks leak on abnormal exit               | H      | M           | H4 is a gating hypothesis; a sweep-on-launch reaper is the fallback if `--rm` cannot cover it                                                                                                                                                                                               |
| ~~Subnet pool exhaustion after N leaks~~ — **RETRACTED** | –      | –           | Podman's default subnet pools allow roughly 42,700 allocatable `/24`s. A leak of one network per crash cannot exhaust that in any realistic timeframe. This row was written on instinct and is removed as a decision input; interface-name space is a separate question and is probed (P12) |
| A per-session network *appears* isolated but is not      | H      | M           | Below netavark 2.0 / Podman 6.0, bridge networks are not strictly isolated by default. P1 establishes the version **before** any implementation; if below, `--opt isolate=` must be explicit                                                                                                |
| Per-session networks silently change DNS                 | M      | H           | User-created networks are DNS-enabled, and `ensure_network_dns()` then adds public resolvers. P10 confirms; `--disable-dns` at create time is the fix                                                                                                                                       |
| Change breaks `--connect` or compose attach              | H      | M           | Both are explicitly out of scope for the default change and are acceptance-tested                                                                                                                                                                                                           |
| Research asserts runtime behaviour it cannot verify      | H      | M           | Sub-agent is forbidden a verdict and must tag every claim; host probes settle facts                                                                                                                                                                                                         |
| Threat model inflated to justify the work                | M      | M           | "Little in practice" is an accepted outcome; Task 2.2 may decide to change nothing                                                                                                                                                                                                          |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00080-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created; research dispatched
