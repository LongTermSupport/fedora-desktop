# Review 2: acceptance-s1 fix, commit 9ca963c25 (answers to review 1's findings)

Scope: `git show 9ca963c25` only. `bash -n` and `shellcheck -x` pass on `_acceptance-s1.inc.bash`
and `acceptance-s1.bash`.

## Verdict: MERGE

All four findings are fixed correctly, and the fixes add no defect. One wording nit is below; it
does not block.

## Finding 1: the wording of a question before the table

The new message in `not_table` (`_acceptance-s1.inc.bash:276-281`) matches the source:

- `pickerSession` (picker.go:128-146) asks `offerCreate` when there are no teams, then
  `offerPathLimits`, `offerDescriptions` and `offerUpdate`. `once` asks `offerCreate` again when
  there are no rows.
- `offer` (offers.go:85-96) gives three answers: now, "Not now (ask again next time)" (Esc is
  taken as not now), and never. Only never writes `neverRecord`. So "until the limit is lifted or
  you choose never" and "until one is written or you choose never" are right.
- `offerUpdate` (offers.go:317-345) asks for ApplyOutdated or ApplyUnbuilt, as "Update them now
  (play-agent-bus.yml)?". A yes runs `p.Ops.Apply(ctx, []string{ops.FlagAll})` under the label
  `ccy-teams apply --all`. A no asks again on the next open.
- `ccy-teams apply --all` is a real command form:
  - `ops.FlagAll = "--all"` (ops.go:33);
  - `Apply` accepts it (ops.go:147);
  - `apply` is a subcommand in app.go:165 and app.go:196;
  - `NeedCheckout` checks the baked RootDir, not the cwd, so it works from any folder.
- "Create a team … until one is made" is right.

The header (`acceptance-s1.bash:36-39`) and the meta-deploy.list comment say the same thing in
fewer words.

**Nit, no change needed:** the meta-deploy.list comment says "while teams are out of date" and
leaves out "or declared and not built". The other two files include it. This only shortens a
summary; nothing in it is wrong.

## Finding 2: an applied-state directory this user cannot read

- `[[ -e "${applied}" && (! -r "${applied}" || ! -x "${applied}") ]]` is valid `[[ ]]` grouping.
  `bash -n` and shellcheck accept it, and `&&` binds as intended.
- It is the right test. `addStamped` (collect.go:255-260) calls `os.ReadDir`, which needs both r
  and x on the directory. It treats a missing directory as no teams and any other error as fatal
  ("could not be read", errReported).
- So the script now returns 1 exactly where ccy-teams fails. A missing directory still falls
  through to the no-team path (return 2), which matches the Go code.
- "(ccy-teams --list fails on it too)" is accurate.
- The test reads permissions as the running user. The script runs as the desktop user, not root,
  so it judges the right user.

## Finding 3: a fixture --list that did not start

- Both redirects are inside the subshell, after `cd "${cwd}" &&`. If `cd` fails, no output file is
  created, and the new check (`:436-439`) reports "did not start" with return 1.
- If `cd` succeeds, bash opens both files before it runs the binary. A binary that crashes or
  exits non-zero still leaves both files, so the check cannot misfire on a real run.
- Even when `${INSTALLED}` is missing (127), both files exist. The run then goes on to the greps,
  which fail with bad=1. That is still a failure, so it is not hidden.
- `${fx}` is under the per-run PLAN_RUN_DIR, so no stale files from an earlier run can satisfy
  the check.
- The check comes after `rmdir`. Using `${cwd}` in the message after that is fine, because only
  the path is printed.

## Finding 4: the NOT JUDGED causes in the header

EXIT CODES now lists "a TMPDIR that git places in a work tree (or GIT_DIR set)". That matches
`outside_checkout`'s exit-2 cause.

## Nothing else

The commit adds review 1's report under subagent-reports/. The comments describe current state,
and no install-specific names appear.
