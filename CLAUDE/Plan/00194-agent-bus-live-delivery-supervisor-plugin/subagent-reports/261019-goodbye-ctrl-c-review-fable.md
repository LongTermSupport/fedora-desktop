# Review of fix/goodbye-ctrl-c at c9691515 (Fable, adversarial, before merge)

Branch `fix/goodbye-ctrl-c`, one commit on top of an origin/F44 that is behind local F44
(d57dab04). Developer's report: `261019-goodbye-ctrl-c-opus.md`. Review branch
`review/goodbye-ctrl-c` from c9691515, not pushed.

## Verdict in one line

FIX-FIRST. The analysis is right and the design is sound (the goodbye was never posted; the
launcher is the only thing that outlives the container; the claim-in-`seat.lock` trick makes
the notice exactly-once). Four things stop a merge as it stands: the branch carries a CCY
version F44 already uses, so it conflicts with F44 on four files; the host fallback dies of
the very endings it exists for (a closed terminal kills its stderr, a mashed Ctrl-C kills it
with a traceback); a lifecycle restart now posts `left the team` to the coordinator, which is
false; and the queue adds a second `play-claude-yolo.yml`.

## 1. The timeout claim

- Where the bound is set, and that it reaches each kind of session:
  - **ccy container**: the entrypoint passes `--settings /tmp/pingbus-home/settings.json`
    (`entrypoint.sh` line 595), the file `seat exec` writes from `seat.session_settings`,
    which now carries `env.CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000` whether or not the
    session is seated. Correct place: a seat can be added later, and the kit's own
    `settings.json` is not the one the container passes.
  - **host `agent-bus-claude`**: `exec claude --plugin-dir … --settings <kit>/settings.json`
    (line 95); the kit file gains the same `env`. Covered.
  - **`cc --teams`**: merges the kit's `settings.json` with its deny file through `jq '.[0] * .[1]'`
    (cc-teams.bash line 124), then runs `seat exec`, which writes its own file. Covered twice.
  - Not verified here: that Claude Code applies a `--settings` file's `env` before reading
    the SessionEnd bound. The developer measured it on 2.1.303 under `claude -p` (hook
    finished at +4.0 s). The unit tests pin only the JSON. Host acceptance step 1 is the
    proof; keep it.
- The kit (`files/opt/claude-yolo/optional/`) is baked into the image (`Dockerfile` line 405
  `COPY optional/`), so the container bump is needed, not a placeholder courtesy.
- **8 s against the stop grace**: no hook runs on `podman stop` at all (tini forwards SIGTERM
  to the supervisor, which has no handler and dies; the kernel then kills the container), so
  the 10 s default grace is not in play today. If the supervisor ever forwards SIGTERM to
  Claude Code, 8 s plus pingbus's 1 s start inside a 10 s grace is tight; a note for that day,
  not for this branch.
- **8 s against the launcher's wait**: unrelated. The launcher's 15 s is `seat disconnected`
  waiting for the seat lock to go, which it needs because bash runs the EXIT trap on SIGHUP
  *while the container is still stopping* (probed: a `bash -c 'trap … EXIT; sleep 30'` sent
  SIGHUP runs the trap within a second, with the child still alive).

## 2. Signal handling in the hook

- `_terminal_signals_ignored` is a context manager around `_post_all` only, main thread only,
  and puts the previous handlers back in `finally`. The hook process spawns nothing while
  inside it (the watcher is spawned by `session-start`, not here), so SIG_IGN is inherited by
  no long-lived process. Correct.
- The 4 s cap holds: `_post_all` still joins its daemon threads against `GOODBYE_TIMEOUT_S`;
  ignoring signals does not lengthen that. SIGTERM is left alone so Claude Code's bound still
  ends the hook.
- The test `test_a_sigint_or_sighup_while_the_goodbye_is_posted_does_not_stop_it` sends both
  signals to the test process from inside `post`, asserts SIG_IGN during, delivery of neither,
  and the handlers restored. Good test.
