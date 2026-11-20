# Triage: `ccy-teams wind-down` on a v1 team, "did not work at all"

This is a read-only triage. No tracked file was changed, nothing was run against real sessions, and
no Ansible was run. Install-specific names are replaced by placeholders: team-T, container-A,
user-C, project-B. Paths are relative to the branch `plan-xhost-lxc-members` (the code equals F44
at a1d2674a7).

## Version

- The owner names the team row's line "Compact every session of <team>". That label first
  shipped in **CCY 3.122.44** (commit 1177f2ea8). No commit after it touches
  `teams/winddown`, `tmuxccy`, `lxcfreeze` or `hostcheckout`. 8e60b9232 (3.122.45/46) changed
  only sessclose and shutdown, and says "the wind-down path ... is unchanged". So the pasted
  output is the same for any CCY from 3.122.44 to 3.122.48.
- **Correction to the brief.** The wind-down preview's wording "a busy one is waited for, up to
  300 s, never interrupted" did not first appear in 3.122.45. It has been in
  `winddown.go:452-454` since Plan 00203 Task 3.14 (d18a08ce0, 2026-10-19). The 3.122.45
  changelog wording is about the *close* (`shutdown`, `ccy --stop`), not the wind-down. So that
  wording cannot pin the version.
- **Fixes already on the branch:** none of them addresses any of these symptoms. The newest
  commits on F44 are the hooks-daemon upgrade and the LastPass play.

## 1. v1 LXC members: "no agent-bus-claude tmux session runs" (6 ✗)

### How wind-down looks for the session

- In `winddown.go:338-388` (`reachLxc`), every LXC row that the play's record holds as joined or
  stopped, and whose container runs, becomes a job. The job gets
  `tmuxccy.Tmux{Runner: Lxc.InContainer(lxcPath, container, user), Socket: "agent-bus-claude"}`
  and pane `agent-bus-claude` (`:381-382`).
- `lxcfreeze/attach.go:44-52` turns each tmux call into
  `sudo -n lxc-attach -P <path> -n <c> --clear-env -- runuser -u <user> -- env -i PATH=<guest> tmux -L agent-bus-claude …`.
  The user is the record's `it.User`. The server is the named socket `agent-bus-claude`. With
  `env -i` there is no TMUX_TMPDIR, so the socket is `/tmp/tmux-<uid>/agent-bus-claude`.
- On the first poll, `windstep.go:131-149` calls `liveSessions`, which runs `list-sessions`. A
  "no server running / error connecting … (No such file or directory)" answer from tmux is an
  empty list (`tmuxccy.go:118-123`, `gone.go:27-48`). `step` then finds the pane missing
  (`windstep.go:212`). `ended()` in phaseIdle fails the job with `j.missing` (`:246-247`), the
  exact message the owner saw.

### How the session is started

- `members.py:953-982` (`render_member_unit`) writes a unit with `User=<user>`, no PrivateTmp,
  only HOME and PATH in its environment, and
  `ExecStart=<tmux> -L agent-bus-claude new-session -d -s agent-bus-claude …/agent-bus-claude`.
- The only thing that starts it is `ccy-teams` Start its session (`lxcsession.go:132-167`,
  `systemctl start`).

### Do the two agree?

**Yes.** The socket name, the socket directory (the same uid, /tmp, no TMUX_TMPDIR on either
side, no PrivateTmp), the session name and the user (both from the play's member entry and
record) all match.

### Could a tmux error be misread as "no session"?

**No.** `goneAnswer` needs exit 1 and every stderr line to be in one of tmux's own forms.
Missing tmux (env exits 127), a refused `sudo -n`, or an `lxc-attach` failure all come back as
errors. Each would fail the job with that error, not with the "no session" message. A
protocol-version mismatch is also an error.

### The cause: deployment, plus a design defect

This host's `agent_bus_lxc_members` has 8 entries, and **none of them sets `service:`**. The
default is `False` (`members.py:457`, `:531`). So:

- The play never renders `agent-bus-claude-<user>.service` for them. `members.py:1354-1355`
  only checks and renders when `group[0].service` is set. Otherwise `_drop_service` stops and
  removes any such unit on every apply (`:1397-1400`, `:1256-1273`).
