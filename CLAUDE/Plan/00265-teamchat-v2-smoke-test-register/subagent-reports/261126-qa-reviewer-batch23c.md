# qa-reviewer: batch 23, fourth build (81f4aa618..34fe00406)

Verdict: **PASS**. The reviewer is read-only; the lead saved its reply here.

## The third review's blockers

1. A team's name in a fact-check report: fixed. `qa-private-names tree 34fe00406` finds only
   the three held journal lines (`JOURNAL/2026-11-25/134943Z-finding-cf58c8.md:15,20,21`).
   The same scan on 81f4aa618 also finds `261126-plan-fact-checker-r80-r83.md:17`, so the
   scanner sees the name and it is gone.
2. The pin play under `--check`: fixed. `play-git-hooks-security.yml:127` (`check_mode: false`
   on the read-only find) and `:168` (the pin's assert skipped in check mode).

## Should fix

- The name is still in commit 2fad98c7e on `origin/v2-smoke`, and R84 does not say so.
  Removing it needs a force-push, which is the owner's; Q21 settled on no history rewriting.
- R84 called the cause of the unset `core.hooksPath` unchecked. The hard-coded `repo_path`
  at `play-git-hooks-security.yml:15` is the likely cause (known; fixed separately with its
  detector).
- `261126-plan-fact-checker-r79b.md` makes no claim about R76, though the message of 3f7b1b54a
  says it checks R76.

## Nits

- `play-agent-bus-element.yml` uses `dnf` where `CLAUDE/AnsibleStyle.md:99` asks for `package`.
- `watch.go:457` prints "above 0 … refused" when the scope is 0.

## Clean

W12 and its test, the plays' syntax, R84's claims, issues #429 and #215, the meta-deploy queue.
The 34fe00406 tree equals 4cc481ad6's.

## Gates

No `qa-all` (sub-agents may not run full QA). `go test -race` passes. Plan QA reports three
blocks, none in 00265 or 00268.
