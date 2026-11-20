## QA Review: round 5 of B.13 (ef7c28ebe, f7f1f8cb3) and the #294 freeze fix (63a1ef55f)

**Verdict**: PASS WITH NITS. I found no merge blocker. One finding is worth fixing or logging before the batch lands, because the commit message does not admit it: the guard now lets a few `rpm` lines through that the previous commit on this branch blocked. It is not a regression against F44.

### Should fix (not a merge blocker)

1. **The `(?!-)` change removes some `rpm` denials, and the commit says it removes only ambiguity.** `.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py:130`
   - I ran the guard as it was before 63a1ef55f and as it is at HEAD, both under the daemon's interpreter (probe: `/workspace/untracked/scratch/qarev-294/r6/probe.py`). These lines were denied before and are allowed now:
     - `rpm --dbpath -d --install x.rpm`
     - `rpm --root -r -i x.rpm`
     - `rpm --define -x -i x.rpm`
     - `rpm -D -x -U x.rpm`
     - `systemctl -H --user enable x`
   - The cause is that `_options()` has a catch-all for any dash word (`-\S*\s+`). A value that starts with `-` simply falls through to it, so `dnf`, `yum`, `pip` and `gsettings` lose nothing; all were checked and are still denied.
   - The hand-written `rpm` pattern has no such catch-all. Its fallbacks are only `--(?!install…)\S+` and `-v+`, so a dash-leading value after `-D`, `--root` and the rest now ends the match.
   - Not a regression against F44: the original patterns are still the first entries of `FORBIDDEN_PATTERNS`, and the oracle test asserts this (`test_ansible_enforcement.py:543`). None of these lines was ever denied by the original.
   - I could not confirm that `rpm` itself accepts `--dbpath -d` (there is no `rpm` in this container), so how much this matters in practice is unproven.
   - The `systemctl -H --user` case is harmless: `getopt` takes `--user` as the host name, so nothing changes locally.
   - **Fix:** in the `rpm` pattern, drop `(?!-)` and instead keep the valued option names out of the `--(?!…)` alternative. That removes the double reading without losing the dash value. Or log it under #294.

### Answers to the questions

- **Can anything still be typed into a session that is not the team's?** Not by any path I found.
  - Jobs come only from `closeHolders` with `KeeperRunning` (`v2host.go:73-78`).
  - `recheck` runs after `confirm` and before `stopAll`, including with `--yes` (`shutdown.go:224-231`). It drops any job whose lock is not `KeeperRunning`, or whose probe errors.
  - What remains is narrow: `sessclose` still targets by name only. `/compact` can be typed up to `Waits.Prompt` (30 s, `sessclose.go:148`) after the recheck, and `/exit` follows the compaction. A swap of a same-name session between two 1 s polls is the only remaining path.
- **Can a seat that failed be reported as exit 0?** No. Only `KeeperEnded` with no live session of that name goes to `idle` (`v2host.go:182`). No lock yet, a refused lock, a free lock with a live namesake, an incomplete holding, an off-tmux holder, a probe error, a holdings error, a bad pool entry and a failed recheck all go to `unreached`, which exits 1. Each of these has a test.
- **Is the sweep wording true?** Yes. `sweep.one()` (`sweep.go:70-90`) releases the seat when the session is not restorable, releases it as a leave when the desired membership no longer names it, and otherwise keeps it. A restore re-takes the seat through `sessionHolds` (`take.go:227-236`). The wording "stays held until the seat sweep releases it, or a restore … resumes it" says nothing about when, and is true. The same wording is in `v2.go:33-36`, the help text, SPEC TC12 and `docs/agent-bus.md:207-212`.
- **Does the new test exercise the real re-check?** Yes. `TestAHolderThatEndsWhileConfirmingIsNotTypedInto` (`v2host_test.go:372-387`) uses the production `Keeper` (`seat.KeeperOf`, the default at `shutdown.go:105`) and frees a real flock inside the prompt runner. It asserts the "changed while confirming" text, which only `recheck` produces, so the test would fail if `recheck` were removed.
- **Do the version bumps agree, and is the changelog truthful?** The bumps agree: CCY 3.122.43, and `REQUIRED_CONTAINER_VERSION` 2.84.43 matches the Dockerfile LABEL 2.84.43. f7f1f8cb3 changes only `teams/shutdown`, so it needs no further bump. The changelog is accurate except for the "just before /compact" nit below.
- **The account in "Fix round, part 5"** matches the code.

### Nits

- The changelog (`docs/ccy-changelog.md`), the help text (`shutdown.go:139`) and `docs/agent-bus.md:212` say the lock is "checked again just before `/compact` is typed". It is checked before the close begins, which can be up to 30 s before `/compact` is typed. With `--no-compact`, no `/compact` is typed at all.
- ef7c28ebe is a commit whose tests fail on purpose (RED). It leaves a commit on the branch where the tests do not pass, which hurts `git bisect`.

### Checked and clean

- The redesigned guard patterns are not slow. Every pattern, run over 60-repeat floods of 14 option shapes for 11 tools and four endings, took at most 0.77 ms.
- The semgrep sinks: the keyword form and `pkg.mod.func` each have a positive test line in the fixture (`hook-handlers.py:283-299`), and the gate finds nothing in the live tree.
- `seat.KeeperOf` and `keeperLive` share one probe. `sessionHolds` behaves exactly as before.
- Plan 00257 has no plan-qa findings, and no install-specific names appear in the diff.

### Mechanical gates

- `go test -count=1 -race ./internal/teamchat/seat/... ./internal/teams/...`: 33 packages ok, exit 0.
- `scripts/qa-hook-handlers.bash`: `HOOK-HANDLERS-OK 1983 test(s)`.
- `semgrep --test --config .semgrep/hook-handlers.yml .semgrep/hook-handlers.py`: 4/4 passed. `scripts/qa-hook-handler-rules.bash`: 423 passed, 0 failed.
- `plan-qa --sweep`: 2 blocking findings, both in other plans (00244, 00238), none in 00257.
- Not run: `qa-all.bash`, because a full QA run inside a sub-agent is denied (R-SUBAGENT-FULL-QA). `--syntax-check`, the helper tests and ESLint are not needed: no playbook, helper or extension changed.
- Scratch files went to `/workspace/untracked/scratch/qarev-294/r6/`, because the hook refused `/tmp`. Nothing was written into the worktree.