# Plan 00189: play-cli-tools verifies the ouch tarball against a pinned sha256

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #63.

## Overview

`play-cli-tools.yml` installs `ouch` (behind the `compress`/`uncompress` wrappers) by
downloading a release tarball from GitHub at run time (#63). The version is pinned
(`ouchVersion`), and the play asserts the installed binary reports it. But the tarball's bytes
are not checked. A re-published or tampered asset, or a compromised download path, would be
extracted into `/opt` and symlinked into `/usr/local/bin` unchecked. The headless provisioner
and the server-recommended optional-play bundle run this play unattended, which is where the
missing check matters most.

The fix is small: a `checksum: "sha256:…"` on the download task, recorded beside
`ouchVersion`, as the other pinned downloads do. Plan 00058 (Dormant) bumped `ouch` without
a checksum in Task 2.2; this plan closes that gap. It does not wait on 00058's darktable
blocker. The issue gives a digest for the `x86_64` musl asset at 0.8.1. Re-derive it from
the published asset rather than copy it, and give each other supported architecture its own
digest.

## Goals

- The ouch download fails unless the tarball matches a pinned sha256.
- The digest sits beside `ouchVersion` and is re-derived on every bump (the
  `update-versions` skill and `scripts/check-pinned-versions.bash` know it).

## Non-Goals

- Bumping `ouch`'s version.
- Other pins (Plan 00058).

## Tasks

### Phase 1: Build

- [ ] ⬜ **Task 1.1**: Re-derive the sha256 of `ouch-x86_64-unknown-linux-musl.tar.gz` at the
  pinned version from the release; list which architectures the play supports.
- [ ] ⬜ **Task 1.2**: Add `checksum:` to the download task, the digest beside `ouchVersion`
  (and in `vars/version-pins.yml` if that is where the checker reads it); `--syntax-check`.
- [ ] ⬜ **Task 1.3**: `deploy.bash` (`play-cli-tools.yml`), an entry in `meta-deploy.bash`;
  `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] The play installs ouch with the pinned digest, and a wrong digest stops it.
- [ ] A version bump without a new digest is caught by the pin checker or the play.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00189-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #63.
