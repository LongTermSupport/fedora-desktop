# Issue 138: status line with CLAUDE_PROJECT_DIR unset

## Defect and class

`pingbus-status-line` found the project from the payload (`projectDir`) but ran the inner
command with the process environment, so a settings command written as
`$CLAUDE_PROJECT_DIR/.claude/hooks/status-line` expanded to `/.claude/hooks/status-line`
(exit 127). Class: a command built from an environment variable that may be unset, run without
checking it.

## Changes

- `src-go/internal/pingbus/statusline/statusline.go`: the inner env gets `CLAUDE_PROJECT_DIR`
  = the resolved project when the variable is not already an absolute path; with no project
  found and the command text containing `CLAUDE_PROJECT_DIR`, the command is not run and a red
  note names the variable and the empty payload fields (`noProjectNote`).
- `helpers/pingbus/statusline.py`: the same defect, same fix (parity).
- `src-go/internal/teamchat/member/statusline/inner.go`: a second instance of the class
  (`teamchat status-line`), same fix.
- `SPEC-status-line.md`: S9 amended, S9a added, with the reason. Golden: scenario
  `inner-environment` re-recorded from the Python (only the `CLAUDE_PROJECT_DIR=` line changed),
  new scenario `inner-no-project-names-variable`.
- Tests: Go (`statusline_test.go` in both packages) and Python (`test_statusline.py`).
  `TestInnerTimeout` in teamchat now expects the project variable in the env.

## Red/green

Red (Go pingbus, before the fix): `got "\n"` for the unset-variable cases and
`(status line exit 127)` for the no-project case. Red (teamchat): `out ""` and
`(status line exit 127)`. Green after the fix; the golden run (Go in-process) is green against
the re-recorded files. The Python tests were written after the Python fix, so their red was
not shown separately; the golden record from the Python shows the old and new values.

## Defence Before Fix: detector

No analyzer or semgrep rule written. The class is a runtime property of a command string taken
from a user's settings file, run through `/bin/sh -c`; the defect is the missing link between
the resolved value and the child's environment, which no syntax pattern separates from correct
code. The sweep was by grep: `CLAUDE_PROJECT_DIR` appears only in the three places fixed. The
owner may want to decide whether this counts as "a class no detector can express".

## QA

`./scripts/qa-changed.bash` passed (12 changed files: ruff, helper unit tests, docs, go-qa on both modules, plan-tests, three bash suites). `qa-all.bash` not run (owner's push gate).

## Other faults met in the tooling

- `gh issue view 138 --comments` printed nothing in this container (issue text not read; the
  defect was taken from the brief).
- The bash safe-mode hook blocks a plain `cmd && cmd` with `;`-free sequences that redirect
  into a missing `untracked/scratch/`; the directory did not exist in the fresh worktree.
- The hooks pipe blocker blocks `| tail` on any python3 command, and the sed blocker blocks
  `sed -n` reads.
- `gofumpt` is not on PATH in the container (golangci-lint's formatter check ran instead).

## Left

Merge and the host rebuild of `pingbus-status-line` (via `meta-deploy.bash`) are not done.
