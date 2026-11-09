# Review: go-port/wsi-watermark (06bacc37) — Plan 00192 P8, wsi and watermark ports

Reviewer: Fable 5.1. Read-only against `F44...origin/go-port/wsi-watermark`; the branch was
also checked out detached under `untracked/scratch/wt-wsi-review` so the suites could be run.

## Verdict: MERGE (one merge-time action, no branch fixes required)

The two ports are faithful to the deleted bash where the bash was right, and each departure
is the one the spec (SPEC-wsi-watermark.md B1-B30) says it is. Nothing deletes or overwrites a
user's image or recording that the bash did not already; the recorder is stopped with SIGINT
through `execx` and killed after 3 s if it ignores it; all chatter stays on stderr; exit codes
match the usage texts. The shared changes (`internal/shellword`, `execx.Cmd.Interrupt`,
`build-go-command.yml` `go_command_owner`) are backward-compatible for every other caller.

## What was run (on the branch, not trusted from the report)

| Check                                                                                   | Result                                                                                                                                     |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `go vet`, `go test -count=1` over wsi, watermark, shellword, execx, registry, both cmds | all ok                                                                                                                                     |
| `golangci-lint fmt --diff` and `golangci-lint run` over the same                        | 0 issues                                                                                                                                   |
| project-rules vettool (`qa/driver`) over the same; `go test ./rules`                    | clean / ok                                                                                                                                 |
| `scripts/test-wsi-stop-grace.bash` (builds and drives the Go binary)                    | passed 120, failed 0                                                                                                                       |
| `scripts/qa-stt-limits.bash`                                                            | pass (reads `src-go/internal/wsi/limit.go`)                                                                                                |
| `scripts/qa-bash-limits.bash`                                                           | BASH-LIMITS-OK, both rows gone, allowlist consistent                                                                                       |
| `scripts/test-qa-changed.bash`                                                          | passed 121, failed 0 (`reclaim` is in the allowlist, row 41)                                                                               |
| `scripts/qa-changed.bash --plan --base F44`                                             | `internal/wsi` and `cmd/wsi` route to the stop-grace suite; `shellword`/`execx`/`registry` route to the five ccy suites that build on them |
| `git merge-tree F44 origin/go-port/wsi-watermark`                                       | one content conflict: `CLAUDE/Plan/meta-deploy.list`                                                                                       |

Not run: `qa-all`, `qa-go` whole-module (the report's 92.6 % / 91.1 % coverage figures are
unverified; lint, vet, rules and every test in the touched packages pass).

## Findings by severity

### Must do at merge (not a branch defect)

- **M1. `meta-deploy.list` conflicts.** The branch still carries F44's older list (00201 at
  3.98.6 and the 00199 md-viewer entry, which F44 has since run and removed). Resolution: take
  F44's three 00201 entries verbatim and append the branch's two 00192 entries (watermark play,
  then speech-to-text play) with their descriptions unchanged. Both descriptions are accurate:
  the watermark one correctly says the example config is redeployed and a `source`-dependent
  config is now refused with file and line; the wsi one correctly names H1 and H2.

### Medium

None.

### Low

- **L1. `cleanup` removes `/dev/shm/wfile-$USER.wav` on every exit after W5, including a run
  that failed in preflight and never recorded** (`internal/wsi/app.go:243-245`). Spec W6/B11 say
  "after recording starts". Harmless today because W5 guarantees no other `wsi` owns that file
  and nothing else uses the name (`wsi-stream` shares only the PID file), but the spec wording
  and the code differ; tighten one or the other.
- **L2. Limit timer is not armed when a TERM is already pending at the end of the prepare
  delay** (`record.go:137-143`: `prepared` returns before `s.limit = After(maxRecording)`). The
  recording is then bounded only by the grace, which the schema caps at 30 s, so the bound
  holds in practice; it is a latent dependency on the schema range rather than on `limit.go`.
- **L3. `-d` dependency check prints a nonsense install hint for the transcriber**
  (`prepare.go:102`): `Install: sudo dnf install faster-whisper (pip install --user faster-whisper)`.
  The bash printed no install line for it. Cosmetic.
- **L4. Watermark B26 removal on exiftool failure also removes a pre-existing file named with
  `--force -o`** (`internal/watermark/app.go:240`). Magick has already overwritten it by then, so
  no additional loss, but worth one sentence in the usage/spec so the behaviour is deliberate.
- **L5. Constants defined but not used at every site:** `paste.go:203` runs `"ydotool"`
  literally while `ydotoolBin` exists; `deliver.go:24-31` uses literal `"wl-copy"`, `"xsel"`,
  `"x11"`, `"wayland"` while `wlCopyBin`, `xselBin`, `sessionX11`, `sessionWayland` exist in
  `prepare.go`. The project-rules gate is satisfied (names are still constants), so this is
  consistency only.
- **L6. `.golangci.yml` excludes gosec G304 for all of `internal/wsi/`,** not just the files that
  read fixed paths. Documented in the exclusion comment; acceptable, but broader than needed.
- **L7. Pre-existing doc staleness surfaced, not introduced:** `CLAUDE/GnomeShell.md:385` says
  the extension calls `GLib.spawn_command_line_async('wsi ...')`; it now spawns through
  `setsid` with `spawnWatched`. The branch edited the adjacent line 379 correctly.

