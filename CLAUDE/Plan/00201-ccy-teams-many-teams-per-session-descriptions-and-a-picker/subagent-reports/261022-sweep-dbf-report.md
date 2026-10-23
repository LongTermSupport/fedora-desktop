# Defence Before Fix report: checker-copies-cited-constant, clause 2 (Task 2.22 sweep)

Branch `fix/checker-owned-values-sweep`, from F44 `b0eb800fa`, F44 `89e0a1a71` merged in.
Defence red `b0dd7c9e1`; fixes in `626447e33` (batch 1), `04f68d188` (step 1, the pin),
`c389a79b6` (step 2, values), `f023a8f30` (step 3, texts), `1df4b9a61` (step 4, fixes the
wrong-product proofs found). Handoffs: `261022-sweep-handoff-1.md`, `261022-sweep-handoff-2.md`.
This extends the rule of `261022-m3-dbf-report.md` (Task 2.21), which stays as it was.

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, the
cached copies (`/root/.cache/defence-before-fix/spec/`).

## Toolchain

As in `261022-m3-dbf-report.md`: no manifest declaration; the rule is a stdlib `unittest`
detector under `tests/helpers/`, run by `qa-helper-tests.bash`, documented in `CLAUDE/QA.md`
("checker-copies-cited-constant"). Single-rule harness:
`python3 -m unittest tests.helpers.plan_acceptance.test_cited_constant_copy_rule`. The red at
any revision: `untracked/scratch/red_at.py <rev>` (the rule module of today run over the
checkers at that revision; scratch, not committed). Calibration assumed: no exceptions.

## Defect and class

