# Plan 00066: Extend ccy so a CI/GitHub-runner-optimised session launches cleanly

**Status**: Not Started
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

## Overview

`ccy` is this repo's Claude-Code container launcher. It is built for one situation: a
human at a Fedora workstation, at a TTY, with a desktop session. A consuming
repository (`LongTermSupport/actions-hub`, private) needs the same thing on a headless
GitHub Actions runner, unattended, with restricted egress and an MCP server wired in.
Rather than extend `ccy`, that repo built its own: **~1,737 lines** across
`ccy-baseline/Dockerfile` (169), `run-sandbox.sh` (510), `resolve-image.sh` (256),
`entrypoint.sh` (240), and a `policy/` tree (~400). It re-implements image resolution,
the container invocation, credential handling, and the entrypoint — all of which `ccy`
already does.

That is the **fourth** re-implementation of upstream in that estate; the previous three
were deleted for the same reason. The fix is not a better fork. It is to grow the
capabilities `ccy` is missing, **at source, here**, so the consumer deletes its copy and
becomes a caller.

This plan is **design + audit only**. It changes no code. Its output is a reviewed,
hostile-audited design and a task breakdown that a later plan executes.

## Goals

- Establish, with cited evidence, exactly what `ccy` lacks for unattended CI use —
  separating *proven* gaps from *suspected* ones.
- Decide the shape: what becomes a flag, what becomes an image variant, and what must
  NOT be forked. Record the reasoning, not just the choice.
- Preserve, provably, the existing project-extensibility contract: a project's own
  `.claude/ccy/Dockerfile` keeps working, and gains the ability to build on a CI base.
- Produce a task breakdown ordered by dependency, where each task has a verification
  that runs on the **host** (not in a nested container).
- Run the design through an audit/fix loop with each round tracked to a file under
  `reports/`, until a round finds nothing material.

## Non-Goals

- **No code changes in this plan.** Not one line of `claude-yolo`, the libs, the
  Dockerfiles, or the plays. Explicit owner instruction.
- **No work on the `actions-hub` side.** Its deletion is the *consequence*; it is
  tracked in that repo's own plan (lts-infra Plan 00015 / 00022).
- **Not a second launcher.** See Decision 1 — a forked `ccy-ci-runner` script is the
  thing this plan exists to avoid.
- **No changes to Plan 00065's files.** Another agent is active in this repo on the
  Cloud Base blockers. This plan touches `CLAUDE/Plan/00066-*/` only.

## Context & Background

### What `ccy` is, precisely

Two things share the `claude-yolo` name and conflating them has already caused one
wrong design in the consuming repo:

| Thing                          | What it is                                                                                                                                                                                    |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `files/var/local/claude-yolo/` | The **launcher and its image**. `claude-yolo` (2847 lines) + 7 libs + `Dockerfile` + entrypoint. Deployed by `play-claude-yolo.yml`. This is "ccy".                                           |
| a project's `.claude/ccy/`     | That **project's** ccy state (`sessions/`, `history.jsonl`, `settings.json`, `ccy.env`) **and** its own `Dockerfile` — the dev container for working on that repo, `FROM claude-yolo:latest`. |

A CI capability belongs in the first. Putting it in the second means every consuming
repo repeats it, and a repo's dev container carries CI-only weight it never uses.

### Evidence — proven

Every row was read out of the source at `eb14ba2`. Line numbers are in
`files/var/local/claude-yolo/`.

