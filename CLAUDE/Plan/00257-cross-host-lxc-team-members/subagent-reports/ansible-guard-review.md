## QA Review: issue #294 hook-handler commits (d3addfdf3, 9e854728a, c7f685789, 6b2976680)

**Verdict**: BLOCK

The change does fix the text false positives, and nothing broke for `hidden_git_mutation`. But `ansible_enforcement` now misses a common real-run shape it used to deny, and one new acceptance test fails under the daemon's live harness.

### Blocking

1. **A written script run through a wrapper is no longer denied.** — `.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py:103-108`
   I built the handler at `d3addfdf3^` and at `6b2976680` and compared `matches()` on about 200 commands. These went from deny to allow (the system command is `dnf install` or `systemctl restart`):
   - `cat > install.sh <<'EOF' ... sudo systemctl restart sshd ... EOF` then `chmod +x install.sh` then `sudo ./install.sh`
   - `cat > s.sh <<'EOF' ... EOF` then `sudo bash s.sh`
   - the same written script run as `sudo sh s.sh`, `sudo -E bash s.sh`, `sudo source s.sh`, `timeout 9 bash s.sh`, `nohup bash s.sh &`, `nice bash s.sh`, `(bash s.sh)`, `{ bash s.sh; }`, `bash $PWD/s.sh`, or `sudo bash /var/tmp/s.sh`
   - `... | sudo tee s.sh; sudo bash s.sh`
   - output piped back into a shell: `git log -1 --format=%s | bash`, or `gh issue view ... -q .body | bash` after posting the body

   **Cause.** The daemon's `_runs_written_script` (`command_position.py:321`) only recognises a plain `bash s.sh` / `./s.sh` / `source s.sh` with the path written exactly as before. Once a wrapper is in front, the heredoc body is blanked as data, so nothing is left to match. Writing an install script and then running `sudo ./install.sh` is exactly what a careless agent does, so this weakens the IaC hard rule.

   **The tests miss it.** `test_ansible_enforcement.py:87` pins only the bare `echo ... > s.sh && bash s.sh`. The commit message's "every real-run form already green" covers only that list.

   **Evidence the daemon knows about this gap.** Its own `sudo_pip.py:266-269` adds `_elevated_body_runs_pip(command)` on top of `command_position_code_segments`.

   **Fix.**
   - If a command writes any file (a redirect, `tee`, or `cp`/`mv`) and a later segment runs a shell, `source`, `.` or an executable path behind any wrapper, judge the raw text as the old handler did.
   - Add the shapes above as RED tests.
   - Report the `_runs_written_script` wrapper gap upstream through `hooks-daemon issue-report`.

2. **The system_paths "Allow Edit of a project path" acceptance probe fails in the live harness.** — `.claude/hooks/handlers/pre_tool_use/system_paths.py:335-349`
   - The probe's payload is a relative `file_path` (`untracked/scratch/zz-...txt`).
   - `playbook_harness.py:521-526` sends it through the whole daemon, not just this handler.
   - I ran `AbsolutePathHandler` (priority 12, `hooks-daemon.yaml:115`) on that exact payload: `matches: True`, `deny BLOCKED [R-ABSOLUTE-PATH-REQUIRED]`. So the probe expects allow and will observe deny.
   - `test_system_paths_rule.py:53` calls only this handler, so it cannot see the clash.
   - The scratch directory is also not the harness's probe root (`playbook_harness.py:148-152` names `ProjectPath.ACCEPTANCE_DIR`).
   - **Fix:** use `$CLAUDE_PROJECT_DIR/untracked/acceptance/zz-...txt`. The harness expands `$CLAUDE_PROJECT_DIR` (`playbook_harness.py:363`).

3. **Defence Before Fix (a hard rule) is incomplete: the class has no detector.** — `.semgrep/hook-handlers.yml`
   - The class is named and the sweep covered every project handler: only 3 handlers read `tool_input`, and all 3 were handled.
   - But `.semgrep/hook-handlers.yml` still has only `hook-path-resolved-against-cwd`. No rule flags a handler that regex-matches `get_bash_command(...)` output without going through `command_position_*`.
   - Semgrep can express that, so either add the rule (red on the old handler, blocking in `qa-hook-handler-rules`) or say explicitly why it cannot be done.

### Should fix

4. **The written-script shapes are untested for the other guard.** `test_hidden_git_mutation_text_mentions.py` pins the text-mention cases well. `hidden_git_mutation` itself showed no regression: 23 real-run forms gave the same operands old and new, and only the 4 quoted mentions changed. Item 1's shapes still need tests on the ansible side.

### Nits

5. **The deny message says BLOCKED twice.** It now opens with `BLOCKED [R-...]: <rule blocked text>` followed by the old `BLOCKED: Direct system management...` (and `❌ BLOCKED: Direct editing...` in system_paths). Drop the old headline line.
6. **The acceptance tests rely on a false positive.** `bash -n -c '...'` runs nothing but is still denied. That is what lets the deny tests be harmless, but the test then asserts a known false positive. Note it in `safety_notes`.
7. **Small cleanups in `hidden_git_mutation.py`.**
   - `:133`: `is_plain_data_command(command)` is evaluated on every pass of the loop with the same whole-command argument; compute it once.
   - `:134`: substituted-file operands use the starting directory even after a `cd` (`cd sub && bash -c "$(cat f)"` resolves to the outer directory). This was already true before the change; the per-segment `cwd` is now available to fix it.

