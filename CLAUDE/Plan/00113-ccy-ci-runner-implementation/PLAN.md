# Plan 00113: ccy CI runner — implementation

**Status**: Not Started
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

Plan 00068 specified a non-interactive `ccy` a CI job can invoke, and stopped
there deliberately: its Task 4.1 is "create the implementation plan", and its
Dependencies name this plan as the thing it blocks. This is it.
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
has landed the **code** for the token-first half of the credential story. Not the
proof: 00089 is still In Progress and its own success criterion — a `--no-ssh`
launch with `GH_TOKEN` pre-exported, live on HOST — is unticked. Treat the
launcher's hardest dependency as written but unproven, and see Task 0.6.

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
  actually reports. It re-confirms **E1, E6 and C3 only**; those were settled by
  run `20260731-225344`, but Plan 00089 has changed the launcher since, so
  re-confirmation is the point. It discharges **nothing else** — Tasks 0.4 and
  0.5 carry the rest, and `triage.bash` cannot substitute for either

- [ ] ⬜ **Task 0.2**: Instrument the CI path and **count the prompt sites**.
  `reports/ci-flow.md` says about six, and says of itself that this is "a
  derivation, not a measurement: confirm by instrumenting the CI path before
  implementing". A derivation that turns out to be fourteen changes Phase 1's shape

- [ ] ⬜ **Task 0.3**: Confirm the CLI about to run exposes every
  security-carrying flag the design uses, **and capture the tool vocabulary by
  name — once per class, with that class's `--disallowedTools` string applied**,
  never just the default. The class deny-lists are already specified, so this is
  runnable now. Capturing only the default and subtracting would be wrong: the CLI
  **substitutes** narrower tools for a withdrawn capability (removing `Bash` adds
  `Glob` and `Grep`), so subtraction predicts 26 names where 28 were measured and
  assertion 2 would fail on every run. ccy auto-updates Claude Code daily, so the
  binary is not the one 00068 measured — and that measurement recorded counts, not
  names. The names are a hard prerequisite for Task 2.2's assertion 2, which diffs against a
  declared set and cannot be written until the set can be declared

- [ ] 🚫 **Task 0.4**: §9 obligations **B1–B4** — spin-vs-abort. **An interactive
  investigation with the owner, not a hand-over script**, and not something this
  plan may automate. Two constraints from
  `00068 JOURNAL/00068-Journal-26-07-31.md:118-131`, both easy to lose:

  - **The exit code does not discriminate.** A spin bounded by `timeout` exits
    124 — and so does a real session that never spun and was killed by the same
    `timeout`. Only the **captured stdout/stderr** tells them apart, which means
    a real container and a real session on the owner's machine, burning quota.
  - **Nothing here scripts the token store.** B1 and B2 need a specific
    token-file population (≥2 files, then exactly 1), and creating or removing
    those means manipulating the owner's credentials — barred by the same rule
    that keeps this plan away from plaintext secrets. The owner arranges the
    population; this plan observes.

  B3 is two runs (podman with a network, expecting `exit 1` at
  `claude-yolo:2597`, versus Docker with no network, expecting the preflight
  never to run); B4 attempts an outbound connection from inside a
  `--no-network` container. Task 0.2's instrumentation covers the same ground as
  B1/B2 and may be cited here instead of measuring twice — **Task 3.2 may not**:
  it runs after Phase 1 has closed every prompt site, and a post-fix pass cannot
  evidence pre-fix behaviour

- [ ] ⬜ **Task 0.5**: §9 obligations **C1/C2** — borrowed from another repo's
  runner and never re-measured under ccy's container shape. Method: a **listener
  on the host**, which is exactly why `probe-network.bash:224` excludes them (a
  probe that opens host sockets is no longer read-only). C1: under
  `pasta:-T,3128`, exactly one port reaches that listener and no other. C2:
  under `--map-host-loopback`, how much of the host's loopback the container can
  reach. Either stays open until measured — do not close it with an adjacent,
  easier probe

- [ ] ⬜ **Task 0.6**: Discharge Plan 00089's unticked success criterion — a
  `--no-ssh` launch with `GH_TOKEN` pre-exported, live on HOST — or record that
  it failed. This plan's credential story rests on it, and 00089 landed the code
  without the proof. If it fails, Phase 1 gains the fix before it gains anything
  else

- [ ] ⬜ **Task 0.7**: Run `entrypoint.sh` with **no SSH key and no
  `GITHUB_USERNAME`** and record what happens. `reports/ci-flow.md` files this
  under *what this does not settle*: the flow assumes the desktop entrypoint is
  reused unchanged, which its two `-n` guards make plausible and which nobody has
  tested. A Phase 0 fact, not compose work — whatever breaks becomes a Phase 1 task

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

