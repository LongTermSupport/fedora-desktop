# Plan 00254: Lightning as the default team client

**Status**: In Progress (every reported fix merged upstream, awaiting a release; D4 open)
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

- [x] ✅ **Task 1.1**: Add `Mizerd/lightning` to the watch list ([Watchlist.md](../../Watchlist.md))
  with a scope note: releases (tags `v*`) and their arrival on Flathub, the thread panel, our
  issues and pull requests, regressions.
- [x] ✅ **Task 1.2**: Owner decision **D1**: do not pin; always take the latest release. The
  maintainer fixed every issue we raised within a day, so keep pulling the latest and keep
  reporting what we find. `play-matrix-clients.yml` sets `flatpak_update: true`, which runs
  `flatpak update` on Lightning each run (changed only when it updated). The rollback pin is not
  built until a bad release calls for it.

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
  All three closed or merged upstream the same day; two more raised since and fixed:
  [#78](https://github.com/Mizerd/lightning/issues/78) (text selection across messages) and
  [#79](https://github.com/Mizerd/lightning/issues/79) (thread list uses half its height).
- [ ] 🔄 **Task 2.5** (waiting: every fix is on upstream `main`, but the latest release is still
  v0.10.5, which carries none): When a release carries the fixes and reaches Flathub (the play
  updates Lightning each run), confirm them live, a long root message first, and delete the
  patch file (keep the upstream link).
- [ ] 🔄 **Task 2.7** (standing): Keep reporting what we find in Lightning upstream, one issue
  per defect with a reproduction, and list each here.
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
  Host run 2026-11-13 through meta-deploy: PASS, but empty. No declared team on the host has an
  https approved url, so the CA placement, the tls-trust-check build and the handshake were
  skipped (only Lightning's CA directory was created and swept, the one change). The handshake is
  NOT ESTABLISHED until a run with an https team, on this host or in the VM lab): The play's last step fails unless a handshake from inside Lightning's sandbox
  with exactly those two variables verifies the team URL, and the server certificate carries a SAN
  for the host (rustls has no CN fallback).
- [x] ✅ **Task 4.3**: Docs: a Lightning paragraph in `docs/agent-bus-cross-network.md`; update the
  comment in `play-matrix-clients.yml` that says no CA reaches Lightning.
- [ ] 🔄 **Task 4.4** (done in code: a team entry's optional `ca_file`, checked by
  `helpers/agent_bus/element.py`, goes to `tasks/https-team-ca.yml` as `https_team_ca_given`,
  which wins over the homeserver's admin state, for Element and Lightning; NOT ESTABLISHED until a
  run on a machine holding a delivered CA): A client on a machine that is not the homeserver host.
  Without it the CA is read from `/var/lib/teamchat-admin/<team>/tls/ca.crt`, so only the
  homeserver host can place it.
  The CA tasks (Lightning's and Element's) take a team's CA from a path the caller gives, place
  it and prove the handshake as before, with no assumption that the CA is on this host.
  Getting the CA file to the laptop or phone is the estate side's work (agreed with its
  coordinator on 2026-11-15); this task only consumes the file. Humans can sign in to a v2 team only once
  issue #296 is fixed.

### Phase 5: Per-team instance, launcher and pre-filled config

- [x] ✅ **Task 5.1** ([DESIGN-client-choice.md](DESIGN-client-choice.md); the code default
  stays `element` until Task 7.1 changes it, so a host that sets nothing is unchanged): Design note (owner decision **D3**, decided 2026-11-15): one home-level
  setting picks the Matrix client for every team, `agent_bus_matrix_client` (default
  `lightning`, or `element`), and a team's host vars may override it with its own `client:`.
  There is no second `agent_bus_lightning_teams` list. Write it as a short DESIGN section in
  this folder first.
- [ ] 🔄 **Task 5.2** (done in code, not established on a host: `src-go/cmd/agent-bus-lightning`
  over `src-go/internal/lightning`, L1-L14 in its package doc): A Go command under `src-go/` (new tooling is Go) that, per declared team,
  writes `teams/<team>/{data,config,cache}` under `~/.var/app/org.lightning_matrix.Lightning/`,
  writes `config/MatrixClient/matrix-client.conf` with `[homeserver] url=<team url>` only when the
  file is absent (Lightning rewrites it and owns it afterwards), and writes the launcher
  `agent-bus-<team>-lightning.desktop` with the Phase 3 launch form and the Phase 4 variables.
  It removes the directories' launcher (never a signed-in data root without the owner's consent)
  for a team no longer declared, as `element.py` does. Unit tests through fakes.
