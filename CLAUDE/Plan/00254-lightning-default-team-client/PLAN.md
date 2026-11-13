# Plan 00254: Lightning as the default team client

**Status**: In Progress (the thread-root fix is prototyped; owner decisions D1-D4 open)
**Created**: 2026-11-13
**Owner**: joseph
**Priority**: High

## Overview

The owner decided (2026-11-13, GitHub issue #253) that Lightning (`org.lightning_matrix.Lightning`,
Flathub, v0.10.5, installed by `playbooks/imports/optional/common/play-matrix-clients.yml`)
becomes the default Matrix client for a human reading an agent team bus. Element Desktop stays
installed and selectable. Lightning is to be integrated the way Element is: per team, opened from
ccy-teams, with the homeserver and the team CA already in place.

One defect blocks the switch. Lightning's thread panel shows the thread's root message in a card
capped at 190 px and six lines with an ellipsis and no scroll, so a long root (an agent's question
ending in a list of choices) cannot be read while the reply is written. A fix is prototyped
([lightning-thread-root.patch](lightning-thread-root.patch)); the route chosen for it is upstream
first ([RESEARCH-fork-vs-upstream.md](RESEARCH-fork-vs-upstream.md)).

The rest is the #253 integration, in the order the issue set: CA trust, a per-team instance with
a launcher and a pre-filled config, ccy-teams opening a team in Lightning, then the default switch.
The source research behind every mechanism is [RESEARCH-integration.md](RESEARCH-integration.md).

## Goals

- **Hard requirement: one separate Lightning instance per team**, each its own process and window,
  signed in to that team's homeserver only, the way Element runs one profile per team today. The
  owner works one desktop per team. Lightning's multi-account mode (several accounts, one active,
  in one window) does not meet this and is not used.
- A long thread root can always be read in full in the thread panel: it grows to a cap and then
  scrolls, and is never cut off.
- An https team's CA is trusted by that team's Lightning instance, and a play proves it with a
  handshake from inside Lightning's sandbox.
- ccy-teams opens a team (and every team, a window each) in Lightning by default, and in Element
  when the owner picks Element.
- Lightning releases and our upstream report are watched, so a fix or a regression is noticed.

## Non-Goals

- A standing fork of Lightning (rejected in RESEARCH-fork-vs-upstream.md).
- Pre-writing a session token: Lightning keeps tokens in the keyring with crypto-store
  invariants (RESEARCH-integration.md section 2). The human still signs in once per team.
