# Plan 00201 M5 review (adversarial, read-only)

Scope: `git diff F44...agent-a8e28bc3bf97f0ea0-51f6110c` (commit f30b370e plus the F44 merge).
Builder report: `261015-m5-many-teams-sonnet.md`. Spec: `SPEC-many-teams.md` S-B3, S-B6, S-B10 to
S-B13, S-C1 to S-C9.

## What was run

- `go test ./internal/teams/...`: all packages pass.
- `tests/helpers/pingbus/test_cli_infer.py`, `test_hooks.py`: pass (real fake-Matrix path, `BusCase`).
- `scripts/test-ccy-teams.bash`: 163 pass, 0 fail. `scripts/qa-bash-limits.bash`: green, the
  `agent-bus-seats.bash` row 62 -> 61 code lines is honest (two statements folded, one added).
- `.claude/hooks/handlers/status_line/tests/test_bus_teams.py`: **could not be run here**. System
  python lacks `pydantic`, the daemon venv lacks `pytest`, and no QA gate (`qa-gates.tsv`,
  `qa-routing.tsv`) routes `.claude/hooks/handlers/**` to a check. The builder's "14 pass" is unverified
  and will stay unverified on every future change.
- semgrep `--lang bash --pattern 'echo $X'` on copies of the launcher: `claude-yolo` F44 = 1
  PartialParsing error (line 17, the `CCY_VERSION` comment), M5 = 1 (same line). `agent-bus-seats.bash`
  F44 = 0, M5 = 0. No parse regression; the one error predates this branch.

## Findings

### F1. Status-line segment reads a per-session fact from a shared process (deviation 7) -- HIGH

