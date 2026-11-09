# n3e-13, PT1 slice 4: the runner built; P2, P4 and P5 reached by real runs and shown red; one open finding (seed 57)

Plan 00209 Phase 3, Task 3.2. Base fast-forwarded to `feat/bus-v2-n3e-wire` (head `a176dd95a`, "PT1 slice 3's records say how
much of the manual target is broken"; `pt1_run_test.go` present). This agent's context passed its budget during (B), so
the commits are at a clean point; section 6 says what remains. No production file is changed: each planted defect was put in,
run and reverted, and `git diff` of production files is empty after each.

## 1. What is built

All in `src-go/internal/teamchat/member/wiring/`, `busfailpoints`:

- **(A) the runner for `remove`, `end` and `die`** (`pt1_end_test.go`, commit `e676a1400`). Each runs at rest: the schedule
  so far is drained with `rest` (the old drain's first three steps), because a removal drops pending, handed and awaiting
  items by design. `remove` writes the releasing generation, waits for the keeper's "leave notice is owed" line, finds the
  store absent (a store back at once is `pt1Seen.storeBack`, a P4 finding), records `atRemoval` and `want = left released`.
  A removed seat's later posts go to `afterRemoval`. `leaveDone` waits for the keeper's "seat alpha released" line (written
  after both posts of the leave), the notice in main, then asks one recv (the verb that could hand a removed team's item
  out). `end` (SIGTERM of the keeper, `want = left other`) and `die` (SIGKILL of CMD, `want = disconnected`) wait for the
  keeper to exit, every link to be gone, and the notice. The transcript is read from its file, whole lines only
  (`session.transcript`, set in `claude()`), because the fake Claude Code is gone after an ending; the links are read before
  the end because the runtime directory goes with it.
- **(B) `recv-fail`, the P2 row** (`pt1_redeliver_test.go`, committed with (C) below). `recvFail`
  runs `cli.Run` in process with a stdout that keeps the bytes and errors, so the client never acks and the link returns the
  items marked repeat; each id is recorded in `marked` with its transcript copy count. `TestPT1Redelivery` is the
  deterministic row: a first item delivered, a second posted and waited for until the link owes it, a failing recv, then a
  wait for the redelivered copy (about 2.5 s); it fails if no item was handed out in 3 tries. `pt1Seated` runs the
  SessionStart hook again until the link answers it (see section 4, the load failure it fixes).
- **(C)** `pt1Unbuilt` and the loud failure are gone; the 22 fixed seeds draw every kind (`TestPT1FixedSeedsDrawEveryOperation`
  asserts it, and that the first eight, the point rows', draw no removal or ending).

## 2. Findings from the previous report, confirmed or corrected by the rows

1. **Finding 1 (no second transcript copy from the fake): stands.** The redelivery row exists because of it.
2. **Finding 2 (the failing recv): confirmed**, in process, with no production change and no `/dev/full`. Corrected detail:
   most drawn `recv-fail` find nothing pending (a recv a moment earlier drained it), so seeds hand items out only in
   some runs (3 of 30 random seeds did); `TestPT1Redelivery` is the row that always does.
3. **Finding 3 (P5 needs a second post with another id): confirmed**, shown red below.
4. **Finding 4 (P4 hard to plant): corrected.** A 6-line plant works: `Engine.Remove` returning at once after
   `link-in-remove` with `removed` reset, so the team goroutine keeps running. What fires is not the transcript copy but a
   post-removal item handed to the recv that `leaveDone` asks, the store written again, and a second goodbye.
5. **Finding 5 (P4's window has no dwell): stands.** The window is the leave's own waits; no sleep was added.
6. **Finding 6 (transcript from its file after an end): confirmed.**
   Also confirmed: an end by SIGTERM in `--claude` mode posts `left other`; CMD's SIGKILL posts `disconnected`; a removed
   seat's later end posts nothing.

## 3. Red and green (each planted defect put in, run, reverted; production tree clean after, `git status` shows test files only)

| Property   | Planted defect                                                                                                                                                                     | Rows red                                                                                                                                                          |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| test first | before the runner: seed 109 failed with "draws remove#6, which the runner does not run yet"; before the seeds: `TestPT1FixedSeedsDrawEveryOperation` failed "never draw recv-fail" | n/a                                                                                                                                                               |
| P5         | `ends.Leave`: a second `post` with `txn+"x"`                                                                                                                                       | seeds 42, 109, 150: `P5: 2 goodbye notices ... map[left released:2]`                                                                                              |
| P5         | `ends.send`: a second `post` with `*end.Txn+"x"`                                                                                                                                   | seeds 35, 11, 24: `2 disconnected notices` and `left other:2`                                                                                                     |
| P4         | `Engine.Remove` returns at once after `LinkInRemove`, `removed` reset                                                                                                              | seeds 42, 109, 150: item posted after the removal reached recv; store written again; seed 109 also a second goodbye                                               |
| P2         | `proto/envelope.go` `closingLines`: `if e.Repeat && e.Kind == "mutation-never"`                                                                                                    | `TestPT1Redelivery` and seed 57: `P2: ... returned to pending marked repeat and its copy 1 ... carries no repeat line`; full `-run TestPT1` ok before, FAIL after |
| P2         | `engine/recv.go` `settleStore`: `it.Handed, it.Repeat = nil, false` (a `Return` that does not mark)                                                                                | the same two rows                                                                                                                                                 |

Before the plants, each row was green on the unmodified tree (full `-run TestPT1`, `-race -shuffle=on -count=1`: ok 57.1 s
after (A), ok 59.5 s after (B)).

## 4. Findings of this slice

- **Seed 57 failed once under load (OPEN, a finding, not hidden).** Four copies of the whole PT1 suite at once (the compiled
  test binary): three passed, one failed `TestPT1Seeds/seed-57` with `P2: <id> was returned to pending marked repeat and its copy 1 in the transcript carries no repeat line`, schedule `[say#0 say#1 say#2 recv-fail#3 recv#4 peer#5 refuse#6 recv#7 recv-fail#8 peer#9 say#10 peer#11]`. 25 unloaded replays (`-pt1.seed=57 -count=25`) all passed. Two readings, not
  separated: (a) a production race, an item both handed out and delivered by the socket, whose first delivery rightly has no
  repeat; (b) the harness samples the copy count (`marked`) before a delivery already in flight lands, so the first, rightly
  unmarked, copy is read as after the mark. The next slice should settle it from the failing run's logs (a run that does not
  end keeps `keeper.log` and `link.log`). The seed stays in `pt1Fixed`; a seed that fails is a finding, not a list to edit.
- **A load failure in the harness, fixed.** The first `qa-changed` on (B) failed in the tagged stage under the load of the
  whole teamchat tree at `-count=2` (628 s for this package): `TestPT1Points/after-auth` timed out at 30 s waiting for its
  failpoint. The keeper log showed the SessionStart hook answered its own recovery line ("teamchat is starting or restarting")
  and the link log "tell the engine the session: context canceled": the link did not answer the hook within its deadline, so the
  engine was never told a session and delivered nothing, and the row waited for a delivery for ever. Claude Code's next hook
  would tell it; `pt1Seated` runs SessionStart again until the link answers. The older rows' `claudeSeated` has the same
  exposure and was not changed (it is shared with rows that assert the exact answer).
- Seed 5 hands out three items in a run with the inbox refused; seed 57 and seed 67 hand out one; neither redelivers by the
  socket there (the refusal), so P11's accounting (a failed recv's print is not a show) is what those exercise.
- `describe()` after an end reads a runtime directory that is gone (its logs say so): a failure after `end` or `die` has the
  keeper's stdout and the notices but not `keeper.log` or `link.log`. A copy of the logs just before the ending would fix it;
  not built.

## 5. Cost, and the manual run

- `-race -shuffle=on -count=1 -tags busfailpoints -run TestPT1` (wiring): 57.1 s after (A), 59.5 s after (B): seeds 29.8 s
  (22 seeds, 0.7 to 2.2 s each), points 26.2 s (8 rows), redelivery row 2.5 s, unit tests 0.02 s. The coordinator measured 53.0 s
  for slice 3 (12 seeds: about 20 s, points about 27 s); so this slice adds about 7 s a count, 14 s in the tagged stage's
  `-count=2`.
- **`-pt1.random=30` (once, by hand): all 30 passed**, 52 s. They drew 18 schedules with a `recv-fail`, 11 with a removal, 1
  `left other` ending, 2 `disconnected`, and 3 runs in which a failing recv handed items out. Seeds the runner draws are
  random per run (`crypto/rand`), so this is one sample.

## 6. Not done, and what the next slice needs

- State of the commits: (A) `e676a1400`; (B) and (C) in one code commit after the last `qa-changed` (green on that tree),
  then this report and the two plan documents in a documents commit; the hashes are in the reply that carried this report.
- **Open: seed 57** (section 4), and whether `claudeSeated` in the other rows needs `pt1Seated`'s retry.
- `describe()` after an ending (above).
- A second `-pt1.random` batch of 100 or more seeds, to look for the rate of rare failures (the seed 57 kind); only 30 were run.
- `-tags tuwunel` was not run (`FAKEHS_TUWUNEL_BIN` is unset). `qa-all.bash` was not run (not allowed here).

## 7. Gates and tests, exactly (container, `FAKEHS_TUWUNEL_BIN` unset)

| What                                                                                        | Result                                                                                                           |
| ------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `-race -shuffle=on -count=1 -tags busfailpoints -run TestPT1` after (A)                     | ok 57.1 s                                                                                                        |
| the same after (B), unplanted                                                               | ok 59.5 s                                                                                                        |
| four copies of the suite at once, after (B) and `pt1Seated`                                 | 3 ok, 1 FAIL (seed 57, section 4)                                                                                |
| `-pt1.seed=57 -count=25`, unloaded                                                          | ok                                                                                                               |
| `-pt1.random=30`                                                                            | ok 52 s                                                                                                          |
| `golangci-lint run`, default and `--build-tags busfailpoints` (wiring), after the last edit | 0 issues both                                                                                                    |
| `go vet`, default and tagged (wiring)                                                       | clean after (A) and, in a chain with both lints, after `pt1Seated` (exit 0)                                      |
| `scripts/qa-changed.bash` after (A)                                                         | passed (6 files: go, go-qa on both modules, teamchat-keeps-off-v1)                                               |
| `scripts/qa-changed.bash` on (B) before `pt1Seated`                                         | FAILED: the tagged `teamchat-failpoints-test` stage, `TestPT1Points/after-auth` timed out under load (section 4) |
| `scripts/qa-changed.bash` on (B) with `pt1Seated` and the documents                         | passed (6 files: docs, go, teamchat-keeps-off-v1); it first waited for another run's QA lock                     |
| `-tags tuwunel`, `qa-all.bash`                                                              | not run                                                                                                          |
