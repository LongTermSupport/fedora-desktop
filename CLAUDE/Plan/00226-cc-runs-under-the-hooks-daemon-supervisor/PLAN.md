# Plan 00226: cc runs under the hooks daemon supervisor

**Status**: Not Started
**Created**: 2026-10-26
**Owner**: joseph
**Priority**: Medium

Addresses #113.

## Overview

A Claude Code session started on the host with `cc` (plain, `--teams` or `--bus`) always runs
`claude` directly. It never runs under the hooks-daemon PTY supervisor
(`.claude/ccy/claude-supervise.py`), even in a project that ships one and that a ccy session in
the same checkout runs under. So a host session goes without auto-compaction, continue and goal
injection, and the ctrl+z guard. `cc` has no supervise path at all: no flag, no
`CCY_CLAUDE_WRAPPER`, no look for a project supervisor.

The wrap was left out on purpose when `cc-claude` was added (commit 70457da5f): the supervisor
of that time took as its own every session id it could read in `/proc`, which is safe only in
a container's private PID namespace. On the host it would have acted on other sessions'
compaction signals and sidecars. hooks-daemon 3.71.0 fixed that upstream (its Plan 00551: a
session is the supervisor's own only when it is in the supervisor's child process tree). This
repo's daemon is 3.70.0.

This plan upgrades the daemon to 3.71.0 and then gives `cc` ccy's supervisor behaviour, with
the same precedence, in `cc-claude` (Go), the one step both launch paths exec `claude` from.
The expected behaviour is [SPEC-cc-supervisor.md](SPEC-cc-supervisor.md), S0-S24.

## Goals

- In a project that ships a supervisor, every `cc` session (plain, `--teams`, `--bus`) runs as
  `claude-supervise ... -- claude <args>`, unarmed by default (SPEC S9, S17).
- `cc --supervise` runs it armed; `cc --no-supervise` (or `CCY_NO_SUPERVISOR=1`) runs claude
  unwrapped (S4, S6).
- A wrapper exported by the operator, or set by the project in `.claude/ccy/ccy.env` /
  `ccy.env.local`, is honoured, in ccy's order (S5, S7, S8).
- cc refuses to start a supervisor older than 3.71.0, naming the bypass (S12-S15).
- No growth in `cc` or `cc-teams.bash` (S0).

## Non-Goals

- The session-lifecycle plugin (`--max-age`, `--run-for`, `--until`): those are ccy options cc
  does not have.
- Any change to ccy: `claude-yolo` and `entrypoint.sh` are unchanged, so no CCY version bump.
- The status-line indicator that can show the supervisor as active in an unsupervised cc
  session: the issue says that is being reported to the hooks daemon separately.
- Upgrading the hooks daemon in other projects on the host. cc refuses in those until they
  are upgraded, and says how to start without the supervisor (Decision 1).

## Context & Background

- The issue names the files: `files/var/local/claude-code/cc` (final launch),
  `files/var/local/claude-code/cc-teams.bash` (`cc_teams_command`, `pingbus seat exec ... -- /var/local/claude-code/cc-claude ...`), `src-go/internal/ccclaude/app.go` (`App.Run` execs
  `claude` from PATH), and on the ccy side `files/var/local/claude-yolo/claude-yolo`
  (`--supervise`/`--no-supervise` set `CCY_CLAUDE_WRAPPER`/`CCY_NO_SUPERVISOR`) and the
  "Supervisor wrap: DEFAULT ON" block in `files/var/local/claude-yolo/entrypoint.sh`.
- Today's chain for `cc --teams` on the host: tmux > cc > `pingbus seat exec` > cc-claude >
  claude. After this plan: tmux > cc > `pingbus seat exec` > supervisor > claude, the order ccy's
  entrypoint already uses.
- Both bash files are over the bash limits and on the ratchet allowlist (cc 182 code lines;
  cc-teams.bash 87 code lines and 7 functions), so the logic goes in Go, and the two new flags
  already reach cc-claude on both paths unchanged.
- The 3.71.0 upgrade also rewrites a ccy wrapper written as `python3 .claude/ccy/claude-supervise` to `sh .claude/ccy/claude-supervise` (its Plan 00540), which is
  why SPEC S8 accepts `sh`, a relative path and `/workspace/`.
