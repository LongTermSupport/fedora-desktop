# Defence Before Fix report: checker-copies-cited-constant

Branch `fix/acceptance-checker-a3-m3` (from F44 22e599b3d). Defence red `2722a74c6`; fixes and
green in the commit after it. The same branch carries Plan 00211's
`fixture-plain-member-bundle` (Plan 00211 `subagent-reports/261022-a3-dbf-report.md`).

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, from
the cached copies the refresh script listed (`/root/.cache/defence-before-fix/spec/`).

## Toolchain

As in the 00211 report: no manifest declaration (none added); the project's QA gates; rules as
stdlib `unittest` detectors under `tests/helpers/`, run by `qa-helper-tests.bash`, documented
in `CLAUDE/QA.md`. Single-rule harness: `python3 -m unittest tests.helpers.plan_acceptance.test_cited_constant_copy_rule`. Resolution: `CLAUDE/QA.md "checker-copies-cited-constant"`. Calibration assumed: no exceptions recorded.

## Defect and class

Originating instance: M7 check M3 (`m7_check expect-session-settings`) failed on the host:
"the status line is {... exec pingbus-status-line ...}, not the shell guard ending 'exec
pingbus status-line'". `m7_check.py` held `GUARD_TAIL = "exec pingbus status-line"`, commented
as `helpers/pingbus/seat.py STATUS_LINE_GUARD`; Plan 00213 changed the guard. Class: a checker
hard-coding a value its subject owns. Hazard: the copy drifts, and the checker then fails a
correct product (or passes a wrong one).

## Rule

