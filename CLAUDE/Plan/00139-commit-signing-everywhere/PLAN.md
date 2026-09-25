# Plan 00139: commit signing everywhere

**Status**: In Progress
**Created**: 2026-09-24
**Owner**: joseph
**Priority**: High
**Issue**: Addresses #4 (its GPG route is superseded by SSH signing; see D1)

## Overview

Every commit and tag made on the owner's machine is signed, so GitHub marks it Verified
and anything consuming this repo can require a signature from the owner or the owner's
machine. That covers the owner at a terminal, `cc` agents on the host, and every ccy
container. The owner wants this rolled in now, ahead of the consumers that will start
relying on it.

Signing exists today (Plan 00137 Task 0.3), but it runs the other way round. It is SSH
signing with a passphrase-protected key, configured in `~/.config/git/config` so ccy does
not copy it, and never on by default. The one thing it signs is `git sign-deploy`, which
the self-updating server's gate trusts. That design rests on Plan 00137's D3: an agent must
not be able to sign. The owner has overruled D3 in those terms: "the goal is not to prevent
MY OWN AGENTS from being able to push stuff". The security gain wanted is provenance, a
commit verified as the owner's or the owner's machine's, not keeping the owner's own agents
out.

## Goals

- Every commit and tag signed by default: on the host, in `cc` sessions, and inside ccy.
- One signing identity per machine, which needs no human at signing time.
- GitHub shows the owner's signed commits as Verified.
- The self-updating server keeps its gate, now trusting the machine key.
- Plan 00137's design docs and `docs/configuration.md` describe the new model, and D3 is
  recorded as overruled rather than silently dropped.

## Non-Goals

