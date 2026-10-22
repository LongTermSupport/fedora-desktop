# Branch fix/acceptance-checker-a3-m3: adversarial review, round 3

Tip 1962e2901 (F44 merged in). This round covers 7e7afadd6 (more checker copies read from the
product) and 824762c51 (M3 follows Plan 00217; round 2's F2 and F4).

## Verdict: DO NOT MERGE (one major finding, R3-F1). The rest is sound.

R3-F1 is round 2's F1 again, in Plan 00203's M3. This branch rewrote the line in question and
left the old format in it.

## What was run

| Check                                                                                                                     | Result                                                                                                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00201 test_m7_check                                                                                                       | OK (54)                                                                                                                                                                          |
| 00203 test_hs_journal / test_m2_inbound / test_m2_procs                                                                   | OK (40)                                                                                                                                                                          |
| 00161 all six test modules                                                                                                | 303 run, 2 skipped, 2 failures: round 2's F5 (test_acceptance_check), unchanged                                                                                                  |
| unittest test_plain_member_bundle_rule, test_cited_constant_copy_rule, ccy_lifecycle.test_plugin, pingbus.test_cli_status | OK (97)                                                                                                                                                                          |
| scripts/test-ccy-relabel-preflight.bash / test-ccy-tmux-targets.bash                                                      | 60 passed / 23 passed                                                                                                                                                            |
| 00211 `acceptance.bash --stage 2 --fake`                                                                                  | rc 0. Every must-be-rejected leg was rejected; the jq "parse error" line is the non-JSON fixture                                                                                 |
| `qa-changed.bash --base F44`                                                                                              | passed, 46 files                                                                                                                                                                 |
| m7_check session-paths / hot-marks / status-line-names, run on this checkout                                              | 4 / 2 / 2 lines: `/tmp/pingbus-home`, `.../settings.json`, `/run/ccy/membership`, `membership.json`; the ✓ and ✗ marks; `👥 ` (trailing space kept), `PINGBUS_STATUS_LINE_INNER` |
| Each new product read run by hand: VERDICT_PASS, login_words, admin.KIT_DIR, the play_var greps                           | each gives the value it replaced                                                                                                                                                 |

## Findings

### R3-F1 (major): 00203 M3 (b) still expects the status line format from before Plan 00217

`_acceptance-m3.inc.bash:268` (`m3_status_line`):

```bash
[[ "${line}" =~ "${M3_SEG_HEAD}${M3_TEAM_A} ("(worker|coordinator)"), ${M3_TEAM_B} ("(worker|coordinator)")" ]] || return 3
```

Since 00217 the segment is drawn as
`👥 <sgr>teamA: member (c),<reset> <sgr>teamB: member (w)<reset>`, by the Go binary and by
statusline.py alike (see golden `bus-two-teams-with-roles/1.stdout`). This regex can never
match that. The leg returns 3 on every poll and, after 10 s, fails with "the status line naming
both teams".

On F44 this is already broken: it has the same text with a literal `👥 `. But 7e7afadd6 rewrote
this very line to read the head from statusline.go and kept the old piece format. It is the
class round 2's F1 named, and the reader that fixes it (segment_piece_re) now exists one file
over. The 00203 acceptance is not on meta-deploy.list, so neither queued run is blocked. It
breaks the next time 00203 M3 is run.

Fix: have M3 judge the line with m7_check. Either add an `expect-segment`
mode with no inner line (PINGBUS_STATUS_LINE_INNER=1 suppresses it, and `expect_segment` wants
one), or add a command that judges the segment rows alone with `_segment_teams`.

### R3-F2 (minor): the Task 2.21 referral list in 00201's PLAN.md is out of date

It still refers "A4, A5, B2-B15" to the owner as inline text. Since round 2, B3, B6 and B13 are
read from the product, and B7 is read in part (its ✓ and ✗ marks). Two items are not recorded
anywhere in the plan:

- round 2's F3 (the second parser of the Tuwunel pin);
- the report's "found, not fixed" list: 00203 `_acceptance-m4/m6` marks and `/tmp/pingbus-home`,
  `m2-check.bash:25`, `_triage-hs.inc.bash`. These are about 25 sites of the same class, and
  they belong with the 00203 fix above.

### R3-F3 (nit): two reads put the caller's directory on the import path

00211 deploy.bash's VERDICT_PASS read runs `python3 -c` without `-I`, so the caller's cwd is
on sys.path. 00214's reads use `-P`, which does keep it off. This is harmless on the host
checkout. The other reads in this sweep use `-I`.

## The questions asked

**Round 2's findings.**

- F1 is resolved for 00201. `expect_segment` draws a sample piece with statusline.py's
  `_team_piece` and placeholder strings, once for each role. It requires each to have the
  same shape, builds the regex with protocol's TEAM_NAME_PATTERN, and requires each team
  to be its own colour span. It refuses `member?`, `role?` and `(colour?)`.
  - `_team_piece({}, ...)` with an empty environment is safe: `_is_down` returns False when
    PINGBUS_HOME is absent.
  - The Go golden passes, and the test drives it.
  - The leg name, the comments and the meta-deploy entry are updated.
  - Not resolved: 00203 (R3-F1).
- F2 is recorded in Task 2.21.
- F3 is not recorded (R3-F2).
- F4 is fixed: the docstrings now name BUS_IFACE.
- F5 is unchanged and outside this branch.

**Where each checker runs.**

- These run on the host, from the checkout:
  - m7_check session-paths, hot-marks and status-line-names (`python3 -I` on the plan path; they
    read helpers and src-go from `_checkout_root`)
  - 00203 m3_read_names and m3_removal_words
  - 00211 deploy.bash spike_passed
  - 00214 check-changed-plays
  - 00109 play_var
  - 00161 u23_kit_parent: it runs on the host and only hands the parent to the guest prepare
  - 00213 KIT_DIR
- Nothing new runs in a container. The only in-container command, session-status-line, is
  unchanged.
- The values read on the host are then used inside the container: the paths, and the
  membership mount's Destination in `podman inspect`. That is correct, because they are the
  product's own constants.

**Each parse fails loudly.**

- session-paths: requires 4 non-empty lines, no newlines.
- go_string_const: requires exactly one plain `name = "..."`.
- board_table: requires exactly one headings var, at least 4 names, and one KindTeam.
- segment_piece: requires each part once and one shape for all roles.
- play_var: requires exactly one plain line, else abort.
- u23: requires the form `/?*/kit`.
- 00213: requires an absolute path.
- VERDICT_PASS: must not be empty.
- login_words: requires one line each.
- m3_lines: requires the count and no empty line.
- m3_removal_words: requires `{handle}` then fixed text.

Each failure stops the leg or the script: FAIL, FATAL or abort. None is silent.

**The m7_check wiring.**

- 00201: m7_prerequisites reads session-paths first, its `|| prereq_status=$?` mapping exit 1
  to `exit 1`. m7_seat_listing refuses empty values. The cleanup trap that runs on an early
  failure returns before calling it when nothing was started. The values are used at :205, :207,
  :604 and :664, all after prerequisites.
- 00203: m3_read_names is the first plan_deploy_leg of acceptance_m3, and every use comes
  after it. `mapfile -t` keeps the trailing space of `👥 `. The `grep -P "^\Q...\E"` greps
  quote the marks correctly.

**tests/ and scripts/.**

- test_plugin: `load_plugin_module()` is called at import, after it is defined. The tests pass.
- test_cli_status: uses `protocol.TEAM_NAME_PATTERN`, the same expression.
- relabel-preflight: `file_selinux_label` is a function in the driver, which sourced
  common.bash, so the unexported readonly `CCY_SHARED_FILE_LABEL` is visible. The
  "already labelled" case still passes.
- tmux-targets: tmux-session.bash sets `CCY_TMUX_SOCKET="ccy"` when sourced, so removing the
  two reassignments changes nothing. 23 pass.

**meta-deploy.list.**

- 00211 deploy-stage-2 is accurate. VERDICT_PASS is `Verdict: every fact holds`. Container
  2.79.7 is REQUIRED_CONTAINER_VERSION, and the 00213 entry builds it before both reruns.
- 00201 is accurate. It names the 00217 form read from statusline.py, and it runs after the
  2.79.7 build, so the session draws that form.
