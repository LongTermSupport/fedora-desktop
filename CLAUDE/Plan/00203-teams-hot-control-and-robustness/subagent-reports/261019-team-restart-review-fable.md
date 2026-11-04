# Review of feat/teams-restart-2 at d1bed1b2 against F44 de85f5f6 (Fable)

Adversarial review of Plan 00203 Tasks 3.10-3.12 (SPEC-team-add-and-restart B, C; Decisions
8-12). Branch read in a detached worktree; nothing changed. Builder's reports: the auto report
of the second part and `261019-team-restart-opus.md`.

## Verdict: MERGE

Every check the owner named holds, with the evidence below. Three findings are worth knowing
about; none blocks the merge, and one (finding 1) is a host-acceptance item in the
daemon-owned supervisor, not in this branch.

## What was run

All with `PINGBUS_*` unset, in the worktree at d1bed1b2.

- `go test -race ./internal/teams/... ./internal/sessions/... ./internal/registry/... ./internal/conversation/...`: pass.
- `golangci-lint run ./...` in `src-go`: 0 issues.
- `scripts/test-ccy-session-registry.bash` 199/0, `test-ccy-restart-request.bash` 180/0,
  `test-ccy-network-disconnect.bash` 137/0, `test-ccy-lifecycle.bash` 223/0,
  `test-ccy-teams-command.bash` 383/0, `test-ccy-restore-askpass.bash` exit 0.
