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
the same precedence, in Go: one decision that `cc-project-dir` runs as a preflight and
`cc-claude`, the one step both launch paths exec `claude` from, runs again and execs. The
supervisor is the project's own code, so cc runs it only in a checkout Claude Code already
trusts (Decision 5). The expected behaviour is
[SPEC-cc-supervisor.md](SPEC-cc-supervisor.md), S0-S32.

Decisions 5, 6 and 7 are decided (by delegated review on the owner's instruction); Phase 2
may start once Phase 1 is done.

## Goals

- In a trusted project that ships a supervisor, every `cc` session (plain, `--teams`, `--bus`)
  runs as `claude-supervise ... -- claude <args>`, unarmed unless the project or the operator
  arms it (SPEC S9, S17, S25).
- In a checkout Claude Code does not trust, cc runs no project file and says so in one line
  (S28).
- `cc --supervise` runs it armed; `cc --no-supervise` (or `CCY_NO_SUPERVISOR=1`) runs claude
  unwrapped (S4, S6).
- A wrapper exported by the operator, or set by the project in `.claude/ccy/ccy.env` /
  `ccy.env.local`, is honoured, in ccy's order (S5, S7, S8).
- cc refuses to start a supervisor older than 3.71.0, naming the bypass (S12-S15), and does so
  before the token chooser, the credential parking or a seat (S31).
- No growth in `cc` or `cc-teams.bash` (S0).

## Non-Goals

- The session-lifecycle plugin (`--max-age`, `--run-for`, `--until`): those are ccy options cc
  does not have.
- Any change to `claude-yolo` or `entrypoint.sh`. The one ccy change is the
  `lib/tmux-session.bash` line of Decision 7 A, which takes the CCY version bump (Task 2.9).
- The status-line indicator that can show the supervisor as active in an unsupervised cc
  session: the issue says that is being reported to the hooks daemon separately.
- Upgrading the hooks daemon in other projects on the host. cc refuses in those that are
  trusted and still on an older daemon until they are upgraded, and says how to start without
  the supervisor (Decision 1).

## Context & Background

- The issue names the files: `files/var/local/claude-code/cc` (final launch),
  `files/var/local/claude-code/cc-teams.bash` (`cc_teams_command`,
  `pingbus seat exec ... -- /var/local/claude-code/cc-claude ...`),
  `src-go/internal/ccclaude/app.go` (`App.Run` execs `claude` from PATH), and on the ccy side
  `files/var/local/claude-yolo/claude-yolo` (`--supervise`/`--no-supervise` set
  `CCY_CLAUDE_WRAPPER`/`CCY_NO_SUPERVISOR`) and the "Supervisor wrap: DEFAULT ON" block in
  `files/var/local/claude-yolo/entrypoint.sh`.

- Today's chain for `cc --teams` on the host: tmux > cc > `pingbus seat exec` > cc-claude >
  claude. After this plan: tmux > cc > `pingbus seat exec` > supervisor > claude, the order ccy's
  entrypoint already uses.

- Both bash files are over the bash limits and on the ratchet allowlist (cc 182 code lines;
  cc-teams.bash 87 code lines and 7 functions), so the logic goes in Go. The two new flags
  already reach cc-claude on both paths unchanged; cc changes on one existing line (S31).

- cc runs `cc-project-dir` first (cc:42), before the tmux re-exec, the token chooser, the
  credential parking and, on `--teams`, `seat take` and `membership-init`. A refusal made only
  in cc-claude would come after all of those (review M5), hence the preflight.

- cc re-executes itself inside tmux. A session created on a running tmux server takes that
  server's environment, plus the names in tmux's `update-environment`, not the launching
  shell's; `lib/tmux-session.bash` forwards only `CCY_GH_TOKEN_HANDOFF` on the pane's command,
  and nothing in the repo sets `update-environment`. This has three consequences (review r2
  M7, SPEC S32):

  - cc's preflight runs once in the shell's environment and again in the pane's, so the two
    can decide differently. A refusal inside the pane comes after the session exists.
  - A setting held by the server, which is often started by the boot restore unit, leaks
    into every later cc session.
  - Forwarding only set values cannot clear a value the server holds.

  This is reasoned from tmux's documented behaviour, not yet observed here: Task 1.2
  establishes it.

- 3.71.0 also repairs a ccy wrapper written with a Python interpreter in front of the
  supervisor, which 3.69.0 and 3.70.0 had pointed at the `sh` launcher (upstream describes the
  input as `python3 .claude/ccy/claude-supervise.py`): the upgrade rewrites the interpreter
  word to `sh` (`sh .claude/ccy/claude-supervise`), keeping a quoted path's quotes (its Plan
  00540). This is why SPEC S8 accepts `sh`, refuses Python in front of the launcher, and
  refuses quotes inside the value.

