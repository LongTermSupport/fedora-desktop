# ftp-camera port (Plan 00192 P8): adversarial pre-merge review

Branch `origin/go-port/ftp-camera` at `970cd831`, diffed against `F44`. Read: the spec
(SPEC-ftp-camera.md), the builder report, every file of `src-go/internal/ftpcamera`,
`internal/rclonerc`, `internal/systemd/unit.go`, `cmd/ftp-camera`, the play, the
meta-deploy entry, the allowlist and QA data, and the deleted bash at F44 (prelude, argument
parsing, launcher and inhibit, `sort_uploads`, `prune_orphan_raws`, `sort_one_file`,
`copy_one_file_to_mount`, `process_async_upload`, `async_monitor_loop`, `cleanup`, the main
server flow) and `rclone-rc-auth.bash`. The branch's `src-go` was extracted with `git archive`
into scratch (no checkout) and `go vet` + `go test -race` run over the four packages: all pass.
One review probe was added there to drive the finding below; it is not on the branch.

**Verdict: FIX FIRST.** Finding 1 is a user-visible regression against the bash on a common path
(Ctrl+C during a burst of uploads) and the test seam cannot see it (finding 2). Nothing found
loses data. Findings 3-7 are follow-ups.

Severity: High 0 · Medium 2 · Low 5.

## Findings

### 1. [Medium] After Ctrl+C, the async handler keeps working through the queued backlog with the cancelled context, so whole files are misreported and a bogus sudo prompt block is printed

`src-go/internal/ftpcamera/server.go:554-567` (`worker`), `server.go:522-537` (`follow`),
`teardown.go:23-24` (`stopBg(); s.bg.Wait()`), `upload.go:50-78` (`handle`), `app.go:230-246`
(`ready`, `asRoot`).

Teardown cancels `bg`, which SIGTERMs `sudo tail`; `tailLog` then closes `queue`, and
`workers.Wait()` waits for the worker, which drains `for u := range q` by calling
`handle(bg, u)` for every upload still queued. `bg` is done, so with the production runner
(`exec.CommandContext` refuses to start a child under a finished context, and SIGTERMs one
already running) every root and exiftool call fails at once:

- the in-flight file's `exiftool` is SIGTERM'd: `exifDate` reads an empty stdout and the file
  is reported `skip  <name>  (no EXIF date, left in place)` and counted skipped, although it
  has a date;
- each queued file hits `ready()`: `sudo -n -v` fails (context), `sudo -n true` fails, the TTY
  opens, so it prints `ftp-camera runs vsftpd... as root.`, `sudo asks for your login password.`, then `sudo -v` fails and prints `ERROR: sudo was not given your password (three wrong tries, or cancelled).`, then `fail  <name>  (sort error, left in place)`, counted as a
  sort error;
- the async summary then carries the wrong counts, and all of this is printed after
  `Stopping FTP server...`.

Reproduced with a probe in the scratch copy (`review_drain_test.go`: a runner that, like
`execx.OS`, fails a command whose context is done; three completed uploads; the first sort's
exiftool cancels the run). Output, trimmed:

```
Stopping FTP server...
  [09:30:00] skip  B.JPG  (no EXIF date, left in place)
  [09:30:00] skip  C.JPG  (no EXIF date, left in place)
...
Async summary:
  Sorted: 0  Skipped: 2  Sort errors: 1
--- stderr ---
ftp-camera runs vsftpd, the firewall and the filing of uploads as root.
sudo asks for your login password.
ERROR: sudo was not given your password (three wrong tries, or cancelled).
  [09:30:00] fail  A.JPG  (sort error, left in place)
```

The bash killed the monitor pipeline in `cleanup` (lines 2090-2101), so a backlog was dropped
silently and nothing false was printed. No data is lost here (`mv` within one filesystem is
atomic and a left-in-place file is sorted by the next run), but the person is told their
password was refused and their files have no EXIF date, and the summary lies. Scenario: a
burst of RAW+JPG pairs arriving faster than exiftool sorts them (two exiftool runs per file),
then Ctrl+C.

Fix: in `worker`, stop handling once `bg.Err() != nil` and drain the rest without calling
`handle` (and say so once: `N completed upload(s) not sorted — run ftp-camera --sort`), or
better, run the in-flight item to completion under `context.WithoutCancel(bg)` and drop only
what has not started. Whichever, the counters must not record cancelled work as skips/errors.
Add the test from finding 2 so the suite proves it.