- `scripts/qa-bash-limits.bash`: `BASH-LIMITS-OK`.
- Not run: `qa-all.bash` (as asked), the Python helper suites (the builder's run is on record).

## The owner's checks, one by one

01. **A ccy session restarted in place resumes its own conversation.** The chain is: `restart`
    writes `relaunch.json` with `"why":"restart"` into the session's membership directory
    (`membership.WriteRestart`); the lifecycle plugin's `on_idle` reads it and returns
    `ExitForRestart` (`ccy_lifecycle.py` `_relaunch_result`); the daemon's supervisor types
    `/exit`, writes `restart-request.json` with the one session id it knows and exits 75; the
    launcher relaunches with `--resume <id>` (`lib/restart-request.bash`). The id is the
    supervisor's own, scanned from `CLAUDE_CODE_SESSION_ID` in its PID namespace, and the
    supervisor refuses when it knows more than one id, so it never resumes a *different*
    conversation and never starts a fresh one. The plugin is loaded for every bus session
    (`entrypoint.sh` line 752: `PINGBUS_MEMBERSHIP != off` extends the wrapper), so F2 holds
    for every member that can be a job. `judge` counts the job done only when the container
    ID from `podman ps --format {{.ID}}` differs from the one noted before the request *and*
    every seat the session names is held again, so a still-running old container or a
    half-started new one is not mistaken for success. See finding 1 for the `/clear` edge.
02. **The 300 s withdrawal removes only a restart's request.** `membership.WithdrawRestart`
    reads the file and removes it only when it decodes with `why == "restart"`; a first join's
    request (no `why`) is left and the result line says so. Pinned by
    `TestARestartRequestSaysWhyAndOnlyItIsWithdrawn`,
    `TestWithdrawingARestartLeavesAFirstJoinsRequest` and
    `TestAFirstJoinsRelaunchIsNeitherOverwrittenNorWithdrawn`. See finding 2 for the one
    gap (ownership is judged by content, not by who wrote it).
03. **A stopped cc session is resumed with the right id; a missing record fails without
    starting anything.** `prepareStart` calls `conversation.For(rec, ClaudeHome)`: a cc
    record's checkout is found by walking up to `.git` (`TestForFindsACCSessionsCheckoutAboveItsFolder`),
    the record must name this session and hold a UUID, and that id's own transcript must exist
    under `transcript.Dir(prefix, rec.Dir, claudeHome)`, never the newest file
    (`TestForNeedsTheRecordedIDsTranscriptNotTheNewest`). `registry.ResumeArgs` strips every
    selector and appends one `--resume <id>`; cc gets only that. No record: `✗`, named with
    `ccy --resume` by hand, `m.started` empty (`TestAStoppedSessionWithNoRecordedConversationIsNotStarted`).
    `ccy-sessions restore` takes the same path (`TestASessionWithNoRecordedConversationIsNotRestored`),
    and `test-ccy-restore-askpass.bash` now drives the built binary with recorded
    conversations for both prefixes.
04. **LXC and unrecorded seats are named and skipped.** `planFor`: `Kind("lxc")` rows go to
    `leftOut` with the LXC wording; a seat row with no `Record` that is held or a member is
    named "nothing recorded to restart"; a roster member not on this desktop's checkouts gets
    the "restart it where it runs" wording; parked seats, team and human rows are skipped.
    None of these counts as a failure (`report` exits 1 only on `resultFailed`/`resultPending`).
    Pinned by `TestARunningCcySessionIsRestartedInPlaceAndTheLxcMemberLeftOut` and
    `TestASeatNoRecordNamesIsLeftOut`.
05. **Partial failure exits 1 and keeps going.** `actAll` and `waitAll` iterate every job;
    each job fails alone; `report` returns `ExitFailure` if any did.
    `TestAStoppedSessionWithNoRecordedConversationIsNotStarted` has one `✗` and one `✓` in the
    same run with exit 1 and the count "1 restarted, 1 failed".
06. **No play, no play lock.** `teams.Wire` builds `restart.New(h, kit, b)` with no
    `o.Locked`; the package imports nothing from `ops` or `playlock`;
    `TestNothingGoesOnTheBus` enumerates every command the fake saw (tmux, podman, systemctl,
    git, env, `agent-bus seat list`, the sudo bus reads) and fails on anything else.
07. **The deleted bash restore has no live caller.** Repo-wide grep (excluding `.git`,
    `untracked/`, `vendor/`, `node_modules/`) for `ccy_registry_restore`,
    `ccy_registry_restore_args` and `_ccy_restore_manifest_merge` finds only plan journals,
    research notes and the 00191 assessment and specs, all historical prose; nothing under
    `files/`, `playbooks/`, `tasks/`, `scripts/` or `src-go/`. The one test line that called
    `ccy_registry_restore_args` (`test-ccy-network-disconnect.bash`) is removed.
08. **The allowlist only shrank.** One row changed: `lib/session-registry.bash`
    `1044 39 1077 assoc,data` → `865 36 871 data`. No row added.
09. **Version bump and changelog are coherent.** `CCY_VERSION` and `REQUIRED_CONTAINER_VERSION`
    in `claude-yolo`, the Dockerfile label, and the changelog heading all say 3.99.12.17 /
    2.77.13 (placeholders, renumbered at merge); the changelog section says "Follows 3.99.12.14
    (container 2.77.10)", which is F44's tip, and lists the four changes that are in the diff.
    Plugin versions moved too: pingbus 1.4.0 (hooks.json changed), ccy-lifecycle 1.2.0.
10. **meta-deploy.list.** Exactly one `playbooks/imports/play-agent-bus.yml` entry then one
    `playbooks/imports/play-claude-yolo.yml` entry. The text is accurate: `agent-bus-install software` builds the host `pingbus` bundle and installs the member kit (`hooks.json`
    included) for `cc --teams`/`--bus`, and `play-claude-yolo.yml` stages the same kit into the
    image's build context and installs the launcher, its `lib/`, the lifecycle plugin and the
    Dockerfile change. "Running sessions are not touched" is right: the rebuild is on next launch.
11. **Nothing install-specific.** Grep of the added lines for home paths, user names,
    organisation names, e-mail domains and IPv4 literals finds only a test's `/home/u`.
12. **D11.** The Claude Code build in this container (2.1.303) carries both
    `CLAUDE_CODE_DISABLE_AGENT_VIEW` (6 occurrences) and `disableAgentView` (4), confirming the
    builder's name check; the Dockerfile sets the variable and no background-task switch
    (`test-ccy-restart-request.bash` pins both).

## Findings (numbered; none blocks)

1. **Host acceptance: a running ccy session that has been `/clear`ed may never restart in
   place.** The daemon's supervisor (`.claude/ccy/claude-supervise.py`, not this repo) learns
   its session id by scanning process environs for `CLAUDE_CODE_SESSION_ID` and keeps a
   union-only cache; it refuses `ExitForRestart` when the cache holds more than one id. Claude
   Code exports that variable to its children (it is set in this review's own shell), so if
   `/clear` gives later children the new id, the cache holds two and every idle tick logs
   "the own session id is not unambiguous". The team restart would then wait 300 s, withdraw
   and print "it may be busy, or its supervisor does not take plugins", which does not name
   the real cause. It fails safe (nothing resumes the wrong conversation), and it is the same
   limit the first-join relaunch (H22) already has. Suggest: at host acceptance, `/clear` one
   member, run `ccy-teams restart`, and read the supervisor's log; if it refuses, add a third
   clause to the timeout wording ("...or it was /cleared: the supervisor knows two ids") in a
   follow-up, and raise the ambiguity rule with the daemon.
2. **`job.wrote` is written and never read.** `fail()` withdraws by file content
   (`WithdrawRestart`), not by whether this run wrote the request. If two `ccy-teams restart`
   runs for two teams that share a session overlap, the second sees a pending request, writes
   nothing (`joinPending` is set, misnamed for this case), and on its timeout withdraws the
   first run's request. Rare and harmless (the first run also times out at the same moment),
   but either use `wrote` to gate the withdrawal or delete the field.
3. **One transient `podman ps` failure fails every in-place job at once.** `judge` fails an
   `actInPlace` job on `podmanErr != nil` on any poll, and `heldSeats` fails a job on any
   `agent-bus seat list` error, both without a retry, and the failure withdraws the request.
   Over a 300 s wait polled every second that is 300 chances for one hiccup to end the run.
   The spec is silent; a tolerance of a few consecutive errors before failing would fit B7.

Low notes, no action needed for the merge: `board.Kind("lxc")` is now spelled in both
`shutdown` and `restart` (the board should own the constant); `restore.go`'s live-session
branch swallows `conversation.For`'s error for the manifest's `Resume` field (best-effort,
falls back to the recorded `--resume`, but it is the only place the error is dropped);
`restart` starts a record marked no-restore (defensible: the owner asked by name, but
`ccy-sessions restore` would skip it).