- 3.71.0's ownership change "reaches a running session by the worker hot reload" (its release
  notes). The host re-fingerprints the on-disk `.py` every 5 s and swaps its worker for
  whatever is there, with no version or ownership check (`_WORKER_RELOAD_CHECK_SECONDS`,
  `reload_if_stale` in claude-supervise.py). That is the residual risk in the table below.

- Claude Code records workspace trust as `projects["<repo root>"].hasTrustDialogAccepted` in
  `~/.claude.json`: [RESEARCH-claude-code-trust-record.md](RESEARCH-claude-code-trust-record.md).

- Dedupe: `find-plan supervisor` and `find-plan cc` found no plan that owns this. Plan 00194
  lists host cc sessions as out of its scope; Plan 00146 is ccy's token switch; Plan 00167
  (cc refuses outside a repo) is the nearest cc plan and does not cover supervision.

## Tasks

### Phase 1: The daemon release that makes a host supervisor safe

- [ ] ⬜ **Task 1.1**: Upgrade this project's hooks daemon to 3.71.0 with the `hooks-daemon`
  skill (`upgrade`). The owner's approval of that upgrade is the approval for this phase.

  - [ ] ⬜ `expected_version` in `.claude/hooks-daemon.yaml` reads `3.71.0`, and
    `.claude/ccy/claude-supervise.py` declares `__version__ = "3.71.0"`
  - [ ] ⬜ `CCY_CLAUDE_WRAPPER` in `.claude/ccy/ccy.env` is unchanged: this repo's wrapper is
    the bare launcher with no interpreter word, which Plan 00540 does not rewrite. Any other
    result is recorded in the journal and SPEC S8 is checked against it
  - [ ] ⬜ Confirm from the 3.71.0 supervisor source that ownership is by child process tree,
    and whether it exposes a marker better than `__version__` for S12; record the finding in
    the journal and amend S12 if so

- [ ] ⬜ **Task 1.2**: Plan-local `triage.bash` (on `_planlib.inc.bash`, read-only) that
  establishes, for `CCY_NO_SUPERVISOR`, `CCY_CLAUDE_WRAPPER`, `CLAUDE_CONFIG_DIR` and
  `CLAUDE_CODE_IGNORE_PERSISTED_TRUST`, what a pane's command sees through cc's tmux
  insulation in three cases:

  - with no ccy tmux server running;
  - with one started by an earlier launch;
  - with one as the boot restore unit starts it.

  It covers both directions: a value exported in the shell but not on the server, and a
  value on the server but not in the shell (the reverse leak, SPEC S32). The result goes in
  the journal and confirms Decision 7 A's premise; if both directions arrive in all three
  cases, that is recorded and Decision 7 is reopened before Task 2.9 is done.

  - [ ] ⬜ The commit that adds `triage.bash` also adds this plan folder to
    `CLAUDE/Plan/meta-deploy.list`, with a comment saying it is a read-only probe of what
    reaches a cc session through tmux. The owner is told "I have updated meta-deploy; run
    `./CLAUDE/Plan/meta-deploy.bash`". The entry is removed once the result is journalled,
    until Task 3.3 adds it back

- [ ] ⬜ **Task 1.3**: Ask upstream for a hot reload that refuses a copy lacking child-tree
  ownership (review M3): body generated with `hooks-daemon issue-report`, filed, and the issue
  recorded under Risks

### Phase 2: The decision, in Go (tests first)

