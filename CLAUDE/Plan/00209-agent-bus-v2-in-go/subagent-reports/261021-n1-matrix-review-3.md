MERGE

# Plan 00209 N1 Task 1.5: adversarial review round 3 of feat/bus-v2-n1-matrix (db3c8350..HEAD)

Tip reviewed: 8019ec6e. All six round 2 findings are fixed, and the fixes break nothing. A
re-read of the branch against SPEC-matrix M45-M53 and the host-check facts H-C13, HC-A1 and
H-C14 found no new defect of any severity.

## Round 2 findings

1. **Fixed (the tagged test now runs the self-delete).** `tuwunel_test.go` `deleteAndReset`:
   after the delete from another device, `taker.DeleteDevice(ctx, "seat", password, "TCTAKER")`
   must succeed, then `r.dead(ctx, "a token that deleted its own device", taker)` runs. The reset
   is then checked against a fresh token signed in as the device. This matches hostchecks-run.md
   H-C13's last line: the C 3.7.3 fallback, 401 without auth, 200 with it, M_UNKNOWN_TOKEN after.
   SPEC M44 names the self-delete. The test still runs in order: the fresh `signIn` re-creates
   TCSEAT after it was deleted, and the final marker read logs in with the reissued password.
2. **Fixed, with a test.** `fakehs/session.go:97-127`: the session is stored as
   `uiaGrant{userID, deviceID}` and must match the caller and the path's device. A mismatch is
   `unmodelled(... for <device>)`. `session_test.go` adds "a session given for another device":
   a challenge for TCOTHER answered on TCSEAT, which must be 500 plus CheckModelled. M53 says "a
   session not given to the caller for that device". The session is deleted only on success, so
   a retry after a wrong password stays within the model, and it is the specification's shape.
3. **Fixed.** M51 now separates what HC-A1 recorded (absent means online; `offline` sets
   offline) from what the specification says (`online` and `unavailable`). M53 marks the
   wrong-password 401 `M_FORBIDDEN` and its text as the specification's and Synapse's, which the
   tagged test checks on the host. The `uiaError` and `syncPresence` doc comments say the same.
   They match run lines HC-A1 1-5.
4. **Answered as round 2 allowed.** The test comment says the hold is the mechanism every held
   sync has (TestHeldSyncEndsEmptyAtItsTimeout), and that this test pins the answer.
5. **Fixed.** The new `seatRun.unknownToken` checks `*Error`, `ErrAuthRefused` and
   `M_UNKNOWN_TOKEN`, and prints the error itself. `dead` and the second logout both use it.
6. **Fixed.** M44 says presence is not in the tagged test (the client reads none; HC-A1 is Task
   1.4's probe).

## Re-check of the whole branch

- H-C13: in fakehs, `signIn` keeps a device's earlier tokens; `logout` calls `removeDevice`,
  which ends every token of the device; `deleteDevice` ends the device's tokens and keeps the
  deleting token unless it is the device's own. In the client, `DeleteDevice` (seat.go:95-121)
  is unchanged since round 2, and round 2 checked it clean.
- HC-A1: `syncPresence` sets offline for `set_presence=offline` and online for an absent one.
- H-C14: unchanged since round 2. A since ahead of the server is held and answered with the
  server's lower `next_batch`.
- Go rules: no `//nolint` and no discarded error in the diff. The four
  `time.Sleep`/`time.Now` uses are M1's reviewed package, unchanged, as round 2 recorded. The
  worktree is clean.

No findings.

## What I ran (worktree `src-go/`)

- `go test -race -shuffle=on -count=1 -cover ./internal/teamchat/...`: pass; `matrix` 98.9%,
  `fakehs` 92.7%.
- `go vet -tags tuwunel ./internal/teamchat/...`: clean.
- `golangci-lint run ./internal/teamchat/...`: 0 issues; `--build-tags tuwunel`: 0 issues.
- `git show 8019ec6e`; `git diff db3c8350..HEAD -- src-go` scanned for `nolint`, `_ =`,
  `_ :=`, `time.Sleep`, `time.Now`; hostchecks-run.md HC-A1, H-C13 and H-C14 read.
- govulncheck was not run (GO-2026-6809 is Task 1.6's and not counted). The tagged test cannot
  run in this container; it was read, vetted and linted with the tag.