- [ ] ⬜ **Task 2.1**: One place per class, and every layer derived from it —
  flag string, startup assertion and documentation generated from it, never three
  hand-kept copies. It holds **two** things: the **denied names**, which become
  the `--disallowedTools` string, and the **expected observed set** captured by
  Task 0.3, which assertion 2 diffs against. Not two views of one list — the CLI
  substitutes narrower tools for a withdrawn capability, so the observed set is
  not the default minus the denied and must not be computed as such
- [ ] ⬜ **Task 2.2**: The four startup assertions of
  [reports/ci-tool-surface.md](../00068-ccy-ci-runner-variant/reports/ci-tool-surface.md).
  Assertion 2 is a **set diff, not an absence check** — a renamed or newly added
  write primitive is absent-by-name and would pass a denylist green. It must fail
  on an unexpected member, which also means a benign new tool fails the job until
  someone declares it; that review is the point. And assertion 3, **assert `Bash`
  is PRESENT for class A** — formally the presence half of the diff, named
  separately because an implementer reading assertion 2 as "the denylist" drops
  it, and a class-A job whose `Bash` went missing would skip the suite and pass
  green
- [ ] ⬜ **Task 2.3**: MCP by `--mcp-config` + `--strict-mcp-config`, at server
  granularity. **No `mcp__*` name in any denylist** — measured downstream to fail
  open on a typo
- [ ] ⬜ **Task 2.4**: Assert every assertion can fail, by breaking each one on
  purpose once and recording the failure. A restriction nobody checks is a
  comment

### Phase 2b: Compose and networking — the capability, not the negotiation

An owner requirement, not an inference (owner, 2026-08-01, quoted at
[DECISIONS.md §6](../00068-ccy-ci-runner-variant/DECISIONS.md)): *"i would not
assume that CI doesn't need compose or podman network stuff"*. A project whose
`ci.bash` needs postgres needs the services up and the container attached. **What
CI drops is the negotiation, not the capability** — and §6 already gives the
keep/drop split over `lib/network-management.bash`, function by function.

- [ ] ⬜ **Task 2.5**: Keep the mechanism, drop the negotiation, per §6's table —
  which is the authority; the lists below are a summary, so **read it, do not work
  from these bullets alone**. Its line numbers were re-verified 2026-09-14 after
  drifting ~40 lines; the function names are the durable reference.
  - **Keep**: `get_expected_network_name`, `has_compose_files`,
    `_compose_already_running`, `network_has_running_containers`,
    `ensure_network_dns`, `connect_to_network`, `_do_compose_start`
  - **Drop**: the project-name heuristic, the cross-engine mismatch wizard, the
    "select network [0-N]" menus, `offer_compose_start`, **and the `read -rp`
    confirmation inside `_do_compose_start` itself** (`network-management.bash:586`).
    That last one is the trap: the function is kept and its prompt is not, so
    "keep `_do_compose_start`" taken literally ships a `read` onto a stdin-closed
    CI path — the exact hang Phase 1 exists to remove
  - `reports/ci-required-config.md` §4.3(c) and §4.3(f) specified the **opposite**
    ("do not start") until 2026-09-14 and now carry supersession notes. If a
    surviving 00068 report still tells you not to start compose, §6 wins
- [ ] ⬜ **Task 2.6**: **Resolve the network before `podman run`** — it is a
  create-time argument, so either compose starts first or `connect_to_network`
  attaches afterwards. §6 says "pick one deliberately"; record which and why
- [ ] ⬜ **Task 2.7**: CI **declares** its services rather than discovering them,
  in the project's own `.claude/ccy/`. Not a security boundary (§6 item 2) — the
  same category as a project's `Dockerfile`
- [ ] ⬜ **Task 2.8**: Teardown tears down **only what CI started** —
  `CCY_COMPOSE_WAS_STARTED` is already the right shape. A pre-existing service
  the developer was using must survive the job

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
- **Builds on**: Plan 00089 (token-first `ssh-handling.bash`) — code landed, plan
  still In Progress and its live-on-HOST criterion unticked; Task 0.6 closes it
- **Coordinates with**: lts-infra Plan 00030 (the workflow and runner side; job
  serialisation fixes the container-naming race)
- **Do not duplicate**: Plan 00080 (ccy network isolation), 00092 (child-claude
  spawn mode), 00093 (version gate) — different subsystems

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00113-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