### Pre-existing gaps (denied by neither version, for the record only)
- `dnf -y install`, `sudo dnf -y install`, `dnf --assumeyes install` (an option before the verb). This is the most common real form.
- `dnf5 install`
- `'dnf' install`, `"dnf" install`
- `$(echo dnf) install`, `` `echo dnf` install ``

### False positives still present (new handler)
- An unquoted heredoc to cat: `cat <<EOF > notes.md` with `sudo dnf install` in the body.
- Text with a `$` expansion: `echo "run: dnf install $PKG"`, `grep -n "dnf install $X" f`.
- `jq '."dnf install"' f`
- `python3 -c "print('pip install x')"`
- `bash -n -c '...'`

### Checked and clean
- **No degrade, everything else:** of about 200 real-run forms, every one denied before is still denied apart from item 1. That covers sudo/doas/pkexec, env assignments, `bash -c` and `sh -c` (both quote styles, flag clusters, nested), `$(...)`, backticks, xargs, exec, pipelines, `;` `&&` `||` `&`, newlines, heredocs and here-strings fed to a shell, eval, `( )` and `{ ; }`, absolute paths, command/nice/timeout/nohup/time/setsid/stdbuf/ionice/flock/strace, if/for, process substitution, `awk system()`, `rg --pre`, `git -c alias`, and `python3 -c os.system`.
- **False positives the issue asked for:** all fixed. Both commands quoted in #294, commit and tag messages (both quote styles), `git log --grep`, gh body and title, quoted heredocs to cat, echo/printf/grep/rg/awk over literals, and comments now pass.
- **`bash -n -c`:** `bash -n -c 'echo RAN'` printed nothing and exited 0, so it parses without running. The harness never executes payloads anyway ("A probe is inert", `playbook_harness.py:15-18`). The ansible probes pass their patterns in-process.
- **Rule ids:** `Rule` + `get_rules()` + `.under_rule()` matches the project precedent in `hidden_git_mutation`. Daemon handlers such as `sudo_pip.py:229-310` use `RuleID` constants plus a disclosure tracker, which project handlers cannot import, so this is acceptable.
- **Generated docs:** the four commits touch only handler and test files. The generated `CLAUDE.md` block and `HOOKS-DAEMON.md` were not hand-edited. The new ids will appear after a daemon restart.
- **Install-specific names:** none added; the diff removes a real username from an old test.
- **Test input shape:** the tests use `{"tool_name": ..., "tool_input": ...}` (plus `cwd` for `hidden_git_mutation`), the shape the handlers read.

### Mechanical gates
- **`scripts/qa-hook-handlers.bash`:** `HOOK-HANDLERS-OK 390 test(s)`.
- **`scripts/qa-hook-handler-rules.bash`** on the 3 handlers: `passed: 3 failed: 0`.
- **ruff 0.16.8** on the 6 changed files: all checks passed.
- **`qa-changed.bash --files ...`:** did not complete. It sat waiting on another session's QA run (pid 1362171) for 3+ minutes, so I stopped it; the background "exit 0" notice is the wrapper's, not a QA result. The checks it had queued for these files (hook-handlers, hook-handler-rules, ruff) are the three runs above.
- **`qa-all.bash`:** not run; sub-agents are barred from full QA (R-SUBAGENT-FULL-QA).
- **plan-qa / syntax-check:** not applicable; no playbook or plan files are in these commits.

### Files
- `/workspace/untracked/worktrees/sealed-carry/.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py`
- `/workspace/untracked/worktrees/sealed-carry/.claude/hooks/handlers/pre_tool_use/system_paths.py`
- `/workspace/untracked/worktrees/sealed-carry/.claude/hooks/handlers/pre_tool_use/hidden_git_mutation.py`
- `/workspace/untracked/worktrees/sealed-carry/.claude/hooks/handlers/pre_tool_use/tests/test_ansible_enforcement.py`
- `/workspace/.claude/hooks-daemon/src/claude_code_hooks_daemon/utils/command_position.py`
- `/workspace/.claude/hooks-daemon/src/claude_code_hooks_daemon/daemon/playbook_harness.py`
- The scratch files I left are in `/workspace/untracked/scratch/qarev-294/` (gitignored): the old and new handler copies, and `qa-changed.out`.

**Dogfood faults:** these need logging by the coordinator.
- The running daemon's old `ansible_enforcement` denied my `python3 -I - <<'PYEOF'` probe, a heredoc script that only defined test strings. That is #294's own symptom.
- `gh_pr_comments` and `gh_issue_comments` likewise denied the same kind of heredoc because it contained the text `gh pr view` / `gh issue view` inside a Python string.

STOPPING BECAUSE: review complete; findings reported above for the caller to fix.