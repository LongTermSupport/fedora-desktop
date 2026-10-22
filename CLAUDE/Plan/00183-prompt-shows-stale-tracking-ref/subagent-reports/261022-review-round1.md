Verdict: FIX-FIRST

1. **FIX-FIRST**: the play change is never queued for the host. The branch edits `playbooks/imports/play-git-configure-and-tools.yml` (the pinned checkout, the assert and the `~/.cache` task), but nothing has run on the host yet. The diff does not touch `CLAUDE/Plan/meta-deploy.list`, and no existing entry there names this play or plan 00183. The rule in CLAUDE.md is that a host run goes into the queue in the same commit. Fix: add `playbooks/imports/play-git-configure-and-tools.yml` (or a 00183 `deploy.bash`) to `meta-deploy.list` in this branch. Then tell the owner what it will do: move `~/.bash-git-prompt` to 4501746, run the assert, and set `~/.cache` to 0700.

2. **FIX-FIRST**: PLAN.md Task 2.3 (around lines 57-67) now says only the owner's hand check is left. That is wrong: the pinned checkout and the assert have never run on a host. The hand check should also come after that run, so it tests the pinned tree. Fix: add "Left: run `play-git-configure-and-tools.yml` via meta-deploy (pin and assert), then the owner's hand check". The JOURNAL `action` entry stands as written, since it is append-only; record the pending run in a `handoff` entry.

3. **NIT**: the docs don't cover the new behaviour. `docs/playbooks.md:265` still just says "Installs bash-git-prompt". The play now:

   - fetches from GitHub on every run (the git module in 2.19.14 always fetches, with `--tags`; the old `creates:` made re-runs offline);
   - fails if a tracked file in `~/.bash-git-prompt` has been changed;
   - leaves the checkout on a detached HEAD at the pin.

   Fix: add one sentence there, for example "checked out at the commit in `bash_git_prompt_version`; local edits to tracked files stop the play".

4. **NIT**: the comment at `play-git-configure-and-tools.yml:199` says only that local changes stop the play. Worth adding that every run fetches over the network, so the play now needs GitHub reachable even when nothing changes. Fix: one clause in that comment.

Checked and clean:

- **Pin is upstream's.** `git ls-remote` gives 450174627cb479d918b4b8c8257de00a2b98b425 for both HEAD and `refs/heads/master`, committed 2026-09-17. In a clone at that commit:
  - `gitprompt.sh:486` is the only bash `async_run` call, `async_run "GIT_TERMINAL_PROMPT=0 git fetch --quiet"`;
  - line 484 is the zsh `async_run_zsh` call, which the pattern correctly does not match;
  - no other `*.sh` file in the repo calls it.
- **The git module moves an existing shallow clone idempotently.** I read the 2.19.14 `modules/git.py`:
  - a SHA `version` skips `ls-remote`;
  - `fetch()` runs `git fetch --tags origin` with no `--depth`; I ran that on a `--depth=1` clone and it worked;
  - `switch_version` then runs `git checkout --force <sha>`;
  - `changed` is just "before differs from after", so re-runs report ok;
  - `force: false` fails on modified tracked files only; untracked `??` files are ignored.
- **The assert matches `check_override`.** Jinja's `match` is anchored like `re.match`, and `[ \t]*async_run ` is the grep's `^[[:space:]]*async_run `. I evaluated the expression with ansible-core 2.19.14 against the pinned file and got exactly line 486. Both conditions in the play mirror the script's two checks: at least one call, and none without `git fetch`. A broken assumption fails the play before the "Git Bash Prompt" bashrc block runs.
- **Task order is right:** checkout, slurp, assert, then later the `~/.cache` task, then the bashrc block.
- **`~/.cache` task:** `become: false`, owned by the login user. 0700 on an existing directory is harmless. The only other `~/.cache` users are the user's own (`build-go-command.yml` subdirectories, claude-yolo state files, git-account-helper), and nothing else sets a mode on `~/.cache` itself.
- **Rules:** no `failed_when` or `ignore_errors`. No 2.19 traps: no `: -x` in task names, and no apostrophes in `shell:` comments. `ansible-playbook --syntax-check` passes.
- **`vars/version-pins.yml`:** the row is a valid manual one (`note` present, no `github`, `kind: untracked` with a `why`), matching the md-viewer row. `qa-version-pins.bash`: VERSION-PINS-OK, 13 of 13 resolve to a live playbook var.
- **Findings 2-5:** the PLAN.md wording, milestones and docs/configuration.md sentence are accurate. The commit hashes are real, and the journal's 07:52 UTC matches the commit's 08:52+01:00.
- **Public repo:** nothing install-specific in the PLAN.md, docs/configuration.md or JOURNAL changes.

One thing left behind: my scratch clone at `/tmp/tmp.ihqejfb5Oa` is still there, because the permission system refused the `rm -rf`. It is outside the repo and can be deleted.
