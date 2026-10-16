# Fable review: fix/teams-wait-play-lock at 8da095cb (Task 2.17)

Branched from F44 7f8b1c23; F44 is now a153aede with an empty meta-deploy queue. Developer's
report: `261016-teams-wait-play-lock-opus.md`. Reviewed on `review/teams-wait-play-lock`.

## Verdict

**FIX-FIRST** (two items, both small; the design is sound and is the right one).

1. `CLAUDE/Plan/meta-deploy.list` conflicts with a153aede: the resolved queue must be ONLY
   the play-claude-yolo.yml entry with its description (speech-to-text ran at 7f8b1c23).
2. Pick a merge order against `origin/fix/context-headings` and say it in the merge commit:
   that branch changes `ui.Picker.Pick` to `Pick(ctx, h ui.Heading, spec)`, and this branch's
   `src-go/internal/teams/ops/playlock.go:185` calls `o.Picker.Pick(screenCtx, spec)`. Textually
   both merge (merge-tree shows import-only conflicts), but the second one in will not compile
   until the wait screen is given a heading (`flow.Here("Waiting for the play lock")` or the
   like). Whoever merges second owns that line.

Recorded, not blocking: the spec (SPEC-many-teams.md section G) was written after the code,
against the spec-first mandate (see "Spec-first breach").

## 1. Lock correctness

**Same lock.** `src-go/internal/playlock/playlock.go` is lock.py's lock, not a second one:
same file name (`playlock.go:30` = `lock.py:51`), same runtime-directory rule
(`playlock.go:60-65`; lock.py's extra `euid == uid` guard is moot because ccy-teams is never
root), same refusals (`checkDir` `playlock.go:292-313` = `_check_runtime_dir` `lock.py:85-99`;
`open` `playlock.go:262-290` = `open_lock` `lock.py:102-133`: O_RDWR|O_NOFOLLOW|O_CLOEXEC,
create O_EXCL 0600, regular file owned by uid), same flock(2) LOCK_EX|LOCK_NB on the
descriptor, same holder line `pid= what= since=` with the same stamp layout (`playlock.go:240`
= `lock.py:171` = `run.bash:1910`). Parity is proven against the real helper:
`playlock/parity_test.go:52` runs `lock.py path` and `lock.py held` over a Go-taken,
Go-delegated hold, and checks the spoof case (env set, no descriptor) is exit 3.

Differences, none material: Go retries the create race once rather than twice
(`playlock.go:269-274` vs `lock.py:111-133`); Go detects a symlink by Lstat after the failed
open rather than by ELOOP (`playlock.go:276`). No fchown, correctly: Go never opens as root.

**No check-then-take race.** `Take` loops on flock on its own descriptor (`playlock.go:219-238`);
the holder is only read for display. Nothing is decided from the read.

**Descriptor containment.** The lock descriptor is O_CLOEXEC (`playlock.go:267`), so no child
started through execx inherits it except where `ExtraFiles` names it: the two play runs
(`ops/ops.go:269-274`, `ops/ops.go:357-364`). fzf, gum, declare.py, sudo, systemctl, tmux, the
session launchers: none receive it. `execx.Cmd.ExtraFiles` is the one seam
(`execx/execx.go:36-38,110`), tested at `execx/execx_test.go:48`.

**A child's background process keeping the lock.** Real, and pre-existing: run.bash itself
runs `ansible-playbook` with `PLAY_LOCK_FD` open (`run.bash:1901`, the play at
`run.bash:2269-2272`), so any task that daemonises a shell child already inherits the lock
today. The Go delegation adds no new exposure: the delegated fd 3 reaches the same
ansible-playbook the same way. Not a finding against this branch; worth a line in lock.py's
doc one day.

**Spoofing.** `lock.py held` (`lock.py:178-205`) checks the fd is open, is the same
dev/inode as the lock path, AND that a non-blocking re-lock on that open file description
succeeds, so a random fd or an unlocked copy of the file fails with exit 3, which
`run.bash:1894` makes fatal. The parity test pins the exit 3.

