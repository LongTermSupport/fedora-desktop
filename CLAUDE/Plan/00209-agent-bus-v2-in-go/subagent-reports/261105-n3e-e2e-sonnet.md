# n3e-12, second half: the `--claude` end-to-end tests (tests 1 to 4 built; 5 and 6 not)

Plan 00209 Phase 3, Task 3.2. Worktree base fast-forwarded to `feat/bus-v2-n3e-wire` (`0bf015a14`), `wiring.go` imports `hook`
and `statusline`. Code commit `c207862e6`; documents in the commit after it.

## 1. What is built

In package `wiring` (all test files; no production code touched):

- `e2e_claudecode_test.go`: the fake Claude Code. The test binary hard-linked as `claude` (`TestMain` case `claudeName`); it
  reads `--plugin-dir` and `--settings`, prints `claude <pid> <runtime>`, then answers `<n> <command>` lines on stdin with
  `cc-reply <n> <json>` or `cc-error <n> <text>`. Commands: `begin` (inbox socket `<XDG_RUNTIME_DIR>/cc-socks/<pid>.sock`
  through `fakeinbox`, transcript `<HOME>/.claude/projects/<cwd, non-alphanumerics as '-'>/<uuid>.jsonl` through
  `faketranscript`, each accepted frame recorded as an idle user turn), `hook <Event> [active]` (the command from the plugin's
  `hooks.json`, `sh -c`, payload on stdin, the two inbox variables in the environment), `statusline` (the command from the run
  settings' `statusLine`, `sh -c`), `frames`, `transcript`.
- `e2e_claude_test.go`: the world side (`claudeKit` re-assembles the world's kit with `kit.Assemble`, `claude`, `claudeSeated`,
  `desiredFlags`, `ccAsk`) and the four rows.
- Existing files, exact edits: `e2e_main_test.go` lines 41-42 (case `claudeName`); `e2e_world_test.go`: `desire` (lines 266-272)
  delegates to new `desireInbound` (277-283, one changed line 278: `Inbound: inbound`), `env()` line 388 (adds `CLAUDE_CONFIG_DIR=`,
  so a developer's own configuration directory never reaches a test), `start` (448-451) delegates to new `startCmd` (mode and CMD
  as parameters; lines 453-454), `launch` (488) delegates to new `awaitCmd` (491-497, the CMD name as a parameter).
  None is near PR 128's lines 248 and 281 (the nearest hunk, `desire`, is 8 lines from 281 and ends before it).

## 2. The rows, what each exercises and does not cover

| Test                   | Exercises (real keeper, link, `hook`, `status-line`)                                                                                                                                                                                                                                                                                                                                                                                               | Does not cover                                                                                                                                                                                                |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestClaudeDelivers`   | SessionStart from `hooks.json` answers the exact frame with the socket mode line (the facts come from the link's engine); a human `say` reaches the fake inbox as one accepted user line starting `teamchat delivery <nonce>:` and holding the text; the transcript holds the needle and the text; fakehs gets the `m.read` receipt for the human's event id; `recv` and `inbox` are then empty; no second delivery. DV1, DV3, DV4, DV5, HK1, PK1. | Busy mode, a rotated or truncated transcript, `missing`, membership notices, a peer's item, the 2 s pace, a failed socket write.                                                                              |
| `TestClaudeLinkSilent` | With the link SIGSTOPped (its socket accepts, says nothing): UserPromptSubmit and SessionStart answer the hook's own recovery line (the verbs' exit 69 starting text), Stop and SessionEnd answer `{}`, every exit 0, empty stderr; after SIGCONT the SessionStart is the link's frame again. HK1.                                                                                                                                                 | The 300 ms bound itself (a slow host cannot make the test flaky, so only "it answers, it does not hang" is asserted); a dead link (ECONNREFUSED/ENOENT) and `restarting` with its seconds; "no file written". |
| `TestClaudeStatusLine` | `statusLine.command` of the run settings (the kit's, `<kit>/bin/teamchat status-line`) prints exactly the team colour escape, `👥 alpha: myrepo.2 (w)`, reset, one newline, exit 0, no stderr; then with the keeper stopped and the link killed, exactly the red `👥 teamchat starting` form, exit 0. SL1, PK1.                                                                                                                                    | An inner status line, columns wrapping, several teams, `(down)`/`(stalled)`/`(missing)`/`(failed)` marks, the stale note, `teamchat restarting` with its countdown.                                           |
| `TestClaudeStopBlocks` | A run launched `inbox: refuse` (so every pending item is one the session must read): SessionStart gives the wait-mode frame; Stop with one pending human item blocks with the pending text and the no-waker text; the same set again is `{}`; Stop with `stop_hook_active` true is `{}` and changes nothing; a second item blocks again with the new counts only; the inbox socket was never written. HK2, DV1.                                    | The 10-minute repeats and a failed team's block (the engine's fake-clock tests own them), a guard that cannot be written, items the socket will still push (no block), a down team.                           |

The four rows take about 5 s together; the whole package still takes 202 s with `-race` (176 s with `busfailpoints`), as before.

## 3. Red and green (each shown, then reverted; production files are unchanged in the commit)

The tests assert end-to-end behaviour of code that already exists, so there is no red before the code. The red is a defect put
into the production code, run with `-race -shuffle=on -count=1 -run TestClaude`, then reverted:

| Mutation                                                                                        | Red                                                                                 | Green                 |
| ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | --------------------- |
| `wiring.Verbs`: `hook` and `status-line` swapped (what an unwired or misrouted verb looks like) | all four                                                                            | all four after revert |
| `hook.go`: no answer from the link gives `{}` instead of the recovery line                      | `TestClaudeLinkSilent` only                                                         | yes                   |
| `link/hooks.go`: Stop's pending-set guard never suppresses a repeat                             | `TestClaudeStopBlocks` only ("the second Stop ... answered {"decision":"block"...") | yes                   |
| `kit.go`: the settings name `hook stop` as the status line command                              | `TestClaudeStatusLine` only (it waits its 30 s bound)                               | yes                   |
| `hook.go`: the socket is read from the token variable                                           | `TestClaudeDelivers`, `TestClaudeLinkSilent`, `TestClaudeStopBlocks`                | yes                   |

## 4. Facts in the earlier report that were wrong or incomplete (re-verified against the code)

- A SIGSTOPped link does not give the status line's red `teamchat starting` form: the stopped link still holds `link.lock`, so the
  segment is drawn from `status.json` (it turns stale only after 90 s). The form needs `link.lock` free and `keeper.json` still saying
  `running`: the keeper stopped (so it cannot restart the link or write `restarting`) and the link killed. `TestClaudeStatusLine`
  does that. The SIGSTOP of the link is right for the hook row (the hook probes `keeper.lock` and `keeper.json`, both still true).
- A launch with `--state` and `--seat` and no `--desired` is `inbound: refuse` (`keeper/prepare.go:115`): the delivery rows must use
  `--desired` with `accept` (`desiredFlags`). Under `--seat` the frame and the delivery are wait mode.
- In a run that refuses the inbox the first Stop block also carries the no-waker text (delivery is `wait`, no `wait` running), not the
  pending text alone; the second block (a new set, within 10 minutes) has the pending text only.
- The status line's `settings` lookup and the keeper read `CLAUDE_CONFIG_DIR` from the environment: the world now sets it empty,
  or a developer's own value would send the fake's transcript into a real configuration directory.
- Lint: `tagliatelle` refuses the snake_case JSON tags of Claude Code's payload outside the listed packages, so the payloads are
  `map[string]any`; the token field must not be named `.secret` (the secret-file guard refuses the token); no `//nolint` anywhere.

## 5. Gates and tests, exactly (container, `FAKEHS_TUWUNEL_BIN` unset)

| What                                                                                          | Result                                                     |
| --------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `go test -race -shuffle=on -count=1 -run TestClaude` wiring (four rows, several runs)         | ok, about 5 s                                              |
| `go test -race -shuffle=on -count=1` whole wiring package, default tags                       | ok, 202 s                                                  |
| `go test -race -shuffle=on -count=1 -tags busfailpoints` whole wiring package                 | ok, 176 s                                                  |
| `golangci-lint run` and `go vet`, wiring, default tags and `--build-tags busfailpoints`       | clean                                                      |
| `scripts/qa-changed.bash` (on the test commit's change, with the two documents also modified) | passed: docs, go-qa on both modules, teamchat-keeps-off-v1 |
| `-tags tuwunel`                                                                               | not run: `FAKEHS_TUWUNEL_BIN` is unset, no Tuwunel here    |
| `scripts/qa-all.bash`                                                                         | not run (not allowed here)                                 |

`qa-changed` did not end red in this container (the node_modules and compose-restore cases of issues 135 and 136 were not picked).

## 6. Not built

- Test 5 (OF1, one team's server down): needs a second team (`w.join`) and the 15 s offline mark in real time, which this package
  cannot afford cheaply; and this agent's context passed the 300,000-token soft budget. The design in the earlier report's 3.3 stands;
  the new world helpers make it a small row (`w.claude` with both seats, `hook UserPromptSubmit`, `statusline`, fakehs stopped).
- Test 6 (held fake inbox, P8's shape): not built; the fake has no `hold` command yet (`fakeinbox.Hold` is one line to add).
- PT1 (n3e-13): not started; it needs these rows, which exist now.
- No `missing` row, by design (180 s of real quiet; the engine's fake-clock tests own it).
