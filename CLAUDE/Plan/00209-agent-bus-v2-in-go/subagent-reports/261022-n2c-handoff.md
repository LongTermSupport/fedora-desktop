# N2 slice n2c: handoff 3

The third builder stopped at its context budget. Worktree
`/workspace/untracked/worktrees/bus-v2-n2c`, branch `feat/bus-v2-n2c`, tip after this file's
commit. Read the brief (`untracked/scratch/n1-common.txt` in the main checkout, task name
"n2c"), then [SPEC-n2c.md](../SPEC-n2c.md), the slice's contract (decisions n2c-D1 to D13).

## Done and committed, each step green under -race and golangci-lint

- Everything of handoff 2: records, fs, the lock, the credential seam, take with the L3 probe,
  release with `Announce`, placement.
- Coverage raised to the floor with behaviour tests.
- `guest.go` (SP3, SP4): `PlaceGuest(layout, Issued)` and
  `KeepGuestToken(base, team, seat, Secret)`.
- `tuwunel_test.go` (`-tags tuwunel`, ST6). It passes against the pinned Tuwunel 1.9.4 (sha256
  `8e9a5873...0b2b`, copied to `untracked/scratch/tuwunel/tuwunel-1.9.4` in this worktree), with
  `-race -count=3`. With `Mint`'s logout removed it fails ("the earlier holder's token survived
  the take"); that mutation was reverted.
- Review round 1 (`261022-n2c-review.md`): FIX-FIRST, 2 high, 1 medium, 8 low. All fixed in
  `1f7442ef`; round 2 confirmed each one fixed.
- Review round 2 (`261022-n2c-review-2.md`): FIX-FIRST, 1 medium, 3 low. The medium (a double
  close) and low 2 (a reused session seat, n2c-D12) are fixed in `9695c84a`. Lows 3 and 4 are
  accepted and recorded as n2c-D13.

Measured state: seat **90.0%** (exactly the floor, so add a behaviour test if any change lowers
it), paths 100%. `golangci-lint run ./internal/teamchat/...` and
`--build-tags tuwunel ./internal/teamchat/seat/...` are clean. qa-changed is red only on
govulncheck GO-2026-6809 (Task 1.6), the accepted red.

## Left

1. Review round 3, the last allowed: a fresh Opus reviewer over `feat/bus-v2-n2a..HEAD` against
   SPEC-n2c.md, C 3.7 and gate B. It verifies the round-2 fixes and writes
   `subagent-reports/261022-n2c-review-3.md`. Fix what it finds, with a test seen red first.
   Anything still open after round 3 goes in the report.
2. The report `subagent-reports/261022-n2c.md`: what was built, coverage per package, the
   decisions (n2c-D1 to D13 and the handoff-2 list below), the review rounds, and the
   design-text fixes proposed.
3. Task 2.4's progress line in PLAN.md (n2c's part).

## Decisions from handoff 2, for the report

- The L3 probe and every path open from the store root's fd with
  `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS` (SW2), not from the fd of
  `sessions/<session>/` as C 3.7.3 words it. This refuses a link on every component, as the
  design asks.
- A free seat lock whose record is over 4 KiB names no run (SD1), as an undecodable one does.
- Release step 1 stops waiting when the lock is free or the session is not restorable.
- An `Outcome` neither posted nor with a one-line `why` is refused (`ErrInvalid`).
- Release step 4 retires with the pool's `seat.json`.
- `Store.Close` releases the root fd.

## Design-text fixes to propose in the report

- B's header for host `cc` against C 3.7.1 (n2c-D3).
- B 2.11's host `since` against `held.json`'s `since_ms`.
- `<seat>` collisions across member types (n2c-D1).
- The release resume's run id (n2c-D5).
- The probe's root-relative `openat2`.
- `released.json` carried across a placement (n2c-D8).
- The release's held/-fd and claim checks (n2c-D9).
- A session seat directory reused by another seat of the team (n2c-D12).
- Whether a reissue may drop a broken `released.json` (n2c-D13).
