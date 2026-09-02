# Plan 00035: GitHub Multi-Account Hardening (gh + SSH Keys + Signed Commits)

**Status**: Dormant
**Created**: 2026-04-24
**Owner**: joseph
**Priority**: High

> Full historical plan text: [PLAN_archive.md](PLAN_archive.md). Root cause,
> code map, decisions and risks: [DECISIONS.md](DECISIONS.md). Activity log:
> `JOURNAL/`.

## Overview

The F42 to F43 upgrade exposed a structural fragility in GitHub multi-account
setup. When `run.bash` re-ran, the four `~/.ssh/github_*` keys were regenerated
but never uploaded to GitHub, and `play-github-cli-multi.yml` declared them
working because its SSH probe fell through to `~/.ssh/id` via the default
`Host github.com` config entry. ccy's entrypoint mismatch check caught it a day
later, and the same false positive reproduces whenever a project picks a
`github_<alias>` key whose alias differs from the account `~/.ssh/id` belongs
to.

This plan replaces the manual-paste upload and the un-isolated probe with a
programmatic flow: `gh` registers pubkeys against the correct accounts, and a
per-account assertion catches wrong-account registrations immediately. It also
reorders fresh install so gh multi-account auth (with all required scopes,
including `admin:public_key`) is established before any SSH key work.

**Dormant since 2026-07-13.** Remaining tasks need HOST deployment and a live
re-run of `run.bash` / the playbooks against real GitHub accounts; none of that
can happen inside the CCY container. Resume at the next host provisioning
session or the next time a `github_<alias>` account is added.

## Goals

- Fresh install sets up gh multi-account first, with all required scopes per
  account, before any SSH key work
- `play-github-cli-multi.yml` SSH probe is fully isolated from `~/.ssh/config`
  and ssh-agent fallbacks
- Every per-account key is verified to authenticate as that account, not just
  "as some account"
- SSH pubkey upload to GitHub is programmatic via `gh ssh-key add`
- Missing `admin:public_key` scope is detected up front with a clear
  remediation command
- ccy's SSH probe (`lib/ssh-handling.bash`) has the same hardening so the
  entrypoint mismatch check stops false-alarming
- Root cause of the 2026-04-23 key regeneration is identified and documented
- Signed-commit feasibility is researched and a follow-up plan created if worth
  doing

## Non-Goals

- Not changing the `github_accounts` format in `localhost.yml`
- Not migrating away from `ansible-vault` for the SSH passphrase
- Not adding a GUI/wizard; CLI-only, consistent with `run.bash`
- Not supporting non-GitHub SSH hosts (GitLab, Bitbucket etc.)
- Not implementing signed commits in this plan (separate plan if research says
  yes)

## Context & Background

See [DECISIONS.md](DECISIONS.md) for the six-point root cause summary, the
code-location map, and the overlap with Plan 034.

## Tasks

### Phase 1: Research & design

- [ ] ⬜ **Identify the key-regeneration trigger** — trace what deleted the
  four `github_*` keys before `run.bash` regenerated them. Candidates: a
  playbook task outside `play-github-cli-multi.yml`, the F42 to F43 home-dir
  migration, or a `run.bash` step before the keygen check. Document the
  finding in DECISIONS.md.
- [x] ✅ **Decide scope list** — codified as `github_required_scopes` at the
  top of `playbooks/imports/play-github-cli-multi.yml`; the PhpStorm token
  function reads the same list. See DECISIONS.md, Decision 4.
- [ ] ⬜ **Decide fresh-install ordering** — write out the new step sequence
  from `run.bash` entry through playbook completion; gh multi-account auth
  must be complete before any SSH key operation.
- [ ] ⬜ **Decide how to surface the interactive `gh auth refresh` step** —
  the browser device-code flow cannot be automated; needs a clean once-per-
  account prompt in `run.bash` naming which account to log into first.

### Phase 2: Standalone `gh-account-setup.bash` script (Decision 3)

- [x] ✅ **Create `scripts/gh-account-setup.bash`** — `--add=alias:username`,
  `--setup-all`, `--check`; idempotent per-account flow of auth, scope audit,
  keygen, `gh ssh-key add`, isolated SSH test. Design in DECISIONS.md.
- [x] ✅ **Update `run.bash`** — inline SSH key generation replaced by a call
  to `scripts/gh-account-setup.bash --setup-all`; primary auth block kept
  (runs before `localhost.yml` exists); `github_accounts` config comment hints
  at `--add`.
- [x] ✅ **Per-account scope audit** — implemented in
  `play-github-cli-multi.yml` rather than `run.bash` (DECISIONS.md,
  Decision 4). Fail-fasts with the exact `gh auth switch && gh auth refresh`
  command per account.
