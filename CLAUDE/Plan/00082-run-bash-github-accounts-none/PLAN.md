# Plan 00082: implement RUN_BASH_GITHUB_ACCOUNTS=none in run.bash headless v1

**Status**: In Progress
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: Medium

## Overview

`run.bash`'s headless v1 (Plan 00063) hard-requires a configured GitHub account:
`RUN_BASH_GITHUB_ACCOUNTS=none` fails preflight naming a "planned follow-up, not
yet supported." This blocks headless-provisioning a box that has no GitHub
identity of its own — e.g. a CI runner or a shared dev/marketing box in another
estate (lts-infra) that would otherwise need a dedicated bot account or a PAT
minted for the operator's personal account just to satisfy this precondition.

This plan implements the GitHub-empty path: `RUN_BASH_GITHUB_ACCOUNTS=none`
clones the public `fedora-desktop` repo over HTTPS, skips all GitHub/SSH-key
setup, and still runs full provisioning (identity + vault + the main playbook).
This is the design `run.bash --help-run-headless` already documented before it
was deferred (see `git show 83cdb2c -- run.bash`) — not a new design, a revival.

## Corrected Blocker Analysis

The 2026-07-23 deferral (commit `83cdb2c`, Plan 00063 Task 1.6) cited two
"latent server-profile playbook bugs" as blocking this work. Re-verified against
current file state:

| #   | Cited blocker                                                                                                                    | Status                                       | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `play-lxc-install-config.yml:240` clones `lxc-bash` via `git@github.com:...` (needs SSH auth)                                    | **FIXED**                                    | Current file (lines 244-258) clones over HTTPS, unauthenticated, public repo. Explicit comment confirms the old git@/SSH-passphrase mechanism was removed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 2   | `play-github-cli-multi.yml:42`'s `gh --version` check is "ungated" and aborts `playbook-main` when gh isn't installed            | **NOT REPRODUCIBLE from current file state** | The audit's cited mechanism (`run.bash:999-1008` at audit time) is run.bash's own separate, pre-Ansible interactive `sudo dnf install gh` step (today's lines 1658-1667) — confirmed by reading that exact line range in the pre-headless base revision (`git show 79742fa4^:run.bash`, "Installing Github CLI" title at line 999). The empty-path design correctly plans to skip THIS step. But `playbooks/imports/play-git-configure-and-tools.yml`'s own independent, **unconditional** Ansible-level "Install Github Client" task (lines 94-104, `scope: general`, no `when:` gate) has been present unchanged since commit `014b919` (2026-07-20, three days before the audit) and runs BEFORE `play-github-cli-multi.yml` in every `playbook-main.yml` execution (`playbook-main.yml` lines 13 vs 16), regardless of GitHub configuration. So `gh` is installed by the time `play-github-cli-multi.yml`'s check runs, GitHub-empty or not. Static trace only — not independently re-verified by executing Ansible (CCY container rules forbid running playbooks in-container; this is a HOST-verification item, see Task 4). |
| 3   | (not separately named in the deferral, but the actual remaining work) run.bash's own self-clone / gh-auth / SSH-key-upload block | **REAL, unaddressed**                        | `run.bash` lines ~1658-1861 run the SAME shared code path in headless and interactive mode: install gh, `gh auth login`, upload the SSH key, update `known_hosts`, clone `fedora-desktop` via `git@github.com:...`. None of this is gated on `HL_GITHUB_ACCOUNTS`. This is the actual work this plan does.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |

**What this means for scope**: blocker 1 needs no further work. Blocker 2's
failure mode could not be reproduced from current files; it is not re-litigated
here as a live blocker, but the ordering claim should be confirmed with a real
`ansible-playbook --list-tasks`/full run on a HOST as part of accepting this
plan (Task 4) rather than trusted from static reading alone. Blocker 3 is the
plan's actual deliverable.

## Goals

- `RUN_BASH_GITHUB_ACCOUNTS=none` in headless mode provisions a box with no
  GitHub identity: clones `fedora-desktop` over HTTPS, skips gh install/auth/
  SSH-key-upload/known-hosts, still writes identity + runs the main playbook.
- No regression to the existing GitHub-configured headless path or the
  interactive path — both are byte-identical in behaviour to before this plan.
- Contradictory headless config (`RUN_BASH_GITHUB_ACCOUNTS=none` combined with
  `RUN_BASH_CONFIG_SOURCE` or `RUN_BASH_RESTORE_PROJECTS=1`, both of which need
  a GitHub identity) fails fast in preflight, not partway through execution.
- `--help-run-headless` documents the empty path again (PRECONDITIONS, var
  docs, GITHUB section, a canonical invocation example).
- Container-runnable acceptance coverage (preflight-stage only — this repo's
  CCY container rules forbid running Ansible/live provisioning here) proves
  the new preflight branches, and the pre-existing 00063 acceptance assertion
  that the empty path fails is corrected to match the new behaviour.

