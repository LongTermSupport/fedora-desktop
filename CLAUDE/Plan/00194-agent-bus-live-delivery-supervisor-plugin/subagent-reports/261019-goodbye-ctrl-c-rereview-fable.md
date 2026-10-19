# Fable re-review: `fix/goodbye-ctrl-c-2` at 39743453 (follow-up to the FIX-FIRST on c9691515)

Reviewed: the local branch tip 39743453 = c9691515 + review dd962f70 + merge of F44 at
c277edfd (c242106d) + the fix commit. Compared against local F44 at 97537444.

## The six FIX-FIRST items

### (1) Versions and rebase — NOT DONE: F44 has moved since the branch's merge

`git merge-tree --write-tree F44 39743453` CONFLICTS in four files:

- `CLAUDE/Plan/meta-deploy.list`
- `docs/ccy-changelog.md`
- `files/var/local/claude-yolo/Dockerfile`
- `files/var/local/claude-yolo/claude-yolo`

Cause: the branch merged F44 at c277edfd (merge-base), but F44 is now 97537444 (Plan 00203
Task 3.8 / Plan 00204 Task 2.3), which carries CCY 3.99.12.12 / container 2.77.8 and its own
play-claude-yolo.yml queue comment. The branch sits at 3.99.12.15 / 2.77.11 with a changelog
heading "Follows 3.99.12.13 (container 2.77.9)" — numbers that exist on no branch. The whole
version delta is one image rebuild, so on top of F44's .12/.8 the branch should land as
**3.99.12.13 / container 2.77.9** (one bump, whatever the version-bump hook forced inside the
branch), with the changelog heading and its "Follows" line, the `CCY_VERSION` comment, the
Dockerfile label, `REQUIRED_CONTAINER_VERSION` and the queue comment all saying the same thing.

### (2) The queue — correct at the tip, must survive the re-merge

At 39743453 `meta-deploy.list` has exactly one `playbooks/imports/play-claude-yolo.yml` entry,
preceded by `playbooks/imports/play-agent-bus.yml` with a comment saying it goes first (the host
pingbus the launcher's exit calls). F44's comment above the play-claude-yolo entry was kept and
ours appended, which is right. The conflict in (1) is in this file too: after the re-merge the
check is again "one play-claude-yolo.yml entry, agent-bus before it".

### (3) `seat disconnected` on a dead stderr — DONE, correctly

`cmd_seat_disconnected` (helpers/pingbus/cli.py) now collects every line into a `StringIO`,
posts to every seat first, then `_report_unless_gone` writes the lot once. Only `OSError` with
errno in `TERMINAL_GONE_ERRNOS = {EIO, EBADF, EPIPE}` is swallowed; anything else re-raises
(the ENOSPC test proves it). The exit code is still the first failure's, so the launcher's
"failed" line is right even when the report itself was lost.

Errno judgement:

- **EIO** is the real case: a write to the slave side of a pty whose master has closed (closed
  terminal, killed tmux window) fails with EIO. Correct and necessary.
- **EPIPE**: stderr is a pipe whose reader died (`ccy 2>&1 | tee`, a dead logger). Python keeps
  SIGPIPE ignored, so this surfaces as `BrokenPipeError` (an `OSError`, errno EPIPE) — caught.
  Right to tolerate: the report has nowhere to go and the exit code still carries the result.
- **EBADF**: harmless but practically unreachable. If fd 2 is closed before Python starts,
  `sys.stderr` is `None`, not a file raising EBADF (see the open case below). Keeping it in the
  set swallows nothing real; it is just not the mechanism the comment implies.
- Does not swallow real failures: ENOSPC, EACCES, EINVAL etc. propagate. Good.

One more thing I checked: Python's exit-time flush of a dead stderr does **not** change the exit
status (only a failed stdout flush gives 120), and `seat disconnected` writes nothing to stdout,
so the exit code the launcher reads is the command's own.

### (4) A mashed Ctrl-C — DONE, correctly

`ccy_bus_disconnected` (lib/agent-bus-membership.bash):

```bash
local rc=0
trap '' INT
timeout "$CCY_BUS_DISCONNECT_TIMEOUT_S" "$CCY_PINGBUS_COMMAND" seat disconnected ... || rc=$?
trap - INT
```

**Trap restore on every path.** `timeout ... || rc=$?` can never abort under `set -e`, so
`trap - INT` is reached on success, failure and timeout (rc 124) alike. `trap - INT` resets to
**default**, which is the right restore because the launcher never installs an INT trap of its
own (its traps are `EXIT`, and a `RETURN` inside ssh-handling): I grepped every `trap` in
`claude-yolo` and `lib/`. So nothing is lost by resetting rather than saving/restoring.

**Restart exec inheriting SIG_IGN.** The restart path is `trap - EXIT; cleanup; …; exec claude-yolo …` (claude-yolo ~3829). `cleanup`'s last statement is `ccy_bus_disconnected`,
which restores SIGINT before it returns, so the `exec`'d relaunch starts with SIGINT at default.
test-ccy-teams' "SIGINT is the launcher's again afterwards" check (`trap -p INT` printing
nothing) covers exactly this. No leak.

