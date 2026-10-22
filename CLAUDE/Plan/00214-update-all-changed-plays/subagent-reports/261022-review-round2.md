# Plan 00214 review, round 2 (adversarial, Opus)

Scope: the whole branch, `git diff F44...HEAD` on `feat/update-all-changed-plays` (5 commits,
tip 842c2828a), read against issue #110 and its owner comment, PLAN.md, the project rules
and round 1's report.

## What I ran

- `./scripts/qa-changed.bash --base F44`: exit 0.
- `python3 -m pytest -q tests/helpers/play_ledger tests/helpers/host_health tests/helpers/self_update`:
  985 passed, 1 skipped.
- `node --test tests/extensions/test-panel-sections.mjs`: exit 0.
- `bash scripts/test-meta-deploy.bash`: exit 0.
- `git grep` outside `CLAUDE/Plan/` for the deleted names and wording (`_drop_never_tracked`,
  `changes_since`, `play_sha256_at_head`, `plays_to_query`, `UNEXPLAINED`, `freshness.classify`,
  `freshness.retire`, "no longer exists at HEAD", "own file", "never tracked").

## Round 1's F1: fixed in the three files it named, two more remain

`collector.py`, `affected_plays.py` and `playbooks/CLAUDE.md` are fixed. The sweep finds the
same retired claim in two more live files, both tests:

### F1 (round 2). Two test files still state the deleted rule

- `tests/helpers/self_update/test_affected_plays.py:3-4`: "The play ledger watches only each
  play's own file, so a change to the ccy lib or launcher never marks `play-claude-yolo.yml`
  stale". This is the same sentence this branch corrected in `affected_plays.py:3-4`, and it
  is now false: `changed_plays.judge` reads those inputs, and so does the report.
- `tests/helpers/play_ledger/test_collector.py:105-107`: "check_freshness drops it later as
  never tracked". `check_freshness` no longer drops anything. `changed_plays.judge` never
  judges a play outside `playbooks/imports/`, which is the wording `collector.py:143-145` now uses.

Fix: reword both to match their module docstrings, as was done for `affected_plays.py` and
`collector.py`. No other live text names a deleted rule or name. The remaining hits are in
plan folders (history) or unrelated ("ccy.env.local is never tracked", and similar).

## Checked and sound

1. **One judgement.** `check_freshness.run` → `changed_plays.judge` only. I diffed the
   refactor of `changed_plays.run` state by state: RUN, GONE, UNRESOLVED and `--all` print as
   before. Successor handling is unchanged: failed stays failed, unresolved becomes stale and
   is popped from `unresolved`, a successor that never ran becomes stale. `freshness.REPORTABLE`
   (stale, failed, gone) never reaches `_STATE_LABEL` with an unlabelled state, and an unknown
   judge state is a loud KeyError. `NoAnswer` → EXIT_UNTRUSTWORTHY with `judged` left empty,
   so the panel rows and the paste command are both withheld.
2. **Coverage path.** `login_report.freshness_section` is now a named function, tested for
   both the judged and the could-not-judge cases.
3. **Command (gap 1).** `run_changed_command` takes the path from `realpath(__file__)`, quotes
   it with `shlex`, and prints it on a line that does not start with ` -` (`item_counts`
   proves it). `main` always returns 0. `fedora-desktop-health.j2:168` and
   `host-health-report.bash.j2:48` run the module from the checkout. The test fixtures use
   `/home/me/...` and `/c` placeholders, and no real path is in the diff.
4. **meta-deploy.** `changed-plays` is accepted only on its own, maps to
   `<repo>/run.bash --changed --yes`, runs at its place in the list, and shows as
   `run.bash --changed --yes` in `--list` and in the summary. An exit 2 from it is FAIL, not
   UNSET (correct: the unit is not an acceptance). Removing the `status=0` lines is safe,
   because `status` is reset at the top of each loop pass and is 0 after a deploy that
   passed. The ratchet record went down (161/180).
5. **Acceptance.** I traced every `set -e`, subshell, `fail` and EXIT-trap path in
   `check-changed-plays.bash`: none loses a failure. The `same` leg's awk keys match
   `_STATE_LABEL` exactly, and the `report` grep matches `render`'s line byte for byte. Exit 2
   is reached only when no leg failed. The entry is queued last, with no `changed-plays`
   above it.

## NOTE

- **N1. The login fetch now feeds nothing in this section.** `check_freshness.run` still runs
  `git fetch` and reports the age of the refs as an "unchecked" item. But `changed_plays.judge`
  diffs the working tree against the commit each play ran from and never reads a remote ref.
  So the offline finding ("nothing was compared against upstream") describes a comparison
  the section does not make even when it is online. `git_history.py:3-4` now says so
  honestly ("refreshes the remote refs, whose age the offline finding reports"). The old code
  also judged against local HEAD, so this is not a regression, and the fetch may still be
  wanted for Plan 00183's stale-tracking-ref prompt. Worth a follow-up issue: either move the
  fetch and its age finding to whatever uses the refs, or say in the section what it is for.
- **N2. "The startup notice."** The owner comment says "The startup notice names the same
  command". The plan reads this as the login shell's report and reminder, and leaves the
  desktop notification alone because a notification cannot be copied
  (`login_report.py:94`). That is defensible, but it is a reading of an owner requirement.
  State it when handing the plan over, so the owner can disagree.
- **N3. A race in the `reminder` leg.** If `host-health.service` rewrites the document
  between the two `login_message --once-a-day` calls (`check-changed-plays.bash:178-179`),
  the second call prints the full message, and the leg fails spuriously. This is rare, and
  re-running clears it. If it ever bites, the leg could pass `--state-dir` a copy of the
  document.
- **N4.** When `plays_to_run` is false, the `reminder` leg does not check that the reminder
  leaves the command out. The `report` leg checks that for the full report. The unit test
  `test_no_command_when_every_play_is_fresh` covers the logic.
- **N5. Unwrapped lines.** `helpers/play_ledger/collector.py:145` (131 characters) and
  `playbooks/CLAUDE.md:31` (122 characters) were edited without rewrapping. This is cosmetic,
  and QA accepts it.
- **N6. Round 1's N7 still stands.** `login_message._RUN_STATES` and `plays.js NOT_RUN` encode
  the same set from opposite sides. Deriving `_RUN_STATES` from `changed_plays.RUN_STATES`
  would make the Python side single-sourced.
- **N7. Round 1's N8 still stands.** `docs/playbooks.md:63` says `--yes` is "for an unattended
  caller", but under `RUN_BASH_OFFER_ISSUE=0` with no tty on stdin, `run.bash` turns HEADLESS
  and refuses `--changed`. meta-deploy is run from the owner's terminal, so the entry works,
  but the docs promise more than that.
- **N8. Behaviour change for panel rows and `--run-play`.** A ledgered `playbooks/dev/` play no
  longer appears in the document's `plays` rows. So `fedora-desktop-health --run-play playbooks/dev/x.yml` is now refused by `play_runner --validate`. This is consistent with
  the plan (only host plays are judged). It is worth one clause in the plan's Task 1.3.

VERDICT: FIX-FIRST
