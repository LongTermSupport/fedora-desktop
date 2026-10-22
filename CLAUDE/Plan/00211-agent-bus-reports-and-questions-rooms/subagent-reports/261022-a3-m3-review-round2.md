# Branch fix/acceptance-checker-a3-m3: adversarial review, round 2

Base F44 (merge base 22e599b3d, F44 now 6de33a73e), tip 7a07fa2fb. This round covers
c895a09cd (the checkers read product values) and 7a07fa2fb (the answers to round 1).

## Verdict: DO NOT MERGE (one major finding, F1). The rest is sound.

The merge itself is clean (`git merge-tree` reports no conflicts). The problem is what happens
after it: F44 has moved on under the branch, and M3 then fails against the product it now
reads its label from.

## What was run

| Check                                                                                               | Result                                                                                                 |
| --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| 00161 test_deploy_check / test_triage_probe / test_u20_check / test_u01_probe / test_u23_kit_unpack | OK (20 / 59 / 81 / 73 / 2 skipped)                                                                     |
| 00161 test_acceptance_check                                                                         | 2 failures (`a HUMAN line has the wrong version...`), see F5; not this branch's                        |
| 00201 test_m7_check                                                                                 | OK (50)                                                                                                |
| 00203 test_m2_procs / test_m2_inbound / test_hs_journal                                             | OK (14 / 8 / 18)                                                                                       |
| unittest test_plain_member_bundle_rule + test_cited_constant_copy_rule                              | OK (12)                                                                                                |
| 00211 `acceptance.bash --stage 2 --fake`                                                            | rc 0, all legs OK                                                                                      |
| shellcheck on the 7 touched bash files                                                              | clean except one SC2329 info in 00055 acceptance.bash:209 (cleanup via trap; there before this branch) |
| `qa-changed.bash` (no flags)                                                                        | "no changed files": with a clean tree it compares with HEAD and checks nothing                         |
| `qa-changed.bash --base 22e599b3d`                                                                  | passed, 31 files                                                                                       |

## Findings

### F1 (major): once F44 is merged in, M3 fails against the Go status line F44 now draws

Plan 00217, merged to F44 after this branch's base, changed how the Go `pingbus-status-line`
draws each team. It was `alpha (coordinator), beta (worker)`. It is now
`alpha: myrepo.1 (c), beta: myrepo.1 (w)`, with each team in its own colour (statusline.go:377,
`roleLetters`; golden `bus-two-teams-with-roles/1.stdout`).

m7_check now reads the label from statusline.go, so the label is right: on F44's source it
reads `('👥 ', '   ')`. The subject is right too. `STATUS_LINE_GUARD` ends in
`exec pingbus-status-line`, so the Go binary draws the segment.

But `_SEGMENT_TEAM_RE` is still a copy of the old format,
`team \((?:worker|coordinator)\)`. I ran `expect_segment` on F44's golden output with F44's
label, and it fails: "want the session's own line then 👥 alpha (worker|coordinator), ...".

F44's own m7_check has the same regex, so F44 is already broken here and this branch does not
cause it. But this branch exists to make M3 match the product, and its meta-deploy entry tells
the owner that M3 "now expects exactly what this checkout's seat.py writes". Once a host has
deployed 00217 (container 2.79.6), the queued 00201 M7 rerun stops at M3 again.

Fix before merging:

1. Merge F44 into the branch.
2. Make the team pattern follow 00217: `team: member (c|w)` and `role?`. In keeping with this
   sweep, read the letters from `helpers/pingbus/statusline.py` (`ROLE_LETTERS`,
   `ROLE_UNKNOWN`), which m7_check already imports for `display_width`.
3. Update test_m7_check's fixtures, the M3 leg name in acceptance.bash:168, and the comment
   in `_acceptance-m7.inc.bash:636-641`, which still say `👥 A (role), B (role)`.

### F2 (minor): a new copy of inline message text, added in the round-1 answer

7a07fa2fb's fake A3 leg greps for `a host member must not run as a user with an Element profile`. That text is copied from helpers/pingbus/config.py:447 and roles.go:237. It is the
same class as the 16 inline-text copies referred to Task 2.21, but it was added after that
sweep and is not in the referral list. Add it to the 2.21 list. The check itself is right: it
now fails on any other refusal.

### F3 (minor): two parsers of the Tuwunel pin

A9 is behaviourally correct:

- triage now defaults to the version the installer pins (v1.9.4). Before, it probed a stale
  v1.9.3.
