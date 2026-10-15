# Integration branch catch-up with F44 (Opus)

`integration/261014` was 125 commits ahead of F44 and 76 behind. F44 was merged into it, not
rebased. F44 kept moving during the work, so there are three merges:

- `daae0612` holds the first round of conflict resolutions.
- `62cf4c0b` takes `9c0c48e0`, one doc commit, with no conflicts.
- `c95036bb` takes Plan 00201 M6 and Plan 00191's `qa-go.bash` change, with a second round of
  conflicts (below).

After `c95036bb` the branch holds all of F44 as of `397ca234`.

The work was done in a detached worktree at `.claude/worktrees/integration-catchup`, because
`integration/261014` is checked out in another agent worktree. That worktree was clean and level
with origin. The result was pushed with `git push origin HEAD:integration/261014`, a
fast-forward. That other worktree's local branch ref is now behind origin. It needs only a
`git pull`; nothing in it is lost.

## Conflicts (11 files) and how each was resolved

None of the conflicts were in the QA ports. The integration branch also carries Plan 00194
(goodbye, relay) and Plan 00177 Phase 8 (`ccy-teams lxc-add`). Those collided with F44's Plan
00201 M3 to M5.

| File                                                                            | Resolution                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `helpers/pingbus/hooks.py`                                                      | Kept the branch's relay-aware `session_start`/`_wake_lines` split. F44's session team record and its `this session is on teams A, B` line moved into a new `_session_teams_lines`, so they are written in relay mode too. `about_lines` was added after the seat lines. `session-end` first forgets the status-line record (F44), then says goodbye (branch). The import set and templates are the union of both sides. |
| `helpers/pingbus/cli.py`                                                        | `send`/`say` use F44's `_sending_member` (team inference) and keep the branch's `relaying` sent log. `_deliver` keeps the branch's spool-locked loop and prints F44's `REPLY` line after each accepted item when not relaying.                                                                                                                                                                                          |
| `helpers/agent_bus/declare.py`                                                  | Imports `bridge` (F44) along with `registry` and the LXC modules (branch).                                                                                                                                                                                                                                                                                                                                              |
| `src-go/internal/teams/app.go`                                                  | Wires both `Lxc` (lxc-add) and `Chooser` (choose-teams), and accepts both subcommands. The two `NeedCheckout` checks became one `inCheckout` helper, which keeps `dispatch` within cyclop 15.                                                                                                                                                                                                                           |
| `tests/helpers/pingbus/test_hooks.py`, `test_cli_status.py`                     | The union: the `reason` and `description` field patterns; the `relay=` field and the `ABOUT` line.                                                                                                                                                                                                                                                                                                                      |
| `docs/agent-bus-protocol.md`                                                    | The §13 table takes the branch's rows (relay, goodbye, `post-tool`), with F44's `ABOUT` line added to `status`.                                                                                                                                                                                                                                                                                                         |
| `files/var/local/claude-yolo/{claude-yolo,Dockerfile}`, `docs/ccy-changelog.md` | In this round CCY became **3.98.1** with container **2.59**; the second round superseded this. Both lines changed the image after 2.55: F44 reached 2.58, the branch 2.56. The commit hook required a bump above the branch's 3.98.0. A new 3.98.1 changelog entry explains the join; the entries for 3.98.0 (2.56) and 3.97.x (2.58) are unchanged.                                                                    |
| `CLAUDE/Plan/meta-deploy.list`                                                  | The `play-agent-bus.yml` entry (Plans 00177/00194) is followed by one `play-claude-yolo.yml` entry, which describes CCY 3.98.1 / 2.59 with both lines' try-it steps. The note "assumes the 00161 acceptance ran from F44's list" was dropped, because F44's list no longer holds 00161.                                                                                                                                 |

Second round, `c95036bb` (7 files):

- `pingbus/hooks/hooks.json`: kept the branch's file. It already registers `SessionEnd`, with
  the goodbye's `timeout: 6`, and the relay `PostToolUse` echo. F44's M6 registration of
  `SessionEnd` would have duplicated it.
- `hooks.py` docstring, `test_plugin_contract.py`, and the protocol §13 `session-end` cell:
  session-end removes the teams record and posts the goodbye.
- Versions: F44 independently took container **2.59** for its `SessionEnd` registration. The
  joined image therefore needed a new number, and is **CCY 3.98.2, container 2.60**. The
  changelog's join entry is rewritten as 3.98.2, which drops the branch-only 3.98.1 entry. F44's
  3.97.10 and 3.97.11 entries are kept.
- `meta-deploy.list`: one `play-claude-yolo.yml` entry now covers M4 to M6 and Plans 00177 and
  00194\. It comes after the `play-agent-bus.yml` entry.

Two branch tests were updated for F44 behaviour:

- `src-go/internal/teams/picker/lxc_test.go`: team rows are now keyed by
  `board.NoDescription`, not `"-"`.
