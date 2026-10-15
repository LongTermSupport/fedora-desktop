# P8 wsi and watermark ports: report

Branch `go-port/wsi-watermark`, merged with origin/F44 (clean against it at the merge commit).

## What landed

- Spec first: [SPEC-wsi-watermark.md](../SPEC-wsi-watermark.md), with W1-W35 for wsi, M1-M14
  for watermark, departures B1-B30 and host checks H1-H3.
- watermark: `src-go/cmd/watermark` and `internal/watermark`. Config files are read as
  `KEY=VALUE` data and never sourced (B21: a file that relied on shell expansion is refused,
  naming its file and line).
  - The new shared package `internal/shellword` replaces registry's own quoting.
  - `execx.Cmd` gains `Interrupt`, the cancel signal (SIGTERM by default).
  - `tasks/build-go-command.yml` gains the optional input `go_command_owner`, so
    `play-image-watermarking.yml` can install the binary root-owned into `/usr/local/bin`.
  - The suite row is `qa-go`.
- wsi: `src-go/cmd/wsi` and `internal/wsi`. The recording is one select loop over the stop
  signals, the timers and `pw-record`; `pw-record` runs through `execx` and is stopped with
  SIGINT.
  - The main subscribes to SIGTERM and SIGINT before anything else.
  - The sibling helpers `wsi-stop-grace`, `wsi-setting` and `wsi-resolve-model` are unchanged
    and run from the binary's directory, which needs a `dynamicNames` row in
    `qa/rules/table.go`.
  - `play-speech-to-text.yml` builds the binary into `~/.local/bin/wsi`.
  - `scripts/test-wsi-stop-grace.bash` builds the binary and drives it (120 passed). Its static
    checks on the bash's `kill` sites are removed.
  - `qa-stt-limits.bash` reads `internal/wsi/limit.go`. A planted second 30 was tested and
    failed the gate, as it should.
  - `test-qa-changed.bash` now uses `reclaim` as its allowlisted example.
- Both bash scripts and their allowlist rows are removed. Both plays are queued in
  `meta-deploy.list` with descriptions.
- Docs updated: QA.md, GnomeShell.md, src-go/CLAUDE.md, docs/playbooks.md, PLAN.md Task 3.3,
  and the journal.

## Verification

- Every test was seen red before its code existed; the new test files failed to compile until
  then.
- `scripts/qa-go.bash` passed: module coverage 92.6%, internal/wsi 91.1%.
- `./scripts/qa-changed.bash` passed on each commit.

## Open

- H1-H3 need a host run: dictation pastes into a terminal and an editor, Escape discards a
  recording through the panel's group kill, and watermark runs end to end. The owner runs
  `./CLAUDE/Plan/meta-deploy.bash` to do this.
- No ftp-camera helpers were needed.
- Merge risks for whoever merges next:
  - `tasks/build-go-command.yml` has both `go_command_owner` and F44's `go_command_module`.
  - `meta-deploy.list` and the journal are append points that other branches also touch.
