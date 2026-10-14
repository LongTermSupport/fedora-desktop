## QA Review: Plan 00192 (F44 state, plus the unmerged `go-port/qa-all` and `go-port/agent-bus-install` branches)

**Verdict**: FIX-BEFORE-MERGE

I found no blockers: no leaks, no fail-fast violations, no missing version bumps. The plan's PLAN.md has fallen behind the code on F44 in five places. The agent-bus branch writes new tooling in Python, which breaks the plan's own rule, and its digest is proven correct for only one of the five commands that now use it.

### Blocking
None.

### Should fix

1. **PLAN.md on F44 says work is unstarted or still on branches when it is merged and pushed.**
   - Task 3.1 (`PLAN.md:70`) is ticked `⬜`, and lines 74-75 say "cut-over (wave 4) done on the branch". But `go-port/freeze` was merged in `f0284848`, and `git merge-base --is-ancestor f0284848 origin/F44` succeeds. It should be `🔄`, waiting only on the owner's host check.
   - Task 3.3 (`:87`) is `⬜`, although qa-changed is merged (`de85afbc`, also on origin/F44). Lines 94-95 say "the push waits on a full QA run, which needs the CCY image rebuilt with golangci-lint". That is no longer true: it is pushed, and `/usr/local/bin/golangci-lint` exists.
   - Task 4.1 (`:108-109`) says freeze's two blockers are "being fixed on its branch". Task 3.1 (`:80-82`) says "The Fable review's fixes are in". The plan contradicts itself.
   - Task 3.2 (`:84-85`) still says "wave 1 … running" on F44. Only the unmerged branch carries the real state.
   - Task 1.3 (`:52`) names `cmd/qa-bash-limits` and `internal/bashlimits`. The code lives in `src-go/qa/cmd/qa-bash-limits` and `src-go/qa/bashlimits`.

2. **The qa-all branch's PLAN.md was not updated by its own fix commit.** `go-port/qa-all:PLAN.md:101` says "138 scenarios with 29 pinned deviations (B1-B6, B12, B13, B20)". The tree has 141 golden files and 32 deviation entries, and pins B21, B22, B23 and B26 as well. Commit `465667a2` added those 3 scenarios and 4 bug numbers but did not touch PLAN.md.

3. **New standalone Python tools on the agent-bus branch break the plan's own rule.** `CLAUDE.md` "Tooling Is Go" says new tools are written in Go, and only Python that Ansible loads stays Python. The branch adds two argparse command-line tools:
   - `helpers/go_drift/check.py`, run by `scripts/qa-deployed-drift.bash:271`;
   - the `python3 -m helpers.agent_bus.installer_digest` entry point, which `tasks/build-go-command.yml:69` runs for every Go build.

   `declare.py` needs a Python digest function, so a library is justified. The drift checker and the build-time CLI are new tooling and belong in the `src-go/qa` module, or in a `--print-digest` mode of the Go code.

4. **The digest is shared by all five commands but checked against `go list` for only one.** `tasks/build-go-command.yml:87` bakes the regex-based Python closure digest into ccy-teams, ccy-sessions, podfreeze, lxcfreeze and agent-bus-install. `helpers/go_drift/check.py` then judges all five by it. `src-go/internal/businstall/digest_agree_test.go` compares Python with `go list -deps` only for `./cmd/agent-bus-install`. `src-go/CLAUDE.md` (branch) still says that test proves "the Go and Python sides agree". If the regex parser misses an import in one of the other four, that command shows drift (or hides it) and nothing fails. This is the "one path asserted" pattern from AgentNotes. Fix: loop the agreement test over every row of `scripts/go-binary-installs.tsv`, and move it out of `businstall`.

5. **A command-wide helper is named and placed as if it belonged to the agent bus.** `helpers/agent_bus/installer_digest.py` (its default command is `agent-bus-install`) now computes digests for the freeze and ccy tools too, so `play-podfreeze.yml` depends on the `agent_bus` package. `helpers/go_drift/check.py:28` imports it from there. Rename and move it, for example to `helpers/go_drift/source_digest.py` or a Go equivalent.

6. **`qa-deployed-drift.bash:271` misreports and miscounts Go drift.**
   - `|| DRIFTED=$((DRIFTED + 1))` counts any non-zero exit from `go_drift.check` as one drifted script. That includes exit 2, "could not run". The result is "The repo and the host disagree… Deploy" for what is really a checker failure.
   - It also mixes units. With 3 drifted binaries and no script pairs, the failure line reads "1 of 0 deployed script(s) differ".
   - Fix: check exit 2 separately, and print the Go counts in their own clause.

7. **No project rule, and no recorded reason, for the Go defect classes the ports found (success criterion 3, `PLAN.md:116`).** `CLAUDE.md` says a Go defect class gets a project rule first. `465667a2` fixed B25 (`proc.OS.Run` dropping its capture on `ErrWaitDelay`) and `de0b6dc5` fixed the `sudo -v` readiness probe. `CLAUDE/GoProjectRules.md` has neither a rule nor a "cannot be a rule, because…" line for either. The plan has no mapping from defect classes to rules for any port.