## Behaviour parity: what was checked and found faithful

wsi (bash `files<home>/bin/wsi` at F44 vs `internal/wsi`):

- Flags, defaults and the "last server address wins" rule (W1); `--claude-style` now in usage (B19).
- Arg errors: whole line read first so the token and debug flag are honoured; exit 2 instead
  of 1. The extension treats any non-zero status identically (`recorderLaunch.js:34`), so the
  panel is unaffected.
- One recorder at a time: PID file read, `kill -0` liveness, stale file overwritten later; the
  PID file is removed only when it names this process (W5/W6). The extension's
  `RECORDER_CMDLINE` (`/\/wsi(-stream|-article)?(\0|$)/`) matches the Go binary's argv[0]
  (`~/.local/bin/wsi`), and Escape's group kill still takes `pw-record` because the Go binary
  is the `setsid` group leader and `pw-record` is its child.
- Signals: the main subscribes to TERM and INT before anything runs (buffered channel of 8), so
  a stop during the model resolve is held and acted on when the recorder starts (B5). Grace,
  second TERM, INT, and the 30 s limit all stop the recorder with SIGINT through
  `execx.Cmd.Interrupt`; a recorder ignoring SIGINT is killed after `execx.DefaultGrace` (3 s).
- Recording environment: `execx.Cmd.Env` is appended to the parent's environment
  (`ChildEnv`), so the transcriber still sees PATH, HOME and HF caches alongside
  `WHISPER_LANGUAGE`/`WHISPER_MODEL`; a nil `Stderr` is captured, so sox and faster-whisper
  stderr are discarded as the bash's `2>/dev/null` did, and shown with `-d`.
- Text clean-up (per-line `(...)`/`[...]` removal, leading newlines, leading white space,
  first-letter upper-case), the 60-character preview, the Claude path (raw text to CLIPBOARD,
  staying notification), wrap marker only without Claude, saved transcript, the
  no-graphics stdout path, PRIMARY/CLIPBOARD with and without trailing newline, the PasteKey
  focus wait (20 polls, 5 settle, 1.0 s before Enter after a retake, re-ask), the three
  ydotool chords, Enter and Ctrl+S as non-fatal problems, SUCCESS last.
- D-Bus: every string is a GVariant literal (B10); signals in the foreground; nothing after
  SUCCESS/ERROR/IDLE; a failed emit or refused notification is non-fatal.
- Siblings run from the binary's directory via `os.Executable()`; the stop-grace test builds
  the binary beside symlinks to them and passes.

watermark (bash `files/usr/local/bin/watermark` vs `internal/watermark`):

- Same options, `--` handling, `-o` alias, usage to stdout on `-h`, stderr on no image, exit
  codes 2/3/4/5 exactly as before; the new exit 1 is only for a failed stdout write (B30).
- Config layers and precedence unchanged; files are now parsed as data (B21) with a clear
  `<path>:<line>: <reason>` on anything the bash would have executed. The shipped
  `defaults.conf.example` parses under the new rules (all values double-quoted, no `$`).
- Idempotency: name suffix then `exiftool -s3 -XMP-wm:Applied`; the probe's failure is now
  exit 5 instead of being read as "not marked" (B24). Output never resolves to the input;
  existing output needs `--force`; argv for magick and exiftool byte-identical to the bash
  arrays; dry-run after every check; exiftool failure removes the half-marked output (B26).
- `--profile ../x` refused (B23); presentation values validated before they reach magick (B22).

## Shared changes

- `internal/shellword`: `Quote`/`Join`; `plain` adds `,` to the registry quoter's set (a comma
  is safe unquoted in POSIX sh outside braces, and `{` is not plain), and leaves `*?[` out so
  `%[fx:w*0.03]` is quoted. `registry.Trampoline` now calls it; registry tests pass.
- `execx.Cmd.Interrupt`: zero means SIGTERM, so every existing caller is unchanged; the new
  test proves SIGINT alone reaches the child.
- `tasks/build-go-command.yml`: `owner`/`group` default to `user_login` and `become` evaluates
  to false whenever `go_command_owner` is unset, so every other importing play renders the
  same install task as before; only the watermark import becomes root for the copy.

## Plays, docs, plan

- `play-image-watermarking.yml` and `play-speech-to-text.yml` import `play-golang.yml` first
  (same relative path as `play-ftp-camera.yml` and `play-podfreeze.yml`) and build through
  `build-go-command.yml`; no other task in either play references the deleted bash paths.
- `qa-stt-limits.bash` reads `limit.go` for the one `30`; `test-wsi-stop-grace.bash` builds the
  binary and dropped its two static `kill`-site greps; `test-qa-changed.bash` uses `reclaim`.
- PLAN.md additions fact-checked against the diff: `internal/shellword` shared (yes),
  `go_command_owner` (yes), `limit.go` read by the gate (yes), the suite drives the binary
  (yes, 120/0), both allowlist rows gone (yes), H1-H3 pending (yes). Journal and report
  figures for the suite (120) match; coverage figures unverified.
- No install-specific names in any added file; `src-go/CLAUDE.md`, `CLAUDE/QA.md`,
  `CLAUDE/GnomeShell.md`, `docs/playbooks.md` updated consistently.
