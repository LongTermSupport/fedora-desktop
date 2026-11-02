# Defence Before Fix report: n3-probebound (Plan 00209 N3)

Branch `feat/bus-v2-n3-probebound`, from `feat/bus-v2-n3` at `e8f8973ea`. The defence is in
`d63a08fcb`, committed red. The fixes are in `277eb33fd`. This report and the search record are
in the commit after them.

## Specification followed

- Method specification 1.1.0, detector specification 1.0.0 and toolchain specification 0.2.0.
- Source: the cached copies the dbf skill's refresh script listed (plugin 0.1.1), and its
  `project-prompt.md`.

## Toolchain

- Detector: `go/analysis` in the nested module `src-go/qa`, as `src-go/qa/rules`.
- Runner: go-qa-ci's `go-qa` builds `src-go/qa/driver` and runs it as `go vet -vettool` over both
  modules (`scripts/qa-go-qa.bash`, and `qa-changed.bash` in diff mode).
- Single-rule harness: `analysistest`, on the fixtures under `rules/testdata/src`.
- Identifier: every finding prints `(project_realtime_bound)`.
- Resolution: the identifier resolves to the rule's doc string
  (`go vet -vettool=<driver> help project_realtime_bound`) and to its section of
  `CLAUDE/GoProjectRules.md`.
- Listing: the rules are listed in `rules.All()` and in the catalogue table of
  `CLAUDE/GoProjectRules.md`. The exceptions are the allowance tables in `realtimebound.go` and
  `table.go`.
- Manifest declaration: none. The project declares no DBF version and no known-gap record, and I
  added none.
- Calibration assumed: the scope is `internal/teamchat`, decided below and referred.

## Defect and class

- Defect: `TestProbesThatHangEndAtTheirOwnBound` (`host/lxc/probe_test.go`) failed under load:
  "1 of 4 probes hung at once". The run was the stage teamchat-failpoints-test, seed
  1792735716368918250\.
  - The test set `Guests.Bound` to 20 ms. `Probe` used `context.WithTimeout` on real time.
  - One probe's deadline fired before it reached the fake root's hang.
- Class: a bound on real time whose length a test can set. The test sets it short, and the bound
  then races the step the test asserts.
  - The inverted form also belongs to the class: a test select whose real timer is the passing
    case asserts that nothing happened in that time.
- Hazard: a test that flakes under load. In the inverted form, a test that passes without
  exercising its claim.

## Rule

- `project_realtime_bound`: `src-go/qa/rules/realtimebound.go`, documented in
  `CLAUDE/GoProjectRules.md#project_realtime_bound`. It covers `internal/teamchat` and below.
- Production clause: it flags these calls when their bound is not a compile-time constant.
  - `context.WithTimeout`, `WithTimeoutCause`, `WithDeadline` and `WithDeadlineCause`.
  - `time.After`, `AfterFunc`, `NewTimer`, `NewTicker`, `Tick` and `Sleep`.
  - `Timer.Reset` and `Ticker.Reset`.
  - Any of them taken as a value.
- Test clause: in `_test.go` files, a `select` case that receives from `time.After` or from a
  `*time.Timer`'s `C`, and does not fail.
- Not judged:
  - The clock package.
  - The `realClocks` table: six real sides of clock seams, each with its reason.
  - Constant bounds, because a test cannot shorten them.
- Next wider rule considered: the same rule over the whole module. Not built: see the decisions
  referred to the owner.
- Also not built: a rule on tests that assign a short duration to a field. Seam fields of the
  fake clock make that shape a false alarm. With the production clause in place, the only route
  left is a `realClocks` adapter left at its real default, which is recorded as not caught.

## Proof

- Red on the fixtures: `fix/internal/teamchat/realtime` and its neighbours `clock`, `realclock`
  and `elsewheretime`. `TestRealTimeBound` holds every want.
- Red on the real tree at `d63a08fcb`: 5 findings, the same untagged and with `busfailpoints`
  and `tuwunel` (listed in the commit message).
- Green at `277eb33fd`: 0 findings, untagged, `busfailpoints` and `tuwunel`.
- Red/green for the link fix: with `close` reordered to cancel first, the new test failed 3 of
  3 runs (`untracked/scratch/probebound-link-red.log`).

## Independent search

- Dispatched before the rule existed and kept out of `src-go/qa/rules`. Its record is
  `261023-n3-probebound-search.md` in this folder.
