# Plan 00177 Phase 6: no hidden keys (Tasks 6.1 to 6.3)

Branch `fix/ccy-teams-no-hidden-keys`. Built test first through the teamtest world.

## What changed

- **Menu on Enter (6.1)**: `picker/menu.go`. Enter on any row (team, person, ccy seat, other
  member) opens a `ui.Picker` menu listing that row's actions in plain words with the
  shortcut beside each, e.g. `Open alpha in Element   (Ctrl-E)`. Esc, q or the final
  `Back to the list` row returns to the board without doing anything. The Ctrl keys remain
  shortcuts straight from the board. `keys.go` split into `act` (Enter means the menu) and
  `do` (an action by key). A unit test fails if any of Ctrl-N, R, X, E, P, U and `?` is in no
  row's menu, and that every row has Element, a new session and help.
- **Footer follows the row (6.1)**: `ui.PickSpec.RowHeaders` and `ExitLabel`. The header gains
  `This row: Ctrl-E open alpha in Element   Ctrl-N ...` and an fzf
  `--bind=focus:transform-header^case {n} in ...^` (run with `SHELL=/bin/sh`) swaps it as the
  cursor moves; the Exit row keeps the base header. Refused, not truncated, past 100 000
  bytes of binding (one argument is limited to 128 KiB).
- **Person rows (6.2)**: `board.Snapshot.Sessions` / `SessionsOf` keep the session counts
  (the device cache now stores counts, not words). With at least one session: `Open <team> in Element` is first, then `New password — signs out guest's 2 Element sessions`, which asks
  first, default No, with `This signs out guest's 2 Element sessions.`. With none: `Sign in: set a password (shown once)` is first and asks nothing (nothing to lose). A count that could
  not be read is not treated as none. After any password is shown (and the clipboard
  offer), `Open <team> in Element now?` is asked (default yes).
- **Element returns to the board (6.3)**: `login.LaunchElement` no longer leaves; it starts
  Element as before (own systemd scope, `setsid`, output to the log), calls
  `Board.ForgetDevices(team)` and returns, so the next pass re-reads that team's sessions.
  A failed launch still ends the picker with a failure and the log tail. create's
  after-create offer is unchanged in effect.
- Wording: the human row's WHERE text is now `Element; Enter for choices`; refusal texts that
  said "Ctrl-E offers it again" / "Ctrl-P sets..." say what to do in words. `help.txt`,
  `usage.txt`, `docs/agent-bus.md` (step 2 and the picker paragraph) and `declare.py`'s
  next-steps (and their tests) teach Enter and the menu, not Ctrl-E/Ctrl-P.

## Bash parity suite (`scripts/test-ccy-teams-command.bash`), changed deliberately

Result: 336 passed, 0 failed (`scripts/test-cc-teams.bash`: 58 passed).

- Enter on a seat/team/person now opens the menu, so the suite picks `Attach this terminal`,
  `What the columns`, `Sign in` or `New password` from it; new checks for the menu lines,
  the sign-in without a question, the sign-out count, Back to the list, and the offer to open
  Element after a password.
- After Ctrl-E the picker returns to the list, so those cases queue a final `|ABORT` and
  assert rc=0 only after leaving; new checks that the list is drawn again and the person's
  sessions are read a second time.
- The fake fzf's `EXIT` answer now picks the last row (a menu ends in `Back to the list`).
- Password cases for the owner (no sessions) no longer see the confirmation; the confirmation
  checks run against the owner given a session (`$STATE/devices/owner`).
- `Enter: new password` in the `--list` WHERE column became `Enter for choices`.

## Checks run

gofmt (`golangci-lint fmt --diff`), `go vet`, `go test -race ./...` in `src-go` (94.9 %
statements), `golangci-lint run ./...` in `src-go` (0 issues), the project-rules driver over
`src-go` (clean), the ccy-teams and cc-teams bash suites, and `tests.helpers.agent_bus.test_declare`.

## Caveats for the owner's host test (Task 6.4)

- The per-row footer needs fzf 0.45 or later (`transform-header`). The sandbox fzf is 0.38, so
  the binding was checked only by unit test on the argv and by the fake; a real fzf has not
  run it. The Fedora host's fzf is new enough. If a `fzf: unknown action` error appears, that
  is the binding.
- `scripts/qa-go.bash` stops in the nested `src-go/qa` module on two linter findings in files
  this branch does not touch (`qa/changed/classify.go` gocognit, `qa/internal/proc/proc_test.go`
  mirror); the src-go steps were therefore run by hand, as listed above. Not caused by this
  work, not fixed here.
- Not run: `qa-all.bash` (told not to), `qa-reviewer`. No CCY file changed, so no version bump.
