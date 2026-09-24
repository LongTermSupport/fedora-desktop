# Plan 00139: implementation fork report

Tasks 1.1, 1.2, 2.1, 2.2, 3.1, 3.2 and 4.1 are done. Tasks 1.3 and 4.3 are in progress:
their code and docs are written, and they wait on the host and the owner. Tasks 4.2
(the owner's call) and 4.4 (the qa-reviewer) were not in scope.

## What changed

- `playbooks/imports/play-git-configure-and-tools.yml`:
  - Generates `~/.ssh/id_ed25519_git_signing` with no passphrase unless it exists
    (`ssh-keygen` with `creates`; community.crypto is not a dependency). The default path
    is overridable with `git_signing_key`, through `git_signing_key_path`.
  - Asserts the key is a 0600 regular file with its `.pub` beside it.
  - Asserts the key loads with an empty passphrase. This is the old assert inverted.
  - Sets `gpg.format`, `user.signingkey`, `commit.gpgsign` and `tag.gpgsign` in the global
    config with `community.general.git_config`.
  - Removes `gpg.format`, `user.signingkey` and `alias.sign-deploy` from the XDG config.
- `files/var/local/claude-yolo/lib/ssh-handling.bash` 1.5.0 has the new
  `stage_git_signing_key`.
- `claude-yolo` calls it right after `trap cleanup EXIT`, and CCY is now 3.66.0.
  `docs/ccy-changelog.md` has the entry.
- `scripts/test-ccy-git-signing.bash` is a new gate. It is wired into `qa-all.bash` as
  `ccy-git-signing` and listed in `CLAUDE/QA.md`.
- `helpers/self_update/update.py`: the refusal no longer tells the owner to run
  `git sign-deploy`.
- Docs: `docs/configuration.md` "Commit Signing" and the self-update "Trust" and
  "Pausing" bullets, `docs/playbooks.md`, `docs/ccy.md`, and `localhost.yml.dist`.
- Plan 00137: PLAN D3 is amended as overruled, pointing at this plan's D4.
  `DESIGN-cycle.md` has its allowed-signers row updated. `deploy.bash` has its header and
  NEXT text updated.
- This plan: `deploy.bash` runs git-configure, then claude-yolo. `acceptance.bash` has 11
  declared checks. PLAN.md ticks are updated.

## Decisions made here, with reasons

1. **Registering the key on GitHub is an owner step (Task 1.3).**
   - `gh ssh-key add --type signing` needs `admin:ssh_signing_key`. Adding that to
     `vars/github-required-scopes.yml` would fail the scope audit in
     `play-github-cli-multi.yml` for every account until each was refreshed.
   - The key should sit on one account only: the one whose verified email is
     `user_email`, since GitHub verifies by committer email. Choosing that account needs
     an API lookup, and by this repo's rules that logic would have to become a helper.
   - Acceptance uses the public `users/<login>/ssh_signing_keys` endpoint, for every login
     `gh` has. That needs no scope.
2. **The key is staged into `CONFIG_TEMP`, not given a new mount.**
   - That directory is already mounted read-only at `/tmp/claude-config-import`, with a
     private relabel where SELinux needs one.
   - It is a fresh 0700 `mktemp` directory, deleted by the launcher's EXIT trap.
   - The container runs in the foreground under the launcher, so the key lives exactly
     as long as the session.
3. **Staging does not depend on `--no-ssh`.** The signing key cannot push anything, and a
   container with signing on and no key would fail every commit.
4. **A launch is refused when signing is on and the key is unusable.** That covers a
   missing file, no `user.signingkey`, a non-SSH format, and a literal `key::` public key.
   With signing off, an unusable key setup is left alone.
5. **The helper's code is unchanged.** `gate.judge` already treats a signature from an
   unknown key as UNTRUSTED, so it is passed over rather than refused. Commits signed by
   another machine's key therefore do not block the cycle.

## Evidence

- `test-ccy-git-signing.bash` passes 19 of 19. Against the pre-change library it stops
  with "stage_git_signing_key is not defined".
- `test-ccy-ssh-handling.bash` passes 44 of 44, and `test-self-update-cycle.bash` passes
  163 of 163. `tests.helpers.self_update` ran 73 unit tests, all OK.
- A smoke test with a throwaway key under an isolated HOME, git 2.39.5:
  - with `commit.gpgsign` and `tag.gpgsign` on, a plain commit and a plain `tag -m` are
    both signed;
  - both verify with `gpg.ssh.allowedSignersFile`, which is how acceptance checks 9 and
    10 work;
  - an allowed-signers file holding a different key refuses them.
- shellcheck at warning level is clean on every changed bash file. The patterns
  (semgrep) gate passes on all 318 files, after one finding in the new gate was fixed.

## What `qa-all.bash` could not run here, and why

These three failures are environmental, and all of them come from the worktree:

- **ruff:** the pin, from a later `.qa-versions` merge now on F44, asks for 0.16.8, and
  this container has 0.16.4. The container needs a rebuild. The coordinator's checkout
  will see the same failure.
- **ansible-syntax:** every playbook fails, 82 of 82. The worktree has no
  `vault-pass.secret`, which is gitignored, so this play has never had a syntax check.
  The YAML parses.
- **eslint:** `extensions/node_modules` is absent in the worktree, so `qa-all.bash`
  aborts at the js gate before the later gates run. Those gates were run on their own
  above.

The coordinator should re-run `qa-all.bash` in the main checkout after merging, where the
vault file and the extension tooling exist.

## Things the owner needs to know

1. **Every push from the desktop is now a release** to a self-updating server trusting
   that key. `git sign-deploy` is gone. The only pause is `self_update_enabled: false`.
   This follows from the owner's ruling, but it changes how releases work day to day.
2. **An existing passphrase key stops the play.** If host_vars still sets
   `git_signing_key` to the passphrase-protected key Plan 00137 documented, the play fails
   at the passphrase assert. The remedy is in the message: remove `git_signing_key`, let
   the play generate the default key, register it with GitHub, and put its `.pub` in the
   server's `self_update_signing_public_key`.
3. **Signing is on by default for every install of this public repo.** Anyone who does
   not register the key sees their commits as Unverified on GitHub.
4. **A server profile gets its own key.** The play is `hosts: desktop` with scope
   `general`, so it also runs on a server, which gets its own machine key and signs its
   own commits. The server's gate treats those as untrusted. They are passed over, not
   refused.
5. **When signing starts:**
   - **Host `cc` sessions:** at once, because they read `~/.gitconfig` live.
   - **ccy sessions already running:** only after a restart.
   - **Leg order:** `deploy.bash` runs git-configure before claude-yolo. A pre-3.66
     launcher would start containers whose copied config names a host key path.
6. **An untested edge case (host run).** `community.general.git_config` with
   `state: absent` against a missing `~/.config/git/config` is expected to be a no-op
   (`git config --file <missing> --unset-all` exits 5, "not set"). This has not been seen
   on a host.

## Journal bodies (for the coordinator to file)

- **decision, Task 1.3:** text as in decision 1 above.
- **action, implementation:** the "What changed" list, and the evidence section.