`checker-copies-cited-constant`: a literal constant of an active plan checker (Python
module-level, or top-level bash with no expansion) whose own comment names a product source
file (`.py`/`.go` under `helpers/`, `src-go/internal/`, `files/`, `extensions/`) and a
constant that file defines (a word of the comment, or the checker constant's own name).

Next wider rules, drafted and run, not built: (1) any checker string literal inside a product
constant's value: 145 hits at 16 characters, mostly install paths, play names, box-drawing
rules and external values; (2) the same, only where the literal is compared: 10 hits, among
them Matrix event types (`m.room.message`, the Matrix spec's), git's own message and CLI
command names; (3) a checker constant of the same name and value as a product constant:
catches `BUS_IFACE` and `PREFIX`, but also `EXIT_USAGE = 64` (sysexits), `GITHUB_API`
(GitHub's) and Claude Code's socket variable names, which the product copies too. Each matches
code without the hazard, so none is sound. The built rule fires only where the checker's own
comment says the value is the product's.

## Proof

Fixtures: the originating `GUARD_TAIL` with its comment, `DESCRIPTION_MAX`, a constant named as
the cited file's, a trailing citation, a Go constant from bash; near misses: the checker's own
timeout, a value read from the module, a cited file defining no word of the comment, a
document instead of a source, an expansion, an indented bash assignment. At `2722a74c6` the
fixtures pass and the sweep fails with four findings: `m7_check.py:67` (`DESCRIPTION_MAX`),
`m7_check.py:438` (`GUARD_TAIL`, the originating instance), Plan 00194 `goodbye_check.py:35`
(`TEXT_KEY`) and `:39` (`DISCONNECT_TEXT`). Seen on an exported tree of that commit; it holds
the red alone.

## Independent search

`261022-m3-owned-value-independent-search.md`, dispatched before the rule, kept out of its
directory. It found 17 copies of named or computed values, 16 of inline text, 8 specification
statements (not instances), 6 unsure, 5 checkers that already read the product. Drifted today:
`GUARD_TAIL` and its test fixture, and Plan 00161's `triage_probe.py` Tuwunel pin. The rule
catches 4 of its instances (A1, A2, A7, and B9's `DISCONNECT_TEXT`); the rest carry no comment
naming the product constant, and no widening reaches them without the false positives above
(text search found each subject location; reading found computed copies such as the segment
indent and a re-implemented power-level function, which no text rule can see). The search wins
on what is an instance; the rule stays narrower than the class, and the excess is referred.

## Sweep and count

Scope: every `.py` and bash file in active plan folders, `tests/` and `scripts/` (the test
suites; widened after conformance round 1, with no new finding). Excluded: `JOURNAL/` and
`subagent-reports/` (records), completed plans (they cannot be run), and the rule's own
fixtures. The independent search first read the plan folders only; tests/ and scripts/ were
searched after conformance round 2 (`261022-m3-round2-fixes.md` Task 3; 4 copies fixed). 4
findings at the red, 0 at the green.

## Fixes

Each examined individually. `m7_check.py`: `expect-session-settings` expects exactly the
`statusLine` this checkout's `seat.session_settings(None, seated=True)` writes (every key it
sets; Claude Code's own padding may be added), and that the guard ends by running the command
in this checkout's kit settings (`files/opt/claude-yolo/optional/agent-bus/settings.json`), so
neither can be stale; `DESCRIPTION_MAX` is read from `protocol.py`. `goodbye_check.py`
(Plan 00194): `TEXT_KEY`, the goodbye template (search A8, outside the rule) and
`DISCONNECT_TEXT` are read from pingbus. Both import from the checkout lazily through the repo
root, as `m7_check.py` already did for `declare.py`; both run on the host.

After conformance round 1, the search's other 11 copies of named or computed values were
fixed too, each examined individually and none by pattern, with no product code changed:
A3, A6, A9-A14 and A16-A18 (`261022-m3-referred-fixes.md`). Each now reads its value from the
product source: a Python import, an `ast` parse where the checker runs in a container with only
`/workspace`, or a narrow read of a Go const or a bash `readonly`. Each one's tests cover the
read. The drifted Tuwunel pin is now v1.9.4, read from `tuwunel.pin`.

## Decisions referred to the owner

Count fixed: every named or computed copy of the search (A1-A3 and A6-A19; A15 since
round 2), B9's `DISCONNECT_TEXT`, round 2's B3, B6, B13 and B7's step marks
(`261022-m3-round2-fixes.md`), its 3 UNSURE rows ruled INSTANCE, and 4 copies in tests/ and
scripts/. Count remaining, referred: the inline message or command text A4, A5, B2, B4 (in
part), B5, B7 (in part), B8, B10-B12, B14 and B15, and Plan 00211's seat refusal text. Split off
by the coordinator as Plan 00201 Task 2.22, not blocking this merge: about 25 copies of the
same class in Plan 00203's M4, M6, `m2-check.bash` and `_triage-hs.inc.bash` (the sites are
listed there), and the second Tuwunel pin parser. What stopped me on the inline text: each text lives inside a product call, not in a
named value, so the checker can read it only once the product names it. That changes deployed
code (pingbus, Go tools, run.bash, the speech-to-text extension), needs their version bumps and
host deploys, and is a product change made for the checkers' sake, which is the owner's call.
What to try next: name each text as a constant in its product module, then have the checker
read it, plan by plan, when that plan's checker next runs. Recorded in Plan 00201 Task 2.21.
The rule is merged as it stands: it is sound for what it claims. The excess is a referral, not
an exclusion.

## Permanence

A test module under `tests/helpers/`, run by `qa-helper-tests.bash` in every `qa-all.bash`;
it fails, never warns. `qa-changed.bash` reruns it only when the module changes. Green through
the module run and `./scripts/qa-changed.bash` (rc 0).

## Toolchain and detector gaps

No manifest declaration; no register-graded detector for this, so the rule is a Python test
reading Python and bash.

Gaps in the project's toolchain, recorded here and not closed, because gates are outside this
remediation:

- `qa-changed.bash` reruns a `tests/helpers` rule only when the rule module changes, so a new
  copy in a scanned checker is caught only by `qa-all.bash` and CI.
- No QA gate runs a plan's checker tests (`test_m7_check.py`, `test_goodbye_check.py` and the
  others touched here). They were run by hand at the tip, all passing.
- The QA.md section arrived in the fix commit, not the red `2722a74c6`, so at the red the
  printed identifier resolved only to the test module's docstring.

Reviews: conformance round 1 (Plan 00211 `subagent-reports/261022-a3-m3-conformance-round1.md`,
NOT YET CONFORMING; its major finding answered by the fixes above) and Opus round 1
(`261022-a3-m3-review-round1.md`, MERGE).

## The original defect

Fixed with tests in `test_m7_check.py`: the settings `seat.py` writes pass; the Python-era
guard, the kit's bare command, a different refresh interval and inbound refuse each fail; the
guard and the kit settings are checked to agree. The checker from before the fix, run on the
settings `seat.py` writes today, fails with the host's message word for word; the fixed one
passes. The host rerun is queued in `CLAUDE/Plan/meta-deploy.list` (the 00201 entry).
