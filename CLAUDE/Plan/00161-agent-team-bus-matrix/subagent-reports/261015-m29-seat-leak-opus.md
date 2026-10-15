# M2.9 seat leak: a plain ccy carried the previous session's seat

## Verdict

Product bug in the launcher, not the acceptance harness.

## Evidence (host run 26-10-15, captures under `untracked/plan-runs/`)

- M2.8 ran `ccy --headless --no-restore --no-supervise --teams accc@<team>` (session accc2) in
  the checkout. Every launch writes `.claude/ccy/.last-launch.conf`, including
  `LAST_TEAMS="${CCY_SEATS}"`.
- M2.9 then ran `ccy --headless --no-restore --no-supervise --prompt ...` with no `--teams`.
  Its `session.err` shows the Quick Launch block: "Found previous launch configuration",
  "Team: accc@<team> (worker)", "Headless launch: using the previous configuration", then
  "agent team bus: accc@<team>". The container's labels hold `ccy-seats=accc@<team>`.
- The harness started a new container (it was not in `containers-before`), so nothing was
  reused by the harness. The seat came from the saved Quick Launch file.

## Cause

`files/var/local/claude-yolo/claude-yolo`, the Quick Launch block: Plan 00177 Task 7.2 (CCY
3.96.0) made the accepted previous configuration replay `LAST_TEAMS`, and 3.96.1 excluded only a
restore. A headless launch and a launch with no terminal accept the configuration without
asking anyone, so they took the saved seats too. This contradicts DESIGN.md D38 (a headless
launch takes the saved token, keys and network) and section 5's rule that a team is given by
each launch or not at all. `scripts/test-ccy-teams.bash` asserted the wrong behaviour
("headless, a saved team: it is launched into").

## Fix (CCY 3.97.3, image unchanged)

- The three unasked paths (restore, headless, no terminal) set `use_config=unasked`. The case
  accepts it like `Y`, so token, keys and network are still taken.
- The saved seats are taken only when `use_config` is not `unasked`, which means a person
  answered yes to the offer that showed the team. `--teams` still wins. The restore exclusion
  is covered by the same test, so the separate `SESSION_RESTORE` condition is gone.
- The unasked messages say "but not its team (only --teams joins one)" when a team was saved.
- The launcher's measured size did not grow (bash-limits ratchet).
- `docs/ccy-changelog.md` 3.97.3, `docs/ccy.md` (the team section and the `--headless` row),
  Plan 00177 Task 7.2 narrowed, Plan 00161 PLAN.md M2 defect line and journal entry.

## Tests

- `scripts/test-ccy-teams.bash`: these tests were added or changed first. Headless with a saved
  team: no team, nothing replayed or recorded, and the other choices still taken. With no
  terminal: no team. The interactive yes still takes the team and its multi-seat list. The
  run was red with 5 failures, then green with 161 passed.
- `./scripts/qa-changed.bash` on the diff.

## Review fixes (Fable, MERGE AFTER FIXES)

- End of input at the Quick Launch prompt was a yes: the failed `read` left the answer empty,
  which defaulted to `Y`, and joined the saved seat. It now counts as not asked, so the
  session gets no team ("No answer (end of input): ... but not its team").
- The test's fake `read` always failed, so the "interactive yes" tests were exercising that
  EOF path. It now answers from `QUICK_ANSWER` (unset is end of input), and the suite covers a
  typed `y`, a bare Enter, a typed `n` and end of input.
- The summary printed "Team: <seat> (worker)" on unasked launches. It now prints "Team: none"
  for them; `--teams` given or an interactive offer still show the seat and role.
- `docs/ccy.md` says a restore replays `--teams` from the session's own registry record.
- To stay within the bash-limits ratchet, the token and key loads are unconditional (both
  are empty on entry) and a bare Enter is matched in the `case`. The code-line record
  tightened from 2839 to 2832.
- Red first: 4 failures (EOF joined the seat, recorded it, gave no message; the summary named
  the role), then 167 passed.

## Parser fix (semgrep tree-sitter-bash)

semgrep rejected the whole launcher at line 1 after this change. The construct was the
two-assignment command `SPECIFIED_NETWORK="$LAST_NETWORK" NETWORK_FROM_CONFIG=true` as the
body of the `if [[ -n "$LAST_NETWORK" ]]` in the Quick Launch `case` arm. It is now one
and-list, `[[ -z "$LAST_NETWORK" ]] || NETWORK_FROM_CONFIG=true SPECIFIED_NETWORK="$LAST_NETWORK"`,
with the same behaviour. The code-line record is now 2830.

## Real-path proof

These are harness tests of the extracted Quick Launch block against fakes. The proof on the
real path is the host's M2.9 acceptance re-run (`acceptance.bash` through `meta-deploy.bash`),
which launches a real headless plain ccy after a `--teams` session and reads its
`ccy-seats` label.

## Host

The ccy play (`playbooks/imports/play-claude-yolo.yml`) and `acceptance.bash` are already in
`CLAUDE/Plan/meta-deploy.list`, in that order. The next `meta-deploy.bash` run installs 3.97.3
and then re-runs the acceptance.
