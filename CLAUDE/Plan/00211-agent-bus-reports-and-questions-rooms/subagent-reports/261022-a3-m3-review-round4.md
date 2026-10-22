# Branch fix/acceptance-checker-a3-m3: adversarial review, round 4

Tip ae0e8efda (base F44 cd0cbba41, merged in). This round covers c1f64e4d7 (round 3 answered)
and ae0e8efda (Plan 00201 Task 2.22), and re-checks the four questions for both checkers.

## Verdict: MERGE

No major finding remains. There is one minor finding: Task 2.22's site list is incomplete. There
are two nits.

## What was run

| Check                                                                                                         | Result                                                                                                                                      |
| ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| 00201 `python3 -m unittest test_m7_check` (in the plan folder)                                                | OK, 55 (one new: `test_expect_segment_rows_wants_the_teams_alone`)                                                                          |
| `unittest` test_plain_member_bundle_rule + test_cited_constant_copy_rule                                      | OK, 12                                                                                                                                      |
| 00211 `acceptance.bash --stage 2 --fake`                                                                      | rc 0; both new A3 seat legs PASS; every must-be-rejected leg rejected                                                                       |
| `./scripts/qa-changed.bash --base F44`                                                                        | passed, 48 files                                                                                                                            |
| VERDICT_PASS read exactly as deploy.bash runs it (`python3 -I`, cwd /tmp)                                     | `Verdict: every fact holds`, rc 0                                                                                                           |
| `fixture-plain-member-bundle` `detect` on A3 at bd3cf6661 / at tip / tip mutated (`--out` back into the home) | [358] / [] / [362]                                                                                                                          |
| `checker-copies-cited-constant` `detect` on m7_check.py at 2722a74c6                                          | [67, 438] (GUARD_TAIL among them); the sweep at the tip is empty (test_no_finding passes)                                                   |
| Mutation: scratch checkout copy, statusline.py `_team_piece` changed to `team @ member [l]`                   | the real Go golden `bus-two-teams-with-roles` is now refused by `expect-segment-rows` (rc 1); output in the mutated form is accepted (rc 0) |
| Mutation: scratch copy, seat.py STATUS_LINE_GUARD `exec sh` changed to `exec bash`                            | `expect-session-settings` refuses the settings the unmutated product writes (rc 1); before the mutation rc 0                                |

Scratch files are under `untracked/scratch/r4mut/` and `untracked/scratch/r4-*`.

## Round 3's findings, re-verified

- **R3-F1 (major): fixed.**
  - 00203 `m3_status_line` now writes the inner-only status line to evidence and judges it with
    `m7_check expect-segment-rows`. The old `team (role)` regex is gone.
  - `expect_segment(..., inner=False)` requires that no line comes before the `👥` row. That
    matches the product: statusline.go `render` returns `seg` alone when `innerVar` is set, so
    the output is exactly the segment rows.
  - The piece shape is read from statusline.py `_team_piece` / `ROLE_LETTERS`, and the team
    names from protocol's TEAM_NAME_PATTERN. `(role?)`, `member?`, the pre-00217 form,
    uncoloured spans and empty output are each refused by test.
  - The Go golden passes.
  - Exit mapping: 0 passes, 1 means "not yet" while the 10 s poll runs, anything else stops the
    leg. On a final failure the reason from `.why` is printed to stderr.
- **R3-F2 (minor): fixed.** B3, B6, B13 and B7's marks are recorded as read. What is left is
  split off as Task 2.22 (see F1 below for gaps in its list).
- **R3-F3 (nit): fixed.** deploy.bash uses `python3 -I` and puts the plan folder and repo root on
  `sys.path` explicitly. The read was verified from another cwd. M3_CHECK already uses `-I`.

## The four questions

1. **Does A3 use the production seat path?** Yes.
   - The bundle goes to its own directory, `a3-seats/<team>/<tag>`, made by the real `sudo -n agent-bus add-member --out`.
   - The wrapper refuses an existing `--out`, and creates it 0700 as SUDO_UID through setpriv.
     That satisfies config.py `_check_linked_bundle` (owner and mode 0700).
   - `<home>/<team>` is a link to it, which is the shape `seat take` and the keeper make.
   - The real `/usr/local/bin/pingbus report` then loads it through `load_bundle`'s `linked and element_accepted` branch, the same branch a `cc --teams` seat takes.
   - The fake legs drive this checkout's real `config.load_bundle`. Only add-member is faked.
     Accepted loads as `host`; never accepted is refused for the Element reason.
2. **Do M3 (00201) and M3 (00203) read their expectations from the product?** Yes.
   - 00201's expectation is `seat.session_settings(None, seated=True)["statusLine"]` and the kit
     `settings.json` command.
   - 00203's expectation is the statusline.py piece shape plus the `label` and `innerVar` read
     from statusline.go.
   - Both mutations above show each checker follows the product, not a copy.
3. **Could either checker pass when the product is wrong?**
   - A3: an add-member that writes into the home (the original defect) is red under the rule.
     It is also refused at runtime by the real load_bundle beside an Element profile.
   - A3 without the link fails: no team is found.
   - M3 segment:
     - a role or member that was not read is refused;
     - a missing span is refused;
     - an extra or missing team is refused;
     - an inner line before the rows is refused.
   - Two teams in the same colour would pass. That is correct: SPEC C5 allows a shared auto
     colour, and C10 asks for one span per team, not distinct colours.
   - No false pass was constructed.
4. **Do the rules go red on the original defect and green at the tip?** Yes, both. Reproduced
   by hand on the historical sources (table above), not only through the fixtures.

## Findings

### F1 (minor): Task 2.22's site list is incomplete for m2-check.bash and \_triage-hs.inc.bash

Plan 00201 PLAN.md Task 2.22 names only `m2-check.bash:25` (`HOME_IN=/tmp/pingbus-home`).
The same file also has:

- `readonly MOUNT=/run/ccy/membership` at :24;
- the literal `membership.json` at :86, :95, :116, :121 and :123.

Both are values that `m7_check session-paths` returns, which is the reader Task 2.22 names. For
`_triage-hs.inc.bash` it lists 157, 161 and 171, and omits :162 (`stat ... /tmp/pingbus-home/watch.log`).

Every line given for M4 and M6 is accurate. Fix: add the missing lines to Task 2.22.

### F2 (nit): expect_session_settings sanity-checks the guard with a literal `[ -e ` prefix

`m7_check.py:534` requires seat.py's own guard to start with `[ -e ` and end with
`exec <kit>`. The ending is read from the product. The prefix is a copy of the shape of
seat.py's STATUS_LINE_GUARD.

This can only fail a correct product (it fails if seat.py changes to `test -e`). It cannot pass
a wrong one, so it is not a blocker. It could be noted under Task 2.22.

### F3 (nit): any m7_check exit 1 is treated as "not yet" in 00203 m3_status_line

An uncaught Python error exits 1, for example a product module that fails to import. The leg
treats it as "not yet" and polls for 10 s before it fails. It still fails and prints the
traceback from `.why`, so nothing passes silently.
