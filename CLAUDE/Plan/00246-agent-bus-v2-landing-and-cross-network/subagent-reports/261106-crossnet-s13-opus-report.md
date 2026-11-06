# Task 8.8, S13: the builder's report

Branch `feat/crossnet-s13`, from `feat/crossnet-s12-int`. S13 (X32), the probe through the
terminator, is built, tested and committed. The health unit's text is not written, as told.

## What is built

- **The probe is `matrix.Reach`** (`matrix/reach.go`), the one home of transport configuration
  (X19). It builds the pin of X16 with `newPin`, using `https://<listen address>` as the base, so
  the root pool is the team CA alone, TLS 1.3 is the minimum, and the verified server name is the
  address itself (an IPv6 address goes in brackets). It uses the member client's
  `newHTTPClient` and asks `GET /_matrix/client/versions` with no credential.
  `matrix.ReachLimit` (5 s) bounds the probe; the bound is armed on the injected
  `matrix.Timers` and cancels the request, as `client.go`'s try does. Its result is nil for a
  200, or a `*matrix.ReachError` whose `Cause` is fixed text:

  - `the connection was refused` (ECONNREFUSED);
  - the pin's own handshake text, `TLS verification of <addr> failed against the pinned CA sha256:<fp>: <class>`;
  - `no answer within 5s` (the bound fired);
  - `it answered HTTP <code>`;
  - `the connection failed` (anything else).

  When the caller's own context is cancelled, the result is the context's error and no cause is
  given. A missing address, missing timers or a bad CA is refused as `ErrInvalid`.

- **`host.AppliedReach(root)`** (`host/reach.go`) is what the last apply recorded: the team CA
  certificate, plus the listen set of units.json on the port of team.json, which is what the
  terminator was started on. It shares its reading of units.json and team.json with
  `AppliedLeafNames`, through the new `appliedListen` in `host/leaf.go`.

- **In the tick** (`health/reach.go`, `probe.go`, `health.go`), `health.Config` has two new
  required parts: `Reach` (the seam, `host.AppliedReach` on the host) and `Timers`
  (`matrix.SystemTimers()` in `admin/wire`). After the renewal step (X31), `terminator()` starts
  one goroutine per listen address, if all of these hold:

  - the unit was read and is active;
  - no restore is recorded, including one first seen under the lock by the renewal's re-read;
  - the renewal did not restart the terminator this tick.

  `gather` collects the probes at its end, after the view, the guests, the backups and the disk.
  So a slow address delays neither another address nor the tick's other work, and the tick waits
  at most `ReachLimit` beyond its own work. If gather fails after the probes start, a deferred
  `abandon` cancels them and waits for them, so none outlives the tick (`project_goroutine_end`:
  each probe ends on the wait group).

- **One problem per failing address**, in reach order: What
  `the TLS terminator does not answer on <addr>: <cause>`, Fix `systemctl status teamchat-tls@<team>.service`.
  If the reach cannot be read, that is one problem of its own: `the TLS terminator cannot be probed: its listen addresses or the team's CA certificate cannot be read`,
  with the fix `teamchat-admin health <team>, then apply the team again (<play>)`. The text of an
  error never reaches a problem.

## Tests (X32's list, and more)

`matrix/reach_test.go` uses a real TLS 1.3 httptest server on 127.0.0.1 behind a leaf made by
`pki`, and `clock.Fake` both for the bound and as the wall clock against which the certificate is
judged:

- an answering address is nil, and the probe asked only the versions path, with no Authorization
  header;
- a closed address is refused;
- a leaf of another CA gives the handshake text with the pinned CA's fingerprint and
  `unknown authority`;
- a leaf for another address is `name mismatch`, which proves the address is the verified name;
- an answer other than 200 is `it answered HTTP 502`;
- an address that accepts and never speaks ends at `ReachLimit` on the fake clock (the test runs
  `AwaitDue` then `Advance`), and the cause text names the bound;
