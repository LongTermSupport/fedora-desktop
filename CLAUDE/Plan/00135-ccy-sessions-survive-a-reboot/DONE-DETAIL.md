# Plan 00135: detail of completed tasks

The full text of every completed task in [PLAN.md](PLAN.md), moved here verbatim so
PLAN.md stays lean. Open work stays in PLAN.md. Organised by phase and task number.

## Phase 1: The session registry

> Independent of the daemon CLI and of restore. Lands first; nothing else needs it to
> exist to be useful, and it is what makes restore possible at all.

### Task 1.1

Registry format and location. One file per session under
`~/.local/state/ccy/sessions/`, named for the tmux session. Records: tmux session name,
project directory, launcher, prefix, restore flag, and the launch arguments with one-shot
arguments removed. `lib/session-registry.bash`; location decision in the 26-09-22 journal.

**The one-shot set is already enumerated with file:line citations** in
[research/launcher-facts.md](research/launcher-facts.md) §4 — use it rather than
re-deriving. Three things from it change this task:

- **`--prevent` is destructive to replay.** It writes `never` into
  `.claude/ccy/allowed-hostnames`, disabling ccy for that project. A restore that
  replayed argv verbatim would turn ccy off for the project it was restoring. This is
  the case that makes the filter load-bearing rather than tidy.
- **`--continue` is not a ccy flag at all** — it is Claude Code's own and falls through
  to `CLAUDE_ARGS` (`claude-yolo:638-640`). The plan's "plus `--continue`" is a
  passthrough, not a ccy option.
- **`--ssh-agent` needs a decision**: the agent socket differs after a reboot, so
  replaying it points at a socket that no longer exists.

### Task 1.2

Write on start, delete on clean exit, both inside
`ccy_tmux_insulate`. The write is there; the delete rides in the pane's trampoline
(`ccy_registry_trampoline`), which runs when the launcher returns and not when the pane
is killed. `ccy-sessions` Ctrl-X removes the record itself, since a `kill-session` never
reaches the trampoline.

### Task 1.3

`--no-restore` marking, consumed by the insulation; neither launcher
sees it. `cc` strips it again on the paths where insulation does not apply.

### Task 1.4

`scripts/test-ccy-session-registry.bash`, wired into `qa-all.bash`.
Covers the write, the clean and failing exits, the **kill** (the real trampoline under
`kill -KILL`), the one-shot filter case by case, `--no-restore`, a directory with spaces,
and the malformed-record rejections.

## Phase 2: The restore service

### Task 2.1

`ccy-sessions-restore.service`, `systemd --user`,
`WantedBy=default.target`, running `ccy-sessions restore`. Each record is started through
`ccy_tmux_start_detached` — the same function the interactive start now uses — with the
recorded arguments plus `--continue`, and `--supervise` for `ccy` (not `cc`, which
forwards its argv to `claude`).

### Task 2.2

`ccy_restore_sessions` (`| default(false)`), declared in `host_vars`.
Linger untouched; the play comment names its owner.

### Task 2.3

In `play-claude-yolo.yml`: enable (or remove the wants-symlink when
not opted in), reload as its own task, read back `list-dependencies default.target` and
assert the live graph matches the opt-in either way.

### Task 2.4

Decided (journal 26-09-22): vanished directory → error, record kept,
run continues, unit ends failed; live name → skip and say so; unreadable live list →
start nothing. Documented in `docs/ccy.md`.

### Task 2.5

The translation is tested in `test-ccy-session-registry.bash` over a
stubbed live list and a recording stub for the start; the executable's `restore`
subcommand is exercised headless in `test-ccy-sessions-reboot.bash`.

## Phase 3: The reboot helper

### Task 3.1

`ccy-sessions` is a dispatcher: bare = picker, `notify`, `reboot`,
`restore`, `--help`. The TTY guard sits under the dispatch, on the picker path only.
Usage mistakes exit 64, refusals exit 1.

### Task 3.2

