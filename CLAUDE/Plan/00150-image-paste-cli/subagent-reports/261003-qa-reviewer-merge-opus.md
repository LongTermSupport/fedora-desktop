## QA Review: commit 36e61be9 (Plan 00150, three plays merged into play-cli-tools.yml)

**Verdict**: FIX-BEFORE-MERGE

The merge itself is correct. All 20 tasks from the three deleted plays are in `play-cli-tools.yml` with their behaviour unchanged. The verdict comes from one live index row that still names a deleted play, and one misleading comment about how the tools now depend on each other.

### Should fix

1. **The plan index still describes `play-disk-reclaim.yml` as current.** `/workspace/CLAUDE/Plan/README.md:159`
   The 00062 row reads "`play-disk-reclaim.yml` plus `reclaim` … HOST deploy pending". The commit updated `00062/PLAN.md:45-46` but not this row, so the index names a play that no longer exists. Fix: point the row at `play-cli-tools.yml` (tag `disk-reclaim`).

2. **The comment about which check stops the run names the wrong one.** `/workspace/playbooks/imports/optional/common/play-cli-tools.yml:17-18`
   - The comment gives the ncompress preflight as its example. But compression is the last block (lines 162-237), so its preflight stops nothing except the summary.
   - The check that really can stop the other tools is imgpaste's. It comes first, and its WebP assert (lines 64-71) runs before open, reclaim and compress.
   - The play is in `server-recommended.bundle`, and that bundle's own rule is that a failing optional play aborts the whole headless run. So every server-recommended host now installs ImageMagick, and if the WebP assert fails, open, reclaim and compress are never installed either.
   - Fix: name the WebP assert in the comment. Better, also move the imgpaste block last, so the older tools never depend on it.

### Nits

- `/workspace/CLAUDE/Plan/00150-image-paste-cli/PLAN.md:82` says "(this plan's next commit)", but this line is in that commit. Say so, or give the hash.
- The header of `/workspace/CLAUDE/Plan/00150-image-paste-cli/acceptance.bash` (lines 10 and 39) still lists only the non-image and over-budget refusals. The pixel and byte cases are missing.
- `acceptance.bash:133` checks for the reason "pixels", which is loose. "limit is 80000000 pixels" is the actual text at `imgpaste:46`.
- `/workspace/CLAUDE/Plan/032-compression-helpers/PLAN.md:254` still has a "Next: … deploy with `play-compression-helpers.yml`" line in a plan that is still In Progress.
- `/workspace/tasks/deploy-freeze-lib.yml:26` quotes reasoning that names `play-open-command.yml`. It's a historical quote, but a reader can't find that file now.

### Prior report (261003-qa-reviewer-tools-play-opus.md)

| Finding | Status |
|---|---|
| 1. Refusals didn't check the reason | Resolved: `refused()` at `acceptance.bash:112-124` checks stderr; called at lines 127 and 130 |
| 2. No oversize tests | Resolved: pixel case at line 133, byte case at line 139 (sparse 51M PNG; `file` still sees an image, size is checked before pixels) |
| 3. `inherit_errexit` missing | Resolved: `imgpaste:12`. I ran `set -euo pipefail; shopt -s inherit_errexit; f(){ false; echo after; }; x=$(f)` and it exited 1 |
| 4. README row out of date | Resolved: `README.md:39` |
| Nit: `plan_finish` | Not adopted. The run-log path is printed by hand at line 145 instead |
| Nit: `r[w]` | Resolved: play line 67 |
| Nit: Delivery section | Resolved: PLAN.md:80-82 |
| Nit: bin-dir tag | Resolved: `tags: always` at line 45 |

### Checked and clean

- **Tasks carried over:** I compared every task against `git show 36e61be9^:` for each play. Packages, modules, `when: provisioning_profile != 'server'` on baobab, the ouch vars, `creates:`, the probe annotated `FAIL-FAST-OK` and the copies all match. The only change is tags added to the compression tasks. Two duplicate bin-dir tasks were merged into one.
- **Retirement rule:** the three old plays are deleted and mapped in `retired-plays.json` in the same commit, and the successor exists. The `tests/helpers/play_ledger` and `tests/helpers/self_update` tests pass (592 passed).
- **Leftover references:** none of the old play names remain in `run.bash`, `scripts/`, `tests/`, `.claude/`, `docs/`, `files/` or `vars/`. A stale `RUN_BASH_OPTIONAL_PLAYBOOKS` value fails loudly at `run.bash:897`, and PLAN.md records this as a HOST item.
- **Server bundle, version pin, tool messages:** the bundle, `vars/version-pins.yml` and the messages in `open`, `reclaim`, `compress` and `uncompress` are all updated. `docs/playbooks.md` gained the new section and lost the three old ones.
- **Headless safety:** no `pause`, no prompts, and the only GUI package (baobab) is still gated.
- **Executable bit and shebang:** both present. `shellcheck -S warning` on every changed script exits 0.

### Mechanical gates

- **qa-all.bash:** not run, as instructed; the coordinator runs it.
- **plan-qa --sweep:** 0 blocking, 11 advisory findings, none for Plan 00150. One says 032 has had no journal entry for 10 days.
- **syntax-check:** `play-cli-tools.yml` passed.
- **Not triggered:** `qa-helper-tests.bash` (only a JSON data file changed under `helpers/`; I ran the ledger tests directly), the extension check and ESLint.