# QA review: Plan 00163, branch at b37ec27a (on ba22402c)

_Copied by the builder from the reviewer's reply. The reviewer cannot write files._

**Verdict**: FIX-BEFORE-MERGE. Finding 1 is close to BLOCK: it breaks the fail-fast rule on
the headless, restart and restore paths.

## Should block the merge

1. **The "no prompt needed" check and the launcher decide differently when more than one key is selected. U20 sides with the check, not with ccy.**
   - `ccy_restart_keys_unattended` (`restart-request.bash:400`) accepts each key on its own when the agent holds it.
   - U20 (`_acceptance-u20.inc.bash:238,246`) does the same, one key at a time.
   - `ccy_agent_filter_start` (`ssh-handling.bash:1073`) only forwards when exactly one key is selected.
   - Reproduced with a real agent, selecting a passphrase key the agent holds plus a key with no passphrase:
     - the check returns `rc=0`;
     - the filter start returns `rc=0` with `PID=[] KEY=[]`, so no filter runs.
   - So the headless check (`claude-yolo:1219`), the restart check (`claude-yolo:3539`), the relaunch recheck (`claude-yolo:1268`) and U20 all pass.
   - The encrypted key file is then mounted, and `entrypoint.sh:131` runs `ssh-add "$key"`: the very prompt these checks exist to prevent. Before this change, that case was refused.
   - **Fix:** either one check over the whole key list, called by the check, by `ccy_agent_filter_start` and by U20, or forward several keys (the filter already takes repeated `--allow`).
   - Add a test for the [held passphrase key, key with no passphrase] case in `test-ccy-restart-request.bash`.

## Should fix

2. **A wrong `.pub` beside the key can make ccy forward a different key.**

   - `ssh_key_fingerprint` (`ssh-handling.bash:1035`) runs `ssh-keygen -lf <key>`. When a `.pub` sits beside the key, ssh-keygen reads that and ignores the private file.
   - Probe: key A with B's public half saved as `A.pub` gives B's fingerprint for both.
   - So selecting A forwards B if the agent holds B, and the container pushes and signs as B. `_agent_public_for_key` (`:1144`) also names B in the signing config.
   - With a mounted file this could not happen, because the private file itself authenticates.
   - `docs/ccy.md:1299` and the changelog say the `.pub` is read first, but nothing checks that it matches.
   - **Fix:** take the fingerprint from the public half stored inside the private key file. When a `.pub` exists, require it to match and refuse if it does not.

3. **The filter is not covered by ccy's version-bump and hash checks.**

   - It is deployed to `/var/local/claude-yolo/lib/ssh_agent_filter.py`.
   - `CCY_HASH` (`claude-yolo:108-117`) hashes only `lib/*.bash`.
   - The pre-commit version-bump check (`scripts/git-hooks/pre-commit:129`) matches only the launcher and `lib/*.bash`.
   - The source is in `helpers/ssh_agent_filter/`, which neither covers.
   - So a future change to this security boundary would ship with no `CCY_VERSION` bump and an unchanged hash. This repeats the "names one file by path" problem in AgentNotes (Plan 00081).
   - **Fix:** cover `helpers/ssh_agent_filter/*.py` in the pre-commit check and `lib/*.py` in the `CCY_HASH` file search, and update `.claude/rules/ccy-version-bump.md` to match.

## Nits

4. **No limit on clients and no idle timeout** (`ssh_agent_filter.py:272`): one thread per client with `settimeout(None)`. A container can open unlimited connections, and a client that sends half a header holds its thread forever.
5. **An identities request is relayed with whatever trailing bytes it carries** (`:85`).
6. **`_parent_alive` treats `PermissionError` as the launcher still being alive** (`:218`). If another user's process reuses the pid, the filter keeps running.
7. **`_agent_public_for_key` skips any line it cannot parse** (`… || continue`, around `ssh-handling.bash:1150`).
   - The real failure then shows up as a misleading "does not offer" refusal.

## Not a finding: probe on the host in Task 3.2

Untested: whether Fedora's `gcr-ssh-agent` lists keys from `~/.ssh` that it has not actually
loaded. If it does, `ccy_agent_forwards_key` would say yes for keys that still need a GUI unlock.

## Checked and found clean

- **Filter protocol**
  - Only an identities request, or a sign request for an allowed fingerprint, reaches the real agent. Every other message gets FAILURE: add, remove, remove-all, lock, unlock, smartcard and extensions, session-bind included.
  - Framing is bounded to 1..256 KiB.
  - A sign request must have exact key/data/flags framing.
  - The identities answer is re-parsed, with a trailing-bytes check.
  - If the real agent has gone, the filter answers FAILURE and reconnects on the next request.
- **Socket permissions and log**
  - The directory is 0700 (`mktemp -d`).
  - The socket is bound under umask 0177, chmod 0600, then hard-linked into place atomically, and never replaces an existing path.
  - The log holds message types and paths only, no key material.
- **Lifetime**
  - The container runs in the launcher's foreground.
  - The early EXIT trap and `cleanup` stop the filter, and the restart path runs `cleanup` before `exec`.
  - After a SIGKILL of the launcher, the parent-pid check stops the filter within 1 second.
- **Fingerprint matching** compares SHA256 of the key blob, never the comment. The only way to get the wrong key is finding 2.
- **Commit signing**
  - With the filter socket, the signing config names `key::<public half the filter offers>`.
  - A real commit signs and verifies.
  - A key the filter does not offer is refused, with a message to `ssh-add` it.
- **Versions:** `CCY_VERSION` is 3.87.0 and the library header is 1.8.0. No file that goes into the image changed, so the container version rightly stays at 2.48.
- **Placement:** the existing shared-library loop in `play-claude-yolo.yml` installs the filter. There is no new playbook. Plan 00161's `deploy.bash`, already in `PLANS`, runs that play.
- **Docs:** `--help`, `docs/ccy.md` and the changelog are updated.
- **Plan:** tasks 1.1–2.4 are ticked and 3.1/3.2 are open. The journal holds the lifetime decision and a handoff.
- **Public repo:** scanned for paths, emails, IPs and usernames. Nothing found.

## Checks that were run

- **qa-all.bash** (in the worktree): every leg except two passed.
  - ansible-syntax and js failed only because the worktree has no `vault-pass.secret` and no `extensions/node_modules`.
  - The coordinator should re-run qa-all on the merged tree.
- **play-claude-yolo.yml syntax-check** with the main checkout's vault file: passed.
- **Helper tests:** 119 of 119 modules, 3686 tests, OK.
- **ccy suites:**
  - `test-ccy-ssh-handling`: 90 passed
  - `test-ccy-git-signing`: 75 passed
  - `test-ccy-restart-request`: 166 passed
  - `test-ccy-restore-askpass`: 89 passed
- **plan-qa sweep:** 0 blocking findings.
