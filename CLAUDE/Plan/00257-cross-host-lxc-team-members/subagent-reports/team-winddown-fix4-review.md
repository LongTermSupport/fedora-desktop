## QA Review: d4a90c9c3 (Plan 00257 B.13, review-4 fixes to `ccy-teams shutdown`'s v2 host seats)

**Verdict**: FIX-BEFORE-MERGE

The main finding is fixed. A leftover holding plus a reused tmux name can no longer get `/compact` or `/exit` sent to an unrelated session at plan time. The problems are elsewhere. The new "crashed holder" message, and the matching text in the spec, docs, changelog and help, state as fact things the lock probe cannot know. The live-keeper check is made at plan time and not again before typing. One new error path has no test.

### Should fix

1. **"Lock not held" is reported as "crashed or rebooted", but it can also be a session still starting.** `src-go/internal/teamchat/seat/holdings.go:82-85`, `src-go/internal/teams/shutdown/v2.go:323-331`
   - `keeperLive` returns false for three probe states: missing, free and refused (`lockprobe.go:19-27`). `whyV2Ended` reports all three as "whose run ended without releasing it (its seat lock is free: a crash or a reboot)". When a live ccy session has that name, it adds "is a later one under the same name, not the team's".
   - Missing does not mean crashed. `teamchatlaunch/launch.go:223` (`TakeAll`) writes the complete holding on the host before the container exists. The lock file is only made later by the keeper's `TakeSeat` (`member/keeper/prepare.go:589`).
   - So during every v2 launch (including a first-run image build) there is a window with a complete holding, no lock file, and a live tmux session. That session is genuinely the team's.
   - In that window, shutdown says the run crashed, calls the session "not the team's", exits 0, and the session then joins the team.
   - "Refused" (a lock the container made unreadable) gets the same false "crash" wording and the same exit 0.
   - The store's own `sessionHolds` (`take.go:226-233`) does not take this shortcut: a lock that is not live falls through to the session record check.
   - **Fix:** give `KeeperLive` (or a sibling) the probe state, not a bool. Only "free" may say the run ended. "Missing" with a live same-name session, and "refused", should be reported as not reachable (exit 1) with the true reason. Remove "is a later one … not the team's" unless a free lock proves it.

2. **What the message says the seat sweep will do is incomplete, and is wrong in the reused-name case it describes.** `v2.go:329-330`, `SPEC-team-close.md` TC12, `docs/agent-bus.md:203-205`, `shutdown.go:305-307`
   - The text says the sweep "keeps the seat for that session's restore while its session record remains, and releases it once the record is gone".
   - The sweep actually keeps it only while the record remains **and** `desired.json` still names the seat. Otherwise it releases it as a *leave* (`teamchat/sweep/sweep.go`, `one()`: `!wanted → Left`).
   - A later v2 session under the same name rewrites `DesiredFile(session)` (`teamchatlaunch/launch.go:234`). So in exactly the "reused" case, the crashed holder's seat is released as a leave, not kept for a restore.
   - It also assumes the per-minute sweep units are deployed and running.
   - **Fix:** state the real rule (kept while the record remains and the desired membership still names the seat; otherwise released), in all four places.

3. **Live-keeper check and typing are separate in time (TOCTOU).** `shutdown.go:205-222`, `v2host.go:63-68`, `stop.go:29-34`
   - `KeeperLive` is asked in `resolveV2Host`. Then `confirm` waits, possibly indefinitely at a terminal prompt. Then `stopAll` targets `sessclose.Target{Name: j.name}` by name only.
   - If the holder exits during the prompt (its lock is released and its tmux session ends) and an unrelated ccy session takes the same name, that session gets `/compact` and `/exit`. This is the review-3 defect again, narrowed to the prompt window.
   - Ordering before the prompt is fine. The snapshot is collected before the probe. A same-name session that takes the team's session seat lock in between can only be a keeper of that name for that team (a restore), which is legitimately the team's. That case is acceptable.
   - **Fix:** re-ask `seat.KeeperLive` for each v2 host job just before `Closer.Close` and drop (and report) any that are no longer live. Pinning the tmux session id would be stronger.