**The children.** `timeout` (GNU coreutils) installs its own SIGINT/SIGHUP/SIGTERM handlers
unconditionally, so it does not inherit bash's SIG_IGN; its child gets SIG_DFL (a handled
signal resets on exec), and the Python then ignores SIGINT and SIGHUP itself via
`hooks.terminal_signals_ignored()` (handlers saved and put back in a `finally`). SIGTERM is left
alone so `timeout` can still end it. Correct.

**Process group.** `timeout` without `--foreground` calls `setpgid(0,0)`, so the pingbus run is
in a process group of its own. A terminal Ctrl-C hits the foreground group (the launcher's),
not pingbus; an interactive shell's SIGHUP fan-out hits its job table (the launcher's group),
not pingbus. The test's `kill -INT 0` under `setsid --wait` is a faithful model of the mash.

**`timeout 60` vs the 15 s wait.** There is no separate 15 s container wait in the launcher's
exit path (I searched the launcher and lib for one); the only 15 s is pingbus's own per-seat
`disconnect_wait_s = 15.0`, the wait for a seat lock still held. So the two bounds are nested,
not additive: 60 s is the outer cap over (per seat: ≤15 s lock wait + `verify_room` ≤15 s +
send ≤5 s). Worst case the cap bites with a homeserver down and three or more seats, which
is the right failure to have — the launcher says so ("stopped after 60 s") and continues. The
normal exit path costs seconds: the container has already gone, so its keeper's flock is
released by the kernel and the wait is zero. The closed-terminal exit time is therefore
unchanged in the common case and bounded at 60 s in the worst case. Acceptable.

Secondary benefit I want recorded: a SIGHUP that arrives *during* the pingbus run (terminal
closed a second time, mid-cleanup) kills the launcher's bash (no HUP trap, EXIT trap not
re-run), but pingbus is in its own group, ignores SIGHUP, and finishes posting. The notice
still goes out.

### The open case: stderr closed before Python starts (`2>&-`)

With fd 2 closed at startup Python sets `sys.stderr = None`; `main()` then does
`err = sys.stderr`, and the first `err.write` raises `AttributeError`, not `OSError`, so
`_report_unless_gone` does not apply — but the posts have all been made by then (the report is
written last), and the traceback itself cannot be printed. Exit code 1, so the launcher's
`print_error` runs, whose own `echo >&2` fails too.

Is it reachable from the launcher? **No, in practice.** The launcher never closes or redirects
fd 2 (`grep` for `2>&-` / `exec 2>` in claude-yolo and lib: nothing); the only way is a user
running `ccy 2>&-`, and that launcher dies at its first `echo … >&2` under `set -e` long before
the container starts. A closed *terminal* leaves fd 2 open and failing with EIO, which is the
handled case. Judgement: not a defect to fix in this branch; not worth a code path. If anyone
wants belt and braces, `err = sys.stderr if stderr is None else stderr` could fall back to
`open(os.devnull, "w")` when `sys.stderr is None` — a one-liner, not required.

### (5) False goodbyes on lifecycle restarts — reasoning sound, Task 2.0e filed

