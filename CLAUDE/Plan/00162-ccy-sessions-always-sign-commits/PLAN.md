# Plan 00162: ccy sessions always sign commits

**Status**: Not Started (recommended: fold into Plan 00139, then delete this plan)
**Created**: 2026-10-07
**Owner**: joseph
**Priority**: Medium

## Overview

The owner's request, verbatim: "we need to ensure ccy sessions are signing commits. i
assume there's no particular reason not to sign commits? you have the gh ssh key so we
need to use that to sign commits for all ccy sessions i think? new plan and sub agent to
sense check this one".

The sense check
([report](subagent-reports/261007-signing-sense-check-opus.md)) found that this is
**already true on the default path** and **already owned by Plan 00139**. Plan 00139's
first goal is signing inside ccy; its D5 / Task 5.2 shipped it, and its Task 4.3 had a ccy
commit confirmed Verified. Every ccy launch with signing on either signs with the session's
SSH identity (`configure_git_signing`, `files/var/local/claude-yolo/lib/ssh-handling.bash`)
or refuses to start. Since the F\* signed-commits ruleset went live, every commit on F44 is
signed, and the latest are Verified on GitHub.

Three gaps remain:

- **G1, signed but Unverified.** In ccy, the key picked at launch signs every commit,
  whatever the repository. The picker ranks keys by push access, not by whether GitHub will
  verify the signature. A key whose account does not have the commit's email verified, an
  arbitrary `--ssh-key`, or the remote's deploy key all produce Unverified commits, and the
  launch does not say so.
- **G2, a server's trust list.** Already tracked as Plan 00139 Task 3.1's owner step.
- **G3, no client-side guarantee.** The container controls its own git config, so only a
  GitHub ruleset can enforce signing. This is the owner's call per repository.

Signing has no reason against it as the default. The deliberate exceptions stay as they
are: Plan 00161's throwaway checkouts set signing off locally, and a box with no GitHub
identity does not sign (the owner's PR #56 decision).

## Goals

- Every ccy session with signing on makes commits GitHub shows as Verified. If it cannot,
  it refuses to launch and says why, rather than signing with a key GitHub will not accept.
- The key picker's default is a key whose signatures GitHub will verify.

## Non-Goals

- Changing CCY 3.85.2's handling of a project-local `commit.gpgsign false` (Plan 00161
  needs it).
- Making a box with no GitHub identity sign (the owner decided on PR #56).
- Rulesets on other repositories (an owner decision, recorded in Task 1.4).

## Tasks

### Phase 0: Placement

- [ ] ⬜ **Task 0.1**: Owner: fold Phase 1 into Plan 00139 as a new phase and delete this
  plan, or keep it separate.

### Phase 1: Signatures GitHub verifies (G1)

- [ ] ⬜ **Task 1.1**: Write the test first: new cases in `scripts/test-ccy-git-signing.bash`
  with `gh` stubbed on `PATH`. A registered key with the commit email verified passes. A key
  on the wrong account, an unregistered key, an unverified email, and a deploy key each
  refuse and name the remedy. Red against the current launcher.
- [ ] ⬜ **Task 1.2**: In `configure_git_signing`, for a key-file identity with signing on,
  check with the session's account token that the key is in `user/ssh_signing_keys` and
  that the commit email (the project's local, else the copy's) is in that account's
  verified `user/emails` or is its noreply address. Reuse the noreply rule from
  `helpers/github_signing`. Refuse with the remedy: the matching account's key,
  `gh-switch <alias> --update-git`, or re-run `play-github-cli-multi.yml`.
- [ ] ⬜ **Task 1.3**: Picker: among keys that can push, rank first the one whose account
  owns the commit email.
- [ ] ⬜ **Task 1.4**: Owner decisions, recorded in the journal: (a) a deploy-key session
  with signing on, which can never be Verified: refuse, or accept with an explicit
  acknowledgement (no warn-and-continue); (b) an allowed-signers file in the container so
  `%G?` works there (optional); (c) signed-commit rulesets for other repositories.
- [ ] ⬜ **Task 1.5**: Bump the CCY version, add a `docs/ccy-changelog.md` entry, and
  describe the check in `docs/ccy.md` and `docs/configuration.md` "Commit Signing".
  Run `qa-all.bash`, then the `qa-reviewer` agent.
- [ ] ⬜ **Task 1.6**: **HOST (owner)**: `./CLAUDE/Plan/meta-deploy.bash` installs the
  launcher. Then, in a repository where a second account can push, accept the default key,
  commit, push to a scratch branch, and confirm GitHub reports `verified: true`.

## Success Criteria

- [ ] A ccy launch whose signature GitHub would not verify is refused, naming the remedy.
  The new `ccy-git-signing` cases prove this.
- [ ] A commit from a ccy session launched with the picker's default key is Verified on
  GitHub (Task 1.6).

## Delivery & Milestones

- Sense check: [subagent-reports/261007-signing-sense-check-opus.md](subagent-reports/261007-signing-sense-check-opus.md)
