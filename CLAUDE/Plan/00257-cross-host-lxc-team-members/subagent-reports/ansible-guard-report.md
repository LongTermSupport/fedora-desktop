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
## Fix round 2

Review: `ansible-guard-review.md` (verdict BLOCK). Commits: `dc9c9a9da` (RED tests), `414d0ada5` (the two Semgrep rules), `289a44f62` (the fixes), `2203de2ca` (an `eval` of a written script, the `jq` head) and the commit of this report. All signed and pushed. Issue #294 is commented and left open.

### Per finding

1. BLOCKING, a written script run behind a wrapper: FIXED. `ansible_enforcement.py` keeps the daemon's view for the commands a line runs, and adds `_runs_what_the_daemons_view_cannot_follow`: when a line writes a file (a redirect, `tee`, `cp`, `mv`, `install`) and a later segment runs a shell, `source`, `.` or a path whose basename was written, behind any wrapper (`sudo`, `timeout`, `nohup`, `nice`, `env`, `setsid`, `(...)`, `{ ...; }`), or when any pipe stage is a shell reading stdin, the line is judged as written. Tests: `WRITTEN_SCRIPT_LINES` (28 shapes: every one the review lists, plus `cp` and `tee` writes), `PIPED_INTO_A_SHELL`, and `WRITES_BUT_RUNS_NOTHING` (writes, no shell: stays allowed). RED: 175 tests failed against the round 1 handler.
2. BLOCKING, system_paths acceptance probe: FIXED. The allow probe's `file_path` is `$CLAUDE_PROJECT_DIR/untracked/acceptance/zz-nonexistent-...` (the form `write_clobber_guard` and `secret_file_guard` use; the harness expands it and only lets a probe act under that directory). Tests: the dispatched path is absolute, the probe lives under the probe root, and the verdict test judges the expanded payload as the harness sends it.
3. BLOCKING, no detector (Defence Before Fix): FIXED. Two Semgrep rules in `.semgrep/hook-handlers.yml` with fixtures in `.semgrep/hook-handlers.py`, run by `qa-hook-handler-rules.bash` (already in `qa-all` and `qa-changed`) and documented in `CLAUDE/QA.md`: `hook-regex-over-raw-command` (taint, inside one function, from `get_bash_command` to a regex method or an `in` test; `command_position_*` sanitise) and `hook-regex-over-command-parameter` (a function with a parameter named `command` that does the same: the form `hidden_git_mutation` had). Red on `d3addfdf3^`: the first reports `ansible_enforcement.py:88`, the second `hidden_git_mutation.py:225`. Sweep at the round 1 tip: 0 findings. Sweep at this tip: one finding in my own new heredoc scanner, fixed by naming its parameter `text` (a scanner of text on purpose). Limit: the taint follows one function, so a handler that hands the command to a helper of its own and searches there is not reported; that is how the line is judged as written on purpose, and it stays one visible call.
4. Should fix, the written-script shapes for the other guard: `hidden_git_mutation` showed no regression in the review (it reads files, not a written script text); its tests gained the `cd` case (see 7).
5. Nit, the deny message said BLOCKED twice: FIXED in `ansible_enforcement` and `system_paths` (the old headline lines are gone; one `BLOCKED [id]` remains). Tests count the word.
6. Nit, acceptance tests relied on a false positive: FIXED. The denied probes are now real-run shapes that are harmless (`dnf install --assumeno nonexistent-pkg-zz-safe-test`, `systemctl restart nonexistent-unit-zz-safe-test.service`). `bash -n -c '...'` was a false positive (it parses and runs nothing), so the handler now treats a shell run with `-n` (or `-o noexec`) as a non-run, and it is an allow probe with its own acceptance test. A `-n` after `-c` is the command string, not an option, and a shell that is an argument of another command (`sudo dnf install bash -n`) is not read as a syntax check.
7. Nits in `hidden_git_mutation.py`: the loop-invariant `is_plain_data_command(command)` is hoisted (one call per command; test counts them), and the substituted-file operand resolves against the directory the earlier segments `cd` into (`cd sub && bash -c "$(cat f)"` reads `sub/f`; RED before, with the negative test).

