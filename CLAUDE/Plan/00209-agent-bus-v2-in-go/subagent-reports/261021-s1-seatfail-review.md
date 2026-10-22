Verdict: FIX-FIRST

# Plan 00209 stage 1: seat-read-failure branch review (c00f7a13, cca5f10b over cea276e4)

What I ran: `go test ./internal/teams/... ./internal/bus/...` (green), `go test -cover` on hot
(90.0%) and teamtest (84.0%), `bash -n` and `shellcheck -x` on the three scripts (clean). For the
mutations I used `go test -overlay`, so no tracked file was edited; `git status` is clean at the
end. I read execx.OS.Run, declare.Client.run (current, and at 7cc64525), ui.Picker/ui.Gum,
picker start-up (offers), tasks/build-go-command.yml, meta-deploy.bash and \_planlib's
plan_gather_leg / plan_prime_sudo.

Confirmed sound:

- The rewritten case in TestAddSaysWhichStepCouldNotBeRead now breaks the in-process reader, and
  it is stronger than before ("seat: agent-bus seat list in "). Mutation: pickSeat ignoring the
  SeatList error turns TestAddSaysWhichStepCouldNotBeRead and TestAddSaysWhyTheSeatListCannotBeRead
  red. Good.
- No other test in hot, membership, board, restart, shutdown, picker or chooser scripts a
  subprocess that stage 1 removed. The seats come from disk (AddSeat/HoldSeat/syncSeats), and the
  declare reads go through OnDeclare, which already panics. fetch_test has noSeatListProcess.
- ccy-teams starts env (declare.Client.run, `Name: "env"`, the same at 7cc64525), agent-bus
  (agentbus wrapperName, user-run seat list), fzf (ui.Picker) and gum (ui.Gum) by name, through
  exec.CommandContext. That uses the parent's PATH even when `Cmd.Env` is set. SHIMS is an absolute
  path, so Go's ErrDot does not apply. The shim's own `#!/usr/bin/env` and its
  `exec /usr/bin/env` use absolute paths, so it cannot call itself.
- The open's start-up asks questions only through gum confirm/choose/input (offers.go, ui.go). All
  of those are shimmed. The only unshimmed gum calls at open are `spin` and `style`. gum
  pager/table are never used.
- The interactive gate looks only at stdin and stdout (picker.go:113), so sending stderr to a file
  does not change the path the open takes. `timeout --foreground` is needed and correct: without
  it the child is put in a background process group and gets SIGTTIN on /dev/tty.
- The old revision is chosen correctly in all three ways this branch can land (merged into F44,
  fast-forwarded, or stage 1 merged first). There is a guard that OLD_REV has no
  src-go/internal/bus. The vendor tree is in the tree at 7cc64525. 7cc64525 has `main.RootDir`.
