# n3e-12: the hook and status-line verbs wired; the `--claude` end-to-end tests and PT1 still to build

Plan 00209 Phase 3, Task 3.2. Scoping read first: the coordinator's n3 scope note; every claim relied on was
re-checked against the code and SPEC-n3 (nothing found different).

## 1. Built (code commit `1726c5c4d`)

Slice A: the member binary's `hook` and `status-line` verbs are built.

- `wiring.Verbs()` passes `hook.Main` and `statusline.Main`: the two packages' first production callers.
  `cmd/teamchat` needed no edit (it passes `wiring.Verbs()` already).

- `cli.Outside()` names both; the `"n3e"` row of `cli`'s `unbuilt` table is gone. Two constants, `verbHook` and
  `verbStatusLine`, because goconst counts three uses of each string.

- The CL2 test, `go test -tags teamchatrelease -count=1 ./internal/teamchat/member/cli/`, before: six verbs
  (hook, status-line, report, ask, thread, questions). After: FAIL on exactly four, each reported unbuilt, slice n3f:

  ```
  --- FAIL: TestNoVerbIsUnbuilt
      release_test.go:11: teamchat report is still unbuilt (slice n3f)
      release_test.go:11: teamchat ask is still unbuilt (slice n3f)
      release_test.go:11: teamchat thread is still unbuilt (slice n3f)
      release_test.go:11: teamchat questions is still unbuilt (slice n3f)
  ```

- Tests, written first and seen red (before the code), then green:

  - `cli/verbs_test.go`: `known` loses `"n3e"`; the Outside list test (renamed
    `TestOutsideAreTheHooksN3csVerbsAndTheSends`) gains `"hook", "status-line"`; the unbuilt list test loses the n3e row.
  - `cli/run_test.go`: `hook stop` and `status-line` with no wired verbs are "not wired into this binary", no longer
    "lands with slice n3e".
  - `wiring/wiring_test.go`: `TestVerbsCarryTheHookAndTheStatusLine`.
  - `cmd/teamchat/main_test.go`, through the binary's real `run`: `TestEveryHookEventIsBuiltInTheBinary` (each of the four
    events, no `TEAMCHAT_RUNTIME`: `{}\n`, exit 0, empty stderr), `TestTheStatusLineIsBuiltInTheBinary` (no session, no
    inner line: zero bytes, exit 0), `TestTheN3fVerbsAreStillUnbuilt` (ask, thread Q1, questions, report: exit 1, message
    "is not built in this binary; it lands with slice n3f").
  - Red run before the code: cli 4 tests failed, wiring 1, cmd/teamchat 2 (the 4-verb test passed, as intended: it guards).

- Mutation proofs (each applied, seen red, reverted, green again):

  1. swapped the two handlers in `wiring.Verbs`: both cmd tests red (hook exits 0 with no output; status-line exits 64).
  2. wired an n3f verb (`"ask"`): `TestTheN3fVerbsAreStillUnbuilt`, `TestRunPassesTheVerbsBuiltOutsideCLI` and wiring's
     `TestVerbsAreTheVerbsBuiltOutsideCLI` red.
  3. put the hooks back in `unbuilt`: four cli tests and the cmd verbs test red.

### Lines changed in files PR 128 also edits (kept to the verbs; nothing reformatted)

Placement was chosen so that git sees no adjacent hunk to PR 128's; the one visible difference is that the two names sit
first in `outside` (the order of that list carries no meaning).

- `cli/run.go`: line 45 added (`verbHook, verbStatusLine,` first in `outside`); line 54 removed (`"n3e": {...}`).
- `cli/verbs.go`: lines 125-130 added (a comment and a two-constant block); table rows `hook` and `status-line` (lines 224-225,
  were 218-219) use the constants. PR 128 inserts at the `reach` row and the const block at `verbPlaceIssued`: not adjacent.
- `cli/verbs_test.go`: line 49 (`known`); lines 73-77 (test comment, renamed test, the added list line 77); line 85 and 88-90
  (`Outside()[0]` is `"hook"`; unbuilt-list comment); line 92 removed. PR 128 changes the list line `"tls", "reply", ...`
  (old 77) and `bVerbs`/usage rows: expect no textual conflict, but the want list there must become
  `"hook", "status-line", "run", ..., "tls", "reply", ...` after merge.
- `cli/run_test.go` (PR 128 does not touch it): line 71 changed, one row added.
- `wiring/wiring.go`: imports at 15 (`hook`) and 18 (`statusline`); map entries at 30-31. PR 128 adds `tlsverb` after `teamsync`
  and `"tls"` after `"reach"`: not adjacent.
- Also touched: `hook/hook.go` and `statusline/statusline.go` (one doc comment each: `wiring.Verbs`, not `cmd/teamchat`),
  `cmd/teamchat/main_test.go`, `wiring/wiring_test.go`.

## 2. Gates and tests, exactly (container, FAKEHS_TUWUNEL_BIN unset)