- a cancelled caller gets the context's error;
- a bad configuration is refused;
- the IPv6 base is bracketed.

`host/reach_test.go` runs over a real applied world:

- the reach is `192.0.2.10:8448` and the CA file's bytes;
- a team that was never applied is an error;
- a missing CA certificate is an error.

`health/reach_test.go` runs the tick in front of the **real terminator** (`tlsterm.Start` on
127.0.0.1:0, serving a leaf made by `pki`, relaying to an httptest stand-in of the homeserver):

- every address answers: no problem, and the homeserver was reached twice;
- one listener is closed: exactly one problem, naming only that address, refused;
- a leaf of another CA: one problem with the fingerprint text, and the homeserver was never
  reached;
- an address that hangs beside one that answers: the answering address is hit and gives up its
  bound, and the view runs, all before the fake clock passes the bound. After `Advance`, the tick
  ends with exactly one `no answer within 5s` problem. This is the "delays nothing else" proof;
- an inactive unit: X18c's one problem, and the reach is not asked;
- an unreadable unit is not probed;
- with a restore recorded nothing is probed, and also when the restore is first seen under the
  lock;
- a reach that cannot be read is its own problem;
- a terminator the renewal restarted this tick is not probed;
- `Refresh` probes as the tick does.

All of these run under `-race -shuffle=on`, and the health, host, matrix and wire packages pass
with `-count=2` and `-count=3`.

## Planted defects, each seen red and restored

| Planted                                                            | Red                                                                                                                                                     |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| probe whatever the unit's state (the active/read guard removed)    | `TestAnInactiveTerminatorIsOneProblemAndIsNotProbed`, `TestAnUnreadableTerminatorIsNotProbed`                                                           |
| the under-lock restore re-check removed from the probe's guard     | `TestNoAddressIsProbedOnceARestoreIsRecordedMeanwhile`                                                                                                  |
| probe a terminator the renewal restarted                           | `TestATerminatorRestartedThisTickIsNotProbed`                                                                                                           |
| probes collected inside `terminator()`, before the view            | `TestAnAddressThatHangsIsAProblemAtTheBoundAndDelaysNothingElse` (at its 60 s test bound: "the slow address's bound is not armed")                      |
| probes run one at a time (no goroutine)                            | the same test, the same way                                                                                                                             |
| no refused class                                                   | `TestReachAClosedAddressIsRefused`, `TestOneClosedListenerIsAProblemNamingOnlyIt`, `TestRefreshProbesTheTerminator`                                     |
| no handshake class                                                 | `TestReachALeafOfAnotherCANamesThePinnedFingerprint`, `TestReachALeafForAnotherAddressIsANameMismatch`, `TestALeafOfAnotherCANamesThePinnedFingerprint` |
| the bound armed on `SystemTimers()` instead of the injected timers | `TestReachAnAddressThatHangsEndsAtTheBound` ("the bound was never armed")                                                                               |
| `Reach` missing from the production wiring                         | `admin/wire` `TestEverySeamIsSetAndNothingIsRead`, `TestStatusIsTheTicks`                                                                               |
| the problem built from `err.Error()`                               | four health tests, and the project rule `project_problem_error_text` (driver over `health`: `reach.go:48`)                                              |

Not proven: `abandon`'s cancel, used when gather fails after the probes started. No test makes a
gather fail with a probe still hanging, so the cancel was not planted out.

The two plants that broke parallelism went red only once the transport's own **real-time**
handshake limit (`dialLimit`, 10 s, in `newHTTPClient`) ended the slow probe. The test then
waited for a bound that was no longer armed, until its own one-minute context ran out. They are
red, but slowly; see finding 2.

## Defence Before Fix

One defect class showed up while building: my first wait for "only the slow address's bound is
armed" was a loop of `AwaitDue` and `runtime.Gosched`. The existing project rule
`project_test_spin` flagged it at once (driver run over `health`), so the class already has its
detector. I replaced the loop with an event: the test's timers wrapper signals each bound stopped
before it fires. No other new class came up. The driver sweep, with the plants restored, over
`./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...`: no finding. Lint and
analyzers were not narrowed, and nothing is suppressed.