- [ ] ⬜ **Idempotency test** — re-running the script with everything in place
  must be a no-op with success logs. Requires the host and real gh accounts.
- [x] ✅ **Bump `RUN_BASH_VERSION`** — bumped to 1.1.0.

### Phase 3: Harden play-github-cli-multi.yml SSH probe

- [ ] ⬜ **Isolate the probe** — add `-F /dev/null`, `-o IdentityAgent=none`,
  `-o IdentitiesOnly=yes` to both the initial probe and the re-probe.
- [ ] ⬜ **Parse the authenticated user** — extract `Hi <user>!` per alias
  into a fact.
- [ ] ⬜ **Classify each key** — OK (authed as expected user), WRONG-ACCOUNT
  (fail fast with "delete from <wrong> then retry"), NEEDS-UPLOAD (Permission
  denied / timeout).
- [ ] ⬜ **Strengthen the final assertion** — assert `('Hi ' + expected + '!') in item.stdout`; failure message names expected and actual users.

### Phase 4: Programmatic pubkey upload (now in script)

- [x] ✅ **Scope preflight in playbook** — superseded by the broader Phase 2
  per-account scope audit, which runs before the SSH key block.
- [ ] ⬜ **Move pubkey upload into `gh-account-setup.bash`** — remove the
  playbook's manual `pause:` prompt and the "Instructions for adding SSH keys"
  block; the playbook's probe stays as verification only.
- [ ] ⬜ **Verify post-upload** — the re-probe block exists; it needs the
  Phase 3 isolation fix to work correctly.

### Phase 5: ccy diagnostics alignment

- [x] ✅ **Apply the same SSH probe isolation** to
  `files/var/local/claude-yolo/lib/ssh-handling.bash`.
- [x] ✅ **Fix the misleading log line** — cross-checks `gh api user` against
  the SSH-detected username and fails on the host on disagreement.
- [x] ✅ **Bump `CCY_VERSION`** to 3.12.2.
- [x] ✅ **Deploy to host** — deployed via the CCY playbook and confirmed to
  resolve the token-mismatch error across projects.

### Phase 6: Signed commits research

- [ ] ⬜ **Research GitHub signed commits options** — GPG keys, SSH signing
  keys, gitsign/Sigstore. Write a decision doc in this folder comparing key
  management complexity, per-account separation, integration with the
  existing gh/ssh flow, revocation, and user UX.
- [ ] ⬜ **Decide go/no-go** — decide whether to open a follow-up plan to
  implement; document in DECISIONS.md.

### Phase 7: Docs & QA

- [ ] ⬜ **Update `docs/` post-upgrade guide** — new fresh-install ordering
  and how to recover from regenerated keys.
- [ ] ⬜ **Add a recovery runbook** — regenerated keys not on GitHub, keys on
  the wrong account, missing scopes. Short, concrete commands.
- [ ] ⬜ **`./scripts/qa-all.bash` passes** for all changed bash/ansible.
- [ ] ⬜ **Test on a clean VM or container** — full fresh-install flow with
  two or more `github_accounts` entries; verify pubkeys land on the right
  accounts.

## Dependencies

- **Depends on**: nothing hard. Plan 034 overlaps conceptually but neither
  blocks the other.
- **Blocks**: future work wanting trustworthy multi-account gh in `run.bash`
  or ccy (per-account push automation, PR creation as a specific account,
  signed commits if Phase 6 says go).

## Technical Decisions

Recorded in [DECISIONS.md](DECISIONS.md): programmatic upload over manual
paste (1), gh multi-account before SSH keys (2), the standalone
`gh-account-setup.bash` script (3), and scope audit in the playbook (4).

## Success Criteria

- [ ] Fresh install with N `github_accounts` entries yields N SSH keys, each
  registered to its correct account, with zero manual paste steps
- [ ] Re-running `run.bash` on a configured system is idempotent: no spurious
  key regeneration, no duplicate uploads
- [ ] Intentionally breaking the setup (e.g. deleting a pubkey from one
  account) produces a clear, actionable failure, not a silent false positive
- [ ] The ccy entrypoint mismatch check passes for every `github_<alias>` key
  against its matching account
- [ ] `./scripts/qa-all.bash` passes
- [ ] Signed-commits research produces a clear go/no-go recommendation

## Risks & Mitigations

See the risks table in [DECISIONS.md](DECISIONS.md).

## Delivery & Milestones

- Phase 5 (ccy SSH probe isolation, CCY 3.12.2) delivered and verified on host
- Scope list and per-account scope audit delivered in
  `play-github-cli-multi.yml`
- `scripts/gh-account-setup.bash` and the `run.bash` 1.1.0 integration
  delivered; host idempotency test outstanding
- Plan marked Dormant pending a host session (see JOURNAL/ and PLAN_archive.md)
