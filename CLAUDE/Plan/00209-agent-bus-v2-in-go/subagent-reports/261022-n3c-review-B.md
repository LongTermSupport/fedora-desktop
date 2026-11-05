# n3c slice review, part B: processes and the client side

Slice 727087da9..a3b52b845. Scope: keeper, link, lock, client, ends, cli, wire, wiring, homefs,
bundle, exits, reexec, failpoint, clock, paths, execx, cmd/teamchat, and the e2e and failpoint
harness. The reviewer checked each finding against the code; none was confirmed by a test run
(the brief was read-only). The reviewer's hook blocked its write, so the lead saved this text
from its message.

## Verdict: FIX-FIRST

3 major, 5 minor, 4 nit.

## Findings

01. **MAJOR: an open `wait` stops the link from exiting, and it keeps the seat locks.**
    link/link.go:439-448 (teardown) and link/server.go:116-128 (close).

    - teardown calls srv.close() first, which closes the connections and then waits on s.wg.
    - A wait handler stays in eng.Serve (engine/recv.go:352-380) until it is woken, its retry fires
      or ctx.Done(). Its ctx is the run ctx plus the request deadline (server.go:199-200), and
      closing the socket cancels neither.
    - cancel() runs only after srv.close() and Shutdown. So the link cannot reach Shutdown, which
      closes the seat descriptors, or release link.lock until every pending wait reaches its
      deadline: by default 110 s, at most 3600 s.

    Scenario: an agent idles in `teamchat wait` and the keeper is SIGKILLed.

    - The link posts disconnected, then hangs in teardown. That breaks LN1 and T20 (the link
      exits within 5 s).
    - Meanwhile the next keeper's sweep removes the run directory under the live link. A relaunch
      of the same seats fails 75 seat-held until the wait's deadline passes.
    - In the normal end, the keeper's 5 s endTimer SIGKILLs the link, so any session that ends
      with a waiter ends with its link killed.
    - An abandoned client's wait also stays registered until its deadline.
    - No test covers close() with a blocked Serve; server_test.go:208-220 tests only the
      deadline.

    Fix: give each connection a ctx that is cancelled on close or on EOF of the request side, or
    cancel the serving ctx before srv.close(). Add a test that close() with a blocked wait
    returns at once.

02. **MAJOR: a remove the link cannot answer gets the whole link killed.**
    link/link.go:368-378 and wire/control.go:403-411.

    - remove sends no Removed and no Left when eng.Remove errors. That includes errNotHeld
      (engine/teams.go:199-201), for a team whose add failed in the link (link.go:355-364).
    - It also sends nothing when UserID is "". That happens when seat.json is unreadable and there
      is no store (engine/teams.go:94-110), and Removed.check refuses an empty user_id anyway.
    - The keeper's 10 s timer then runs killLink (keeper/seats.go:264-267), and every team's link
      restarts.

    Scenario: the owner removes a seat whose bundle is broken, which is exactly the seat one would
    remove. When the keeper's 10 s timer fires, every team loses its link, its hand-outs and its
    waits. LN8 says the link answers.

    Fix: make user_id optional in Removed, and have the link always answer, errNotHeld included.

03. **MAJOR: no gate runs the failpoint tests.**
    qa/go-qa.toml and scripts/qa-gates.tsv hold no build tags; busfailpoints appears only in
    qa/rules/table.go:244. So qa-all, qa-changed and CI never build, vet, lint or run these:

    - failpoint/armed\*.go;
    - keeper/failpoint_armed_test.go;
    - link/failpoints_test.go;
    - wiring/e2e_failpoints_test.go, the nine crash scenarios.

    PT1 and gap 9 put them in the gate.

    Fix: add a gate run of `go test -race -tags busfailpoints ./internal/teamchat/...`, plus lint
    and vet under the tag.

