# Plan 00207: team roles and minimum models

**Status**: Not Started (design done; waiting on the owner's decisions D1-D19)
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

## Non-Goals

- Drawing the team settings pane. Its toolkit is Plan 00204's D1 and its frame is Plan
  00201's `feat/team-settings`. This plan supplies its Roles, Communication and Deploy
  sections and the commands behind them.
- A security boundary against a hostile session in the same checkout or on the owner's uid
  (Plan 00203 D6, H31). This is a control against mistakes and silent fallbacks.
- A separate liaison room for collaborators (D19).
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
- [ ] ⬜ **Task 0.3**: The owner decides D1-D19.

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
  (G1-G9 with a coverage line), `judge` and `verify`.
- [ ] ⬜ **Task 5.2**: The `meta-deploy.bash` guard for an agent caller. It stays within the
  bash limits.
- [ ] ⬜ **Task 5.3**: The `deploy` verb's flow in the skill's Deployer section. The authority
  check against `history`.

### M6: the team settings pane sections

- [ ] ⬜ **Task 6.1**: The Roles, Communication and Deploy sections in Plan 00201's team
  settings pane, in Plan 00204's toolkit, each edit running `ccy-teams team policy`.
  Includes the raise-a-floor confirmation and Restore.

### M7: migration and host acceptance

- [ ] ⬜ **Task 7.1**: `deploy.bash` and `acceptance.bash` in this folder, queued in
  `meta-deploy.list`. They cover:
  - the fleet upgrade check;
  - a deployer refused on a Sonnet session;
  - a launch given `--model` and `--effort`;
  - a forced downgrade suspending and demoting, loudly;
  - a worker's message to the deployer refused and never delivered;
  - a gate PASS and REFUSE;
  - `meta-deploy.bash` refusing an agent with no verdict.
- [ ] ⬜ **Task 7.2**: `qa-reviewer` over the full diff; resolve its findings.

## Owner decisions

Each has a recommendation. M1 starts once these are answered.

| #   | Decision                                                                                                                         | Recommendation                                                                                          |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| D1  | Add the **reviewer** role now?                                                                                                   | Yes. Every new role needs the same fleet-wide pingbus upgrade, so one upgrade carries all of them       |
| D2  | Does a higher family meet a floor whatever its version (Fable 5 meets `opus-5.5`)?                                               | Yes: family first, version only within a family                                                         |
| D3  | An absolute deployer floor no team may go below?                                                                                 | Yes, `sonnet/medium`. Haiku never deploys. The default stays `opus-5.5/medium`                          |
| D4  | A default floor for the coordinator?                                                                                             | None at rollout, so no existing coordinator is suspended. Set it per team in the pane                   |
| D5  | How many deployers per team?                                                                                                     | Default 1, and a team may allow up to 4                                                                 |
| D6  | Deployer only on a host `cc` session?                                                                                            | Yes. A ccy container may never run Ansible                                                              |
| D7  | Deploy authority per deploy, or standing per team?                                                                               | Per deploy: a human's message naming the commit, verified in history                                    |
| D8  | Who may authorise a deploy?                                                                                                      | An explicit `deploy.authorisers` list, required when the deployer is enabled                            |
| D9  | May the deployer answer a human who addressed it (an `re` reply, with the coordinator copied)?                                   | Yes, answers only. It never starts a message to anyone but the coordinator                              |
| D10 | Team allowances: offer `workers-talk`, `reviewers-talk`, `collaborators-workers` and `collaborators-humans`, all off by default? | Yes, all four, off by default. None can name the deployer                                               |
| D11 | Existing teams: hub and spoke at their first policy write, or keep `workers-talk` on for them?                                   | Hub and spoke for every team (the safe default). The first write says so, and the toggle is one click   |
| D12 | Outside collaborator: may it send `review`, and may it start messages to the team's humans?                                      | `review` yes, to the coordinator. Messages to humans: answers only, unless `collaborators-humans` is on |
| D13 | Restoring a suspended role: by hand, or automatic when the model recovers?                                                       | By hand, through the full assignment check                                                              |
| D14 | The maximum: enforced at run time too?                                                                                           | No. Enforce it at launch and assignment; at run time only report it                                     |
| D15 | Gate check G3 with no `qa-all` record for the commit: the gate runs `qa-all`, or refuses?                                        | The gate runs it                                                                                        |
| D16 | A delivery-gate drop: tell the sender?                                                                                           | No. Log and count it. An honest sender was already refused on send                                      |
| D17 | An effort the status line does not report: fail the floor?                                                                       | Yes, fail closed. The launcher always pins `--effort` under a floor, so it is reported                  |
| D18 | The warden runs as a root system timer, or a user timer with a sudo rule?                                                        | A root system timer. It only ever demotes                                                               |
| D19 | A separate liaison room, so a collaborator cannot read the team's room?                                                          | Not now. Say plainly in the pane that a collaborator can read the room                                  |

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