## Non-Goals

- Multiple GitHub accounts (`RUN_BASH_GITHUB_ACCOUNTS=alice,bob`) — unchanged,
  still rejected in v1 (unrelated to this plan).
- Live end-to-end HOST verification of the full empty-path provision (clone,
  playbook run) — this plan's acceptance coverage is preflight-only, run
  inside the CCY container; live verification is a follow-up the operator runs
  on a real box (Task 4/5).
- Re-opening the bot-account-vs-owner-account identity question for lts-infra
  — already decided (lts-infra Plan 00045 Task 2.8: `LTSCommerce`, no bot
  account). This plan removes the *need* for any GitHub identity at all on
  boxes that don't want one, which is orthogonal to that decision.

## Scope note: shared QA tooling fix

Verifying this plan's own acceptance coverage surfaced a pre-existing defect in
shared, repo-wide QA infrastructure (`scripts/qa-discovery.bash`,
`scripts/qa-js.bash`): file discovery excluded on an absolute-path substring
match, so a checkout whose ancestor path contains an `untracked` segment (e.g.
this CCY container's `/workspace/untracked/repos/fedora-desktop`) excluded
every file in the repo. This plan fixes it (Task 3.5) — repo-relative exclusion
now, matching the mechanism the git-based tracked-file discovery already used —
because `qa-all.bash` could not otherwise report a real result for this plan's
own changes. It is out of scope beyond that fix: no further QA-tooling work is
planned here.

## Tasks

### Phase 1: Preflight

- [x] ✅ **Task 1.1**: `headless_preflight()` — allow `RUN_BASH_GITHUB_ACCOUNTS=none`: skip the token/SSH-passphrase-file requirement when empty; keep the multi-account-comma rejection for a real (non-`none`) value. Add cross-field validation: `RUN_BASH_CONFIG_SOURCE` (set, non-`none`) or `RUN_BASH_RESTORE_PROJECTS=1` combined with `GITHUB_ACCOUNTS=none` fails fast naming the conflict (both require a GitHub identity).

### Phase 2: Execution — skip GitHub/SSH setup, HTTPS-only self-clone

- [x] ✅ **Task 2.1**: SSH keygen block (~1589-1610) — skip entirely when `HL_GITHUB_ACCOUNTS=none` (no login key needed for an HTTPS-only clone).
- [x] ✅ **Task 2.2**: gh-install / `gh auth login` / SSH-key-upload / known-hosts / self-clone block (~1658-1861) — wrap in a top-level `HL_GITHUB_ACCOUNTS=none` branch: new HTTPS clone of `fedora-desktop`, no gh install/auth, `primary_gh_username=""` (set -u safety for downstream refs). Existing branch (GitHub-configured, interactive) untouched.
- [x] ✅ **Task 2.3**: `hl_write_localhost_yml()` — when `HL_GITHUB_ACCOUNTS=none`, write `github_accounts: {}` (not an alias/user pair) so `github_accounts_configured` in `play-github-cli-multi.yml` evaluates false AND the function's own idempotency re-run check (`grep -qE '(!vault|github_accounts)'`) still matches on a second run.
- [x] ✅ **Task 2.4**: "GitHub SSH Key Passphrase" section (~2147-2187) — skip entirely when headless + empty path (no GitHub SSH keys to vault-encrypt a passphrase for).
- [x] ✅ **Task 2.5**: "Setting Up GitHub Multi-Account Access" section (~2189-2206) — the existing `grep -q 'github_accounts'` guard would false-positive on the new `github_accounts: {}` marker; add an explicit `HEADLESS=true && HL_GITHUB_ACCOUNTS=none` skip branch ahead of it. Interactive path's grep-based check is untouched.
- [x] ✅ **Task 2.6**: "Restoring Projects" section (~2289-2313) — defensive-only given Task 1.1's preflight rejection makes this combination unreachable; confirm `primary_gh_username=""` (Task 2.2) keeps it `set -u`-safe if ever reached.

### Phase 3: Docs, versioning, acceptance

- [x] ✅ **Task 3.1**: `--help-run-headless` — restore/update PRECONDITIONS, the `RUN_BASH_GITHUB_ACCOUNTS` var doc, the GITHUB section, and add a canonical invocation example for the empty path.
- [x] ✅ **Task 3.2**: Bump `RUN_BASH_VERSION` with a changelog-style entry describing this change (this file's own convention — "BUMP THIS VERSION ON EVERY CHANGE — NO EXCEPTIONS").
- [x] ✅ **Task 3.3**: Fix `CLAUDE/Plan/00063-.../acceptance.bash` assertion #4 (`github=none deferred in v1`) — it now asserts the OLD (wrong) behaviour and would fail against the new run.bash. Invert it to assert the empty path now proceeds past preflight (mirrors test #10's "hits the NOPASSWD gate" pattern), keeping the fix in the plan that originally shipped the assertion rather than duplicating a corrected copy here.
- [x] ✅ **Task 3.4**: This plan's own `acceptance.bash` — preflight-stage coverage (same container-safe technique as 00063's: drop to `nobody`, controlled env, assert exit code + message) for: `none` no longer requires token/SSH-passphrase files and reaches the NOPASSWD gate; `none` + `RUN_BASH_CONFIG_SOURCE` fails fast; `none` + `RUN_BASH_RESTORE_PROJECTS=1` fails fast.
- [x] ✅ **Task 3.5**: Fix `scripts/qa-discovery.bash` / `scripts/qa-js.bash` — file discovery excluded on an absolute-path substring match instead of a repo-relative one, so a checkout under a path containing `untracked` (this CCY container) excluded every file in the repo. Repo-relative exclusion now, unified with the existing git-based tracked-file discovery mechanism. See "Scope note" above.
- [x] ✅ **Task 3.6**: `./scripts/qa-all.bash` green — 535 files checked, all six stages pass (137 shellcheck findings are info/style, non-gating).

### Phase 4: Review

- [x] ✅ **Task 4.1**: `qa-reviewer` (opus) review of the branch diff — required final step per `CLAUDE.md`'s QA section. Verdict: FIX-BEFORE-MERGE (no BLOCK findings). All 6 "should fix" items resolved: (1) `hl_reconcile_vault`'s abort message and header comment named the wrong cause (github_ssh_passphrase, which the empty path never vault-encrypts) — reworded to the real reason, `ansible.cfg`'s `vault_password_file` needs a readable secret for ANY run to start; (2) the empty path's vault password was documented as REQUIRED but not enforced in preflight — added an explicit check scoped to `GITHUB_ACCOUNTS=none` (the configured path's pre-existing, out-of-scope gap is unchanged); (3) added the missing `CLAUDE/Plan/README.md` index row; (4) anchored `qa-js.bash`'s remaining root-only exclusions (`.git`, `.ansible/roles`, `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`) to `$REPO_ROOT`, leaving only `node_modules` anywhere-in-tree; (5) ticked the two Success Criteria this session can prove in-container; (6) `acceptance.bash`'s `expect_not` now asserts the non-zero exit its own docstring already promised. Also fixed the dead `GH_REPO="gh"` assignment in the empty-path branch (nit).

### Phase 5: Host verification (operator, not this session)

- [ ] ⬜ **Task 5.1**: On a real HOST, run `ansible-playbook playbooks/playbook-main.yml --list-tasks` (or a full run) to independently confirm the Task-2-of-blocker-analysis ordering claim (gh installed before `play-github-cli-multi.yml` runs, regardless of GitHub config) rather than relying on the static trace alone.

- [ ] ⬜ **Task 5.2**: On a real HOST (or a disposable cloud/server VM), run a full `RUN_BASH_GITHUB_ACCOUNTS=none` headless provision end-to-end and confirm it completes.

  **In progress, from the consuming side**: `lts-infra` (the sibling repo pinning this
  one, Plan 00045 Task 3.1/3.1b there) is running exactly this proof — `run.bash`
  downloaded at this branch's merge commit (`68f1596`, PR #33), `GITHUB_ACCOUNTS=none`,
  against a real Fedora guest (`dc-lts-dev-vm`) on a live Proxmox estate, twice (primary

  - idempotency). Its own in-container CLAUDE.md forbids running Ansible in THIS
    container ("CCY container: edit only, deploy on host") and this container is not a
    target Fedora host regardless, so neither task can be closed from inside this repo's
    own checkout — the lts-infra run is the closure for both, and its result (pass/fail,
    with the `changed=0`-equivalent recap) should be copied back here once it completes.

## Success Criteria

- [ ] `RUN_BASH_GITHUB_ACCOUNTS=none` passes preflight with no token/SSH-passphrase files, proceeds through identity + vault + main-playbook execution with no GitHub/SSH setup, clones `fedora-desktop` over HTTPS. (Preflight-level proven in-container; full execution is Phase 5 host work.)
- [ ] Existing GitHub-configured headless path and interactive path unchanged (no regression). (Preflight-level regression proven in-container via both acceptance.bash suites; full execution is Phase 5 host work.)
- [x] ✅ Contradictory `none` + `RUN_BASH_CONFIG_SOURCE`/`RUN_BASH_RESTORE_PROJECTS=1` fails fast in preflight.
- [x] ✅ `./scripts/qa-all.bash` green; 00063's and 00082's acceptance.bash both green in-container.
- [x] ✅ qa-reviewer (opus) review completed with findings addressed — verdict FIX-BEFORE-MERGE, all 6 "should fix" items resolved (see JOURNAL).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00082-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Branch: `task/run-bash-github-accounts-none` (local, not yet pushed — per
  operator instruction, this work is reviewed locally before any push decision).
