# Plan 00153: release tags, Fedora major plus semver

**Status**: In Progress (Phase 1 decided; Phase 2 release command next; nothing is tagged)
**Created**: 2026-10-03
**Owner**: joseph
**Priority**: Medium

## Overview

The repo has no releases: every host deploys whatever the branch tip is, so work in
progress and work that has passed acceptance on a real host look the same. The owner wants
the scheme their PHP QA CI library uses: the major version tracks the platform, and the
other two numbers are semver for the repo's own changes. Here the platform is Fedora, so
releases on the `F44` branch are `44.MINOR.PATCH` (`44.0.0`, `44.1.0`, `44.1.1`, ...), and
the first release on `F45` is `45.0.0`.

Development carries on on the branch as now. When the branch reaches a point that has
passed acceptance on a host, it is tagged. Anything that deploys without a person watching,
first of all the unattended server self-update (Plan 00137), then follows the newest
release tag rather than the branch tip.

## Goals

- Signed, annotated release tags `<fedora-major>.<minor>.<patch>` on the release branches,
  pushed to GitHub.
- One command makes a release: checks the tree, picks the next number, writes the
  changelog entry, tags, pushes.
- The unattended server self-update deploys the newest signed release tag, not the branch
  tip.
- The rules (when minor vs patch, what makes a point releasable) written down once.

## Non-Goals

- Changing the branch model (one default branch per Fedora release stays).
- Replacing ccy's own version (`CCY_VERSION` and the container label keep their meaning;
  a release just contains whatever ccy version is on the branch).
- Tagging automatically on every merge: a release is the owner's call.

## Tasks

### Phase 1: Decisions and design

- [x] ✅ **Task 1.1**: **Owner accepted all** the proposals below and the design's decisions 1
  and 3; on decision 2 they chose tags as the default with branch-tip following kept as an
  explicit opt-in channel (recorded in the design; Task 3.1 builds the switch). Nothing is
  tagged: `44.0.0` waits for Task 4.1. Proposals:
  - Tag name `44.0.0`, no `v` prefix (as dictated); signed annotated tags (`git tag -s`),
    because self-update already trusts only signed commits.
  - Minor: new features, plays or user-visible behaviour. Patch: fixes only. Major: only
    the Fedora release, at the new branch's first release.
  - A point is releasable when `qa-all.bash` and CI pass on it and the plans it ships have
    passed host acceptance (meta-deploy summary all PASS).
  - Changelog: one `CHANGELOG.md` at the root (`docs/ccy-changelog.md` stays ccy's own).
  - GitHub Release created from each tag, body = that changelog entry.
- [x] ✅ **Task 1.2**: Read how `fedora-desktop-self-update` and its play pick the commit to
  deploy (newest signed commit on the branch) and design "newest signed release tag for
  this Fedora major" in its place, including the first-run case of a branch with no tag
  yet (refuse loudly, never fall back to the tip silently). Done:
  [DESIGN-self-update-tags.md](DESIGN-self-update-tags.md). Three points need the owner
  with Task 1.1: the tagged commit must be one the release command signs (a GitHub merge
  commit is signed by GitHub, so tagging it would be refused); tags replace branch-tip
  deployment outright (no running server to migrate); an unusable newest tag refuses, never
  falls back to an older one.

### Phase 2: Release command

- [ ] ⬜ **Task 2.1**: Tests first, then `scripts/release.bash`: refuses on a dirty tree, on
  a branch that is not `F<major>`, behind its remote, or with CI not green on HEAD; takes
  `minor` or `patch`; derives the major from the branch name; writes the changelog entry
  from the commits since the last tag for the owner to edit; signs and pushes the tag;
  creates the GitHub Release.
- [ ] ⬜ **Task 2.2**: `.github/workflows/qa.yml` also runs on pushed tags.
- [ ] ⬜ **Task 2.3**: Docs: release process in `CLAUDE/` (agent rules: agents never tag
  without the owner asking) and a short user-facing note in `docs/`.

### Phase 3: Self-update follows tags

- [ ] ⬜ **Task 3.1**: `fedora-desktop-self-update` and its play deploy the newest signed
  release tag for the host's Fedora major by default; one play variable opts into following the
  branch tip (owner decision, Task 1.1). Tests in `scripts/test-self-update-cycle.bash`, both channels.
- [ ] ⬜ **Task 3.2**: Coordinate with Plan 00137 (unattended server self-update), which is
  waiting on server runs: land this before or as part of its acceptance.

### Phase 4: First release

- [ ] ⬜ **Task 4.1**: After the current round's host acceptance (Plans 00148, 00151, 00109,
  00144 at the next reboot), tag `44.0.0`.
- [ ] ⬜ **Task 4.2**: `qa-reviewer` over the plan's diff; plan complete.

## Success Criteria

- [ ] `44.0.0` exists on GitHub as a signed tag with a Release and a changelog entry.
- [ ] A server running the unattended self-update moves to a new tag and ignores commits
  on the branch that are not tagged.
- [ ] Making the next release is one command.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