- Removing Element or its plays.
- MatrixRTC calls through a private-CA SFU (Lightning's call websocket uses webpki roots only).

## Context & Background

- Issue #253: request, unknowns, research comment, the owner's verdict after the trial.
- Issue #251 (merged): one Matrix client list for every guard,
  `src-go/internal/matrixclients/clients.json`; Lightning is listed with `keyring: true` and no
  native profile directories yet.
- Element today: `play-agent-bus-element.yml` with `helpers/agent_bus/element.py` (profile and
  launcher per `agent_bus_element_teams` entry), `tasks/element-team-ca.yml` (Plan 00246 X34),
  ccy-teams in `src-go/internal/teams/ops/ops.go` and `src-go/internal/teams/picker/menu.go`.
- Plan 00253 (dormant): the Element fork case study whose conclusions Phase 2 reuses.

## Tasks

### Phase 1: Track Lightning

- [ ] ⬜ **Task 1.1**: Add `Mizerd/lightning` to the watch list ([Watchlist.md](../../Watchlist.md))
  with a scope note: releases (tags `v*`), the thread panel, and our upstream issue once posted.
- [ ] ⬜ **Task 1.2**: Owner decision **D1**, pin the Flatpak commit or not. Recommended: do not
  pin. Fixes, ours included, arrive through Flathub, and the project ships often. Instead, record
  the known-good commit after each accepted update in this plan's journal, and give
  `play-matrix-clients.yml` an optional `lightning_flatpak_commit` host var that, when set, runs
  `flatpak update --commit=<sha>` and masks updates, as the rollback for a bad release.
  - [ ] ⬜ Implement whichever D1 chooses; QA with `./scripts/qa-changed.bash`.

### Phase 2: The thread-root fix

- [x] ✅ **Task 2.1**: Find the cause and prototype the fix: `qml/ThreadPanel.qml` caps the card at
  190 px with `clip` and the body at six lines with an ellipsis, both since the panel was written.
  Patch on upstream `e6ea127a`: [lightning-thread-root.patch](lightning-thread-root.patch), with a
  data-driven QML test. Existing reports searched: none
  ([UPSTREAM-DRAFT-thread-root.md](UPSTREAM-DRAFT-thread-root.md)).
- [x] ✅ **Task 2.2**: Build and test the patch on the host, through IaC: `build-test.bash` (per
  [PlanScriptStandards.md](../../PlanScriptStandards.md)) runs the non-Rust tree's QML test in a
  podman container from `Containerfile.build-test` (Fedora 44, Qt 6 devel RPMs,
  `-DLIGHTNING_ENABLE_WEBRTC=OFF`) through `build-test-inner.bash`. Run on the host at `e6ea127a`:
  RED with the test alone (`theThreadRootGrowsToItsCapThenScrollsAndIsNeverElided`, both rows
  fail; QTest totals 7 passed, 2 failed, 0 skipped); GREEN with the fix (that suite 1/1 in CTest;
  every target built, every CTest named `thread` 10/10).
- [x] ✅ **Task 2.3**: Owner decision **D2**: yes, post the issue now with a suggested fix and the
  grow-then-scroll proposal; the pull request follows once Task 2.2 has real totals.
- [x] ✅ **Task 2.4**: Issue [Mizerd/lightning#75](https://github.com/Mizerd/lightning/issues/75);
  pull request [Mizerd/lightning#77](https://github.com/Mizerd/lightning/pull/77) from a fork,
  one commit on `main` `bc05fb11`, with Task 2.2's totals. Also raised: the side panel's fixed
  640 px width cap, [Mizerd/lightning#76](https://github.com/Mizerd/lightning/issues/76).
- [ ] ⬜ **Task 2.5**: When a release carries the fix and reaches Flathub, confirm it live with a
  long root message, and delete the patch file (keep the upstream link).
- [ ] ⬜ **Task 2.6** (fallback, only if upstream refuses the fix or two releases pass without it,
  and the owner asks): a play that builds Lightning at a pinned upstream commit plus the patch with
  `flatpak-builder` (`org.kde.Sdk//6.11`, rust-stable extension) under a separate app id, installs
  it, and adds that id to `clients.json`. Deleted as soon as a release carries the fix.

### Phase 3: Prove one instance per team (hard requirement; before any launcher work)

- [x] ✅ **Task 3.1** (run 2026-11-13: `--env` does NOT reach the sandbox, `--command=env` does;
  the runtime has the CA bundle and python3; the two probe copies launched with `--env` only
  raised the running Lightning, so the two-instance fact moved to Task 3.2): Write `triage.bash` (plan-local, `_planlib.inc.bash`, read-only apart from
  two probe directories under `~/.var/app/org.lightning_matrix.Lightning/teams-probe/` that it
  removes). Run through `meta-deploy.bash`, never in the container. It establishes:
  - [ ] ⬜ that `flatpak run --env=XDG_DATA_HOME=… --env=XDG_CONFIG_HOME=… --env=XDG_CACHE_HOME=… --command=printenv org.lightning_matrix.Lightning` prints the given values inside the sandbox;
  - [ ] ⬜ that two instances started with different XDG roots both stay running after a settle
    period, each holding its own `instance-matrix-client.lock` under its own data root;
  - [ ] ⬜ that a second launch on the SAME root raises the first and exits (the guard still
    works);
  - [ ] ⬜ whether `/etc/ssl/certs/ca-certificates.crt` and `python3` exist in the runtime
    (Phase 4 needs both);
  - [ ] ⬜ NOT ESTABLISHABLE by script, listed for the owner: two windows on screen, each showing
    its own login page.
  - [ ] ⬜ Add the plan to `meta-deploy.list` in the same commit; ask the team's host agent to run
    `./CLAUDE/Plan/meta-deploy.bash`.
- [x] ✅ **Task 3.2** (run 2026-11-13 with `--command=env`: instances A and B both running at 20 s,
  each holding its own lock under its own probe root; a second launch on root A exited after 1 s
  with A still running; the owner saw two windows, each on its own login page, and their own
  running Lightning untouched): If `--env` does not reach the sandbox, use the fallback and re-run 3.1 with
  it: `flatpak run --command=env org.lightning_matrix.Lightning XDG_DATA_HOME=… XDG_CONFIG_HOME=… XDG_CACHE_HOME=… lightning-matrix`. `env` sets the variables inside the
  sandbox, after Flatpak has set its own, so it does not depend on how Flatpak orders them. Ruled
  out as fallbacks: `--persist` (one mapping per app, not per launch) and instance ids (Flatpak
  only reports one through `--instance-id-fd`; it cannot be chosen). If neither works, the plan
  stops and the owner decides between an upstream `--profile` feature request and Element.
- [x] ✅ **Task 3.3**: Record the result and the chosen launch form in the journal and in
  RESEARCH-integration.md section 1. Chosen form: `flatpak run --command=env <app> XDG_DATA_HOME=… XDG_CONFIG_HOME=… XDG_CACHE_HOME=… lightning-matrix`.

### Phase 4: Team CA trust for Lightning

- [x] ✅ **Task 4.1** (the discovery of https teams and their CAs is one file both clients
  import, `tasks/https-team-ca.yml`; every other file in the directory is removed, because
  Lightning trusts whatever it holds): `tasks/lightning-team-ca.yml`, beside `tasks/element-team-ca.yml`: for each
  https team, place the team CA as one PEM file per team in
  `~/.var/app/org.lightning_matrix.Lightning/teamchat-ca/`. The team's launch sets
  `SSL_CERT_FILE` to the runtime bundle and `SSL_CERT_DIR` to that directory (rustls reads only
  these when set; never set `SSL_CERT_FILE` to the team CA alone, which drops every public root).
- [ ] 🔄 **Task 4.2** (done in code: `src-go/cmd/tls-trust-check` over `internal/tlstrust`,
  built static and run in the sandbox through `flatpak run --command=env`; imported by
  `play-agent-bus-element.yml` under tag `agent-bus-lightning`, Lightning installed there too.
  The host proof is pending its `meta-deploy.list` entry): The play's last step fails unless a handshake from inside Lightning's sandbox
  with exactly those two variables verifies the team URL, and the server certificate carries a SAN
  for the host (rustls has no CN fallback).
- [x] ✅ **Task 4.3**: Docs: a Lightning paragraph in `docs/agent-bus-cross-network.md`; update the
  comment in `play-matrix-clients.yml` that says no CA reaches Lightning.

### Phase 5: Per-team instance, launcher and pre-filled config

- [ ] ⬜ **Task 5.1**: Design note (owner decision **D3**): make the per-team host vars generic over
  the client (one `client:` per team, default `lightning`) instead of a second
  `agent_bus_lightning_teams` list. Write it as a short DESIGN section in this folder first.
- [ ] ⬜ **Task 5.2**: A Go command under `src-go/` (new tooling is Go) that, per declared team,
  writes `teams/<team>/{data,config,cache}` under `~/.var/app/org.lightning_matrix.Lightning/`,
  writes `config/MatrixClient/matrix-client.conf` with `[homeserver] url=<team url>` only when the
  file is absent (Lightning rewrites it and owns it afterwards), and writes the launcher
  `agent-bus-<team>-lightning.desktop` with the Phase 3 launch form and the Phase 4 variables.
  It removes the directories' launcher (never a signed-in data root without the owner's consent)
  for a team no longer declared, as `element.py` does. Unit tests through fakes.
- [ ] ⬜ **Task 5.3**: The play that runs it (an edit to `play-agent-bus-element.yml` generalised,
  or a sibling, decided in 5.1), with the same guard as Element: refuse a user holding pingbus
  bundles, because the instance holds the human's Matrix session.
- [ ] ⬜ **Task 5.4**: Add Lightning's per-team directories to its `native` entry in
  `src-go/internal/matrixclients/clients.json`, so every #251 guard sees a Lightning session.
- [ ] ⬜ **Task 5.5**: Deploy through `meta-deploy.bash`; `acceptance.bash` checks each declared
  team's launcher, directories and config, and the CA handshake; the windows and the login are
  NOT ESTABLISHABLE and listed for the owner.

### Phase 6: ccy-teams opens a team in Lightning

- [ ] ⬜ **Task 6.1**: In `src-go/internal/teams/ops` and `.../picker`, "Open the team" and "Open
  every team, a window each" launch the team's client launcher, Lightning by default; the
  sign-in check (`signInAfter`) learns Lightning's data root. Tests through the existing fakes.
- [ ] ⬜ **Task 6.2**: A way to pick Element for a team in ccy-teams, stored in the team's host vars.

### Phase 7: Switch the default

- [ ] ⬜ **Task 7.1**: Default client becomes Lightning in the host vars defaults,
  `localhost.yml.dist`, ccy-teams and the docs (`docs/playbooks.md` and the agent-bus docs);
  Element remains selectable and its plays unchanged.
- [ ] ⬜ **Task 7.2**: Owner live check, recorded as PASS, FAIL or NOT TESTED: two teams open in
  two Lightning windows at once; an `@agent` mention reaches the agent; a thread reply is
  threaded by pingbus; an edit arrives as an edit; a long thread root reads in full (after Task
  2.5).
- [ ] ⬜ **Task 7.3**: Run the `qa-reviewer` agent over the plan's full diff; resolve its findings.

## Dependencies

- Depends on: issue #251 (merged); Plan 00246's team CA work (X34) for the CA source.
- Related: Plan 00253 (Element fork case study, dormant); Plan 00161 (agent bus Element profiles).

## Technical Decisions

### Decision 1: Upstream first for the thread-root fix

**Context**: the fix is one QML file; the owner wants it before Lightning becomes the default.
**Options considered**: upstream issue and pull request; a carried patch in a local Flatpak build;
a standing fork. **Decision**: upstream first, a short-lived local build only as the fallback
(Task 2.6). The maintainer applies small fixes promptly and releases often, while the file
patched had 79 upstream commits between 2026-08-13 and 2026-11-13, so a carried patch would
conflict on most releases. Full reasoning: RESEARCH-fork-vs-upstream.md. **Date**: 2026-11-13

### Decision 2: One process per team through XDG directories

**Context**: the owner requires a window per team; Lightning has no `--profile`.
**Decision**: per-team `XDG_DATA_HOME`, `XDG_CONFIG_HOME` and `XDG_CACHE_HOME`. Data, settings,
cache, the keyring scope and the single-instance lock all follow them, so each team is a separate
process (RESEARCH-integration.md section 1). Proven on the host in Phase 3 before anything is
built on it. **Date**: 2026-11-13

## Owner decisions

| ID  | Question                                                      | Recommendation                          |
| --- | ------------------------------------------------------------- | --------------------------------------- |
| D1  | Pin Lightning's Flatpak commit?                               | No; an optional rollback pin (Task 1.2) |
| D2  | Post the upstream issue and pull request?                     | Yes, after Task 2.2's build             |
| D3  | Per-team host vars generic over the client, or a second list? | Generic (Task 5.1)                      |
| D4  | Fallback build (Task 2.6) if upstream does not take the fix?  | Only if asked, and deleted after        |

## Success Criteria

- [ ] Two teams run in two Lightning windows at once, each signed in to its own homeserver only.
- [ ] A long thread root is readable in full in the thread panel of the Lightning build in use.
- [ ] An https team's CA verifies from inside Lightning's sandbox, proven by the play.
- [ ] ccy-teams opens teams in Lightning by default and in Element on request.
- [ ] QA passes (`./scripts/qa-changed.bash`, then CI), and the `qa-reviewer` agent's findings
  are resolved.

## Risks & Mitigations

| Risk                                                        | Impact | Probability | Mitigation                                                 |
| ----------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------- |
| `flatpak run --env` does not override the sandbox XDG paths | H      | M           | `--command=env` fallback (Task 3.2), proven before Phase 5 |
| A Lightning release regresses the bus (mentions, threads)   | H      | M           | Watch (Task 1.1), live check (Task 7.2), rollback pin (D1) |
| Upstream does not take the fix                              | M      | L           | Fallback build (Task 2.6); Element stays selectable        |
| Team server certificate lacks a SAN, so rustls refuses it   | M      | M           | The play's SAN check names it (Task 4.2)                   |
| Every team's window groups under one dock entry (shared id) | L      | H           | Accepted; Element has the same limit                       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00254-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Thread-root fix prototyped and the fork question researched (this plan's first commit).