- Its (A) list:
  - A1: the original instance.
  - A2 to A4: `guestroot/bound_test.go`, forced by `bound.go:80`.
  - A5: `link/server_test.go:281`.
- Its (C) list: `probe.go:48`, `guestroot/bound.go:80` and `matrix/client.go`.
- Reconciliation:
  - The rule catches A1 to A5 through their production bounds or the test clause.
  - The rule also found `guestbus.go:179`, whose test used 1 ms, and `units.go:467`. The search
    had classed these as const-only or injectable from the grep line, without reading the test.
  - The search found `matrix/client_test.go` `TestRequestTimeLimit` (`m.limit` at 20 ms on the
    real `SystemTimers` adapter), which the rule cannot see. It is fixed, and recorded under
    "Not caught".
- What each technique found that the other could not:
  - Reading found the realClocks-adapter case and the meaning of A2 to A4: the 20 ms literal
    sits in a shared helper.
  - The rule found the two production bounds the reading did not open.

## Sweep and count

- Scope: all first-party Go in `src-go` (both modules), with vendored code and `testdata`
  excluded, run untagged, `busfailpoints` and `tuwunel`.
- The rule judges `internal/teamchat` only. That is the project's decision, made by me in its
  absence, and referred below.
- Count: 5 rule findings, plus 1 search-only instance, for 6 fixed.

## Fixes

- Each instance was examined individually. None received a fix by pattern.
- Every production bound now runs on an injected clock. Production passes `clock.System()`, so
  the bounds are unchanged: ProbeBound is 5 s per probe, callBound 10 s, CallWait 30 s and
  JobWait 10 min.
- `probe.go`:
  - `Guests.Clock` replaces `Bound`. The wiring is `admin/wire/lxc.go`.
  - The bound is `AfterFunc` plus `clock.WithBound`.
  - The cause is `DeadlineExceeded`, so a caller's errors are unchanged.
- `guestroot`: the bound is `pinned.hold`, which is the clock's `Hold`.
- `guestbus` and `units`: each takes a `clock.Clock` field. guestbus keeps the deadline as the
  call's error.
- Tests:
  - Each waits until the work is at its step and has armed its bound, then advances the clock.
  - The tests of probe, guestbus and units also check that nothing fires short of the bound.
  - `link`: the test peeks at the client's end as the cancel reaches the engine.
  - `matrix`: the test fires the member's limit on fake timers after the request has arrived.

## Decisions referred to the owner

- **The scope beyond teamchat (blocking per the conformance review).** I ran the rule once with
  its teamchat scope removed (a scratch driver, reverted). It reported 39 findings over both
  modules, none of them fixed:

  - The nested `qa` module (14): `ansiblesyntax/run.go:194`, `bash/run.go:239`,
    `all/gate.go:20`, `hookhandlers/run.go:308`, `ghscopes/run.go:166`,
    `helpertests/run.go:291`, `patterns/run.go:188`, `changed/run.go:253`, `js/run.go:193`,
    `readywait/run.go:215`, `toolchain/versions.go:72`, `sttrules/run.go:215`,
    `python/run.go:223` and `vmtestmanifest/validator.go:37`. Each bounds a real child process.
  - `internal/teams` (9): `roles/roles.go:363`, `lxcadd/adder.go:769`,
    `lxcadd/checkout.go:77,171`, `shutdown/shutdown.go:89`, `restart/restart.go:86`,
    `winddown/winddown.go:111`, `hot/hot.go:105`, and the test `board/fetch_test.go:62`.
  - Elsewhere in `internal` (16): `tmuxccy/panes.go:126`, `tmuxccy/takeover_test.go:322`,
    `sessprobe/collect.go:381`, `execx/session.go:72`,
    `pingbus/statusline/golden/golden_test.go:342`, `ftpcamera/app.go:107`,
    `playlock/playlock.go:175`, `restore/manifest.go:358`, `stt/app.go:136`,
    `sessions/app.go:385`, `wsitoken/app.go:253`, `wsimedia/app.go:122`, `wsi/app.go:72` (two)
    and `vmtest/app.go:71,235`. Several of these are the real side of a package's own clock
    seam: `ftpcamera`, `playlock`, `sessions`, `stt`, `vmtest:71` and `wsi`.

  These packages are outside teamchat's fake-clock contract, and most bound real processes. I
  have not read each one to confirm it carries no race with an asserted step. Under SPEC §4
  that doubt is not mine to settle.

  - Count fixed outside teamchat: 0. Count remaining: 39.
  - What fixing would take: a `realClocks`-style row for each real clock adapter, and a clock
    seam, or a confirmed exclusion, for each process bound.
  - Nothing is suppressed or baselined.
  - **The rule's branch is not ready to merge** until the owner decides the scope: widen and fix,
    or record the teamchat-only scope as the project's calibration.

