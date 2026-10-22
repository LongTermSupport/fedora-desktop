VERDICT: FIX-FIRST

The hand-off fixes round 1's finding 1 for the paths it targets. The tmux cross-session leak stays closed and the custom-Dockerfile re-runs don't widen the narrowing. One defect blocks: the inner launcher reads and then deletes any file the variable names, with no check on where that file is. One claim in the comments and commit is false (finding 2).

**Blocking**

1. **`CCY_GH_TOKEN_HANDOFF` can name any file the user owns: the launcher reads it into GH_TOKEN, deletes it, and may send its contents off the host.** `lib/token-handoff.bash:23-28` only checks that the path is not a symlink, is a regular file and is owned by the user. Then it does `GH_TOKEN="$(<"$file")"; rm -f -- "$file" && rmdir -- "$(dirname "$file")"`. It runs on every launch at load (`:64`), the outer one included, so a stale or copied value in a shell is enough. I tested this in a scratch directory, `/workspace/untracked/scratch/rv2-handoff-poc`:
   - **File alone in its directory:** `GH_TOKEN=PRIVATE-KEY`, the file and its directory were deleted, and the launch carried on. From there the contents go to `resolve_token_owner_login` (`lib/ssh-handling.bash:1735`, a GitHub API call with that value as the token, made when an account name is known) and into the container as `-e GH_TOKEN`.
   - **File with other files beside it:** the file was deleted anyway. Only the `rmdir` failed, then `exit 1`.
   - **How bad:** someone who can set the user's environment gains nothing over the user. The danger is to the user's own files: `ps` and `#{pane_start_command}` show this variable, so it is easy to copy into a shell, and doing so deletes a file without being asked. The value can also be sent somewhere the user never meant it to go.
   - **Fix:** in `ccy_token_handoff_take`, accept only `"${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}"/ccy-gh-token.??????/token`, and check that its directory is not a link, is the user's and is mode 700 before reading. Add a refusal case to `scripts/test-ccy-token-handoff.bash`, for example a file owned by the user outside the runtime directory, which must survive.

**Not blocking**

2. **On the normal path the outer launcher's EXIT trap never runs.** `lib/tmux-session.bash:427` ends with `exec tmux … attach-session`, and `exec` discards the trap. The trap does run on the quit path, on an attach to an existing session, and on give/take when no tmux wrapping happens (the tests cover these, `:132-154`).
   - So the comments at `token-handoff.bash:10` and `:32`, and the commit's "the outer EXIT trap removes a file never taken", are wrong for a new session.
   - If the inner launcher dies before it loads the hand-off library (the missing-library exit at `claude-yolo:45-50`, or `common.bash` failing), the token file stays until logout, or until reboot when it falls back to TMPDIR.
   - The upside is that the race you asked about cannot happen: nothing removes the file before the inner launcher reads it.
   - Fix the comments, or have the inner side or the trampoline clean up.

3. **TOCTOU (check-then-read swap).** Bash has no O_NOFOLLOW, so the `-L` check and the read can be raced. With the real path, though, the directory is a fresh `mktemp -d`, mode 0700, owned by the user, so only a process running as the same user could swap the file. Such a process can read the token anyway, and the path is not guessable. Once finding 1's check on the directory is in place this does not matter.

4. **What other processes can see.** Only the path appears in `ps` and in tmux's `pane_start_command`, and it points to a mode-0700 directory that is gone once the token is read. Containers never mount the whole `XDG_RUNTIME_DIR` (`claude-yolo:3560`, which mounts only the Wayland socket and the membership subdirectory), so another team's session cannot reach the file. The tmux leak stays closed: the variable goes on the `env` in the pane's command, never into tmux's environment, and the inner launcher unsets it (`:21`) before it starts anything. The test confirms no child process has it (`test:124-127`).

5. **`GH_TOKEN="${GH_TOKEN:-}" exec` in the custom-Dockerfile modes is fine** (`lib/dockerfile-custom.bash:49,168,730,773`). The value goes only to the launcher being started. That launcher un-exports it at line 23 before running anything, and these modes run at `claude-yolo:1157/1163`, so the new launcher then goes through the hand-off normally. An empty value is treated as no token (`ssh-handling:1720` tests `-n`).

6. **`--debug` writes the token into the debug log in plain text.** `set -x` comes first (`claude-yolo:1074`), so the trace records the token at `token-handoff.bash:42` (the `printf` that writes the file) and `:27` (the read). The log is created in `${TMPDIR:-/tmp}` with the default umask. This was already the case before the branch (`ssh-handling:1720` `[ -n "$GH_TOKEN" ]` is traced too), so it is not new, but it is now worth fixing: turn tracing off around the token lines, or create the log with mode 600.

**Checks that held**

7. **Versions:** CCY is 3.99.13.16; `REQUIRED_CONTAINER_VERSION` and the Dockerfile label are both 2.78.13. The bump is justified because the guide is copied into the image. The changelog entries for 3.99.13.15 and 3.99.13.16 are present and state the tmux leak.

8. **Allowlist and size limits:** the allowlist only shrank (claude-yolo 2809→2804 / 2740→2738, token-management 1389→1386). The new `token-handoff.bash` is within the limits (4 functions).

9. **Test wiring:** the `ccy-token-handoff` row in `qa-gates.tsv` and its `addedAfterPort` entry are legitimate. The suite's 27 checks drive the launcher's real entry point. `meta-deploy.list` is accurate and contains nothing specific to an install. I found no install-specific names in the changed files I searched.

**What I ran (all passed)**
- `scripts/test-ccy-token-handoff.bash`: 0 failed
- `go test -race -count=1 ./internal/ccylaunch/...`: ok
- `qa-changed.bash --base <merge-base>`: exit 0

I changed nothing in the worktree. My logs and the test files are in `/workspace/untracked/scratch/rv2-*`.