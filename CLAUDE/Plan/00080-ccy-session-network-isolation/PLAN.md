# Plan 00080: ccy session network isolation

**Status**: Dormant
**Created**: 2026-08-20
**Owner**: joseph
**Priority**: Low

> Dormant: decided (Task 2.2, a network per session) and parked at Low priority until the
> owner schedules Phase 3. The owner's judgement is that sessions on one machine reaching
> each other is not a significant attack vector.

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
  `ea7ba129` (CCY 2.5.2 → 2.6.0) added the line because `--connect` failed with
  `"pasta" is not supported: invalid network mode` — pasta cannot join networks
  after container start. The shared L2 domain is a **side-effect of preserving
  `--connect`**, not a judgement about isolation; any option that removes the
  bridge must answer for `--connect`
- **F2b** — consequently **`--no-network` really does isolate**: it leaves
  `NETWORK_FLAG` empty, the `elif` at 2677 is skipped, and the session gets
  pasta. The runtime message `✓ Skipping network connection` is defensible after
  all

> **Correction.** F1/F2/F2b previously said the opposite. Cause: an assignment
> list read from a `grep … | head -n 30` **truncated at exactly 30 lines**, whose
> decisive eleventh entry was cut — a truncated result read as exhaustive, which
> is this repo's own defect class. Full account:
> `JOURNAL/00080-Journal-26-08-20.md`.

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
- **F5** — zero listening TCP sockets in that session; every socket ESTABLISHED
  outbound to `:443`. One idle session — superseded in scope by **F22**
- **F6** — the ARP table holds **only the gateway**: no L2 frame has ever been
  exchanged with a peer. Says nothing about whether it *could* be — that is H1

**F4 + F5 are the shape of the answer**: the path is open by construction, and
whether anything is listening at the end of it is the variable. That makes H3
the fact worth spending effort on, not H1.

**From host triage, run 1** (`logs/network-isolation-triage.log`, gitignored):

- **F18** — **five** live CCY sessions, and the `podman` bridge reports **5
  members**; every other network on the host has **0**. F17 confirmed at fleet
  scale: all sessions share one L2 segment, and the app networks are idle
- **F19** — the probed session had **zero listening TCP sockets**, consistent
  with F5 — but coverage was **1 of 5** (see F20). Superseded by F22
- **F20** — the `ccy=true` rollout is **partial**: only the session relaunched
  under 3.40.0 carries the labels; the other four predate it and show empty
  `project=`/`github=`. Exactly what the 3.40.0 changelog predicted
  ("containers started by an earlier CCY carry none of them until restarted"),
  now observed. **This invalidated run 1's decisive probe** — see the
  correction below
- **F21** — `podman network ls --format '{{.NetworkID}}'` is invalid for this
  Podman (`rc=125`, *"can't evaluate field NetworkID"*), so run 1's network
  inventory was **absent**, not empty. Field is `.ID`; fixed

> **Correction (my probe, not the host).** P4 filtered on `ccy=true` and guarded
> only the **empty** case, so a 1-of-5 sample printed under a 5-of-5 header. Now
> selects the **union** of the label and `podfreeze`'s name pattern, with a
> COVERAGE line. Re-run as F22. Full account: `JOURNAL/00080-Journal-26-08-20.md`.

**From host triage, run 2** (the fixed probe):

- **F22** — **coverage 6 of 6** (2 labelled, 4 matched by name; a sixth session
  had appeared since run 1), and **every one shows `(no listening sockets)`**.
  So across the whole live fleet the listening surface is **empty**: nothing is
  bound for a bridge neighbour to reach. This is the fleet-wide answer F19 could
  not give. **H3 is answered for the idle case**, and the reachability of the
  path (H1) is therefore reachability *to nothing*
- **F23** — with `.ID` the network inventory returns `rc=0`: six bridge
  networks, of which only `podman` has members. Confirms F18's shape

**From host triage, run 3** (2026-09-23 batch, passive; anonymised detail in
`JOURNAL/00080-Journal-26-09-23.md`):

- **F24** — Podman 5.8.7 / netavark 1.17.2: **below** the 2.0 / 6.0 line, so a
  per-session network needs an explicit `--opt isolate=` (the third hypothesis, answered)
