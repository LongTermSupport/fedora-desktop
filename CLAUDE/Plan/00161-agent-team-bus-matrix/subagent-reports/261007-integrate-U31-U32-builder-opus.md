# Integrate U31+U32 onto `seats-integration`

Integrator for Plan 00161 units U31 (`ccy --teams`, headless Quick Launch) and U32
(`agent-bus seat check|take|list|remove`).

## What was merged

`origin/seats-integration` (with U30 and U33 already in it), then, each with
`git merge --no-ff`:

1. `origin/seats/U31`: merged clean.
2. `origin/seats/U32`: three conflicts, all plan text:
   - **DESIGN.md decisions table.** Both units added a row numbered D56, and each branch's
     table formatter had re-padded the whole table to its own widest row, so the conflict
     covered every row. Compared row by row with whitespace normalised, the only real
     differences were the two new rows. U31 keeps D56-D57; U32's row is now **D58**,
     placed after D57, and the table realigned.
   - **JOURNAL 26-10-07.** Both entries kept, in time order: U32 (17:26), then U31 (17:35).
     The U32 entry's text still says "D56"; journal entries are append-only, so the
     renumbering is recorded in this integration's own journal entry instead.
   - **PLAN.md unit line.** Combined: U31 built (host run pending, the image rebuild in
     `deploy.bash`), U32 built (now citing D58), and a note that U31 and U32 are merged
     together on `seats-integration`.
   - U32's report now cites D58, and U31's report no longer says the numbers may collide.

The local branch is named `integrate-U31-U32` because a branch called `seats-integration`
is already checked out in another worktree. It was pushed to `origin/seats-integration`
as a fast-forward.

## Interface check across the two units

The launcher (`lib/agent-bus-seats.bash`) calls `agent-bus seat check <list>` and expects
one canonical line on stdout, then calls `agent-bus seat take <canonical> [--no-prompt]`
with its stdout sent to stderr. U32's parser and wrapper match: `check` prints the one
line, `take` accepts `--no-prompt` after the list, and seat commands run as the caller
(and are refused as root, 77). The exit codes the launcher passes on (64, 75, 78) are the
codes U32's `cli.main` returns. The `PINGBUS_TEAMS` text that U31 left to U32 (in the
podman next step of `admin.py`, `README.podman`, and `docs/agent-bus.md`) now says
`ccy --teams`. The text that still mentions `PINGBUS_TEAMS` covers the host, LXC, VM and
docker forms, where it is correct.

## Verification (on the merged tree)

- All 27 `tests.helpers.agent_bus` and `tests.helpers.pingbus` modules, by module name:
  1181 tests, OK (includes U32's `test_checkout`, `test_wrapper`, `test_member_docs`).
- The plan folder's five Python test modules: 231 tests, OK.
- `scripts/test-ccy-teams.bash` 73 passed; `test-ccy-agent-bus.bash` 41 passed;
  `test-ccy-env-local-dist.bash` 35 passed; `test-ccy-restore-askpass.bash` (run with
  `bash`, as `qa-all.bash` runs it; the file is mode 644 on F44 too) 87 passed;
  `test-agent-bus-install.bash` 261 passed. None failed.
- `ruff check` (0.16.8) on `helpers/agent_bus`, `helpers/pingbus`, their tests and the
  plan folder: clean.
- `shellcheck -x --severity=warning` on every bash file either unit touched (launcher,
  entrypoint, `lib/agent-bus-seats.bash`, `lib/common.bash`, `lib/session-registry.bash`,
  `qa-all.bash`, the four ccy test scripts, the `agent-bus` wrapper, `deploy.bash`): clean.
  At info level, the only findings are SC1091 (sourced files not followed from the repo
  root) and an SC2086 on `$NETWORK_FLAG`, which is already on the base branch.

## Notes for the coordinator

- No integration fix to code was needed, so there is no CCY bump beyond U31's
  (ccy 3.86.0, container 2.46).
- Host run: still only U31's image rebuild, through `deploy.bash`'s ccy play leg, with
  Plan 00161 already in `meta-deploy.bash`. U20's rework exercises both units.
