# QA Review addendum — Plan 00125, Task 5.3: judging the two proposed fixes

> Provenance: `qa-reviewer` subagent (Opus 5), 2026-09-16, Bash heredoc because `Write` is
> withheld (seventh round). Companion to `260916-qa-reviewer-00125-final-opus-5.md`.

## 0. THE TREE MOVED WHILE I WAS ANSWERING — read this first

The dispatch said: *"The tree is untouched at `ac706483` and stays that way until you report
— including these two fixes, which is why I am sending them rather than applying them."*
That is no longer true. Measured at 05:24:18, mid-answer:

    $ git status --short
     M helpers/qa_environment/unittest_counts.py
     M scripts/lib/qa-helper-summary.bash
     M scripts/qa-all.bash
    $ git diff --stat
     3 files changed, 71 insertions(+), 26 deletions(-)
    $ ls --time-style=full-iso
     05:23:47  scripts/lib/qa-helper-summary.bash
     05:23:58  scripts/qa-all.bash
     05:24:08  helpers/qa_environment/unittest_counts.py

HEAD is still `ac706483`. A **third function, `qa_gate_detail()`**, now exists at
`scripts/lib/qa-helper-summary.bash:262`, and the header has been partly rewritten.

**Attributability.** Everything in the main report was measured while the tree was clean at
`ac706483`; I re-confirmed clean at the end of it, and the edits began at 05:23:47, after.
**The main report stands as written, pinned to `ac706483`.** Its line numbers into
`qa-helper-summary.bash` and `qa-all.bash` are already stale — which is, precisely, the
citation argument, demonstrated on my own report eleven minutes after I filed it.

**I have not reviewed the in-flight changes and this addendum does not judge them.** Two
observations offered only so they are not lost, explicitly NOT findings:

- The header is currently self-contradictory mid-edit: `:6` still says "it defines **one
  function**", `:9` now says "The other **two** functions here DO parse output", `:38` still
  says "It is also **ONE function** rather than two". Finding 1 named three sites; one moved.
- `qa_gate_detail()` exists in the library, while `scripts/test-qa-helper-summary.bash` is
  untouched since 04:55 and its definition-guard loop still covers only
  `helper_counts_summary qa_gate_case_count`. A new function with no case is the TDD order
  this repo requires, inverted. Presumably in flight — flagging so it is not forgotten.

This is the **fourth consecutive round** affected. It has now gone past inconvenience: it has
twice corrupted the attributability of a review while it was being written. **Endorsing the
worktree isolation unreservedly** — and the cost is no longer hypothetical, so it should be
done before the next round rather than after it.

## 1. Is `required=True` right, or does it break a legitimate caller?

**Right. No caller lacks an honest value.** Enumerated, not assumed — the module has exactly
three invocation sites in the whole repo:

| Site | Value it would pass | Honest? |
| ---- | ------------------- | ------- |
| `scripts/qa-helper-tests.bash:159` | `${#QA_TRACKED_HELPER_TESTS[@]}` (git ls-files) | yes — already passes it |
| `scripts/test-qa-helper-summary.bash:404` | `1` — one synthetic module `t_e2e` | yes; the case already asserts `(1 tracked)` |
| `tests/helpers/qa_environment/test_unittest_counts.py:271` | `1`, with an override param for the new case | yes |

Two arguments beyond "nothing breaks", both from the file itself:

- **The file's own stated principle, applied to its own input side.**
  `unittest_counts.py:33`: *"Every key is always present: an absent key and a zero must not
  look alike, because one means a clean run and the other means the reader has gone blind."*
  The `default=None -> len(args.modules)` fallback is exactly an absent **input** synthesised
  into a value indistinguishable from a measured one. Same defect, one layer up.
- **It is the file's existing idiom, not a new one.** `--counts-file` is already
  `required=True` (`:103`), justified at `:125-127`: *"an empty invocation must never look
  like a run."* Consistency, not novelty.

Must move with it, or the change is half-done:

- `unittest_counts.py:21-23` — the docstring usage example would become an **invalid
  invocation**. A documented command that fails is worse than none.
- `unittest_counts.py:116-118` — the help text ends "defaults to the number of modules
  given". Delete that clause.
- `scripts/test-qa-helper-summary.bash:404-405` and `test_unittest_counts.py:262` — add the
  flag; give the Python helper an override parameter so the new case can pass 7.
- Check `CLAUDE/QA.md:123` and `FINDINGS.md:152` for a spelled-out invocation.