- Nothing in these containers ever runs a tmux server on the `agent-bus-claude` socket.
- Whatever session these members have was started by hand inside the container. Wind-down
  cannot reach a session started that way.

The probe's answer is **true**. The defect is that wind-down **ignores the declared `service`
flag**:

- It treats every reachable LXC member as if a service unit ran its session.
- Its preview says "its session agent-bus-claude in container container-A" without looking.
- It then counts the miss as a ✗ failure.

`lxcsession.offersV1` (`lxcsession.go:118-125`) already reads `Declare.LxcMembers` and its
`Service` flag. Wind-down does not.

Since B.12, a service member's session also does not run until someone starts it, so
"container up, no session" is the **normal** state. `ccy-teams shutdown` (B.13) already handles
this. It probes the server when it builds its plan, and lists a missing session as "no session
running in container X" under not running (`shutdown/lxc.go:106-118`). Wind-down never got the
same probe.

### "Shut down instantly": a misreading

- Wind-down ran only `tmux -L agent-bus-claude list-sessions` in each container, then failed the
  job on that first poll (`windstep.go:236-248`). Nothing was typed, killed or stopped. The ✗
  lines came "instantly" because the first look decided them.
- B.12's "DISABLE AND STOP" (`members.py:1238-1252`) acts only on a `service: true` unit that is
  enabled. With `service` unset here, it does not apply. `_drop_service` would only stop a
  leftover unit from an earlier `service: true` config.
- I could not establish whether `play-agent-bus.yml` or `lxc-no-autostart-acceptance.bash`
  (which can reboot containers on the owner's yes) ran recently:
  - The newest meta-deploy run logged in the main checkout's `untracked/plan-runs/_meta-deploy/`
    is 2026-11-18. It ran play-claude-yolo and play-python only.
  - The installed ccy-teams is at least 3.122.44 (committed 2026-11-20). So play-claude-yolo
    was run some other way.
  - The host's play ledger (in the owner's home) would say whether play-agent-bus ran.
- Whichever ran, none of it touches a session started by hand in a tmux server it does not
  own. One exception: a container reboot by the acceptance script ends every process in the
  container.
- **Owner check (read-only):** is each member's bus LIVE state "listening" (the board's STATE
  column)? If not, there was no session to wind down.

### Fix (rank 1)

In `reachLxc`, decide each running-container member by its declared entry and a probe, as
shutdown's `addGuest` does:

- **(a)** `service: true` and the `agent-bus-claude` session is listed: make it a job, as now.
- **(b)** `service: true` and no session: list it under not running, as "no session running in
  container X (start it: Enter on its row, Start its session)". This is not a failure.
- **(c)** `service` false or unset: wind-down cannot type into a session started by hand.
  - If the row's `Live` is `listening`, list it under **not reachable from here**, with the
    hand text.
  - Otherwise list it under not running.
- **(d)** List the server before the preview, so the preview never names a session that is not
  there.

**Failing test first** (in `teams/winddown/lxc_test.go`):

- `TestAnLxcMemberWithNoServiceSessionFails` encodes the old W7 and becomes:
  - `TestAServiceMemberWithNoSessionIsNotRunning`: with `m.lxcSessions = nil`, exit 0, and
    "- agent@box: not running: no session running in container box".
  - `TestAHandStartedMemberIsNotReachedNotFailed`: an entry with `Service: false` and a row with
    `Live: listening` gives "✗ …: not reached: … type /goal clear in its session …", and no
    `lxc-attach` tmux call is made.
- The fixture needs a `Declared` fake. `lxcsession` has one to copy.

## 2. Host ccy seats: "✓ … compacted", but the owner says they "nooped"

### How the lines are typed and judged

