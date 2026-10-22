# n3e brief review (Plan 00209 N3, slice n3e)

Reviewed: `subagent-reports/261022-n3e-handoff.md` at 1a97bab64, against SPEC-n3.md section 6
(and 1, 9 gap 9, 10, 11), DESIGN-v2-B-member.md 1.4-1.5, 2.4.4, 2.7, 2.8, 2.9, 2.10, 2.12, 2.15,
DESIGN-v2-A-protocol.md 13.1-13.7, Plan 00217's SPEC-team-colours (aea3e0ba2, C1-C18), Plan
00213's SPEC-status-line S5-S15 and S26-S31, and the built code at 91faebcf0 (`go doc`, reads of
`engine/status.go`, `link/server.go`, `link/facts.go`, `client/nolink.go`, `teamsync/session.go`,
`execx/session.go`, `health/view/view.go`).

## Verdict: FIX-FIRST

The split is sound in shape: every section 6 item has a home (section 1's table is complete), B27
is quoted in full and every clause has a part, Plan 00217's palette, hash, override grammar and
`<team>: <member> (<r>)` piece are carried correctly (C12/C13's colour file and C6's Python
parity fall away rightly, since v2 holds the colour in memory and shares nothing with v1), the
offline rule is carried into every part with a test, and the n3d seams are named and faked. The
quoted built APIs agree with `go doc` everywhere I checked (clock, wire ops, `team.Batch`,
`Health`, `state.Session`/`StatusTeam`/`Awaiting`/`Told`, `proto.Delivery`/`Membership`/
`HeldBack`, `kit.Hooks`, `cli.Env`/`Parse`, `wire.Deadline`, `paths.InboxSocket`). Four gaps
would each leave a design requirement unbuilt or a part unable to build without network calls on
the hook path, so they are fixed first.

Findings: 4 major, 6 medium, 9 low.

## Findings

### Major

1. **Major. `Batch.HeldBack` is never filled (sections 5, 8; n3e-D1).** The flood budget is
   applied in teamsync (`session.go` `judgeAll`, `Judge.Flood`, verdict `ReasonRate`), but no part
   edits teamsync to count those drops into `Batch.HeldBack`: n3e-3 adds the field, n3e-6
   consumes it, and n3e-7's teamsync files are `facts.go` and `receipts.go`. T19's `held back:`
   line (A 11.4, B 2.7.3) would exist only in a test that plants the field. **Fix:** give n3e-7 a
   hunk in `teamsync/session.go`: each `ReasonRate` verdict of an agent sender counted per sender
   into the persisted batch's `HeldBack` (committed with the flood window, so a discarded answer
   counts nothing); test with a flooding peer over fakehs reaching the `held back:` line.

