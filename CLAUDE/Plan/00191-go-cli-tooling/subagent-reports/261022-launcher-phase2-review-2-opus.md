VERDICT: FIX-FIRST

I checked tip d6b508bf in its worktree and changed nothing there. All runs passed:

- `test-ccy-token-handoff.bash`: 0 failed.
- `test-ccy-info-flags.bash`: 90 of 90.
- `go test` for ccylaunch, cmd/claude-yolo, engine, sessions and teams/restart: all ok.
- The difftest with `LAUNCHER=` a front built with `-X main.Version=3.99.14.2`, with `claude-yolo.bash` and `lib/` beside it: TestLauncherParity 82 of 82. The front-only tests skip, as designed.
- `qa-changed.bash --base 0390d976`: green.
- `ansible-playbook --syntax-check` on play-claude-yolo.yml: clean.
- The branch contains F44.

The only scratch output is under `/workspace/untracked/scratch/r2-*`.

## Blocking

**1. The new recovery advice in docs/ccy.md does not work for an ordinary terminal launch.**

- `docs/ccy.md:1689-1691` (new in 21c4c816) says that if the Go program will not start, you run `/var/local/claude-yolo/claude-yolo.bash` directly.
- After fix 1, though, the bash's terminal insulation at `files/var/local/claude-yolo/claude-yolo:1181` starts the tmux pane on `"$SCRIPT_DIR/claude-yolo"`, which is the broken front.
- The custom-Dockerfile modes (1159, 1165) and the restart relaunch (3847) also re-run the front.
- So the documented recovery attaches a tmux session whose pane runs the program that will not start. It only works for `--headless` and the token-create/renew modes.
- Fix, either of:
  - (a) Correct the sentence: the working way out is the old-revision play rollback, or `--headless`.
  - (b) Have the front hand its own path to the bash (for example an env var it sets before exec). The bash then names `${that:-$SCRIPT_DIR/$(basename "$0")}`, so a direct `.bash` run records itself and a handed-over run records the front. Pin both cases in the new token-handoff block.

## Not blocking

**2. Self-path sweep is clean.** No remaining `$0`, `BASH_SOURCE` or `basename` self-path in the launcher or `lib/` reaches a record or a re-run.

- `claude-yolo:126` (the `CCY_HASH` input) and `:2145` (the rebuild changelog line) read `$0` as text. That is correct, because `$0` is the bash, and the Go `IntegrityHash` matches it.
- `SCRIPT_DIR` (line 26) uses `BASH_SOURCE`, which only picks the directory, so it is fine.
- A host before this deploy, or a checkout run: `$SCRIPT_DIR/claude-yolo` is the bash itself, so behaviour is unchanged.
- CCY_LEGACY: inside the pane the front runs without CCY_LEGACY, because tmux does not carry it. That is harmless, since the front only answers `--version` and `--help` itself. The docs' "every run" is loose but not wrong in effect.

**3. No Go caller compares the recorded launcher against a fixed path.**

- `sessions/restore.go:236` and `teams/restart/jobs.go:288` replay `rec.Launcher` as written.
- `sessions/app.go:37`, `teams/config/config.go:39` and `wsitoken/app.go:95` execute the front's path.
- The VM guest acceptance checks only test `-x`.
- 00194 and 00201 declare `CCY_LAUNCHER` but never read it.
- 00098 reads the checkout's copy, not the deployed one.
- No plan script that reads the deployed launcher as text was missed.

**4. The `#!` check in the plan scripts is sound.** An ELF starts with `\x7fELF`.

- The empty-value guards fail loudly in m2-check:183, triage-ssh-agent-probe:77 and the u20 message.
- One small gap: if `claude-yolo` itself is missing, `head` fails inside `[[ ]]`, which `set -e` ignores, and the script falls through to `.bash`. The later read of the missing file still fails (an awk assignment under `set -e`, or `|| return 1`), so it is not silent, just less precise.

**5. The new test does fail on the old code, for the right reason.**

- I put the 0390d976 launcher into a scratch copy of the tip.
- "the record's launcher is the front" failed: it wanted `launcher=…/install/claude-yolo` and got nothing.
- "and the pane runs the front" failed: it wanted 1 and got 0.
- The red record check shows an empty value rather than the `.bash` path, because under the old code the pane runs the `.bash` file and the fake front never prints. The pane-command check is the one that shows the behaviour directly.

**6. Versions and deploy text are correct.**

- CCY 3.99.14.2, and container 2.78.14 matches the Dockerfile LABEL. The container bump is needed because `CCY-GUIDE.txt` is copied into the image (`Dockerfile:374`).
- The meta-deploy entry says 3.99.14.2, a one-time image rebuild, and the stale-`.bash` rollback note. All three are accurate.
- The allowlist was tightened from 2733 to 2732.

**7. Still open from round 1, minor.** `.claude/hooks/handlers/pre_tool_use/system_paths.py` has no mapping for `/var/local/claude-yolo/claude-yolo.bash` to its source file, which is `files/var/local/claude-yolo/claude-yolo`.

**8. Not a defect.** The worktree checkout has `claude-yolo` at mode 0600 (git 100644, the container's umask). The play installs it at 0755.
