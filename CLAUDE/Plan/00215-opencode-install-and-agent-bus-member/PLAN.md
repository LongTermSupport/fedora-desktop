# Plan 00215: opencode install and agent bus member

**Status**: In Progress (Phase 1 built, awaiting its host run; Phase 2 designed; Phase 3 waits on Plan 00209)
**Created**: 2026-10-22
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants opencode, the open-source terminal coding agent
([anomalyco/opencode](https://github.com/anomalyco/opencode), formerly `sst/opencode`), as an
extra team worker for GitHub issues, plans and suggestions. That needs two things: opencode
installed the way this repo installs every pinned upstream binary, and opencode sessions able
to join an agent-bus team, be woken by deliveries and answer.

Phase 1 is the install: an optional play, pinned by version and sha256, for the login user,
with no credential in the repo. Phase 2 is the bus membership design, written against agent
bus v2 (`teamchat`, [Plan 00209](../00209-agent-bus-v2-in-go/)) because v1's wake path is
Claude Code's own and v1 is being replaced. Phase 3 builds that design, and **every Phase 3
task depends on Plan 00209, not on v1**.

Facts: [research/opencode-upstream-facts.md](research/opencode-upstream-facts.md) (upstream,
with sources) and [research/agent-bus-member-facts.md](research/agent-bus-member-facts.md)
(the bus, v1 and v2). Design:
[research/opencode-bus-member-design.md](research/opencode-bus-member-design.md).

## Goals

- `play-opencode.yml` installs the pinned opencode, checksum-verified, idempotent and fail-fast,
  tracked by `vars/version-pins.yml` so `scripts/check-pinned-versions.bash` reports its drift.
- A design that lets an opencode session be a v2 team member with the same HUMAN, PEER and PING
  authority as a Claude Code member, and the exact shape of the member-adapter seam v2 would
  need.

## Non-Goals

- Any bus code, on v1 or v2, in this plan's Phases 1-2.
- An opencode member on v1.
- Writing provider credentials, or choosing the provider, for the owner (decision O1).
- Any new owner-visible `teamchat` name or path: Plan 00209's Gate A is untouched.

## Tasks

### Phase 1: install

- [x] ✅ **Task 1.1**: Research the upstream release assets and their verification, where
  opencode keeps config and credentials, and its non-interactive and server modes
  ([research/opencode-upstream-facts.md](research/opencode-upstream-facts.md)).
- [x] ✅ **Task 1.2**: `playbooks/imports/optional/common/play-opencode.yml`: opencode 1.19.3,
  `opencode-linux-x64.tar.gz`, sha256 from the release API's digest, installed under
  `~/.local/lib/opencode-<version>/` and linked as `~/.local/bin/opencode`, other versions
  removed, version asserted; `/etc/opencode/opencode.json` managed config with `share`
  disabled and `autoupdate` off. No credential.
- [x] ✅ **Task 1.3**: Track the pin in `vars/version-pins.yml` (the manifest
  `scripts/check-pinned-versions.bash` reads), name the checksum in the `update-versions`
  skill, and document the play in `docs/playbooks.md`.
- [ ] 🔄 **Task 1.4**: Host run: queued in `CLAUDE/Plan/meta-deploy.list`. Done when the play
  has run on the host and `opencode --version` there prints the pin.

### Phase 2: bus membership design (no code)

- [x] ✅ **Task 2.1**: Read the bus as it is (pingbus skill, `files/` and `src-go/`, the wake
  paths, Plan 00161) and v2 (Plan 00209 on F44 and the DESIGN and SPEC files on
  `feat/bus-v2-n3`) ([research/agent-bus-member-facts.md](research/agent-bus-member-facts.md)).
- [x] ✅ **Task 2.2**: Write the design: join, wake, deliver, reply, authority, the differences
  from a Claude Code member, the adapter seam and its shape
  ([research/opencode-bus-member-design.md](research/opencode-bus-member-design.md)).
- [ ] ⬜ **Task 2.3**: Owner decisions O1-O7 below answered.

### Phase 3: build (depends on Plan 00209, not on v1)

Each task names the Plan 00209 work it waits for. None starts before the owner has answered
O2 and O4.

- [ ] ⬜ **Task 3.1**: Put the seam (design section 5) to Plan 00209 so n3e, the Claude Code
  delivery slice of N3, is built as the first adapter. **Depends on** Plan 00209 Task 3.2
  (n3e) not yet built. If n3e lands first without it, this task becomes carving it out.
- [ ] ⬜ **Task 3.2**: The opencode adapter in v2's member, with a fake opencode server for the
  tests and host checks H-OC1 to H-OC5 (design section 7). **Depends on** Plan 00209 N3
  (Task 3.2) merged.
- [ ] ⬜ **Task 3.3**: The opencode member kind where O2 places it: an LXC guest (**depends
  on** Plan 00209 Task 3.3, N3's LXC members), or a host member (**depends on** Plan 00209
  Task 5.1, seam 56, and its **Gate B**), or the ccy image (**depends on** Plan 00209 N4,
  Task 4.1's v2 kit in the image, and a CCY version bump).
- [ ] ⬜ **Task 3.4**: Acceptance: one opencode worker on a throwaway v2 team answers a HUMAN
  message, a PEER message and a closed-verb ping from the coordinator, and its HUMAN items get
  read receipts only after proof. **Depends on** Tasks 3.2-3.3.

## Owner decisions

Each has the default the design assumes.

- **O1 Provider and credentials.** Which model provider opencode workers use, and how its key
  reaches them. Default: the owner signs in once per place a worker runs with
  `opencode auth login` (kept in `~/.local/share/opencode/auth.json`, outside the repo); a
  vault variable written by a play only if a worker must be provisioned unattended.
- **O2 Where workers run.** Default: an LXC guest, because an opencode worker runs outside the
  hooks daemon and allows nearly everything by default; not the host login user (Plan 00209
  seam 56 and Gate B), and not the ccy image unless O2 says so.
- **O3 Worker permissions.** opencode's `permission` rules are a worker's only guard rail.
  Default: a per-worker policy that denies `git push --force`, `git reset --hard`, history
  rewrites and reads outside the checkout, set by the play that provisions the worker.
- **O4 The seam and its launch spelling.** Default: v2 defines the in-process adapter seam of
  design section 5 now, in n3e, and `teamchat run` gains an opencode launch mode beside
  `--claude` and `--plain` (spelling, e.g. `--opencode`, put with v2's names).
- **O5 Role.** Default: opencode members are workers only, never a team's coordinator.
- **O6 Claude Code files.** opencode reads `CLAUDE.md` and `.claude/skills/` by default.
  Default: keep it (the repo's rules reach the worker), knowing the Claude-specific skills,
  including the bus skill, are written for Claude Code.
- **O7 Managed config.** Default: keep the play's `/etc/opencode/opencode.json` (`share`
  disabled, `autoupdate` off) for every opencode session on the host.

## Success Criteria

- [ ] The play has run on the host and `opencode --version` prints the pin (Task 1.4).
- [x] `scripts/check-pinned-versions.bash` lists `opencodeVersion` against `anomalyco/opencode`
  (checked in the container: `1.19.3`, up to date).
- [x] The design covers join, wake, delivery, reply, authority, the differences and the seam,
  and leaves Gate A untouched.
- [ ] The owner has answered O1-O7.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00215-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 play and Phase 2 design on branch `feat/opencode`.
