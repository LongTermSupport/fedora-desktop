# n3e: merge of feat/bus-v2-n3 into feat/bus-v2-n3e, finished

Continues `261023-n3e-merge-n3.md` (merge `70bef3169`, goroutine-end fix `988666bc2`, handoff
`f284172f0`). Worktree `/workspace/untracked/worktrees/bus-v2-n3e`, branch `feat/bus-v2-n3e`.

## Commits

- `267cacdba`: the hook view's ping fixture carries its verb (n3d-D30); B's store shows it.
- `cc98cda0b`: `project_fakeclock_count` widened to every count of a fake clock's waiters
  (the defence red).
- `32c0e2cf2`: the TIMEOUT tests name their timer; `clock.(*Fake).Armed` (the green).
- `15b7d0b69`: a test that a batch's answers reach the delivery's `answered:` lines.
- `77d8cde70`: the kit's skill names status's `queued` records (n3d-D21); SPEC-n3 LN5 too.
- `86c770a33`: the conformance review's doc findings (the interface route, a reason per
  allowance row).

## 1. The four engine test failures

**`TestTheHookViewCountsWhatIsNotPushed`.** n3d's schema is right. n3d-D30 (a reviewed row of
the n3d brief) gives every pending and ledger entry a `verb`: the ping's own for an item
`ping`, null otherwise, and the strict decoder (`state.validVerb`) refuses any other pairing.
n3e's fixture persisted a `ping` item whose verdict named no verb. SPEC-n3 holds only n3d-D31
to D34, and B's store section had not been updated for D30. The fixture's ping now carries
`proto.VerbCheck` in its verdict, which Persist copies into the entry. B's store example and
its pending notes now show `verb` and the pairing.

**The three TIMEOUT tests.** The fix and the DBF record are below.

## 2. answeredLines

`TestABatchsAnswersReachTheDelivery` (`engine/deliver_test.go`) persists a batch with one item
and three answers to one outbox ping:

- an ack, which owes a line;
- an on-time `done` to an addressee whose TIMEOUT was made, which owes a line marked "(after its
  TIMEOUT)";
- an on-time `done` with no TIMEOUT, which owes no line.

The test asserts the delivery's `answered:` lines exactly. I checked it against two mutants:

- Persist hands `answeredLines` an empty batch: it fails with no lines.
- The "already reported" condition is dropped: it fails with an extra line.

The batch's item makes the delivery go either way, so each failure is an assertion, not a
hang. Without the item, the first mutant hung on the next delivery.

## 3. qa-changed

The first run (on `15b7d0b69`) failed only go-qa's test runner. Every detector passed,
`analyzers` included. The failure was kit's `TestTheSkillNamesWhatTheBinaryHas`: "SKILL.md
misses the record kind queued". n3e-11's skill predates n3d's `queued` record (n3d-D21).
`77d8cde70` adds it to the skill's status line and to SPEC-n3 LN5. Log:
`/workspace/untracked/scratch/n3e-merge-b-qa1.log`.

The second run (`./scripts/qa-changed.bash --base 7730f8d5a` on `77d8cde70`, log
`n3e-merge-b-qa2.log`) passed every check except one:

- Passed: go-qa over both modules (format, detectors, analyzers, failpoints lint and vet,
  vuln, mod, build, test), docs, bash-limits, teamchat-keeps-off-v1, hidden-bindings,
  go-deps, and 38 shell suites.
