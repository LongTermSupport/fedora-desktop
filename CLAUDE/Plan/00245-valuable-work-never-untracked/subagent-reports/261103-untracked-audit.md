# Plan 00245 Task 2.1: the untracked-work audit (detector, red proof, real-checkout sweep input)

## What was built

- `src-go/qa/untrackedaudit` and command `src-go/qa/cmd/qa-untracked-audit`, wrapper
  `scripts/qa-untracked-audit.bash`, data `scripts/untracked-policy.yml` (YAML, strictly loaded;
  the nested module already depends on a YAML library and not on a TOML one directly).
- Check 1 (untracked/ files): files older than `ageHours` (24) outside the registered stores
  (`repos`, `worktrees`, `plan-runs`, `clones`, `facts`). `.md`, `.patch`, `.diff` older than the
  floor are findings whatever the name (kind `untracked-asset`, `-decision` for a name containing
  "decision", `-patch`); any other old file is a finding (`untracked-unshaped`) unless its name is a
  scratch shape. The daemon's stores (`agent-reports`, `issue-reports`) give a WARNING count only.
- Check 2 (unpushed work): local branches with commits no remote holds (no age floor; exempt list
  `activeBranches`, empty), linked work trees with uncommitted changes whose newest change is older
  than the floor, detached work trees with unpushed commits, any stash.
- Every finding names its age and the tracked home from the design's table. Exit 0 clean, 1
  findings, 2 cannot judge (bad policy, git failing, unreadable tree). `--policy FILE` reads a
  policy from elsewhere (a checkout that does not carry the file yet).

## Proof

- Unit tests over a fake tree (`fstest.MapFS`), a fake git and an injected clock: green controls
  (fresh file, old `*-msg.txt`, old capture, registered stores, file just under the floor) and red
  fixtures (36-hour research note, patch, diff, owner-decisions sheet, old unshaped file, unpushed
  branch, dirty old work tree, detached work tree, stash), plus exit 2 cases.
- Red proof: with the age check made to skip `.md` files, `TestRedFixtures` (three cases),
  `TestDaemonStoresWarn` and `TestReportIsGroupedByKind` failed; reverted, all pass.
- Real-git end-to-end tests in `cmd/qa-untracked-audit` (repository with a bare remote: clean,
  old note, unpushed branch, stash, old dirty linked work tree, not a repository).
- Package coverage 88.3 percent; lint (golangci-lint) clean.

## Real-checkout run (read-only, `--root` the main checkout)

Exit 1. 63,271 findings, 2 warnings:

| Kind                 | Count                               |
| -------------------- | ----------------------------------- |
| untracked-asset      | 6,197                               |
| untracked-decision   | 108                                 |
| untracked-patch      | 28                                  |
| untracked-unshaped   | 56,879                              |
| unpushed-branch      | 12                                  |
| dirty-worktree       | 47                                  |
| unpushed-worktree    | 0                                   |
| stash                | 0                                   |
| daemon store warning | 453 agent-reports, 21 issue-reports |

The bulk is whole clones and tool working trees sitting unregistered under `untracked/` (one
directory holds 42,594 unshaped files, another 11,305, a third 1,571): the sweep must either
register those as stores in the policy (decision for the owner) or remove them. Anonymised first
lines of the report, by kind:

```
untracked-asset (6197):
  untracked/<dir-A>/PLAN.md  (3190h old)
  untracked/<dir-B>/<file>.md  (7154h old)
  untracked/<handoff>.md  (593h old)
  untracked/config-changes/<range>/ADVISORY.md  (1149h old)   [6 such, 165h to 1149h]
  untracked/<clone-C>/**/*.md  (483h to 574h old)              [many; an unregistered clone]
untracked-decision (108): owner-decision and similar sheets (mostly inside <clone-C> trees)
untracked-patch (28):
  untracked/scratch/<plan>-cli.diff  (309h old)
  untracked/scratch/<plan>-fix.diff  (309h old)
  untracked/scratch/<plan>-wip.patch  (170h old)
  untracked/scratch/<branch>.diff  (288h old)
  untracked/scratch/<proto>.diff  (301h old)
unpushed-branch (12): 12 local branches, 5 to 17 commits each, tips 303 to 306 hours old
dirty-worktree (47): linked work trees with uncommitted changes older than 24 hours
```

Nothing under `untracked/` or any branch was touched.

## Blocking

Not wired. There is no "red until swept" convention in the repo, so `CLAUDE/QA.md` gained a
"Gates waiting on a sweep" section recording this gate and what it waits on, and the gate is left
out of `scripts/qa-gates.tsv` (wiring it now would fail every `qa-all.bash`). The step after Task
2.2's sweep is one row in `scripts/qa-gates.tsv`. CI cannot run it (a clean checkout has no
`untracked/`), stated in QA.md.