I verified the claim in the supervisor (`.claude/ccy/claude-supervise.py`, `ExitForRestart`
docstring, ~3677): "The host types `/exit`, and once the child has exited, writes the restart
…". So at SessionEnd time the only artefact is the same `/exit` the hook sees for a real exit;
the `restart-request.json` does not exist yet. The alternative the developer rejected — a marker
written by `ccy_lifecycle.py` at `ExitForRestart` — is correctly rejected: the supervisor may
refuse (no single session id) or abandon the request, and a stale marker would then silence a
real goodbye, which is worse than a redundant one.

Task 2.0e is precise enough to file upstream: it names the component (hooks-daemon supervisor),
the change (record the request, or export a marker path, *before* typing `/exit`; clear it when
a request is abandoned), and the consumer's behaviour once it exists (hook skips the goodbye and
`seat.record_goodbye`s so the launcher's `seat disconnected` stays quiet too). One addition when
filing: name the contract — the marker's path (or env var) and that it must be readable from
inside the container's hook, since the supervisor runs on the host side of the pty. docs/agent-bus.md
states today's behaviour honestly.

### (6) Acceptance steps — amended as asked

PLAN.md's host acceptance now has the five numbered steps: mash Ctrl-C past the container's
exit (one `left the team`, no traceback, no "failed"); `podman stop` (one `disconnected`);
close the tmux window of a session holding two seats (each team told once); no ending posts
both; and a lifecycle-restart baseline for Task 2.0e. These exercise the code paths this
branch changed. Good.

## Other checks

- **Allowlist row**: `files/var/local/claude-yolo/claude-yolo 2813 12 2741` (down from
  2814/2742 on F44). The ratchet moved the right way; `qa-bash-limits.bash` is green.
- **Docs**: agent-bus-protocol.md's `seat disconnected` row says lines are written once every
  seat is done, EIO/EBADF/EPIPE lose the lines, SIGINT and SIGHUP ignored meanwhile — matches
  the code. docs/agent-bus.md and the changelog match too (modulo the version number in (1)).
- **Tests are real**: the teams test mashes SIGINT at a setsid group while a 1.5 s fake pingbus
  runs, checks the hang is cut at a 1 s bound and said, checks the default bound is 60 s in the
  source; the Python test sends SIGINT and SIGHUP from inside the wait, checks handlers are
  put back, and the four dead-stderr tests cover both-told, failure-still-exit, every-seat-
  before-first-line, and ENOSPC-raises. Each vouches for the path it names.
- **Journal**: hand-stamped-looking entry "10:20 · action" exists for the fix; consistent with
  the commit. Its last bullet still says "CCY 3.99.12.14 / container 2.77.10 as placeholders";
  the renumber in (1) supersedes it (journal is append-only, so a new line, not an edit).

## Targeted QA run (PINGBUS_HOME, PINGBUS_TEAMS, PINGBUS_MEMBERSHIP unset)

| Run                                                 | Result                              |
| --------------------------------------------------- | ----------------------------------- |
| `python3 -m pytest tests/helpers/pingbus`           | 1565 passed                         |
| `… tests/helpers/pingbus/test_seat_disconnected.py` | 22 passed                           |
| `scripts/test-ccy-teams.bash`                       | passed 214, failed 0 (new checks ✓) |
| `scripts/test-ccy-lifecycle.bash`                   | passed 215, failed 0                |
| `scripts/qa-bash-limits.bash`                       | BASH-LIMITS-OK (181 checked)        |

qa-all not run (sub-agent).

## Verdict

The code fixes for (3), (4) and (6) are correct and tested; (5) is correctly reasoned and
filed. The branch cannot merge as it stands only because F44 moved underneath it.

**FIX-FIRST**:

1. Merge local F44 (97537444) into the branch and resolve the four conflicts
   (`meta-deploy.list`, `docs/ccy-changelog.md`, `Dockerfile`, `claude-yolo`); renumber to
   **CCY 3.99.12.13 / container 2.77.9** everywhere (changelog heading and its "Follows
   3.99.12.12 (container 2.77.8)" line, `CCY_VERSION` comment, Dockerfile label,
   `REQUIRED_CONTAINER_VERSION`, queue comment); keep exactly one
   `playbooks/imports/play-claude-yolo.yml` entry with `play-agent-bus.yml` before it.
2. Re-run `git merge-tree --write-tree F44 <tip>` clean, then the same targeted QA, and hand
   the tip back for a tip check.

Nothing else.
