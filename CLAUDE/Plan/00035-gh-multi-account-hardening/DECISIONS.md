# Plan 00035: Root Cause, Code Map, Decisions and Risks

Supporting document for [PLAN.md](PLAN.md). Extracted from the original plan
prose, preserved in full in [PLAN_archive.md](PLAN_archive.md).

## Root cause summary

1. **SSH probe bug in playbook** — `play-github-cli-multi.yml` (initial probe
   and re-probe) ran `ssh -o IdentitiesOnly=yes -i <key>` without
   `-F /dev/null` or `-o IdentityAgent=none`. The user's `~/.ssh/config` has a
   default `Host github.com / IdentityFile ~/.ssh/id / IdentitiesOnly yes`
   block, so SSH offers BOTH the `-i` key AND `~/.ssh/id`. Whichever GitHub
   accepts wins. Since `~/.ssh/id` is registered to LTSCommerce, every probe
   returned `Hi LTSCommerce!` regardless of which `github_<alias>` key was
   specified. Verified in strict isolation (`-F /dev/null -o IdentityAgent=none`):
   all four newly-regenerated local keys returned `Permission denied`,
   confirming none were registered on GitHub at all.
2. **Same bug in ccy** — `files/var/local/claude-yolo/lib/ssh-handling.bash`
   had an identical SSH command missing the same isolation flags. This produced
   the `Expected: LTSCommerce / Got: <gh-username-b>` entrypoint mismatch in an
   unrelated project (user picked `github_<alias-b>`; SSH probe fell through to
   `~/.ssh/id` and reported LTSCommerce; alias-extracted `gh-token-<alias-b>`
   returned <gh-username-b>'s token; container's `gh api user` returned
   <gh-username-b>; mismatch).
3. **Weak assertion** — the playbook asserted only
   `'successfully authenticated' in item.stdout`. That is true when the probe
   falls through to `~/.ssh/id`, so the assertion rubber-stamps the broken state.
4. **Manual paste** — the playbook used `pause:` to show pubkeys and ask the
   user to paste them into `github.com/settings/keys`. Because the SSH probe
   was buggy (1+3), the paste step rarely even ran: `ssh_keys_to_add` was
   usually `[]` due to the false positives.
5. **Scope preconditions undocumented** — `gh ssh-key add` requires
   `admin:public_key` scope. Two of the four accounts (`<gh-username-a>`,
   `joseph-uk`) did not have it. Manual remediation ran
   `gh auth switch --user <X> && gh auth refresh --hostname github.com --scopes admin:public_key`
   for each. That work belongs in the baseline setup, not reactive.
6. **Key regeneration trigger unknown** — `~/.ssh/github_*` keys all had mtime
   `2026-04-23 16:43`. User is certain they did not manually `rm` them. Neither
   `run.bash` nor `play-github-cli-multi.yml` contains an explicit `rm`.
   Something during the F43 upgrade caused regeneration (possibly their absence
   at the point `run.bash` checked `if [[ ! -f "$_key_private" ]]`). Still
   needs investigation (Phase 1).

## Relevant code locations

Line numbers are as of plan creation (2026-04-24) and will have drifted.

- `run.bash:522-545` — single-account `gh auth login` (primary only)
- `run.bash:573-587` — primary SSH key upload to whichever account is active
  (uses `gh-lts` wrapper if available)
- `run.bash:835-902` — per-account SSH key generation from `github_accounts`
  dict (since replaced by a call to `scripts/gh-account-setup.bash --setup-all`)
- `playbooks/imports/play-github-cli-multi.yml:201-296` — SSH probe, key
  "needs to be added" classification, manual paste prompt, re-probe, assertion
  (the block being rewritten)
- `files/var/local/claude-yolo/entrypoint.sh:39-51` — ccy's post-auth mismatch
  check (the detector that finally surfaced the bug)
- `files/var/local/claude-yolo/lib/ssh-handling.bash:111-189` — ccy's
  SSH-username probe (fixed in Phase 5)

## Prior related work

- Plan 034 — `config_github_account` tracking in `localhost.yml`. Overlaps
  conceptually (both are about making `run.bash` robust across multi-account
  setups). Not a blocker, but worth coordinating.

## Decision 1: Programmatic `gh ssh-key add` vs. manual paste

**Context**: The playbook asked the user to manually paste pubkeys into
`github.com/settings/keys`. This is the step that failed silently (the pause
prompt was skipped due to the SSH probe false positive, so no paste prompt ever
appeared).

**Options**:

1. Fix the probe, keep manual paste — minimal change, but still fragile (wrong
   tab, wrong browser session).
2. Fix the probe AND switch to `gh ssh-key add` — larger change, but eliminates
   the manual step entirely.

**Decision**: Option 2. The manual step has no real upside once gh is set up
multi-account anyway.

**Date**: 2026-04-24

## Decision 2: Fresh-install ordering — gh multi-account before SSH keys