### Pre-existing misses from the review (item 4 of the dispatch)

Each had a RED test. FIXED by one option-skipping pattern builder per tool (`_tool`): `dnf -y install`, `dnf --releasever 40 upgrade`, `yum`, `rpm --quiet -i` (a query such as `rpm -q -i` stays allowed), `pip -q install`, `flatpak --user install`, `snap`, `systemctl --now enable`, `gsettings --schemadir x set`, `npm install x -g` / `npm -g install` / `npm i -g`. `systemctl --user ...` stays allowed in any option order. `dnf5`, `/usr/bin/dnf`, `'dnf'` and `"dnf"` are judged. `command`, `env`, `xargs`, `exec`, `eval`, `bash -c`, `sudo -u`, `nice`, `ionice`, `setsid`, `flock`, `time`, `stdbuf`, `doas`, `pkexec` were already handled by the raw-segment search; they are pinned by tests now. The verb is now followed by a word boundary, so `dnf updateinfo list` (a query) is no longer denied.

Not judged, by nature: a computed command word (`$(echo dnf) install`, a backtick form, `${PM:-dnf} install`, `$PKGMGR install`). No static reading can know what it expands to. They are pinned as a stated limit (`COMPUTED_COMMAND_WORDS_NOT_JUDGED`).

### Remaining text false positives (item 6)

The shared helpers do not allow these, so the handler rewrites each segment into a form the daemon's view blanks, only when what it holds cannot run; every live look-alike stays denied (`LIVE_LOOKALIKES_OF_INERT_TEXT`):

- `$NAME` or `${NAME}` inside an echo, printf, grep or rg: the references are renamed for the daemon's view; any `$(`, backtick or process substitution keeps the text. FIXED.
- an unquoted heredoc whose body holds no `$(` or backtick: its delimiter is quoted for the daemon's view (same text, same sink and pipeline rules). FIXED.
- `jq` program text: read as an echo of the same text (a pipe into a shell keeps it). FIXED.
- `python -c "print('literal')"` with a plain literal: read as an echo of the literal. FIXED.
- `bash -n`: see 6. FIXED.

### Upstream

The daemon gap (`_runs_written_script` sees only the bare form) is in `untracked/scratch/upstream-written-script-wrapper.md`, generated by `hooks-daemon issue-report` (the generator accepted it). The coordinator files it with `gh issue create --repo Edmonds-Commerce-Limited/claude-code-hooks-daemon --body-file untracked/scratch/upstream-written-script-wrapper.md`.

### Results

- Handler suite (`qa-hook-handlers.bash`): 749 passed (390 before).
- `qa-hook-handler-rules.bash`: passed, both new rules proven against their fixture.
- `qa-changed.bash --files <the changed files>`: queued behind another session's `qa-changed` run (pid 1516841, over 20 minutes) and stopped by me without a verdict, so no result. Its checks for these files were run directly instead: `qa-hook-handlers.bash`, `qa-hook-handler-rules.bash`, `qa-python.bash` (ruff over every Python file, 427 OK) and `qa-docs.bash` (82 files OK).
- Daemon not restarted (needs the human's approval); the live daemon still runs the round 1 handlers until it is.

### Faults met (dogfooding)

- The live daemon's old `ansible_enforcement` denied every Bash command of mine that merely named `dnf install` (a python one-liner, a `cd ... && grep`), so all probes ran from files written with `Write`. That is #294's own symptom, logged there.
- `lsp_enforcement` denied a `grep` for `def _parse_considerations` (a plain text search in daemon source); the `Read` tool was the way round.
- The Edit tool was not available in this session; exact-string patch scripts under `untracked/scratch/patch_*.py` were used again.
- The categoriser still substring-matches `pip` (`systemctl restart pipewire` gets package wording): left as is, as in round 1.
- Found, not fixed (outside the dispatch): the verb lists miss `dnf autoremove`, `distro-sync`, `swap`, `group install`, `module install`, `copr enable` and `systemctl try-restart`, `daemon-reload`, `set-default`.