Originating instances: the copies Task 2.21's search found and left (PLAN Task 2.22's site
list: 00203 M4/M6 step marks, the session home, the membership file names, `m2-check.bash`'s
mount and home, `_triage-hs.inc.bash`'s watcher log) and Plan 00161's second Tuwunel pin
parser, which had already drifted once (v1.9.3 against the pin's v1.9.4, fixed in Task 2.21).
Class: a checker holding a value its subject owns, with no comment saying so (clause 1 needs
the comment). Hazard: the copy drifts, and the checker fails a correct product or passes a wrong
one.

## Rule

Same identifier, `checker-copies-cited-constant`, second clause: `OWNED` lists product
constants (product file, constant, kind), each value read from the product source when the rule
runs. A line of an active plan checker (not its `test_*.py`, not a comment or docstring) whose
code holds one, in the form its kind says, is a finding. Kinds: ANYWHERE, FILE_NAME (after a
`/`), DIRECTORY (between two), LINE_START (a mark a pattern anchors, `^✓`), PATH (a whole path
and below it, not a longer sibling such as `/var/lib/agent-bus-applied`). A text with
placeholders is held when any fixed part of 12 or more characters is. Clause 1 also reads a
literal wrapped in one call (`pathlib.PurePosixPath("...")`) and no longer reports an empty bash
constant (`X=""`, set later by a reader).

Deliberately excluded: values the product does not own (the Matrix spec's event types, GitHub's
address, Claude Code's names, sysexits), and short or common fragments. The next wider rule,
`OWNED` widened to the converted message fragments ("you joined team", "kind: MEMBERSHIP"), was
not built: a fragment rule reports ordinary phrases in other checkers' evidence lines.

## Proof

At `b0dd7c9e1` the fixtures pass and the sweep fails. Counted by `red_at.py` with today's
`OWNED`: 83 findings at `b0dd7c9e1`, 39 after batch 1 (`626447e33`), 38 after step 1, 0 at the
tip (the handoff's 39 at the red was the `OWNED` of that commit). Clause 1's widening is red on
the old `TUWUNEL_PIN` (00161 `triage_probe.py:60`) at `04f68d188`'s parent and green after.
The rule module passes at the tip (10 tests, rc 0).

## Independent search

`261022-sweep-independent-search.md`, dispatched before the clause and without sight of it:
34 value copies (a) and 34 inline-text copies (b). Text search found the literal sites; reading
found the computed ones (00128's census re-implementing `verdicts.STAGE`, 00211's `WHERE_PY`
inbox names, 00194's goodbye reason) that no literal rule reaches; those were fixed by hand.
The rule initially caught the batch-1 names only; `OWNED` was widened in step 2 by 12 entries
and the PATH kind to the search's named, distinctive (a) values. The search wins on what an
instance is: the rest is referred below.

## Sweep and count

Scope: every `.py` and bash file of the active plan folders (clause 2), and for clause 1 also
`tests/` and `scripts/`. Excluded: `JOURNAL/`, `subagent-reports/`, completed plans (not
runnable), the checkers' own `test_*.py` (fake input), the rule's fixtures. 83 at the red, 0 at
the tip.

## Fixes

Each examined individually; none by pattern; nothing suppressed. Readers added to
`m7_check.py` (Plan 00201): `session-names`, `membership-dirs`, and `product-value FILE NAME [FILL...]` (any named text of a `helpers/` module, `NAME[key]` for a table entry, or a `src-go/`
string const; `{key}`/`%s` fills), with tests in `test_m7_check.py`. Converted: 00203 M2, M3
(`m3_read_names`, `m3_value`; the joined/left notice heads from hooks.py `TEMPLATES`, the
MEMBERSHIP/HUMAN kind lines from envelope.py `KIND_LINES`), M4 (remove.go's newly named
`removedHomeserverDown`, byte-identical), M6, robust, `m2-check.bash`, `_triage-hs.inc.bash`;
00194 goodbye (reason read in `gb_judge`, seats in `gb_paths`, state roots); 00201 M7 (source
time reads, `expect-plain` reads `MEMBERSHIP_OFF`); 00209 S1; 00211 stage 2; 00213 C5; 00128's
census parses with `verdicts.STAGE`; 00161 `u20_read_names` and `triage_probe` (the pin through
`declare.pinned_tuwunel_version`, the one reader `declared_software` also uses, which also
refuses an empty version). helpers/agent_bus is in the image: CCY 3.100.13, container 2.79.8,
above F44's 3.100.12 / 2.79.7.

Wrong-product proofs (`untracked/scratch/proofs.py` on a scratch worktree of the tip, each
mutation restored, the Python caches cleared before each run):

| Wrong product (scratch copy)                                          | Checker                                       | What it did                                                    | Verdict                             |
| --------------------------------------------------------------------- | --------------------------------------------- | -------------------------------------------------------------- | ----------------------------------- |
| `helpers/pingbus/seat.py` CHECKOUT_SEATS renamed                      | 00203 M3/M4/M6, 00194 gb_paths, 00201 M7      | product-value exit 1: NameError CHECKOUT_SEATS                 | REJECTED                            |
| `helpers/pingbus/seat.py` SESSIONS_DIR renamed                        | 00203 M2/M3/M4/M6                             | product-value exit 1: names no one-line text SESSIONS_DIR      | REJECTED                            |
| `helpers/pingbus/membership.py` APPLIED_FILE renamed                  | 00203 M6                                      | product-value exit 1: NameError APPLIED_FILE                   | REJECTED                            |
| `helpers/pingbus/config.py` ELEMENT_ACCEPTED_FILE renamed             | 00203 M6, 00211 stage 2                       | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/pingbus/config.py` ELEMENT_FLATPAK_DIR renamed               | 00211 stage 2                                 | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/pingbus/config.py` STATE_DIR renamed                         | 00211 stage 2                                 | product-value exit 1: names no one-line text STATE_DIR         | REJECTED                            |
| `helpers/pingbus/inbox.py` CONSUMED_DIR renamed                       | 00211 stage 2                                 | product-value exit 1: names no one-line text CONSUMED_DIR      | REJECTED                            |
| `helpers/agent_bus/render.py` STATE_ROOT renamed                      | 00203 M3/M4/robust/triage-hs, 00201 M7, 00194 | product-value exit 1: names no one-line text STATE_ROOT        | REJECTED                            |
| `helpers/agent_bus/health.py` INSTALL_STATE_ROOT renamed              | 00203 robust, 00201 M7, 00194                 | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/agent_bus/checkout.py` APPLIED_DIR renamed                   | 00201 M7, 00194, 00209 S1                     | product-value exit 1: NameError APPLIED_DIR                    | REJECTED                            |
| `helpers/pingbus/statusline.py` SESSIONS_DIR_TEMPLATE renamed         | 00201 M7, 00194                               | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/pingbus/protocol.py` TEAM_NAME_PATTERN renamed               | 00209 S1                                      | product-value exit 1: NameError TEAM_NAME_PATTERN              | REJECTED                            |
| `src-go/internal/bus/seat/seat.go` seatsDir renamed                   | 00209 S1                                      | product-value exit 1: names no one-line text seatsDir          | REJECTED                            |
| `helpers/pingbus/membership.py` MEMBERSHIP_VAR renamed                | 00203 m2-check, 00213 C5                      | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/pingbus/hooks.py` GOODBYE_REASONS[prompt_input_exit] renamed | 00194 gb_judge                                | product-value exit 1: names no one-line text                   | REJECTED                            |
| `helpers/pingbus/hooks.py` TEMPLATES[joined] renamed                  | 00203 M3                                      | product-value exit 1: names no one-line text TEMPLATES[joined] | REJECTED                            |
| `helpers/pingbus/envelope.py` KIND_LINES[MEMBERSHIP] renamed          | 00203 M3                                      | product-value exit 1: names no one-line text                   | REJECTED                            |
| `src-go/internal/teams/hot/remove.go` removedHomeserverDown renamed   | 00203 M4                                      | product-value exit 1: names no one-line text                   | REJECTED                            |
| tuwunel.pin names an empty version                                    | 00161 triage_probe                            | exit 1: refused: tuwunel.pin names no single tuwunel_version   | REJECTED                            |
| membership.py MEMBERSHIP_OFF = none, container has =off               | 00201 M7 P1 (expect-plain)                    | exit 1: the container's environment sets PINGBUS_MEMBERSHIP    | REJECTED                            |
| verdicts.py STAGE narrowed to ✓                                       | 00128 probe-gate-census                       | exit 0: counts `ruff` only                                     | FOLLOWED (counts what STAGE parses) |
| `helpers/pingbus/inbox.py` INBOX_DIR renamed                          | 00211 `acceptance.bash --stage 2 --fake`      | exit 1: names no one-line text INBOX_DIR                       | REJECTED                            |
| `helpers/pingbus/hooks.py` GOODBYE_REASONS key renamed                | 00194 `acceptance.bash --fake`                | exit 1: FAIL G1 (fake) one goodbye in each team                | REJECTED                            |

The first run had three ACCEPTED rows. Two were the proof's own fault: two mutations of one file
of the same size in the same second let Python reuse the first one's `.pyc`; the script now
clears the caches, and both rows reject. The third was a real defect in this branch: 00211
stage 2 read the three inbox names in one array literal, where only the last substitution's
status reaches errexit, so `--fake` passed with `INBOX_DIR` gone. Fixed in `1df4b9a61` (one
`+=` assignment per read); red: `--fake` exit 0 with the name renamed; green: exit 1, and the
real tree's `--fake` still passes. The class is referred below (no rule exists for it).

## Decisions referred to the owner

Count fixed: 83 clause-2 findings (today's `OWNED`), the by-hand computed copies (00128 STAGE,
00211 `WHERE_PY` inbox names and `STATE_DIR`, 00194 `GB_REASON` and `GB_RECORD_PY`, 00201
`M7_RECORD_PY` at 3 sites, the sessions directory in M2/M3/M4/M6, `expect_plain`'s marker), the
pin parser, and the texts the product names (search b20 at 3 sites, b21 at 2, b8 at 1).
Remaining, referred, each needing a product change or a judgement that is the owner's:

- ccy-teams hot-package texts (b1-b7, b9-b19, b22, b23) and 00203 M4/M6 fragments: each is
  built by concatenation inside its call (`"session "+s+": ..."`), so naming it is a format
  constant per message in deployed Go code.
- pingbus and agent-bus inline texts (A4 `CHOICES_TEXT`, A5 cli.py; b25 health.py and
  checkout.py; b26 cli.py's watcher words; b31 seat.py; b33 config.py's Element refusal and
  roles.go; B10 seat.py): naming them changes the image (another container bump) and the
  agent-bus software digest, so every team's play reruns.
- Inline values the product does not name: `.config/pingbus` (config.py `home()`, 00161 u23,
  00211's lxc home), `/usr/local/lib/agent-bus/tuwunel` and `agent-bus-state-`
  (agent-bus-install), unit names (row 24), the `ccy-session=` label and `projects/-workspace`
  (row 32), `LAST_TOKEN=` (row 31), cc-teams' mktemp (row 19), and rows 22, 25-27 (00080,
  00109, 00137, 00148).
- Rows 17, 21, 29, 30, b17 and b34: unsure, or not soundly a copy (b27 was done: `m2-check`
  reads the variable).
- B4's wire tags: the protocol specification (`docs/agent-bus-protocol.md` sections 13 and 15),
  which a checker may state; recorded, not a copy.

What to try next: name each text in its product module (a format constant for the Go ones),
bump and deploy, then add it to `OWNED` and read it through `product-value`. The rule is merged
at its width, sound for what it claims; the excess is a referral, not an exclusion.

Two gaps found on the way, also the owner's:

- No QA gate runs a plan folder's own `test_*.py`: `qa-helper-tests` enumerates only
  `tests/helpers/**`, and `qa-changed`'s classifier maps only those. Plan 00161's
  `test_acceptance_check` had been red since `a5486643d` (Plan 00211 added the HUMAN line's room
  field to `cli.LINES`; the fixtures still built five fields): two positive tests failed, and the
  negative ones passed on the field-count refusal instead of on what each tests. The checker was
  right. Fixed in `1df4b9a61` (the fixtures carry the absent room field `-`); all 68 pass, also
  with a malformed line made an error rather than a refusal. All 12 active plan test modules pass
  at the tip, run by hand.
- In bash, one assignment carrying several command substitutions reports only the last one's
  status, so errexit misses the others (`a=("$(false)" "$(true)")` continues; so does
  `a="$(false)$(true)"`). A text sweep of the repo-owned trees finds 7 lines: the 00211 one
  (fixed), and the rest harmless (`basename`, `printf`) or in tests and a completed plan. No
  rule detects it; a sound one needs the syntax tree (sibling, not nested, substitutions), which
  `src-go/qa/bashlimits` already builds. Building that gate is outside this remediation.

## Permanence

A test module under `tests/helpers/`, run by `qa-helper-tests.bash` in every `qa-all.bash` and
CI; it fails, never warns. `./scripts/qa-changed.bash --base F44` after the F44 merge
(`a1920ff03`, with this report and the PLAN update): rc 0, 37 changed files, the rule module
among the tests it reran because it changed on this branch.

## Toolchain and detector gaps

No manifest declaration; no register-graded detector, so the rule is a Python test reading
Python, bash and Go. `qa-changed.bash` reruns a `tests/helpers` rule only when the module
changes. The plan-test and multiple-substitution gaps are above.

## The original defect

The listed sites and the second pin parser are read from the product (above); each converted
checker refuses a wrong product (the table). Host reruns are queued in
`CLAUDE/Plan/meta-deploy.list`.