`.claude/hooks/handlers/status_line/bus_teams.py:33` reads `os.environ["PINGBUS_TEAMS"]`. The daemon
is one process per project (daemon's own docs: `handlers/status_line/CLAUDE.md` "multiple sessions
share one daemon"; `Architecture/StatusLine.md:397`), lazily started by whichever hook fired first
(`.claude/init.sh:1973-1978`, no env scrubbing: it inherits that hook's, i.e. that session's,
environment). So:

- Inside a ccy container (one session per container) the variable happens to be right, which is why
  it looked fine.
- On the host, where M6 puts `cc --teams` on the same flow (`cc-teams.bash:194` runs `pingbus seat exec`), a plain session started first means every later `cc --teams` session shows **nothing**; a
  teams session started first means every later plain session (and every session on other teams)
  shows **its** teams. `hooks-daemon restart` from any session re-stamps the daemon with that
  session's teams. The segment then states a falsehood in the one place "that stays in view for the
  whole session".

`PINGBUS_TEAMS` is also refused by the entrypoint from the launcher (`entrypoint.sh:574-576`) and is
derived per session by `pingbus seat exec` (`seat.py:355`): it is by design a session-process fact,
never a daemon fact.

Per-session source that does reach the handler: the Status JSON carries `session_id` (and
`transcript_path`, `workspace.*`; `StatusLine.md:183` lists `session_id` as already read by daemon
handlers). Minimal fix: have the pingbus SessionStart hook (`helpers/pingbus/hooks.py:376`, which
runs in the session's own environment and has the hook's `session_id` on stdin) write
`{team_list}` to a per-session file keyed by session id (for example under
`$PINGBUS_HOME/sessions/<session_id>` or the existing seat lock directory), removed at SessionEnd;
`BusTeamsHandler.handle(hook_input)` reads `hook_input["session_id"]` and that file, never the
environment. The existing `test_bus_teams.py` then has to feed `hook_input` with a session id, which
also makes it test the real seam instead of `monkeypatch.setenv`.

### F2. `--to` inference is asymmetric when one team's record is not yet verified -- MEDIUM

`helpers/pingbus/cli.py:766-783` `_fits`: an `Untrusted` cached record means "fits nothing". A
fresh two-team session (no `team.json` yet for beta because nothing has synced it) with a handle
that exists on both teams: alpha fits, beta is unknown, `say --to peer.1` goes to alpha silently.
The message was meant for an ambiguous name and the tool chose. When both are unverified, every
`--to` fails with "fit more than one (or none)", which is misleading: the cause is "no verified
record yet", not ambiguity. Fix: treat `Untrusted` as unknown, not as a non-fit: if any active
team's record is unknown, do not infer from `--to`; fail with the choices and append
`  TEAM  (no verified team record yet: run pingbus recv or name it with --team)`. Test: one team
with a record, one without, a shared handle, expect exit 64.

### F3. Blocked-mark bookkeeping forgets a "Leave out" -- LOW

`chooser.go:180-193`: `held` is set only when `take` says again; when `settle` says again (every
team left out) `held` is not reset. Scenario: round 1 marks gamma (excluded) + beta (widen): gamma
blocked, `held=[beta]`. Round 2 marks delta (widen): taken `[beta, delta]`, both "Leave out",
`settle` again. Round 3: `take(..., held=[beta])`: beta is hidden from the list and silently back
in `taken` (`list.go:78` `judgement{taken: slices.Clone(held)}`), so the scope question for beta is
asked again after it was declined. Consent is intact (it is asked again, nothing is applied without
a yes) but the owner's answer was dropped. Fix: `held = nil` before `continue` at `chooser.go:192`,
with a test of exactly this sequence.

### F4. Consent on widening -- OK, verified

`widen.go:40-73`: each widen team gets its own gum question; `adds` are applied only after the loop
(`widen` at :70), so Esc on the second question (`ui.ErrCancelled` -> `nothingChosen`, exit 0)
applies nothing for the first. Esc on the list after kept marks: `ErrLeave` -> exit 0, nothing on
stdout (`TestEscAfterMarksWereKeptTakesNothing`). An empty fzf answer -> exit 1 at `list.go:44`. EOF
on gum surfaces as an error -> exit 1. Three rounds shared by both loops, then exit 1. S-B6, S-B7,
S-B9, S-B10, S-B13 hold.

### F5. `describe-seats --block` failure leaves no "Not starting" line -- LOW

`agent-bus-seats.bash:88`: the block is the function's last statement and its status propagates to
`claude-yolo:3883` (`|| exit $?`), so it fails fast (test rc=70 pins it). But the header comment
(`:70-73`) still says the status is "seat take's code", and on this path the owner sees only
ccy-teams' stderr and a silent exit, not `print_error "Not starting: ..."`. Fix: `|| { print_error "Not starting: ccy-teams describe-seats --block failed (exit $?)"; return $rc; }` style, within
the ratchet (one statement), and the comment updated.

### F6. The host `cc` launcher prints no block -- INFO (M6 scope)

S-C4 says "the launcher prints"; `cc-teams.bash:99` takes seats and prints nothing. M6 (Task 2.6)
owns it; record it there so it is not lost.

### F7. Status-line handler is outside every QA gate and untestable from this checkout -- MEDIUM

See "What was run". Either route `.claude/hooks/handlers/**/tests` through a gate that runs with the
daemon venv (the existing `pre_tool_use/tests` have the same gap, so this is a pre-existing class),
or at minimum record in the plan that the owner's host run must include `hooks-daemon restart` and
`health` and a look at the status line of a two-team session (deviation 7 already asks for it; the
meta-deploy entry does not mention it).

### F8. `--to-coordinator` alone never infers (deviation 6) -- INFO

`cli.py:793`: `fitting` is built from `args.to` only. A worker on two teams replying to "its"
coordinator with `--to-coordinator` always gets exit 64. Spec Q6 only names `--to`, so this is
within spec; the SKILL.md text should say so, since the REPLY line `recv` prints for a ping may
use `--to-coordinator` and `--re`, and `--re` does resolve it. Verified the REPLY path still works
through `--re` (`test_send_re_an_event_of_one_teams_room_goes_to_that_team`).

### F9. Versions and changelog -- OK

`CCY_VERSION` 3.97.7 -> 3.97.8, `REQUIRED_CONTAINER_VERSION` and the Dockerfile label 2.57 -> 2.58
together; the container bump is warranted (the pingbus zipapp in the image is built from
`helpers/pingbus`, which changed). `docs/ccy-changelog.md` entry present; meta-deploy entry rewritten
for 3.97.8. The PLAN.md status line (`:3`) still reads "M4 shared chooser next" while Task 2.5 is
ticked: drift, one line.

### F10. Payload on stdout -- OK

`block()` writes to `c.Host.Stdout`; the launcher redirects it with `>&2`
(`agent-bus-seats.bash:88`), and the test "the launch block is shown on stderr" pins it. The seat
list stays the only stdout of `ccy_seats_check`. `print_error` with an embedded newline goes through
`echo -e ... >&2` (`common-pure.bash:26`): both lines on stderr.

## Verdict

**MERGE AFTER FIXES**: F1 (the segment must read a per-session source keyed by `session_id`, not the
daemon's environment, or be withdrawn until M6 provides it), F2 (unknown record is not a non-fit),
F3 (`held = nil` when settle says again), F9's PLAN.md status line. F5, F7 and F8 can follow in the
same milestone's fix-up; F6 is M6's.