`notify reboot-warning --minutes N`, `notify shutdown-warning`,
`notify reboot-cancelled`. Every live project is checked for a daemon CLI **before** any
is signalled, so a refusal leaves no project half-warned. The CLI is run inside the
container for a project with a ccy session, and asked for `signal --help` first
(journal 26-09-25: a ccy-only project's host CLI had no venv).

### Task 3.3

`reboot --in N [--dry-run]`: warn N, wait, warn 1, wait, `systemctl reboot`. `--in 1` warns once. Minutes are a positive integer or a usage error.

### Task 3.4

`scripts/test-ccy-sessions-reboot.bash`, wired into `qa-all.bash`:
the real executable under a fake `tmux`, a fake `systemctl` and a per-project logging
stand-in for the daemon CLI, with the minute shortened to zero.

### Task 3.5

`reboot-with-update [--in N]` (owner's request, 26-09-22):
`shutdown-with-update` under a second name, symlinked by `play-basic-configs.yml`. Same
updates, then `ccy-sessions notify` as the invoking user for the two warnings, then
`systemctl reboot` as root (polkit refuses a plain user's reboot over SSH). Rehearses the
warning with `--dry-run` before updating, so an unwarnable session refuses early.
🧑 Not unit-tested: the body is dnf and firmware; Phase 5 proves it.

### Task 3.6

`shutdown-with-update` warns too (owner's decision, 26-09-22). The
rehearsal, the two warnings and the countdown run under both names; the name chooses the
signal kind (`shutdown-warning` or `reboot-warning`) and the last step. Both now need
`SUDO_USER`. Open decision 3 in PLAN.md is thereby reversed.

### Task 3.7

Review fixes to 3.6 (`subagent-reports/260923-t36-fixes-opus-5.md`):

- The warning kind follows the **restore opt-in**, not the power action. The daemon's
  two texts encode "a restore will follow" and "NO restore, leave a handoff". So
  `ccy-sessions notify going-down` sends `reboot-warning` when this user's restore unit
  is enabled and `shutdown-warning` when it is not. `ccy-sessions reboot` and both
  names of `shutdown-with-update` use it.
- **Known and not fixed here:** the same texts name the action. With restore on, a
  shutdown reads "will reboot"; with restore off, a reboot reads "will shut down". What
  the agent is asked to do is right; the verb is not. Fixing that is a hooks-daemon
  change: a kind for "restore follows" separate from the action. It has **not** been
  filed, because the tracker is public; that is the owner's call.
- From the first warning on, any exit that is not the machine going down sends
  `reboot-cancelled` and exits non-zero. That covers a warning that fails part-way or
  at one minute, Ctrl-C, a blocked shutdown answered N, no terminal to ask on, and a
  forced poweroff or reboot request that fails.
- A root shell (`SUDO_USER=root`) is refused before anything runs.
- A dangling wants-symlink reads as restore off.
- Each case above is driven under fakes in `scripts/test-ccy-sessions-reboot.bash`. A
  real `shutdown -h now` blocked by inhibitors is left to Task 5.7.

### Task 3.8

Fixes ported from the superseded PR #47's review
(`subagent-reports/260923-pr47-ports-opus-5.md`), CCY 3.62.0:

- `ccy-sessions reboot` withdraws its warning on every non-zero exit, as Task 3.7 made
  `shutdown-with-update` do. A withdrawal carries on past a project that refuses it.
- Derived guards in `test-ccy-session-registry.bash`: every launcher flag has a replay
  decision, and every `read -p` prints a registered prompt or is listed as unreachable.
  The compose-stop and token-setup prompts are now registered.
- A registry path that exists but cannot be listed fails the restore.

## Phase 4: Docs

### Task 4.1

`docs/tmux-sessions.md`: the row stays "gone" for plain tmux
sessions (true), and the sentence beneath it says which sessions are the exception and
on which machines.

### Task 4.2

`docs/ccy.md` "Sessions Survive a Reboot": registry, opt-in, the
restore decision table, the replay filter, the prompt behaviour, the reboot helper and
its refusal; command-reference and troubleshooting rows; `docs/ccy-changelog.md` 3.60.0.

## Phase 5: Proof on a real machine

### Task 5.5b

A restored `ccy` session runs with the supervisor ARMED
(`--supervise`), which the original may not have. The owner confirmed on 2026-09-24
that this is wanted, because a restored session runs with nobody watching it.

## Phase 6: a restored session stops at the SSH key passphrase

Background (the phase intro in PLAN.md is kept there): the reboot on 2026-09-25 restored
the session, and it stopped at ccy's key selection and passphrase prompt. That is
acceptable on the desktop, where the owner logs in anyway. It is not acceptable on the
server, where nobody is present. Four independent brainstorms are in
[`brainstorm-ssh-key-restore/`](brainstorm-ssh-key-restore/BRIEF.md).

### Task 6.1

**Owner decision: the restore-only `SSH_ASKPASS`, alone.** The owner
accepted the recommendation; the TPM seal and the lazy unlock are not built.
Choose how a restored session on the server
unlocks its key. Every brainstorm ranks the same answer first: a restore-only
`SSH_ASKPASS` fed from the vault's `github_ssh_passphrase`, the same way
`run.bash --headless` already unlocks it. The trade-off is that anyone who can read
both the key and the vault password file on that disk can use the key. Two options add
to it rather than replace it:

- seal the passphrase to the TPM with `systemd-creds`;
- leave the session's key locked until its first push, and report it as pending.

### Task 6.2

Implement the decision, tests first. CCY 3.72.1, container 2.40.
Applies where `provisioning_profile` is `server`, restore is on, and `github_accounts` is
not empty. `play-claude-yolo.yml` writes the passphrase to a 0600 file, then adds a drop-in
that names it to the restore unit. `ccy-sessions restore` checks the file before it starts
anything, drops it from its environment so tmux never holds it, then hands its path to each
`ccy` session's command. The host probe and the container entrypoint each `ssh-add` through
their own askpass copy, and each copy is removed once its key is added, or by a trap if the
launcher is killed mid-probe. The container gets only the stage's mount; the entrypoint sets
`SSH_ASKPASS` for its own `ssh-add`, so `podman exec` never sees it.
`scripts/test-ccy-restore-askpass.bash` tests this with the real ssh-agent. Choices, the
ssh-add retry-loop finding and the review fixes: journal 26-10-02; the review is
[`subagent-reports/261002-qa-reviewer-t62-opus.md`](subagent-reports/261002-qa-reviewer-t62-opus.md).

## Phase 7: fedora-desktop#69, three restore defects

Report: [`subagent-reports/261006-issue69-fixes-opus.md`](subagent-reports/261006-issue69-fixes-opus.md).

### Task 7.1

`verify-restore` fails on its first session with
`can't find pane: =<session>`. Its `capture-pane -t "=${name}"` needs the pane form
`=${name}:`. A sweep found no other pane command with an `=name` target. The fake tmux in
`test-ccy-sessions-reboot.bash` stripped the `=`, which is why the suite passed; it now
refuses an `=name` pane target without the colon, as real tmux does, and the suite's ten
`verify-restore` cases were red against the old target, green after.

(The open sub-task, a test against a REAL tmux server, stays in PLAN.md.)

### Task 7.2

A key chosen at the SSH key menu goes into the session's record as
`--ssh-key <file>`, so its restore never shows the menu. Only records that skip Quick
Launch (`--token`, `--network`, `--no-network`) take it; Quick Launch holds the key for
the rest. An agent or "no key" is not recorded, and `verify-restore` names that session
`WAITING-AT-PROMPT ssh-key`. CCY 3.82.0. Tests: `test-ccy-session-registry.bash` ("the SSH
key chosen at the prompt goes into the record", "the SSH key menu, as it is printed") and
`test-ccy-sessions-take-over.bash` ("which CCY session this process runs in"). The
launcher's call into the new code is covered only by an awk source-order check; the
launcher cannot run in the container. Sessions started with no flags are restored through
Quick Launch, whose saved settings now survive a ccy version change (Task 7.5).

### Task 7.3

`ccy-sessions reboot` asks logind's `CanReboot` before it warns
anyone, and refuses on any answer but `yes`, pointing to `sudo reboot-with-update`. Every
session that cannot be warned is named in the same refusal, with its options. A failed
`systemctl reboot` is reported as a failure. `reboot-with-update` and
`shutdown-with-update` run as root and share only the session check, through their
`--dry-run` rehearsal. Tests: `test-ccy-sessions-reboot.bash` ("everything that would stop
it is found before anyone is warned", and "systemctl refusing the reboot is a failure").

### Task 7.5

OWNER decision (2026-10-07): option A, keep the saved Quick Launch
settings across ccy versions when their shape has not changed. `load_launch_config`
deleted them on any ccy version change. It now keeps them while the file's format
(`SAVED_CONFIG_VERSION`, the existing `CONFIG_VERSION=1`, raised only when the keys or
their meaning change) matches, and discards them when the format differs or is missing or
a choice key is missing. Every existing file is format 1 with the same keys, so it is
kept. CCY 3.86.3. Also unblocks Plan 00161's U20 prerequisite, which now checks the
format, not the version. Tests: `test-ccy-teams.bash` ("Quick Launch across ccy versions").
[report](subagent-reports/261007-quick-launch-across-versions-opus.md).

## Phase 8: fedora-desktop#88, a restored session is set going, and verify waits for it

The infra agent found three gaps on the restore path after unattended self-update reboots
(issue #88, body and its 2026-10-08 comment). A restored `--continue` session either starts
work on a cold prompt cache before anyone can compact it, or, with nothing queued, sits at an
empty prompt until a person types `continue`; and `fedora-desktop-self-update verify` gave up
after five minutes with five of six sessions still starting, leaving its unit failed.

### Task 8.1

Design, in this plan's journal, the restore-time context check: how
`ccy-sessions restore` reads a restored session's context size once its prompt has drawn,
and the floor at or above which it compacts. Grounded in what the pane or the transcript
actually shows, not assumed. Journal 26-10-08: the prompt is Claude's framed `❯` input
box (measured on 2.1.293), the size is the transcript's last main-thread usage or
compaction boundary, the floor is `ccy_restore_compact_floor_tokens` (default 150000).

### Task 8.2

`ccy-sessions restore` sets each restored session going once its prompt
has drawn, before any work turn: at or above the floor it sends `/compact` (the
supervisor's continue-after-compaction carries the session on); below it, `continue`. A
session whose prompt cannot be read is left untouched and named in the output, never
skipped silently. Built as `ccy-sessions set-going`, run by
`ccy-sessions-set-going.service` after the restore (the restore's `Wants=`), so the wait
never holds `default.target`; the manifest (format 2, written under a lock) records what
each session got. After review: one session per conversation, a `--resume <id>` session
read from that conversation, and `continue` after the compaction for a session with no
supervisor (journal 26-10-08, second entry). CCY 3.88.0. Gated by
`scripts/test-ccy-sessions-reboot.bash` and `scripts/test-ccy-session-registry.bash`.

### Task 8.3

`verify-restore` reports a continuing session whose compaction (or
`continue`) did not start within a short window, instead of counting it OK.
`NOT-SET-GOING <compact|continue>-not-started` when the transcript shows no input within
`CCY_SESSIONS_START_WINDOW` (120 s); `NOT-SET-GOING <reason>` for one left alone.

### Task 8.4

`fedora-desktop-self-update verify` waits until the restore settles
(every recorded session running, or definitely failed), not a fixed five minutes, and a
passing `verify` clears its unit's failed state. The verify unit belongs to Plan 00137's
self-update; the change is made here because the wait is on this plan's restore.
`verify-restore --wait` stops once settled; the ceiling is 1500 s; a failed verify keeps
the check owed, so a later run re-checks; a pass runs
`systemctl reset-failed fedora-desktop-self-update-verify.service` (exit 25 if that
fails). Gated by
`tests/helpers/self_update/test_cycle.py` and `scripts/test-self-update-cycle.bash`.

## Success criteria evidence

- **Missing daemon CLI refuses, no reboot.** `scripts/test-ccy-sessions-reboot.bash`:
  "a missing daemon CLI refuses the reboot", and the same inside a ccy container.
- **`--dry-run` signals nothing and reboots nothing.** Same script: "dry run signals no
  daemon", "dry run invokes no systemctl".
- **`./scripts/qa-all.bash` green** (1142 files, run on F44 after `a8480fec`;
  ccy-sessions-reboot 156 passed).
- **`qa-reviewer` over the full plan diff:** round 1 FIX-BEFORE-MERGE, round 2 PASS;
  journal 26-09-23.
- **No hostname, address, username or private path anywhere in the diff or the PR.**
  The added lines and messages of all 42 commits naming the plan were scanned with the
  pre-commit hook's own `localhost.yml` denylist (no field matched) and for home paths,
  IPv4 addresses and emails (none outside placeholders). The work went straight to F44;
  there was no PR of its own.
