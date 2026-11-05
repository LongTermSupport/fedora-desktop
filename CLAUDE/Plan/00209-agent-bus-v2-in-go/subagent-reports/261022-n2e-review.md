# N2 slice n2e: adversarial review

Verdict: FIX-FIRST

Scope: `git diff feat/bus-v2-n2..HEAD` on `feat/bus-v2-n2e` (status, teamlock, health,
health/view, notice, seat holdings and the NT3 bound, sweep, element, `.golangci.yml`), against
SPEC-n2e.md, C 3.7.5, 3.10 and 3.11, the rows DC4, DC16, DC21 and INT7, gates A and B, and the Go
rules.

What was run: `go test -race -cover ./internal/teamchat/...` passes. Coverage of the slice's
packages: status 94.6%, teamlock 93.1%, health 90.7%, health/view 92.9%, notice 90.8%, seat 90.3%,
sweep 100%, element 93.6%. `golangci-lint run ./internal/teamchat/...` reports 0 issues, with
and without `--build-tags tuwunel`. The diff adds no `time.Now`, `time.Sleep`, `//nolint` or
`_ =` line. Gates A and B hold: the slice adds no play, unit or install step, and no pool seat.

Trust boundaries checked and found sound: the seat lock record and the session seat directory
(the store's L3 probe: a link, a non-regular file, a swapped inode, a bad record, a refused
directory are all `lockRefused`/no run, never an error; the sweep tests drive the real
`seat.Store`); the run id (always a parsed `paths.RunID`, so `tc-disc-<run>` is always valid);
room events in the notice's scan (a bad event is skipped, the page bound posts, the goodbye
counts only when sender and subject are the subject); room state in the view (a membership
outside the five is skipped; the response cap is the client's 32 MiB); the status record
(strict, 1 MiB, lowercase keys only, no repeated key, every line checked for C0/C1, format
characters and U+2028/U+2029); display names and the launcher's `Name=`; `approved.json`, not
the input, as the Element URL; the consent file (owner, mode, link, exact three keys and the
current text). The findings below are what is left.

## Findings

### 1. medium: a failed view wipes `lxc-online.json`, so a run that ended around then is never announced

`src-go/internal/teamchat/health/health.go:186-193` with `health/online.go:285-315`.

**What is wrong.** When `Viewer.View` fails, `f.view` stays empty, so `probeGuests` probes no
one and `online` builds `next` with no handles. `maps.Equal(next.Handles, was.Handles)` is false,
so the empty file is written over the last tick's entries. At the next tick, an LXC handle that
was held before the failure and is now free or stopped has no entry, so `moveOn` finds
`entry.Run == ""` and asks for no notice.

**Why it matters.** HT6 exists to post that notice (C 3.11.2 step 6, ops 2). The view fails
exactly when the homeserver is not answering: after a reboot (guests and homeserver both coming
up), after this tick's own step-2 restart, or during any 5xx spell. That is also when LXC runs are
ending. Each such run's `disconnected` is lost. Only the member's own next link can recover it,
and only if its store survived. No test covers a view failure while `lxc-online.json` holds an
entry.

**Fix.** When the view fails, leave step 6 alone: do not read, change or write `lxc-online.json`
(and set no `online`, since no LXC member entry is written). Or, more narrowly, carry every entry
of `was` whose handle is not in this tick's view into `next` unchanged. Add a test: held at tick
1, view fails at tick 2 with the guest now free, view back at tick 3. The notice for run A must
be asked at tick 3.

### 2. medium: the step-2 restart does not re-check the stamp, so it can restart and record a removed team

`src-go/internal/teamchat/health/probe.go:124-159`.

**What is wrong.** `restart` takes the team lock and `restartLocked` calls `Units.Restart`, then
writes `status.json`. Neither checks `Teams.Stamp` first. The unit is the template instance
`teamchat-server@<team>.service` (`paths.ServerUnit`), so a restart after `team remove` has
finished starts that team's homeserver again.

**Why it matters.** C 3.11.2 says the tick "re-checks the stamp under the lock before it writes,
so a tick that overlaps a `team remove` never writes a record for a removed team". HT5 does this
re-check for the step-5 write. The step-2 write does not. Here is the window. Steps 1-2 run on
the stamp list read at the start. The re-probes span about 20 s. A remove that takes the lock,
stops the unit and deletes the team between the last `probeAgain` state read and this
`TryLock` leaves the lock free. The tick then restarts the removed team's instance, which may
recreate its state directory through the unit's directives, and tries to write its record.

**Fix.** In `restartLocked`, before `Units.Restart`, call `t.cfg.Teams.Stamp(ctx, team.ID)`. If
the team is not stamped, return without restarting or writing, and let the team's outcome be
dropped with "the team was removed". Add a test with the `teams` fake removing the team during
the third probe.

### 3. low: the n2e-D7 ten-minute bound is keyed on a member-written run id

`src-go/internal/teamchat/seat/release.go` (`announce`, the `thisRun` switch).

**What is wrong.** `until_ms` is reused only when the earlier `notice.json` names the same claim
and the same run. The run comes from the seat lock record, which the member writes. A live
writer can put a new run id in the record before each retry, so `thisRun` is false each time and
`until` starts again from now. Renaming the session seat directory into quarantine does not stop
this, because a bind mount follows the inode. The precondition is a release for a session that
is over (record gone) while its container still runs with its lock free. That is unusual, which
is why this is low.

**Why it matters.** NT3 states the bound without conditions: "delays its token's retirement by 10
minutes at most".

**Fix.** Key the bound on the claim alone: if a not-posted `notice.json` of this claim carries
`until_ms`, keep that `until_ms` whatever run it names, and give up at it. Add a test that
changes the lock's run between retries.

### 4. low: HT3's problem text and fix differ from the SPEC

`src-go/internal/teamchat/health/health.go:188`.

**What is wrong.** The SPEC says "the team's rooms could not be read: <class>", with the fix
`teamchat-admin health` again, or the homeserver. The code writes the text with no class and the
fix `journalctl -u teamchat-health.service`. The test checks only the prefix.

**Fix.** Append the error's class (the `matrix` error class or "the child failed"; never the
error's text) and use the SPEC's fix. Or amend the SPEC in the same branch.

### 5. low: NT3 records a fixed `why`, not the error class

`src-go/internal/teamchat/seat/release.go` (`whyTransient`).

**What is wrong.** The SPEC says the transient record holds "the error class as `why`". The code
always writes "a transient failure; asked again until until_ms".

**Fix.** Record the class (for example from `matrix.Error`'s class, or "unreachable" or
"cancelled"). Or amend the SPEC.

### 6. low: a re-probe that cannot read the unit, or sees a new invocation, is recorded as probe `ok`

`src-go/internal/teamchat/health/probe.go:69-74, 111-113`.

**What is wrong.** `probeAgain` returns `answered` true when the unit state cannot be read, the
unit is no longer active, or the invocation changed. `homeserver` then sets `f.probe = status.ProbeOK`, although no probe answered.

**Why it matters.** This is right for "do not restart", but the record tells the board the
homeserver answered.

**Fix.** Return two results: whether to stop the miss count, and whether a probe answered.
Record `ok` only for a real answer, and `missed` (or `not-run` for a unit no longer active)
otherwise.

### 7. low: the tick's own restart can be followed by a false "restarted by hand" event

`src-go/internal/teamchat/health/probe.go:91-102` and `health.go:296`.

**What is wrong.** The record keeps the `NRestarts` read before the tick's restart. If systemd
clears the counter on an explicit restart job, the next tick sees a fall and adds "restarted by
hand" beside "restarted after three missed probes".

**Fix.** After `Units.Restart`, read the unit state again and store that `NRestarts` in
`f.unit`. Or confirm the counter's behaviour on the pinned systemd in the host checks and
document it beside n2e-D4.

### 8. low: a duplicated constant, and a test comment that does not match its data

`src-go/internal/teamchat/seat/holdings.go:23` and `health/health_test.go`
(`TestTheLXCDisconnectedNotice`).

**What is wrong.** `claimMark` repeats `paths.claimSuffix` (`.new-`); the two can drift. The
test's comment says the new run B "failed", but its record is `lockRecord(runB, false)`.

**Fix.** Export the suffix from `paths` (or a `paths.IsClaimName` helper) and use it in both
places. Correct the comment, or make the record a launch failure and assert that run A is still
announced, which is the more useful case.
