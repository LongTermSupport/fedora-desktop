# Task 2.22 sweep: handoff 1

Branch `fix/checker-owned-values-sweep`, from F44 b0eb800fa. Not pushed, not merged.

## Done

- **Defence red** `b0dd7c9e1`: `checker-copies-cited-constant` (tests/helpers/plan_acceptance/
  test_cited_constant_copy_rule.py) gained a second clause. `OWNED` lists product constants
  (product file, constant, kind); each value is read from the product source; a line of an
  active plan checker (not `test_*.py`; not comments or docstrings) whose code holds one is a
  finding. Kinds: ANYWHERE, FILE_NAME (`/x`), DIRECTORY (`/x/`), LINE_START (`^x`). A text with
  `{name}` or `%` placeholders is matched by each fixed part of 12 or more characters. Fixtures
  pass; at the red the sweep had 39 findings. CLAUDE/QA.md documents the clause.
- **Batch 1 (in the commit after this handoff's)**: 44 findings fixed, the sweep is green for
  the current OWNED.
  - Product: src-go/internal/teams/membership/membership.go names `ccyRoot`/`ccRoot`
    (`Dir`/`CCDir` give the same paths). Added to OWNED as DIRECTORY.
  - m7_check.py (Plan 00201): new readers `session-names` (seat.py ACTIVE_FLAG,
    STATUS_LINE_INNER, WATCH_LOG) and `membership-dirs` (membership.go ccyRoot, ccRoot,
    desired.go RelaunchFile), with tests in test_m7_check.py (57 pass).
  - Plan 00203: acceptance.bash reads the names (m3_read_names, now also M3_DESIRED,
    M3_WATCH_LOG, M3_CCY_DIRS, M3_CC_DIRS, M3_RELAUNCH) before M2; M2, M4, M6 use them; M4/M6
    `m4_said`, `m4_wait_line`, `m6_said` are grep -P with `\Q<mark>\E`; m2-check.bash reads
    session-paths and session-names; \_triage-hs.inc.bash reads the watcher log path.
  - Plan 00161 \_acceptance-u20.inc.bash: `u20_read_names` leg (acceptance.bash) before U20.
  - Plan 00194 \_acceptance-goodbye.inc.bash: gb_paths reads membership-dirs.
  - Plan 00211 \_acceptance-stage2.inc.bash: CCY_PINGBUS_HOME from session-paths;
    `acceptance.bash --stage 2 --fake` still FAKE ACCEPTED, rc 0.
  - A scratch copy of m2-check.bash (untracked/scratch/m2copy.py) prints the same values the
    old copies held.
- **Independent search** (dispatched before the rule): subagent-reports/
  261022-sweep-independent-search.md: 34 (a) value copies and 34 (b) inline-text copies.

## Left

1. Widen OWNED to the search's named, distinctive (a) values and fix their sites: membership
   APPLIED_FILE (row 7), config ELEMENT_ACCEPTED_DIR/FILE (9), statusline
   SESSIONS_DIR_TEMPLATE (11), seat CHECKOUT_SEATS and the sessions dir (12), render/health/
   checkout state roots (23), protocol TEAM_NAME_PATTERN (15), hot/cc.go ccUnguarded (20),
   membership MEMBERSHIP_VAR/OFF (33); by hand: 00211 WHERE_PY inbox names (13, 14), 00194
   GB_REASON (28), 00128 verdicts STAGE (16).
2. Plan 00161 pin parser: declare.py should expose the `tuwunel_version:` reader
   (`declared_software` uses it, error text unchanged); triage_probe.pinned_tuwunel_version
   calls it. Note any helpers/pingbus or helpers/agent_bus change changes the agent-bus
   software digest (bundle.build), so play-agent-bus.yml must run (already in meta-deploy).
3. Inline texts. Planned design: a generic m7_check reader `product-text FILE NAME [ARG...]`
   (a Go string const with `%s`/`%d` filled in order, or a Python str constant with `{key}`
   filled by key=value), so bash checkers read any named text. Then name each text in the
   product with the smallest change, use it there and in the checker, add it to OWNED:
   the referred list (A4 CHOICES_TEXT and A5 cli.py; B2 choose.go block head and row; B5:
   make launch_conf_clean key-independent, no product change; B7 hot add.go relaunch words and
   remove.go re-baseline step; B8 health.py/checkout.py; B10 seat.py; B11 the speech-to-text
   extension; B12 wsimedia; B14 run.bash; B15 entrypoint.sh (container version bump),
   rclonecache, convert, self_update cycle.py; Plan 00211's Element refusal in config.py and
   roles.go), and the search's b1-b23 in 00203 M3/M4/M6 (hot package messages).
   B4 (wire tags SEAT/ABOUT/SENT/TEAM, trust=trusted) are specified in
   docs/agent-bus-protocol.md §13/§15: treat as specification, record it.
4. Refer, with reasons, what is UNSURE or not soundly a copy: search rows 17, 19, 21, 22, 24-27,
   29-32, b24, b26, b27, b34, b17.
5. Version bumps the pre-commit hook asks for (launcher B5 none; entrypoint.sh: container
   2.79.7 -> next; pingbus in the image may need the container bump too), meta-deploy text,
   PLAN.md Task 2.22 status, the DBF report (REPORT-TEMPLATE.md), wrong-product proofs in a
   scratch copy for each converted checker, conformance-reviewer to CONFORMS, Opus review to
   MERGE.