- **F25** — the `podman` network: one `/16` bridge, `dns_enabled: false`, no isolate
  option, and its six members are exactly the six CCY sessions (H2 confirmed on the host)
- **F26** — 6 of 6 sessions labelled (F20's partial rollout is over). Five have no
  listeners; one listens on loopback only, which no neighbour can reach
- **F27** — one project network holds 9 members, a compose stack this repo does not own
- **F28** — P13: 3 projects persist a non-default network and none the default, so
  `--connect` is live and any bridge-removing option must keep it (narrowed by F31)

**From host triage, run 4** (2026-09-24, `--reachability`, all probes). Per-probe evidence
and limits are in `subagent-reports/260924-triage-findings-opus-5.5.md`:

- **F29** — everything on `podman` is a CCY session: 5 sessions, 5 distinct projects, two
  GitHub identities. So the bridge crosses project boundaries, and identity boundaries too
- **F30** — P4 coverage is 5 of 5, and nothing a neighbour can reach is listening, for the
  third snapshot running. One session runs a database bound to loopback only
- **F31** — preferences are written **only** by `--connect`
  (`network-management.bash:389,417,428`) and are applied as a *launch-time* `--network`
  (`claude-yolo:2186-2199`). 4 projects have one. So what the bridge buys is the **first,
  mid-session** attach. Recorded projects join at launch without it
- **F32** — P6 `REACHED`: **H1 confirmed on the host**
- **F33** — P7 is **confounded**. The one-shot `nc -l` listener was consumed by P6, so the
  cross-network silence cannot be read as isolation
- **F34** — P8 rc=0: `network connect` works from a user-created bridge (U6)
- **F35** — P9: a fresh network has egress and the host alias (H5 in part)
- **F36** — P10: a fresh network is `dns=true`, `opts={}` (U9's premise)
- **F37** — P11 **measured nothing**. The container had no `--rm` and was run `-d`, so there
  was no client to kill (pkill rc=1). The only fact: `network rm` refuses (rc=2) while a
  container is attached
- **F38** — P12: the config paths probed were wrong (rc=2, not absence). The 3 networks with
  no members hold no bridge interface

> **The snapshot caveat is now the whole of the remaining risk.** Six sessions
> were sampled while none happened to be running a dev server. F22 establishes
> the **floor is empty in normal use**, not that a listener can never appear. A
> `npm run dev` bound to `0.0.0.0` inside any session would still be reachable
> from all five others, and nothing in the current setup would prevent or
> reveal that.

## Hypotheses

Each needs a probe before it becomes a fact. None is a decision input until it
does.

- **H1** — two containers on the shared `podman` bridge can reach each other's
  TCP ports by IP. **Confirmed on the host by P6 (F32)**, between probe
  containers on the shared bridge
- **H2** — name resolution does **not** work on the *default* network.
  **Confirmed twice**: Podman's docs say the default `podman` network "does not
  support dns resolution", and the repo already encodes it —
  `ensure_network_dns()` returns early for that network
  (`network-management.bash:751`). So cross-session reach, where it exists, is
  **by IP only**
- **H3** — the processes CCY actually runs bind to `0.0.0.0` rather than
  `127.0.0.1`. **ANSWERED for the idle fleet by F22**: 6 of 6 live sessions have
  *no* listeners at all, so there is nothing bound to either address. What
  remains is not a hypothesis but a **conditional**: *if* a session runs a dev
  server on `0.0.0.0`, five neighbours can reach it
- **H4** — a per-session network can be created and removed without leaking on
  abnormal termination (`SIGKILL`, OOM, power loss). **NOT settled**: P11 was
  defective (F37). Matters only if Task 2.2 creates networks
- **H5** — a per-session network reaches the internet and the host identically.
  **Partly confirmed** (F35) for a default-option network. It is not confirmed
  for one with `--opt isolate=` or `--disable-dns`

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

**Consequence of each outcome** (corrected against F23 — see below):

| Outcome                      | What happens to the `podman` row       | `podfreeze` change needed                                       |
| ---------------------------- | -------------------------------------- | --------------------------------------------------------------- |
| Shared default stands (D2#6) | stays, ≈ "did not join a project net"  | **relabel** — one line                                          |
| Per-session nets (D2#2)      | **fragments** into one row per session | **suppress single-member network rows** — more than a relabel   |
| Revert to pasta (D2#4)       | loses every CCY member                 | **none** — cleanest outcome; row becomes genuinely non-CCY only |

> **Correction.** The per-session row originally read *"dissolves … no `podfreeze`
> change needed"*. F23 shows it **fragments** into N single-member rows instead —
> so that outcome needs *more* work than the status quo, not none. Full account:
> `JOURNAL/00080-Journal-26-08-20.md`.

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
- [x] ✅ **Task 1.3**: User runs `triage.bash` on the HOST; convert confirmed
  hypotheses into numbered facts, and record what was refuted. Runs 1 to 3
  gave F18 to F28 (two probe defects fixed along the way; see the journal).
  **Run 4** (`--reachability`) gave F29 to F38. H1 is confirmed and H5 is partly
  confirmed. P7 and P11 were **defective** (F33, F37), so the per-session isolation
  default and H4 are still open. Both matter only if Task 2.2 creates networks. They
  are carried into Task 3.1 as a precondition, not left as findings

### Phase 2: Decision gate

- [x] ✅ **Task 2.1**: Write the threat model from the facts — concretely what
  cross-session reachability gains and does not gain. If the honest answer is
  "little in practice", say so rather than inflating it. **Done:
  [`THREAT-MODEL.md`](THREAT-MODEL.md)**. Little in practice today, and one bind
  flag away from a cross-project, cross-identity exposure
- [x] ✅ **Task 2.2**: **DECISION (owner, 2026-09-24): Option 2, a network per
  session**, because it is the only option that keeps mid-session `--connect`. The
  networks are named with the prefix `ccy-isolate-`, and `podfreeze` ignores that prefix
  (D1), so the menu gains no per-session rows. **Priority: Low.** Nothing listens that a
  neighbour could reach (F22, F26, F30), and every session runs as the same user on one
  machine. The plan is parked until the owner schedules Phase 3. What would change the
  answer, and the other options, are in [`DECISIONS.md`](DECISIONS.md)

### Phase 3: Implement (only if Task 2.2 says so)

- [ ] ⬜ **Task 3.1**: Per-session network creation + attach in the launcher,
  with a cleanup path that survives abnormal exit — the leak is the risk, not
  the create. **Precondition** (if Option 2 or 5 is chosen): fix P7, P10, P11 and P12
  per the findings report's "Probe defects" list, and re-run `--reachability`, so that
  the isolation default and H4 are measured, not assumed
- [ ] ⬜ **Task 3.2**: Confirm `--network`, `--no-network`, `--connect` and the
  compose auto-detect still behave; `CCY_VERSION` minor bump + changelog
- [ ] ⬜ **Task 3.3**: `deploy.bash` + `acceptance.bash` for this plan; QA;
  `qa-reviewer`

### Phase 4: Close out

- [ ] ⬜ **Task 4.0**: Document that `--no-network` is the *isolating* mode
  (F2b) — `docs/ccy.md` and the `--help` line both describe it only as "skip
  auto-detection", which undersells it: it is currently the one way to run a
  session that no other container can reach
- [ ] ⬜ **Task 4.1**: Apply the D1 consequence to `podfreeze` per the outcome
  table in D1 — relabel (shared stands), suppress single-member network rows
  (per-session), or nothing (pasta). Decided: per-session, and `podfreeze` ignores
  networks prefixed `ccy-isolate-`. Whichever it is, `--network <name>` stays
  accepted on the command line: this curates the *menu*, it never removes a
  capability
- [ ] ⬜ **Task 4.2**: Update `docs/ccy.md`'s networking + security sections;
  mark plan Complete and move to `Completed/`

## Dependencies

- Depends on: Plan 00079 (produced F15/F17; its `podfreeze` is the consumer)
- Blocks: nothing

## Success Criteria

- [ ] H1–H5 are settled by a HOST `triage.bash` run, not by argument. **Not met**:
  H1, H2 and H3 are settled. H5 is partly settled. H4 is open because P11 was
  defective (F37)
- [x] A written threat model that a reader can disagree with on the evidence
  ([`THREAT-MODEL.md`](THREAT-MODEL.md))
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
- Host triage runs 1 to 4 converted to F18 to F38. Threat model written. Task 2.2
  options prepared for the owner
