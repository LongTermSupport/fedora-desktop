The report file was not written, so it does not exist at `/workspace/CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/subagent-reports/261126-qa-reviewer-batch22.md`. I have no Write or Edit tool here, and my rules forbid writing files through Bash. The full report is below for the caller to save.

## QA Review — c5f273419 (ci/v2-batch13) against F44 e67e28d72

**Verdict**: FIX BEFORE MERGE

The commit's tree is the same as v2-smoke 9482716c9's (`03b366c5a…`) and its parent is e67e28d72, so it is built as described.

### Should fix

1. **The test the coordinator decided on for (3) was never written, but the REGISTER calls (3) fixed.**
   - The REGISTER decision reads: "(3) is fixed by (a) … with a red-then-green test of remove, a new team of the same name and a plain launch."
   - The "(3) fixed" entry then lists other tests: "the choice, the taking off, the order and refusals in remove, and the wiring". No test runs remove, then a new team under the same name, then the seat lookup.
   - `grep` finds `takeStoppedOff`, `TakeStoppedOff` and `stoppedOnBus` only in the unit tests, and `ForSeats` only in `seatresume_test.go:61`.
   - `TestTakingStoppedSessionsOffATeamKeepsTheirOtherTeams` (`src-go/internal/teams/forgetteam_test.go:147-182`) says "so neither a restore nor a seat lookup finds the old team". It only checks that the record has changed and that `applied.json` is gone. It never runs `conversation.ForSeats` or `v1Sessions`.
   - The fix probably works: `v1Sessions` (`conversation/seats.go:175`) reads `membership.ReadReport`, which is the same `applied.json` that `removeReport` deletes. But that conclusion comes from reading the code, not from a test.
   - Fix: add the scenario test the decision promised (stopped session plus report, remove, new team of the same name, `ForSeats` finds nothing), or correct the REGISTER to match what was actually tested.

2. **The seat-record lock waits without limit and says nothing while it waits.** `withSeatRecordLock` (`src-go/internal/conversation/seatrecord.go`) calls a blocking `syscall.Flock(..., LOCK_EX)` with no timeout and no message.
   - The lock file sits inside the checkout, which this same commit says a container can write.
   - So any process holding `.claude/ccy/seat-conversations/.lock` makes a host launch, or `ccy-teams remove`, hang with no output. Remove would already be past the take-down at that point, since `ForgetTeam` runs inside `confirmDown`.
   - The repo's own pattern for this is `qa/internal/qalock`: a wait that names the holder. Fix: use a bounded `LOCK_NB` retry that fails naming the lock.

### Nits

3. **`O_NOFOLLOW` is used inconsistently on the lock file.** `forgetIn` opens the lock without it (`forget.go`, the `root.OpenFile(... os.O_RDWR|os.O_CREATE, 0o600)` call). `recordSeatsIn` opens it with it. `os.Root` already keeps any link inside the checkout, so this is harmless, but the two opens should match.

4. **The stopped-session list can be out of date when it is used.** The names come from `stoppedIn` at `takedown.go:91`, before the backup prompt, the fate prompt and the play run. They are used in `confirmDown` (`takedown.go:331`) without checking them again.
   - A session restored in between would have its record rewritten and its `applied.json` deleted while it runs. This is the same harm that review finding 3 closed for running sessions.
   - Fix: check that each named session is still not running inside `takeOneOff`.

### Checked and clean

- **os.Root confinement:**
  - `RecordSeats` and `writeInRoot` create each temporary with `O_EXCL|O_NOFOLLOW` under a `rand.Text()` name, rename it into place, and remove it on failure.
  - `removeReport` and `forgetIn` resolve every path inside the root.
  - Link tests exist for both seat-record links and for the report directory.
- **ForgetTeam:**
  - It checks for a real directory, then takes the lock, then checks again, then calls `RemoveAll`.
  - When the seat-record directory is missing, it creates no lock file.
  - A lock test exists for both writers, and it checks the lock is held from another open file description.
- **Takedown order:**
  - `heading`/`sessionsOff` runs before `stoppedIn`, so running sessions are taken off first.
  - If `stoppedIn` fails, nothing is taken down (tested).
  - `TakeStoppedOff` failing still runs `ForgetTeam` and exits 1 (tested).
  - When `SessionsIn` cannot read a running session's membership, it now returns an error instead of skipping it (`hot/list.go:380`, tested).
- **Semgrep widening:**
  - The fixture has 6 new `ruleid` cases and 1 new `ok` case.
  - Sweep claim: 12 sites in 9 files. Counted in the diff: 1+1+1+1+2+1+3+1+1 = 12 sites in 9 files, which matches.
  - The UniFi temporary takes the original file's mode with `chmod --reference`, and its script runs under `set -e`. The two suites that have no `set -e` stop explicitly when `mktemp` fails.
  - `CLAUDE/QA.md` "What it does not see" was updated to match.
- **Versions:**
  - `CCY_VERSION` goes from 3.124.72 to 3.124.77, and the comment lists every change from .73 to .77.
  - `git diff --name-only e67e28d72 c5f273419 -- <75 image-source-paths pathspecs>` returns nothing.
  - `go list -deps` of `ccy-start-claude`, `pingbus-status-line` and `teamchat` (261 packages) includes none of `internal/conversation`, `internal/teams` or `internal/registry`.
  - The Dockerfile LABEL and `REQUIRED_CONTAINER_VERSION` are both 2.84.89. Not bumping the container is correct.
- **Changelog and REGISTER:** the .73 to .77 entries match the code. Usage text and `docs/agent-bus.md` were updated. `meta-deploy.list` already queues `play-claude-yolo.yml` and `changed-plays`.
- **Public-repo leaks:** none found in a scan of the added lines for home paths, emails, private IPs and long hex strings. The only IP, `1.1.1.1` in the UniFi play, was already there before this commit.

### Mechanical gates

- **`go test -race -count=1`** on `internal/conversation`, `internal/teams`, `internal/teams/ops` and `internal/teams/hot`: all ok. `internal/teams/ops` with `-tags ccyteamstest`: ok.
- **`qa-temp-exclusive.bash`:** passed 3434, failed 0.
- **`qa-bash-limits.bash`:** BASH-LIMITS-OK.
- **`qa-ansible-syntax.bash play-unifi-controller.yml`:** did not run (exit 2). This container has no vault password file. It needs to run on CI or the host.
- **`plan-qa --sweep`:** 3 blocking findings, all in plans this diff does not touch (00244, 00262, 00238). Nothing in 00265.
- **`qa-all`:** not run, as instructed.

One slip on my side: I tried to redirect a file list to `/tmp`. The project-containment hook blocked it, so nothing was written.