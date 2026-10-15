# ftp-camera port to Go (Plan 00192, P8): builder report

Branch `go-port/ftp-camera`, from F44 at `3474744f`. Ported and cut over; not merged.

## What was built

- Spec: [SPEC-ftp-camera.md](../SPEC-ftp-camera.md), expectations E1-E65, departures B1-B23,
  host-only proofs H1-H5. Written before the code; the bash was read for requirements only.
- `src-go/cmd/ftp-camera` (thin main: INT/TERM cancel a context) and `src-go/internal/ftpcamera`:
  command line and launcher (gum or fzf through `internal/ui`), config read as data, the
  completeness check, the sort pass, push, copy-preflight, copy (RC stats parsed in Go), prune,
  and the vsftpd session (start, banner, one log follower feeding the printer, the async
  handler and the default-mode viewer, inotifywait, teardown, summaries).
- `src-go/internal/rclonerc`: the RC credential, the authenticated `rclone rc` call, the
  availability probe and the mount-to-RC-address lookup. The Go twin of
  `files/home/.local/bin/rclone-rc-auth.bash`, which stays because `rclone-tail`,
  `rclone-cache-warm` and `rclone-cache-status` still source it (SPEC D2).
- `src-go/internal/systemd`: `UnitActive`, `StartAsRoot`, `StopAsRoot`, `DaemonReloadAsRoot`.
- `src-go/qa/rules/table.go`: `nmcli` gains the owner `internal/ftpcamera`; `cli.asRoot` and
  `cli.rootCapture` are program runners with `dynamicNames` rows; `cli.execVault` has a
  `dynamicNames` row (the vault helper's path is configuration). `CLAUDE/GoProjectRules.md`
  says the same.
- `src-go/.golangci.yml`: gosec file-path exclusions for `internal/ftpcamera` and
  `internal/rclonerc`, with the reason.

## Cut-over

- `files/home/.local/bin/ftp-camera` deleted; its row removed from `scripts/bash-limits.allowlist`.
- `playbooks/imports/optional/common/play-ftp-camera.yml` imports `play-golang.yml` and builds
  the binary with `tasks/build-go-command.yml` at the old path, mode 0755.
- `scripts/go-binary-suites.tsv`: `ftp-camera	qa-go`.
- `scripts/test-qa-changed.bash` used ftp-camera as its allowlisted-tool example; it now uses `wsi`.
- `CLAUDE/Plan/meta-deploy.list`: the play is queued with what it does and how the host proves it.
- No CCY launcher or library file changed, so no CCY version bump.

## Proof

- Red then green per file (each test file written and seen failing to compile before its
  source). The session tests drive whole sessions over the fake runner with scripted
  long-running feeds and a clock that moves only on sleep.
- `go test -race`: ftpcamera 93.2%, rclonerc 94.7%, systemd 97.1%, cmd 80%. golangci-lint clean,
  project rules (`go vet -vettool`) clean, `qa/rules` tests pass.
- `./scripts/qa-changed.bash`: passed (49 files; go, bash-limits, docs, ansible-syntax for the
  play, test-qa-changed 118 cases, the ccy suites that build from `internal/systemd`). One
  advisory: the deleted bash file is "not covered" (nothing left to check).
- Not run: `qa-all.bash` (coordinator's), the qa-reviewer, and anything on the host.

## Departures worth the owner's eye (SPEC section 4)

- B4: an arrival the camera's retry had already moved was reported as a truncated fragment; now
  a quiet no-op, as the bash intended.
- B13: a monitor that died (inotifywait, the log tail) ended the session with exit 0; now an
  ERROR and exit 1, after the full teardown.
- B17: root is `sudo -n` after `sudo.Ready` (reason, then the password prompt) before each root
  command, so a session that outlives the sudo timestamp still tears down.
- B11: colour only on a terminal and never with `NO_COLOR`.
- B10/B22: the launcher continues in-process instead of re-executing itself, and the log is
  followed by one `sudo tail` instead of up to three.
- B18: the `--debug-ftp` note names the triage script at its real path (the plan moved to
  `Cancelled/`).

## Not done / blocked

- Host proof (H1-H5) needs the owner: run `./CLAUDE/Plan/meta-deploy.bash`, then
  `ftp-camera --copy-preflight` and `ftp-camera --sort`; a camera session for H3-H5.
- P8's `wsi` and `watermark` are not started.