### 2. [Medium] The test seam cannot see cancellation: `execx.Fake` ignores the context, and every session test ends through the "monitor stopped, interrupt within grace" path

`src-go/internal/ftpcamera/kit_test.go:61-87` (`kitRunner`), `server_test.go:35-46`
(`interruptAt`), and every `interruptAt(k, time.Second)` caller in `server_test.go`,
`upload_test.go`, `teardown_test.go`.

`interruptAt(k, time.Second)` cancels at the one-second grace sleep that only runs after
`s.ended` fires, i.e. after a feed with `hold=false` has ended and every queued line is already
handled. The plain Ctrl+C path with live monitors (`run`'s `case <-ctx.Done()`,
`server.go:83-84`) is driven only by `TestInterruptDuringStart`, before the monitors exist.
Nothing cancels a run while a handler is mid-file, and because `Fake.Run` never looks at its
context, even a test that did would see the fake keep answering `sudo` and `exiftool` happily —
which is exactly why the 93% coverage figure did not catch finding 1. Fix: make `kitRunner`
return `ctx.Err()` for a command started under a finished context (the one `execx.OS` property
the session relies on), and add a test that cancels with both feeds held and a queued backlog.

### 3. [Low] `sudo mv <file> <dest>/` overwrites a sorted file of the same name (E21, pre-existing)

`sort.go:89-95` (`moveAsRoot`), used by `sortListed` and `sortOne`; spec E21 codifies it.

Sony bodies number files DSC00001-DSC09999 and wrap; two bodies collide trivially. A new
upload named like an already sorted file of the same date lands on top of it with no notice.
The bash did the same (`sort_one_file:1525`, `sort_uploads:936`), so this is not a port
regression, but the spec's section 4 reviewed 23 departures and did not take this one. Follow-up
(not a merge blocker): `mv -n` and report a collision as a sort error, or state the overwrite
as accepted in section 4's "kept as they were" list so it is a decision rather than an omission.

### 4. [Low] "No discarded errors" is satisfied by no-op branches

`out.go:47-51` (`stream.f`: `if _, err := ...; err != nil { return }`), `verify.go:76-80`
(`closeQuietly`), `server.go:420-422` (`regularFiles`: `if walkErr != nil { return }`).

`stream.f` is defensible: the `lockedWriter` beneath records the first failure and `App.Run`
turns it into exit 1 (tested). `closeQuietly` on a read-only file is the usual idiom. But
`regularFiles`' `walkErr` is always nil — the callback never returns anything but nil or
`fs.SkipDir` — so the branch is dead, and the one error that matters there (the upload root
itself unreadable) is silently "no files" for the landing advisory and the upload summary. Fix:
drop the dead branch and have the callback's root-error case return the error, or document in
the function comment that the root's own error is deliberately "nothing counted". The project
rule says return or handle; a `return` that handles nothing reads as the former without being it.

### 5. [Low] Documentation still describes the bash

- `CLAUDE/QA.md:584-586` says, in the present tense, that `ftp-camera` "reports a
  `PartialParsing` range of lines 585-2486", and the example block at `:595` shows a report
  line for `files/home/.local/bin/ftp-camera`, a file this branch deletes. The paragraph is
  about the gate's design and the example is illustrative, but a reader checking it against the
  tree finds no such file.
- `files/home/.local/bin/rclone-rc-auth.bash:141` cites "ftp-camera's find_mount_path" and
  `:159` "`ftp-camera` runs `set -e` WITHOUT `pipefail`" as the reason for a spelling; neither
  caller exists any more (the Go `mountPath`/`AddrForMount` replace them).