| ID     | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **E1** | **`--headless` already exists — but it is a Claude-Code-invocation mode, not a launcher mode.** It sets exactly two things: `claude -p "$PROMPT"` instead of an interactive invocation (`claude-yolo:2626-2628`), and `-i` instead of `-it` (`2694-2699`). It requires `--prompt` (`728-740`).                                                                                                                                                                                                                                                                                                                   |
| **E2** | **35 interactive `read -rp` prompts across the launcher and libs**, and `HEADLESS_MODE` guards exactly **one** of them (`lib/ssh-handling.bash:357`). The rest fire regardless of `--headless`.                                                                                                                                                                                                                                                                                                                                                                                                                  |
| **E3** | **At least ten of those prompts are `while true` menu loops that spin forever on EOF.** With no TTY, `read` returns non-zero with an empty value, the `case` falls to `*) echo "Invalid choice"`, and the loop repeats — unbounded. Confirmed by replicating the exact loop shape and feeding it `/dev/null`: it span until an injected counter tripped. Sites include `claude-yolo:1104`, `claude-yolo:2011`, `lib/docker-health.bash:162,369,486`, `lib/network-management.bash:271`, `lib/dockerfile-custom.bash:37,117,157,718`. **This is the hard blocker: unattended `ccy` can hang instead of failing.** |
| **E4** | **`ccy` has no MCP support of any kind.** An exhaustive search for `mcp`/`MCP` across `claude-yolo`, all 7 libs, all 4 Dockerfiles and `entrypoint.sh` returns **zero matches**. So "inject MCP into a ccy session" is a net-new capability, not a repair.                                                                                                                                                                                                                                                                                                                                                       |
| **E5** | **`--network` means the OPPOSITE of restriction.** It attaches the container to an additional podman network so it can reach compose services (`claude-yolo:148`, `1801-1812`). The default is `--network podman` (`2516`). There is a *positive* connectivity probe that fetches `http://google.com` and warns if it fails (`2529`). There is no proxy, allowlist, or egress restriction anywhere. **A flag named `--network` that widens reach is a naming trap for anyone implementing restriction.**                                                                                                         |
| **E6** | **`--device /dev/dri:/dev/dri` is passed unconditionally** (`claude-yolo:2773`) — the single occurrence, with no guard. By contrast the GUI socket mounts *are* properly guarded (`2704-2727`). Whether podman treats a missing device node as fatal is **NOT yet verified** — see Task 1.1.                                                                                                                                                                                                                                                                                                                     |
| **E7** | **The entrypoint hard-requires `GH_TOKEN`** and exits 1 without it (`entrypoint.sh:14-17`), then runs `gh auth login --with-token` (`33`) and `gh auth status` (`53`). Both need reachable GitHub. Under a restricted-egress design, GitHub must be allowed or **the container never starts**.                                                                                                                                                                                                                                                                                                                   |
| **E8** | **A daily in-container `npm i -g @anthropic-ai/claude-code@latest`** runs once per 24h per image (`auto_update_claude_code`, `claude-yolo:1254`; `update_claude_inplace`, `1343`). In CI this is non-deterministic and needs npm-registry egress. `CCY_AUTO_UPDATE=0` degrades it to notify-only.                                                                                                                                                                                                                                                                                                                |
| **E9** | **Existing extension seams that a CI mode must reuse, not duplicate.** `CCY_EXTRA_MOUNTS` — env-supplied `-v` tokens (`1781-1790`); `.claude/ccy/ccy.env` — tracked per-project config sourced *in-container* (`entrypoint.sh:265-274`); `CCY_CLAUDE_WRAPPER` / `--supervise` — wraps the `claude` invocation (`2759-2768`, `entrypoint.sh:280-284`); `CCY_CONTAINER_ENGINE`, `CCY_AUTO_UPDATE`.                                                                                                                                                                                                                 |

### Evidence — the consumer already proved each piece

`actions-hub` did not speculate; it built and ran these. That makes them requirements
with a known shape, which is why this plan can be specific.

| Capability           | What the consumer built                                                                                           | Proven                                                                                    |
| -------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| CI tooling in a base | `ccy-baseline/Dockerfile` — bakes `github-mcp-server` v1.7.0 by pinned sha256 (`:118-125`)                        | Builds; the fetch needs release-download egress **at build time**                         |
| MCP wiring           | `entrypoint.sh:173-193` — asserts the binary, checks it advertises `--tools`, writes an MCP config                | Runs; `policy/tool-matrix.sh` + `scripts/mcp/` derive a checked tool allowlist            |
| Egress restriction   | `policy/egress.sh` — `pasta:-T,3128` forwarding container loopback to a host squid                                | Measured on the live runner VM: allowed host tunnels, denied host gets 403 **from squid** |
| Platform-owned layer | `policy/sandbox-overlay.Dockerfile` — built `--network=none`, creates mount points, asserts `claude`/`jq` present | Needed because a repo Dockerfile cannot be trusted to provide platform invariants         |

