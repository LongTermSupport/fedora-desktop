VERDICT: FIX-FIRST

I ran the new gate, the Go tests and qa-changed in the worktree and changed nothing. Everything passed:

- `scripts/qa-hook-handlers.bash` printed `HOOK-HANDLERS-OK 111 test(s)`.
- `go test` over `hookhandlers`, `cmd/qa-hook-handlers`, `changed` and `all` passed.
- `./scripts/qa-changed.bash --base integration/261021-membership` passed 21 files, including ansible-syntax on `play-python.yml`, bash-limits, docs and `test-meta-deploy`.
- `git diff --check` is clean.

One finding blocks the merge; the rest do not.

**1. BLOCKING: the block check still reads the path as written, so a path through a symlink reaches a system directory.** In `.claude/hooks/handlers/pre_tool_use/system_paths.py`, `matches()` (around lines 161-188) only folds `.`, `..` and repeated slashes before comparing against `BLOCKED_PATHS`. It never follows symlinks. The new resolution check guards only the `.claude/projects` exemption. I ran `matches()` with the daemon venv interpreter and each of these returned `False` (allowed):

- `/bin/foo`, `/lib/x`, `/sbin/x`: on Fedora these are symlinks into `/usr`, so the writes land in `/usr/bin`, `/usr/lib` and `/usr/sbin`.
- `/proc/self/root/etc/hosts`: this writes `/etc/hosts`.
- The project exemption is also lexical, so `<project>/link/x` with `link -> /etc` is exempt. So is `<project>/link/../x`, which is the same `link/../x` shape the commit fixed for the projects directory.

This predates the branch, but it is the "spelling and resolution disagree" class the commit says it closed, and the fix is small. Also apply the project exemption and the block check to `os.path.realpath(file_path)`: block if either the folded path or the resolved path starts with a blocked prefix, and exempt only if both lie under the project. Add real-symlink tests for `/bin` and `/proc/self/root`.

**2. Not blocking: the other attack inputs either hold or are out of scope.**

- `//etc`, `/./etc`, `/tmp/../etc` and `<project>/../etc` are blocked.
- A trailing slash on a directory, case, and Unicode lookalikes give different paths on Linux, so there is nothing to bypass.
- Relative paths and `~` are not blocked, but the active `R-ABSOLUTE-PATH-REQUIRED` handler already denies relative `Write`/`Edit` paths.
- No false positives on `/workspace/untracked/x`, `/workspace/.claude/worktrees/...` or `/root/.claude/projects/-workspace/memory/...` (a symlink here).
- Trusting a symlinked projects directory is acceptable: re-pointing `/root/.claude` takes Bash, which this handler never guards.
- One host false positive remains from before this branch: a checkout reached through a symlinked parent is blocked under `/home/`, because the project root is resolved but the incoming path is not.

**3. Not blocking: the CI exemption.** `run.go` (the `exemptVar` branch in `run()`) trusts `GITHUB_ACTIONS=true`, so a local run with both variables set skips the gate. That takes two deliberate variables and prints `HOOK-HANDLERS-EXEMPT`, so it is acceptable. However, `CLAUDE/QA.md:82,196` presents it as "GitHub Actions only" without saying it is detected from the environment. Add one sentence. It is the same kind of named skip `qa-deployed-drift` already makes, it is set in the open in `.github/workflows/qa.yml`, and its reason is printed, so I don't count it against the fail-fast rule. Provisioning the daemon in CI instead is a later choice.

**4. Not blocking: exit codes are handled correctly.** `hook-handlers` is a `gate` row, and `src-go/qa/all/gate.go:48` treats any non-zero code as a failure that does not abort the run. That matches the other gates: 1 is a finding, 2 means it could not judge.

**5. Not blocking: mixing Pythons.** Only one site directory is added, at the end of the path, and pytest, pluggy and iniconfig are pure Python, so there is no binary-compatibility risk. In the container the venv is built from `/usr/bin/python3.11` itself, so nothing is mixed. The daemon's resolver (`paths.py` around line 502) picks the highest `python3.N` on PATH. On the host, `play-python.yml` installs pyenv but never sets `pyenv global`, so the `python3.14` shim falls through to the system Python and Fedora's `python3-pytest` matches. If the owner does set a pyenv global, the gate exits 2 and its message says to run `play-python.yml`, which would not help; the meta-deploy comment does warn about that case. A sturdier design is to ask the venv's own base interpreter (`sys._base_executable`) for pytest instead of whatever `python<X.Y>` comes first on PATH. A smaller point: on Debian the appended directory is `/usr/lib/python3/dist-packages`, so a module the venv lacks would be found there quietly.

**6. Not blocking: the playbook, queue and table edits are correct.**

- The `play-python.yml` task (become, `ansible.builtin.dnf`, `python3-pytest`) is in the same style as the task beside it, and the syntax check passes.
- The `meta-deploy.list` entry is accurate and names no install.
- `qa-gates.tsv` gains one row, whose detail pattern matches both the pass and exempt lines.
- `qa-routing.tsv` gains one `also` row, and the `perfile-gate` line differs only by the appended `qa-hook-handlers`.
- The two corrected `ansible_enforcement` tests match the handler's fail-closed design.

**7. Not blocking: docs and plan are accurate.** The PLAN Task 4.6 text matches the code, `CLAUDE/QA.md` and `src-go/CLAUDE.md` are updated, bash-limits is green (the wrapper has 4 code lines), and no install-specific names appear (tests use `/home/u/`).

**Required before merge:** finding 1. **Recommended:** the documentation sentence in finding 3 and the base-interpreter probe in finding 5.

My scratch files are `/workspace/untracked/scratch/sp_probe.py`, `hh-review.out` and `hh-qac.out`.
