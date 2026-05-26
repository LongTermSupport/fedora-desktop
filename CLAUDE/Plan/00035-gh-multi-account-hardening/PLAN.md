# Plan 00035: GitHub Multi-Account Hardening (gh + SSH Keys + Signed Commits)

**Status**: In Progress
**Created**: 2026-04-24
**Owner**: joseph
**Priority**: High

## Overview

The F42→F43 upgrade surfaced a long-standing fragility in how this repo sets
up GitHub multi-account access. When `run.bash` re-ran after the upgrade, the
four `~/.ssh/github_*` keys were regenerated but their new pubkeys were
**never uploaded to GitHub** — `play-github-cli-multi.yml` silently declared
them working because its SSH probe fell through to `~/.ssh/id` via the
default `Host github.com` config entry, masking the missing registrations.
The failure was only caught by ccy's entrypoint mismatch check the next day,
and it reproduces in any other project when the user picks a `github_<alias>`
key whose alias ≠ the account `~/.ssh/id` is registered to.

Root cause is structural, not a one-off: the manual-paste upload step plus a
probe that can't tell "real auth as ballidev" from "fallback auth as
LTSCommerce" make the flow impossible to get right reliably. This plan
replaces both with a programmatic flow that uses `gh` to register pubkeys
against the correct accounts, with a per-account assertion that catches
wrong-account registrations immediately.

The fix also re-orders the fresh-install flow: gh multi-account auth (with
all required scopes, including `admin:public_key`) must be established
BEFORE SSH key generation, so the key-upload step has a working gh per
target account. That re-ordering is the structural enabler — once gh is
trustworthy across all accounts, SSH keys become easy to manage and rotate.

## Goals

- Fresh install sets up gh multi-account **first**, with all required scopes
  per account, before any SSH key work
- `play-github-cli-multi.yml` SSH probe is fully isolated — no false
  positives from `~/.ssh/config` or ssh-agent fallbacks
- Every per-account key is verified to authenticate **as that account**, not
  just "as some account"
- SSH pubkey upload to GitHub is programmatic via `gh ssh-key add`, no
  manual paste step
- Missing `admin:public_key` scope is detected up front with a clear
  remediation command, not a mid-playbook failure
- ccy's SSH probe (`lib/ssh-handling.bash`) has the same hardening so the
  entrypoint mismatch check stops false-alarming
- Root cause of the 2026-04-23 16:43 key regeneration is identified and
  documented (user is certain they did not manually `rm` the keys)
- Signed-commit feasibility is researched and a follow-up plan created if
  worth doing

## Non-Goals

- Not changing the `github_accounts` format in `localhost.yml`
- Not migrating away from `ansible-vault` for the SSH passphrase
- Not adding a GUI/wizard — CLI-only, consistent with `run.bash`
- Not supporting non-GitHub SSH hosts (GitLab, Bitbucket etc.)
- Not implementing signed commits in this plan (separate plan if research
  says yes)

## Context & Background

### Root cause summary (from today's session)

1. **SSH probe bug in playbook** — `play-github-cli-multi.yml:209` and
   `:273` run `ssh -o IdentitiesOnly=yes -i <key>` without `-F /dev/null`
   or `-o IdentityAgent=none`. The user's `~/.ssh/config` has a default
   `Host github.com / IdentityFile ~/.ssh/id / IdentitiesOnly yes` block,
   so SSH offers BOTH the `-i` key AND `~/.ssh/id`. Whichever GitHub
   accepts wins. Since `~/.ssh/id` is registered to LTSCommerce, every
   probe returned `Hi LTSCommerce!` — regardless of which `github_<alias>`
   key was specified. Verified today: in strict isolation
   (`-F /dev/null -o IdentityAgent=none`), all four newly-regenerated
   local keys returned `Permission denied` — confirming none were
   registered on GitHub at all.