The last row is the interesting one: the consumer independently discovered that **one
project Dockerfile is not enough** — it needed a platform-controlled layer on top of a
repo-controlled one. `ccy` today has no such concept.

## Technical Decisions

### Decision 1: One launcher, layered images — NOT a `ccy-ci-runner` script

**Context.** The request floated "a `ccy-ci-runner`, a special version of ccy". Taken
literally that is a second launcher.

**Options.**

- **(A) Forked launcher + forked image.** A `ccy-ci-runner` script beside `claude-yolo`.
  *Against:* it is re-implementation #5. The two would drift on every one of the 35
  prompt sites, the token path, the image-version gate, and the podman argv. This repo's
  own `CLAUDE.md` naming rule and the three deletions behind it say precisely this.
- **(B) One launcher, orthogonal flags, layered images.** `claude-yolo` grows
  `--non-interactive`, `--mcp`, `--egress`. CI *tooling* lives in a new image layer
  (`Dockerfile.ci`, `FROM claude-yolo:full` → tag `claude-yolo:ci`) so the desktop image
  does not carry MCP servers it never runs. A project's `.claude/ccy/Dockerfile` may be
  `FROM claude-yolo:ci` and keeps working unchanged.

**Decision: (B).** The variant that is justified is an **image**, not a launcher. Image
layering is already the repo's model (`base` → `full` in the existing `Dockerfile`), so
`ci` is a third stage in an established pattern rather than a new mechanism. Every
behavioural difference becomes a flag on the one code path that is already exercised
daily by desktop use — so CI cannot silently rot.

**Consequence for the three options as posed:** they are not alternatives. A CI image
variant, MCP injection, and egress restriction are three of the four things a CI mode
needs; the fourth (non-interactivity) was not named and is the prerequisite for all of
them.

### Decision 2: `--non-interactive` is separate from `--headless`, and lands first

**Context.** `--headless` (E1) sounds like the flag for this and is not. It describes how
*Claude Code* is invoked (`-p`). The gap is that the *launcher* still prompts (E2, E3).

**Options.** Widen `--headless` to also suppress prompts / add a distinct
`--non-interactive` / infer from `[ ! -t 0 ]`.

**Decision: a distinct `--non-interactive`, and never infer.** Widening `--headless`
silently changes behaviour for existing users who pass it at a TTY. Inferring from
`-t 0` makes behaviour depend on invocation context, which is how a CI hang becomes
unreproducible by hand. An explicit flag is greppable and testable. It lands **first**
because until `ccy` reliably fails instead of hanging, no other CI capability can be
verified unattended.

**Semantics:** under `--non-interactive`, every prompt site becomes one of —
*(i)* satisfied from a flag/env already, *(ii)* takes a documented safe default and logs
that it did, or *(iii)* **fails fast** with a message naming the flag that would have
answered it. Never a silent default, never a wait.

### Decision 3: Egress restriction is independent of CI, and useful on the desktop

**Context.** The consumer's proven egress work exists to defend **prompt injection**:
its AI action files GitHub issues from *system log lines*, which carry
outsider-controlled strings (crafted SSH usernames, User-Agents) into a tool-using model
holding a live write token.

**Decision:** `--egress` is not a CI-only feature and must not be gated behind the CI
variant. The same threat applies to a desktop `ccy` session pointed at untrusted input.
It ships as an independent flag, usable from either. This also means it can be developed
and proven **on the workstation**, where iteration is cheap, before a runner exists.

## Tasks

### Phase 1: Ground the unverified claims (host-run, no nesting)

Everything here runs on the **HOST**. This session is inside a podman container; a nested
`podman` test is not evidence about the host — an attempt to check E6 that way failed for
an unrelated userns/subuid reason and would have been mistaken for a result.

- [ ] ⬜ **Task 1.1**: Resolve E6 — does an absent `/dev/dri` make `ccy` fail?
  - [ ] ⬜ Write `triage.bash` probing: whether `/dev/dri` exists; whether
    `podman run --device /dev/dri` succeeds; whether `--device` with a *missing* node
    is fatal or ignored; podman version.
  - [ ] ⬜ Have the owner run it on the workstation, and on a headless VM if one is up.
  - [ ] ⬜ Record the verdict in `reports/`. If fatal, `--device` must become conditional
    — a one-line change that is otherwise a guaranteed day-one CI failure.