8. **Success criterion 2 (`PLAN.md:115`) was not met as written, and the plan does not say so.** For freeze, the bash suites were deleted and replaced by a golden replay (`src-go/CLAUDE.md`, "Parity oracles"). For qa-all, `test-qa-helper-summary.bash` was deleted and its cases moved into Go tests. Both may be valid substitutes, but the criterion should record them.

9. **The path-triggered QA rule does not load for the new gate data.** `.claude/rules/qa-gates.md:2-5` matches `scripts/qa-*.bash`. The gate list now lives in `scripts/qa-gates.tsv` (qa-all branch), and the routing in `scripts/qa-routing.tsv` and `qa-trees.tsv` (F44). Editing them, or `src-go/qa/{all,gates,changed}`, loads no QA rule. Add those globs, and update the matching row of the `CLAUDE.md` table.

10. **Both branches conflict with current F44.**
    - `git merge-tree F44 go-port/qa-all`: conflict in `scripts/bash-limits.allowlist`.
    - `git merge-tree F44 go-port/agent-bus-install`: conflicts in `meta-deploy.bash`, `CLAUDE/QA.md` and `bash-limits.allowlist`.
    - The two branches conflict with each other in the same three files.

    Resolve the allowlist only with `qa-bash-limits.bash --update`, never by editing a record.

11. **No full qa-all run after the review fixes.** `261014-qa-all-fixes-sonnet.md` says "The full `qa-all` was not run". `465667a2` turns a zero count and a non-zero `failed:` into a failed gate, so a real suite could now go red. The plan correctly says "Awaits the coordinator's full QA run" (`:104`). This is a reminder that it must run before the merge.

### Nits
- The sudo fix (`src-go/internal/sudo/sudo.go:71-75`) treats a passing `sudo -n true` as ready. That only proves `true` runs without a password, not the commands that follow. A host whose NOPASSWD rules cover `true` but not podman or `lxc-*` would skip the prompt and then fail. Rare in practice.
- Task 2.2 (`PLAN.md:62-64`) and `src-go/CLAUDE.md:47` list four project rules. `GoProjectRules.md:36-40` and `src-go/qa/rules` have five; `project_tmux_targets` is missing from both lists.
- Task 1.2 (`:49-50`) leaves out the 90-statement limit; `README.md:277` does too. `CLAUDE.md` and `measure.go:51` include it.
- `ACCEPTANCE-agent-bus-install.md` step 4 asks the owner to run backup and restore by hand "on a throwaway team". No declared throwaway team exists, and making one means changing `host_vars` first. Say how.
- `**Owner**: joseph` (`PLAN.md:5`) is a username in a public repo, but `mkplan.bash` writes it into all 164 plans. The fix belongs in the scaffolder, not in this plan.
- `F44` is 7 commits ahead of `origin/F44`, including the 00192 meta-deploy commit `4841da0c`. The host is running meta-deploy from commits that are not pushed.

### Checked and clean
- **Gate population (qa-all):** I compared the sets, not the sizes, of what F44's `qa-all.bash` runs and the rows of `qa-gates.tsv`. The only differences are the deleted `qa-helper-summary` library and suite, and the script's own and `qa-changed` names. All 92 rows match, including both `helpers.gnome` modules.
- **Detail patterns are checked against real output:** at the branch tip they run against captured gate output (`testdata/wording/*.txt`), and the runner fails any gate whose pattern finds nothing. That replaces the deleted `gate_call_sites` coupling.
- **Leftover references to removed names:** none outside history files (Completed plans, INVENTORY, the capture scripts).
- **Docs moved with the behaviour:** QA.md, `src-go/CLAUDE.md`, the `qa.yml` comment, `docs/agent-bus.md` and `docs/playbooks.md` were all updated on their branches.
- **Public-repo safety:** across both branch diffs, every IP is RFC 5737 or loopback. No `/home/<name>`, private ranges or real emails. The captured wording files contain only a mktemp path.
- **IaC placement:** `play-agent-bus.yml` imports `play-golang.yml`. Only the copy runs as root; the build stays the login user's. The digest task fails the play on error with no `failed_when`.
- **CCY version bump:** not triggered. Nothing under `files/var/local/claude-yolo/` changes on F44 or either branch.
- **Allowlist:** both branches only shrink or remove entries.
- **Plan index:** the README row for 00192 exists.

### Mechanical gates
- qa-all.bash: not run, as you instructed. Also not run after the fix commit on the branch (finding 11).
- plan-qa --sweep: 15 advisories, 0 blocking, none for 00192 (exit 1 because advisories exist). I saved its output to `/workspace/untracked/scratch/qa-reviewer-planqa.txt`. That gitignored scratch file is the only thing I wrote. The `git merge-tree` checks added only unreferenced objects to `.git`.
- syntax-check: not run (no Ansible, as instructed). `play-agent-bus.yml` and `tasks/build-go-command.yml` change on the agent-bus branch and need `ansible-playbook --syntax-check` before it merges.
- qa-helper-tests: triggered by the `helpers/` and `tests/helpers/` changes on both branches, not run here. The qa-all fix report says the `verdicts` and `link_check` tests passed. I found no record of a run of `tests/helpers/go_drift` or `test_installer_digest` on the agent-bus branch.