# QA review: per-account signing keys and the gh scopes single source

Reviewer: `qa-reviewer` (Opus 5.5), diff `5ca8db83..00aa7d6d` on F44. The reviewer is
read-only and cannot write files, so the coordinator saved its returned summary here. The
reviewer's longer prose was not returned.

**Verdict: BLOCK**

## Blocking

1. **ccy can copy any host file the user can read into the container.** See
   `lib/ssh-handling.bash:1191`, `:1213` and `:1229`.

   - The key now comes from `git -C "$project" config --get user.signingkey`, and that
     reads the project's own `.git/config`.
   - The container mounts the project read-write (`claude-yolo:2081`).
   - The only check before `install` is `[ -f "$key" ]`.
   - Reproduced: a project-local `user.signingkey` pointed at a fake `~/.ssh/id` staged
     that file's contents.

   **Fix:** accept only the machine key or `~/.ssh/github_<alias>_signing`, or refuse a
   value whose scope is local, worktree or command. Add a test that must go red.

## Fix before merge

2. **Two account remotes in one repo.** The last alias in `github_accounts` silently wins
   (measured), and an alias remote also beats plain github.com. Neither case is tested.

3. **Whether a commit is Verified depends on its email, which the design ignores.**

   - The machine key goes on the account that holds `id.pub`, not on the account whose
     verified email is `user_email`. The diff deleted the doc that said so.
   - `gh-update-git-config` sets the email only when the account's email is public
     (`play-github-cli-multi.yml:843-847`).

   **Fix:** choose the machine key's account by `user/emails`, and write `user.email`
   into each per-account include.

4. **A streamed `run.bash` with an older existing checkout stops before the pull that
   would fix it.** `gh_scopes_repo` checks only for the vars file, but the helper is
   missing (`run.bash:2437-2448`). The test at `test-run-bash-gh-scopes.bash:176-179`
   asserts that this broken case gets used.

5. **`meta-deploy.bash` does not list 00139 in `PLANS`**, although a host deploy is
   pending. `PLAN.md` hands the owner `./deploy.bash` to type (`AgentNotes.md:61-78`).

6. **An undocumented new requirement.** `github_accounts` must now include the account
   that holds `id.pub`, or the play fails on every run.

7. **The appended `[user]` section breaks the gitconfig copy when `~/.gitconfig` has no
   final newline.** Measured: `bad boolean 'true[user]'`. **Fix:** start the `printf`
   with `\n`.

8. **A stale comment at `run.bash:2606-2610`.** "Clone via SSH… guaranteed" is no longer
   true.

## Nits

- The `ssh://` includeIf form is never tested.
- `register` claims that "a refusal leaves GitHub as it was". That is not so when an
  upload fails partway.
- An empty signing key is reported as a passphrase problem, and no remedy is given.
- The HTTPS URL is hardcoded three times.
- There is an unwrapped line in `docs/ccy.md`.
- The XDG-order comment at `play-git-configure-and-tools.yml:167` is wrong (measured).
- `2>/dev/null` hides an `unshare` failure in the history-search test.
- `PLAN.md` has no entry for this review.

## Checked and clean

- The scope hierarchy, `admin:ssh_signing_key` included.
- `GH_TOKEN` acting with each account's own token.
- Include ordering and idempotency.
- Play order.
- A host with signing off.
- Version bumps: CCY 3.67.0, lib 1.6.0, run.bash 1.26.0.
- Fail-fast, stderr hygiene and public-repo safety.
- Ansible 2.19 traps.

## Gates

- `qa-all.bash`: exit 0, 1111 files.
- `plan-qa --sweep`: 0 blocking findings.
- `--syntax-check` on both plays and on `playbook-main.yml`: rc=0 for all three.