- Dedupe: `find-plan supervisor` and `find-plan cc` found no plan that owns this. Plan 00194
  lists host cc sessions as out of its scope; Plan 00146 is ccy's token switch; Plan 00167
  (cc refuses outside a repo) is the nearest cc plan and does not cover supervision.

## Tasks

### Phase 1: The daemon release that makes a host supervisor safe

- [ ] ⬜ **Task 1.1**: Upgrade this project's hooks daemon to 3.71.0 with the `hooks-daemon`
  skill (`upgrade`). The owner's approval of that upgrade is the approval for this phase.
  - [ ] ⬜ `expected_version` in `.claude/hooks-daemon.yaml` reads `3.71.0`, and
    `.claude/ccy/claude-supervise.py` declares `__version__ = "3.71.0"`
  - [ ] ⬜ Read the upgrade output for a rewritten `CCY_CLAUDE_WRAPPER` in
    `.claude/ccy/ccy.env` and record the line it now holds (SPEC S8 must accept it)
  - [ ] ⬜ Confirm from the 3.71.0 supervisor source that ownership is by child process tree,
    and whether it exposes a marker better than `__version__` for S12; record the finding in
    the journal and amend S12 if so

### Phase 2: cc-claude chooses and runs the wrapper (tests first)

- [ ] ⬜ **Task 2.1**: Tests for S1-S3 (the two flags) in `src-go/internal/ccclaude`, red
- [ ] ⬜ **Task 2.2**: Tests for S4-S11 and S16 (precedence, the ccy.env reader, the accepted
  wrapper shapes, path rewriting, the default and the absent supervisor), on a temporary
  checkout and the `execx.Fake` exec seam, red
- [ ] ⬜ **Task 2.3**: Tests for S12-S15 (the release check), including a pre-release and a
  file with two `__version__` lines, red