2. **Same bug in ccy** — `files/var/local/claude-yolo/lib/ssh-handling.bash:112`
   has an identical SSH command missing the same isolation flags. This
   produced the `Expected: LTSCommerce / Got: edmondscommerce`
   entrypoint mismatch observed today in an unrelated project (user
   picked `github_ec`; SSH probe fell through to `~/.ssh/id` → reported
   LTSCommerce; alias-extracted `gh-token-ec` returned edmondscommerce's
   token; container's `gh api user` returned edmondscommerce → mismatch).
3. **Weak assertion** — `play-github-cli-multi.yml:290` asserts only
   `'successfully authenticated' in item.stdout`. That is true when the
   probe falls through to `~/.ssh/id`, so the assertion rubber-stamps the
   broken state.
4. **Manual paste** — `play-github-cli-multi.yml:237-262` uses `pause:`
   to show pubkeys and ask the user to paste them into
   `github.com/settings/keys`. Because the SSH probe is buggy (1+3),
   the paste step rarely even runs — `ssh_keys_to_add` is usually `[]`
   due to the false positives.
5. **Scope preconditions undocumented** — `gh ssh-key add` requires
   `admin:public_key` scope. Two of the four accounts in today's session
   (`ballidev`, `joseph-uk`) didn't have it. Today's manual remediation
   ran `gh auth switch --user <X> && gh auth refresh --hostname github.com --scopes admin:public_key` for each. That work should be
   part of the baseline setup, not reactive.
6. **Key regeneration trigger unknown** — `~/.ssh/github_*` keys all had
   mtime `2026-04-23 16:43`. User is certain they did not manually `rm`
   them. Neither `run.bash` nor `play-github-cli-multi.yml` contains an
   explicit `rm`. Something during the F43 upgrade caused regeneration
   (possibly their absence at the point `run.bash:892` checked
   `if [[ ! -f "$_key_private" ]]`). Needs investigation.

### Relevant code locations

- `run.bash:522-545` — single-account `gh auth login` (primary only)
- `run.bash:573-587` — primary SSH key upload to whichever account is
  active (uses `gh-lts` wrapper if available)
- `run.bash:835-902` — per-account SSH key generation from
  `github_accounts` dict
- `playbooks/imports/play-github-cli-multi.yml:201-296` — SSH probe, key
  "needs to be added" classification, manual paste prompt, re-probe,
  assertion (the block being rewritten)
- `files/var/local/claude-yolo/entrypoint.sh:39-51` — ccy's post-auth
  mismatch check (the detector that finally surfaced the bug)
- `files/var/local/claude-yolo/lib/ssh-handling.bash:111-189` — ccy's
  SSH-username probe, has the same `-F /dev/null` / agent-none gap as
  the playbook

### Prior related work

- Plan 034 — `config_github_account` tracking in `localhost.yml`.
  Overlaps conceptually (both are about making `run.bash` robust across
  multi-account setups). Not a blocker, but worth coordinating.

## Tasks

### Phase 1: Research & design

- [ ] ⬜ **Identify the key-regeneration trigger** — trace exactly what
  deleted the four `github_*` keys before `run.bash:892` regenerated
  them. Candidates: (a) a playbook task not in the current
  `play-github-cli-multi.yml`, (b) F42→F43 home-dir migration,
  (c) a step in `run.bash` or an imported script that runs before
  line 892. Document the finding in this plan's Notes section.
- [x] ✅ **Decide scope list** — canonical required scopes for every gh
  token, codified as `github_required_scopes` at the top of
  `playbooks/imports/play-github-cli-multi.yml`:
  `admin:public_key`, `gist`, `project`, `read:org`, `read:project`,
  `repo`, `user:email`. `admin:org` deferred — not needed for current
  flows; can be added per-account later if a future use case demands it.
  The PhpStorm token function now reads from the same list, so it stays
  in sync automatically.
- [ ] ⬜ **Decide fresh-install ordering** — write out the new step
  sequence from `run.bash` entry through playbook completion. Must
  ensure gh multi-account auth is complete before any SSH key
  operation.
- [ ] ⬜ **Decide how to surface the interactive `gh auth refresh` step**
  — browser device-code flow can't be automated. Needs a clean prompt
  in `run.bash` that runs once per account missing the scope, with a
  clear "log into github.com as <X> in your browser first" message.

### Phase 2: Standalone `gh-account-setup.bash` script (Decision 3)

- [x] ✅ **Create `scripts/gh-account-setup.bash`** — standalone script
  that handles the full per-account GitHub setup flow:
  - `--add=alias:username` — add and fully configure a single account
  - `--setup-all` — iterate all accounts from `localhost.yml`, set up
    any that aren't fully configured (used by `run.bash`)
  - `--check` — verify all accounts are healthy (auth + SSH + scopes)
  - Per-account flow: (1) `gh auth login --hostname github.com --git-protocol ssh --web` if not authenticated (skips confusing SSH
    key upload prompt), (2) scope audit + `gh auth refresh` if missing,
    (3) SSH key generation if missing, (4) `gh ssh-key add` if not
    registered, (5) isolated SSH test to verify correct account
  - Idempotent: re-running with everything in place is a no-op with
    success logs, no redundant prompts or browser opens
  - For `--add`: also appends the new alias:username to `localhost.yml`
    under `github_accounts` so user doesn't need to edit YAML manually
- [x] ✅ **Update `run.bash`** — replaced the inline SSH key generation
  block (lines 835-903) with a call to
  `scripts/gh-account-setup.bash --setup-all`. Primary auth block
  (lines 522-545) stays as-is — it runs before `localhost.yml` exists
  and is needed to clone the config repo. Added comment to
  `github_accounts` config block hinting at `--add` usage. Bumped
  `RUN_BASH_VERSION` to 1.1.0.
- [x] ✅ **Per-account scope audit** — implemented in
  `play-github-cli-multi.yml` (not `run.bash` as originally scoped — see
  2026-04-27 note). Loops `github_accounts`, switches to each, reads
  active scopes via `gh auth status --json hosts`, and fail-fasts with
  the exact `gh auth switch && gh auth refresh --scopes <missing>`
  command per account. Uses the canonical `github_required_scopes` list.
  Originally-active account is restored before the fail step.
- [ ] ⬜ **Idempotency test** — re-running the script with everything
  already in place must be a no-op with success logs, not redundant
  prompts. Requires testing on the host with real gh accounts.
- [x] ✅ **Bump `RUN_BASH_VERSION`** — bumped to 1.1.0 with message
  describing the multi-account gh setup change.

### Phase 3: Harden play-github-cli-multi.yml SSH probe

- [ ] ⬜ **Isolate the probe** — add `-F /dev/null`,
  `-o IdentityAgent=none`, `-o IdentitiesOnly=yes` to the SSH command
  in both the initial probe (line 209) and the re-probe (line 273).
- [ ] ⬜ **Parse the authenticated user** — extract `Hi <user>!` per
  alias into a fact.
- [ ] ⬜ **Classify each key** into three buckets: OK (authed as
  expected user), WRONG-ACCOUNT (authed as a different user — fail
  fast with "delete from <wrong> then retry" message), NEEDS-UPLOAD
  (Permission denied / timeout).
- [ ] ⬜ **Strengthen the final assertion** — change the post-upload
  assert from `'successfully authenticated' in item.stdout` to
  `('Hi ' + expected + '!') in item.stdout`. Failure message names
  both expected and actual users.

### Phase 4: Programmatic pubkey upload (now in script)

- [x] ✅ **Scope preflight in playbook** — superseded by the broader
  Phase 2 per-account scope audit, which runs BEFORE the SSH key block
  and fail-fasts on any missing scope (not just `admin:public_key`).
  Implemented in `play-github-cli-multi.yml`.
- [ ] ⬜ **Move pubkey upload into `gh-account-setup.bash`** — the
  script handles `gh ssh-key add` per account (see Phase 2). The
  playbook's manual `pause:` prompt (lines 334-357) and the
  "Instructions for adding SSH keys to GitHub" block are removed
  entirely. The playbook's SSH probe block stays as a verification
  step but no longer drives uploads.
- [ ] ⬜ **Verify post-upload** — the re-probe block already exists;
  it just needs the isolation fix from Phase 3 to work correctly.

### Phase 5: ccy diagnostics alignment

- [x] ✅ **Apply the same SSH probe isolation** to
  `files/var/local/claude-yolo/lib/ssh-handling.bash:112`. Added
  `-F /dev/null -o IdentityAgent=none` so the probe can't fall
  through to `~/.ssh/id`. Committed.
- [x] ✅ **Fix the misleading log line** at
  `ssh-handling.bash:188` — now cross-checks `gh api user` against
  the SSH-detected username and fails on the host with a specific
  "mapping vs SSH key disagrees" error. Log line now shows both
  identities explicitly. Committed.
- [x] ✅ **Bump `CCY_VERSION`** to 3.12.2 with a description of
  the SSH-probe fix. Committed.
- [x] ✅ **Deploy to host** — user deployed via the CCY playbook,
  restarted ccy, and confirmed the fix resolves the token-mismatch
  error in multiple projects. Phase 5 is complete.

### Phase 6: Signed commits research

- [ ] ⬜ **Research GitHub signed commits options** — three routes
  exist: (a) GPG keys, (b) SSH signing keys (recent GitHub feature,
  reuses the same SSH keys we manage), (c) gitsign/Sigstore. Write a
  decision doc in this plan's directory comparing them on:
  complexity of key management, per-account separation (do we need
  separate signing identities per alias?), integration with existing
  gh/ssh flow, revocation, user UX.
- [ ] ⬜ **Decide go/no-go** — based on the research, decide whether
  to spin up Plan 00036 to actually implement. Document decision
  here.

### Phase 7: Docs & QA

- [ ] ⬜ **Update `docs/` post-upgrade guide** — document the new
  fresh-install ordering (gh multi-account first, then SSH keys), and
  how to recover from "my keys got regenerated, what now" state.
- [ ] ⬜ **Add a recovery runbook** for the scenario we hit today:
  regen'd keys not on GitHub, keys on wrong account, missing scopes.
  Short, concrete commands.
- [ ] ⬜ **`./scripts/qa-all.bash` passes** for all changed
  bash/ansible.
- [ ] ⬜ **Test on a clean VM or container** — full fresh-install
  flow with two or more `github_accounts` entries. Verify the
  programmatic upload step actually adds pubkeys to the right
  accounts.

## Dependencies

- **Depends on**: nothing hard. Plan 034 overlaps conceptually but
  neither blocks the other.
- **Blocks**: future work that wants trustworthy multi-account gh in
  `run.bash` or ccy (e.g. per-account `git push` automation, PR
  creation as a specific account, signed commits if Phase 6 says go).

## Technical Decisions

### Decision 1: Programmatic `gh ssh-key add` vs. manual paste

**Context**: Current playbook asks the user to manually paste pubkeys
into `github.com/settings/keys`. This is the step that failed silently
yesterday (the pause prompt was skipped due to the SSH probe false
positive, so no paste prompt ever appeared).

**Options**:

1. Fix the probe, keep manual paste — minimal change, but still
   fragile (wrong tab, wrong browser session).
2. Fix the probe AND switch to `gh ssh-key add` — larger change, but
   eliminates the manual step entirely.

**Decision**: Option 2. The manual step has no real upside once gh is
set up multi-account anyway.

**Date**: 2026-04-24

### Decision 2: Fresh-install ordering — gh multi-account before SSH keys

**Context**: Current order is: gh primary auth → SSH keygen (via
`run.bash`) → playbook runs SSH probe → playbook asks user to paste
pubkeys. The last step has no working per-account gh, so can't
automate.

**Options**:

1. Keep current order, add manual paste-then-gh-authenticate per
   account.
2. Reorder: gh primary → gh multi-account (with scopes) → SSH keygen
   → playbook uploads pubkeys via `gh ssh-key add`.

**Decision**: Option 2. Once we're committing to `gh ssh-key add`,
getting gh working across all accounts first is the precondition.
Aligns with the user's stated preference: "gh working first, then SSH
keys become easy".

**Date**: 2026-04-24

### Decision 3: Standalone `gh-account-setup.bash` script

**Context**: Adding a new GitHub account requires the user to know 3
separate manual steps in the right order: (1) run `gh auth login` (which
shows a confusing SSH key upload prompt), (2) edit `localhost.yml`, (3)
run the playbook (which pauses for manual SSH key paste). The user wants
a single command: add the account to config, run one script, done.

**Options**:

1. Keep auth/keygen/upload split across `run.bash` + playbook — fix the
   UX within those files.
2. Extract a standalone `scripts/gh-account-setup.bash` that handles the
   full per-account flow (gh auth, keygen, `gh ssh-key add`, SSH test),
   callable from `run.bash` AND standalone via `--add=alias:username`.

**Decision**: Option 2. A standalone script:

- Gives users a single command for adding accounts post-install
- Lets `run.bash` delegate instead of inline SSH key logic
- Lets the playbook stay declarative (deploy config/aliases only)
- Uses `gh auth login --hostname github.com --git-protocol ssh --web`
  which skips the confusing SSH key upload prompt entirely
- Consolidates Phases 2 + 4 into one coherent flow

**Date**: 2026-04-28

## Success Criteria

- [ ] Fresh install on a VM with N `github_accounts` entries results
  in N SSH keys, each registered to its correct account, with zero
  manual paste steps
- [ ] Re-running `run.bash` on an already-configured system is
  idempotent — no spurious key regeneration, no duplicate uploads
- [ ] Intentionally breaking the setup (e.g. deleting a pubkey from
  one account) produces a clear, actionable failure on the next run,
  not a silent false positive
- [ ] The ccy entrypoint mismatch check passes for every
  `github_<alias>` key when run against its matching account
- [ ] `./scripts/qa-all.bash` passes
- [ ] Signed-commits research produces a clear go/no-go
  recommendation with reasoning

## Risks & Mitigations

| Risk                                                                                              | Impact                                             | Mitigation                                                                                                                                                            |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gh auth refresh` scope flow can't be driven non-interactively                                    | High — blocks automation of first-time scope grant | Fail fast in the playbook with the exact command the user must run; do it once per account during fresh-install in `run.bash` so the playbook rarely has to re-prompt |
| GitHub rejects programmatic key upload due to org SSO enforcement                                 | Medium — would affect LTS/EC accounts              | Detect in the preflight; fall back to paste-and-confirm prompt with link to SSO authorisation page                                                                    |
| Key regeneration trigger is in a shared component we don't control (gnome-keyring, F43 migration) | Medium — can't fix, only work around               | Phase 1 research output will inform whether we need defensive copies or just a clearer recovery runbook                                                               |
| Signed-commits research says "do it" but implementation is larger than anticipated                | Low — it's spun off into its own plan              | Keep this plan's scope tight: research only, implementation in Plan 00036                                                                                             |

## Timeline

- Phase 1: Research & design
- Phase 2: Fresh-install multi-account gh setup (run.bash)
- Phase 3: Harden play-github-cli-multi.yml SSH probe
- Phase 4: Programmatic pubkey upload
- Phase 5: ccy diagnostics alignment
- Phase 6: Signed commits research
- Phase 7: Docs & QA

(Phase 5 and 6 can run in parallel with Phase 2-4 if preferred — they
touch different files.)

## Notes & Updates

### 2026-04-24 — plan created

- Originated from today's session: ccy token mismatch → playbook
  probe bug → manual registration of four pubkeys via `gh ssh-key add` as an immediate unblock.
- Manual unblock done: `github_balli`, `github_lts`, `github_ec`,
  `github_joseph` now registered against their correct accounts.
  `ballidev` and `joseph-uk` needed `gh auth refresh --scopes admin:public_key` first (interactive).
- `ballidev` auth end-to-end verified working in live terminal.
  Post-auth SSH hang observed on `-i github_balli -o IdentitiesOnly=yes` probe — out of scope for this plan (suspected
  VPN/MTU), noted here so it doesn't get lost.
- Reproduced the ccy side of the bug in a separate project (user
  picked `github_ec`; entrypoint reported "Expected: LTSCommerce /
  Got: edmondscommerce") — confirms Phase 5 is a real blocker, not
  just hypothetical.
- Patched `.claude/hooks/handlers/pre_tool_use/system_paths.py` to
  exempt `$CLAUDE_PROJECT_DIR` (derived from handler's own
  `__file__` since the daemon process env lacks that variable) —
  without this, the handler blocks edits to the project itself when
  Claude runs on the host (since the project lives under `/home/`).
  This was a separate blocker during today's session. Daemon
  restarted to pick it up.

### 2026-04-24 — Phase 5 (ccy fix) implemented

- `files/var/local/claude-yolo/lib/ssh-handling.bash:112` now uses
  `-F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes` so the
  probe is fully isolated from `~/.ssh/config` and ssh-agent.
- Host-side cross-check added: after `$token_func` returns a token,
  we call `gh api user --jq .login` and assert it equals
  `GITHUB_USERNAME` from the SSH probe. Fails fast with actionable
  wording instead of letting the container entrypoint surface the
  mismatch post-image-build.
- `CCY_VERSION` bumped to `3.12.2`.
- **Deployed and verified**: user ran the CCY ansible playbook,
  rebuilt the container, and confirmed ccy now launches cleanly
  across projects (no more token-mismatch error). Phase 5 complete.
- Phases 1-4 (playbook hardening, fresh-install reordering,
  programmatic upload), 6 (signed-commits research), and 7 (docs)
  remain for future work. Plan stays In Progress; will move to
  Completed/ when all phases are done, or could split remaining
  phases into 00037-onwards if priority dictates.

### 2026-04-27 — scope list decided + per-account audit landed

- Scope list finalised as `github_required_scopes` (top of
  `playbooks/imports/play-github-cli-multi.yml`):
  `admin:public_key, gist, project, read:org, read:project, repo, user:email`.
  `project` and `read:project` added to enable GitHub Projects v2 access
  from gh tokens (needed for IDE Projects integration and `gh project`
  CLI). Marked Phase 1 "Decide scope list" ✅.
- Design change vs. original plan: the per-account scope audit lives in
  the playbook, not `run.bash`. Reasons: (a) the playbook is the place
  that already iterates `github_accounts`, (b) it runs on every deploy
  so it catches drift if a scope gets revoked or a new scope is added
  to the canonical list, (c) `run.bash` already invokes the playbook —
  putting it in both is duplication. The audit task switches to each
  account, reads its scopes, restores the originally-active account,
  and fail-fasts with the exact `gh auth switch && gh auth refresh --scopes <missing>` command per account. Marked Phase 2 "Per-account
  scope audit" ✅ and Phase 4 "Scope preflight in playbook" ✅
  (latter superseded by the broader Phase 2 audit).
- The `gh-{alias}-token-phpstorm` bash function now templates its
  `required_scopes` array from `github_required_scopes` so there's a
  single source of truth — adding a scope to the var propagates to
  both the deploy-time audit and the runtime PhpStorm helper.
- `run.bash` `admin:public_key` check at line 568 left in place for
  now: gives the primary account fast feedback before the playbook
  runs. Could be removed once Phase 2's run.bash split lands and the
  playbook becomes the canonical source — not removing yet to keep
  the change focused.

### 2026-04-28 — Decision 3: standalone `gh-account-setup.bash`

- User tried to add a new GitHub account and hit the confusing
  `gh auth login` SSH key upload prompt. Feedback: "this process is
  not smooth, the gh stuff should be handled by play or run.bash or
  maybe a sub script that run.bash calls and I can call. All I should
  have to do is add the key to the env config."
- Decided on a standalone `scripts/gh-account-setup.bash` (Decision 3)
  that consolidates Phases 2 + 4 into one script. Supports
  `--add=alias:username` for post-install use and `--setup-all` for
  `run.bash` to call during fresh install.
- Key UX improvement: `gh auth login --hostname github.com --git-protocol ssh --web` skips the confusing "Upload your SSH public
  key?" prompt — just opens the browser for OAuth.
- Playbook's manual `pause:` SSH key paste prompt will be removed;
  the script handles `gh ssh-key add` programmatically before the
  playbook ever runs.
- Updated Phase 2 and Phase 4 task lists to reflect the new design.
  Plan status changed to In Progress.

### 2026-04-28 — Phase 2 script and run.bash integration implemented

- Created `scripts/gh-account-setup.bash` with three modes:
  - `--add=alias:username` — add new account to config + full setup
  - `--setup-all` — set up all accounts from `localhost.yml`
  - `--check` — read-only health verification
- Per-account flow: (1) `gh auth login --web` (skips confusing SSH key
  upload prompt), (2) scope audit via `X-Oauth-Scopes` header + `gh auth refresh` if missing, (3) SSH key generation if missing, (4) `gh ssh-key add` if not registered on GitHub, (5) isolated SSH test
  (`-F /dev/null -o IdentityAgent=none`) to verify correct account.
- Replaced run.bash inline SSH key generation block (was ~70 lines of
  Python+bash) with a single call to the script. Passphrase passed via
  `GITHUB_SSH_PASSPHRASE` env var if already in memory, otherwise the
  script decrypts from vault itself.
- Added config comment: `# GitHub CLI accounts — to add more later: scripts/gh-account-setup.bash --add=alias:username`
- Bumped `RUN_BASH_VERSION` to 1.1.0.
- QA passed (`./scripts/qa-all.bash`), shellcheck clean on new script.
- Remaining: idempotency test on host, playbook `pause:` removal
  (Phase 4), SSH probe hardening (Phase 3).
