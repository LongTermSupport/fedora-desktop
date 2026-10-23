# Task 2.22 sweep: handoff 2

Branch `fix/checker-owned-values-sweep`, worktree `.claude/worktrees/agent-acdc1e0a86013e161-2dec0d03`.
Not pushed, not merged. F44 not merged in yet (b0eb800fa is already the base: nothing to merge
unless F44 moved again). Handoff 1: `261022-sweep-handoff-1.md`.

## Done (commits after 626447e33)

- **Step 1** `04f68d188`: declare.py `pinned_tuwunel_version(pin)` (declared_software uses it; an
  empty version is refused too); Plan 00161 triage_probe calls it and reads `declare.PIN_FILE`
  (TUWUNEL_PIN removed). Clause 1 of the rule now reads a literal wrapped in one call
  (`pathlib.PurePosixPath("...")`): red on the old TUWUNEL_PIN (line 60), green after.
  helpers/agent_bus is in the image: **CCY 3.100.13, container 2.79.8** (launcher, Dockerfile,
  docs/ccy-changelog.md, meta-deploy.list numbers).
- **Step 2** `c389a79b6`: `m7_check product-value FILE NAME [FILL...]` (any named text of a
  helpers/ module, `NAME[key]` for a dict entry, or a src-go/ Go string const; `{key}`/`%s` fills;
  tests in test_m7_check.py). Value copies converted in 00203 (M2, M3 `m3_read_names`/`m3_value`,
  M4, M6, robust, m2-check, triage-hs), 00194 (goodbye: reason read lazily in gb_judge, seats in
  gb_paths, state roots via M7\_\*), 00201 (M7 source-time reads `m7_value`, expect_plain),
  00209 (S1 `s1_value`), 00211 (stage 2 `stage2_value`, WHERE_PY takes the inbox names in argv),
  00213 (C5 unset list), 00128 (census parses with verdicts.STAGE). OWNED gained 12 entries and
  a PATH kind. Red at the parent 38 findings (`untracked/scratch/red_at.py`), green after.
  00211 `--stage 2 --fake` and 00194 `--fake`: FAKE ACCEPTED.
- **Step 3** (commit after this file, if QA passed; else staged): 00203 M3 reads the joined/left
  notice heads (hooks.py TEMPLATES) and the MEMBERSHIP/HUMAN kind lines (envelope.py
  KIND_LINES); ccy-teams remove.go names `removedHomeserverDown` (byte-identical) and M4 reads
  it. Clause 1 no longer reports an empty bash constant (`X=""`, set later by a reader).

## Counts for the report

Converted: 1 pin parser; value copies 38 rule findings at the red plus by-hand ones (00128
STAGE, 00211 WHERE_PY inbox names and STATE_DIR, 00194 GB_REASON and GB_RECORD_PY, 00201
M7_RECORD_PY x3 sites, sessions dir in M2/M3/M4/M6, expect_plain marker); texts: b20 (3 sites),
b21 (2 sites), b8 (1 site).

Referred (to list in PLAN Task 2.22, one sentence each):

- Go hot-package texts b1-b7, b9-b19, b22, b23 and 00203 M4/M6 fragments: built by string
  concatenation inside each call (`"session "+s+": ..."`), so naming them means a format constant
  per message, not a trivial change.
- helpers/pingbus and helpers/agent_bus inline texts (A4 CHOICES_TEXT and A5 cli.py; b25 health.py
  and checkout.py; b26 cli.py watcher words; b31 seat.py; b33 config.py Element refusal and
  roles.go; B10 seat.py): naming them changes the image (another container bump) and the
  agent-bus software digest.
- Inline values with no product name: `.config/pingbus` home (config.py home(), 00161 u23,
  00211 lxc home), `/usr/local/lib/agent-bus/tuwunel` and `agent-bus-state-` (agent-bus-install),
  unit names (row 24), `ccy-session=` label and `projects/-workspace` (row 32), `LAST_TOKEN=`
  (row 31), cc-teams mktemp (row 19), 00137, 00148, 00109, 00080 (rows 22, 25-27).
- Rows 17, 21, 29, 30, b17, b27 (done: m2-check now reads the variable), b34: UNSURE or not
  soundly copies (handoff 1 list).
- B4 wire tags: specification (docs/agent-bus-protocol.md sections 13, 15), not a copy.
- OWNED not widened for the converted texts: the old checkers held fragments ("you joined
  team", "kind: MEMBERSHIP"), and a fragment rule would report ordinary phrases.

Pre-existing failure (not this branch, also at b0eb800fa): Plan 00161
`test_acceptance_check` has 2 failures (expect_human: "a HUMAN line has the wrong version or
number of fields").

## Left

1. If step 3 is not committed: `./scripts/qa-changed.bash` (log `untracked/scratch/qa-step3.log`),
   commit with `untracked/scratch/msg3.txt`-style message.
2. Step 4: `git worktree add untracked/scratch/proof-wt <tip>`, then
   `python3 untracked/scratch/proofs.py untracked/scratch/proof-wt` prints the wrong-product
   table (each mutation restored); paste it into the report. Remove the worktrees after
   (`untracked/scratch/head-wt` is b0eb800fa, used for the pre-existing failure check).
3. PLAN.md Task 2.22: status, counts, the referred list above; the DBF report
   `subagent-reports/261022-sweep-dbf-report.md` from REPORT-TEMPLATE.md (rule red commits
   b0dd7c9e1, 04f68d188 parent, c389a79b6 parent).
4. Merge F44 if it moved; defence-before-fix:conformance-reviewer to CONFORMS; Opus review to
   MERGE. Never qa-all; no push.