On the proposed case: **`--tracked-modules 7` over one module is well chosen.** 7 cannot
arise from the default, from `len(args.modules)`, or from any arithmetic on a one-module
list, so it fails if the flag is ignored *or* silently aliased to the module count. Two
refinements: assert the **counts file's bytes** (`modules=1` / `tracked=7`), not only a
rendered line; and name the case for the property it pins — `AgentNotes.md:877`, "Name a test
for what it asserts."

**Required does not subsume the test.** It stops the bash caller dropping the flag; it does
not stop a future edit making `counts_text` ignore `tracked_count`. The `7 -> tracked=7` case
pins the wiring. Both halves are needed; you have both.

## 2. Should the reader refuse `tracked > modules`?

**Agree — do not add it. But the reason you gave is wrong, and written into a comment it
would be the ninth over-claim.**

The reason offered: *"a guard against an unreachable state is untestable through the
production path."* That does not distinguish this case from the guards already shipped.
`helper_counts_summary` refuses duplicate keys, unknown keys, non-numeric values, empty
values, negative counts and a foreign token — and **not one of those is producible by
`unittest_counts.py` either.** Every one is unreachable from the production path, and every
one is tested with a synthesized file (`skipped=-1`, `skipped=none`, `tests=`...). By the
stated reason, none of them should exist. The file's own body disproves the argument.

**The reason that does distinguish it is placement.** `tracked <= modules` is already
enforced upstream at `scripts/qa-helper-tests.bash:103-111`, by a guard that exits 2 and
**names the missing files**. A reader-side guard would fire later, on strictly less
information, against a state the upstream guard already refuses. The existing reader-side
guards have no upstream equivalent — that is the difference. It is DRY, not testability.

**Your own hypothetical is not a path to the state either.** "A tracked file was discovered
and then failed to import" cannot produce `tracked > modules`: `modules` is written as
`len(args.modules)` — the count of names on the command line — and an import failure yields a
`_FailedTest` without reducing it. The state is less reachable than you thought.

**One caveat, and it is the useful half.** That unreachability is **derived from** the
upstream guard, not intrinsic. Write the dependency down where it is relied on — one sentence
in `helper_counts_summary`: *"`tracked <= modules` is guaranteed by `qa-helper-tests.bash`'s
exit-2 cross-check, not here."* Without it, a future relaxation of that check silently widens
this reader's input domain and nothing connects the two. Same discipline that produced
findings 2 and 3.

## 3. Blocking or follow-up?

**`--tracked-modules`: blocks. Agreed, and your framing is better than mine.** My finding 5
asked for a test; deleting the state is strictly stronger than pinning it. A silent fallback
inside the mechanism built to make silence impossible is the plan's own class, in the plan's
own code.

**Citation conversion: blocks — on correctness, not on principle.** Two of the three
instances are *currently wrong*: `PLAN.md:164` and `FINDINGS.md:298` both cite
`qa-all.bash:138` where the invocation is `:139`. A plan document wrong today is drift, and
drift blocks. The third, `FINDINGS.md:372 -> qa-all.bash:610-627`, was **correct at
`ac706483`** (`jq -s` at 609, program spanning 610-627) — converting it is prophylactic and
free in the same edit. Leaving the seven stable ones alone is right, and the rule as worded,
"no line-number citation into a file this plan edits," is correctly scoped.

**I checked the four anchors for uniqueness** in `qa-all.bash` at `ac706483` — an anchor
matching twice would be worse than a line number:

    # Merge JSON from all checks   -> 1 hit  (605)
    jq -s                          -> 1 hit  (609)
    # Final terse summary          -> 1 hit  (630)
    qa-deployed-drift.bash         -> 1 hit  (141)

All unique. **Re-check after the in-flight edits land** — `qa-all.bash` has already gained 11
lines since I measured.

## Revised close on Task 5.3

Unchanged: **does not discharge yet.** The required list from the main report stands, with
items 3 and 4 replaced by your better versions:

1. Header and doc-comment corrections — findings 1, 2, 3 (one of the three sites for finding
   1 appears to be in flight already).
2. Finish Task 4.5's sweep — `qa-all.bash:129` (the nokill reader that has never matched) and
   `:246`, plus `FINDINGS.md:162`'s "every other hard gate".
3. `--tracked-modules` required, plus the `7 -> tracked=7` case through `main()`.
4. Convert the three `qa-all.bash` citations to the four unique anchors; record the rule.

Follow-up, not 5.3: the within-line match rule (nit 3); the `1,482` drift; anything arising
from `qa_gate_detail`, which I have not reviewed.

Nothing on that list is large. The code has been right for a while; the claims around it keep
outrunning it, and this round's two proposals are both corrections of that kind rather than
of the code.