**Release on every exit path.** Normal: `Locked` releases after fn (`ops/playlock.go:62-68`),
nested holds are the same hold (`:54-56`, test `TestANestedHoldIsTheSameHold`). Ctrl-C during the
play: the run context is `signal.NotifyContext` (`teams/app.go:121-123,134`), execx signals the
child, runPlay returns, Locked releases. Panic or SIGKILL: no `recover()` anywhere in
`src-go/internal`/`cmd`, the process dies and the kernel drops the flock with the descriptor.
A Stop waiting that races a take: `waitOnScreen` cancels the waiter, drains `done`, and
releases a lock taken in the same instant (`ops/playlock.go:194-200`, test
`TestStopWaitingOnTheScreenRunsNothingAndHoldsNothing`).

One nit, not blocking: `Locked` keeps `o.held` as a field (`ops/ops.go:86`); it is only sound
because every flow is single-threaded, and `waitOnScreen` is the one place a goroutine exists.
It never touches `o.held`. Fine today; the comment on the field could say so.

## 2. Write + apply atomicity

Every host_vars writer, and whether its play runs in the same hold:

| Writer                                | Hold                                                                                   |
| ------------------------------------- | -------------------------------------------------------------------------------------- |
| `settings.go:349` `Declare.Set`       | inside `Locked` `settings.go:346-352`; apply at `:350` in the same fn                  |
| `offers.go:262` `Declare.Describe`    | inside `Locked` `offers.go:261-271`                                                    |
| `offers.go:149` `Declare.DropLimit`   | inside `Locked` `offers.go:148-157`                                                    |
| `widen.go:116` `Declare.AddRepo`      | inside `Locked` `widen.go:94-97` via `widenHeld`                                       |
| `wizard.go:863,877` Create, Element   | inside `Locked` `wizard.go:834-836` via `declareAndApply`                              |
| `adder.go:590,594,678` LxcSetRole/Add | inside `Locked` `adder.go:647-649` via `declareAndApply` (the demote and its undo too) |
| `takedown.go:236` `Declare.Remove`    | inside `Locked` `takedown.go:224-227` via `takeDownHeld`                               |
| `login.go:377` `Declare.Element`      | inside `Locked` `login.go:376-382`                                                     |

Play runs: `runPlay` and `runElementPlay` refuse without a hold (`ops/ops.go:269-272`,
`ops/ops.go:357-361`, S-G11); nothing outside `ops` names `Cfg.Play` or `Cfg.ElementPlay`
(grep over `src-go/internal/teams`, non-test, empty). `ApplyTeams` and `RunElementPlay` wrap
themselves in `Locked` so a caller that forgets is still inside a hold, with the write outside
it; today no such caller exists.

**Stopped wait writes nothing**: `Take` returns before fn runs (`ops/playlock.go:57-60`).
Tested per flow: `settings_test.go`, `offers_test.go`, `chooser_test.go`, `wizard_test.go`,
`adder_test.go`, `login/element_test.go`, `TestRemoveDeclaresNothingAbsentWithoutTheLock`.

**Exit 75 still reaching ccy-teams** is named as a refused lock, not a raw failure
(`ops/ops.go:386-388`, `:366-369`; test `TestAPlayRefusedTheLockIsSaidAsThatNotAsARawFailure`),
`ApplyTeams`'s deferred `o.changed()` (`ops/ops.go:124`) marks the board, and the flow's own
"saved but not applied" line follows (`offers.go:267`, `wizard.go:841-849`). S-G9 holds.

One behaviour to know: in the settings screen, `save` returns the unsaved changes to the screen
on a not-taken lock (`settings.go:353-357`), as S-G8 says. In the create wizard a not-taken
lock after the person has answered every question exits the wizard (`wizard.go:838-842`); the
answers are lost. S-G8 allows this ("run ccy-teams create again"); it is the one flow where a
30-minute wait is the better path, and the screen gives it.

