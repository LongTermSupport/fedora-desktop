# Plan 00189: play-cli-tools verifies the ouch tarball against a pinned sha256

**Status**: In Progress
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

- [x] ✅ **Task 1.1**: Re-derive the sha256 of `ouch-x86_64-unknown-linux-musl.tar.gz` at the
  pinned version from the release; list which architectures the play supports. Taken from
  the release API's published asset `digest` and confirmed by a fresh download; the play
  supports `x86_64` musl only (`ouchArch` is fixed), so one digest.
- [x] ✅ **Task 1.2**: Add `checksum:` to the download task, the digest beside `ouchVersion`
  (and in `vars/version-pins.yml` if that is where the checker reads it); `--syntax-check`.
  `ouchSha256` sits beside `ouchVersion`; a `get_url` with `checksum:` replaces the
  unchecksummed `unarchive` of the URL. `vars/version-pins.yml` holds no pin values, so the
  `update-versions` skill names the digest instead.
- [ ] 🔄 **Task 1.3**: `deploy.bash` (`play-cli-tools.yml`), an entry in `meta-deploy.bash`;
  `qa-all.bash`; the `qa-reviewer` agent. The play is queued directly in `meta-deploy.list`
  (no `deploy.bash` needed for one play). `qa-all.bash` passed and the host run passed at F44
  96454752 (ok=23 changed=1, the one change the checksum-verified download); the `qa-reviewer`
  pass remains.

## Success Criteria

- [ ] The play installs ouch with the pinned digest, and a wrong digest stops it.
- [ ] A version bump without a new digest is caught by the pin checker or the play.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00189-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #63.
