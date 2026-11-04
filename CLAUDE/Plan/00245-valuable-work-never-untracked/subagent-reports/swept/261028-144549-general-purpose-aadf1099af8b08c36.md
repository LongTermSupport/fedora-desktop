Task 3.11 is done in one commit, `c4c22aec5`, on branch `agent-aadf1099af8b08c36-f9bb4fce` (from F44). Its tests and `./scripts/qa-changed.bash` pass; `qa-all.bash` was not run, as instructed, and nothing is pushed or merged.

**Where the code was:** `run.bash --changed` does not run a Go binary. It runs the Python helper `helpers/play_ledger/changed_plays.py`, so I extended that helper instead of writing Go. The health report's play-freshness check uses the same code, so it follows the new rule too.

**The rule for "a play reads a variable":** it only applies when `environment/localhost/group_vars/desktop.yml` changed since the commit the play last ran from.
- **Which variables changed:** each top-level key is compared, with its indented lines, between that commit and the working tree. Comments and blank lines are ignored.
  - A key counts as changed if its value differs, or if it was added or removed.
  - A key whose value uses a changed key also counts, and so on down the chain. For example, `provisioning_profile` is computed from `_systemd_default_target`.
- **Which plays read them:** a play is stale if a changed name appears as a whole word in any of its input files. These are the files the existing staleness mapper already gives it: the play, the task, vars and template files it includes, files it copies or runs, and every file under a directory it names. Go packages are left out. This leans towards rerunning a play, never missing one.
- **When the file can't be read:** a line the reader cannot parse, or a key set twice, makes `--changed` stop with no answer rather than silently drop a variable.
- **Output:** each stale play gets a reason line, `changed variables of environment/localhost/group_vars/desktop.yml: <names>`.
- **Where it's documented:** in full in the docstring of `helpers/play_ledger/group_vars.py`, and briefly in the `--changed` paragraph of `docs/playbooks.md`.

**A problem I found and fixed:** the new module names the desktop.yml path in its code. Because of the earlier change in Task 3.7, three plays that run the play-ledger helpers then counted desktop.yml as their own input: fedora-desktop-panel, displaylink and gnome-shell-extensions. That would have rerun them after any edit to the file. I added an entry to `helpers/self_update/not-play-inputs.json`, the same way `changed_plays.py`'s `playbook-main.yml` entry is done. A new test on the real checkout checks that no play has desktop.yml as an input. The journal entry was written before this, so the fix is in the commit message and the report but not the journal.

**Tests:**
- **Parsing and matching:** a new `tests/helpers/play_ledger/test_group_vars.py` with 15 tests, including a real git repo.
- **The judgement, with stubs:** 7 new tests in `test_changed_plays.py` covering:
  - one changed variable reruns only the play that reads it;
  - a variable no play reads reruns none;
  - added and removed variables;
  - the file is read once per commit and not at all when unchanged;
  - an unreadable file gives no answer.

  These failed first, then passed.
- **End to end:** one test through `main` with real git and a real ledger.
- **On this checkout's real plays:** a new variable leaves the basic-configs and fonts plays current; changing `install_audience` makes basic-configs stale but not fonts.
- **Results:** 168 tests pass across the play-ledger and self-update modules, plus the run.bash `--changed`, `--rerun`, gh-scopes, consumer and health suites that qa-changed picked.

**Measured on this checkout:** a change to `install_audience` reruns 53 of 82 plays, and `provisioning_profile` 38. Reading every play's files takes about 2 s, and only when desktop.yml changed.

**Not changed:**
- `run.bash` itself, so there is no version bump or changelog entry. Its `--help` text still describes inputs as "its own file, or anything it deploys" and doesn't mention variables.
- The unattended self-update timer still treats desktop.yml as an input of no play; the task covered `--changed` only.

Task 3.11 is ticked in `PLAN.md` and journalled once, in `JOURNAL/2026-10-28/144217Z-action-be54c3.md`.

The full report is at `<worktree>/untracked/scratch/261028-changed-vars-report.md`.