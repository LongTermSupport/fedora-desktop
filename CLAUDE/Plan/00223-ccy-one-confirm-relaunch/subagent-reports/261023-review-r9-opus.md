# Plan 00223 review, round 9 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at 8235a4f6d, against r1-r8 and the code
at the base. The code is unchanged on the branch, and line numbers are the base's.

**Verdict: MERGE** (0 blocking, 3 should-fix, 3 nits)

## Earlier items (r8)

| Item                                       | State       | Note                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------ | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1 `--debug` across the re-exec            | Fixed (C22) | DESIGN 2.1:115-128. Checked against the code. A Go-front `CCY_LAUNCHER_PATH` keeps the descriptor and the environment: `handOver` is `syscall.Exec` with `os.Environ()` (internal/ccylaunch/front.go:477, internal/execx/exec.go:109-113). The token hand-off's EXIT trap is dropped by the exec (lib/token-handoff.bash:57-58, 69). Layers 2 and 3: outer run once and tmux run once, which is today's count. The tmux run carries `--settled`, so it shows no screen |
| S1 redirection order                       | Fixed       | DESIGN:110-111                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| S2 no `else` on 1243                       | Fixed       | DESIGN:177, a new `elif` before 1336 (claude-yolo:1336)                                                                                                                                                                                                                                                                                                                                                                                                                |
| Nit: migration prompt asked again          | Open        | See S3                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Nit: `CCY_ATTENDED` init, `local attended` | Open        | Still a nit (N1)                                                                                                                                                                                                                                                                                                                                                                                                                                                       |

## Blocking

None.

## Should-fix

1. **"read and unset" loses the values the re-run needs.** DESIGN 2.1:121-126 says the re-run
   "reads them once at the start and unsets them, as `CCY_SESSION_RESTORE` is read and unset". The
   same lines say 870 then "takes `CCY_ATTENDED` as given". `DEBUG_LOG` is read later too: at
   claude-yolo:2403, 2406, 2480 and 2487 (the log is printed and named at exit). The restore idiom
   copies the value into another variable (1208-1217). A literal `unset` would leave
   `CCY_ATTENDED` empty at 870, so the run is unattended. It would also leave `DEBUG_LOG` empty,
   so no log is shown at exit. Say `export -n` (one line: the value is kept and is no longer in
   the environment), or name the copies. Also say which variable carries "the layer answer" (for
   example `layer_input` for 1036). The check against `--settled` has to come after the parse
   (claude-yolo:853), not "at the start". Task 3.1's scenario "`--debug` at layers 1, 2 and 3
   through the screen's `run`" (PLAN:159-161) catches this if it asserts that the exit-time
   `DEBUG LOG (saved to ...)` names the outer run's log. Add that assertion.
2. **The 1090 condition alone still makes a second log.** DESIGN:124-126 says "1090 starts no
   `tee` and keeps the inherited pipe and log". But when `DEBUG_CCY` is true, 1084 runs
   `mktemp` and assigns `DEBUG_LOG`, and 1085 prints `Debug logging to:` before the 1090 `exec`.
   Guarding only 1090 leaves an empty second log file, with `DEBUG_LOG` pointing at it. State
   that the hand-off skips 1084-1085 as well as 1090 (in place, for example
   `DEBUG_LOG=${DEBUG_LOG:-$(mktemp ...)}`), and keep `set -x` (1091) on. The same Task 3.1 layer 1
   scenario catches it if it counts the `ccy-debug-*.log` files (one).
3. **Hand-off names and the migration prompt.** `DEBUG_LOG` is a generic name with no default
   at claude-yolo:599-602. Under C22, any user whose shell exports `DEBUG_LOG` gets every plain
   `ccy` refused with exit 64 (no `--settled`). Hand it over under a `CCY_`-prefixed name, as
   `CCY_GH_TOKEN_HANDOFF` is handed over. Separately, r8's nit still holds: the `run` exec
   re-runs the migration prompt (claude-yolo:471-491) for a person who answered `n`. With
   insulation it is then asked three times (outer, re-run, tmux) instead of today's two. This is
   more questions, against the owner's aim. The hand-off is already present at 471, so
   `[ -z "$CCY_RESTART_RELAUNCH_SESSION$CCY_ATTENDED" ]` (in place, no line) skips it on the
   re-run. Add a Task 3.1 scenario: old `.claude/ccy/sessions` present, answer `n`, launch from the
   screen, prompt asked once before tmux.

## Nits

- N1 (r8, carried): fold the initial value into 870 (`CCY_SEATS="" CCY_ATTENDED=...`), and drop
  `attended` from `ccy_launch_teams`'s `local` (claude-yolo:872), using `$CCY_ATTENDED` at 877
  and 882.
- N2: the re-run prints the steps before the screen a second time on a terminal. These are
  `ccy_env_local_dist_sync`'s warning (claude-yolo:452) and, at layer 1, the
  `Debug logging to:` line, if S2 keeps 1085. Either accept it in DESIGN 2.1 or skip it under the
  hand-off.
- N3: `{CCY_TTY_FD}` is not close-on-exec, so every child of the layer 1 run inherits it
  (agent-bus, the engine, ssh-agent). It is harmless, but worth one clause in DESIGN:109-114.

## Checked and holding

- 857-900, 1014-1093, 1184-1223 and 1237-1343, against DESIGN 2.1-2.4 and C21/C22.
- The front's hand-off of an unknown first argument (`--settled=...`) goes to the bash
  (front.go:283-307, `flagNone`).
- `shellChanged` hands over on `CCY_GH_TOKEN_HANDOFF` (front.go:372).
- Session registry: `--rebuild=* | --update-token=*` at lib/session-registry.bash:134, and
  `CCY_REGISTRY_DROP_FLAGS` at 123-124. A second word in the existing array adds no line.
- The difftest harness runs with no pty: sandbox_test.go:505-511. `x/sys/unix` is already
  imported (fake_test.go:18). `ccy-teams` is already faked (fake_test.go:45).
- The allowlist row is claude-yolo 2782/12/2713 (scripts/bash-limits.allowlist:55). The
  estimate of about 1 over, with C15 behind it, is honest. S1-S3 need no new counted line.
- C1-C22 are taken as choices. None of them causes a defect beyond the items above.
