VERDICT: MERGE

The round-3 fixes hold. I found no blocking defects, only three small non-blocking ones. I changed nothing in the worktree, which is still clean at fb17dd7e.

**What I ran**
- `scripts/test-ccy-token-handoff.bash`: 91 passed.
- `go test ./internal/ccylaunch/...`: ok.
- `./scripts/qa-changed.bash --base 12087b38`: exit 0. Log: `/workspace/untracked/scratch/rv4-qa-changed.log`.
- My own attack script against the real library: `/workspace/untracked/scratch/rv4-handoff-probe.bash`.

**Checks that held**

1. **(1) Path shape is checked before anything touches the file** (`files/var/local/claude-yolo/lib/token-handoff.bash:33-39`). Taking the last two parts off a path is only string work. The path must pass both the base comparison and the anchored pattern `^/…/ccy-gh-token.XXXXXX/token$`, and the `||` chain stops before either `find` runs if it does not. Each candidate base is resolved with `cd -P && pwd`, so it can only match a clean absolute path. My probe refused all of these and left the file in place: `//tmp/…`, `/tmp//…`, `/./tmp/…`, `/tmp/./…`, `/tmp/x/../…`, a trailing `/`, a relative `ccy-gh-token.X/token`, and a path under a base not on the list.

2. **(3) The ownership and mode checks apply to every accepted base, /tmp included.** Under /tmp the probe refused, unread and left in place:
   - a directory owned by another uid (65534)
   - a file owned by another uid
   - a symlinked directory
   - a symlinked file
   - a 0755 directory

   On a sticky /tmp, another user can only make directories that fail the owner check. A process running as the same user could already read the token, so accepting four bases gives it nothing new.

3. **(2) Empty file.** It prints an error and is removed. Nothing is removed until all the shape and ownership checks have passed (`:45-47`). When the directory holds a second file, only `token` goes, `rmdir` fails out loud, and the second file stays.

4. **(4)(5) and the paperwork.**
   - The comments, changelog, `QA.md` row, `PLAN.md` notes and `meta-deploy.list` all match what the code does.
   - The `--debug` notice and the changelog now say only tokens with a known prefix are masked, and that the terminal shows the raw trace.
   - The trap sentence in `PLAN.md` is corrected.
   - CCY is 3.99.13.18 and the container stays 2.78.13 (the image is unchanged), with a changelog entry.
   - The allowlist only shrank: claude-yolo went from 2802/2735 to 2801/2733.
   - The review copy has every word of the original. The only difference is the formatter renumbering "8." and "9." as "08." and "09.".
   - No install-specific names appear anywhere.

**Not blocking**

5. **`/run/user/<uid>` and every `-user` check use `$EUID`, not `id -u`.** Bash 5.2 takes `EUID` from the environment when it is exported: `env EUID=5 bash -c 'declare -p EUID'` gives `declare -x EUID="5"`, not read-only. My probe set `EUID=65534` and a `TMPDIR`, and the library accepted another user's directory: `rc=0 GH_TOKEN=TOK` (`:34,38,39`). This needs someone who can already set the victim's environment, which is the same-user threat model, and round 2 already used `$EUID` the same way. Hardening would be `uid="$(id -u)"` once, used in all three places.

6. **A file holding only a newline is removed with no message.** `read` succeeds with an empty value, so the "is empty" message is skipped, and the function then returns 1 silently (`:45-48`). The writer never produces this, but making `:45-46` test the value rather than `read`'s exit status would cover it.

7. **Minor wording.** The changelog says a base "is still refused, with an error naming the variable". In fact the error lists all four base names generically and does not say which one was missing.