- `--tuwunel-version` still overrides it.
- An unreadable pin is a `parser.error` (exit 2) before any leg runs.

But `pinned_tuwunel_version` is a second copy of `declare.declared_software`'s inline pin
parse. Both readers are strict, single-match and fail loudly, so they cannot silently diverge.
They can still drift in what they accept. Left as is, this is acceptable. Ideally declare.py
would expose the reader, but that is a product change, so record it with 2.21.

Note also: the Plan 00161 triage results recorded earlier are v1.9.3 evidence. They are not
invalidated, but they are now one release behind what triage would probe.

### F4 (nit): stale text in deploy_check.py

The docstrings of `conflicts()` and the module still name `agentbus0` literally. That is
harmless, because the name now comes from agent-bus-install (`readonly BUS_IFACE=agentbus0`,
exactly one match).

### F5 (outside this branch): test_acceptance_check has 2 red tests

`test_a_reply_must_thread_under_the_message_it_answers` and
`test_the_expected_human_line_and_its_reply_pass_with_the_text_decoded` both fail. Neither
this branch nor F44 touches acceptance_check.py or helpers/, so the failures were already at
the merge base. Hand them to Plan 00161's owner. They also show the toolchain gap the DBF
reports already record: no gate runs the plan checker tests.

## The questions asked

**Where each checker runs.**

- These run on the host with the checkout:
  - m7_check expect-segment (`CHECK=(python3 -I ...)`)
  - m2-check's guard read
  - deploy_check and triage_probe (from deploy.bash and triage.bash)
  - check-fetch-age (from 00183 acceptance.bash)
  - 00055 acceptance (`git rev-parse` root)
- m2_procs runs in the container as `/workspace/<plan>/m2_procs.py`. It walks up to
  `/workspace/ansible.cfg`, which is tracked, and reads seat.py with `ast`, never importing it.
  So that works with only /workspace present.
- Each bounded walk stops at `.git`. That also works in a worktree, because `ansible.cfg` is
  tested before `.git` in the same directory.

**Narrow parses that fail loudly.** Each of these wants exactly one match and fails loudly
otherwise:

| Parse                                    | Strictness                                      | Failure                                         |
| ---------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| Go `const FailedMark = "..."`            | escape-free                                     | `[FATAL]`, exit 1                               |
| statusline.go `label = "..."`            | -                                               | `Failed`                                        |
| `readonly BUS_IFACE=`                    | -                                               | exit 2                                          |
| the launcher's `for key in LAST_...; do` | must name LAST_SSH_KEYS                         | `ValueError`; a missing file is OSError, tested |
| seat.py `KEEPER_LOCK` / `LOCK_FILE`      | one plain string each (via `ast`)               | `Failed`, caught by main, exit 1                |
| `notice_re`                              | refuses any unknown field and a missing `{who}` | -                                               |
| 00055 D-Bus names                        | wants exactly 2 non-empty lines                 | exit 1                                          |

Nothing falls back silently.

Nit on 00055: `--dest` is given the interface name. It is the same value as before, but the
watchdog owns no bus name, so the old conflation is now spelled out. This is not a regression.

**A17 (LAST_TEAMS).** Both callers pass `CCY_LAUNCHER_TEXT`, which is defined and readonly in
both:

- `_acceptance-u20.inc.bash:267`
- `triage-ssh-agent-probe.bash:80`

No other caller exists. F44's claude-yolo has the same loop
(`LAST_TOKEN LAST_SSH_KEYS LAST_NETWORK LAST_TEAMS`). A record without LAST_TEAMS is one ccy
itself discards, so refusing it fixes a false pass rather than breaking anything.

The u20 remedy text ("launch ccy interactively once") is wrong only when the launcher cannot be
parsed. The real reason still reaches stderr.

**A18 (exact guard).** seat.py:465 writes `"command": STATUS_LINE_GUARD` verbatim, and the
value has no trailing newline for `$(...)` to strip. So the exact match is right, and stricter
than the old `startswith`. `fail` exits, so an unreadable or empty guard stops the check.
Nothing else calls `cmd_plain`.

**Round-1 answer (7a07fa2fb).** The unaccepted fake leg is now a `check_leg`. It is satisfied
only by the Element refusal, and the --fake run passes. The cited-constant rule sweeps
`tests/` and `scripts/` and skips its own fixtures in `tests/helpers/plan_acceptance/`. Its
tests pass.
