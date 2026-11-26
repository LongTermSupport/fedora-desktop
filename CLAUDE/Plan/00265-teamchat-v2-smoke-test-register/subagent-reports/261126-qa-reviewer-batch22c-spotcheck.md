# Spot-check of batch 22c's `realClocks` row (qa-reviewer)

The reviewer's reply is copied here word for word. It was given in the conversation and not saved
as a file at the time.

**PASS.** The project-rules driver is clean on `internal/conversation/...`, so the one blocker
from the 842294f2 review is closed.

1. **The row matches its neighbours and the rule doc.** The new row is at
   `src-go/qa/rules/table.go:448` in `realClocks`. It has the same shape as the
   `qa/internal/qalock` `RealClock.Sleep` row at line 443 and the `playlock` `sleep` row: "the
   real side of X, the poll...; its tests replace X whole". `CLAUDE/GoProjectRules.md` (around
   line 735) says `realClocks` is "for the real side of a clock seam whose tests replace it
   whole", and this row fits that.

2. **The reason it gives is true.** In `src-go/internal/conversation/seatrecord.go`:

   - The bound is a constant: `seatRecordLockWait = 30 * time.Second` (line 167).
   - The poll is a constant: `seatRecordLockPoll = 100 * time.Millisecond` (line 170).
   - The only call is `lockClock.Sleep(seatRecordLockPoll)` (line 235), and it is the only place
     `realLockClock.Sleep` gets used.
   - The only test that holds the lock is `TestSeatRecordWritersFailNamingALockHeldTooLong`. It
     replaces `lockClock` whole with `fakeLockClock` and puts it back afterwards
     (`seatrecord_test.go:481-483`). No other test touches `lockClock`, and only a held lock
     reaches the sleep.
   - Nothing outside these two files mentions `lockClock` or `realLockClock`, apart from the new
     row.

3. **The driver is clean.** `go -C src-go/qa build -o <scratch>/qar2 ./driver` built it, and
   `go vet -vettool=<scratch>/qar2 ./internal/conversation/...` exited 0 with no output. I ran
   this on the working tree, which is at HEAD `84ed9176b`. `git diff --stat bf9ec613 HEAD -- src-go` is empty, so the result holds for bf9ec613. I did not rebuild at 842294f2 to see the
   rule fail without the row; that would have needed a worktree. The earlier report quotes the
   violation at `seatrecord.go:183:47`.

4. **The other files are only what you said.**

   - `seatrecord_test.go:511-512` is a comment-only change. It now says an `os.Root` follows the
     link "at the last name, O_NOFOLLOW or not", which fixes the wording the fact-check flagged.
   - `REGISTER.md` gets one six-line bullet for the re-review outcome.
   - Two subagent reports are added: `261126-factcheck-b22-review.md` and
     `261126-qa-reviewer-batch22b.md`.
   - No other code changed.
