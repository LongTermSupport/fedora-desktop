# Plan 00153: release tags, Fedora major plus semver

**Status**: In Progress (Phases 1 and 2 done; Phase 3 built and merged; Phase 4, the first release, waits on host acceptance; nothing is tagged)
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

- [x] ✅ **Task 2.1**: Tests first (`scripts/test-release.bash`, 59 cases, mutation-checked, a gate in
  `qa-all.bash`), then `scripts/release.bash`. Two steps, because F44 only takes pull requests:
  `prepare first|minor|patch` refuses on a dirty tree, a branch that is not `F<major>`, one
  behind or ahead of its remote, or CI not green on HEAD; derives the major from the branch;
  writes the changelog entry (commit subjects since the last tag) for the owner to edit;
  commits it signed on `release-<version>` and opens the pull request. `tag <version>`, after a
  merge-commit merge, signs the annotated tag on the signed release commit (not GitHub's merge
  commit), refuses an unsigned or CI-red release commit, pushes it and creates the GitHub Release.
  Neither step is ever run by an agent against the real repository.
- [x] ✅ **Task 2.2**: `.github/workflows/qa.yml` also runs on pushed tags `N.N.N`.
- [x] ✅ **Task 2.3**: [CLAUDE/Release.md](../../Release.md) (agents never tag without the owner
  asking; indexed in `CLAUDE.md`) and [docs/releases.md](../../../docs/releases.md).

### Phase 3: Self-update follows tags

- [x] ✅ **Task 3.1**: `fedora-desktop-self-update` and its play deploy the newest signed
  release tag for the host's Fedora major by default; one play variable (`self_update_channel`,
  `tags` or `branch`) opts into following the branch tip (owner decision, Task 1.1). Built as
  designed: `gate.parse_release_tag` / `choose_release`, `update.py --channel` (fetch without
  force and with prune, only the newest tag judged, no fallback, exits 20/21/22, the
  `SELF-UPDATE-TAG` marker, the anchor), `cycle.py` (`CHANNEL` config key, `tag` in the result,
  published copy and owed marker). Tests: `test_gate.py`, `test_update.py`, `test_cycle.py`, and
  both channels end to end in `scripts/test-self-update-cycle.bash`.

- [x] ✅ **Task 3.2**: Coordinate with Plan 00137 (unattended server self-update), which is
  waiting on server runs: land this before or as part of its acceptance. Done: the CORRECTION
  note in 00137's `DESIGN-cycle.md`, and 00137's Task 5.3 and acceptance script now say the
  cycle needs a release tag (`44.0.0`, Task 4.1) or `self_update_channel: branch`.

- [x] ✅ **Task 3.3**: Dropped. The Phase 3 review asked for a "release 44.2.1" line in the
  host-health report. Its findings (`probe_results.Finding`) are problems only, and `[]` means
  healthy, so an informational line would read as a fault or need a new kind of finding for one
  line nobody asked for (YAGNI). The release a host runs is in its published self-update record,
  the status and every alert; a refusal names the tag and the check.

### Phase 4: First release

- [ ] ⬜ **Task 4.1**: After the current round's host acceptance (Plans 00148, 00151, 00109,
  00144 at the next reboot), tag `44.0.0`. Owner, 2026-10-05: tag it once the plans in
  progress are finished and the branch is at a stable point; no new plan starts before
  then. Agents still tag only when the owner asks ([Release.md](../../Release.md)).
- [ ] ⬜ **Task 4.2**: `qa-reviewer` over the plan's diff; plan complete.

## Success Criteria

- [ ] `44.0.0` exists on GitHub as a signed tag with a Release and a changelog entry.
- [ ] A server running the unattended self-update moves to a new tag and ignores commits
  on the branch that are not tagged.
- [ ] Making the next release is one command.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