- Is it right that `-trimpath` leaves `-ldflags` out of the build info? Yes. The grep for the
  baked-in path as text is sound, and the cmp afterwards is what decides. The build flags match
  build-go-command.yml (GOTOOLCHAIN=local, GOFLAGS=-mod=vendor, -trimpath, -buildvcs=false, the
  same -X; the play's GOCACHE/GOBIN do not change the output).
- micros handles a comma decimal. Every number fed to `$((…))` is an integer. R1–R5, R10, R12
  and R13 are followed, and the files are mode 0755/0644 in the index. I found no
  install-specific names.

## Findings

01. **blocker: TestAddSaysWhenTheSeatListCannotBeReadAfterTheSeatIsMade cannot fail for the reason
    its name gives.** src-go/internal/teams/hot/failures_test.go:72-84.
    Evidence: the test's `r.Fake.OnFunc("agent-bus seat take", …)` replaces the rig's own take
    (hot_test.go:340-345). The rig's take is what puts the new seat in `r.m.exists`, and
    `syncSeats` writes seats to disk only from that map. So seat 4 never appears, and seat list
    never shows it, whether or not the list can be read. I deleted the `r.CrashSeat` line (by
    overlay) and the test still PASSES. The assertion holds only because the seat is missing, not
    because the read failed.
    Fix: in the OnFunc, do what the rig's take does (`r.m.mu.Lock(); r.m.exists[c.Args[2]] = "proj.4+local.podman"`), then CrashSeat. Add a control case without the crash that expects
    ExitOK and "✓ seat 4: created". Then the crash is the only difference between the two.

02. **should-fix: when the seat list cannot be read after the seat is made, or while waiting
    for it to be held, the reason is dropped, and no test can catch that.**
    src-go/internal/teams/hot/add.go:287-290 and add.go:386-395 (isHeld's heldErr). The
    assertions in failures_test.go:81 and :133 are the ones affected.
    Evidence: mutation `seatHeld` ignoring its SeatList error (hot.go:618-621, by overlay) leaves
    every hot test green, including both new tests. This is because nil seats and a read error give
    the same words: "agent-bus seat take ran, but agent-bus seat list does not show the seat free",
    and "seat 3 not held after 10 s: <report reason>". The user is told the seat is not free or not
    held when the real cause is a member.json the reader refuses (here, seat 9's). The add also
    polls the whole of Waits.Apply on a read error that will not go away. CLAUDE.md fail-fast asks
    for a clear message.
    Fix: put `err` in the makeSeat message when it is not nil. Carry heldErr into waitHeld's `why`
    (or stop at once on it). Then assert "urlsplit" in both tests, as
    TestAddSaysWhyTheSeatListCannotBeRead already does. That makes the mutation above go red.

03. **should-fix: the "not judged" outcomes exit 1 (REJECTED), but meta-deploy has a separate
    verdict for them.** \_acceptance-s1.inc.bash:212-215, 227-231, 284-287; acceptance-s1.bash:30,
    105-110. meta-deploy.bash:221-233 reads exit 2 as COULD NOT ESTABLISH.
    Evidence: these cases fail the leg like a real departure, and the run then prints
    "REJECTED: S3 …":

    - the open asked a question first;
    - no old open showed a seat list (`proven=0`);
    - no report_per_hour seat exists.
      The header and the list entry both say "S3 is not judged", which is not what the exit code says.
      There is a further problem with the update offer (picker/offers.go:317-343). It has no "never"
      marker, so it is asked on every open while any team is out of date or not built. On such a host
      S3 can never pass unless the owner applies play-agent-bus.yml from inside a check described as
      read-only.
      Fix: record the not-judged legs apart from the failed ones and exit 2 when nothing failed but
      something was not judged. Say in the entry that an out-of-date team blocks S3 until
      play-agent-bus.yml has applied it. The play-agent-bus.yml entry runs earlier in the same queue,
      so normally no team should be out of date.

04. **should-fix: one live change between the two halves of a pair makes the --list byte diff
    REJECT.** \_acceptance-s1.inc.bash:136-160.
    Evidence: each old/new pair runs one after the other, and the old run takes 9-23 s. In that time
    the owner's running sessions can change a member's STATE, an age word, or a held seat, and any
    one diff in any of the three runs rejects the run. The script's own message admits that age
    words can do this, and then tells the owner that "any other is not a departure". That puts the
    triage on the human after the fact.
    Fix: a real departure happens every time and a race does not. Reject only when every run
    differs (or the same hunk appears in all three runs). Report a diff that shows up in one run
    only as a note, or as not judged (finding 3).

05. **should-fix: the play-claude-yolo.yml entry that S0 depends on does not say it installs
    stage 1's ccy-teams.** CLAUDE/Plan/meta-deploy.list:95-102.
    Evidence: that entry's description lists only Plan 00191's CCY 3.99.13.14 changes. The new
    entry says "after the play-claude-yolo.yml entry above has installed the ccy-teams that reads …
    in-process". The list header says the comment is how the owner learns what a run will do, so the
    play's own description should say it.
    Fix: add one sentence to that entry: "It also rebuilds ~/.local/bin/ccy-teams with Plan 00209
    stage 1 (team, seat and declare reads in-process)."

06. **nit: the timing comparison favours the new binary in small ways.**
    \_acceptance-s1.inc.bash:137, 203.
    Evidence: old always runs first in each round. That warms the sudo/runuser/agent-bus list path
    and the page cache for the new run. The shims also add one bash start to each old `env` and
    `agent-bus` call. The saving expected is about 12 Python starts at about 0.45 s each, so the
    verdict will not flip. But DESIGN.md:38-42 records 9-23 s at a load average of 30-39, so a
    strictly-faster check on medians of 3 can still flake under heavy load, and the report has no
    load figure.
    Fix: alternate the order (odd rounds old first, even rounds new first), and write
    `/proc/loadavg` into the report next to each median.

07. **nit: the guard against scripting the seat list covers World.On/OnErr only.**
    teamtest/world.go:150-155.
    Evidence: `Fake.On`/`Fake.OnFunc` called directly (as hot_test.go and picker_test.go do for
    other commands) and the declare-read prefixes passed to `On(DeclarePrefix("limited"))` skip
    both guards. No test does this today.
    Fix: either put the checks for seat list and the declare reads in `scriptable` together, so On
    refuses a declare read as OnDeclare does, or add a sweep test that fails on any Fake rule whose
    prefix is a read handled in-process.

08. **nit: inside plan_gather_leg, `set -e` is off.** \_planlib.inc.bash:718 runs each check as
    `if ! "$@"`. These lines are therefore not checked:

    - the writes at \_acceptance-s1.inc.bash:134-135, 145-146, 174, 178, 200-201, 207;
    - `text="$(<"${member}")"` at :262, where a member.json that cannot be read becomes "" and is
      skipped without a word, and can turn into a misleading "no live member.json … sets
      report_per_hour";
    - the `sha256sum` calls inside say_report at :63-64 and :96.
      Fix: add `|| return 1`, and name a member.json that cannot be read.

09. **nit: the tty probe's error goes to the report and not to stderr.**
    \_acceptance-s1.inc.bash:192: `{ : </dev/tty; } 2>>"${REPORT}"`.
    Fix: capture the error and print it together with the FATAL line, as plan_prime_sudo does
    with PLAN_TTY_PROBE_ERR.

10. **nit: the old binary runs today's helpers/agent_bus/declare.py** (ROOT_DIR_BAKED is the
    current checkout). That is fair for stage 1, which changed no Python, and it measures only the
    Go change. The header should say so, so that a later stage reusing the script knows the old
    side is "old Go, current Python".