- `tests/helpers/pingbus/test_relay_cli.py`: a relay-off `recv` now prints its `REPLY` line.

## Carried QA changes

`git log` over F44 since the merge base shows one change to a `scripts/qa-*.bash`. It is
`d6b21f1c` (Plan 00191), in which `qa-go.bash` builds each module without its test files. That
script is the one gate the branch did not port, so the change arrives as is and there is no Go
port to carry it into. No ported script changed, so nothing was carried into a Go port's code,
golden or spec. F44 did change some QA data and
suites, and each change auto-merged and takes effect through the Go tools as data:

- `scripts/qa-routing.tsv`: `also CLAUDE/Watchlist.tsv go covered`. The Go `qa-changed`
  reads this table, and `scripts/test-qa-changed.bash` asserts it. That suite passed (114).
- `scripts/go-binary-suites.tsv`: the `watchlist qa-go` row. Its header was reworded to say
  every `src-go/cmd` command takes a row. The Go `qa/changed` real-tree test enforces this
  and passed.
- `scripts/bash-limits.allowlist`: the `agent-bus-seats.bash` row is gone and the
  `claude-yolo` counts changed. `qa-bash-limits.bash` returned BASH-LIMITS-OK.
- `scripts/test-meta-deploy.bash`: the empty-queue case passed (15).
- F44 added no new gate or `scripts/test-*.bash` suite, so `scripts/qa-gates.tsv` needed no
  new row. The new Python test modules (`test_cli_infer.py`, `test_cli_reply.py`) live under
  `tests/helpers`, and the helper-tests gate finds them by its own scan.

## Tests run

- `src-go`: `go build`, `go vet`, `go test -race ./...` and `golangci-lint` all pass. Before
  the picker test fix, `go test` failed in the lxc picker test.

- `src-go/qa` (the nested module): `go vet`, `go test ./...` and `golangci-lint` all pass.

- Python, every module under `tests/helpers/{pingbus,agent_bus,agent_bus_lxc}`: 1863 tests.
  Two failures came up and both are fixed: the relay `REPLY` test, and the docs-invocation
  test, which `9c0c48e0` fixed. The failing modules and `test_member_docs` were re-run and pass.

- `./scripts/qa-changed.bash --base F44` (458 files): every check and suite passes except two
  that **could not run** in this worktree:

  - `js`: `extensions/node_modules` was missing. I seeded the symlink the way the
    `worktree_create` seed does, and `qa-js.bash` then passed (30 files).
  - `ansible-syntax-gate`: the vault password file is missing from the worktree. The secret
    guard denies creating that symlink from an agent, so this gate is unrun here. No file under
    `playbooks/`, `tasks/` or `vars/` differs from F44.

  Suites that passed include `test-ccy-teams-command` (344), `test-ccy-agent-bus` (59),
  `test-agent-bus-install` (283), `test-qa-changed` (114) and `test-qa-docs-exit-codes` (6).

- After the second merge (`c95036bb`), everything was run again:

  - `src-go`: `go build`, `go vet`, `go test -race ./...` and `golangci-lint` pass.
  - Python: the same 1863 tests pass.
  - `qa-changed --base F44` (459 files): 68 checks and suites pass, including
    `test-cc-teams` (57), `test-ccy-teams-command` (344) and `test-agent-bus-install` (283).
    `ansible-syntax-gate` again could not run, for the vault-file reason above.

- qa-all was not run; the coordinator runs it.

## What still blocks the merge into F44

1. **A full `qa-all.bash` over the merged tip.** The plan's Task 3.3 says: "The full `qa-all`
   run comes before the merge." It has not been run on this tip, and `ansible-syntax-gate` was
   not run here. **Not yet met.**
2. **The owner's acceptance.** Task 3.3 says qa-all "lands in F44 after the owner's
   acceptance". **Not yet met.**
3. **A qa-reviewer over the integration branch.** Task 4.1 requires "the ports get their own
   review before merge", and that is met: every port has a Fable review, and the review fixes
   are recorded as checked in the merged tree. The qa-reviewer integration review
   (`261014-qa-reviewer-integration.md`) predates the later ports (docs, cmdmain, pins,
   vmtest, helper-tests, toolchain, stt, psl) and this merge. Task 4.2 says it "runs again over
   the finished plan". **It should be re-run over this tip.**
4. **`qa-go.bash`** stays bash until the owner makes go-qa-ci public. It is on the allowlist,
   so it does not block the merge, but Task 3.3 stays open.
5. **One design point to confirm.** In relay mode, `recv`/`watch` spool items without the M3
   `REPLY` line, because the relay line format is unchanged. Whether relayed items should
   carry it is a Plan 00194/00201 decision.

The host run is queued in `CLAUDE/Plan/meta-deploy.list` as `play-agent-bus.yml` followed by
`play-claude-yolo.yml`. It is due once this branch reaches F44.
