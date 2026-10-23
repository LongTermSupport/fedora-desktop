# n3c-owed: the TestOwedNotice flake (Defence Before Fix report)

Branch `feat/bus-v2-n3c-owed`, from `feat/bus-v2-n3c` at `0e9e89277`.

| Commit      | What                                                                                    |
| ----------- | --------------------------------------------------------------------------------------- |
| `f46e42d1c` | harness guard, red: `TestLoggedWaitsForTheTurnThatWroteTheLine` against the old barrier |
| `116646072` | harness guard, green: `Config.turned`, the turn-end copy; fixes both instances          |
| `f3d7e247a` | rule `project_keeper_log_barrier`, red: fixture, and five uses on the real tree         |
| `a60cd3df6` | rule green: every keeper.log read through `w.keeperLog`; GoProjectRules.md section      |
| (tip)       | this report                                                                             |

## Specification followed

Defence Before Fix method specification 1.1.0, detector specification 1.0.0, toolchain
specification 0.2.0. The refresh script read all three from its cache
(`~/.cache/defence-before-fix/spec/`), and the project prompt was generated from the 1.1.0
copy.

## Toolchain

The detector is the project's own `go/analysis` rules in `src-go/qa/rules`. go-qa runs them as
the go vet tool built from `src-go/qa/driver`, through `scripts/qa-go-qa.bash` and, in diff
mode, through `scripts/qa-changed.bash`. Each rule has an `analysistest` test of its own, which
is the single-rule harness. `CLAUDE/GoProjectRules.md` is the catalogue, and an identifier
resolves to its heading there. Neither `src-go/go.mod` nor `qa/go-qa.toml` has a DBF manifest
declaration or a known-gap record, and this work left that untouched. The project records no
calibration. I assumed the catalogue's own convention: every rule is proven red on a fixture
and on the real tree, then green.

## Defect and class

**Originating instance.** `src-go/internal/teamchat/member/keeper/membership_test.go:189-190`,
in `TestOwedNotice`.

- The test runs `w.logged("the leave notice was not posted in time")`, then asserts
  `!w.held(alpha)`.
- The owed notice's timer turn (`seats.go:363-367`) writes that line, and only then calls
  `k.drop(s)`, which closes the seat lock.
- `w.logged` polled keeper.log every 5 ms. When the loop goroutine was preempted between the
  write and the close, the test saw the line and found the seat still held.