- Failed: `test-wsi-stop-grace.bash`, only in
  `test_a_running_job_is_marked_for_the_panel_and_unmarked_when_it_ends` ("a failed job is
  unmarked too"). This is the known flake.

Run alone, the suite passed: 121 PASS, exit 0 (`n3e-merge-b-wsi.log`).

`86c770a33` changed doc text only (an analyzer doc string and GoProjectRules) after the run
started. Its `src-go/qa` was vetted and `go test ./rules` passed. host/lxc's
`TestProbesThatHangEndAtTheirOwnBound` did not flake in either run.

## 4. Coverage

I ran `go test -count=1 -cover` over every Go package the diff from `7730f8d5a` touches (log
`n3e-merge-b-cover.log`). Every one is at or above the 90% floor.

- **The lowest:**
  - `member/keeper` 90.8%
  - `reexec` 90.5%
  - `admin/wire` 91.5%
  - `apply` 91.8%
  - `host/lxc` 91.9%
  - `member/link` 92.3%
  - `member/engine` 92.7%
  - `member/client` 92.8%
- **The rest:** 93% to 100%, `qa/rules` at 93.9%.
- **`failpoint`:** has no statements in the untagged build.

## 5. Conformance review

I ran `defence-before-fix:conformance-reviewer` over both remediations. It reproduced the reds
and greens itself:

- `project_goroutine_end`: 4 findings at `70bef3169` and 0 at the end.
- `project_fakeclock_count`: 14 at `cc98cda0b` and 0 at the end, in every build tag and in the
  nested `qa` module.

**project_goroutine_end (988666bc2): conforms with findings.** All three concern the record,
not the code.

- The record in `261023-n3e-merge-n3.md` was an unfinished handoff: no conformance pass, no
  coverage, no sweep scope stated, and no statement of how the fixes were examined.
- The red is a merge commit that carries other changes. That is acceptable because the rule
  already existed.
- The wider form was referred with no count.

These are resolved in the goroutine_end section at the end of this report.

**project_fakeclock_count (cc98cda0b, 32c0e2cf2): conforms with findings.**

- **Allowance rows.** It judged the rows a narrowing within my authority, conditionally: the
  searcher had written that the client checks "would turn false red" if the client later armed
  a timer it stops late.

  That is the claim working, not a hazard. LN3 says the client never waits, so a timer still
  armed when the verb returns is a wait the claim forbids. A timer stopped before it returns is
  not counted. So the doubt does not apply.

  The rows are still exceptions recorded in `table.go`, so they are listed for the owner under
  "Decisions referred".

- **Per-row reasons.** GoProjectRules gave no reason for each row. `86c770a33` adds one per
  row. The fixture `countallowed` proves a row reaches only its own function.

- **Evasion.** A fake held as an interface with `Waiters() int` and no `Advance` is not
  caught. None exists in the tree. `86c770a33` lists it under "Not caught" in the analyzer doc
  and in GoProjectRules, and it is referred below.

- **Report.** The placeholders are now filled.

- **The next wider rule** is named and referred, with the ~25 `AwaitDue` calls unchecked. The
  reviewer called this a gap, not a conformance failure.

## DBF: a test that judges a fake clock's timers by a count of its waiters

### Specification followed

Method specification 1.1.0, the cached copy (refresh script: `SPEC.md cached 1.1.0`,
`DETECTOR-SPEC.md cached 1.0.0`, `TOOLING-SPEC.md cached 0.2.0`).

### Toolchain

The detector is `go/analysis` analyzers in `src-go/qa/rules`, run by go-qa-ci's `analyzers`
stage through `scripts/qa-go-qa.bash` (in `qa-changed` and `qa-all`).

- **Single-rule harness:** `analysistest` (`go test ./rules -run TestFakeClockCount` in
  `src-go/qa`).
- **Listing and resolution:** `go vet -vettool=<driver> help` and
  `help project_fakeclock_count`, plus the catalogue `CLAUDE/GoProjectRules.md`.
- **Recorded exceptions:** the allowance tables in `src-go/qa/rules/table.go`.

There is no manifest declaration: a Go module has no field for one, and none was found.

The project's calibration is GoProjectRules' existing one: first-party Go in both modules,
`vendor/` excluded, all build tags.

### Defect and class

**Originating instance.** Three n3d tests went red on correct code at the merge:

- `engine/outbox_test.go:125`, `waiters = %d, want the one TIMEOUT timer`
  (`TestSD4_1OneTimeoutPerSilentAddresseePastTheGrace`);
- `engine/timeout_test.go:23` (`TestSD4_6OneTimerAndOnlyWhenOwed`);
- `engine/timeout_test.go:56` (`TestSD4_6AWallStepBackReArmsWithoutWriting`).

**Class:** a test that judges a fake clock's timers by the number of its armed waiters.

**Hazard:** the fake is shared with all the code under test, so any other timer changes the
count. The assertion then fails on correct code. It also passes when the timer it means is
missing and another timer makes up the number. Here, n3e's delivery loop, proof and
status.json timers share the TIMEOUT timer's fake.

### Rule

The rule is `project_fakeclock_count`, widened. This is the "next wider rule" its own
catalogue entry named.

What it flags, as before:

- a wait by count (a context and an integer), anywhere;
- a count polled in a `for` or a returned predicate, anywhere.

What it now also flags: every other use of the no-argument `int` method of a type with
`Advance(time.Duration)` outside the package that declares it. That covers an assertion, a
variable, a method value, a helper argument and a switch. The new message is "counts the fake
clock's waiters (Waiters): name the timer by when it is due (AwaitDue, Armed)". Its
documentation is the analyzer's doc string and `CLAUDE/GoProjectRules.md#project_fakeclock_count`.

Excluded as hazard-free:

- **The fake's own package**, its external tests included. Nothing else arms a timer on the
  fake there.

- **Four rows of `waiterCountAllowances`** (package, function, reason). In each, the claim is
  that nothing at all is armed, and the code under test is the clock's only user, so every
  timer is the one the test means:

  - the client's `world.noLink` and `TestASendNeverRetries`;
  - inbox's `TestOneDeliveryRecordsTheAuthFrameThenTheContent`;
  - faketranscript's `TestRecordNeverWritesNothing`.

  A row excuses an assertion only, never a wait. The independent searcher judged the same four
  not hazardous, independently.

**Next wider rule considered:** a timer named by its due time must be the only waiter due at
that instant. This would close `AwaitDue`/`Armed` answering a different timer of the same
length. It is a fact of a run, not a syntax shape, so it belongs in the fake, not in an
analyzer. The searcher listed about 25 `AwaitDue` calls and did not check them for collisions.
This is referred to the owner below.

### Proof

The fixture is red, kept as the rule's test (`src-go/qa/rules/testdata/src/fix/internal`):

- `badcount/assert_test.go`: the originating assertion and its evasions.
- The `armed := clk.Waiters()` snapshot in `badcount/bad_test.go`.
- `fakeclock/fake_test.go` and `fakeclock/external_test.go`: clean.
- `countallowed/a_test.go`: a row clean; a poll inside it and its neighbour flagged.

`go test ./rules ./driver` passes in `src-go/qa`.

On the real tree the rule is red at `cc98cda0b`, a commit of its own before any fix, with the
same result untagged and with `busfailpoints`, `tuwunel` and `teamchatrelease`. The command was
`go vet -tags <t> -vettool=<driver> -project_fakeclock_count ./...` in `src-go`. It gave 14
findings: 7 assertions, each with its message argument, at
`outbox_test.go:125,126,144,145,447,448` and `timeout_test.go:22,23,27,28,34,35,55,56`. There
were none in the nested `qa` module. Log: `/workspace/untracked/scratch/n3e-merge-b-sweep.log`.

### Independent search

The `defence-before-fix:independent-searcher` was dispatched before the rule was changed and
kept out of `src-go/qa/rules`. Its notes are in
`untracked/scratch/n3e-merge-b-search-waitercount.md` (not tracked).

It found the same 11 count reads outside the clock package:

- 7 that carry the hazard, the rule's 7;
- 4 it judged not hazardous, the four allowance rows.

The clock package's own five reads are clean. There are no waits on a count.

- **What text search found** and reading could not have checked: every count read; the count
  method has one name.
- **What reading found** and text search could not:
  - which timers share each fake;
  - that `outbox_test.go:447` passes only because the removal also ends the status timer;
  - that the four "nothing armed" claims are genuine.

The search found nothing the rule missed, so the rule was not widened further.

It also listed non-count inferences that are related but outside the class: `advanceTo` after
`heldWith` without awaiting the TIMEOUT timer, and the blind advances at `recv_test.go:391` and
`forge_test.go:324`. They are recorded below.

### Sweep and count

The scope is both modules, every build tag, with `vendor/` and the rule's own testdata
excluded. That is the project's existing calibration. The count is 7 instances (14 findings).
The narrowing is the fake's own package and the four rows above, each with its reason.

### Fixes

All 7 were examined individually (`32c0e2cf2`):

- `w.timeoutArmed()` reads the engine's one TIMEOUT timer field on the state goroutine.
- `w.dueAt(ms)` asks the clock, through the new `clock.(*Fake).Armed(d)` (tested in
  `clock_test.go`), whether a waiter is due at the TIMEOUT's wall time.
- "One timer for three pings" is now: armed for the first ping's due, and not for the other
  two.
- The removal test also checks that the timer was armed before the removal.

With the timer armed 1 ms late, all four tests fail. Nothing was suppressed. After the fix
there are 0 findings in every build.

### Decisions referred to the owner

1. **The next wider rule** (a due time naming exactly one waiter) is not built. It would need
   `AwaitDue` and `Armed` to refuse an ambiguous due. Fixing it means a check in the fake,
   then running every test that uses `AwaitDue`. There are 0 known instances: no instance is
   known, but nothing has checked the ~25 calls.

2. **The four allowance rows** are a narrowing I am confident of (see Rule). They are listed
   here because they are recorded exceptions in `table.go`.

3. **Related shapes outside the class**, from the search:

   - an advance that assumes a timer is armed without awaiting it: `advanceTo` after
     `heldWith` in the TIMEOUT tests;
   - the blind advances at `engine/recv_test.go:391` and `teamsync/forge_test.go:324`.

   These are not fixed here.

4. **The interface route.** A fake held as an interface that has the count method and no
   `Advance` is not caught. There are 0 instances. Catching it would take flagging the
   conversion of a fake clock to such an interface. It is listed under "Not caught" for the
   owner to accept or have built.

The rule is merged at full width. No instance remains.

### Permanence

The rule is in `rules.All()`, so go-qa's `analyzers` stage runs it as a blocking step in
`qa-changed` and `qa-all`. The green run through the project's own entry point is in section 3.

### Toolchain and detector gaps

These are as GoProjectRules records them, unchanged:

- go-qa's analyzers stage judges only the untagged build. The tagged builds were swept by
  hand, as above.
- go-qa-ci expects one documentation page per analyzer, and this catalogue stands in for them.

### The original defect

The three red tests (and the passing-by-coincidence removal test) now pass: `go test -race`
over `engine` and `clock`. Logs: `/workspace/untracked/scratch/n3e-merge-b-engine1.log`,
`-engine2.log`.

## DBF: project_goroutine_end ×4 (988666bc2)

The record is in `261023-n3e-merge-n3.md`, and its conformance verdict is in section 5. This
section completes it.

- **Red.** The red is the merge commit `70bef3169`. The rule predates the merge, so the red is
  the rule run over the merged tree before any fix, and the commit carries the rest of the
  merge.

- **Sweep scope.** Both modules, untagged and with `busfailpoints`, `tuwunel` and
  `teamchatrelease`, with `vendor/` and the rule's testdata excluded. It gave 4 findings, the
  same in every build, and none in the nested `qa` module. The reviewer reproduced the 4 and
  the 0.

- **Fixes.** Each of the 4 was examined on its own; none received a fix by pattern. Each fix
  is named in the `988666bc2` message:

  - `engine/teams.go:241`
  - `guestroot/bound.go:93`
  - `teamsync/forge.go:139`
  - `teamsync/forge_test.go:269`

- **Wider form, referred with its count.** The wider form is a goroutine whose end is signalled
  but not awaited on every path of its owner. The search lists 8 such goroutines, none fixed
  here:

  - in production code: `guestroot/bound.go:76`, which leaves a stalled guest call's goroutine
    by design, `host/lxc/probe.go:55`, `forge/token.go:102`, `engine/teams.go:235` and
    `reexec/reexec.go:219`;
  - test readers in `wiring/e2e_world_test.go:449-450`, `link/server_test.go:119` and
    `client/client_test.go:193`.

  Fixing them takes the call-graph rule GoProjectRules names as "next wider", or a review of
  each owner's paths. That is the owner's call.