- Honest caveat the developer also states: with `podman run -it` the host terminal is raw
  while attached, so Ctrl-C never becomes a SIGINT to anything on the host; inside, the
  supervisor's gate and Claude Code's own shutdown absorb the presses. The hook's SIG_IGN is
  belt-and-braces. Fine.

## 3. The fallback (`pingbus seat disconnected`)

### 3a. False positives

- **A live second session on the seat** (take-over, a relaunch, a second launcher): its
  keeper holds `seat.lock`, so `probe_lock_at` stays non-None for the whole 15 s and the
  command writes one stderr line and posts nothing. No false notice. Cost: the first
  launcher's exit takes 15 s. Acceptable.
- **A claim from before this launch** (`claimed < since`): skipped and the lock text left
  alone. `since` is taken on the host (`date +%s%3N`) in `ccy_membership_args`, before
  `podman run`; the keeper's claim is `rt.clock_ms()` in the container, which shares the host
  clock. Ordering holds.
- **An orderly release** empties the lock (`Lock.release` truncates before LOCK_UN), so
  `parse_claim` is None and nothing is posted. Verified in `inbox.py`.
- **Two launchers both reaching the same dead claim**: `acquire_lock_at` captures `previous`
  under the lock and the caller compares it to what it read before; the second arrival sees
  `""` and returns. Exactly-once holds.
- **The report is by tmux session name and "never authority"**: a stale
  `sessions/<name>/applied.json` from an earlier launch is harmless because the claim test
  decides, but two ccy launches in one tmux session would overwrite each other's report and
  give the first exit a 15 s wait and a misleading "still held" line. Pre-existing design,
  noted, not a blocker.

### 3b. The lifecycle restart — judgement: restarts must post nothing (follow-up, tracked)

The supervisor types `/exit`, and only *after* Claude Code has exited writes
`restart-request.json` (claude-supervise.py, `ExitForRestart`). So at SessionEnd nothing on
disk says "this is a restart"; the hook posts `left the team: session ended (exit)`, and
seconds later the relaunched session re-claims the seat. Before this branch no goodbye was
ever posted, so lifecycle restarts were silent. Now every scheduled restart tells the
coordinator the member has left, which is false and is exactly the kind of PEER line a
coordinator acts on (reassigning work). A "restarting" notice would be truthful but is one
more line per restart that nobody acts on. **Restarts should post nothing.** It needs the
supervisor to leave a marker *before* typing `/exit` (and the launcher's restart path to
skip `ccy_bus_disconnected`, or the hook to record the goodbye as said), so it is a
follow-up — but it must be a task in PLAN.md, not a bullet in a report, and the docs must say
the current behaviour until it lands (`docs/agent-bus.md` "Which endings say goodbye" does
not mention it).

### 3c. Race and atomicity

- `seat.record_goodbye` uses `inbox.write_json_file`: temp file, fsync, rename, directory
  fsync. Atomic.
- The launcher reads only after `podman run` has returned (Ctrl-C, `/exit`, stop) or after
  waiting for the lock to go (SIGHUP). With the container gone the hook is gone; the record is
  either there or not. The one gap, hook killed between post and record, is stated.
- `_claim` reads the claim while the keeper holds the lock, without taking it
  (`read_claim_at` opens O_RDONLY). Correct, the lock is the keeper's.

### 3d. Identity, keys, tokens

- Posts through the shared `_post_to_humans_and_coordinators` → `_send_text`, from the seat's
  own member bundle under the checkout (`config.read_member(ref.team, directory)`), as
  `seat farewell` already does on the host. `_send_text` refuses a text containing the token
  and logs only a digest. Nothing prints a token.
- Under a rootful Docker engine (`container_engine=docker`) the container's `said-goodbye.json`
  and `seat.lock` are root-owned 0600 on the host; `read_claim_at`/`goodbye_said` would raise
  `PermissionError`, which is not in `FAILURES`, so a traceback rather than one line. Podman
  is the default and the seat files already have this property; a note, not a blocker.

### 3e. Two real defects in the fallback (FIX-FIRST)