QA never captured the failure message. Planting a 50 ms pause after the log line gives
`membership_test.go:191: the seat is held after the notice's 30 s` on every run. That is a
fast assertion failure, which fits the 0.33 s the QA run reported (a hang would have run to
the barrier's bound).

**Class.** A test barrier released partway through the unit of work it waits for: the code
under test emits the awaited observable (a keeper.log line) during a loop turn, and produces
the effect the test then asserts later in the same turn.

**Hazard.** The barrier can release before the effect lands. A positive assertion then flakes
under load, and a negative assertion passes even when the effect is wrong.

## Rule

The defence has two halves. The guard removes the gap; the rule keeps every test on the guard.

**1. Harness guard.**

- The keeper calls `Config.turned(run)` at the end of the launch steps, of the link's start,
  and of each loop turn. The field is unexported, so only the keeper's own tests can set it;
  in production it is nil.
- The harness (`world.turned`) keeps keeper.log as each call left it.
- `w.keeperLog(run)` is the one reader of keeper.log. Before the loop, while the launch steps
  run in order (their progress lines, such as "waiting for it to end", are deliberate mid-step
  barriers), it reads the file. While the loop runs it reads the kept copy. Once the keeper has
  ended it reads the file again.
- `w.logged`, `w.seen`, `w.log` and `w.logOf` all read through it.

A line therefore shows only after its whole turn has run.

**2. Rule `project_keeper_log_barrier`** (`src-go/qa/rules/keeperlogbarrier.go`).

- It flags any reference to `paths.Run`'s `KeeperLog` in the keeper's `_test.go` files: a call,
  a method value, a method expression, or a use inside a goroutine.
- It allows the functions in the `keeperLogReaders` table, each with a reason:
  `world.keeperLog`, `world.turned`, and `world.runs` (existence only).
- Message: `keeper.log read outside the harness's barrier (project_keeper_log_barrier): use w.logged, w.log or w.logOf`.
- Documentation: the rule's doc string (`go vet -vettool=<driver> help project_keeper_log_barrier`), and the catalogue row and section `### project_keeper_log_barrier`
  in `CLAUDE/GoProjectRules.md`.

**Width.** The rule covers every keeper test, where the turn-end seam exists. It deliberately
excludes three things:

- The keeper's own code, which writes the log.
- Other files of the run (`link.log`, `keeper.json`).
- Other packages' tests. Those have no turn-end seam, and their barriers already wait for the
  end of a unit of work: engine `until` runs on its state goroutine, link has `settled` and
  `await`, and teamsync has `poll`. The wiring end-to-end tests poll the effect they assert
  (see the independent search below).

**The next wider rule, considered and not built.** Write keeper.log one turn at a time, so the
same guarantee holds for every reader of it, including the wiring end-to-end tests that read a
real keeper's log. I did not build it because it costs production diagnostics: a turn that
crashes would lose its lines, including the lines before a `busfailpoints` crash and before a
panic, which are the lines a crash investigation needs. No instance exists outside the keeper
today, but the hazard could arise there, so this decision goes to the owner (see below).

## Proof

- **Guard, red** at `f46e42d1c`. `TestLoggedWaitsForTheTurnThatWroteTheLine`
  (`barrier_test.go`) calls the barrier's predicate from inside the ready turn, from CMD's
  start, which that turn makes after it logs "the link is ready". Running
  `go test -race -run TestLoggedWaitsForTheTurnThatWroteTheLine -count=5` printed
  `barrier_test.go:27: logged saw the ready line while its turn was still starting CMD` on
  5 of 5 runs. The conformance reviewer reproduced this independently, 3 of 3.

- **Guard, the real instances.** I planted a 50 ms sleep after the log line in the owed timer
  (`seats.go`) and in `fail` (`loop.go`), uncommitted and removed afterwards. With the old
  harness and `-count=2`, both tests failed on every run:

  - `membership_test.go:191: the seat is held after the notice's 30 s`
  - `lifecycle_test.go:181: a failed link's seat is still held`

  With the new harness and `-count=5`, both pass. The reviewer reproduced the green side.

- **Rule, red** at `f3d7e247a`.

  - The fixture `qa/rules/testdata/src/fix/internal/teamchat/member/keeper` carries one `want`
    per flagged form, and clean allowed readers, keeper code and link.log beside them.
    `member/elsewhere` is not judged.
  - On the real tree, `go vet -vettool=<driver> ./internal/teamchat/...` reported five uses:
    `harness_test.go:302` (runDir), `:320` (log), `:708` (seen), `:778` (logOf), and
    `keeper_test.go:94`.

- **Rule, green** at `a60cd3df6`: no findings over `./internal/teamchat/...`, untagged and
  with `busfailpoints`.

## Independent search

**Searcher.** I dispatched `defence-before-fix:independent-searcher` before any defence
existed, keeping it out of `src-go/qa`. Its scope was every `*_test.go` under
`src-go/internal/teamchat`. It searched for:

- the helpers `logged|eventually|waitFor|await*|wait*|poll*|until|settled|within` and their
  callers;
- log, status and notice polls;
- `time.Sleep` and `time.After` waits;
- the barrier before each `held` or store assertion.

What each technique found:

- **Text search** found every barrier helper and call site. Reading alone could not have
  enumerated them.
- **Reading** found the order of emission against effect, which text search cannot see.
  Reading `fail()` was the only way to find the second instance: keeper.json is written before
  the log line, and the locks are closed after it.

The searcher returned two instances and two weaker cases:

1. The originating instance.
2. `lifecycle_test.go:176-180`, in `TestLinkRestartsWithBackoff`. The test waits on
   `w.logged("the link failed: crash-loop")`, then asserts `!w.held(alpha)`. `fail` logs at
   `loop.go:431`, before it closes the locks.
3. Wiring `awaitLinkKilled`, then `oneLink()`. I examined this myself and found it is not an
   instance: each `oneLink()` follows a step that waits for the next link (`seatedIn`,
   `recvAny`, or the store-gone poll).
4. Wiring `awaitNotice`, then a count of 1. Also not an instance: the count can only miss a
   duplicate posted later by another process, which is a limit of any check that something
   happens "at most once so far", not an assertion that depends on the rest of a turn.

**Cleared.** The searcher cleared the other keeper barriers by reading them against the code.
A named sample:

- "seat alpha released" (`membership_test.go:153`, `:208`, `:227`, `:252`, `:295`): `drop`
  logs it last.
- "its leave notice is owed" (`:148`, `:176`): a positive held check that only a later turn
  undoes.
- "the link failed: config" (`lifecycle_test.go:196`): keeper.json is written first.
- "the link is ready" (`harness_test.go` `ready`): every caller next makes a blocking receive.

**What the search showed about the defence.** The guard covers instances 1 and 2 through the
shared reader. The search also showed that a test could read keeper.log past the guard, as
`w.log` and `w.logOf` did. That is why I added the rule: the guard alone was not blocking for
the class.

**A third technique**, a module-wide text search, which the reviewer asked for. I searched
`src-go` outside teamchat for log or screen polls and sleep loops in tests: tmuxccy, execx,
qa/internal/proc and wsi. Every one polls the effect it asserts: a pid file, the process gone,
a stub's started file. One looked like a barrier on the screen text: the tmuxccy screen wait
before its record-removed check (`start_test.go:197-207`). It is not an instance, because the
trampoline (`registry.Trampoline`) runs `rm -f` on the record before it prints the ended text.

## Sweep and count

**Scope:** first-party Go tests in `src-go`, vendored code excluded. The project had no recorded
scope, so this is my decision.

- **Mid-turn barrier with a later assertion:** 2 instances, both in the keeper.
- **Rule, reads past the reader:** 5 uses, all in the keeper harness and `keeper_test.go`.

Nothing was narrowed.

## Fixes

- **Instances 1 and 2:** fixed by the guard, through the shared reader. Each was examined
  individually and shown red, then green, with the plants. Neither test was edited.
- **The 38 other `w.logged` sites:** received the guard by pattern. The sample named above was
  examined individually.
- **The five rule uses**, each examined:
  - `log` and `logOf` now read through `keeperLog`.
  - `seen` reads through `keeperLog`.
  - `runDir` uses `runs`, which checks existence only.
  - The ending-session test's wait for its run uses `runs`.

Nothing was suppressed. No timeout was lengthened, and no sleep was added to committed code.

## Decisions referred to the owner

**The next wider rule: keeper.log written one turn at a time.**

- Count fixed: 2 instances and 5 uses. Count remaining: 0 known.
- Why it is unbuilt: its cost to crash diagnostics, not because the hazard cannot arise.
- What it would cover: every reader of keeper.log, including the wiring end-to-end tests. Those
  read a real keeper's `keeper.log` through their session's `log("keeper.log")` (for example
  `linkKilled`), outside this rule's scope and without a turn to wait on.
- What fixing it would take: buffer `logf` per loop turn and flush when the turn ends, flushing
  also before `failpoint.Hit` and in a deferred recover, so that a crash keeps its lines.
- What to try first: a per-turn buffer with those two flush points, proven by the wiring
  failpoint rows still finding their lines.

The rule is merged at full width for its scope. No exception is recorded.

## Permanence

The rule is in `rules.All()`, which go-qa's driver registers. It therefore blocks in
`qa-changed` and `qa-all`, untagged and in the `teamchat-failpoints` stages.
`TestLoggedWaitsForTheTurnThatWroteTheLine` runs in every go-qa test run.
`./scripts/qa-changed.bash` passed at `116646072` and at `a60cd3df6`, on the docs,
go-qa (both modules) and teamchat-keeps-off-v1 gates.

## Toolchain and detector gaps

- There is no listing command for the project rules. `CLAUDE/GoProjectRules.md` is the
  hand-kept listing, and `go vet -vettool=<driver> help` lists them from the live driver.
- There is no manifest declaration and no known-gap record.

## The original defect

The guard is the conventional fix. `TestOwedNotice` is unchanged and is the test that
reproduces the defect: with the pause planted, it fails every time on the old harness and
passes on the new one.

**Load runs** at `a60cd3df6`. The keeper package ran shuffled (`-race -shuffle=on`) while a
loop of `go test -race ./internal/teamchat/...` loaded the CPUs:

- untagged, `-count=30 -cpu 1,2,4` (90 runs of every test): ok, in 426 s, all under load
- `busfailpoints`, `-count=15 -cpu 1,2,4` (45 runs): ok, in 163 s. The eight-pass load loop
  ended during this run, so only part of it was under load.
- the three tests concerned, `-count=500 -cpu 1,2,8` (1500 runs each): ok, in 236 s, after the
  load had ended

An earlier pair of the same runs, started at `116646072` while I was editing the tree, was also
ok. Because the tree changed during it, it is not counted as evidence. The original flake was never reproduced without the plant. It had
not reproduced in the earlier 2700 isolated runs and 31 whole-package runs either. The planted
pause is what makes it fail every time.

**Other gates:**

- `golangci-lint run ./internal/teamchat/...`: 0 issues untagged, with `tuwunel` and with
  `busfailpoints`. `golangci-lint run ./rules/` in `qa/`: 0 issues.
- Coverage of the keeper package: 90.8% untagged, 90.7% with `busfailpoints`.
- `qa/rules` tests: pass.