- [ ] ⬜ **Task 2.4**: Tests for S17-S20 (the argv exec'd, the auto-compact window on the
  wrapper's environment, exec failures, stderr only), red
- [ ] ⬜ **Task 2.5**: Implement in `internal/ccclaude` (the ccy.env reader in its own file);
  rewrite the package doc's C1 and add the S list; all tests green
  - [ ] ⬜ `./scripts/qa-changed.bash` passes, the Go gate included
- [ ] ⬜ **Task 2.6**: Confirm the diff touches neither `files/var/local/claude-code/cc` nor
  `cc-teams.bash` (S0), and that `scripts/test-cc-teams.bash` still passes
- [ ] ⬜ **Task 2.7**: Document the flags, the precedence and the 3.71.0 floor in the cc
  section of `docs/ccy.md`, with an entry in `docs/ccy-changelog.md`; update the
  `internal/ccclaude` line of `src-go/CLAUDE.md`'s layout

### Phase 3: Host deploy and acceptance

- [ ] ⬜ **Task 3.1**: Plan-local `deploy.bash` running `playbooks/imports/play-claude-yolo.yml`
  (it builds and installs `/var/local/claude-code/cc-claude`), on `_planlib.inc.bash`
- [ ] ⬜ **Task 3.2**: Plan-local `acceptance.bash` for S21-S24, with a coverage line; what a
  script cannot establish (a person at the token chooser, a live terminal) is named NOT
  ESTABLISHABLE and listed for the owner
- [ ] ⬜ **Task 3.3**: Add this plan folder to `CLAUDE/Plan/meta-deploy.list` in the commit that
  lands Phase 2, with a comment saying it rebuilds cc-claude and checks cc sessions run
  supervised; tell the owner "I have updated meta-deploy; run `./CLAUDE/Plan/meta-deploy.bash`"
- [ ] ⬜ **Task 3.4**: The owner's run passes; remove the entry from `meta-deploy.list`

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: `qa-reviewer` agent over the plan's full diff; resolve every BLOCK and
  FIX-BEFORE-MERGE finding
- [ ] ⬜ **Task 4.2**: Plan Complete, moved to `Completed/`, README index updated

## Dependencies

- Depends on: hooks-daemon 3.71.0 in this project (Task 1.1).
- Related: Plan 00167 (cc refuses to start outside a repo); the checkout cc-claude resolves is
  the same git top level.

## Technical Decisions

Each decision below records the plan's default. The owner may overturn any of them before
Phase 2 starts.

### Decision 1: An older supervisor is refused, on every path

**Context**: The issue says host wrapping should require the 3.71.0 fix and "refuse or warn"
on an older one. Every other project on the host keeps its own daemon release until it is
upgraded.
**Options considered**: (A) refuse on every path, naming `cc --no-supervise`; (B) warn and run
the old supervisor (it would act on other sessions: harmful); (C) refuse when the supervisor was
asked for (`--supervise`, a configured wrapper) and run unwrapped with a loud line on the
default path.
**Decision**: A. B does harm, and C is the skip-and-continue the fail-fast rule bans: a
project that ships a supervisor would run without it while looking configured. The cost is
that `cc` in a project still on 3.70.0 or older stops until that project's daemon is
upgraded, or until the operator passes `--no-supervise`; the refusal says both. C is the
alternative the owner may prefer for that reason.
**Date**: 2026-10-26

### Decision 2: Project files are read, never sourced, and may only name the project supervisor

**Context**: ccy sources `ccy.env` inside its container. On the host, sourcing it, or running
whatever wrapper it names, would run a cloned project's code with the owner's rights as soon as
cc starts.
**Options considered**: (A) source the files in bash; (B) read the two assignment forms in Go
and accept only a wrapper that runs the project's own supervisor; (C) ignore project files on
the host.
**Decision**: B (SPEC S7, S8). It honours what projects actually write (the daemon's own
`${CCY_CLAUDE_WRAPPER:-... --arm --}` line) without running project code. An operator who wants
another wrapper exports it (S5). C would break the issue's expected behaviour.
**Date**: 2026-10-26

### Decision 3: `--supervise` outranks the project files

**Context**: In ccy, `--supervise` exports a wrapper that a plain assignment in `ccy.env`
still overrides (only the `:-` form yields to it).
**Decision**: In cc, `--supervise` beats both project files (S6 above S7): it is the
operator's explicit instruction for this launch. This is the one place cc's order differs from
ccy's, and the docs say so.
**Date**: 2026-10-26

### Decision 4: In cc-claude, through the launcher, with no parse probe

**Context**: The logic could live in `cc`, `cc-teams.bash` or `cc-claude`. ccy's default runs
`python3 claude-supervise.py --` after an `ast.parse` probe.
**Decision**: `cc-claude` (Go), because the two bash files may not grow and it is the one step
both paths exec claude from. The default and `--supervise` run the daemon's `sh` launcher
(`claude-supervise`), which picks a Python of at least 3.11, as the daemon's own `ccy.env`
does. No `ast.parse` probe: the release check (S12) already reads the file, and a supervisor
that fails to start fails loudly in the session's own terminal.
**Date**: 2026-10-26

## Success Criteria

- [ ] Every expectation in SPEC-cc-supervisor.md S1-S20 has a passing Go test named for it
- [ ] `files/var/local/claude-code/cc` and `cc-teams.bash` are unchanged by the plan
- [ ] The owner's `acceptance.bash` run passes S21-S24 on the host
- [ ] `./scripts/qa-changed.bash` passes on each commit and `./scripts/qa-all.bash` before push
- [ ] `qa-reviewer` finds no unresolved BLOCK or FIX-BEFORE-MERGE

## Risks & Mitigations

| Risk                                                                                                               | Impact | Probability | Mitigation                                                                                |
| ------------------------------------------------------------------------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------------------------- |
| cc stops in every other host project still on an older daemon                                                      | M      | H           | The refusal names `cc --no-supervise` and the upgrade; Decision 1 records the alternative |
| Something that follows a cc session expects the pid pingbus execs to be claude (bus wake, `ccy-teams` hot control) | M      | M           | SPEC S23 in acceptance; the same chain already works in ccy                               |
| A claude argument whose value is exactly `--supervise` is taken as cc's flag                                       | L      | L           | Only whole arguments before a bare `--` (S1); documented                                  |
| A project's ccy.env sets the wrapper in a form S7 does not read                                                    | L      | M           | Refused with the file and line, never ignored (S7)                                        |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00226-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and SPEC written