4. **The "seat locks unreadable" path has no test.** `v2host.go:64-66`, `v2host.go:96-98`
   - Coverage I ran (`go test -coverprofile`) shows `v2host.go:64.16,67.3 … 0` and `96.17,98.4 … 0`.
   - The fix report lists "a seat lock that cannot be probed: every member left is not reachable once, exit 1" as one of its four cases, but no test drives it.
   - This is the fail-closed branch for a filesystem where flock errors (e.g. `ENOLCK`). Add a test, for example an unreadable seat-store directory on the lock's path that gives an error `memberRefused` does not swallow.

### Nits

5. **One bad pool entry blocks every host holder of the team, which is stricter than the sweep.** `v2host.go:181-201`
   - One bad entry under `pool/` makes all of the team's host seats "not reachable" (exit 1), including live holders in valid checkouts whose holding and lock prove they are the team's.
   - It is not destructive and not silent: the team's LXC seats still resolve, and other teams are unaffected because shutdown handles one team per run. So it is acceptable fail-fast.
   - But the comment says "as the seat sweep … judge it". The sweep (`seatsweep.go:129-141`) judges the entry the same way but reports it and keeps sweeping the other checkouts. A proportionate version would close the verified holders and report the bad entry once.

6. **`KeeperLive` builds a half-initialised `Store`.** `holdings.go:133-140` passes `paths.CheckoutKey{}` and no `sessions`. It is correct today, because `probe` uses neither. But it is a trap if `probe` ever reads `s.checkout`. A comment at `readOnly` saying which fields are zero would help.

### Checked and clean

- **The lock probe does not disturb the live holder:** it takes a momentary `LOCK_SH|LOCK_NB` on a separate open file description and then closes it (`lockprobe.go:111-122`). Keeper acquires already allow for this with 3 tries 50 ms apart; `member/lock/lock.go:5-7` names "the host's probe" explicitly. Shutdown probes one lock at a time.
- **Filesystems where locks don't work:** a flock error other than `EWOULDBLOCK` (e.g. `ENOLCK`) becomes an error, then "not reachable", exit 1. It is never reported as "no session". (That path is untested; see 4.)
- **`sessionHolds` refactor preserves behaviour.** A probe error gives `(false, err)`; held gives `(true, nil)`; anything else falls through to `Restorable`. Identical to the old code.
- **The first finding is fixed:** live holders now require HeldBy plus a held lock (`v2host.go:63-68`). `TestALeftoverHoldingDoesNotMakeAReusedNameTheTeams` asserts nothing was typed into the reused session.
- **Tests use the real lock, not a fake:** `keepSeatLock` takes a real `unix.Flock(LOCK_EX)` on the real lock path, and `TestKeeperLiveIsTheSessionsSeatLockHeld` probes missing, held, free and no-store states through the production `probe`. None of them covers a lock created inside a container and probed from the host (different namespace and uid mapping).
- **Version bumps are required and correct:** `files/var/local/claude-yolo/image-source-paths:51` lists `src-go/internal/teamchat/seat/*`. CCY 3.122.42, `REQUIRED_CONTAINER_VERSION` 2.84.42 and the Dockerfile LABEL 2.84.42 agree. The changelog entry is accurate apart from finding 2's wording.
- **`seatsweep.Desired.Read` is unexported** and has no outside callers (grep).
- **Docs:** SPEC TC2/TC4/TC12, `docs/agent-bus.md`, the help text and PLAN B.13 are consistent with the code, apart from findings 1 and 2.
- **Red logs** for the new tests are present in the worktree's `untracked/scratch/`.
- **No install-specific names:** the added lines use placeholders only (`ccy-container-a`, `ccy-reused`, `/srv/proj`).

### Mechanical gates

- **Tests:** `go test -count=1 -race ./internal/teamchat/seat/... ./internal/teams/seatsweep/... ./internal/teams/shutdown/...` all pass; the full `./internal/teams/...` set passes; shutdown coverage is 88.5%.
- **Lint:** `go vet` is clean and golangci-lint reports 0 issues on the three changed packages.
- **plan-qa --sweep:** 2 block findings, in 00244 and 00238. Neither is in 00257, and neither was introduced by this commit.
- **Not run:** `qa-all.bash` (a full QA run inside a sub-agent is denied by R-SUBAGENT-FULL-QA; the coordinator runs it). `--syntax-check` and helper tests are not triggered: no playbook or helper changed.