04. **MINOR: a relaunch soon after a keeper crash fails 75.**
    keeper/prepare.go:537-547 and lock/lock.go:34-39. The dying link holds the seats through
    finish: 5 s, or longer per finding 1. Lock tries are only 3 x 50 ms.
    TestFailpointKeeperLinkWriting waits for the link to be gone first
    (e2e_failpoints_test.go:276), so this case is untested.

    Fix: when the holder's run has a free keeper.lock, wait up to endWait plus a margin before
    refusing with 75. Add the e2e case.

05. **MINOR: `wait` at its deadline can exit 69 instead of 4.**
    client/client.go:189-207, link/server.go:195-198 and client/nolink.go:48.

    - With the link down at the deadline (crash-loop backoff), the client exits 69, not n3c-D7's
      4\.
    - At the boundary, the retry re-sends a request already past its deadline. The running link
      discards it unanswered, and the client reports 69 "starting or restarting" about a live
      link.

    Fix: for wait, exit 4 timed-out when left \<= 0, and never dial past the deadline.

06. **MINOR: the desired-membership watch can end silently.**
    keeper/osproc.go:121-167 and keeper/seats.go:138-148.

    - IN_DELETE_SELF, IN_MOVE_SELF and IN_IGNORED have an empty name, so they are dropped, and the
      channel never closes.
    - A parse error closes the channel, and a closed channel only writes a log line.
    - If the host recreates the desired directory, keeper.json and report.json keep saying
      "applied" while no change is followed.

    Fix: report refused (missing, or a new code), publish it, and re-add the watch.

07. **MINOR: the kit's `--version` has no bound.**
    keeper/prepare.go:206-208 runs it with context.Background(), despite the "bounded" comments.
    A hung kit hangs the launch. SIGTERM is already Notify'd (keeper.go:167-168) but is read only
    in the loop, so it cannot end the launch either.

    Fix: a bound of a few seconds, reported as 78.

08. **MINOR: the sweep can remove a sibling keeper's new run directory.**
    keeper/prepare.go:310-337, against makeRun at 356-364. Between Mkdir(run) and TakeKeeper,
    another keeper's sweep can take the new keeper.lock, find no session.pid and RemoveTree the
    directory, so the concurrent launch fails 78.

    Fix: build the run directory under a name ParseRunID refuses, lock it, write session.pid, then
    rename it into place. Or have the sweep skip a directory that has no keeper.lock yet.

09. **NIT: a link that dies after `end()` gets no second chance.**
    keeper/loop.go:366-368 and 626-629. If the link dies after end() has told it, linkExited in
    the ending phase finishes without an end-only restart. The end degrades to the next take's
    disconnected, so it does recover.

10. **NIT: e2e cleanup may SIGKILL a reused pid.**
    wiring/e2e_world_test.go:581-585 SIGKILLs s.agent even after the session has ended, when the
    keeper has already reaped CMD and the pid may be reused. Kill only when !s.exited().
    Otherwise the harness is sound:

    - kill refuses pid \<= 0;
    - childOf refuses group pids;
    - nothing signals the test's process group.

11. **NIT: an EAGAIN connect exits 1.**
    client/client.go:211-217 maps EAGAIN (the link's backlog is full) to exit 1 internal. Treat it
    as gone.

12. **NIT: a failed Dup on a hot add is never retried.**
    keeper/seats.go:219-228: the keeper holds a seat the link never gets, and reconcile does not
    retry it. Drop the seat instead.

## Checked and found sound

- W-D1: the link opens --state with homefs Private, which refuses another uid's directory or a
  mode other than 0700.
- Bounds: the 30 s ready bound, the link's 10 s Hold over launch and adds, the 10 s removal bound
  and the 30 s owed bound all behave as specified.
- Hot add and remove while the link restarts: adds go through greet, and removals through
  settleRemovals.
- Locks: flock on OFDs, released by close and never by LOCK_UN; AdoptSeat checks SameFile and that
  the lock is held.
- Processes: Claude is held by pidfd with start ticks; no PDEATHSIG.
- Exit codes: 69 per LN3; reach and seat disconnected give 20, for that team only.
- Failpoints: compiled out without the tag, and the release-build scan checks the binary.