- **The search-only instance the rule cannot see** (conformance finding 2). The rule does not
  see a test that shortens a bound running through a `realClocks` adapter left at its real
  default: matrix `TestRequestTimeLimit`.

  - It is fixed, and recorded under "Not caught" in the catalogue.
  - Whether the rule must close it is the owner's decision. A closing rule would forbid
    teamchat tests from constructing a seam's real default, and is not built.

## Permanence

- `RealTimeBound` is in `rules.All()`, so the Go gate runs it as a hard failure.
- `TestAllIsValid` expects 11 rules.
  - At the red commit `d63a08fcb` the rules suite also failed `TestAllIsValid`, which still
    expected 10. The count was corrected in `277eb33fd`.
  - The red proof of the rule itself is `TestRealTimeBound` and the driver over the tree, both of
    which hold at `d63a08fcb`.
- `./scripts/qa-changed.bash --base e8f8973ea`: green (exit 0). The run checked the docs, go, teamchat-keeps-off-v1 and the
  qa-changed suite over the 28 changed files (`untracked/scratch/probebound-qa-changed.log`).

## Conformance review

The `defence-before-fix:conformance-reviewer` returned CHANGES REQUIRED: 1 blocking finding, 4
should-fix and 2 notes.

- It reproduced the red run (5 findings under each tag at `d63a08fcb`) and the green run (0 at
  `277eb33fd`).
- Its full text is in `untracked/agent-reports/auto/`
  (`261023-064409-...conformance-reviewer...md`).

Each finding, and how it was resolved:

1. **Blocking.** The narrowing to teamchat. Not resolved by me. The 39 sites are listed above,
   and the merge decision is referred.
2. **Should-fix.** The search-only miss is not referred. Now referred above.
3. **Should-fix.** The search record is thin: it did not open `probe.go`, `units.go`,
   `matrix/client.go` or the tuwunel files, and it missed `guestbus` and `units`.
   - Recorded, not repeated. The rule's two extra findings were each read and fixed.
   - A second search would cost a fresh agent; the lead can order one.
4. **Should-fix.** The report was uncommitted and its results not filled in. Both are
   resolved in the report commit.
5. **Should-fix.** The fixes are in one commit, so "the original defect fixed last" cannot be
   seen.
   - Accepted as a deviation. The class instances and the original test share one production
     change (`Guests.Clock`), so the original could not be fixed apart from its sweep.
   - The link red proof is in `untracked/scratch/probebound-link-red.log`, which is not
     versioned: 3 of 3 runs failed with `close` reordered to cancel first.
6. **Note.** The constant-bound and ticker narrowings are argued, not searched. Recorded.
7. **Note.** `TestAllIsValid` at the red commit. Recorded under Permanence.

## Toolchain and detector gaps

- The project has the known gap that go-qa-ci's per-analyzer doc pages are not written. The
  catalogue stands in for them.
- The project has no manifest declaration.
- The `project-rules-tagged` stage named in the brief is not on this base (`e8f8973ea`). I ran
  the driver by hand with each tag.

## The original defect

- `TestProbesThatHangEndAtTheirOwnBound` has been rewritten on the fake clock: the clock moves
  only after all n probes are at the hang and have armed their bound.

- Proof:

  - `-race -shuffle=on -count=5 -cpu 1,4` on `host/lxc`: pass, untagged and with
    `busfailpoints`.
  - The original seed, `go test -race -shuffle=1792735716368918250 -count=2 -tags busfailpoints ./internal/teamchat/...`: pass, all 52 packages with tests.

- Coverage of the touched packages, all over the 90% floor:

  | Package    | Coverage |
  | ---------- | -------- |
  | lxc        | 91.9%    |
  | guestroot  | 95.9%    |
  | guestbus   | 97.9%    |
  | units      | 98.5%    |
  | link       | 91.3%    |
  | matrix     | 98.9%    |
  | admin/wire | 91.5%    |
  | qa/rules   | 94.9%    |

- golangci-lint: 0 issues, untagged, with `busfailpoints` and with `tuwunel` over
  `internal/teamchat`, and over `qa/rules`.
