# Task 2.22 sweep: handoff 3

Branch `fix/checker-owned-values-sweep`, worktree `.claude/worktrees/agent-acdc1e0a86013e161-2dec0d03`.
Not pushed, not merged. F44 `89e0a1a71` merged in (`a1920ff03`); CCY 3.100.13 / container 2.79.8,
above F44's 3.100.12 / 2.79.7, and still unreleased, so further image or launcher changes on this
branch need no further bump (only the changelog entry and the meta-deploy text widened).

## Done since handoff 2

- Wrong-product proofs: 22 REJECTED, 00128 FOLLOWED (table in `261022-sweep-dbf-report.md`).
  The script is now committed: `sweep_wrong_product_proofs.py <scratch worktree of the tip>`.
  It found a real defect, fixed in `1df4b9a61`: 00211 stage 2 read three names in one array
  literal (only the last substitution's status reaches errexit).
- 00161 `test_acceptance_check` (2 failures at b0eb800fa): its fixtures lacked the HUMAN line's
  room field (added to `cli.LINES` in `a5486643d`); the checker was right. Fixed in `1df4b9a61`.
  Cause of qa-all passing: no gate runs a plan folder's `test_*.py` (`qa-helper-tests` reads only
  `tests/helpers/**`, `qa-changed`'s classifier maps only those). All 12 active plan test modules
  pass at the tip, run by hand.
- PLAN Task 2.22 and `261022-sweep-dbf-report.md` written (`9001c6a3a`).
- `qa-changed --base F44`: rc 0 (37 files) at `9001c6a3a`.
- Opus review round 1: MERGE (`261022-sweep-review-round1.md`). Its finding 1 is fixed here
  (`product-value NAME[key]` on a non-table now fails; red then green in `test_m7_check.py`).
  Findings 2 (the Go const reader also matches an indented line in a function body) and 3 (00194
  fake reject leg counts a failed read as a pass, the fake run still fails) are open, minor.
- `sweep_red_at.py <rev>` committed: 38 at `c389a79b6^`, 0 at `c389a79b6` (the step-2 widening's
  red and green from history, answering conformance F3's second option).

## Conformance round 1: NOT YET CONFORMING

The review: `261022-sweep-conformance-review.md` (copied from the reviewer's own worktree). Its main finding (F1, with SPEC section 4 quoted correctly): cost (a format
constant per Go message, an image bump, play reruns) is work, not a decision outside the
practitioner's authority. Every search row that is an instance must be fixed; only doubt, the
rule's width and the undefended classes go to the owner. The other findings: F2 a row-by-row
record with counts; F3 the widening's red (answered above); F4 which fixes were by pattern, with
the sample examined (batch 1 shared readers, the ~30 M4/M6 mark sites by `\Q<mark>\E`); F5
record both gaps in `CLAUDE/QA.md` (or the plan's decisions) and put the multi-substitution class
to the owner; F6 the rule must not become blocking while instances remain (or the rule waits
unmerged); F7 the rule's decisions should be discoverable from QA.md.

## Every search row (`261022-sweep-independent-search.md`)

FIXED: 1-16, 18, 20, 23 (named roots), 28, 33, 34; b8, b20, b21, b27.

LEFT, instances to fix (name the text in the product with the smallest change, use it there,
read it through `m7_check product-value`, add it to OWNED where distinctive):

- Go ccy-teams hot package and roles (00203 M3/M4/M6): b1, b2, b3, b4, b5, b6, b7, b9, b10,
  b11 (statusline.go/.py ` (down)`, with row 30), b12, b13, b14, b15, b16, b18, b19, b22, b23.
  Each message is concatenated in its call; give each a format const (`%s` fills, which
  `product-value` already does) and use `fmt.Sprintf` there, byte-identical output; Go tests
  must stay green (`go test ./internal/teams/...`).
- pingbus / agent_bus Python: b25 (health.py, checkout.py; board/health.go), b26 (cli.py watcher
  words), b29 (A4 `CHOICES_TEXT`, A5 cli.py, in m7_check), b31 (seat.py), b33 (config.py Element
  refusal and roles.go), row 14 (`.config/pingbus`: config.py `home()`, note it equals
  `ELEMENT_ACCEPTED_DIR`'s text for another purpose), row 17 (keeper and watch argv grammar).
- ccy bash: b30 (`lib/agent-bus-membership.bash`, `lib/common-pure.bash`, read by 00194
  `goodbye_check.py`), row 31 (`LAST_TOKEN=`, claude-yolo), row 32 (`ccy-session=` label,
  `projects/-workspace`), row 19 (cc's mktemp template, `files/var/local/claude-code/cc`).
  A bash constant is read like m2-check reads `REQUIRED_CONTAINER_VERSION`.
- Others: row 23's inline `/usr/local/lib/agent-bus/tuwunel` and `agent-bus-state-`
  (agent-bus-install, health.py); row 24 unit names (agent-bus-install `UNIT_FILES`, named);
  b28 (00167, `src-go/internal/ccproject/app.go`); b32 (00183, `src-go/internal/fetchage`);
  b17 (locate first).

DOUBT, to put to the owner with the sentence (do not fix silently): row 21 (`ccy-seats` label,
retired; the negative check cannot fail: possibly a live defect, ask whether to delete the check
or keep it as a retirement assertion); row 22 (00080 counts legacy unlabelled sessions by name on
purpose); rows 25, 26, 27 (00137, 00148, 00109 state what must be deployed, a shade of
specification); row 29 (the guard's head shape beside a read value); row 30 (the ` (down)` form,
stated deliberately); b34 (00061, not located); b24 and B4 (wire tags, specified in
`docs/agent-bus-protocol.md` sections 13 and 15: recorded as specification).

## Left, in order

1. Fix the LEFT rows (parallel batches by product suit: Go hot/roles; pingbus/agent_bus;
   ccy bash and cc; the rest). Each batch: product naming with byte-identical output and its
   tests, the checker reading it, OWNED where distinctive, a wrong-product row added to
   `sweep_wrong_product_proofs.py`. Changing helpers/pingbus or helpers/agent_bus changes the
   agent-bus software digest (already changed on this branch; play-agent-bus.yml is in
   meta-deploy). Any OWNED widening goes in its own commit before its fixes (rule red).
2. Rewrite the DBF report's referral section as a table of every row (fixed by hand / by
   pattern with the sample / doubt with its sentence / specification), with counts; state F4's
   pattern record; record in `CLAUDE/QA.md` under the rule: the excluded wider rules, the
   plan-test gap, and the multi-substitution class (7 sweep lines: 00211 fixed; 00103 completed
   plan; ssh-keys-rekey `printf`/`basename`; ps1-prompt single-quoted, a false hit;
   test-ccy-image-build-lock and test-ccy-ssh-handling and test-qa-tool-pin-converges are tests)
   as an undefended class the owner must decide on.
3. Conformance round 2 until CONFORMS; Opus round 2 to MERGE; `qa-changed --base F44` (never
   qa-all); no push, no merge.