## 3. UI

The wait screen is a `ui.PickSpec` through `o.Picker.Pick` only (`ops/playlock.go:185,234-243`):
no second TUI. Its one action is the exit row, labelled `Stop waiting (nothing is changed)`;
the legend `Enter choose   Esc or q stop waiting` names both keys (`:237`), so
project_hidden_bindings has nothing hidden to find. The header lines are short and wrap in the
header, not in a row, so nothing is truncated. It is redrawn when the holder or the minute
changes and closed by itself once the lock is taken (`:180-203`, test
`TestOnATerminalTheWaitIsAScreenThatGoesOnByItselfWhenTheLockIsFree`), which does mean fzf is
killed and restarted once a minute during a wait; a flicker, acceptable.

Context heading: none, because this branch has no `ui.Heading` yet; see FIX-FIRST 2. Once
merged with context-headings the screen should carry `Teams › Waiting for the play lock` like
every other ccy-teams screen.

Non-tty path (`waitSaying`, `:129-139`): two lines on stderr saying who holds it and until
when, one line each time the holder changes, Ctrl-C through the context (test
`TestCtrlCDuringTheWaitRunsNothing`). On a tty with no fzf it takes the same path (`:88`).

## 4. Version bump

Not required, and the branch is right not to bump. `.claude/rules/ccy-version-bump.md` scopes
the mandate to `files/var/local/claude-yolo/**` and `helpers/ssh_agent_filter/**`;
`CLAUDE/ContainerRules.md:59-116` lists the launcher, `lib/*.bash`, the Dockerfile,
entrypoint.sh and what the image bakes in. `src-go/` is in none of them, and no hash or hook
checks the ccy-teams binary against `CCY_VERSION`. The precedent was checked: every 3.99.12.x
bump that mentions ccy-teams carried a mandated file (3.99.12.10 = `lib/tmux-session.bash`;
3.99.12.9 and .8 = the image's pingbus; .5 = `helpers/agent_bus/` in the image; .4 = declare.py
in the image), and `origin/fix/context-headings` (src-go only) also bumps nothing. There is no
precedent of a src-go-only bump, so "consistent" here means no bump. `origin/fix/lxc-checkout-typed`
is already at 3.99.12.14 / container 2.77.8, so a gratuitous bump here would also have to
leapfrog it.

## 5. meta-deploy merge

`git merge-tree --write-tree a153aede 8da095cb`: one conflict, `CLAUDE/Plan/meta-deploy.list`
(F44 emptied the queue; the branch carries speech-to-text plus play-claude-yolo). Resolution:
keep ONLY the Plan 00201 Task 2.17 comment and `playbooks/imports/play-claude-yolo.yml`. The
description on the branch is accurate (ccy-teams only, no image rebuild, what the wait does).

`origin/fix/context-headings` 7e6f2ca6: conflicts in `meta-deploy.list`,
`chooser/widen.go` (imports: `flow` vs `playlock`, keep both), `create/wizard.go` (same),
`create/wizard_test.go`; plus the semantic one above (`Pick` signature) that merge-tree does not
show. `settings.go`, `offers.go`, `login.go`, `adder.go`, `takedown.go`, `chooser.go` auto-merge.

`origin/fix/lxc-checkout-typed` 90c02eed: conflict only in `meta-deploy.list`; `lxcadd/adder.go`
and `adder_test.go` auto-merge. Its CCY bump (3.99.12.14) does not collide with this branch.

## 6. run.bash still refuses at once

Acceptable as a follow-up, and I would keep it one. The owner's defect was ccy-teams dying with
host_vars half-applied; that is fixed at the source that wrote the host_vars. run.bash waiting
would change meta-deploy and the panel's behaviour (today both fail fast on a held lock, which
the owner can see and act on), and run.bash is on the bash-limits ratchet so the change should
be `lock.py acquire --wait` plus a two-line call, planned on its own. Note it in Plan 00137's
successors or a Task 2.18, not here.