1. **A closed terminal kills the command's stderr, and the command with it.** The stated
   reason for the fallback is "a closed terminal or tmux window". When the pane goes, the pty
   master is closed and every write to stderr raises `OSError` (EIO). `_tell_disconnected`'s
   success line `✓ seat …` is written *after* the post but *outside* the per-seat
   `except FAILURES`, and `OSError` is not in `FAILURES`, so for a session holding two seats
   the first seat's `✓` write raises, the loop ends, and the **second seat is never told**.
   The "still held" line and the launcher's `print_error` fail the same way. Probed:
   `python3 -c 'import sys; sys.stderr.write("line\n")' 2>&-` exits 1. Fix: in
   `cmd_seat_disconnected`/`_tell_disconnected`, write the diagnostics through one helper
   that tolerates `OSError` on the stream (annotated: a dead terminal is the one case where
   a diagnostic has nowhere to go), post to every seat before any line, and add a test whose
   `err` raises `OSError` on write and asserts both seats were told. Alternatively the
   launcher sends the command's stderr to its debug log when there is no tty; either is
   acceptable, the test is not optional.
2. **A mashed Ctrl-C kills the fallback with a traceback.** While `podman run -it` is attached
   the host terminal is raw and Ctrl-C is a byte; once podman exits the terminal is cooked
   again and each further Ctrl-C is a SIGINT to the launcher's foreground process group, which
   now contains `pingbus seat disconnected` (1–2 s on the Ctrl-C path, up to 15 s on others).
   Python's default handler raises `KeyboardInterrupt`, `main` does not catch it, so the
   owner's exact habit prints a traceback and `pingbus seat disconnected failed`. The hook
   ignores SIGINT; the host fallback must too. Fix: `ccy_bus_disconnected` sets `trap '' INT`
   for the call (the lib is within the bash limits), or `main` maps `KeyboardInterrupt` to
   one stderr line and exit 130; the former also protects a goodbye already in flight from
   nothing (the container is gone by then), so it is the simpler choice.

## 4. Fixed template, logging, fail fast

- `DISCONNECT_TEXT` is a constant with one `{handle}` slot; no model text, no message text in
  any log (the `✓` line names the seat only). The homeserver-down test asserts a non-zero exit
  and a line naming the seat and the homeserver. Per-seat failures are one line each and the
  exit is the first failure's. Good.
- `--since x` → 64 through the parser's own exit (tested); `--root rel` → 64; a bad report → 78
  naming the path. Good.

## 5. Launcher change

- **printf fold**: `echo ""; echo "Compose services were started for this CCY session."; echo ""`
  → `printf '\nCompose services were started for this CCY session.\n\n'`: same bytes, same
  stream (stdout). Identical.
- **Allowlist**: the row shrank 2819→2818 lines, 2743→2742 statements against the branch's
  base. But local F44's row is already `2814 12 2742` (the compose fix shrank it). After
  rebasing, the row must be re-measured (`scripts/qa-bash-limits.bash` prints it); it should
  land at 2813/2741. `qa-bash-limits.bash` passes on the branch as is.
- **Versions**: local F44 is already **CCY 3.99.12.11 / container 2.77.7** (the compose
  start fix). This branch also claims 3.99.12.11, so `git merge-tree --write-tree F44 c9691515`
  conflicts on `claude-yolo` (the version line), `docs/ccy-changelog.md` (two 3.99.12.11
  sections), `scripts/bash-limits.allowlist` and `CLAUDE/Plan/meta-deploy.list`. With
  `fix/lxc-checkout-typed` taking 3.99.12.12 / 2.77.8 and `feat/pingbus-compact-before-wake`
  3.99.12.13 / 2.77.9, this branch needs **3.99.12.14 / container 2.77.10** if it merges after
  both, or whatever pair is next free at merge time; the container bump is real (the kit is
  baked in). The changelog section must say "Follows 3.99.12.13 (container 2.77.9)".
