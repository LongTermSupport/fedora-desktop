# Plan 00201 M4 review (adversarial, read-only): `ccy-teams choose-teams`, scope widening, ccy wired

Reviewed `git diff F44...agent-af31157b95eaa0f4b-09c5327a` (commit 888a68df, merge fa6c9dd1) against
SPEC-many-teams.md S-B1 to S-B15 and the builder's report `261015-m4-chooser-sonnet.md`.

## What was checked and held

- **Trust (1).** Widening needs three explicit acts on a terminal: a team row taken in fzf, the
  `Add` answer in `gum choose`, then sudo for the play. `Chooser.Run` refuses when stdin or stderr
  is not a terminal (`chooser.go:77`), and the launcher calls it only when `attended=true`
  (`claude-yolo:846-850`): headless, no terminal, `CCY_SESSION_RESTORE` and a restart all leave
  the old `seat check --resolved` path, which refuses a team alone. Ctrl-C/Esc/EOF on any screen
  map to `ErrLeave`/`ErrCancelled` (nothing printed, exit 0) or a failure; no path reads as yes.
  `add_repo` validates lower-case OWNER/REPO, refuses an already-trusted repo (64), an excluded
  repo (78), the repos limit (78), a vaulted or commented `repos:` line (78), re-parses the file
  and compares the entry to the expected one before `write()` (backup + atomic replace). The
  repository is the checkout's `origin` lower-cased, so a fork widens to the fork, which is what
  the checkout is, and the prompt names it.
- **Payload (2).** `NewAppFor` gives `choose-teams` a diverted host: `Stdout=os.Stderr`,
  `Payload=os.Stdout`. `Ops.Stdout` comes from `h.Stdout`, so `runPlay` and the Element play
  (`ops.go:103,293`) write to stderr; the wizard's CHANGED marker goes through `c.term` in
  chooser mode; gum/fzf stdout is captured, not inherited. Only `printList` writes to `Payload`.
- **M2.9 (4).** `ccy_launch_teams` runs only under `TEAMS_GIVEN=true` (`:865`) or Quick Launch's
  `LAST_TEAMS` on a yes (`:1268`); a plain ccy never reaches the chooser or agent-bus.
- **Merge (7).** `git diff 888a68df fa6c9dd1` on the launcher is exactly the M2.9 hunk from
  F44; changelog carries 3.97.5 and 3.97.6; allowlist counts match (`qa-bash-limits.bash` green).
  Nothing hidden.
- **Tests (6).** Go: 29 chooser tests through `teamtest.World` with real `git`/`declare.py`/
  `systemctl` fakes and the real launcher-side contract; Python 99 green incl. `ChoicesTest` and
  `AddRepoTest` on real file edits; `test-ccy-teams.bash` 156 green through the real launcher.
  CCY 3.97.6 bumped, changelog entry present, container unchanged (correct: no Dockerfile change).

## Findings

| #   | Where                                                                     | Scenario                                                                                                                                                                                                                                                                                | Severity | Fix                                                                                                                                                                                                                                |
| --- | ------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `src-go/internal/teams/picker/offers.go` (unchanged); PLAN.md Task 2.4    | S-B15 (picker's first pass: "Start a ccy session here (choose its teams)?" in an uncovered checkout, one question in a covered one) is in M4's scope per SPEC line 265 and is not built, yet the plan marks Task 2.4 done for "S-B1 to S-B15" and the report lists no deviation for it. | Medium   | Build S-B15 in a follow-up commit before closing Task 2.4, or record it as deviation 8 and move it to M5 explicitly in PLAN.md.                                                                                                    |
| 2   | `CLAUDE/Plan/meta-deploy.list:23-27`                                      | The ccy play entry still says "installs CCY 3.97.5"; after this merge the same run installs 3.97.6 with the new `ccy-teams` binary and the chooser. CLAUDE.md requires the queue to be exact in the same commit.                                                                        | Medium   | Update the entry's description to 3.97.6 and name the chooser (Plan 00201 M4) in the same PR.                                                                                                                                      |
| 3   | `docs/agent-bus.md:62-66`                                                 | Still documents the removed flow: bare `--teams` "finds the team for you (`ccy-teams for-checkout`)... with none it says so and stops". The grep for leftovers (5) found no code callers (cc, ccy-sessions, tests clean); this is the one stale doc.                                    | Medium   | Rewrite step 3 to describe `choose-teams`: the list with descriptions, scope offer, create, seat.                                                                                                                                  |
| 4   | `files/var/local/claude-yolo/lib/agent-bus-seats.bash:53` with empty list | Bare `--teams --headless`: agent-bus says "the seat list is empty: give <seat>@<team>..." and the launcher adds "--teams '' refused by agent-bus seat check (exit 64...)". Correct, but neither says the cause: a bare `--teams` needs a terminal to choose.                            | Low      | In `ccy_launch_teams`, when `attended=false` and `TEAMS_LIST` is empty, one `print_error` line "a bare --teams needs a terminal to choose a team; name the seat: --teams SEAT@TEAM" before the check (one line, fits the ratchet). |
| 5   | `src-go/internal/teams/chooser/list.go:66-78` (`created`)                 | A team made from the list is returned as `StandingCovers` without re-reading `choices`; the wizard lets the owner change the scope, so a team that does not cover the checkout skips the Add/Join/Leave question and goes to the seat silently.                                         | Low      | After the wizard, call `c.Declare.Choices(ctx, repo)` again and take the new team's real standing (or at least assert it covers and say so otherwise).                                                                             |
| 6   | `src-go/internal/ui/ui.go:117-122` (`cancelOrFail`, pre-existing)         | Any non-zero gum exit (a bad option, gum crash) is read as cancel: the scope question then ends with "No team chosen" and exit 0 instead of a failure. Safe direction (nothing starts, nothing widened) but hides a tool defect.                                                        | Low      | Treat only 1 and 130 as cancel; other codes are errors. Pre-existing, not an M4 regression.                                                                                                                                        |
| 7   | `src-go/internal/teams/chooser/widen.go:40` default `Add`                 | Enter on the scope question widens trust (spec Q4 chose this). Acceptable, but the preceding sentence is the only warning and it scrolls with fzf's redraw.                                                                                                                             | Info     | None required; consider repeating "every member then trusts REPO" in the gum header.                                                                                                                                               |

## Verdict

**MERGE AFTER FIXES**: findings 2 and 3 (deploy queue text, stale doc) before merge; finding 1
decided (build S-B15 or record the deviation and re-scope) before Task 2.4 is marked done.
Findings 4-7 are follow-ups.
