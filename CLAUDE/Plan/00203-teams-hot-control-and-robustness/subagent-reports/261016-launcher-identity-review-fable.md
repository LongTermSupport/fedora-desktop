# Pre-merge review: fix/m2-acceptance-host (a1c5cdad) — launcher session identity

Reviewer: Fable (adversarial, read-only). Diff reviewed: `origin/F44...origin/fix/m2-acceptance-host`.
Tests run in the branch worktree: `scripts/test-ccy-sessions-take-over.bash` (78/78),
`scripts/test-ccy-tmux-targets.bash` against real tmux 3.3a (24/24).

## Verdict: FIX-FIRST

One blocking finding (B1). The launcher-side fix is correct and well tested; the same defect
survives in a second identity route the branch did not touch.

## Blocking

### B1. `ccy-teams record-seats` still names the OUTER session from `TMUX` alone

`src-go/internal/tmuxccy/current.go` `CurrentSession` is the Go twin of the old
`ccy_tmux_current_session`: socket basename check on `TMUX`, then `display-message -p '#S'`
with no pane target and no `pane_pid` comparison. Its one caller is
`src-go/internal/teams/find/record.go` `RecordSeats`, invoked by the launcher at
`files/var/local/claude-yolo/claude-yolo:1276` when a person accepts Quick Launch's saved
seats (`ccy-teams record-seats "$CCY_SEATS"`). It writes the seats with `store.SetTeams(session, ...)`
into the record of whatever session `TMUX` points at.

So a `ccy` run from a shell inside another ccy/cc session's pane (the owner opening a shell
in a cc pane and saying yes to Quick Launch) still crosses identities: the nested launch's
seats land in the outer session's registry record, and the outer session's next restore or
age restart replays `--teams <nested seats>` onto the wrong session. That is exactly the
class the branch's commit message and changelog say is closed ("takes no session identity").

Fix options, either is small: have the launcher pass the name it already computed
(`ccy-teams record-seats --session "$ccy_session_name"`, skipping the call when it is empty),
or give `CurrentSession` the same `-t $TMUX_PANE` + `pane_pid == pid|ppid` rule (the Go
side would need the launcher's pid and ppid, so the first option is the honest one: one
decision, made once, in the launcher). Add a fake-tmux case in the Go tests either way.

## Non-blocking

### N1. Merge mechanics: local F44 (bb424d20) conflicts with this branch

`origin/F44` is `bbbd0604`; the local `F44` has `bb424d20`, which emptied
`CLAUDE/Plan/meta-deploy.list` and appended a JOURNAL entry. The branch rewrites the list
(keeps `play-claude-yolo.yml`, the 00203 acceptance, the 00201 and 00161 acceptances; drops
`play-agent-bus.yml`) and appends its own JOURNAL entry. Both files will conflict; resolve by
keeping the branch's queue and both journal entries in time order. The branch's removal of
`play-agent-bus.yml` from the queue is consistent with bb424d20's "deployed".

### N2. Plan 00201 P1 is weaker than it needs to be

`m7_launch` now unsets `TMUX`/`TMUX_PANE`, so the plain session is always unnamed and the
only correct form is `off`. `m7_check_plain` nevertheless accepts `kit` (a mounted seatless
membership). An unnamed launch that wrongly mounted a membership — the very symptom this
branch fixes — would pass P1 as `kit`. Assert `off` here; keep `expect-plain`'s `kit`
branch only if some caller actually launches a named plain session.

### N3. No acceptance exercises a NAMED seated session any more

00201 and 00161 launch unnamed (by design, now explicit). 00203 M2 launches named sessions
but only plain and `--no-bus`; M1 adds a named plain session. The owner's normal path, a
`ccy --teams` in a pane of ccy's tmux (name, label, membership directory with seats, record
line), has no automated proof after this change. Not introduced by this branch, but the
branch removed the accidental coverage the nested launches gave it.

## Checklist answers

1. **Detection.** `ccy_tmux_current_session` names the session only when `TMUX_PANE` is set,
   `TMUX`'s socket is ccy's, and tmux reports the pane's `pane_pid` equal to `$$` or `$PPID`.
   Inputs: `TMUX`/`TMUX_PANE` from the launcher's host environment, `$$`/`$PPID` of the host
   launcher process, `pane_pid` from the host tmux server. Spoofing from a container is not
   possible (no reach to the host env or server); a host shell can set `TMUX_PANE` to another
   pane but cannot make that pane's first process its own parent. Bash keeps `$PPID`
   unchanged in `$(...)` subshells (verified), so the `session=$(...)` call sites are sound.
   - Real launches stay named: ccy-sessions/Go `tmuxccy/start.go` and the bash
     `ccy_tmux_start_detached` both run `bash -c <trampoline> ccy-tmux [env ...] <launcher>`;
     the trampoline is a compound command so bash forks, and `env` execs, so the launcher's
     `$PPID` is the trampoline, the pane's first process. M2's `m2_launch` runs
     `exec <launcher>` as the pane's string command: the direct case. Both covered by the
     real-tmux test.
   - Nested launches: a Claude Bash tool's shell is at least two forks below the pane's first
     process (claude, then its shell), so neither `$$` nor `$PPID` is `pane_pid`. Covered.
   - A stale `TMUX_PANE` (pane gone) makes `display-message -t` fail and the launch exits 1:
     fail-fast, and the same class of failure the old code had with a dead server.
2. **Restore and age restarts.** Restore (bash and Go) is the trampoline case above, so the
   same name and membership directory. Restart (age, first join) is `exec "$SCRIPT_DIR/claude-yolo"`
   (`claude-yolo:3841`): same pid, inherited `TMUX`/`TMUX_PANE`, so the same answer.
3. **First-join relaunch replay.** Same `exec` path; `ccy_restart_relaunch_args` is untouched.
   No change in behaviour for named sessions. B1 is the one replay-related hole (wrong record).
4. **Real-tmux test.** Direct, trampoline (using the real `ccy_registry_trampoline` string) and
   nested cases are present and pass here. The fake-tmux test checks the `-t %pane` target and
   the pid comparison in the format string.
5. **CCY 3.99.4.1.** Every parser is textual: pre-commit `awk -F'"'`, `test-ccy-info-flags`
   `grep|cut`, U20 `awk -F'"'`, Quick Launch string equality (`SAVED_CCY_VERSION`). No numeric
   or `sort -V` comparison anywhere. `REQUIRED_CONTAINER_VERSION` stays 2.71;
   `files/var/local/claude-yolo/Dockerfile` and `entrypoint.sh` (the image inputs) and
   `files/opt/claude-yolo/*` are untouched: no image change, the changelog claim is true.
6. **Acceptance changes.**
   - U20/M7 `unset TMUX TMUX_PANE`: a real proof of the unnamed path and makes the runs
     deterministic whatever terminal runs meta-deploy. See N3 for what it stops proving.
   - P1 plain rule: see N2.
   - Permissive SELinux: real. `Z` relabels whenever SELinux is enabled, so comparing the
     mount source's and `membership.json`'s type (`container_file_t`) and MCS level against the
     container's `ProcessLabel` proves H1's relabel in either mode; the read is proved only
     when Enforcing and the output says so; Disabled fails; a label-disabled container must
     carry no `Z`. The jq over `HostConfig.Binds` and `Mounts.Options` catches the option in
     either place podman reports it.