- [ ] 🔄 **Task 5.3** (done in code, not established on a host: `play-agent-bus-element.yml`
  resolves each entry's `client`, refuses any other value first, gives Element and its CA tasks
  the element teams and Lightning's the lightning teams, and runs the Task 5.2 command after
  Lightning's CA proof; `element.py` learns `client`): The play that runs it (an edit to `play-agent-bus-element.yml` generalised,
  or a sibling, decided in 5.1), with the same guard as Element: refuse a user holding pingbus
  bundles, because the instance holds the human's Matrix session.
- [ ] 🔄 **Task 5.4** (done in code, not established on a host: the native entry is the folder
  that holds every team's instance, `teams`, as the list's schema has no wildcard; held by
  `TestL14TheListCoversTheTeamRoots`): Add Lightning's per-team directories to its `native` entry in
  `src-go/internal/matrixclients/clients.json`, so every #251 guard sees a Lightning session.
- [ ] 🔄 **Task 5.5** (written, not yet run on a host: `acceptance.bash` over
  `acceptance_lightning.py`, its tests `test_acceptance_lightning.py`; queued in
  `meta-deploy.list` after the play's entry; exits 2 while no team opens in Lightning): Deploy through `meta-deploy.bash`; `acceptance.bash` checks each declared
  team's launcher, directories and config, and the CA handshake; the windows and the login are
  NOT ESTABLISHABLE and listed for the owner.

### Phase 6: ccy-teams opens a team in Lightning

- [ ] 🔄 **Task 6.1** (done in code, not established on a host: `login.OpenTeam`,
  `OpenEveryTeam` and `LaunchTeam` open `ops.Launcher` of the team's client, Lightning's
  being `lightning.Launcher`; `systemd.LaunchClient` names the scope and log for it. The sign-in
  check reads no client data root: the count is of sessions on the team's homeserver, from any
  client, and every text that shows it says so. No data root can say whether Lightning is
  signed in: both clients keep the token in the keyring, and the play writes the instance's
  folders before Lightning first opens. The default stays Element until Task 7.1): In
  `src-go/internal/teams/ops` and `.../picker`, "Open the team" and "Open every team, a window
  each" launch the launcher of the client the team resolves to. Tests through the existing
  fakes.
- [ ] 🔄 **Task 6.2** (done in code, not established on a host: the rule is
  `HostVars.MatrixClients` in `src-go/internal/bus/declare/clients.go`, tested on the cases of
  `src-go/internal/bus/declare/testdata/client-cases.json`, the table the plan's
  `test_acceptance_lightning.py` reads too (the play's Jinja is not run on it); the board
  reads it with the teams, the menu, the WHERE column and every password text name the
  client, and the set-up and remove paths check the launcher of the team's client): ccy-teams opens each team in the client that the home-level setting
  names, or in the team's own `client:` override (D3), and says which client it opened.

### Phase 7: Switch the default

- [ ] ⬜ **Task 7.1** (held: built on a work branch and landed only when BOTH gates are met,
  (a) a Lightning release carrying the thread-root fix is on Flathub (Task 2.5), and (b) this
  plan's `acceptance.bash` has passed on the host for at least one team set to `lightning`;
  ordered by the team coordinator on 2026-11-15): Default client becomes Lightning in the host
  vars defaults, `localhost.yml.dist`, ccy-teams and the docs (`docs/playbooks.md` and the
  agent-bus docs); Element remains selectable and its plays unchanged.
- [ ] 🔄 **Task 7.1a** (done in code, not established on a host: `lightning.HoldsAccount`, L15,
  and `login.OpenTeam`): the switch-client password gap, landed ahead of 7.1. Lightning keeps
  each account in its own folder of the instance's data root (`MatrixClient/matrix-client/ <localpart>_<server>`), made at sign-in and removed at sign-out, so an instance holding no
  team human's folder has never been signed in. While that holds, "Open the team" tells each
  human with sessions on the homeserver (another client's, such as an Element sign-in of a team
  since moved) so and asks whether to set a new password, default no; once any human's account
  is there (ccy-teams does not know which human the desktop is), nobody is asked.
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

| ID  | Question                                                      | Recommendation                                             |
| --- | ------------------------------------------------------------- | ---------------------------------------------------------- |
| D1  | Pin Lightning's Flatpak commit?                               | Decided: no, update every run (1.2)                        |
| D2  | Post the upstream issue and pull request?                     | Yes, after Task 2.2's build                                |
| D3  | Per-team host vars generic over the client, or a second list? | Decided: one home-level setting, a per-team override (5.1) |
| D4  | Fallback build (Task 2.6) if upstream does not take the fix?  | Only if asked, and deleted after                           |

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
