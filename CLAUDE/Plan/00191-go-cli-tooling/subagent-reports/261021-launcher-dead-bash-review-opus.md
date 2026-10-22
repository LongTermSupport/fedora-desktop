VERDICT: MERGE

I found no blocking defects. Below: the six checks, then three small optional cleanups.

**Checks**

1. **Every deleted name is dead at 7a4e0d8f.** I ran `git grep` across the whole tree (changelog excluded) for `ccy_tmux_take_over`, `ccy_tmux_client_words`, `ccy_tmux_idle_words` and `CCY_TMUX_TAKE_OVER_WAIT_TENTHS`.
   - Nothing in `files/`, `helpers/`, `playbooks/`, `tests/` or `docs/` calls them, and no plan script does either. The only hits are history in plan docs (00135 patch, 00158 PLAN/JOURNAL/deploy.bash comment, 00191 ASSESS/SPEC/subagent-reports).
   - `src-go` never shells out to these functions.
   - The env var is still live, but in Go only: `src-go/internal/sessions/app.go:103` reads it itself and `app_test.go:82,122` test it.
   - So `scripts/test-ccy-sessions-take-over.bash:242` (`export CCY_TMUX_TAKE_OVER_WAIT_TENTHS=3`) is correctly kept: the Go picker under test reads it. The changelog's "which reads that variable itself" is accurate.

2. **The suite still tests something real.**
   - The cases removed were exactly the take-over-only ones (idle words, client words, the nine take-over cases, and the kicked-attach take-over case).
   - What remains: `go test ./internal/tmuxccy/` (its `takeover_test.go` covers detach refused, detach-gone, timeout, ended-while-waiting and list failures), the bash `ccy_tmux_attach` race case, `ccy_tmux_current_session`, the single-attach hook, and all eight Ctrl-T picker cases through the real binary.
   - The function gate at line 259 now checks three live functions.

3. **Version bump, changelog and allowlist are correct.**
   - `CCY_VERSION="3.99.13.12"` is set at `claude-yolo:17`. The new changelog section `## 3.99.13.12 — container 2.78.11` is above 3.99.13.11 and follows the same "Follows …" convention.
   - No acceptance script pins 3.99.13.11, so nothing breaks on the bump.
   - The allowlist was only lowered (377/22/445 to 315/19/372). I recounted at both commits: code lines 377 to 315 and functions 22 to 19 match. I did not recount statements.

4. **The meta-deploy entry is valid but redundant (non-blocking).** It is well described in house style, says no rebuild, and gives a clear check afterwards. But two earlier queued entries already run the whole `play-claude-yolo.yml` from this same checkout: `00213-…/deploy.bash` (list lines 54-67) and `00211-…/deploy-stage-2.bash` (lines 68-86). Once merged, either one would already install 3.99.13.12. AgentNotes.md:96-97 says "A run that would only repeat one already passed is not pending." Defending it: entries are pruned one by one, so a separate line keeps Task 4.3's deploy tracked if 00213 or 00211 pass and are pruned first. The play is idempotent, so the cost is one extra run.

5. **The script-made `.md` edits did no collateral damage.** The PLAN.md, changelog and QA.md hunks change only what they should, and the QA.md table row keeps its column padding. Two small accuracy notes, neither blocking:
   - The QA.md row (`CLAUDE/QA.md:125`) now names "the bash library's `ccy_tmux_attach`". That holds, but the suite also tests `ccy_tmux_current_session` and the hook, which the old row did not mention either.
   - The row still claims coverage of "the detach was refused". In this suite that now comes only from the `go test tmuxccy` step (`TestTakeOverDetachFailsAndTheClientStays`), not from a bash case. Still true.

6. **No install-specific names** appear in any added line.

**Optional cleanups**

1. `scripts/test-ccy-sessions-take-over.bash:89-90,125-129,137-145`: the fake tmux knobs `TEST_TMUX_DETACH_FAIL`, `TEST_TMUX_DETACH_GONE` and the `linger` countdown above 0 have no remaining case. Every removed user was a `ccy_tmux_take_over` case (old lines 299, 323, 336), and the picker cases use only `STICKY`, `END_ON_DETACH` and `RACE_ATTACH`. Test files are exempt from the bash limits, so this is only a YAGNI point. Either delete the knobs or add picker cases for detach-refused and detach-gone.
2. Two descriptions go stale once this merges: the `00213-…/deploy.bash` comment at `CLAUDE/Plan/meta-deploy.list:57-58` and `CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/deploy.bash:10` both say the ccy play gives "CCY 3.99.13.11". After merge it gives 3.99.13.12. This is cosmetic: no script checks the version.
3. Drop the separate `playbooks/imports/play-claude-yolo.yml` entry and add a line to the 00213 entry's comment saying it also delivers 3.99.13.12. Or keep it if per-plan tracking is preferred.