- [ ] ⬜ **Task 1.2**: Enumerate all 35 prompt sites into a table in `reports/`, each
  classified: *on the default launch path* vs *only reachable on an error/recovery
  path*, and *EOF-safe* vs *EOF-spins*. E3 proves the loop shape spins; this task
  establishes **which** sites a CI job would actually hit.
- [ ] ⬜ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and whether the image
  is built by Ansible or on first `ccy` run — this decides where `claude-yolo:ci`
  gets built and whether a CI job ever builds an image (it must not).

### Phase 2: Design `--non-interactive`

- [ ] ⬜ **Task 2.1**: For every site from Task 1.2, specify which of the three
  outcomes (satisfy / default+log / fail-fast) applies, and for fail-fast the exact
  message and the flag that answers it.
- [ ] ⬜ **Task 2.2**: Decide the interaction with `--headless` and `--prompt`
  (orthogonal? does `--non-interactive` imply anything?) and state it explicitly.
- [ ] ⬜ **Task 2.3**: Specify the regression guard. A prompt added later must not
  silently reintroduce a hang — propose a QA gate wired into `qa-all.bash` that fails
  when a `read -rp` exists on a path reachable under `--non-interactive` without a
  guard. Note honestly whether this is statically decidable, and if only partially,
  what the residual risk is.

### Phase 3: Design the image layering + CI variant

- [ ] ⬜ **Task 3.1**: Specify `Dockerfile.ci` as a stage/file `FROM claude-yolo:full`:
  what it adds (MCP server binaries, pinned by checksum), and what it must NOT add.
- [ ] ⬜ **Task 3.2**: Specify how a project selects it, and prove on paper that an
  existing `.claude/ccy/Dockerfile` (`FROM claude-yolo:latest`) is unaffected.
- [ ] ⬜ **Task 3.3**: Address the platform-vs-repo layer problem the consumer hit: is a
  `ccy`-owned overlay applied on top of a project's Dockerfile warranted, or is that
  complexity only justified for untrusted-checkout CI? **Argue both sides** — the
  answer is not obvious and getting it wrong in either direction is expensive.
  - [ ] ⬜ Cover the version-gate interaction: `REQUIRED_CONTAINER_VERSION`
    (`claude-yolo:39`) currently gates one base; state how it behaves with a variant.
- [ ] ⬜ **Task 3.4**: Specify how the CI image is built by **Ansible**, never per-job.
  E8 (daily npm auto-update) and the consumer's build-time release fetch both need
  egress that a locked-down job must not have.

### Phase 4: Design MCP injection

- [ ] ⬜ **Task 4.1**: Specify the interface (`--mcp <name>`? a `ccy.env` declaration?
  both?), and where the config is written given the entrypoint already symlinks
  `/root/.claude` → `/workspace/.claude/ccy` (`entrypoint.sh:183-195`).
- [ ] ⬜ **Task 4.2**: Decide whether tool-level restriction (the consumer's
  `tool-matrix.sh` + checked vocabulary) belongs in `ccy` or stays consumer policy.
  **Default to "stays out"** unless there is a general case — `ccy`'s job is to wire a
  server, not to own one consumer's authorisation matrix.
- [ ] ⬜ **Task 4.3**: State how this serves the ad-hoc desktop case the owner asked
  about ("inject MCP into a standard ccy session"), not just CI.

### Phase 5: Design egress restriction

- [ ] ⬜ **Task 5.1**: Specify `--egress`. Resolve the `--network` naming collision from
  E5 head-on: two flags whose names suggest the same axis and act oppositely is a
  trap. Propose either a rename (with a deprecation path) or names that cannot be
  confused.
- [ ] ⬜ **Task 5.2**: Specify the mechanism, reusing the consumer's *measured* result
  (`pasta:-T,<port>` forwarding container loopback to a host proxy) rather than
  re-deriving it. Record why `--map-host-loopback` was rejected: it was measured to
  expose the host's entire loopback.
- [ ] ⬜ **Task 5.3**: Reconcile with E7 — the entrypoint cannot start without reaching
  GitHub — and with E8's npm fetch. State the minimum allowlist for a container that
  merely boots.
