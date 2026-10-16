# Plan 00207: team roles and minimum models

**Status**: Not Started (design done; waiting on the owner's decisions D1-D18 and D20-D26; D19 decided)
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: Medium

## Overview

Teams have two roles today: one optional coordinator and any number of workers. The owner
wants explicit roles with contracts. The new ones are an **outside collaborator**, an agent
from a repository outside the team that is a colleague rather than a teammate, and a
**deployer**, a host desktop session with the owner's permissions whose job is to deploy and
to be the final sanity check before anything is deployed. Each role has a **minimum model
and effort**. The deployer's default is Opus 5.5 at medium. It is enforced at assignment, at
launch and at run time, so no session can hold a role below its minimum without being
stopped and without everyone being told. Each team can override the minimum in the team
settings pane.

A later owner requirement adds **communication gates**. By default the team is hub and spoke:
every agent talks only to the coordinator and the team's humans. The deployer is locked
harder: it receives only from the coordinator and humans, and sends only to the coordinator.
pingbus enforces that at send and at delivery, so a disallowed envelope never reaches the
conversation. A team can open named allowances (workers talking to each other, say), but
never for the deployer.

Two further owner requirements:

- **A management room** where only the coordinator and the team's humans meet. The
  coordinator posts terse, capped briefs and questions there. When the owner answers in the
  coordinator's terminal instead, a `NOTE` records it in the room, so the room stays the full
  record. A `NOTE` is never authority. Who posts it is D26; the recommendation is the
  coordinator itself, as its own labelled claim.
- **Rooms as defence in depth.** The owner decided this: private, invite-only channel rooms
  (workers, management, deployment, one liaison room per collaborator), enforced by the
  homeserver, alongside the pingbus gate, kept in sync by one reconciler, and denying on any
  disagreement.

Rooms only mean something because each seat has its own Matrix account. Seat tokens are kept
per checkout, though, so the deployer's token must move to a host-only store (SPEC §12.2).

The design is [SPEC-roles.md](SPEC-roles.md). This plan holds the milestones and the
decisions the owner must make before M1 starts.

## Goals

- A role set with written contracts: coordinator, worker, reviewer (D1), outside
  collaborator and deployer (SPEC §2).
- Communication gates at send and delivery, hub and spoke by default, with the deployer lock
  fixed in the protocol (SPEC §3).
- A model and effort ladder, floors per role, and enforcement at assignment, launch and run
  time that fails closed and is loud (SPEC §4).
- A deployer whose pre-deploy sanity check is a command with a verdict that
  `meta-deploy.bash` requires, and which deploys only with a human's verified authority
  (SPEC §6).
- Per-team policy in the team file and record, edited in the team settings pane, applied
  live (SPEC §7, §8).
- A migration that changes nothing for existing coordinators and workers until the owner
  sets a policy (SPEC §9).
- A management room with capped briefs and questions, human replies as `HUMAN`, and a `NOTE`
  (never `HUMAN`, never authority) recording a reply typed in the terminal (SPEC §11).
- Channel rooms enforced by the homeserver beside the pingbus gate (the owner's decision),
  with the deployer's token in a host-only store (SPEC §12).
- One source of truth, one reconciler, loud drift detection, and deny when the two layers
  disagree (SPEC §13).

## Non-Goals

- Drawing the team settings pane. Its toolkit is Plan 00204's D1 and its frame is Plan
  00201's `feat/team-settings`. This plan supplies its Roles, Communication and Deploy
  sections and the commands behind them.
- A security boundary against a hostile session in the same checkout or on the owner's uid
  (Plan 00203 D6, H31). This is a control against mistakes and silent fallbacks.
- A room boundary between two host cc sessions. Both run as the owner's uid, and both can read
  the host-only seat store (SPEC §12.2).
- A protocol version bump. New keys and roles follow the upgrade-first rule, as `org` and the
  coordinator did.
- Changing models mid-session from the host. Claude Code cannot be told to, so a session below
  a new floor is relaunched.

## Context & Background

- Plan 00161 (the bus), Plan 00196 (the coordinator role, the copy to the coordinator), Plan
  00203 (hot control: roles in the record, changed only by `set-role`; the keeper; the
  membership notice), Plan 00201 (many teams, the chooser's external-scope choice, the
  settings pane to come), Plan 00204 (TUI toolkit), Plan 00205 (SSH team members: roles apply
  to every member kind).
- Research facts are in SPEC §1. The research reports are saved under `untracked/agent-reports/`.
  The key facts: no launcher sets a model or an effort today; Claude Code 2.1.301 has
  `--model`, `--effort` and `--fallback-model`; the status line JSON carries `model.id` and
  `effort.level`; transcript entries carry `message.model` and fallback records.

## Tasks

Every milestone ships on its own, test-first: the failing test is committed or recorded
before the code.

### M0: spike, what Claude Code lets us see and set

- [ ] ⬜ **Task 0.1**: A triage script in this folder, run in a ccy bus session and a host cc
  session. It confirms:
  - `model.id` and `effort.level` in the status line payload, both with and without
    `--effort`;
  - how fast a typed `/model` and `/effort` show there;
  - `message.model` and the fallback record shapes in the transcript;
  - the precedence of `--effort`, `CLAUDE_CODE_EFFORT_LEVEL` and `effortLevel`, and of
    `--model` over `ANTHROPIC_MODEL` and settings;
  - the name of the variable Claude Code sets in its tool shells (`CLAUDECODE`);
  - whether `qa-all.bash` leaves a per-commit run record.
- [ ] ⬜ **Task 0.2**: Findings in `RESEARCH-observability.md`. Amend SPEC §4.4, §4.5 and §6.4
  where a fact differs.
- [ ] ⬜ **Task 0.3**: Only if D26 chooses (b): the spike confirms what the terminal notary
  needs (SPEC §11.4):
  - what `script --log-in` captures for a tmux pane running `podman run -it`;
  - that the pty's echo state can be read at each Enter;
  - that the supervisor's typing is recorded closely enough to subtract;
  - how often submitted lines reconstruct cleanly.
- [ ] ⬜ **Task 0.4**: The owner decides D1-D18 and D20-D26 (D19 is decided).

### M1: the ladder and the per-team policy (no behaviour change)

- [ ] ⬜ **Task 1.1**: Tests first, then `src-go/internal/teams/modelladder` and
  `helpers/pingbus/ladder.py`: ID grammar, families, versions, effort, floors, unknown fails
  closed. A parity test feeds both the same table.
- [ ] ⬜ **Task 1.2**: Tests first, then `teamfile.py`: `role_policy`, `launch_model`, `comms`
  and `deploy`, with defaults and every validation rule of SPEC §7.1. `declare.py` edits
  them.
- [ ] ⬜ **Task 1.3**: `ccy-teams team policy` (show, and edits with `--yes`). It writes host_vars
  only, and is not yet sent in the record.

### M2: roles, verbs and communication gates in the protocol (upgrade-first)

- [ ] ⬜ **Task 2.1**: Tests first (SPEC §3.7, the full matrix), then `protocol.py`:
  - the roles `reviewer`, `collaborator` and `deployer`;
  - the verb `deploy`;
  - the record keys `role_policy`, `comms` and `deploy_authorisers`;
  - the matrix and allowances, the gate at send and at delivery, and the reason code `gate`.
- [ ] ⬜ **Task 2.2**: The coordinator copy extended to every non-coordinator role. The
  fixed-template exemptions (goodbye, farewell, step-down).
- [ ] ⬜ **Task 2.3**: Envelope authority lines for the new roles. The `role_*` templates for
  reviewer, collaborator, deployer and suspended. The pingbus skill's Roles section, and its
  relay rule for collaborators.
- [ ] ⬜ **Task 2.4**: Protocol §5, §8, §9, §14, and the new §16. The contract test. `docs/agent-bus.md`.
- [ ] ⬜ **Task 2.5**: `agent-bus add-member` and `set-role` accept the new roles, with counts.
  The admin sends the policy in the record. The writers refuse while a running member
  reports an older pingbus.

### M3: enforcement at assignment and launch

- [ ] ⬜ **Task 3.1**: Tests first, then `internal/teams/rolecheck`: enabled, count, kind,
  observed model, launch model and maximum. It is wired into `session role`,
  `session add --role`, the picker's Ctrl-O, lxc-add and bare `ccy --teams`.
- [ ] ⬜ **Task 3.2**: Tests first, then `ccy-teams internal launch-model`. ccy, cc,
  `cc-teams.bash`, `agent-bus-claude`, restore and restart call it. It passes `--model` and
  `--effort`, refuses below the floor, a fallback model or an overriding environment, and
  records `launch` in the registry record. The launchers do not grow. CCY version bump.
- [ ] ⬜ **Task 3.3**: `members.py` writes LXC and VM unit arguments from the same function.

### M4: enforcement at run time

- [ ] ⬜ **Task 4.1**: Tests first, then `pingbus status-line` writes `model.json` from the payload.
- [ ] ⬜ **Task 4.2**: Tests first, with a fake clock and transcript fixtures for each fallback
  shape. The keeper evaluates `ok`, `unverified` and `below`, and writes `role_check`.
  pingbus refuses privileged sends (`role-model`). The `role_suspended` notice. The step-down
  text, sent once.
- [ ] ⬜ **Task 4.3**: Tests first, then `ccy-teams warden` and its root timer (Ansible):
  demotion only, `suspended` in the registry, the record re-sent, a desktop notification,
  and the board's suspended state. For a cc session it reads the host transcript.

### M5: the deployer's gate

- [ ] ⬜ **Task 5.1**: Tests first, against fixture repositories, then `deploy-gate check`
  (G1-G8 with a coverage line and item ids), `judge` (every id covered, closed dispositions,
  evidence resolving at SHA; a pass refused with any concern) and `verify`. G6 reads the host
  transcript, never the keeper report.
- [ ] ⬜ **Task 5.2**: The `meta-deploy.bash` guard for an agent caller. It stays within the
  bash limits.
- [ ] ⬜ **Task 5.3**: The `deploy` verb's flow in the skill's Deployer section. The authority
  check against `history`.

### M6: the team settings pane sections

- [ ] ⬜ **Task 6.1**: The Roles, Communication and Deploy sections in Plan 00201's team
  settings pane, in Plan 00204's toolkit, each edit running `ccy-teams team policy`.
  Includes the raise-a-floor confirmation and Restore.

### M6a: channel rooms and the reconciler (SPEC §12, §13)

- [ ] ⬜ **Task 6a.1**: Tests first, then `rooms_for` in Python and Go, pinned equal. The bundle's
  `rooms` (the old `room` read as `workers`), per-room trust, `invite_acceptable` per room, and
  the `drift` reason at send and delivery. Upgrade first.
- [ ] ⬜ **Task 6a.2**: Tests first, against a fake homeserver, then `reconcile_rooms` in the admin's
  sync: create rooms, removes before adds, kicks with a reason, the record and power levels in
  every room, and a read-only mode for health. Hot-control commands report the room moves.
- [ ] ⬜ **Task 6a.3**: The host-only seat store for deployer seats (and cc coordinators, D25).
  `cc --teams` takes those seats from it. ccy-teams warns about seats of different room sets
  sharing a checkout.
- [ ] ⬜ **Task 6a.4**: The health check and the board report drift. Board letters for rooms. The
  picker previews room moves.

### M6b: the management room (SPEC §11)

- [ ] ⬜ **Task 6b.1**: Tests first, then `pingbus brief` and `ask`, with their caps, interval,
  duplicate refusal and open-question limit. The `Q<n>` counter and open set are read from the
  room's history.

- [ ] ⬜ **Task 6b.2**: tests first, then the `NOTE` kind:

  - an explicit `NOTE` branch in `envelope.py`, never `HUMAN`, never closing a question;
  - single-team routing.
  - With D26 (a): `pingbus note`, which needs an open `Q<n>`, has a cap and posts with the
    fixed "coordinator's note" prefix.
  - With D26 (b), instead:
    - the launcher's host-side input tap;
    - `ccy-teams notary` with every SPEC §11.4 mitigation (FIFO and memory only, count-only
      logs, echo-off and secret-shaped lines discarded);
    - the `notary` account, with `notary` added to `RESERVED_LOCALPARTS`;
    - a CCY version bump.

- [ ] ⬜ **Task 6b.3**: Human replies in the room as `HUMAN` with the question beside them.
  `history --room`. The skill and `role_coordinator` guidance.

### M7: migration and host acceptance

- [ ] ⬜ **Task 7.1**: `deploy.bash` and `acceptance.bash` in this folder, queued in
  `meta-deploy.list`. They cover:
  - the fleet upgrade check;
  - a deployer refused on a Sonnet session;
  - a launch given `--model` and `--effort`;
  - a forced downgrade suspending and demoting, loudly;
  - a worker's message to the deployer refused and never delivered;
  - a gate PASS and REFUSE;
  - `meta-deploy.bash` refusing an agent with no verdict;
  - with a worker's token, the deployment room refusing a read, a self-invite, a join and a
    power-levels write;
  - a role change moving a seat between rooms;
  - a hand-made extra invite reported as drift and dropped;
  - the owner's Element steps for the management room.
- [ ] ⬜ **Task 7.2**: `qa-reviewer` over the full diff; resolve its findings.

## Owner decisions

Each has a recommendation. M1 starts once these are answered.

| #   | Decision                                                                                                                                                                                                                                                                                                                                                                                | Recommendation                                                                                                                                        |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Add the **reviewer** role now?                                                                                                                                                                                                                                                                                                                                                          | Yes. Every new role needs the same fleet-wide pingbus upgrade, so one upgrade carries all of them                                                     |
| D2  | Does a higher family meet a floor whatever its version (Fable 5 meets `opus-5.5`)?                                                                                                                                                                                                                                                                                                      | Yes: family first, version only within a family                                                                                                       |
| D3  | An absolute deployer floor no team may go below?                                                                                                                                                                                                                                                                                                                                        | Yes, `sonnet/medium`. Haiku never deploys. The default stays `opus-5.5/medium`                                                                        |
| D4  | A default floor for the coordinator?                                                                                                                                                                                                                                                                                                                                                    | None at rollout, so no existing coordinator is suspended. Set it per team in the pane                                                                 |
| D5  | How many deployers per team?                                                                                                                                                                                                                                                                                                                                                            | Default 1, and a team may allow up to 4                                                                                                               |
| D6  | Deployer only on a host `cc` session?                                                                                                                                                                                                                                                                                                                                                   | Yes. A ccy container may never run Ansible                                                                                                            |
| D7  | Deploy authority per deploy, or standing per team?                                                                                                                                                                                                                                                                                                                                      | Per deploy: a human's message naming the commit, verified in history                                                                                  |
| D8  | Who may authorise a deploy?                                                                                                                                                                                                                                                                                                                                                             | An explicit `deploy.authorisers` list, required when the deployer is enabled                                                                          |
| D9  | May the deployer answer a human who addressed it (an `re` reply, with the coordinator copied)?                                                                                                                                                                                                                                                                                          | Yes, answers only. It never starts a message to anyone but the coordinator                                                                            |
| D10 | Team allowances: offer `workers-talk`, `reviewers-talk`, `collaborators-workers` and `collaborators-humans`, all off by default?                                                                                                                                                                                                                                                        | Yes, all four, off by default. None can name the deployer                                                                                             |
| D11 | Existing teams: hub and spoke at their first policy write, or keep `workers-talk` on for them?                                                                                                                                                                                                                                                                                          | Hub and spoke for every team (the safe default). The first write says so, and the toggle is one click                                                 |
| D12 | Outside collaborator: may it send `review`, and may it start messages to the team's humans?                                                                                                                                                                                                                                                                                             | `review` yes, to the coordinator. Messages to humans: answers only, unless `collaborators-humans` is on                                               |
| D13 | Restoring a suspended role: by hand, or automatic when the model recovers?                                                                                                                                                                                                                                                                                                              | By hand, through the full assignment check                                                                                                            |
| D14 | The maximum: enforced at run time too?                                                                                                                                                                                                                                                                                                                                                  | No. Enforce it at launch and assignment; at run time only report it                                                                                   |
| D15 | Gate check G3 with no `qa-all` record for the commit: the gate runs `qa-all`, or refuses?                                                                                                                                                                                                                                                                                               | The gate runs it                                                                                                                                      |
| D16 | A delivery-gate drop: tell the sender?                                                                                                                                                                                                                                                                                                                                                  | No. Log and count it. An honest sender was already refused on send                                                                                    |
| D17 | An effort the status line does not report: fail the floor?                                                                                                                                                                                                                                                                                                                              | Yes, fail closed. The launcher always pins `--effort` under a floor, so it is reported                                                                |
| D18 | The warden runs as a root system timer, or a user timer with a sudo rule?                                                                                                                                                                                                                                                                                                               | A root system timer. It only ever demotes                                                                                                             |
| D20 | The management room: create it for every team with a coordinator, with the brief format and caps of SPEC §11.2 (headline plus 4 tagged lines of at most 120 characters, at most 600 bytes; `brief_min_interval_s` 900; up to 3 open questions)?                                                                                                                                         | Yes, as specified                                                                                                                                     |
| D21 | History visibility of the new rooms                                                                                                                                                                                                                                                                                                                                                     | `joined` for deployment and liaison rooms, so a new deployer or collaborator does not read the past; `shared` for workers and management              |
| D22 | Option (b) only: what the notary posts                                                                                                                                                                                                                                                                                                                                                  | The first line, at most 160 characters. A secret-shaped line or an echo-off line is discarded, never posted                                           |
| D23 | Whose terminal the coordinator runs in (`terminal_human`)                                                                                                                                                                                                                                                                                                                               | A team-file key, defaulting to the first human                                                                                                        |
| D24 | Note every terminal prompt, or only those that answer something?                                                                                                                                                                                                                                                                                                                        | Only while a question is open or within `terminal_note_window_s` (1800) of the last brief or question                                                 |
| D25 | Move a cc coordinator's seat into the host-only store as well as the deployer's?                                                                                                                                                                                                                                                                                                        | Yes. A ccy coordinator keeps its seat in its own checkout and should not share that checkout with workers                                             |
| D26 | The terminal note (SPEC §11.4): (a) the coordinator posts it itself as `NOTE`, "(coordinator's note) the owner answered Q<n> in the terminal: <paraphrase>", plainly its own claim; or (b) a host-side notary that taps the pane's input, with its own account and the secret mitigations of SPEC §11.4. Either way `NOTE` is never `HUMAN`, never closes a question and grants nothing | **(a).** The owner asked for a record, not proof. (a) meets that with no keystroke tap, which would see secrets typed in the pane, and no new account |

### Decided by the owner

- **D19, collaborator confidentiality, and section B, rooms.** The owner's words are quoted
  once, as (Q-rooms) at the top of SPEC-roles.md.
  - This means private, invite-only channel rooms enforced by the homeserver, alongside the
    pingbus gate (SPEC §12).
  - They are kept in sync by one reconciler, with loud drift detection, and deny on
    disagreement (SPEC §13).
  - Each collaborator gets its own liaison room, which settles D19.

## Dependencies

- Builds on Plan 00196 (the coordinator role) and Plan 00203 (the keeper, reports, record
  sync and hot control).
- M6 needs Plan 00201's team settings pane and Plan 00204 D1.
- M3 touches the ccy launcher: CCY version bump, bash-limits ratchet.

## Success Criteria

- [ ] A session below a role's floor cannot be given the role, cannot be launched into it, and
  loses it within seconds of falling below, with the session, the room, the desktop and the
  board all told
- [ ] A worker's message to the deployer is refused on send with the rule named, and a raw one
  never reaches the deployer's conversation
- [ ] An agent cannot run `meta-deploy.bash` without a fresh PASS verdict carrying a verified
  human authority
- [ ] A team's floors and allowances change from the pane and apply without a relaunch
- [ ] A worker's account cannot read, join or post in the deployment room (server-refused), a
  role change moves a seat between rooms, and any drift between rooms and record is reported
  and denied
- [ ] The owner sees briefs and questions in the management room in Element, can answer there
  or in the terminal, and a terminal answer appears as a note in the room
- [ ] Existing coordinators and workers see no change until their team's policy is written
- [ ] Touched suites, `qa-changed.bash` and the `qa-reviewer` agent pass

## Risks & Mitigations

| Risk                                                       | Impact | Probability | Mitigation                                                                                                |
| ---------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------- |
| The status line payload omits `effort.level` in some cases | M      | M           | M0 confirms. The launcher pins `--effort`, and an unknown effort fails closed (D17)                       |
| Hub and spoke breaks working teams                         | M      | M           | It applies at a team's first policy write, it is announced, and `workers-talk` is one toggle (D11)        |
| A mixed fleet sees the new record as untrusted             | H      | M           | Upgrade first. Writers refuse while a running member is older (SPEC §9)                                   |
| A usage-limit downgrade suspends a deployer mid-run        | M      | M           | G6 re-checked by `verify`. A suspended deployer answers `blocked`. A half-run is visible in `summary.txt` |
| A new model family name                                    | L      | L           | Fails closed, named. A one-line ladder change with a test                                                 |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00207-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and spec filed (design only)
