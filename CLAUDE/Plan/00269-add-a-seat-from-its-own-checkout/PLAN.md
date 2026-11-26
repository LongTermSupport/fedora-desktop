# Plan 00269: add a seat from its own checkout

**Status**: Not Started (design; no owner questions open: Q1 and Q2 decided 26 Nov 11:07Z)
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
host over the owner's own ssh, through a narrow no-password sudo rule there that allows one fixed
command, and places the answer straight into this checkout's pool from memory. No password is
asked; no seat number is shown or typed (register R90). A seat that is enrolled on the bus but whose
credential this machine lost is recovered by a reissue, offered in words and done only on the
owner's word.

Findings, code references and the route options: [RESEARCH.md](RESEARCH.md).

## Goals

- One command, in team A's checkout on the laptop, adds a seat of team A when team A is served on
  another machine: issue there, place here, nothing else to run.
- The owner types no seat number, handle, pipe or password; refusals say what to do in words.
- Secrets stay where they are today: never in argv, never on a screen or in a file outside the pool;
  the issued answer is held in memory and placed straight into the pool.
- An enrolled seat with no credential on this machine is recovered (reissue) on the owner's word,
  never by guessing; a seat another checkout holds is never reissued from under it.
- Which machine serves a team is declared, not asked: a fresh laptop with its host_vars needs
  nothing else.
- The other repository's two scripts can be deleted, with this verb named as their replacement.

## Non-Goals

- A seat request over the bus, a broker daemon or a request spool (Plans 00197 and 00264). The
  issuer is an interface, so one can replace the ssh issuer later; for now ssh is kept (D1).
- Moving a seat's conversation between places (#415).
- LXC guest seats (`seat place-lxc`, Plan 00257, the lxc apply).
- R90's detector and the checkout-picks-its-seat launch (Plan 00265 Task 4.2), R89's restore
  command, R75's `seats forget`. This plan supplies the add path they call, and links to them.