- `files/etc/vsftpd/vsftpd.conf:4,30` and `play-ftp-camera.yml` (task "Disable vsftpd
  auto-start (managed manually via ftp-camera script)", summary "The script will print your
  PC's IP address") call it a wrapper script.

Cosmetic; a follow-up commit.

### 6. [Low] A log line over 1 MiB silences the follower until teardown

`server.go:597-611` (`tailLog`). `bufio.Scanner` with a 1 MiB cap stops on `ErrTooLong`; the
code then drains the pipe and waits for `tail` to exit, which only happens at teardown, so for
the rest of the session nothing is printed or handled and the error surfaces only in the
closing report — E55 says a part that stops ends the session. vsftpd lines are a few hundred
bytes, so this is hypothetical; noted because it is the one place a `read` failure is deferred
rather than reported. Fix: on `sc.Err() != nil`, cancel the tail (a local context) so the
failure is reported through `s.ended` at once.

### 7. [Low] `--copy` and `--push` ship whatever is in the upload root, truncated fragments included (pre-existing)

`remote.go:217` (`cp -r <upload>/. <dest>/`) and `remote.go:110-114` (`rclone copy <upload>`).
Same as the bash (`copy_to_mount:1203`, `push_uploads`). The sort passes deliberately leave
fragments in the root and the spec says so, yet the two shipping modes then copy them to the
remote under their real names. Not a port regression; a spec section 4 candidate.

## Checked and found correct

- **Play and host run.** `play-ftp-camera.yml` imports `../../play-golang.yml` (path resolves)
  and replaces the `copy` with `tasks/build-go-command.yml` at the old path and mode; the
  `end_play` on an unset `ftp_camera_password` still precedes it, so the meta-deploy note "only
  installs the Go toolchain and ends" is accurate. `/etc/ftp-camera/config` is written as plain
  `KEY=VALUE` lines and comments, which `readConfig` accepts. `meta-deploy.list` names the exact
  play and what proves it (H1-H5). `rc-auth.env` is written as `KEY=VALUE` with no quotes or
  `export`, so `rclonerc.firstValue` reads what the awk did. The rclone unit passes the
  mountpoint as one argv token, which `AddrForMount`'s exact-token match needs.
- **Flags, exit codes, streams.** Parsing left to right, `-h`/`--help` 0, unknown 2, the three
  modifier refusals, one-primary-mode, tunables 2 (new), launcher 130, `--pass` exec, `--copy`
  interrupt 130, `--sort` 1 on a failed pass, session 0 on Ctrl+C after teardown, 1 on a part
  that died (B13, correct departure). Diagnostics on stderr, the RC address alone on stdout
  (tested). Colour only on a TTY (B11).
- **Root and teardown.** Same `/run` files, override text, firewall rules, `nm-shared`
  handling, hotspot own/leave logic and order as the bash; `tee` writes carry the same bytes
  (tested). Teardown runs on every start failure as the bash's EXIT trap did (its `trap` was set
  before the prerequisites). `firewall-cmd --query` exit 1 is "absent", other failures stop
  (B14). Teardown runs under `context.WithoutCancel`, so a second Ctrl+C does not abort it.
  `execx.OS` SIGTERMs `sudo tail`, which relays to `tail`; `--pid` remains the backstop.
- **Completeness and sort.** Byte-aligned `FF D9` (B2 fixes a real bash bug), strips from stdout
  only (B3), zero bytes truncated, unknown types unverifiable then "unsupported" (bash parity),
  top-level regular files only, name order (B20), truncated left in place and counted, the
  empty-dir sweep as root.
- **Per-upload copy.** `findmnt -T ... -t fuse.rclone` guard, `.ftp-camera.tmp.<name>` then
  rename, temp removed on failure, unprivileged copy as the bash's `cp` (uploads are 0664 under
  `local_umask=002`), directories with umask.
- **Prune.** `RAW` directories anywhere under `photos/`, `.arw` any case, `<stem>.jpg|.jpeg`
  any case in sibling `JPG/`/`JPEG/`, `[y/N]` default no, EOF is no, per-file `rm -f` as root
  with failures listed and the rest continuing.
- **Rules and QA data.** `nmcli` second owner, `asRoot`/`rootCapture` as program runners with
  `dynamicNames` rows, `execVault` row, `.golangci.yml` exclusions with reasons,
  `GoProjectRules.md` and `src-go/CLAUDE.md` updated, allowlist row removed,
  `test-qa-changed.bash` example moved to `wsi`, `go-binary-suites.tsv` row added. The dynamic
  `Env` on `systemd-inhibit` is additive (`ChildEnv`), so the re-exec keeps the environment.
- **Public repo.** No hostnames, usernames, checkout paths or private addresses in tracked
  files: tests use RFC 5737 addresses and `/home/u`.
- **Plan state.** PLAN.md Phase 3 paragraph, JOURNAL entry and the builder report agree with the
  diff; the report's "not run" list is honest.

## Not findings, noted

- `--pass` still needs the `camera` account (the bash did too; only help and usage errors are
  exempt, as E7 says).
- `copyFile` gives the mount copy the source's permission bits rather than `cp`'s umask
  default; on a cloud mount they are not representable, so the difference has no effect.
