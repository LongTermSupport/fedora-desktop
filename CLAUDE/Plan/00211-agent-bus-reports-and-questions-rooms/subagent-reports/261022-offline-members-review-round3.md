# Adversarial review, round 3: fix/offline-members-never-block

Diff reviewed: `git diff 8cb85c27c..HEAD` (commits 4958cfba9, e6e883de0). Read-only review. It
checks the fixes for round 2's N1-N5, the record of N6, and the delta for new defects.

All runs were green:

- `python3 -m pytest` over test_offline_member_rule, test_admin_side_rooms and test_cli: 70
  passed
- 00211 `acceptance.bash --stage 1 --fake`: FAKE ACCEPTED, rc 0
- 00211 `acceptance.bash --stage 2 --fake`: FAKE ACCEPTED, rc 0
- `scripts/test-planlib.bash`: rc 0
- `bash -n` on every changed plan script: ok

## Round 2 findings

| #   | Verdict  | Evidence                                                                                                                                                                                                                                                                                                                                   |
| --- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| N1  | fixed    | `rooms_joined` now splits its message by membership. `leave` says: relaunch, then run meta-deploy again, whose play invites it again (SPEC §5.3). `invite` and `-` point at `pingbus status` in that session, which names the reason: an old pingbus, or human_text false. The leg still returns 1, so a running wrong member still fails. |
| N2  | fixed    | A5's refusal now says to run `deploy-stage-2.bash` with the coordinator online. It no longer says "until ACCEPTED", so following it cannot loop.                                                                                                                                                                                           |
| N3  | fixed    | Stage 2's verdict is built from an `unjudged` list: `ACCEPTED, <A3 inbox, A4, the test question> NOT JUDGED (each named above): ...`. The positive text claims only what always runs. 00213's `accepted_line` and 00203's final line and `robust_said` each name the offline count when only some containers are online.                   |
| N4  | fixed    | The `RoomCheck` docstring, the `fake_a12` OFFLINE fixture, and both test docstrings (test_admin_side_rooms, test_cli) now say "no live status". They no longer say "invited".                                                                                                                                                              |
| N5  | fixed    | `docs/agent-bus.md`, the changelog and the `check_rooms` docstring now say "no session of it is on the bus". That matches what the evidence shows.                                                                                                                                                                                         |
| N6  | recorded | PLAN.md Task 2.8 now has a "Known and accepted" sentence. It covers the missing 180 s grace for a host-member coordinator that is listening but has not synced, says the result is a loud PROBLEM and never a pass, and gives the reason (the image rebuild runs between the invite and the check).                                        |

## Delta checks

- **The `wc -l` arithmetic** (`R9_OFFLINE` and `C3_OFFLINE`) runs only after the
  `[[ -z "${online}" ]]` early return, so the here-string is never empty and its added newline
  cannot count a phantom line. `$(...)` strips the helper's trailing newline, and `<<<` adds back
  exactly one. `names` is unique (`sort -u` or jq `unique`), and `online` is a subset of `names`
  with one line each. So the difference is the true offline count, and never negative.
  Exercised in scratch under `set -euo pipefail`: 3 names with 2 online gives 1, and 1 with 1
  gives 0. The statement is an assignment, not `(( ))`, so a result of 0 does not trip `set -e`.
- **The `unjudged` joiner** (`${unjudged:+${unjudged}, }`) gives `A3 inbox, A4, the test question`, `A4`, `the test question` or empty, with no stray leading comma. Exercised in
  scratch.
- **SPEC §11.2 form.** The stage-2 line is `ACCEPTED, X NOT JUDGED (each named above): ...`.
  It carries the promised prefix, with a short aside before the colon. This is acceptable, and
  no script parses the verdict line: a grep for consumers of `ACCEPTED` found none.
- **The version bump is consistent.**
  - Dockerfile LABEL and REQUIRED_CONTAINER_VERSION are both 2.78.18.
  - CCY_VERSION is 3.99.15.6, and its comment names container 2.78.18.
  - The changelog has one entry, `3.99.15.6 — container 2.78.18`, and it follows 3.99.15.4.
  - meta-deploy.list changes all five mentions.
  - No stale 3.99.15.5 or 2.78.17 is left in CLAUDE/, docs/ or the ccy files (reports and the
    JOURNAL excluded).
  - F44 is still at 3.99.15.4 and container 2.78.15. F44's one new commit touches only
    AgentNotes.md, so there is no conflict.
- **The owner's rule holds.**
  - Every message changed in this delta is wording or verdict text. No pass or fail outcome
    changed.
  - Offline members are still named, not judged, and the run still exits 0.
  - The joins-wait timeout still returns 1. The ABORTING and lxc-info failure paths are
    untouched.
- **Public repo.** No install-specific names appear in the delta.

## Nits (not blocking)

- **R3-1.** When some checks are unjudged, the stage-2 line no longer credits the checks that
  did pass. For example, with only A4 unjudged it does not say that the test question was asked.
  This understates; it never overstates. The `==> NEXT` line still follows `COORD`, so the owner
  is still sent to the Element steps.
- **R3-2.** This one is outside the delta. The `--after-owner` A5 NOT JUDGED reason and verdict
  (`acceptance.bash` around line 294) still say "the coordinator is offline". The rest of the
  plan now says "not driven from here (offline, or a host member)".

VERDICT: MERGE