- GPG keys and gpg-agent forwarding (issue #4's original route; D1).
- Re-signing existing history.
- Requiring signatures on GitHub (a ruleset). That is a separate, outward-facing repo
  setting, and the owner's call once everything signs (Task 4.2).
- Toolbox and LXC guests.

## Decisions

- **D1 — SSH signing, not GPG.** git, GitHub and the self-update gate already handle it,
  and the key file can be mounted into ccy with SELinux left on. GPG would need agent socket
  forwarding into every container.
- **D2 — The key has no passphrase.** Host `cc` agents and ccy containers have no terminal
  to type one into. The only other way to sign unattended is a forwarded ssh-agent, which
  ccy supports only with SELinux labelling disabled. The key is 0600 in `~/.ssh`, the same
  trust level as the push keys ccy already mounts, and it is mounted read-only.
- **D3 — Signing config moves to `~/.gitconfig`,** which ccy copies. The launcher mounts
  the key and repoints `user.signingkey` in its private copy.
- **D4 — Plan 00137's D3 is overruled by the owner.** The play's "must have a passphrase"
  assert inverts to "must not have one", and `git sign-deploy` goes. Every commit is signed,
  so the server gate's check of HEAD needs no special commit.

## Tasks

### Phase 1: Host signing on by default

- [x] ✅ **Task 1.1**: `play-git-configure-and-tools.yml` generates the machine signing
  key when it is absent (ed25519, no passphrase, 0600). It asserts that the key needs no
  passphrase, and fails with a remedy if it does.
- [x] ✅ **Task 1.2**: Global config: `gpg.format ssh`, `user.signingkey`,
  `commit.gpgsign true`, `tag.gpgsign true`. Remove the XDG copies and `alias.sign-deploy`
  that Plan 00137 wrote, so no stale setting stays live.
- [ ] 🔄 **Task 1.3**: Signing keys per GitHub account, registered by IaC. The owner
  overruled the earlier "owner registers one key by hand" decision: signing covers every
  repo on the machine, not just this one, and commits go out under each account's email
  (`gh-switch <alias> --update-git`). So it follows the pattern the auth keys already use:
  - `play-github-cli-multi.yml` generates `~/.ssh/github_<alias>_signing` per account (no
    passphrase, D2) and uploads it with `gh ssh-key add --type signing`, idempotently;
  - git picks the key through `includeIf` on the repo's `github.com-<alias>` remote, and
    falls back to the machine key for other repos;
  - ccy stages the key for the project's account;
  - the new scope joins `vars/github-required-scopes.yml`, which every login, refresh and
    audit reads.
  - [x] ✅ Implementation. `helpers/github_signing` registers every key GitHub lacks, with
    each account's own token, and the machine key on the account that has `user_email`
    verified; then proves git picks each key in a scratch repository. CCY 3.67.1 stages
    the key git picks for the project, taken only from `~/.gitconfig` and the system
    config. Acceptance gains checks 12 and 13
  - [x] ✅ QA review: BLOCK, one blocking finding (ccy copied in whatever a project-local
    `user.signingkey` named), seven to fix, eight nits
    ([report](subagent-reports/260925-qa-reviewer-per-account-signing-opus-5.md)). All fixed
    or answered in the journal
  - [ ] ⬜ **HOST**: meta-deploy (`CLAUDE/Plan/meta-deploy.bash`, which runs this plan's
    deploy and acceptance). If the token audit stops it, the one command it names
    (`scripts/gh-account-setup.bash --setup-all`) asks each account for everything it
    lacks in one authorisation; then meta-deploy again
- [ ] 🔄 **Task 1.4**: The owner asked for this machine's public key to be kept in the
  private config repo. The play writes it into `localhost.yml` as `git_signing_public_key`,
  in a managed block, the way other plays record values there. It uses its own name,
  because `self_update_signing_public_key` is the key a server trusts. Merged as
  `cd50ffc1` (the desktop agent's commit, `a594063e`)
  - [x] ✅ **HOST**: a deploy run. The task reported `changed`, and exactly one managed block
    was written
  - [ ] ⬜ **HOST (owner)**: "Save local config to repo" in `run.bash`

### Phase 2: ccy signs

- [x] ✅ **Task 2.1**: The launcher stages the signing key into the gitconfig copy's
  directory, which is already mounted read-only (and relabelled where SELinux needs it),
  and points `user.signingkey` at the mounted copy. Signing that is on with no usable key
  refuses the launch. `stage_git_signing_key` in `lib/ssh-handling.bash`, CCY 3.66.0.
- [x] ✅ **Task 2.2**: `scripts/test-ccy-git-signing.bash`, gate `ccy-git-signing`. It
  fails against the launcher from before this plan, where the function does not exist.

### Phase 3: The self-update server

- [x] ✅ **Task 3.1**: The server's allowed signers carry the machine key
  (`self_update_signing_public_key`). `localhost.yml.dist` and the docs updated. The
  helpers needed no change beyond the refusal message's wording, and
  `scripts/test-self-update-cycle.bash` still passes.
  - [ ] ⬜ **HOST (owner)**: a server that already trusts the old passphrase key needs the
    new key's `.pub` line in `self_update_signing_public_key`, then a re-run of
    `play-self-update.yml`
- [x] ✅ **Task 3.2**: Plan 00137's PLAN D3 and `DESIGN-cycle.md` record D4 of this plan.

### Phase 4: Docs, acceptance, review

- [x] ✅ **Task 4.1**: `docs/configuration.md` "Commit Signing", `docs/playbooks.md` and
  `docs/ccy.md` describe the new model.
- [ ] ⬜ **Task 4.2**: Owner's call, after everything signs: a GitHub ruleset requiring
  signed commits on `F*` branches.
- [ ] 🔄 **Task 4.3**: `deploy.bash` and `acceptance.bash`. Acceptance checks, on the host,
  that a commit made in a scratch repo carries a good signature from the machine key. The
  same check inside ccy needs a fresh ccy session, so it is an owner step.
  - [x] ✅ Both scripts written
  - [x] ✅ **HOST**: `./deploy.bash`, then `./acceptance.bash`. Deploy passed; acceptance
    passed 10 of 11. Only check 11 (key registered on GitHub) fails, which waits on Task 1.3
  - [ ] ⬜ **HOST (owner)**: a commit inside a ccy session started after the deploy
- [x] ✅ **Task 4.4**: qa-reviewer pass over the full plan diff.
  - [x] ✅ First pass: FAIL, one blocking finding (the self-update-cycle test signed its
    "unsigned" fixtures on a host that signs by default), four should-fix, five nits
    ([report](subagent-reports/260924-qa-reviewer-opus-5.md)). All ten are fixed
    ([fixes](subagent-reports/260924-fork-review-fixes-opus-5.md)).
  - [x] ✅ Confirming pass: PASS WITH NITS
    ([report](subagent-reports/260924-qa-reviewer-round2-opus-5.md)). The should-fix and
    all three nits are fixed

## Success Criteria

- [ ] A commit made by the owner, a host `cc` agent and a ccy agent each shows
  `git log --format=%G?` = `G` and is Verified on GitHub.
- [ ] The self-update gate still refuses an unsigned HEAD, and accepts a HEAD the machine
  key signed.
- [ ] No doc still says signing is deliberate or opt-in.

## Delivery & Milestones

- `6001a540` — signing on by default, ccy 3.66.0, the `ccy-git-signing` gate, docs
  (merged as `78d33e43`)
- `d8a1af89` — the first review's ten findings: tests isolated from the machine's git
  config, launcher deployed first, the signing key named in ccy's risks (merged as
  `8003868e`)
- The confirming review's should-fix and nits: the isolation probe fails when it makes no
  commit, and environment-passed git config is dropped by every isolated test
- Branch `per-account-signing-keys`: the GitHub scopes in one file and one helper
  (`helpers/github_scopes`, the DBF remediation of `gh-scope-outside-ssot`), run.bash
  asking for every missing scope in one pass, and a signing key per account (CCY 3.67.0)