| What                                                                                               | Result                                                        |
| -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `go test -count=1` cli, cmd/teamchat, wiring `-run TestVerbs\|TestNewTeam\|TestATeam\|TestTheSeat` | ok                                                            |
| `go test -race -shuffle=on -count=1` cli, cmd/teamchat, hook, statusline, kit                      | ok                                                            |
| `go test -race -shuffle=on -count=1` wiring, whole package (171 s, every end-to-end test)          | ok                                                            |
| `go test -tags teamchatrelease -count=1` cli                                                       | FAIL on the four n3f verbs only (above)                       |
| `go vet` member/..., cmd/teamchat                                                                  | clean                                                         |
| golangci-lint cli, wiring, hook, statusline, cmd/teamchat                                          | clean                                                         |
| `scripts/qa-teamchat-separate.bash`                                                                | passed                                                        |
| `scripts/qa-changed.bash` (code commit)                                                            | passed, 9 files: go-qa on both modules, teamchat-keeps-off-v1 |
| `-tags tuwunel`                                                                                    | not run: `FAKEHS_TUWUNEL_BIN` unset, no Tuwunel here          |
| `-tags busfailpoints`                                                                              | not run: no failpoint code touched                            |
| `scripts/qa-all.bash`                                                                              | not run (not allowed here)                                    |

## 3. NOT built: slice B (the `--claude` end-to-end tests) and PT1

I stopped here because this agent's context passed the 300,000-token soft budget the hooks daemon sets, so a fresh agent
should continue from this section. A draft of the fake Claude Code is kept at
`untracked/scratch/wip-e2e_claudecode_test.go.txt` in the agent's worktree (untracked, never committed: it fails lint until
its tests exist). Everything it needs is below.

### 3.1 Facts established (verified in code)

- A `--claude` run's CMD must have an argument whose base name is `claude` (SPEC-n3 CL5). The world's `agent` file is the test
  binary hard-linked; a `claude` link is needed and `TestMain` (`e2e_main_test.go`) gets a `case "claude"` that calls the fake.
- The keeper launches CMD with `--plugin-dir <kit>/plugin/teamchat --settings <run>/settings.json`
  (`agent/agent.go`, `claude.Prepare`); `<kit>/settings.json` must exist (the kit must be assembled by `kit.Assemble` into an
  empty, user-owned, not group/other-writable directory; its path matches `^[A-Za-z0-9/._-]+$`), and its `statusLine.command`
  is `<kit>/bin/teamchat status-line`. The world's kit is a bare `bin/teamchat`: a Claude world re-assembles it (remove
  `<dir>/k`, make the directory 0700, `kit.Assemble(w.kit, paths.KitAt(w.kit), testfiles.Executable)`), keeping the hard link
  to the binary made before.
- The link accepts a hook only from a process that descends from the keeper (`link/facts.go`, LN7): the test process cannot run
  the hooks; the fake CMD must, as `sh -c <command from hooks.json>` with the payload on stdin and the environment variables
  `CLAUDE_CODE_MESSAGING_SOCKET` (`hook.EnvInboxSocket`) and `CLAUDE_CODE_MESSAGING_TOKEN`.
