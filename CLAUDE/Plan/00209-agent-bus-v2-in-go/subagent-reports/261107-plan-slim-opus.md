# Plan 00209: PLAN.md slimmed (Tasks 0.5, 0.6, 2.4)

Branch `docs/plan-00209-slim` (from `origin/F44` at 69c72cff5), commit dbef4cbc8, worktree
`.claude/worktrees/plan-00209-slim`. Not pushed, not merged.

## Measurements (bytes, `wc -c` of PLAN.md)

| State                                                       | Bytes  |
| ----------------------------------------------------------- | ------ |
| F44 before                                                  | 36,393 |
| slim branch after (dbef4cbc8)                               | 32,768 |
| slim + head of #178 (`feat/bus-v2-n3f-c2a-int`), test merge | 34,219 |

The test merge was a real `git merge --no-ff` in a throwaway detached worktree (since removed):
clean, no conflict anywhere. That leaves 781 bytes under 35,000. The stack (merge base 2d4aa6842 to
#178's head) changes PLAN.md in one hunk only, at Task 3.2 (old line 318, +18 lines). It does not
touch Tasks 0.5, 0.6 or 2.4, and does not touch DONE-n0-n1.md or BUILT-n2.md.

## What moved where

- **Task 0.5**: the three answer bullets (Accepted; Irreversible with Gate A; Owner only with
  Gate B and the section 7.1 alternative) moved to `DONE-n0-n1.md`, Phase 0, as a new Task 0.5
  entry. PLAN.md keeps one summary line linking `[DONE-n0-n1.md](DONE-n0-n1.md)`.
- **Task 0.6**: the full task statement, the check's findings and the four design changes moved to
  `DONE-n0-n1.md`, Phase 0, as a Task 0.6 entry marked "still open in `PLAN.md`". PLAN.md keeps the
  task (🔄), a short statement, the ✅ check sub-item (short) and the ⬜ open sub-item (its four
  changes, condensed), with the same link.
- **Task 2.4**: the "Six N2 packages" paragraph and its six bullets moved to `BUILT-n2.md`, new
  section "The six N2 packages and their callers" under Task 2.4. PLAN.md keeps one line linking
  `[BUILT-n2.md](BUILT-n2.md#the-six-n2-packages-and-their-callers)`.

Why DONE-n0-n1.md rather than a BUILT-*/SPEC-* file: it is the existing home for Phase 0's task text
in full (Tasks 0.1 to 0.4 already live there); no BUILT or SPEC file covers Phase 0. Its header now
says Tasks 0.1 to 0.6, and that 0.6 is not finished. Flag if a different home is wanted.

Only wording changes in the moved text: checkbox markers dropped (as in the rest of that file),
"Decisions, above" became "Decisions, in `PLAN.md`", and the 0.6 entry gained its open-status
prefix. No status changed; nothing in the moved text shows a status is wrong.

## How nothing was lost

The original three task blocks were extracted from F44's PLAN.md. Every token matching numbers,
upper-case IDs (OD-A10, H-C15, INT6...), backticked spans, quoted phrases, bold spans and markdown
links (77 for 0.5/0.6, 45 for 2.4) was searched for in new PLAN.md + DONE-n0-n1.md + BUILT-n2.md:
0 missing. Each original line (checkbox stripped) was also searched verbatim: all found except
the two deliberate edits named above.

## QA

`./scripts/qa-changed.bash`: exit 0, "docs: whole gate" passed, 4 changed files.

## Not done

Nothing outstanding. The failsafe-cron advisory fired on an Edit; left to the coordinator
(subagents do not manage session crons).