2. **Major. The coordinator's handle has no route to the engine (sections 8, 12).** `HookTeam` and
   `TeamRole` carry `Coordinator`, `hooktext.Frame` needs it, and the frame and MEMBERSHIP name
   it, but `team.Health` has only `Role` (n3e-3 adds `Colour`, `Description`). The roster is
   reachable only through `Notices.Roster` (RoomState plus VerifyMain: a network call), which the
   state goroutine may not call and a hook op (300 ms, "no hook op makes a network call", OF1)
   must not wait on. **Fix:** n3e-3 adds `Health.Coordinator string` (the roster's non-parked
   coordinator's handle, "" for none); n3e-7's `facts.go` sets it at each main-room verification
   with Colour and Description; n3e-10 reads it from Health only.

3. **Major. The unusable counts never reach `status.json` or `status` (sections 7, 8, 12).** B25
   and T13 require stdin a hook cannot use to be "counted and reported through `status`,
   `status.json` and the status line", and B 2.7.2 shows `unusable.render` in `status`. The link
   counts both in `session.json` (n3e-6, n3e-10) and n3e-5 draws `unusable_hook_stdin`, but the
   engine writes `status.json` (`status.go:94` never sets `UnusableHookStdin`) and no part feeds
   it. **Fix:** n3e-6, which adds `Session.CountUnusable` and `OnRender`, also adds an engine
   setter (for example `SetUnusable(ctx, hookStdin, render int64)`) that the link calls at start
   from `session.json` and after each count; `publish` writes `unusable_hook_stdin`, and the
   `status` record carries both counts. Test: the count survives a link restart into
   `status.json` (2.15's unusable-inputs row).

4. **Major. The stall does not reach the hooks (sections 8, 12).** B 2.7.3: "if every write has
   failed for 120 s, the team rows show `(stalled)`, the hooks bring back pending counts and Stop
   blocks once per pending set (E15)". n3e-10's prompt and Stop rules key on "delivery not live,
   or held as missing", and `Live()` stays true during a stall (the socket is not gone), so a
   stalled session is told nothing and Stop never blocks; `hooktext.Stalled` has no caller.
   **Fix:** in n3e-10 a stalled delivery counts as not pushed: the prompt answers
   `hooktext.Stalled`, Stop blocks once per pending digest; tests for both, and the twin where the
   stall clears.

### Medium

05. **Medium. A previous run's `awaiting` and `missed` entries are unspecified (section 11;
    n3e-D9).** The store outlives the session: the next session on the seat takes it with the old
    run's awaiting entries (another transcript, which will never hold their needles) and missed
    entries, whose `at_ms`/`stop_ms` are CLOCK_BOOTTIME of a boot that may have ended (D9 says "a
    session never spans a boot", but the store does). n3e-9 covers a link restart only; the design
    is silent. **Fix:** a decision (n3e-D20): when a new run takes a store, each awaiting entry of
    an earlier run is declared missing at once (items back to `pending` marked `repeat`, out of the
    ledger, in one write; not `held`, since the new session's socket has not held them), and
    earlier runs' `missed` entries are dropped; test across `BeginRun`. Flag it for the lead as a
    B 2.7.3 addition.

06. **Medium. The FIFO test contradicts the open the brief prescribes (section 4).** n3e-2 opens
    the transcript as TB4 does (`O_NOFOLLOW|O_NONBLOCK`, then `fstat`) yet asserts that a writer
    polling a FIFO at the path gets `ENXIO` every time. An `O_RDONLY|O_NONBLOCK` open of a FIFO
    succeeds and makes the link a reader (n3-D17), so the test fails, and the common rule "never
    open a FIFO" is broken. **Fix:** `proof.Scan` opens `O_PATH|O_NOFOLLOW|O_CLOEXEC`, `fstat`s a
    regular file, and reopens it through `/proc/self/fd/<n>` with a device and inode check (TB1's
    pattern); the `Mark`'s Dev/Ino come from that `fstat`.

07. **Medium. Tagged failpoint tests are in no gate (n3e-D19).** D19 adds only PT1's package. But
    n3e-9's P10 test, n3e-6's DV3 failpoint rows, and n3c's `link/failpoints_test.go` and
    `keeper/failpoint_armed_test.go` are `//go:build busfailpoints` and run in no gate today, so
    P10 would merge unrun. **Fix:** the gate row is
    `go test -race -tags busfailpoints ./internal/teamchat/...`, landed by the first part that adds
    a tagged test (n3e-6), with n3e-13 adding only its seeds.

08. **Medium. n3e-5 rebuilds what `execx` already has (section 7).** `execx.Cmd{NewSession: true}`
    through `Runner.Run` is the S9-S12 run, built for this status line's inner (`execx/session.go`
    `runSession`: setsid, SIGKILL to the group when ctx ends, a grandchild holding stdout bounded
    by `Grace`). The brief has `execx.Starter` plus an own `unix.Kill(-pgid)` seam. **Fix:**
    `Runner.Run` with `NewSession` and a ctx cancelled by `clock.AfterFunc(10 s)` or by the
    signal; the builder checks that a signal arriving before the pid exists still kills the group
    (exec's `Cancel` covers a started child). Also add S9's working directory (the project
    directory when it exists) and S7's "HOME only if absolute", which section 7 omits.

09. **Medium. `hooktext` importing `client` inverts the layering (sections 6, 12).**
    `Recovery(d client.Down, ...)` makes hooktext import client, and `engine.HookTeam` embeds
    `hooktext.Counts`, so the engine (and the link) would import `client`, and through it `matrix`,
    `notice`, `bundle`, `seat` and `cli`: the state engine depending on the CLI package. **Fix:**
    `Down` and `ReadDown` live in a small leaf package (or `state`), which client, hook, statusline
    and hooktext import; `HookTeam`'s counts are a type the engine owns (or one in that leaf), so
    the engine never imports hooktext.

10. **Medium. n3e-6 is above one builder's size (section 8).** It holds a new pure package
    (`Choose`, `Build`, the mapping goldens per kind), the delivery loop with restored re-fetch,
    pacing, caps, retry, stall and four failpoints, the membership send, the trailing-only rule,
    the `recv` failpoint hunk and a link hunk, with about twelve test groups. **Fix:** split the
    pure `deliver` package (it depends only on n3e-3 and proto) into its own wave-2 part, so
    n3e-6 keeps the loop; or move `SendMembership` and its pacing to n3e-10.

### Low

11. **Low. `proto.Desired` has two callers (section 5).** Besides `rooms.go:149` there is
    `health/view/view.go:395` (the questions room's power-level form). **Fix:** name both; the view
    passes `""`, as it reads only questions' power levels.

12. **Low. The colour and topic types go into `proto.StateTypes` (section 5, n3e-D5).**
    `StateTypes` is documented as A 6.5's trust types, feeds `TimelineTypes`, and drives
    teamsync's `trustTriggered` (`session.go:576`), so every colour or topic event would run a full
    re-verification. **Fix:** a separate list of informational state types joined into the filter's
    state and timeline sections only, keeping `StateTypes` the trust set; or record the re-verify
    as intended (it is where the colour is re-read) in D5.

13. **Low. C8's `{}` rule (section 10).** C8 writes `{}` only "when the entry sets none and the
    event exists". `Desired` carrying `ColourContent("")` would write `{}` into every team's main
    room at the first apply after upgrade. **Fix:** rooms skips a `{}` colour spec when no colour
    event exists; test "no colour, no event: nothing written".

14. **Low. `WriteUser`'s `ErrRetry` after the awaiting entry is persisted (section 8, step 07).**
    Nothing of the user line was written, yet the entry stays `unknown` with the ids out of
    `pending` until 180 s or 1 h, then returns them marked `repeat`. **Fix:** decide it: either one
    write drops the entry and puts the ids back in `pending` with no `repeat` (nothing reached
    Claude Code), or say it waits for the scan and why.

15. **Low. The hook route needs the peer's pid (section 12, n3e-D18).** `server.peerUID`
    (`server.go:271`) returns only the uid; `Facts.Peer` needs the `SO_PEERCRED` pid. **Fix:** name
    the change (a `peerCred` returning uid and pid) in n3e-10's server hunk.

16. **Low. An unreadable `status.json` under a running link is silent (section 7).** The OF1 test
    allows "no segment" for a missing, oversized or undecodable `status.json`, which hides a fault
    (B25 "never ignored silently"; C16 "said, never guessed"). **Fix:** with `link.lock` held, a
    red `teamchat status unreadable`; with it free, the red stopped form as written.

17. **Low. n3c's failpoints are nine, not ten (sections 0, 15).** `go doc failpoint` lists eight
    of PT1's points plus `link-after-judge-before-persist`. **Fix:** correct the count, and have
    n3e-13 name all fifteen with n3d's.

18. **Low. n3e-D13 departs from the text (section 17).** SPEC-n3 SY8 and RC1 and B 2.10 ("into
    `report.json` and a NOTICE") still say a NOTICE; D13's `stopped:` line and hook lines are the
    better carrier. **Fix:** record it as a departure for the lead to amend SY8, RC1 and B 2.10.

19. **Low. Sections thin for a builder working without the design.**

    - n3e-9 gives no API: where `Scan` runs (file I/O on or off the state goroutine), the three
      timers (180 s quiet, 1 h, 24 h) as `clock.AfterFunc`, and the restart entry point.
    - n3e-10 does not map each event case to its `hooktext` template.
    - n3e-13 has no driver design: the operations, the mapping from seed to schedule, and how
      P1-P11 are judged from the stores, the fake transcript and fakehs.
    - `hook.Main` and `statusline.Main` take only `cli.Env`, so their fake-clock, dialer and
      signal tests need unexported deps seams, as `client.run` has. Say so.
    - The frame's role lines and the skill name n3f's verbs (`ask`, `questions`, `report`), which
      stay `unbuilt` until n3f. Say that PK2 accepts them because `cli.Parse` knows the whole verb
      table.

## Checked and sound

- Wave order and compile dependencies. Wave 1's four parts import nothing of each other. Wave 2
  compiles against n3e-3 and n3e-4. n3e-9 and n3e-10 are parallel: each tests through
  `SetSession` and the store, and touches different engine and link files, with hunks named.
- The engine and link file split. n3e-6 and n3e-7 both touch `statusRow` and `engine.Config`,
  in separate hunks, which the brief names.
- n3e-D3, D4, D6, D7, D8, D10, D11, D12, D14 to D18 are sound. D12 matches the owner's offline
  rule. D17 matches `client/nolink.go`'s `starting` case.
- DV1 to DV6, HK1 to HK3, SL1, PK1, PK2 and PT1 each map to tests that exercise the code path.
  The exceptions are findings 1, 3 and 4.
