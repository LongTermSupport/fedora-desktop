VERDICT: FIX-FIRST

The token narrowing works as claimed for every container run, and the old behaviour was worse than the branch says (finding 2). But one path the docs promise now silently stops working, and its own error message tells the user to do the thing that no longer works (finding 1).

**Blocking**

1. **A `GH_TOKEN` you export yourself is now dropped on every interactive launch, and the error message recommends exactly that.**
   - `claude-yolo:23` takes the export off all three token variables.
   - `claude-yolo:1178` (`ccy_tmux_insulate`) then re-runs the launcher inside tmux for every non-headless launch. That happens before GitHub setup at `claude-yolo:1384`.
   - So the inner launcher never sees your `GH_TOKEN`. It falls back to `gh auth token` (`lib/ssh-handling.bash:1776`). On a box with no gh login that fails, and `lib/ssh-handling.bash:1783-1789` prints "export GH_TOKEN=… then re-run ccy", which is the step the user already took.
   - The same thing happens through the `exec "$script_path"` re-runs in `lib/dockerfile-custom.bash:45,164,726,769` (the custom-Dockerfile modes), even before tmux is involved.
   - This path is documented: `docs/ccy.md:1768` says "`export GH_TOKEN=…` for `gh`" on a box with no account. The comment at `lib/ssh-handling.bash:1721-1723` also names it ("a session on a box with no gh login that received the token from the person's own machine"). That is typically an interactive ssh session, which is wrapped in tmux.
   - The draft changelog admits it in passing ("takes its GitHub token from gh's login"). But the message, the troubleshooting row and the comment were not changed. And `TestLaunchHandsTokensOn` covers `launch-gh-token-env` only for a run that is never wrapped in tmux, so this regression is invisible to the harness.
   - Fix, either:
     - (a) carry the token to the inner launcher without it reaching argv or tmux's global environment, for example through a 0600 file in `XDG_RUNTIME_DIR` that the inner launcher reads and deletes; or
     - (b) have the owner accept the loss explicitly, and correct `ssh-handling.bash:1783-1789`, `docs/ccy.md:1768` and the comment at 1720-1726 to say an exported `GH_TOKEN` works only with `--headless`.
   - The owner's own desktop, where gh is logged in, is not affected. Headless servers and boxes with no login are.

**Not blocking**

2. **The old behaviour leaked one session's token into later sessions. This confirms the change is right.** I tested it directly. `GH_TOKEN=abc tmux -L S new-session …` followed by `GH_TOKEN=xyz tmux -L S new-session …` gave the second pane `abc`. tmux's global environment comes from whichever client started the server, so the first launch's token went to every later session's panes, even a different account's. The changelog should say this, since it is the strongest reason for the change.

3. **The function-prefix question (task item 2) is answered: it reaches podman.** `container_cmd` is a function (`lib/common.bash:46-48`, `"$CONTAINER_ENGINE" "$@"`). In bash's normal mode, `VAR=x f` exports `VAR` to the function's children only while it runs. I checked this live: `X=secret f env` shows `X`, and `X` is unset afterwards. The launcher is `#!/bin/bash` with no `set -o posix`. The function calls the engine directly with nothing in between, so the token reaches podman and nothing else.
   - The three runs are `claude-yolo:3888` (session), `lib/token-management.bash:1117` (`validate_token`) and `:1257` (`claude setup-token`).
   - Headless, Quick Launch replay and teams all go through the single run at 3888. Both tokens are passed by name with `-e` (3902-3903), and the in-container entrypoint still finds `GH_TOKEN` (`entrypoint.sh:31,67`).
   - Restart relaunch (exit 75) behaves as before: `unset` at 3839, then the relaunch gets `GH_TOKEN` again from gh.
   - I found no other way out:
     - The only plain `export` hits on the token variables are the three-name `export -n` at line 23; no `declare -x`, `local -x` or `set -a` anywhere.
     - The other `GH_TOKEN=` assignments in `ssh-handling.bash` (1650, 1776) are plain, unexported assignments.
     - The prefixed `gh` calls (355, 1339) go to gh, which reads the token.
     - `.last-launch.conf` (`claude-yolo:531`) holds the token's name, not its value.
     - Compose never reliably had the token before this change, so nothing is lost there.
     - The SSH passphrase file and askpass helper are passed as file paths and env-prefix assignments to ssh-add only, and this diff does not touch them.

4. **The draft PLAN text contradicts itself.** `untracked/scratch/task-4.4b-md-edits.txt` says "The restart relaunch's `unset` of the tokens went with the exports." Its last bullet says "The restart relaunch still unsets both tokens (defence…)". The code (`claude-yolo:3839`) still unsets them. Delete the first sentence.

5. **The new comment at `claude-yolo:3834-3838` is partly wrong.** It says "the relaunch re-derives them from the --token file". Only the Claude token comes from that file; `GH_TOKEN` comes from gh again (see finding 1).

6. **The guide's advice can't work from inside the container.** `CCY-GUIDE.txt:182` says "ask Claude to read the printed log". That log is in the host's `${TMPDIR:-/tmp}` (`claude-yolo:2226`), and the container has no `/tmp` or `TMPDIR` mount (I searched for one). The rest of the guide text is accurate (mktemp name, last 50 lines, an existing `--prompt` is kept, base image used).

7. **One small harness gap.** The check on which programs get a secret covers the recorded fakes and the quiet fakes (`fake_test.go` `refuseUnrecordedSecret`). The "pure tools that run for real" are not checked. A future `export` would still be caught via getenforce or podman, so this is low risk.

**Checks that held**

8. **`lib/host-paths.bash` (lines 26-46) is correct.**
   - A relative path is refused before it is resolved. `realpath -m` follows symlinks and `..`, and the resolved path is what gets checked and stored.
   - `"$default"` is quoted in the case pattern, so it matches literally.
   - `/dev/dri/` and `/dev/dri` are accepted as the default.
   - `/dev/dri/../shm`, `/dev/dri/renderD128`, `/proc/self`, `/sys/class`, `//`, and a symlink pointing into `/dev` are all refused.
   - With absent-ok, the parent of the resolved path is what is checked.
   - The harness sandbox is still allowed (the Go tests pass).
   - `/etc` and `/home` are still accepted; that is in line with the stated scope.

9. **Version bumps are consistent.** `CCY_VERSION` is 3.99.13.15. The Dockerfile label and `REQUIRED_CONTAINER_VERSION` are both 2.78.12. The bump is justified because the Dockerfile copies the guide in (`Dockerfile:374`).

10. **The bash-limits allowlist only shrank** (claude-yolo 2809→2808 / 2740→2739, token-management 1389→1386), and `qa-bash-limits` reports BASH-LIMITS-OK.

11. **The three edited suites still check line order against the real run line.** `test-ccy-git-signing`, `-restart-request` and `-teams` each find that line with `grep -F` on its new prefixed form, which matches exactly one line.

12. **The meta-deploy entry is accurate** (one rebuild; "gh must work inside it"), and it contains no install-specific names. It could add one line saying an exported `GH_TOKEN` no longer reaches interactive sessions, depending on how finding 1 is resolved.

**What I ran (all passed)**
- `go test -race -count=1 ./internal/ccylaunch/...`
- `test-ccy-gpu-device.bash`: 60 of 60
- `test-ccy-git-signing.bash`: 76
- `test-ccy-restart-request.bash`: 180
- `test-ccy-teams.bash`: 221
- `qa-changed.bash --base integration/261021-membership`
- `qa-bash-limits.bash`

I changed nothing in the worktree. My scratch logs are under `/workspace/untracked/scratch/rv-*.log`.