- A sudoers rule written by this repository's plays. The rule D1 needs is the serving host's own
  configuration, declared in that host's own IaC (the other repository); this repository supplies
  only the fixed command it allows and documents the rule's exact shape. This matches
  `docs/agent-bus-cross-network.md` ("A sudoers rule for the verb is the homeserver host's own
  configuration to make or not; the play installs none").

## Decisions

The team coordinator, 26 Nov 11:07Z, answering Q1 and Q2. Each is reversible on the owner's word.

- **Q1, the sudo on the serving host:** a narrow sudoers rule on the serving host, declared in that
  host's own IaC, allowing only the seat issue, reissue and read for the owner's existing login, with
  NOPASSWD. No interactive password sudo: least privilege (one fixed command, not the owner's whole
  sudo), and it works with no tty. Shape: D1.
- **Q2, where the route lives:** declared, not asked. Which host serves a team is tracked config on
  the team's own record in the laptop's host_vars (undeclared state in a seat store would be drift
  under Everything-is-IaC). Field: D2.
- **ssh, not a bus-carried request:** kept. The reasons are D1's last bullet.

## Design

### D1. The serving host is reached by ssh pull, then `sudo -n` of one fixed command

- The verb runs on the laptop and ssh-es **into** the serving host (which has no ssh egress), as
  `docs/agent-bus-cross-network.md` prescribes: the owner's own login, ssh key and agent, no
  `ssh -t`: `ssh -T <route> sudo -n /usr/local/sbin/teamchat-admin member serve`.

- `sudo -n` never prompts: with the rule absent it fails at once ("a password is required"), and the
  verb refuses, naming the rule below and that it belongs to the serving host's IaC. No password is
  asked, fed or held anywhere; no agent seat holds or uses root on the serving host.

- **`teamchat-admin member serve`** is a new `ClassRoot` verb that takes **no arguments**: it reads
  one strict JSON request on stdin (capped, unknown keys refused) and runs exactly one of:

  | `op`      | Runs, with the request's fields                                                                                                                          |
  | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
  | `add`     | `member add --team <team> --prefix <repo>+<site>.<type> --seat next --avoid <parts> --kind <type> --human-text <true\|false> --credential password` (D3) |
  | `reissue` | `member reissue --team <team> --handle <repo>.<seat>+<site>.<type> --credential password`                                                                |
  | `list`    | `member list --team <team> --prefix <repo>+<site>.<type>` (D4, read-only)                                                                                |

  `<type>` is `podman` or `host` only and the credential is always `password`: no token, no LXC
  kind, no other member or team verb is reachable. The request holds no secret; the answer (the
  issued document, or the list) comes back on stdout, is held in memory, given to
  `placeissued.Place` in process and cleared (`seatadd`'s existing shape).

- **The sudoers rule, for the serving host's IaC to add** (`/etc/sudoers.d/`, `visudo -c` checked):

  ```
  <owner-login> ALL=(root) NOPASSWD: /usr/local/sbin/teamchat-admin member serve ""
  ```

  `<owner-login>` is the owner's existing login there. The path is `paths.AdminBinaryPath`, the
  link `play-teamchat.yml` installs (`tc_admin_link`). `""` permits no arguments, so the rule allows
  this one command line and nothing else.

- **Why a fixed verb, not a rule over `member add` and `member reissue`:** their values are free
  text, so such a rule needs `*`, and sudoers' `*` matches spaces, so no such rule is narrow.
  RESEARCH.md section 6.

- **Why ssh, not the bus:** the first seat has no voice on the bus to ask with; an agent issuing on
  request is the coupling the owner objects to and needs an agent with root; the credential would
  cross the homeserver. RESEARCH.md section 5.

### D2. The route to the serving host is declared on the team's own record

A team served elsewhere gets an entry in the laptop's `teamchat_teams` (untracked host_vars, the
owner's estate) with one new field:

```yaml
teamchat_teams:
  - team: <team>
    served_by: <ssh-host-alias>   # an entry in the owner's ssh configuration, never a hostname
```

- An entry with `served_by` declares the team served on another machine: the play installs nothing
  for it, and it takes no install field (`port`, `networks`, `humans`, `coordinator`, ...); the
  strict reader refuses one beside `served_by`. `state: absent` keeps its meaning (removed: its
  seats refused).
- `system.Life` gains a fourth answer, declared served elsewhere: its seats can be taken, and
  `seats add` takes the route from the entry. The verb checks the route with
  `ssh -T <route> /usr/local/sbin/teamchat-admin version` (`ClassAny`, no sudo) before issuing.
- An undeclared team with no status record here is refused by `seats add`, in words naming the
  entry to add to host_vars. Seats already placed for an undeclared remote team keep today's
  behaviour (R59's "no record and no entry").
- The alias is private: it joins the private-names pin (`src-go/qa/privatenames`) so it cannot
  leak into a tracked file.

### D3. The serving host chooses the number

Reading the remote roster to pick a number races with other adders. Instead `member add` gains
`--seat next` with `--avoid <seat parts>` (the parts this checkout's pool already uses): the serving
host picks the lowest free number under its own roster lock and answers the handle in the issued
document. The local-team path uses the same, so `freeHandle` has one home on the serving side.

### D4. Recovery of an enrolled seat with no credential here (the reissue case)

- A read verb on the serving host, `teamchat-admin member list --team <team> --prefix <prefix>`,
  answers each handle under the prefix with its state, enrolment time and last login. Read-only,
  reached through `member serve`'s `list`.
- The laptop classifies each **enrolled** handle under this checkout's prefix: in this checkout's
  pool (held here, nothing to do); in another checkout's pool on this machine (`seatcheckouts`,
  never touched); in no pool on this machine (**lost here**).
- A lost seat is offered in words, never by number: "A seat of team A for this repository on this
  machine was enrolled on <date> and last signed in <when>, but its credential is not on this
  machine. Give it a new credential? It keeps its history; anything still signed in with the old
  credential is signed out." The other choice is a new seat. Yes runs `member serve`'s `reissue`
  for that handle and places it; the owner never sees the handle as a thing to type.
- Without a terminal (headless, a restore), nothing is chosen: the run refuses, naming the choice
  waiting for the owner and the command that asks it.
- `member add`'s enrolled refusal becomes a typed outcome (Task 1.2), so the verb tells "taken just
  now by another adder" (retry with `next`) from "this handle" without reading words.

### D5. How it fits

- `ccy-teams seats add <team> [--host]`: the verb. A team with a status record here issues locally
  (`SudoIssuer`, unchanged); one declared `served_by` issues over the route (`SSHIssuer`); a removed,
  undeclared-remote or declared-not-installed team is refused (R59's `system.Life`).
- The launch: `Adder.NewSeat` (R5) takes the same path, so `cc` in a checkout with no seat of a
  remote team asks to fetch one, in words (closes the add half of R91).
- R90's picker (Plan 00265 Task 4.2) calls this when the checkout holds no seat; it lists a lost
  seat as a choice ("recover the seat this checkout used before") beside "a new seat".
- R75's `seats forget` deletes a retired team's pool seats here; this verb never deletes, and never
  offers a seat of a team `system.Life` calls gone.
- Retiring the other repository's scripts: once accepted on the laptop, their owner team is told the
  verb that replaces each and the sudoers rule to declare, and their headers' pointer (#416) is
  answered by a row on #416.

## Tasks

### Phase 1: buildable now

- [ ] ⬜ **Task 1.1**: `SPEC-seat-add.md` in this folder: the verb's expectations, numbered (S1...),
  each testable through fakes: inputs, the words each screen and refusal says (no seat number, no
  handle to type), exit codes (0, 1, 64, 78, cancelled), the secret rules (argv, stdin, memory,
  clear), the headless refusals, the `member serve` request schema and its refusals, and D1-D5's
  decision table (local, remote, undeclared, removed, lost, held elsewhere, race, no sudo rule).
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
- [ ] ⬜ **Task 2.2**: `served_by` on a `teamchat_teams` entry (D2), test first: the strict reader
  (`teamchat/declare`) accepts it alone with `team` and `state`, refuses it beside an install field;
  the play installs nothing for it; `system.Life`'s fourth answer; the alias in the private-names
  pin; `localhost.yml.dist` and the docs' example.
- [ ] ⬜ **Task 2.3**: `teamchat-admin member serve` (D1), test first: no arguments accepted, one
  strict capped JSON request on stdin, `add`/`reissue`/`list` only, `<type>` podman or host only,
  credential always password; every other field or value refused before any work.
- [ ] ⬜ **Task 2.4**: `SSHIssuer`, test first over a fake `execx`: the argv is exactly
  `ssh -T <route> sudo -n /usr/local/sbin/teamchat-admin member serve`, holds no secret, never
  `ssh -t`; the request is the stdin; a failure carries stderr only; `sudo -n`'s "a password is
  required" is a named refusal citing D1's rule; the answer is cleared after placement.
- [ ] ⬜ **Task 2.5**: `Adder.Add` for a team declared `served_by` takes the route instead of
  refusing 78; removed, undeclared and declared-not-installed teams are still refused (R59). The two
  hand-typed pipes in `seatadd.go` (the not-served-here and issued-not-placed errors) go: an
  issued-not-placed seat is recovered by D4, in words. They are instances of R90's class; R90's
  detector (Plan 00265 Task 4.2) must be red on them before they are removed.
- [ ] ⬜ **Task 2.6**: `Adder.NewSeat` (the launch) offers the same, in words, at a terminal; refuses
  headless, naming the command that asks.

### Phase 3: recovery

- [ ] ⬜ **Task 3.1**: `teamchat-admin member list --team --prefix` (D4), read-only, test first, and
  `member serve`'s `list`.
- [ ] ⬜ **Task 3.2**: the lost-seat classification and the owner's question (D4), test first over a
  fake roster and the fake pools of two checkouts; a seat another checkout holds is never offered.
- [ ] ⬜ **Task 3.3**: the reissue run: `member serve`'s `reissue` over the route, placed; the old
  pool record, if any, quarantined by `Place` as today.

### Phase 4: fit, docs, retirement

- [ ] ⬜ **Task 4.1**: the R90 picker's "add a seat" and "recover" rows call this path (with Plan
  00265 Task 4.2).
- [ ] ⬜ **Task 4.2**: docs: `docs/agent-bus-cross-network.md` "A member on another machine" names the
  verb first, replaces the password-sudo pipe with `member serve` and gives D1's sudoers rule as the
  serving host's to declare; `served_by` beside the `teamchat_teams` example; `docs/agent-bus.md`'s
  seats section; the `ccy-teams` usage text.
- [ ] ⬜ **Task 4.3**: a row on #416 for this verb; R91's add half pointed at this plan in the Plan
  00265 register.
- [ ] ⬜ **Task 4.4**: acceptance on the laptop against a real serving host whose IaC has declared
  D1's rule: a new seat of a remote team, and the live lost seat recovered; `acceptance.bash` in this
  folder, run through `meta-deploy.bash` on the machine it judges. Then the other repository's team
  is told to delete the two scripts.
- [ ] ⬜ **Task 4.5**: `qa-reviewer` over the plan's full diff.

## Owner questions

Both answered by the team coordinator, 26 Nov 11:07Z; reversible on the owner's word (Decisions).

- **Q1** (answered): How does the laptop get root for the seat verbs on the serving host? A narrow
  NOPASSWD sudoers rule for the owner's existing login, declared in the serving host's own IaC,
  allowing only the fixed `member serve`; no password sudo. D1.
- **Q2** (answered): Where does "team A is served by <ssh host>" live? Declared on the team's own
  `teamchat_teams` entry in the laptop's host_vars (`served_by`), not asked or kept in a seat store.
  D2.

## Success Criteria

- [ ] In team A's checkout on the laptop, one `ccy-teams seats add <team>` (or a `cc` launch) adds a
  seat of a team declared `served_by`, asking no password and only real choices in words.
- [ ] The live lost seat is recovered by the verb on the owner's word, keeping its history.
- [ ] No seat number or handle is shown as a thing to type on these paths (R90's detector green).
- [ ] The secret tests pass: no secret in argv, nothing printed, the answer cleared.
- [ ] The serving host's sudoers rule allows exactly one command line with no wildcard.
- [ ] The other repository's two scripts are deleted by their team, this verb named in their place.
- [ ] QA passes and `qa-reviewer` finds nothing blocking.

## Risks & Mitigations

| Risk                                                        | Impact | Probability | Mitigation                                                                                           |
| ----------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------------------- |
| A reissue signs out a seat another machine still uses       | H      | L           | Only handles in no pool here are offered; the words say what is signed out; the owner decides        |
| Two adders race for one number                              | M      | M           | The serving host picks under its roster lock (D3); a lost race is a named outcome (Task 1.2)         |
| The issued answer leaks into argv, a log or a pty           | H      | L           | Request on stdin, answer on stdout, no `ssh -t`, the tests of Task 2.4                               |
| The NOPASSWD rule lets the owner's login do more than issue | H      | L           | Q1 decided: one fixed command, no arguments, no `*`; `member serve` refuses all but add/reissue/list |
| The serving host's IaC has not declared the rule            | M      | M           | `sudo -n` fails at once; the verb names the rule and whose IaC adds it (Task 2.4)                    |
| A remote team's route is not declared on a laptop           | L      | M           | Q2 decided: `served_by` in host_vars; `seats add` refuses naming the entry to add (D2)               |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/ — see CLAUDE/PlanJournalling.md. -->

- Plan written.
- Q1 and Q2 decided by the team coordinator; D1 and D2 revised to match.
