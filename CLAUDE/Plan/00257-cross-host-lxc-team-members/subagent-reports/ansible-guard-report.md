# Issue 294 report: ansible_enforcement and the same fault class

Branch `plan-xhost-lxc-members`. Commits (all signed and pushed): d3addfdf3 (RED), 9e854728a (fix), c7f685789 (RED for the sweep), 6b2976680 (sweep fixes).

## Per item

1. Judge only what runs: FIXED. `ansible_enforcement.py` matches per segment of `command_position_code_segments`. 88 tests were red against the old handler; every real-run form in the block list (sudo, `&&`, `$(...)`, backticks, `bash -c`, `env X=1 pip install`, pipelines, heredoc to a shell, written-then-run script, subshell, loops) was already green before the change, so nothing that denied before denies less now. Text mentions pass (echo, printf, grep, rg, git log --grep, git/gh messages, heredoc to cat, comments).
2. Rule id: FIXED. `R-ANSIBLE-DIRECT-SYSTEM-CHANGE`. Mechanism found: a project handler subclasses `PreToolUseHandlerBase`, builds a `Rule`, returns it from `get_rules()` and files the deny with `GatingResult.deny(...).under_rule(rule)`. The id is a plain string (no `RuleID` constant needed). `explain-rule R-UNTRACKED-SCRIPT-GIT-MUTATION` resolves today in the live daemon; the new ids resolve only after a daemon restart (see below).
3. Acceptance tests: FIXED. Denied ones are `bash -n -c '...'` (parse only), allowed ones are an echo and `dnf info` of a package that does not exist. Unit tests pin both properties and the matcher verdict of every acceptance command.
4. Sweep:
   - `hidden_git_mutation.py`: one instance, FIXED. The `bash -c "$(cat f)"` / `eval "$(< f)"` regex ran over the whole text. Now per segment, plus `is_plain_data_command` for a bare echo/printf/grep. Rule id already present. Acceptance tests run an uncommitted script that exits before its git line (deny) and one that only echoes and runs `git status` (allow): judged safe, unchanged. Residual: an echo/grep with `$` inside a compound command is still read as written by the daemon's view (false positive, never a bypass).
   - `system_paths.py`: Write/Edit only, so no whole-text matching. Two other instances FIXED: no rule id (now `R-SYSTEM-PATH-DEPLOYED-FILE-EDIT`), and acceptance tests that ran a Bash `echo` the handler never sees (now Edit calls on non-existent paths, so a miss changes nothing).
   - Tests: `test_ansible_enforcement.py` (rewritten), `test_hidden_git_mutation_text_mentions.py`, `test_system_paths_rule.py`. Handler suite: 390 passed.
5. QA: the diff-mode run of `qa-changed.bash` (`untracked/scratch/ansible-guard-qa.log`) failed only on the other agent's uncommitted Go files (typecheck errors in `src-go/internal/teamchat/seat` and `teams`), not on my work. The same script with `--files` on my six files passes (`untracked/scratch/ansible-guard-qa-mine.log`: ruff, hook-handlers, hook-handler-rules, gitleaks and the rest green). Not a full qa-all.
6. Daemon restart: NOT done, needs the human's approval. Until it restarts, the live daemon runs the old `ansible_enforcement` (text-matching, no rule id), and the new rule ids do not resolve in `explain-rule`.
7. Comment on #294: posted (not closed).

## Faults met (dogfooding)

- `qa-changed.bash` with no arguments judges the working tree against HEAD, so once a change is committed it checks nothing of it, and another agent's dirty files on the same branch fail it. Workaround: `--files` (documented). Not logged as a separate issue: noted here for the coordinator.
- The Edit tool was unavailable in this session; two one-off exact-string patch scripts were used (`untracked/scratch/ansible-guard-patch*.py`).
- Left as is: the guidance categoriser substring-matches `pip` (a `systemctl restart pipewire` gets package wording).