**Context**: Original order was: gh primary auth, SSH keygen (via `run.bash`),
playbook runs SSH probe, playbook asks user to paste pubkeys. The last step has
no working per-account gh, so cannot automate.

**Options**:

1. Keep current order, add manual paste-then-gh-authenticate per account.
2. Reorder: gh primary, gh multi-account (with scopes), SSH keygen, playbook
   uploads pubkeys via `gh ssh-key add`.

**Decision**: Option 2. Once we commit to `gh ssh-key add`, getting gh working
across all accounts first is the precondition. Aligns with the user's stated
preference: "gh working first, then SSH keys become easy".

**Date**: 2026-04-24

## Decision 3: Standalone `gh-account-setup.bash` script

**Context**: Adding a new GitHub account required the user to know three
separate manual steps in the right order: (1) run `gh auth login` (which shows
a confusing SSH key upload prompt), (2) edit `localhost.yml`, (3) run the
playbook (which pauses for manual SSH key paste). The user wants a single
command: add the account to config, run one script, done.

**Options**:

1. Keep auth/keygen/upload split across `run.bash` + playbook; fix the UX
   within those files.
2. Extract a standalone `scripts/gh-account-setup.bash` that handles the full
   per-account flow (gh auth, keygen, `gh ssh-key add`, SSH test), callable
   from `run.bash` AND standalone via `--add=alias:username`.

**Decision**: Option 2. A standalone script:

- Gives users a single command for adding accounts post-install
- Lets `run.bash` delegate instead of inlining SSH key logic
- Lets the playbook stay declarative (deploy config/aliases only)
- Uses `gh auth login --hostname github.com --git-protocol ssh --web`, which
  skips the confusing SSH key upload prompt entirely
- Consolidates Phases 2 + 4 into one coherent flow

**Date**: 2026-04-28

## Decision 4: Scope audit lives in the playbook, not `run.bash`

The per-account scope audit was originally scoped for `run.bash` but was
implemented in `play-github-cli-multi.yml`. Reasons: (a) the playbook already
iterates `github_accounts`, (b) it runs on every deploy so it catches drift if a
scope gets revoked or a new scope is added to the canonical list, (c) `run.bash`
already invokes the playbook, so putting it in both is duplication. The audit
switches to each account, reads its scopes, restores the originally-active
account, and fail-fasts with the exact
`gh auth switch && gh auth refresh --scopes <missing>` command per account.

The canonical scope list is `github_required_scopes` at the top of the playbook:
`admin:public_key, gist, project, read:org, read:project, repo, user:email`.
`project` and `read:project` enable GitHub Projects v2 access (IDE Projects
integration and `gh project` CLI). `admin:org` is deferred. The
`gh-{alias}-token-phpstorm` bash function templates its `required_scopes` array
from the same variable, so there is a single source of truth.

The `run.bash` `admin:public_key` check for the primary account is left in
place: it gives fast feedback before the playbook runs.

**Date**: 2026-04-27

## The shipped `gh-account-setup.bash` design

Three modes: `--add=alias:username` (append to `localhost.yml` + full setup),
`--setup-all` (all accounts from `localhost.yml`, called by `run.bash`),
`--check` (read-only health verification).

Per-account flow: (1) `gh auth login --web` if not authenticated, (2) scope
audit via the `X-Oauth-Scopes` header + `gh auth refresh` if missing, (3) SSH
key generation if missing, (4) `gh ssh-key add` if not registered on GitHub,
(5) isolated SSH test (`-F /dev/null -o IdentityAgent=none`) to verify the
correct account. Passphrase is passed via `GITHUB_SSH_PASSPHRASE` if already in
memory, otherwise the script decrypts it from vault itself.

## Risks and mitigations

| Risk                                                                                              | Impact                                             | Mitigation                                                                                                                            |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `gh auth refresh` scope flow can't be driven non-interactively                                    | High — blocks automation of first-time scope grant | Fail fast in the playbook with the exact command; do it once per account during fresh-install so the playbook rarely has to re-prompt |
| GitHub rejects programmatic key upload due to org SSO enforcement                                 | Medium — would affect LTS/<org-b> accounts         | Detect in the preflight; fall back to paste-and-confirm prompt with link to SSO authorisation page                                    |
| Key regeneration trigger is in a shared component we don't control (gnome-keyring, F43 migration) | Medium — can't fix, only work around               | Phase 1 research output will inform whether we need defensive copies or just a clearer recovery runbook                               |
| Signed-commits research says "do it" but implementation is larger than anticipated                | Low — it's spun off into its own plan              | Keep this plan's scope tight: research only, implementation in a follow-up plan                                                       |

## Open observations parked here

- Post-auth SSH hang observed on `-i github_<alias-c> -o IdentitiesOnly=yes`
  probe; out of scope for this plan (suspected VPN/MTU).
- `.claude/hooks/handlers/pre_tool_use/system_paths.py` was patched to exempt
  `$CLAUDE_PROJECT_DIR` (derived from the handler's own `__file__`), otherwise
  the handler blocked edits to the project itself when running on the host.
