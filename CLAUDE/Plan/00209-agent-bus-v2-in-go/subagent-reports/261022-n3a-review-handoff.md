# Plan 00209 N3 n3a review: handoff 1

The reviewer-fixer's context passed its budget after round 1's first fix. This is where things
stand. A fresh agent can continue from it.

Worktree `/workspace/untracked/worktrees/bus-v2-n3a`, branch `feat/bus-v2-n3a`. The slice under
review is `git diff f6e4ad62..79d6d66e8` under `src-go/`. The contract is SPEC-n3 sections 1, 2,
9 and 10.

## Done

- `355b9a2b0`: round 1 review, `subagent-reports/261022-n3a-review.md`. Verdict FIX-FIRST: 1
  major, 4 minors, 4 nits. Each finding has its file:line and its fix.
- `131c0ade7`: finding 1 fixed (MAJOR, LK2 and MS2).
  - `homefs.Root.SameFile(full, f)` compares device and inode, using `fstatat` with no follow.
  - `lock.AdoptSeat(root, f, seat)` refuses a description that is not the seat's `seat.lock`
    (`homefs.ErrConfig`).
  - `SeatLock.LocksIn(dir)` is new.
  - `state.NewStoreWriter` refuses a seat directory whose `seat.lock` is not the held one.
  - Tests are in `homefs_test.go` (`TestSameFile`), `lock_test.go`
    (`TestAdoptSeatRefusesADescriptionOfAnotherFile`, `TestLocksInIsTheHeldSeatsDirectoryOnly`)
    and `writer_test.go` (`TestStoreWriterRefusesASeatDirectoryNotItsLocks`).
  - Each test was seen red first, as compile failures, then went green. `-race` passed and
    golangci was clean. Coverage: homefs 93.5, lock 93.3, state 99.7. qa-changed passed.

## Left: round 1

Do one commit per finding, test first.

- **2 (MINOR):** in `member/guest/places.go:318`, `SetAside` opens `--state` as `homefs.Owned`.
  Make it `homefs.Private`. Test: a 0755 state root gives 78 `bundle` and leaves the store
  untouched.
- **3 (MINOR):** in `placement.into` (`places.go:178`), on an identity change under the hold,
  first set the store aside, then unlink the old `seat.json`, then place token, then
  `seat.json`. Use `Root.Unlink`, ignoring ENOENT. Tighten `TestALinkReadingBetweenTheRenames`
  (`places_test.go:423`):
  - for the same identity, the read between must be the same user's pair with the new token;
  - for another identity, the read between must be `fs.ErrNotExist`.
  - Keep the expected CHANGED list `store.json, token, seat.json`, unless the unlink earns its
    own line. It should not: its `what` set is fixed.
- **4 (MINOR):** `TestStoreListCapsFireFirst` (`member/state/store_test.go:254`): give each row
  the rule substring it must trip, and match it. The rules are:
  - `"<list> holds more than"` for a count;
  - `"a <list> entry is over"` for a size (awaiting and missed);
  - `"a ping is not to 1 to 16"`;
  - `"agent items"`;
  - `"transcript is not"`;
  - `"holds an id twice"`.
- **5 (MINOR):** add a `member/guest` test in which a second process holds `LOCK_SH` until the
  first wait. `Remove` succeeds after one 50 ms wait, and `seat.lock` keeps the same inode.
  - The places_test `flock` helper takes `LOCK_EX`, so add an `sh` mode, as lock_test's helper
    has.
  - The fake clock needs an `onSleep` that releases the holder.
- **6 (NIT):** `homefs.createAt` (`homefs.go:171`) should unlink the name when anything after
  the exclusive create fails. Test: a write to a full pipe is not possible, so use a read-only
  data source, or a directory fd fault. The simplest route is to fail `Fchmod`, which is hard.
  Alternatively, test through `Create` with a data size that hits `RLIMIT_FSIZE`. That is
  awkward, so the fix may stay test-less only with a reason in the report. Better: introduce no
  seam, and check what the review accepts.
- Nits 7, 8 and 9 need no code. They are written up in the review file with their design-text
  proposals.

## Then

- **Round 2.** Spawn ONE fresh Opus reviewer over the fix commits (`355b9a2b0..HEAD`) and a skim
  of the whole slice. It writes `261022-n3a-review-2.md`. Fix its findings. Run round 3 the
  same way if round 2 is not MERGE, and stop at 3.
- **PLAN.md.** Update Task 3.2 with n3a's coverage, in the style of the earlier tasks. Add a
  Task 3.3 note: the install build must run
  `go test -tags teamchatrelease ./internal/teamchat/member/cli/`, the CL2 gate.
- **The slice report.** Write `subagent-reports/261022-n3a.md`. It covers:
  - what was built;
  - coverage per package. At 79d6d66e8 this was bundle 93.1, cli 99.1, exits 98.9, guest 93.9,
    homefs 93.2, kit 93.3, lock 92.9, records 100, state 99.7, reexec 92.3, paths 100, seat
    91.0, health 91.9, and cmd/teamchat 50.0 (its `main` only);
  - every decision, with its proposed design text. Collect these from the part reports:
    `261022-n3a-{cli,codecs,bundle,writer,guest-check,guest-place}.md`, and
    `261022-n3a-state-handoff.md` for the store decisions;
  - the review rounds.
- **Rules:**
  - `-race`, 90% coverage per package, golangci clean with and without `--build-tags tuwunel`,
    and qa-changed green before each commit.
  - No push, qa-all, sed, stash, amend or rebase.
  - Chain commands with `&&`, and never pipe to head or tail.