- `typeLine` (`windstep.go:329-351`) calls `tmuxccy.Submit` (`submit.go:43-69`). Submit sends
  `send-keys -l <text>`, pauses 200 ms, presses Enter, then reads the box up to 5 times. **An
  empty or busy box counts as taken** (`boxTaken`, `:99-101`). A box still holding text gets
  another Enter, up to 5 (issue #288's held submit). No box gives `ErrBoxNotDrawn`, which is
  left to the next phase.
- In phaseText, a Ready or Busy box sets `wound = true` at once (`windstep.go:275-280`).
- The answer is judged by `sessclose.Quiet.Idle` (`sessclose.go:930-946`). A box seen busy and
  then Ready is answered. **A box never seen busy that stays Ready for 30 s also counts as
  answered.**
- For a host session, "compacted" is
  `transcript.CompactedSince(file, compactAt)` (`compact.go:144-154`, `:172-183`): a main-thread
  `compact_boundary` entry dated at or after the second /compact was typed, in the transcript
  that the session record names.

### What that means here

**"Compacted" on a host seat rests on real evidence**, a new `compact_boundary` in that
session's own transcript, not just on /compact being typed. For all three seats to read ✓
compacted, a compaction finished in each session's recorded transcript after wind-down typed
/compact. A ✓ with no compaction behind it would need the record to name the wrong transcript.

So the likeliest reading of "nooped" is a **misreading**: Compact every session leaves sessions
open by design, and the agent's answer to the wind-down may have been brief. This is **not
established**.

**Read-only check to settle it:** in each of the three transcripts, look around the run's time
for a user entry beginning `[ccy-teams wind-down]` and for a `compact_boundary`.

### Weak points (real, ranked)

1. **The text is never seen to arrive.** Submit does not check that the text reached the box
   before it presses Enter. An empty box after Enter is "taken" even if the keys never reached
   Claude. Two ways that happens:

   - The pane is in tmux copy mode (an attached owner who scrolled). `send-keys` keys go to the
     mode, and Enter leaves it.
   - A dialog takes the keys and closes.

   The screen then reads Ready → `wound` → 30 s of quiet → "answered". The ✓ text
   "/goal clear and the wind-down typed" can be printed for a line Claude never received.

   - **Fix:** before typing, read `#{pane_in_mode}` and fail if it is set ("the pane is in copy
     mode; leave it and run again"). For a host session, confirm the turn with
     `transcript.TookInputSince(file, typedAt)` (`transcript/cache.go:200-206`, which already
     exists) before setting `wound`.
   - **Failing tests:**
     - tmuxccy: `TestSubmitRefusesAPaneInCopyMode` (the fake answers `display -p #{pane_in_mode}` with 1).
     - winddown: `TestAWindDownLineTheTranscriptNeverRecordsIsNotWound` (the fake transcript has
       no user entry since typing; expect ✗ "the wind-down was typed but the session never took
       it").

2. **A never-busy answer counts as answered** (`Quiet.Idle`, the 30 s settle). Together with
   point 1, a lost line looks like an answer.

   - The `TookInputSince` check above also covers this for host seats.
   - LXC members have no transcript here, so they keep the pane-only reading and should say so.

3. **LXC "compacted" is pane-only** (`compact.go:166-169`): idle again after /compact counts as
   compacted. The report already says the end was read from the pane, which is acceptable once
   that is clear.

4. **`paneTarget` targets `=<name>:`** (`panes.go:49`), the session's active window. A session
   with a second window active is typed into that window. This is an edge case.

   - **Fix:** target the session's first pane by id, from `list-panes`, or fail on more than one
     window.

5. **A second-resolution timestamp, and any boundary counts.** A compaction the agent itself
   triggered after /compact was typed counts too. This is minor.

## 3. "Not running" list: `1@team-T`, `2@team-T`, … repeated and indistinguishable

- **Source:** `winddown.go:320-327` (`addSeat`):
  - `p.idle = append(p.idle, member{row.Member + "@" + team, whyFree})`
  - the same for a stopped record (`:324`)
- `row.Member` is the seat number within one checkout. A v1 team spans several checkouts
  (project-B, …), each with its own seat 1, 2, …. So the label repeats with no project.
- `shutdown.go:320-322` and `restart/jobs.go:163` at least add `(handle)`.
- **It should say** the member's handle, as the board's MEMBER column shows it (`row.Label()`,
  `project-B.1+local.podman`), followed by the seat and checkout, like the job lines:
  `project-B.1+local.podman   seat 1 in ~/Projects/project-B: no session holds it`.
  - A seat with no handle yet (`-`) should be `seat 1 in ~/Projects/project-B`.
  - Apply the same wording to the held seat (`:322`) and in shutdown (`:320-322`). This is the
    class "a v1 seat named by its number alone", and the sweep found three sites.
- **Failing test:** `winddown_test.go`, `TestFreeSeatsOfTwoCheckoutsAreToldApart`: two checkouts,
  each with a free seat 1. Expect two distinct lines, each naming its handle and directory.
  Today both read `1@alpha   no session holds it`.

## 4. First line: "\<path under the container's rootfs> is LXC container container-A's checkout … must not run there"

- **Printer:** `picker/offers.go:360-371` (`offerJoinHere`). On the picker's first pass
  (`picker.go:299-303`), `Checkouts.Find("")` resolves the **shell's working directory**, and
  `hostcheckout.go:50-55` (`Refusal`) prints the text with `Kit.Tell`.
- The owner's shell was inside a container's checkout, seen through
  `/var/lib/lxc/container-A/rootfs/…`. That is a real container checkout, so the fact is right.
- **For this command it is a false alarm in effect.** Opening ccy-teams starts no session. The
  join offer only needed to be skipped. The same refusal also appears in the picker header as "A
  new ccy session is unavailable: …" (`picker.go:222-235`), which is where it belongs.
  Printing it as a free-standing warning on open reads like an error about what the owner just
  did, and it stays in the scrollback above the wind-down output.
- **Fix:** in `offerJoinHere`, return without telling when `!here.Host()`. The header line
  already carries the refusal. Alternatively, reword it as "No team session is offered for this
  folder: <refusal>".
- **Failing test:** `picker` `TestOpeningInAContainerCheckoutPrintsNoWarning`: cwd under the
  fake LXC path. Expect no stderr line before the list, and the header to hold the refusal.

## 5. Exit 1 and the summary

- `report` (`winddown.go:478-522`) returns 1 when `failedN > 0 || unreached > 0`. Six ✗ LXC
  jobs made it 1. "12 not running" correctly counted the 10 free seats and the 2 stopped
  containers.
- **With fix 1:**
  - LXC members that were not listening on the bus move to not running.
  - The result is "3 wound down, 0 failed, 18 not running" and **exit 0**. That is right: every
    running session it can reach was wound down, and nothing it can see is out of reach.
  - If any hand-started member was listening, it is "not reachable from here" and exit 1 stays.
    That is also right, since the owner must type into it by hand, and the hand text is printed.
- With the label fix, the not-running lines can be told apart.
- The help's exit-code text (`:154-158`) should add "a member whose session was started by hand
  in its container" to exit 1's list. It should also say that a service member with no session
  is not running.

## Ranked defects

| Rank | Symptom                                 | Class                                                                           | Kind                                                                                              | Fix site                                               |
| ---- | --------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| 1    | 6 LXC ✗, exit 1                         | Wind-down assumes a service unit, does not probe, counts "no session" as failed | Code defect (on top of this host's deployment choice: no `service: true`)                         | `winddown.reachLxc`, mirror `shutdown/lxc.go addGuest` |
| 2    | Possible ✓ for a line never received    | Submit and phaseText judge "taken" from an empty box alone                      | Code weakness. Not shown to have happened here: the host seats' compactions are transcript-backed | `tmuxccy.Submit`, `windstep.act` phaseText             |
| 3    | Repeated `N@team` labels                | A v1 seat named by its number alone                                             | Code defect (3 sites)                                                                             | `winddown.addSeat`, `shutdown.addSeat`                 |
| 4    | Container-checkout warning on open      | A refusal told when nothing was asked                                           | Code defect (wording and placement), with a true fact behind it                                   | `picker.offerJoinHere`                                 |
| 5    | Owner's "LXC seats shut down instantly" | -                                                                               | Misreading: wind-down only listed sessions; the ✗ came on the first poll                          | -                                                      |
| 6    | Owner's "ccy seats nooped"              | -                                                                               | Most likely a misreading (compactions recorded); not established without the transcript check     | -                                                      |
