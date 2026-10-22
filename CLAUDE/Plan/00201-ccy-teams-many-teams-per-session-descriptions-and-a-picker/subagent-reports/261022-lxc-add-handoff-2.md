# Handoff 2: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Worktree: the agent worktree whose branch is fix/lxc-add-already-declared
(`git -C /workspace worktree list`). Not pushed. Tip at handoff: the commit that adds this file,
on top of 7e40c23a9. Use Write/Edit only (sed is blocked); never qa-all, a play, a push or a merge.

## Done since handoff 1

- 11b48c687: local F44 (c159e6626) merged in; one meta-deploy.list conflict resolved.
- 2bf348eab: create wizard `Wizard.unanswerable` (ErrUsage re-asks, interrupt cancels, others
  stop); `ghLookup`/`ghNotFound`: only gh's "(HTTP 404)" is not found (the coordinator's ask).
- 9778b10d3: DBF report draft.
- 1c93f845b: conformance review 1 and Opus round 1 answered (rule tightened; declare.py: absent
  team 78 plus Go mirror, `set` 78 only for an untouched key, `_replace_lxc` comment guard,
  create "already declared" names `ccy-teams apply`; "declare.py failed (exit N)" in lxcadd and
  create; lxc-add Stop names `ccy-teams apply`). **CCY 3.99.14.14 / container 2.78.17** (the
  pre-commit hook asked for a new bump for declare.py; changelog, meta-deploy, PLAN updated).
- 7e40c23a9: RED probe `stopsOnOneKindThenReasks` in `src-go/qa/rules/testdata/src/fix/internal/badrefusal/bad.go`
  (TestRefusalKind fails at bad.go:91); round-2 reports committed.

Reviews so far: conformance-review.md (DOES NOT CONFORM), conformance-review-2.md (DOES NOT
CONFORM), review-round1.md (FIX-FIRST), review-round2.md (FIX-FIRST).

## Left to do

1. Rule (conformance-2 finding 1): in `src-go/qa/rules/refusalkind.go`, a returned code must
   clear a recovery only when the return is unconditional in the block that recovers it: for a
   recovery in an `if` Init/Cond, a direct statement of that `if`'s body; for a recovery in an
   assignment statement of a block, a later direct statement of that block; for a type-switch
   case, a direct statement of the case body. `ErrUsage` named anywhere still clears the
   function. Then `go test ./rules -run TestRefusalKind` green, and the driver over both modules
   clean: `cd src-go/qa && go build -o ../../untracked/scratch/bin/rules-driver ./driver && cd .. && go vet -vettool=../untracked/scratch/bin/rules-driver ./...`
   (and the same in src-go/qa). The real-tree code-returns are wizard.go `run` and adder.go
   `summary` (both if-init form). Update GoProjectRules.md's section to say so.
2. Conformance-2 finding 2: the "Not caught" shapes (ErrUsage named but branched the wrong way;
   `ErrUsage || ErrRefused` then a re-ask; a kind read of another error; a re-wrapped refusal)
   carry the hazard: list them in GoProjectRules.md "Not caught" and in the DBF report as
   detector gaps with reasons, and under "Decisions referred to the owner" as a rule left
   narrower than the hazard.
3. Round 2 finding 1 (FIX): `helpers/agent_bus/declare.py` `set_values` (~line 1148): some
   teamfile rules name one key but read others (`exclude` reads `org`/`repos`, `humans` reads
   `removed_humans`, required `org` reads `repos`). Keep a table of the keys each such rule
   reads (default: the key itself) and give 78 only when none is being changed. Test first in
   `tests/helpers/agent_bus/test_declare.py` SetTest: exclude `["foo"]` with repos
   `["example-org/foo"]` in the file, setting repos to a list that still holds `example-org/foo`
   must be 64 with the file unchanged. Read `helpers/agent_bus/teamfile.py` for the rules.
4. Round 2 nits: Task 2.20 to say `ops.go:160`/`adder.go:341` keep their own 64 for an absent
   team (CLI usage), and that `add-repo` of a trusted repository is UNCHANGED.
5. Journal entry (`CLAUDE/Plan/mkplan.bash --journal 00201 action <body-file>`), DBF report
   update, `./scripts/qa-changed.bash` green, commit (declare.py changes need a container bump
   again: the pre-commit hook will say; bump to container 2.78.18 / CCY 3.99.14.15, or keep the
   branch's single unreleased release by retitling the changelog entry as done in 1c93f845b).
6. Conformance re-review 3 and Opus round 3 over `git diff F44...HEAD` until CONFORMS and
   MERGE; save as subagent-reports/261022-lxc-add-conformance-review-3.md and
   261022-lxc-add-review-round3.md. The dedicated conformance-reviewer type fails to launch
   (WorktreeCreate hook); use general-purpose told to follow its definition under
   /root/.claude/plugins/cache/defence-before-fix/defence-before-fix/0.1.1/agents/.

Owner's rules: a config-state refusal stops at once naming the file and entry, never
re-prompts; offline members never block (another branch owns that; do not touch it).
