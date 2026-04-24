# Plan 00035: GitHub Multi-Account Hardening (gh + SSH Keys + Signed Commits)

**Status**: Not Started
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
- [ ] ⬜ **Decide scope list** — enumerate the scopes each account needs.
  Known so far: `admin:public_key`, `repo`, `read:org`, `gist`,
  `user:email`. Possibly `admin:org` for primary accounts. Document.
- [ ] ⬜ **Decide fresh-install ordering** — write out the new step
  sequence from `run.bash` entry through playbook completion. Must
  ensure gh multi-account auth is complete before any SSH key
  operation.
- [ ] ⬜ **Decide how to surface the interactive `gh auth refresh` step**
  — browser device-code flow can't be automated. Needs a clean prompt
  in `run.bash` that runs once per account missing the scope, with a
  clear "log into github.com as <X> in your browser first" message.

### Phase 2: Fresh-install multi-account gh setup (run.bash)

- [ ] ⬜ **Split single-account auth from multi-account auth** — the
  current `run.bash:522-545` block handles only the primary. Add a new
  block that runs AFTER `github_accounts` is known (currently after
  line 740) and BEFORE `play-github-cli-multi.yml` runs, iterating
  each account and calling `gh auth login --hostname github.com --user <X>` for any not yet authenticated.
- [ ] ⬜ **Per-account scope audit** — for each account, query
  `gh auth status --json hosts` and check for the required scope list.
  If any scope is missing, prompt the user to run
  `gh auth switch --user <X> && gh auth refresh --hostname github.com --scopes <missing>` before the install continues.
- [ ] ⬜ **Idempotency test** — re-running the block with everything
  already in place must be a no-op with success logs, not redundant
  prompts.
- [ ] ⬜ **Bump `RUN_BASH_VERSION`** with a message describing the new
  multi-account auth step.

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

### Phase 4: Programmatic pubkey upload

- [ ] ⬜ **Scope preflight in playbook** — for each account in
  `ssh_keys_to_add`, verify `admin:public_key` is present. If not,
  fail the playbook with the exact `gh auth refresh` command needed.
  Phase 2 should eliminate this path in normal fresh installs, but
  the playbook must still fail-loud if someone's setup got out of
  sync.
- [ ] ⬜ **Replace the manual `pause:` prompt** at lines 237-262 with
  a task that loops over `ssh_keys_to_add` and runs
  `gh auth switch --user <expected> && gh ssh-key add <pubkey> --title "<hostname>-<alias>-<date>" --type authentication` per key.
- [ ] ⬜ **Verify post-upload** — the re-probe block already exists;
  it just needs the isolation fix from Phase 3 to work correctly.

### Phase 5: ccy diagnostics alignment

- [ ] ⬜ **Apply the same SSH probe isolation** to
  `files/var/local/claude-yolo/lib/ssh-handling.bash:112`. Without
  this, ccy has the same false-positive risk as the playbook did.
- [ ] ⬜ **Fix the misleading log line** at
  `ssh-handling.bash:188` — "Retrieved token for GitHub account:
  $GITHUB_USERNAME (via $token_func)" conflates the SSH-detected
  user with the alias-mapped user; log both explicitly when they
  differ, and fail-fast on the host side (not inside the container)
  when they disagree.
- [ ] ⬜ **Bump `CCY_VERSION`** per the CCY version-bump rule in
  `CLAUDE/ContainerRules.md`.

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
