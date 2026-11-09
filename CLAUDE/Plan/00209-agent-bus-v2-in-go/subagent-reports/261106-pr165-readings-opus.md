# PR #165 second round: P2's readings kept apart, F44 merged

Branch `fix/pt1-p2-readings-156-r2`, from `origin/fix/pt1-p2-readings-156` (1a271b488). Not
pushed. It answers the coordinator's review of PR #165 (2026-11-06) and addresses issue 156.

## Commits

1. `4b8b1ae99`: merge of `origin/F44` (PR #144 included).
   - `BUILT-n3.md`: two entries were added in the same place, and both are kept. This branch's
     P2-readings paragraph stays under the PT1 entry. F44's store-size, sender and retry-hold
     entries follow it.
   - `PLAN.md`, Task 3.2: F44's line is kept: "n3a-n3d; n3e in part (…). Open: n3e's rest (the
     OF1 row, PT1's unreached points, seed 57 in issue 156), n3f". This PR's "open: n3f" wording
     is dropped.
   - The Go files merged with no conflict.
2. `4e1a88e6b`: the classifier fix, its tests and the caveat in BUILT-n3.
3. This report.

## Review point 1: awaiting and missed entries are kept apart

- `pt1Snap` (`pt1_model_test.go`) used to have one `inFlight` list. It now has `awaiting` and
  `missed`. `pt1SnapOf` (`pt1_redeliver_test.go`) fills each from its own store list.
  `describe` prints both: `awaiting [..], missed [..]`.
- `pt1Classify`, in order:
  1. no record, store unread, no nonce, or a contradictory first-seen: "neither", as before.
  2. The nonce was a **missed** entry before the recv: **(b)**, the harness. The copy was found
     late, after the mark.
  3. The nonce was **awaiting** before the recv: **(a)**, production. The reason names P11's
     question: a recv hands out only an item handed to nobody, so production released the item
     wrongly (a hand-out exclusivity breach, or an unawait of a line that was written).
  4. A link crash: "neither", as before.
  5. The nonce was in flight at the mark (awaiting or missed), or not at all: (a), as before.
- The doc comment of `pt1Classify` states why only a missed entry can be (b).

## Review point 2: the tests

Red came first. The two new tests run through the real `pt1SnapOf` over real `state.Store`
values, then through `pt1Classify`.

- `TestPT1P2ReadsAMissedEntryFoundLateAsTheHarness` builds the shape `declareMissing` leaves.
  The store before the recv holds `$a` pending, repeat, held, with `Handed` nil, so a recv can
  hand it out. It also holds a missed entry d4 naming `$a`. The store at the mark holds `$a`
  returned, with d4 still missed. The test wants (b).
- `TestPT1P2ReadsAnAwaitingEntryBeforeTheRecvAsProduction`: before the recv, `$a` is handed to
  the awaiting entry d5. The test wants (a) with "P11" in the reason.

Red against the old classifier (`go test -tags busfailpoints -run TestPT1P2Reads`): both
failed, with exit 1.

- The awaiting case read `"reading (b), the harness"`. That is the defect.
- The missed case read (b), but with the old reason ("in flight"), not "declared missing".

Other changes:

- The real-run `TestPT1MarkBeforeAnInFlightDelivery` is renamed
  `TestPT1MarkBeforeAnAwaitingDelivery`. It now requires (a) naming P11. Its record in the
  green run: "before the recv (seq 1): … pending, handed to <nonce>, in the ledger, awaiting
  \[<nonce>\], missed []".
- The longer name `…ReadsProduction` failed at first. The temporary directory path made the
  link's unix socket path too long ("bind: invalid argument"), so the name was shortened.
- The unit table's old (b) case is split in two: a missed case, which reads (b), and an
  awaiting case, which reads production. `TestPT1P2FailurePrintsTheReadingAndTheRecord` now
  checks both record lines. `TestPT1P2VerdictIsTheRecordsNot` is unchanged and passes over
  every case, so the verdict does not move.

A real-process run cannot reach (b) through a missed entry. `declareMissing` needs 180 s of
quiet, and no run waits that long: `pt1Unreached` lists `LinkAfterMissing` for this reason. So
the store-built timeline is (b)'s proof. It is a timeline a recv can reach, because the missed
item is handed to nobody.

## Review point 3: the caveat in BUILT-n3.md

The (b) entry now says that a recv hands out only an item handed to nobody. A missed entry's
item is handed to nobody only after `declareMissing`, which no seed run reaches. So (b) printed
after a recv in a seed run would mean a production defect, never the harness. The entry also
names the new tests.

## Commands and results

- `go vet -tags busfailpoints ./internal/teamchat/member/wiring/` after the merge: clean.
- `go test -tags busfailpoints -count=1 -v -run 'TestPT1P2|TestPT1MarkBefore|TestPT1ReadsThe'`
  over member/wiring: all 8 pass.
- `golangci-lint run --build-tags busfailpoints ./internal/teamchat/member/wiring/`: 0 issues.
  Without the tag: 0 issues.
- `go -C src-go/qa build -o untracked/scratch/driver ./driver`, then
  `go vet -vettool=<driver> ./internal/teamchat/...`, untagged and with `-tags busfailpoints`:
  clean, exit 0.
- `go test -tags busfailpoints -race -count=20 -shuffle=on ./internal/teamchat/member/wiring/`
  over the whole package: it hit Go's default 10 m `-timeout`. The panic was "test timed out
  after 10m0s" while `TestE2EQueuedAtStart` was running. That is 20 copies of the whole e2e
  suite exceeding the time limit. No assertion failed.
- `go test -tags busfailpoints -race -count=20 -shuffle=on -timeout 60m -run TestPT1` over
  member/wiring: `ok` (1655 s), exit 0.
- `./scripts/qa-changed.bash --base origin/F44` (8 changed files): docs, go (go-qa, both
  modules, diff mode) and teamchat-keeps-off-v1 all passed, exit 0. Neither environmental
  failure (qa-js TestReal\*, ansible-syntax) came up, because neither gate was routed.
- A plain `./scripts/qa-changed.bash` with no base reported "no changed files (vs HEAD)",
  because everything was committed. The run with `--base` is the one that counts.

## Outside the task

- The whole-package `-count=20` race run does not fit in Go's default 10 m timeout. That is no
  defect, but a run over the whole package needs `-timeout` raised.
- Unix socket paths cap the length of a wiring test's name: a long name fails the link's bind.
  It is worth knowing when naming tests in this package. Nothing was changed for it.
