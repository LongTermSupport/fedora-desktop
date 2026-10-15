# ftp-camera port (Plan 00192 P8): re-review of the review fixes

Branch `origin/go-port/ftp-camera` at `09eec4c6`, diffed against `970cd831` (the commit the
first review, `261015-ftp-camera-review-fable.md`, read). The branch's `src-go` was extracted
with `git archive` into scratch (no checkout): `go vet`, `golangci-lint run` (0 issues) and
`go test -race` over `ftpcamera` and `rclonerc` pass; the seven new or changed tests pass
`-race -count=30`. `git merge-tree F44 origin/go-port/ftp-camera` was run to check the
meta-deploy claim.

**Verdict: MERGE.** Each of the seven findings is fixed, and the two Medium ones are each
proven by a test that runs the production path through `App.Run` with the hardened test runner.
No new defect of consequence. One merge note (the merge is not clean) and three Low follow-ups.

Severity: High 0 · Medium 0 · Low 3 · Merge note 1.

## The seven findings

| #   | Was                                                                                                                                | Fixed                                                                                                                                                                                                                                                                                                                                                                                                                                                  | Proof through the production path                                                                                                                                                                                                                                                                                  |
| --- | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1 M | Ctrl+C drained the async backlog under the cancelled context: false `skip`/`fail` lines, a bogus sudo password block, wrong counts | `handle` and `file` stop at `stopping(ctx)` and count `unsorted`; an outcome the cancel cut short (`outcome != sortMoved && stopping`) is counted, not reported; `ready` refuses under a finished context so `sudo -v` is never reached; the enqueue that loses to the stop counts; `worker` counts what follows a failure; summary prints `N completed upload(s) not sorted — run ftp-camera --sort` (E66, B25)                                       | `TestAsyncInterruptWithBacklog`: three queued uploads, cancel fires inside the first exiftool call, held tail feed; asserts no `skip`/`fail`/`sudo asks`/`ERROR`, no `sudo -n mv`, all three files in place, `Sorted: 0 Skipped: 0 Sort errors: 0` and the `3 ... not sorted` line. Runs `App.Run` with `--async`. |
| 2 M | `kitRunner` ignored the context; every session test ended through the grace-sleep path                                             | `kitRunner.Run` returns `ctx.Err()` for a finished context (the one `execx.OS` property the session relies on). `interruptAt` now waits for every held feed to be live and fully consumed before cancelling (the zero-length `io.Pipe` write returns only when the scanner next reads, which is after the previous line's handler returned). `TestInterruptDuringStart` uses `cancelAtSleep` with no wait.                                             | The whole session suite now runs on the refusing runner; finding 1's test cancels with a live monitor (`run`'s `<-ctx.Done()` branch) which only `TestInterruptDuringStart` reached before.                                                                                                                        |
| 3 L | `sudo mv` silently overwrote a same-named sorted file (E21)                                                                        | `moveAsRoot` `Lstat`s the target first and refuses with `<rel> is already in the library — rename one of them, then sort again`; a non-`ErrNotExist` stat error is its own error (B24)                                                                                                                                                                                                                                                                 | `TestSortPassRefusesToOverwrite` (`--sort` exits 1, no `sudo -n mv`, old bytes intact, new file left) and `TestAsyncRefusesToOverwrite` (per-file `fail ... (sort error, left in place)`, session goes on). Both through `App.Run`.                                                                                |
| 4 L | `regularFiles` had a dead `walkErr` branch; an unreadable upload root was "no files"                                               | returns the root's own error; `staleBytes` propagates it; `landingAdvisory` prints `note: no landing-zone check — <err>` and `uploadSummary` `could not list what arrived — <err>`, both on stderr                                                                                                                                                                                                                                                     | `TestRegularFilesRootUnreadable` (unit level, `fs.ErrNotExist` on a missing root, `staleBytes` errors). The two call sites are two-line wrappers; acceptable.                                                                                                                                                      |
| 5 L | Docs described the bash                                                                                                            | `CLAUDE/QA.md` says "the bash `ftp-camera` (since ported to Go)" and marks the sample report as historical; `rclone-rc-auth.bash` comments no longer name `find_mount_path` or the script's `set -e`; `vsftpd.conf` and the play say "command". The `vsftpd.conf` comment's description of the runtime copy (`cp` to `/run/vsftpd-camera.conf`, `pasv_address` appended, `/run/systemd/system/vsftpd.service.d` override) matches `server.go:250-277`. | Not testable; read and checked.                                                                                                                                                                                                                                                                                    |
| 6 L | A >1 MiB log line silenced the follower until teardown                                                                             | `tailLog` runs `tail` under a local `tailCtx`; `sc.Err() != nil` cancels it, the pipe is drained, and `read <log>: <err>` is returned ahead of the tail error (E55)                                                                                                                                                                                                                                                                                    | `TestLogLineTooLongStopsTheFollower`: held feed with a 1 MiB+1 line; exit 1, `ERROR: the upload monitor stopped: read /var/log/vsftpd.log: bufio.Scanner: token too long`, `FTP server stopped.`. Through `App.Run`. The journal records it hung red before the fix.                                               |
| 7 L | `--copy`/`--push` ship truncated fragments (pre-existing)                                                                          | Recorded in spec section 4 as kept, with the reason                                                                                                                                                                                                                                                                                                                                                                                                    | n/a                                                                                                                                                                                                                                                                                                                |

Spec, PLAN.md and JOURNAL agree with the code (E21, E50, E55, E57, E62, E66, B24, B25; `src-go/CLAUDE.md` says E1-E66).

## New-defect sweep of the fixes

Checked and found sound:

- **Cancellation in `file`.** Every exit of `completeness` and `sortOne` under a finished
  context is covered: `exec.CommandContext` refuses to start (empty exiftool output →
  `sortNoDate`) or SIGTERMs a running exiftool (same), `asRoot` returns `ready`'s `ctx.Err()`
  (→ `sortFailed`); all three hit `outcome != sortMoved && stopping` → `unsorted`, nothing
  printed. A move that did complete is reported as moved (correct: it is in the library).
  `show` under a cancelled context fails → `handle` returns the error → `report` is a no-op
  because `bg.Err() != nil` → `worker` sets `failed` and counts the rest. `copyOut`'s
  `err != nil && stopping` lists the file in `failed` without a `copy fail` line; the summary
  now keys `Some files were sorted locally but did not reach the remote` on `len(t.failed)`,
  not `copyErr`, so those appear. `copyFile` is pure Go and finishes a copy in flight.
- **`ready` returning `ctx.Err()`.** Callers in the session either run under
  `context.WithoutCancel` (teardown, closing sort) or are in `prepare`, where `run` already maps
  a cancelled-context failure to exit 0. `tailLog`'s `ready(bg)` runs before any cancel.
- **The seam.** `awaitFeeds` runs inside the sleep hook, i.e. in `run`'s goroutine at the grace
  sleep; it only waits on held feeds, and every `interruptAt` caller has a held `tail` or
  `inotify` that does start (the one test where the feed never starts uses `cancelAtSleep`).
  `live` has capacity 8, so a test that never calls `awaitFeeds` (finding 1's) does not block
  the feed. For a non-held `tail` with a worker, `tailLog` closes the queue and `workers.Wait()`s
  before `report`, so the grace sleep (and the cancel) comes after every upload is handled: the
  existing async tests are not made racy by the refusing runner. 30 race runs clean here too.
- **`tailLog` ordering.** On `ErrTooLong` the remaining bytes of the long write are drained by
  `io.Copy`, the feed (or `sudo tail` after SIGTERM) returns, `pw.CloseWithError` ends the
  drain, `done` is read. No path leaves the goroutine or the pipe open.
- **Collision check as the unprivileged user.** `os.Lstat` on the library target needs traverse
  rights through root-created `755` directories, which `copyIntoMount` (reads the sorted file as
  the user) and `uploadSummary` (walks the library) already require; a `permission denied` is
  surfaced as `check <rel>: ...`, not swallowed.
- **meta-deploy.list entries.** Both paths exist on the branch;
  `play-rclone.yml:459` does deploy `files/home/.local/bin/rclone-rc-auth.bash` so the
  description (comment-only redeploy to satisfy `qa-deployed-drift`) is exact;
  `play-ftp-camera.yml:162-169` copies `vsftpd.conf`, so "the vsftpd config changes only in its
  comments" is exact (its `notify: Restart vsftpd if running` is harmless, vsftpd is not
  enabled). Each entry has its description comment above it.

## Findings

### Merge note. The merge into F44 is not clean: `meta-deploy.list` and the day journal conflict

`git merge-tree --write-tree F44 origin/go-port/ftp-camera` reports two content conflicts:

- `CLAUDE/Plan/meta-deploy.list`: F44 (`06914d00`) deleted the ccy and vm-test-lab entries;
  the branch appended its two entries directly after them. Resolve by keeping the header and
  **only** the branch's two entries (ftp-camera, then rclone) — the lead's intended outcome, but
  it needs a hand resolution, it will not fall out of the merge.
- `CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/JOURNAL/00192-Journal-26-10-15.md`:
  F44 appended `16:28 vmtest host checks`; the branch appended `16:02 ftp-camera ported` and
  `16:35 review fixes`. Keep all three; the file is append-only and the stamps already order them
  (16:02, 16:28, 16:35), so place F44's entry between the branch's two.

Nothing else conflicts.

### 1. [Low] A collision halts the whole `--sort` pass at its first instance, and the message assumes a different shot

`sort.go:90-98` (`moveAsRoot`), `sort.go:177-178` (`sortListed` → `failErr` → pass ends,
`sweepEmptyDirs` not run), spec E21/B24.

The refusal is right (the bash lost data). But a collision is a per-file data condition, not a
system fault, and the common way to get one is not two bodies wrapping at 9999: it is the camera
re-sending files that were already transferred (the camera's "not yet transferred" state is
reset by a card swap or a menu action, and the weak-link path E59 exists because re-sends are
routine). Then every file in the root collides with an identical copy in the library, the pass
stops at the first with `rename one of them, then sort again`, and nothing after it is sorted;
the renaming asks the user to work in a root-created directory. Async mode is fine (per-file
error, session continues). Follow-up, not a merge blocker: in `sortListed`, treat a collision
the way async does (report, count as a sort error, continue; exit 1 at the end if any), and
consider a same-size byte comparison so an identical duplicate is removed as root and reported
as `dup`, with only a differing file refused.

### 2. [Low] `unsorted` can count files that do not exist

`upload.go:57-60` (`handle`): the `stopping` check precedes `isRegular(u.path)`, so a log line
for a file that has since vanished (the `GONE.JPG` case `TestAsyncSession` already models) or a
camera re-send of a name already queued is counted as "completed upload not sorted". The line
then overstates. Move the `stopping` check after `isRegular`, or accept and say "log lines" in
the spec. Cosmetic.

### 3. [Low] Finding 4's production call sites are not exercised

`server.go:443-447` (`landingAdvisory`) and `teardown.go:148-151` (`uploadSummary`) print the
new `note:` / `could not list what arrived` lines; only `regularFiles`/`staleBytes` are tested
directly (`TestRegularFilesRootUnreadable`). The wrappers are two lines each and the kit cannot
easily make the upload root unreadable after the session started, so this is noted, not
required.

## Checked and found correct, in addition

- `.golangci.yml` is clean over the package; `go vet` clean; no `_ =` introduced.
- No `//nolint`, no `FAIL-FAST-OK`, no suppression.
- Public repo: the new test and spec text carry no install-specific names.
- Plan commit rule: PLAN.md Phase 3 paragraph, SPEC (E21, E50, E55, E57, E62, E66, B24, B25,
  section 4 addition) and the 16:35 journal entry are in the same commit as the code.