- [ ] ⬜ **Task 5.4**: Specify the proof. An egress control asserted but not measured is
  worth nothing; a `triage.bash` must show a denied host actually refused **by the
  proxy** and an allowed host reaching through.

### Phase 6: Audit / fix loop — tracked to files

Each round is a file in `reports/`. A round that finds nothing material ends the loop.

- [ ] ⬜ **Task 6.1**: Round 1 — hostile review of Phases 1-5 (fable). Brief: attack the
  design, not the prose. Hunt specifically for *a true statement about a check
  presented as a stronger statement about the world* — the recurring failure mode in
  this estate, and the reason Task 1.1 exists as a task rather than an assertion.
- [ ] ⬜ **Task 6.2**: Round 1 — independent deep scan (sonnet) for what the author and
  the hostile reviewer both missed. Read the actual source; do not trust this plan's
  own citations.
- [ ] ⬜ **Task 6.3**: Apply round-1 findings. Corrections **append**; never rewrite a
  section a reviewer has already reviewed, or their finding stops referring to a real
  document.
- [ ] ⬜ **Task 6.4**: Repeat rounds until a round finds nothing material. Record every
  round, including the quiet one that ends the loop.
- [ ] ⬜ **Task 6.5**: Final gate — restate the design in one page, and list what a
  **later** implementation plan must prove on real hardware before any task is ✅.

## Dependencies

- **Coordination:** another agent is active in this repo on Plan 00065 (Cloud Base
  blockers). This plan is confined to `CLAUDE/Plan/00066-*/` and on branch
  `plan-00066-ccy-ci-runner`, so it cannot collide with that work.
- **Counter note:** `hooksdaemon.latestPlanNumber` was stale at 63 while 00065 existed
  (00064/00065 arrived via `git pull` from another clone). Reconciled to 65 per
  `mkplan.bash`'s own drift-guard message before scaffolding; this plan is 00066.
- **Consumes:** the measured results from lts-infra Plan 00015 (egress probes V4-V9) and
  Plan 00022 (the re-implementation audit).
- **Blocks:** the `actions-hub` deletion of `run-claude-sandboxed` + `ccy-baseline`.
- **Not blocked by** Plan 00065 — this plan writes no plays and provisions nothing.

## Success Criteria

- [ ] Every claim in this plan is either cited to a file:line or explicitly marked
  unverified with a named probe that resolves it.
- [ ] Task 1.1's `/dev/dri` question is answered by a host run, not by inference.
- [ ] The four capabilities each have a specified interface, and it is stated which are
  CI-only and which are generally useful.
- [ ] A reader can say what happens to an existing `.claude/ccy/Dockerfile` — proven by
  reading the resolution path, not assumed.
- [ ] The audit loop has run to a quiet round, with every round on disk in `reports/`.
- [ ] **No source file outside this plan folder has been modified.**

## Risks & Mitigations

| Risk                                                                                        | Impact | Probability | Mitigation                                                                                          |
| ------------------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| Designing from this plan's citations instead of the source, propagating any error in them   | H      | M           | Task 6.2 re-reads the source independently and is told not to trust these citations                 |
| `--non-interactive` scope creep across 35 sites, stalling everything behind it              | M      | H           | Task 1.2 splits default-path from error-path sites; only the former block a first cut               |
| A nested-container test is mistaken for host evidence                                       | H      | M           | Owner instruction, now plan policy: anything needing an engine goes in `triage.bash` for a host run |
| Growing `ccy` for a CI consumer degrades the daily desktop experience                       | H      | M           | Decision 1 keeps CI weight in an image layer; Decision 3 keeps flags opt-in and default-off         |
| `claude-yolo` is 2847 lines with a version-hash gate; a large change is hard to land safely | M      | H           | Phases are independently shippable; each bumps `CCY_VERSION` per `CLAUDE/ContainerRules.md`         |
| The consumer keeps its fork anyway, leaving two implementations                             | H      | L           | Deletion is an explicit success criterion of the *consumer's* plan, gated on this one landing       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00066-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Scaffolded on branch `plan-00066-ccy-ci-runner`; failsafe recovery cron `ffc583d1`.