- **Queue**: local F44 already has one `play-claude-yolo.yml` entry (compose fix, Plan 00204,
  Plan 00201). The branch's file, written against the older base, drops that entry's comment
  and adds a second `play-claude-yolo.yml`. Required shape after rebase: the existing entries,
  `playbooks/imports/play-agent-bus.yml` before the launcher play with its comment, and **one**
  `play-claude-yolo.yml` whose comment gains this branch's lines (new version, image rebuilt).

## 6. Overlap with the other open branches

- `origin/fix/teams-member-roles`: `git merge-tree` auto-merges `cli.py`, `hooks.py`,
  `membership.py`, `test_hooks.py` cleanly; conflicts only on the launcher version line, the
  Dockerfile label, the changelog and the queue. Semantically independent: it adds role
  notices (`tell_roles`, `_role_lines`, `told-roles` files, `_tell` gains a parameter) in the
  SessionStart/prompt path; this branch edits only `session_end`/`_post_all` and adds
  `is_session_name`. No shared function changed by both.
- `feat/pingbus-compact-before-wake` (6d3d61ab): `cli.py` auto-merges (it touches
  `_send_delivery` and imports `coldwake`); conflicts on the launcher version line, the
  changelog, the queue and an add/add on the same JOURNAL day-file (concatenate). Its launcher
  diff against F44 shows the compose block only because it, too, predates the compose fix; not
  a clash with this branch.
- All three plus this one edit the top of `docs/ccy-changelog.md` and `meta-deploy.list`:
  textual conflicts each time, resolved by ordering the version sections.

## 7. Host acceptance steps

Precise and testable, with two additions:

- Step 1 must say **mash** Ctrl-C for several seconds (the owner's habit), not one press, and
  then check that the launcher's exit printed no traceback and no `pingbus seat disconnected failed`.
- Step 3 (closed tmux window) must be run with a session holding **two seats** (two teams), and
  both must receive `disconnected`: that is the case defect 3e.1 breaks.
- Add: a lifecycle restart (`CCY_LIFECYCLE_MAX_AGE_SECONDS` small) and record what the team
  sees, so the follow-up in 3b has a baseline.

## Targeted QA run here (never qa-all)

- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK.
- `shellcheck -x` on `lib/agent-bus-membership.bash`: clean; on `claude-yolo`: only the
  pre-existing info-level SC1091/SC2086 lines, none on changed lines.
- With `PINGBUS_*` unset: `test-ccy-teams.bash` 205/0, `test-ccy-agent-bus.bash` 78/0,
  `test-ccy-teams-command.bash` 371/0, `test-ccy-restart-request.bash` 178/0,
  `test-ccy-lifecycle.bash` 215/0, `test-cc-teams.bash` 80/0.
- The 30 `tests/helpers/pingbus/test_*.py` modules in one `python3 -m unittest` run:
  1560 tests, OK (268 s), no flake this time.

## Secrets and names

The added lines carry no hostnames, usernames, paths or container names; the journal and
report use `ccy-proj-1`, `yolo-1`, placeholders only.

## Required before merge (exact list)

1. Rebase on local F44 (d57dab04); take the next free version pair (as the queue stands,
   CCY 3.99.12.14 / container 2.77.10), fix the changelog "Follows" line, re-measure the
   allowlist row.
2. `meta-deploy.list`: keep F44's existing `play-claude-yolo.yml` entry, add
   `play-agent-bus.yml` before it, fold this branch's text into the one launcher entry.
3. `seat disconnected` survives a dead stderr: every seat is told even when a write to `err`
   raises `OSError`; test with an `err` that raises.
4. The host fallback is not killed by a Ctrl-C after the container has gone: `trap '' INT`
   around the call in `ccy_bus_disconnected` (or `KeyboardInterrupt` → one line, exit 130 in
   `main`); a test in `test-ccy-teams.bash` that sends INT to the launcher's cleanup while the
   fake pingbus sleeps.
5. PLAN.md gets a task for "a lifecycle restart posts nothing" (3b), and `docs/agent-bus.md`
   states that today a lifecycle restart says goodbye and re-joins.
6. Acceptance steps amended as in §7.

Verdict: FIX-FIRST