## Departures from the S12 builder's design

- **A function, not a `Client`.** The design proposed a `matrix` constructor built on the
  client. But `Client.failed` turns every connection failure into "no answer", and the refused
  class needs the raw error. So `matrix.Reach` reuses the parts the design named (`newPin` with
  the address as the URL host, `newHTTPClient`, `pin.detail`, a bound by `Timers.AfterFunc`
  cancelling the request) without a `Client` and without its 429 loop. Each probe builds its
  client and closes its idle connections.
- **The timers are a Config part of their own** (`health.Config.Timers`), because the tick's
  `Clock` has no `AfterFunc`. The certificate's validity is judged by the tick's `Clock.Now`
  through `wallOf`.
- **The probes start after the renewal, not at the top of `terminator()`.** A renewal can restart
  the terminator, and a probe in flight would then see a false failure. A terminator restarted
  this tick is not probed.
- **The addresses and CA come from a new `host.AppliedReach`.** The design did not say where
  they come from.

## Readings where the spec was silent

- **The bound is 5 s per probe.** That is the loopback probe's own bound (hostnet). It is fixed
  in `matrix`, not configurable.
- **`Refresh` probes.** It changes nothing, and a caller that holds the lock (apply) gets the
  terminator's reach on the record. The cost is at most `ReachLimit` added to such a call when an
  address hangs.
- **Two more causes than the spec names:** `it answered HTTP <code>` (the terminator relayed and
  the homeserver answered something other than 200) and `the connection failed` (reset, or no
  route). Both are fixed text.
- **There is no start grace for the terminator.** Only the renewal's own restart is excluded. A
  terminator that systemd restarted by itself between the state read and the probe could show as
  silent for one tick.
- **The tick's own cancellation** (ctx ended) gives the cause `the probe did not finish`, if such
  a record were ever written.

## Findings for the coordinator

1. **Carried, not built:** `teamchat-health.service`'s `IPAddressAllow=localhost` (DESIGN-v2-C-host
   3.11.1) will refuse this probe of every non-loopback listen address. Under the unit, every
   address would then show as `the connection failed` or refused. When that unit is written, its
   filter must also allow the listen addresses (or the effective networks), as X31's as-built
   paragraph says. Nothing here pretends the unit exists.
2. **The real-time limits beneath the injected bound.** `newHTTPClient`'s dial and handshake
   limits are 10 s of real time. In production the 5 s injected bound always fires first. In a
   test that never advances the fake clock, a hanging probe still ends after 10 s of real time.
   That is why the parallelism plants went red only at the test's one-minute bound, not
   promptly. I left it as it is: the member client shares these limits, and changing them is
   outside S13.
3. The health package's doc comment still describes C 3.11.2's steps. X32 is documented on
   `terminator()`, `reaching` and in the spec's as-built paragraph, not in the package doc.

## Gate results

- `./scripts/qa-changed.bash --base feat/crossnet-s12-int`, run in a detached worktree of the
  S13 code commit's tree (`a772a3774`): exit 0, `✓ qa-changed passed: 15 changed file(s)`.
  Its checks: docs (whole gate), go (go-qa, both modules, diff mode), teamchat-keeps-off-v1
  (whole gate), and `test-ccy-container-version-hook.bash` (121 passed). The known `qa-js`
  failure (issue #135) did not appear in this run. This report commit was not run through QA
  again; it changes this file only.

Run by hand on the working tree:

- `go test -race -shuffle=on -count=2` over `health`, `admin/wire`, `host`, `matrix`: passed;
- `golangci-lint run` over the same four: 0 issues;
- the project-rules driver over `./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...`:
  no finding.

## Not done

- The health unit's text (not to be written in this slice; finding 1).
- A test that proves `abandon` cancels the probes still running when gather fails.
- S14 and S15 were not touched.