- The socket must be `<XDG_RUNTIME_DIR>/cc-socks/<pid>.sock` with `pid` on the chain (the fake's own pid; `paths.InboxSocket`),
  the transcript `<HOME>/.claude/projects/<cwd with every non-alphanumeric as '-'>/<session-id>.jsonl` (`link/facts.go`
  `checkTranscript`), the session id a lowercase UUID, `cwd` the keeper's (`start` runs it in `w.dir`).
- Under `--claude` the team goroutines wait for the first SessionStart (`FirstSessionStart`), so `seatedIn()` comes after the
  SessionStart hook.
- `keeper.startCmd` gives CMD the keeper's environment plus `TEAMCHAT_RUNTIME`, `PATH` and the kit variable, and the keeper's
  stdin: the test can drive the fake over the pipe `s.stdin`, with replies on the shared stdout.
- A real-time `missing` takes 180 s of quiet after the Stop hook (`engine/proof.go` `quietAfter`, no override, one hour cap):
  too long for this package (it already runs 171 s). Do not build a `missing` row on the wall clock; the engine's fake-clock tests
  own the timing. A deterministic Stop-block row exists instead (3.3, test 4).
- Receipts: `w.hs.fake.Receipts()` (type `fakehs.Receipt{UserID, RoomID, Type, EventID}`).
- Status segment: `<colour.SGR(colour.Auto("alpha"))>` then `👥 alpha: myrepo.2 (w)` then reset, one row, no inner line when the
  home has no settings (`statusline/segment.go`).

### 3.2 The fake Claude Code (the draft, to finish)

A process in `package wiring` that reads `--plugin-dir`/`--settings`, prints `claude <pid> <runtime>` (so `launch`'s first-line
parse works), then answers `cc-reply <n> <json>` to each `<n> <command>` line on stdin. Commands: `begin` (fakeinbox at
`cc-socks/<pid>.sock` with `fakeinbox.Options{Token, Clock: clock.System()}`, `faketranscript.New`, `inbox.OnAccepted` records
each frame with `tr.Record(content, faketranscript.Idle, 0)`), `hook <Event>`, `statusline`, `frames`, `transcript`, `hold`,
`release`. The draft also needs these lint fixes (no `//nolint` allowed): `errors.AsType` result used (`errcheck`); the const
named `ccToken` trips gosec G101 (rename); `os.ReadFile(path)` G304 and `os.MkdirAll` G703 (read through
`testfiles.Try`/an `os.Root`, as the world does); `musttag` wants JSON tags on the type marshalled (mirror
`fakeinbox.Frame` locally).

World edits (all in the new files except two small ones in `e2e_world_test.go`, far from PR 128's lines 248 and 281): `start`
becomes `startCmd(mode, cmd, flags...)` with `start` delegating (`--plain`, `w.agent`); `launch` splits into `awaitCmd(s, name)`;
`desire` delegates to a variant taking the inbound value (`state.InboundRefuse` for test 4). Do not add bundle or matrix
literals (PR 128 changes them). Test names stay short: the socket path limit is 107 bytes.

### 3.3 The tests to write (each: red, green, one mutation; say what it does not cover)

1. **Delivery** (DV1, DV3, DV4, DV5, HK1, PK1): `--claude`; `begin`; `hook SessionStart` answers the frame
   (`proto.FrameText` plus the mode line); a human `say`; the fake inbox gets one frame holding the human's text and
   `teamchat delivery <nonce>:`; the transcript holds it; the receipt for the human's event id appears on fakehs; `inbox` and `recv`
   are empty. Does not cover Busy mode, a rotated transcript, `missing`, membership notices.
2. **Hook with the link not answering** (HK1's 300 ms): SIGSTOP the link (pid from keeper.log, `s.links()`), then `hook UserPromptSubmit` and `hook SessionStart` answer the hook's own recovery line (`hooktext.Recovery(client.ReadDown(run)...)`),
   `hook Stop` and `hook SessionEnd` answer `{}`, every exit 0; SIGCONT in a cleanup registered after the session's (so it runs
   first: a stopped link cannot see its keeper die and the session cleanup would hang). A SIGKILL row is racy: the keeper
   restarts the link after 1 s.
3. **Status line through the kit's command** (SL1, PK1): the run settings' `statusLine.command` shows `alpha: myrepo.2 (w)` in
   the team's colour; with the link stopped (SIGSTOP) the red `teamchat starting` form; exit 0 both times.
4. **Stop block** (HK2, deterministic): `desired.json` with `inbound: refuse` (delivery lowered to `wait`), a human item pending,
   `hook Stop` blocks once (`hooktext.Block(hooktext.StopPending(...))`), the second Stop with the same pending set answers `{}`,
   a new item blocks again. Does not cover the 10-minute repeats (fake clock tests own them).
5. **OF1 with one team's fakehs stopped** (needs a second team: `w.join`, as `TestE2EOfflineServer`): after `down`, the prompt hook
   carries `hooktext.TeamDown(alpha)`, Stop does not block for it, the status line marks alpha `(down)` while beta delivers.
6. Optional: a held fake inbox for 6 s then released (P8's shape in real processes).

Run each with `-race -shuffle=on` and under `-tags busfailpoints` (src-go/CLAUDE.md), then golangci-lint (also `--build-tags busfailpoints`), `go vet`, `scripts/qa-changed.bash`.

### 3.4 PT1 (n3e-13)

Not started; needs 3 first. New package `member/pt1` (tag `busfailpoints`): a seeded driver over real keeper and link processes
interleaving sync batches, `recv`/`wait`, deliveries, fakeinbox hold and refuse, fakehs drop and restart, n3d's sends and a SIGKILL
at each failpoint (`failpoint/names.go` already names n3e's five); each run prints its seed, a failing seed replays it, a fixed
seed list runs in the gate, `-pt1.random=<n>` is the manual long run; P1-P11 checked after each run (SPEC-n3 section 11); show it
red on a planted defect. SPEC-n3 PT1, B 2.15, handoff section 15, n3e-D19.

## 4. Documents

`PLAN.md` Task 3.2 and `BUILT-n3.md` (a new n3e record) now say what is true: verbs wired; end-to-end tests, PT1 and n3f open.
`PLAN.md` shrank by 22 bytes. SPEC-n3 needed no change (it states the behaviour, not who calls what).

## 5. Other findings

- The class that let n3e be ticked built (a package built and tested but never wired) has a detector, the CL2 test, that no
  gate runs; it stays failing until n3f's four verbs land. Putting it in a gate before then needs a shrinking allowlist of unbuilt
  verbs, which is the owner's call (Defence Before Fix); left alone.
- `go test` of the whole wiring package takes 171 s with `-race`: any new end-to-end row must be cheap.
