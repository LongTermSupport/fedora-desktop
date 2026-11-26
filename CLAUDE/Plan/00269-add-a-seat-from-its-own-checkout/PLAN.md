# Plan 00269: add a seat from its own checkout

**Status**: Not Started (design; owner questions Q1 and Q2 open)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: High

## Overview

The owner, 26 Nov 11:01Z: adding a seat to a team should not need another repository's scripts.
Today a laptop seat of team A, served on another machine, takes two shell scripts from another
repository: one issues the seat on the serving host (an agent seat with sudo there runs it), one
places it on the laptop (the owner runs it from that repository's checkout). Both scripts say their
home is this repository's seat verbs (#415, #416).

This plan makes adding a seat **one owner step, run in the team's own checkout on the machine where
the seat will run**, whichever machine serves the team. `ccy-teams seats add <team>` (and the
launch's own add, when `cc` or `ccy --teams <team>` finds no seat) issues the seat on the serving
host over the owner's own ssh, asks the owner's password there once, and places the answer straight
into this checkout's pool from memory. No seat number is shown or typed (register R90). A seat that
is enrolled on the bus but whose credential this machine lost is recovered by a reissue, offered in
words and done only on the owner's word.

Findings, code references and the route options: [RESEARCH.md](RESEARCH.md).

## Goals

- One command, in team A's checkout on the laptop, adds a seat of team A when team A is served on
  another machine: issue there, place here, nothing else to run.
- The owner types no seat number, handle or pipe; refusals say what to do in words.
- Secrets stay where they are today: never in argv, never on a screen or in a file outside the pool;
  the issued answer is held in memory and placed straight into the pool.
- An enrolled seat with no credential on this machine is recovered (reissue) on the owner's word,
  never by guessing; a seat another checkout holds is never reissued from under it.
- The other repository's two scripts can be deleted, with this verb named as their replacement.

## Non-Goals

- A seat request over the bus, a broker daemon or a request spool (Plans 00197 and 00264). The
  issuer is an interface, so one can replace the ssh issuer later.
- Moving a seat's conversation between places (#415).
- LXC guest seats (`seat place-lxc`, Plan 00257, the lxc apply).
- R90's detector and the checkout-picks-its-seat launch (Plan 00265 Task 4.2), R89's restore
  command, R75's `seats forget`. This plan supplies the add path they call, and links to them.
- A NOPASSWD sudoers rule installed by this repository's plays (the cross-network doc forbids it;
  Q1 asks the owner).

## Design

### D1. The serving host is reached by ssh pull, from the machine the seat is for

- The verb runs on the laptop and ssh-es **into** the serving host (which has no ssh egress), as
  `docs/agent-bus-cross-network.md` already prescribes: the owner's own ssh key and agent, no
  `ssh -t`, `ssh -T <route> sudo -S -p '' teamchat-admin member add ... --credential password`.
- Whose credential: the owner's own ssh login and the owner's own password sudo on the serving host.
  No agent seat holds or uses root on the serving host for this.
- Where the password is asked: on the laptop's terminal, once per run, echo off (`ui.Lines.Secret`),
  the prompt naming the route as the owner wrote it ("Your password for sudo on <route>"). It goes
  to the ssh's stdin as the one line `sudo -S` reads (an `execx` feed), is never in argv, and is
  cleared after the run. The issued answer comes back on the ssh's stdout, is held in memory, given
  to `placeissued.Place` in process and cleared (`seatadd`'s existing shape).
- Why not the bus: the first seat has no voice on the bus to ask with; an agent issuing on request
  is the coupling the owner objects to and needs an agent with root; the credential would cross the
  homeserver. RESEARCH.md section 5.

### D2. The route to the serving host

Nothing on the laptop records which machine serves a team (`system.Life`: "nothing here records
another machine's teams"). The route is the owner's ssh configuration entry (never a hostname in
this repository). Proposed, pending **Q2**: a per-machine record in the seat store, team to route,
asked once in words the first time ("Team A is not served on this machine. Which of your ssh hosts
serves it?", offering the `Host` entries of the owner's ssh configuration), checked by
`ssh -T <route> teamchat-admin version`, then reused. A seat of the team already on this machine
proposes the route whose host matches its homeserver URL.

### D3. The serving host chooses the number

Reading the remote roster to pick a number races with other adders. Instead `member add` gains
`--seat next` with `--avoid <seat parts>` (the parts this checkout's pool already uses): the serving
host picks the lowest free number under its own roster lock and answers the handle in the issued
document. The local-team path uses the same, so `freeHandle` has one home on the serving side.

### D4. Recovery of an enrolled seat with no credential here (the reissue case)

- A read verb on the serving host, `teamchat-admin member list --team <team> --prefix <prefix>`,
  answers each handle under the prefix with its state, enrolment time and last login. Read-only,
  through the same ssh and sudo.
- The laptop classifies each **enrolled** handle under this checkout's prefix: in this checkout's
  pool (held here, nothing to do); in another checkout's pool on this machine (`seatcheckouts`,
  never touched); in no pool on this machine (**lost here**).
- A lost seat is offered in words, never by number: "A seat of team A for this repository on this
  machine was enrolled on <date> and last signed in <when>, but its credential is not on this
  machine. Give it a new credential? It keeps its history; anything still signed in with the old
  credential is signed out." The other choice is a new seat. Yes runs `member reissue` for that
  handle and places it; the owner never sees the handle as a thing to type.
- Without a terminal (headless, a restore), nothing is chosen: the run refuses, naming the choice
  waiting for the owner and the command that asks it.
- `member add`'s enrolled refusal becomes a typed outcome (Task 1.2), so the verb tells "taken just
  now by another adder" (retry with `next`) from "this handle" without reading words.

### D5. How it fits

- `ccy-teams seats add <team> [--host]`: the verb. A team with a status record here issues locally
  (`SudoIssuer`, unchanged); one served elsewhere issues over the route (`SSHIssuer`); a removed or
  undeclared-installed team is still refused (R59's `system.Life`).
- The launch: `Adder.NewSeat` (R5) takes the same path, so `cc` in a checkout with no seat of a
  remote team asks to fetch one, in words (closes the add half of R91).
- R90's picker (Plan 00265 Task 4.2) calls this when the checkout holds no seat; it lists a lost
  seat as a choice ("recover the seat this checkout used before") beside "a new seat".
- R75's `seats forget` deletes a retired team's pool seats here; this verb never deletes, and never
  offers a seat of a team `system.Life` calls gone.
- Retiring the other repository's scripts: once accepted on the laptop, their owner team is told the
  verb that replaces each, and their headers' pointer (#416) is answered by a row on #416.

## Tasks

### Phase 1: buildable now

- [ ] ⬜ **Task 1.1**: `SPEC-seat-add.md` in this folder: the verb's expectations, numbered (S1...),
  each testable through fakes: inputs, the words each screen and refusal says (no seat number, no
  handle to type), exit codes (0, 1, 64, 78, cancelled), the secret rules (argv, stdin, memory,
  clear), the headless refusals, and D1-D5's decision table (local, remote, removed, lost, held
  elsewhere, race).
- [ ] ⬜ **Task 1.2**: `member add`'s enrolled refusal as a named outcome, test first: `apply` answers
  a typed refusal (carrying the handle and its state) that `teamchat-admin` maps to its exit and a
  machine-readable reason in its error object, and `seatadd` recognises it instead of reading text.
  Red: a `seatadd` test that, given that refusal, expects a named recovery; it fails today.
  Defence Before Fix: the class is "a recovery a caller branches on that exists only as message
  text"; sweep `apply`'s other refusals a caller recovers from (the return and reissue states) and
  give each the same, or record that no caller branches on it.

### Phase 2: issue over the route

- [ ] ⬜ **Task 2.1**: `member add --seat next --avoid <parts>` on the serving side (D3), test first
  in `apply` and `admin`; `seatadd`'s local path uses it.
- [ ] ⬜ **Task 2.2**: the route record and its question (D2), test first over a fake ssh
  configuration and a fake `execx`. Waits on Q2.
- [ ] ⬜ **Task 2.3**: `SSHIssuer`, test first: argv holds no secret (`execx` refuses one), the
  password is the one stdin line, a failure carries stderr only, the answer is cleared after
  placement, `ssh -t` never appears. Waits on Q1 for the sudo shape.
- [ ] ⬜ **Task 2.4**: `Adder.Add` for a team with no status record here takes the route instead of
  refusing 78; removed and declared-not-installed teams are still refused (R59). The two hand-typed
  pipes in `seatadd.go` (the not-served-here and issued-not-placed errors) go: an issued-not-placed
  seat is recovered by D4, in words. They are instances of R90's class; R90's detector (Plan 00265
  Task 4.2) must be red on them before they are removed.
- [ ] ⬜ **Task 2.5**: `Adder.NewSeat` (the launch) offers the same, in words, at a terminal; refuses
  headless, naming the command that asks.

### Phase 3: recovery

- [ ] ⬜ **Task 3.1**: `teamchat-admin member list --team --prefix` (D4), read-only, test first.
- [ ] ⬜ **Task 3.2**: the lost-seat classification and the owner's question (D4), test first over a
  fake roster and the fake pools of two checkouts; a seat another checkout holds is never offered.
- [ ] ⬜ **Task 3.3**: the reissue run: `member reissue` over the route, placed; the old pool record,
  if any, quarantined by `Place` as today.

### Phase 4: fit, docs, retirement

- [ ] ⬜ **Task 4.1**: the R90 picker's "add a seat" and "recover" rows call this path (with Plan
  00265 Task 4.2).
- [ ] ⬜ **Task 4.2**: docs: `docs/agent-bus-cross-network.md` "A member on another machine" names the
  verb first and keeps the pipe only as what it runs; `docs/agent-bus.md`'s seats section; the
  `ccy-teams` usage text.
- [ ] ⬜ **Task 4.3**: a row on #416 for this verb; R91's add half pointed at this plan in the Plan
  00265 register.
- [ ] ⬜ **Task 4.4**: acceptance on the laptop against a real serving host: a new seat of a remote
  team, and the live lost seat recovered; `acceptance.bash` in this folder, run through
  `meta-deploy.bash` on the machine it judges. Then the other repository's team is told to delete
  the two scripts.
- [ ] ⬜ **Task 4.5**: `qa-reviewer` over the plan's full diff.

## Owner questions

- **Q1**: On a serving host, does the owner have an account with password sudo that the laptop can
  ssh into? If not, should that host's own IaC (the other repository) carry a narrow sudoers rule
  for `teamchat-admin member add|reissue|list`, or is another route wanted?
- **Q2**: Where should "team A is served by <ssh host>" live on the laptop: asked once and kept in
  this machine's seat store (proposed), or declared in the laptop's host_vars by ssh alias?

## Success Criteria

- [ ] In team A's checkout on the laptop, one `ccy-teams seats add <team>` (or a `cc` launch) adds a
  seat of a team served elsewhere, asking only the owner's password there and real choices in words.
- [ ] The live lost seat is recovered by the verb on the owner's word, keeping its history.
- [ ] No seat number or handle is shown as a thing to type on these paths (R90's detector green).
- [ ] The secret tests pass: no secret in argv, nothing printed, the answer cleared.
- [ ] The other repository's two scripts are deleted by their team, this verb named in their place.
- [ ] QA passes and `qa-reviewer` finds nothing blocking.

## Risks & Mitigations

| Risk                                                  | Impact | Probability | Mitigation                                                                                    |
| ----------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------- |
| A reissue signs out a seat another machine still uses | H      | L           | Only handles in no pool here are offered; the words say what is signed out; the owner decides |
| Two adders race for one number                        | M      | M           | The serving host picks under its roster lock (D3); a lost race is a named outcome (Task 1.2)  |
| The password leaks into argv, a log or a pty          | H      | L           | `execx` feed and secret checks, no `ssh -t`, the tests of Task 2.3                            |
| The owner has no sudo account on the serving host     | H      | M           | Q1, answered before Task 2.3                                                                  |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/ — see CLAUDE/PlanJournalling.md. -->

- Plan written.