## 7. Names, fail-fast, stderr

No install-specific names in the diff (the only `/home/...` is the placeholder user `u` in
tests). Fail-fast holds: every error path returns or exits; `lock.Release` errors are joined
and printed (`ops/playlock.go:64-67`); the `<-picked` after `closeScreen()` discards fzf's
exit on a screen the code itself closed, which is the one deliberate drop. Stderr hygiene: all
wait text goes through `Term.Printf/Say/PrintError`, which write to `t.Err`
(`term/term.go:121-141`); stdout carries nothing new. The `.golangci.yml` exclusion of gosec
G302 for `playlock_test.go` is a config exclusion for a test that must create a world-writable
directory to prove the refusal, not a `//nolint`; acceptable.

## Spec-first breach

Section G was written after the code: the developer's report says so, and the one commit
holds spec, tests and code together, so red-first cannot be read from history. What can be
checked: the tests exist for every S-G expectation and the mutation checks below show they
fail when the behaviour is removed. The breach is recorded here and in the journal line the
owner adds at merge; the spec itself is correct and matches the code.

## QA run (targeted; qa-all not run)

- `go test -race -count=1 ./internal/playlock/... ./internal/execx/... ./internal/teams/...`: ok.
- the same with `-tags ccyteamstest` over `internal/teams/ops`: ok.
- `scripts/qa-go.bash`: fmt, vet, lint and every touched package green; one red in the nested
  `qa` module, `qa/cmd/qa-patterns` `TestRealSemgrepScansTestsDirectoriesAndLargeScripts`:
  semgrep timed out on its large fixture under the full `-race` run. Rerun alone it passes in
  10s. The branch's only `src-go/qa` change is a renamed function in the allowance table
  (`qa/rules/table.go:140`, `Ops.RunElementPlay` to `Ops.runElementPlay`, which is what the
  method is now called). Host load, not this branch; the coordinator's full run will say.
- `scripts/qa-hidden-bindings.bash`: clean.
- `python3 -m unittest tests.helpers.play_lock.test_lock`: 27 tests, OK (lock.py is unchanged).
- `scripts/test-ccy-teams-command.bash` with every `PINGBUS_*` unset: 373 passed, 0 failed,
  including the two new checks that the stub play is handed the lock as `lock.py held` proves
  it, and never runs without it. `scripts/test-ccy-container-name.bash`: 14 passed.

### Mutation checks (do the tests have teeth)

Each mutation was applied, the package tested, and the mutation reverted; the tree is back at
8da095cb.

- M1 `ops/playlock.go:58-60`, a lock not taken still runs fn: 5 ops tests fail
  (`TestAWaitThatReachesItsBoundRunsNothingAndSaysWhatToDo`, `TestCtrlCDuringTheWaitRunsNothing`,
  `TestAnUnusableLockIsAFailureNotAWait`, `TestStopWaitingOnTheScreenRunsNothingAndHoldsNothing`,
  `TestRemoveDeclaresNothingAbsentWithoutTheLock`).
- M2 `ops/ops.go:273`, the play run without the descriptor: 2 ops tests fail
  (`TestThePlayRunsInsideTheHeldPlayLockHandedDownToIt`, `TestTheElementPlayRunsInsideTheSameHold`).
- M3 `picker/offers.go:261-266`, the description written before the lock is taken: 2 picker
  tests fail (`TestADescriptionIsWrittenAndAppliedInOneHoldOfThePlayLock`,
  `TestStoppingTheWaitForThePlayLockLeavesTheDescriptionUnwritten`).

So the "each failed first" claim is plausible for the behaviours that matter; it is still not
red-first in the record, which is the breach noted above.