- [ ] ⬜ **Task 2.1**: Tests for S1-S3 (the two flags, including S1's pinned limit), red

- [ ] ⬜ **Task 2.2**: Tests for S4-S11 and S16 (precedence, the ccy.env reader with the
  parity oracle on this repo's `ccy.env`, word splitting, the accepted shapes and options,
  `--plugin` paths, path rewriting, the default and the absent supervisor), on a temporary
  checkout and the `execx.Fake` exec seam, red

- [ ] ⬜ **Task 2.3**: Tests for S25-S29, red. They cover:

  - the trust record: the config path, `HOME` and `CLAUDE_CONFIG_DIR` both empty, both path
    forms, the ignore variable, a missing file, a broken file read twice, a non-object
    `projects[key]`, a non-boolean `hasTrustDialogAccepted`;
  - the untrusted default and the refusals;
  - the key: a directory `.git`, a real worktree (absolute and relative `gitdir:` and
    back-link), a `.git` symlink, a submodule, a missing `gitdir:` target;
  - the spoofs of review r2 B2, all with no key: a forged `.git` file with its `gitdir:`
    inside the tree, a forged `commondir` naming a trusted repository, an in-tree
    `<x>/.git/worktrees/<id>` with a back-link it wrote, and a real worktree's admin
    directory whose back-link names another checkout

- [ ] ⬜ **Task 2.4**: Tests for S12-S15 (the release check), including a pre-release and a
  file with two `__version__` lines, red

- [ ] ⬜ **Task 2.5**: Tests for S17-S20 and S30 (the argv exec'd, the auto-compact window and
  `CLAUDE_PROJECT_DIR` on the wrapper's environment, exec failures, stderr only), red

- [ ] ⬜ **Task 2.6**: Tests for S31-S32 in `internal/ccproject`, red. They cover:

  - a refusal exits 1 with no directory on stdout;
  - informational lines are not printed;
  - a refusal names `ccy-sessions`;
  - arguments after `--` are cc's and are never rejected: `-- --model x` and
    `-- -h`/`-- --help` print only the directory;
  - P1 before `--` is unchanged: `--help` alone prints the usage, `--nope` exits 64.

  P1 in the package doc (`app.go`) is rewritten for the `--` contract. The
  `cmd/cc-project-dir` test keeps its `--help` and `--nope` cases and gains a `--` case

- [ ] ⬜ **Task 2.7**: Implement the decision as one package that `internal/ccclaude` and
  `internal/ccproject` call (the ccy.env reader and the trust reader in files of their own);
  rewrite the package doc's C1 and add the S list; all tests green

  - [ ] ⬜ The new package is outside `go list -deps ./cmd/claude-yolo`. The packages that
    list holds (`execx`, `gitx`, `fsx`, `shellword`, `term` among them) may be imported but
    not changed. The worktree resolver and S8's word split live in the new package. A
    change to one of those packages would need the CCY version bump
    (`CLAUDE/ContainerRules.md`), so if one turns out to be needed, it is planned here with
    its bump before it is made
  - [ ] ⬜ S25's worktree branch also requires the common directory `<C>` to be owned by the
    caller's uid, so another local user's planted `.git` (for example under `/tmp`) never
    passes, even where the owner once trusted that directory; a test covers it
  - [ ] ⬜ `./scripts/qa-changed.bash` passes, the Go gate included

- [ ] ⬜ **Task 2.8**: Change cc's `cc-project-dir` call to pass `-- "$@"` (S31); confirm
  `cc-teams.bash` is untouched, that `scripts/qa-bash-limits.bash` reports cc's counts
  unchanged, and that `scripts/test-cc-teams.bash` still passes

- [ ] ⬜ **Task 2.9**: Decision 7 A: the `lib/tmux-session.bash` line 422 change, forwarding
  `CCY_NO_SUPERVISOR`, `CCY_CLAUDE_WRAPPER`, `CLAUDE_CONFIG_DIR` and
  `CLAUDE_CODE_IGNORE_PERSISTED_TRUST` as `NAME=value` when set and `env -u NAME` when
  unset, with the CCY version bump and a `docs/ccy-changelog.md` entry under that version.
  Check that ccy's launcher behaves the same with the forwarded variables

- [ ] ⬜ **Task 2.10**: Document the flags, the precedence, the trust rule, the 3.71.0 floor,
  that `--supervise` drops a project wrapper's `--plugin` words, and the hot-reload risk in the
  cc section of `docs/ccy.md`. The `docs/ccy-changelog.md` entry goes under the CCY version
  Task 2.9 makes. Update the `internal/ccclaude` and `internal/ccproject` lines of
  `src-go/CLAUDE.md`'s layout

### Phase 3: Host deploy and acceptance

- [ ] ⬜ **Task 3.1**: Plan-local `deploy.bash` running `playbooks/imports/play-claude-yolo.yml`
  (it builds and installs `/var/local/claude-code/cc-claude` and `cc-project-dir`), on
  `_planlib.inc.bash`
- [ ] ⬜ **Task 3.2**: Plan-local `acceptance.bash` for S21-S24, with a coverage line: the
  fixture cases drive `cc-claude` directly with a stub `claude` and a temporary
  `CLAUDE_CONFIG_DIR`; what needs the real `cc` (a person at the token chooser, a live
  terminal, the real registry) is named NOT ESTABLISHABLE and listed for the owner
- [ ] ⬜ **Task 3.3**: Add this plan folder to `CLAUDE/Plan/meta-deploy.list` in the commit that
  adds `deploy.bash` and `acceptance.bash` (Tasks 3.1-3.2), with a comment saying it rebuilds
  cc-claude and cc-project-dir and checks cc sessions run supervised; tell the owner "I have
  updated meta-deploy; run `./CLAUDE/Plan/meta-deploy.bash`"
- [ ] ⬜ **Task 3.4**: The owner's run passes; remove the entry from `meta-deploy.list`

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: `qa-reviewer` agent over the plan's full diff; resolve every BLOCK and
  FIX-BEFORE-MERGE finding
- [ ] ⬜ **Task 4.2**: Plan Complete, moved to `Completed/`, README index updated

## Dependencies

- Depends on: hooks-daemon 3.71.0 in this project (Task 1.1).
- Related: Plan 00167 (cc refuses to start outside a repo), whose `cc-project-dir` gives the
  checkout cc-claude runs in and hosts the preflight.

## Technical Decisions

Each decision below records the plan's default. Decisions 5, 6 and 7 were decided by
delegated review on the owner's instruction. The owner may overturn any decision before
Phase 2 starts.

### Decision 1: An older supervisor is refused, on every path

**Context**: The issue says host wrapping should require the 3.71.0 fix and "refuse or warn"
on an older one. Every other project on the host keeps its own daemon release until it is
upgraded.
**Options considered**: (A) refuse on every path, naming `cc --no-supervise` and
`export CCY_NO_SUPERVISOR=1`; (B) warn and run the old supervisor (it would act on other
sessions: harmful); (C) refuse when the supervisor was asked for (`--supervise`, a configured
wrapper) and run unwrapped with a loud line on the default path.
**Decision**: A. B does harm. C spares less than it seems: the daemon's own `ccy.env` sets a
wrapper (`--arm`), so every project carrying it counts as configured and C refuses there too;
C only spares a project that ships the supervisor with no ccy.env wrapper, and runs that one
unwrapped while it looks supervised. That is the skip-and-continue the fail-fast rule bans.
This is consistent with S10 running unwrapped when the supervisor is absent: absent means
nothing is configured to run, old means the project is configured for a supervisor this host
must not run. The cost is that `cc` in a trusted project still on 3.70.0 or older stops until
its daemon is upgraded; the refusal names the per-launch escape and the persistent one
(`export CCY_NO_SUPERVISOR=1`), which matters across many not-yet-upgraded projects. With
Decision 5's trust rule, an untrusted project is never refused for this.
**Date**: 2026-10-26

### Decision 2: Project files are read, never sourced; only the project's own supervisor runs, and only in a trusted checkout

**Context**: ccy sources `ccy.env` inside its container. On the host, sourcing it would run a
cloned project's code with the owner's rights as soon as cc starts. Running the supervisor
does the same: `claude-supervise` and its `.py` are files of the repository being opened, and
so is a `--plugin` path. Any repository holding a file at that path with a 3.71.0 version line
would have it run on a plain `cc`, before Claude Code's own trust dialog, which is what gates
project hooks today (review B1).
**Options considered**: (A) source the files in bash; (B) read the two assignment forms in Go
and accept only a wrapper that runs the project's own supervisor; (C) ignore project files on
the host.
**Decision**: B (SPEC S7, S8). It bounds the shape of what a ccy.env line can run to the
supervisor at its fixed path, with plugin files under the checkout's path. Those checks
follow symlinks, so they are not a security boundary: trust is (Decision 5, S25-S29). A
trusted repository's hooks can already run anything. B does not stop project code from
running: it is the project's code. Whether that code runs is Decision 5's question. An operator who wants another
wrapper exports it (S5). C would break the issue's expected behaviour.
**Date**: 2026-10-26

### Decision 3: `--supervise` outranks the project files

**Context**: In ccy, `--supervise` exports a wrapper that a plain assignment in `ccy.env`
still overrides (only the `:-` form yields to it).
**Decision**: In cc, `--supervise` beats both project files (S6 above S7): it is the
operator's explicit instruction for this launch. It replaces the project's wrapper whole, so
the project's `--plugin` words are dropped with it. This is the one place cc's order differs
from ccy's, and the docs say both.
**Date**: 2026-10-26

### Decision 4: Through the launcher, with no parse probe

**Context**: ccy's default runs `python3 claude-supervise.py --` after an `ast.parse` probe.
**Decision**: The default and `--supervise` run the daemon's `sh` launcher
(`claude-supervise`), which picks a Python of at least 3.11, as the daemon's own `ccy.env`
does; a project wrapper naming the bare launcher (this repo's) is exec'd directly. No
`ast.parse` probe: the release check (S12) already reads the file, and a supervisor that fails
to start fails loudly in the session's own terminal.
**Date**: 2026-10-26

### Decision 5 (decided): cc runs project code only in a checkout Claude Code already trusts

**Context**: Decision 2 bounds what runs but not whether a stranger's repository may run it.
Claude Code asks its owner before it uses a repository's hooks (the workspace trust dialog);
the supervisor would otherwise run before that question is ever asked.
**Options considered**:

- (A) **Trust-gated** (the default). cc wraps only when Claude Code's own record says its owner
  accepted the trust dialog for the checkout (`projects["<key>"].hasTrustDialogAccepted` in
  `~/.claude.json`, read in Go, nothing run; S25-S27). Otherwise it launches unwrapped and
  says why in one stderr line, naming `cc --supervise` and how to trust (S28); claude then
  shows its trust dialog, and the next cc is supervised. The key is never taken from a file
  the launched tree can write: a directory `.git` keys on the checkout itself, and a
  worktree only through git's back-link in the main repository (S25, review r2 B2).
  Submodules and `--separate-git-dir` checkouts are therefore never trusted by cc. For: cc
  runs a checkout's code only under a record of the owner's acceptance that the checkout
  cannot redirect; no new list for the owner to keep. Against: it reads another program's state file, whose format is Claude
  Code's (documented as the way to trust a folder by hand, so not a private detail, but it can
  change; a change makes cc run unwrapped, the safe direction, or refuse on a malformed file);
  the first session in a new checkout is unsupervised.
- (B) **Accept the risk and document it**: wrap by default wherever the supervisor exists, with
  the sentence in `docs/ccy.md` that `cc` runs a repository's `.claude/ccy/claude-supervise`
  without asking. For: simplest, every session supervised. Against: cloning and running `cc`
  in an unknown repository runs its code with the owner's rights before any question.
- (C) **An allowlist of checkouts in the operator's config**. For: explicit, independent of
  Claude Code's internals. Against: a second trust list beside the one the owner already keeps
  through Claude Code's dialog, which drifts from it; every new checkout and worktree path must
  be added by hand; and it needs a new config file and its own tooling.

`cc --supervise` still requires trust (S29). The flag is typed per launch, often out of habit,
by an alias or by a restore replaying argv, and none of those is a review of the code; the
trust record is. Untrusted, it is refused rather than run unwrapped, keeping S6's rule that
what was asked for is never quietly dropped. The same applies to an operator wrapper whose
words name a file inside the checkout.
**Decision**: A, decided by delegated review on the owner's instruction. B is the one option
that runs a stranger's code with the owner's rights on a plain `cc`, before any question, and
no sentence in the docs undoes that. C is safe but adds a second trust list that the owner
keeps by hand, a config file and tooling, and drifts from the record Claude Code already
keeps through its dialog. A is one documented field read in Go with nothing run, keyed by
something the launched tree cannot write, and its only cost in daily use is that the first
session in a new checkout runs unsupervised with one line saying why; the dialog the owner
answers in that session is the whole of the setup. On the day-to-day convenience and the
safety it is the best of the three, and on simplicity it is second only to B.
**Date**: 2026-10-26

### Decision 6 (decided): Every check is made before cc's side effects

**Context**: cc-claude runs last, after the token chooser, the credential parking, and on
`--teams` after `seat take` (sudo for a new seat) and `membership-init` (review M5).
**Options considered**: (A) a preflight in `cc-project-dir`, which cc runs first, by passing
cc's arguments on the existing call line (no new code line); (B) refuse in cc-claude and rely
on the cc trap and pingbus to undo the side effects; (C) a new preflight call in cc (a new
line, which the ratchet forbids).
**Decision**: A, decided by delegated review on the owner's instruction. A refusal belongs
before the token chooser, the credential parking and a seat, not after them: B relies on
the trap and pingbus undoing side effects that need not have happened, and leaves a person
at the token chooser for a launch that will not start; C adds the line the ratchet forbids
for no gain over A, since cc already runs `cc-project-dir` first (cc:42). A changes one
existing line of `cc` and nothing in `cc-teams.bash`. Under A, cc passes `-- "$@"` (S31), so
cc-project-dir's own argument contract stays as it is before the `--`. cc-claude repeats the
decision at exec (S32), and cc-claude's decision is the one that counts. With Decision 7 A
the pane's environment equals the shell's for the four inputs that matter, so a refusal
inside the pane comes only from a real change in between (a `git checkout` after the
preflight); that refusal is loud, and its side effects are undone as B describes, checked
in acceptance. Under A, a refusal also stops `cc` re-attaching a detached session here; the
refusal names `ccy-sessions` (S31).
**Date**: 2026-10-26

### Decision 7 (decided): An exported setting reaches the session

**Context**: S4, S5 and S13's persistent escape rely on the environment reaching cc-claude, but
cc's session runs in tmux, which may hand it the tmux server's environment instead of the
launching shell's.
Review r2 M7 adds two cases. A server that holds one of these values, often because the boot
restore unit started it, leaks it into every later cc session. And the two preflights then
decide on different environments (SPEC S32).
**Options considered**:

- (A) Forward `CCY_NO_SUPERVISOR`, `CCY_CLAUDE_WRAPPER`, `CLAUDE_CONFIG_DIR` and
  `CLAUDE_CODE_IGNORE_PERSISTED_TRUST` explicitly on the pane's command. Each one is
  forwarded as `NAME=value` when set and as `env -u NAME` when unset, so a value the server
  holds is cleared as well as a missing one supplied. The `${VAR:+env …}` idiom used for
  `CCY_GH_TOKEN_HANDOFF` cannot clear. This edits the existing line 422 of
  `lib/tmux-session.bash`, which is ccy code, so it takes the CCY version bump. The line is
  shared with ccy's launcher, so Task 2.9 checks that ccy does not change behaviour on the
  forwarded `CLAUDE_CONFIG_DIR`. With A, the pane's decision equals the shell's except for a
  real change in between.
- (B) Make the flags the only reliable form, and name a shell alias
  (`alias cc='cc --no-supervise'`) as the persistent escape in S13. This leaves the reverse
  leak.
- (C) Leave it, documented.
- (D) Add the four names to ccy's tmux server's `update-environment`. tmux copies those
  names from the client into a new session and removes the ones the client lacks. That
  closes both directions too, but it changes a server-wide option that ccy's sessions share.

A session restored by `ccy-sessions` replays argv only, so under A or D a restore is decided
by its flags and the restore unit's environment.
**Decision**: A, decided by delegated review on the owner's instruction, without waiting for
Task 1.2. tmux's documented rule, that a session created on a running server takes the
server's global environment, is exactly `ccy_tmux_start_detached`'s case (`new-session` on
ccy's own server), and on this host that server is usually started by the boot restore unit,
so the reverse leak is there whichever way the probe lands; Task 1.2 now confirms the
premise rather than gating the choice. B and C leave that leak and make the two preflights
disagree systematically, which turns S4's and S13's escapes into advice that works in one
environment and not the other. D closes both directions too, but by a server-wide option
every ccy session shares and that depends on the client's environment at attach time; A is
local to the one launch line, deterministic (set values forwarded, unset ones cleared), and
makes the pane's decision equal the shell's. Its cost is one edited line of ccy code and the
CCY version bump, which Task 2.9 carries.
**Date**: 2026-10-26

## Success Criteria

- [ ] Every expectation in SPEC-cc-supervisor.md S1-S20 and S25-S32 has a passing Go test
  named for it
- [ ] `cc-teams.bash` is unchanged and `cc` differs by its one `cc-project-dir` line, with the
  bash-limit counts of both unchanged
- [ ] The owner's `acceptance.bash` run passes S21-S24 on the host
- [ ] `./scripts/qa-changed.bash` passes on each commit and `./scripts/qa-all.bash` before push
- [ ] `qa-reviewer` finds no unresolved BLOCK or FIX-BEFORE-MERGE

## Risks & Mitigations

| Risk                                                                                                                                                                                             | Impact | Probability | Mitigation                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------ | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A `git checkout` of a pre-3.71.0 commit in a checkout with a live host session hot-reloads old ownership code into that session's worker within seconds, and it acts on every session it can see | H      | M           | cc checks only at launch and cannot guarantee what the worker loads later; re-checking at reload is the daemon's (Task 1.3 asks upstream). Until then the docs tell the owner not to check out a pre-3.71.0 commit while a cc session runs in that checkout. Setting `CLAUDE_SUPERVISE_NO_WORKER` would pin the launch-time code but loses the worker's isolation and upgrades, so it is not the default |
| cc stops in every trusted host project still on an older daemon                                                                                                                                  | M      | M           | The refusal names `cc --no-supervise`, `export CCY_NO_SUPERVISOR=1` and the upgrade; untrusted projects are not refused (Decision 1)                                                                                                                                                                                                                                                                     |
| An exported `CCY_NO_SUPERVISOR` does not reach the session through tmux, or a value the tmux server holds reaches every later session (SPEC S32)                                                 | M      | M           | Decision 7 A forwards set values and clears unset ones (Task 2.9); Task 1.2 confirms both directions on the host; a refusal inside the pane is loud and its side effects undone (S32)                                                                                                                                                                                                                    |
| A copied or extracted tree forges a `.git` file and `commondir` so that cc keys it on a trusted repository                                                                                       | H      | L           | S25 takes no key from a file the tree can write: a directory `.git`, or a worktree verified by git's back-link in the main repository; Task 2.3 tests the forgeries                                                                                                                                                                                                                                      |
| A submodule or a `--separate-git-dir` checkout is never trusted by cc, though Claude Code trusts it                                                                                              | L      | L           | Unwrapped with S28's line, the safe direction; documented in Task 2.10                                                                                                                                                                                                                                                                                                                                   |
| Claude Code changes where or how it records trust                                                                                                                                                | M      | L           | cc then finds no record and runs unwrapped, saying why (S28); a malformed file is refused (S27); acceptance S21 confirms the record in this checkout                                                                                                                                                                                                                                                     |
| Something that follows a cc session expects the pid pingbus execs to be claude (bus wake, `ccy-teams` hot control)                                                                               | M      | M           | SPEC S23 in acceptance; the same chain already works in ccy                                                                                                                                                                                                                                                                                                                                              |
| A claude argument whose value is exactly `--supervise` is taken as cc's flag                                                                                                                     | L      | L           | Only whole arguments before a bare `--` (S1); a test pins it; documented                                                                                                                                                                                                                                                                                                                                 |
| A project's ccy.env sets the wrapper in a form S7 does not read                                                                                                                                  | L      | M           | Refused with the file and line, never ignored (S7)                                                                                                                                                                                                                                                                                                                                                       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00226-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and SPEC written
- Review round 1 answered in the plan and SPEC (Decisions 5-7 added)
- Review round 2 answered in the plan and SPEC (back-link trust key, presence before trust,
  the `--` preflight contract, which environment decides)
- Decisions 5, 6 and 7 decided (A, A, A) by delegated review on the owner's instruction
