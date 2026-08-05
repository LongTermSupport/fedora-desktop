# Plan 00068: Document the CCY System

**Status**: Complete
**Created**: 2026-08-05
**Completed**: 2026-08-05
**Owner**: joseph
**Priority**: High

## Overview

CCY (`ccy` — Claude Code YOLO) is the daily-driver development environment this repo
provisions: a containerised Claude Code running with `--dangerously-skip-permissions`
inside a rootless Podman container, with project-local state, a named-token pool, SSH
key handling, network attachment, per-project custom images, and an in-container
supervisor seam. The launcher alone is ~141 KB of Bash plus ~170 KB of shared libraries.

Despite that, **there is no user-facing documentation for it**. `docs/features/README.md`
still lists CCY under "Documentation for these features is planned". The only existing
coverage is fragmentary and aimed elsewhere:

| Existing artifact                             | Audience                | Covers                                   |
| --------------------------------------------- | ----------------------- | ---------------------------------------- |
| `docs/containerization.md#custom-dockerfiles` | user                    | custom Dockerfiles only                  |
| `docs/ccy-debug-mounts.md`                    | user                    | `CCY_EXTRA_MOUNTS` only                  |
| `files/opt/claude-yolo/docs/CCY-GUIDE.txt`    | **agent, in-container** | container internals, Dockerfile priority |
| `CLAUDE/ContainerRules.md`                    | **agent**               | edit-only rule, ctrl+z patch             |

Nothing explains — to a human, before they launch it — what CCY is, how to install it,
how the token pool works, what the security/mount model actually exposes, how the
hooks-daemon supervisor integration works, or how to get out of trouble. This plan adds
that guide and wires it into the docs index.

## Goals

- Add `docs/ccy.md`: a complete user-facing guide to the CCY system.
- Document the supervisor integration (`--supervise`, `.claude/ccy/ccy.env`,
  `claude-supervise.py`, `ccy.deploy_supervisor`) — the part that makes CCY most
  powerful and that is currently only documented inside the vendored hooks-daemon.
- Document the security/mount model explicitly (what IS and is NOT exposed to the
  container), since this is a `--dangerously-skip-permissions` tool.
- Wire the new doc into `docs/README.md`, `docs/features/README.md`, and
  `docs/containerization.md`, and retire the "documentation is planned" placeholder.
- Keep every claim grounded in the source (launcher help text, `entrypoint.sh`,
  `lib/*.bash`, `play-claude-yolo.yml`) — no invented behaviour.

## Non-Goals

- **No code changes.** This is documentation only; `claude-yolo` is not touched, so no
  CCY version bump is required.
- **No duplication** of the custom-Dockerfile workflow already in
  `containerization.md`, the debug mounts already in `ccy-debug-mounts.md`, or the
  agent-facing internals in `CCY-GUIDE.txt` — link to them instead.
- Not documenting the hooks-daemon itself beyond its CCY-facing seam (it is a vendored
  upstream dependency with its own docs).

## Tasks

### Phase 1: Ground the facts

- [x] ✅ **Task 1.1**: Confirm the documentation gap (docs tree, grep for `ccy`)
- [x] ✅ **Task 1.2**: Extract the authoritative CLI surface from the launcher help text
- [x] ✅ **Task 1.3**: Establish the mount/state model (`DOCKER_MOUNTS`, `entrypoint.sh`
  symlink, `SSH_MOUNTS`, `CCY_EXTRA_MOUNTS`)
- [x] ✅ **Task 1.4**: Establish the supervisor wiring (`ccy.env` precedence, entrypoint
  `exec` wrap, `ccy.deploy_supervisor`)

### Phase 2: Write the guide

- [x] ✅ **Task 2.1**: Write `docs/ccy.md`
- [x] ✅ **Task 2.2**: Cover the supervisor integration as a first-class section
- [x] ✅ **Task 2.3**: Cover the security model and the version/update machinery

### Phase 3: Integrate and verify

- [x] ✅ **Task 3.1**: Add `docs/ccy.md` to `docs/README.md` (topic index + purpose
  sections + quick reference)
- [x] ✅ **Task 3.2**: Replace the "planned" placeholder in `docs/features/README.md`
- [x] ✅ **Task 3.3**: Cross-link from `docs/containerization.md` and
  `docs/ccy-debug-mounts.md`
- [x] ✅ **Task 3.4**: Run QA: `./scripts/qa-all.bash` — passed (417 files)
- [x] ✅ **Task 3.5**: Verify every internal cross-reference resolves — also repaired
  three pre-existing broken `installation.md` anchors surfaced in `docs/README.md`

### Phase 4: Adversarial review (multi-agent)

- [x] ✅ **Task 4.1**: Ground-truth verification of every claim in `docs/ccy.md`
  against the launcher, entrypoint, libs, playbook and supervisor source — 7 sonnet
  verifiers, one per claim group
- [x] ✅ **Task 4.2**: Clarity/honesty/structure review — 3 fable reviewers
- [x] ✅ **Task 4.3**: Apply corrections; re-run QA and the link check

Six high-severity defects were found and independently re-verified against source
before being fixed (full detail in the journal): an invented `--no-cache` flag, a
wrong launch-sequence order, an omitted Wayland/X11 + `/dev/dri` exposure in the
security model, a "warns" claim where the launcher actually hard-blocks, backwards
supervisor dry-run/armed guidance, and containment framing that overclaimed given
the account-wide SSH key and `gh` token.

## Success Criteria

- [x] `docs/ccy.md` exists and covers: what/why, install, quick start, architecture,
  security model, tokens, full flag reference, per-project config, supervisor,
  networking, updates, troubleshooting
- [x] No "documentation is planned" placeholder remains for CCY
- [x] Every cross-reference link resolves to a real file and anchor
- [x] QA passes (`./scripts/qa-all.bash`) — exit 0, 417 files
- [x] No duplication of content that already lives in another doc
- [x] Every factual claim traced to source and adversarially re-checked

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00068-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: `e85368fc` (non-durable, hourly at :23)
