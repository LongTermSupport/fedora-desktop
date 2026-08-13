# Plan 00071: encrypted claude transcripts at rest

**Status**: In Progress
**Created**: 2026-08-13
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code persists every conversation as plaintext JSONL at a predictable path. Those
transcripts accumulate whatever passed through the context — API keys, tokens, `.env`
contents, command output, customer data — and nothing bounds how long they sit there. Anyone
who can read the filesystem as this user gets a searchable archive of every secret the
assistant has ever seen.

There are **two** stores, not one, and they need separate answers:

| Store               | Written by                | Path                                                                            |
| ------------------- | ------------------------- | ------------------------------------------------------------------------------- |
| Desktop Claude Code | host, no container        | `~/.claude/projects/<slug>/*.jsonl`                                             |
| CCY (containerised) | rootless podman container | `<repo>/.claude/ccy/projects/-workspace/*.jsonl` (gitignored, in the repo tree) |

CCY does **not** bind-mount the host `~/.claude`; `entrypoint.sh` symlinks `/root/.claude` to
`/workspace/.claude/ccy`, so container session state lands *inside the project working tree*.

The proposal under investigation is encryption at rest — ciphertext on disk, with the key
supplied interactively at CCY launch (the existing SSH-key prompt is the model), so a browsing
attacker sees only opaque blobs. This plan is **research-gated**: before any design is
committed, it establishes what Anthropic officially supports, what prior art exists, which
Linux mechanisms actually work under rootless podman with SELinux enforcing, and — critically
— whether at-rest encryption of a directory that stays *decrypted and mounted for the whole
session* defeats the threats the user actually faces, or merely duplicates the full-disk
encryption already present.

## Goals

- Establish, from primary sources, the complete inventory of plaintext Claude Code state and
  Anthropic's official position on its storage, retention and redaction.
- Produce an honest threat-model verdict: which attacks at-rest encryption defeats here, and
  which it does not.
- Select a design that measurably reduces exposure, deployable as Ansible IaC, fail-fast, and
  integrated into the CCY launch flow.
- Cover both stores (desktop and CCY) or state explicitly why one is out of scope.

## Non-Goals

- Encrypting the whole home directory or replacing existing full-disk encryption.
- Patching Claude Code itself, or any change that depends on unreleased upstream behaviour.
- Retaining transcripts that are unrecoverable after key loss without the user knowingly
  accepting that trade-off.

## Tasks

### Phase 1: Research and decision gate

- [x] ✅ **Task 1.1**: Confirm where CCY and desktop Claude Code state actually lives, with
  file:line evidence
- [ ] 🔄 **Task 1.2**: Run the research workflow — official guidance, prior art, Linux
  mechanisms, adversarial threat model
- [ ] ⬜ **Task 1.3**: Produce three independent designs and judge them on security and
  operability lenses
- [ ] ⬜ **Task 1.4**: Record the recommendation, the options rejected, and why

<!-- Phases 2+ are written at the decision gate, once Task 1.4 lands. -->

## Success Criteria

- [ ] Threat-model verdict recorded, including any finding that the proposal is not worth
  building as originally framed
- [ ] Chosen design deployable via a playbook, idempotent, fail-fast, no silent plaintext
  fallback
- [ ] QA passes (`./scripts/qa-all.bash`)

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00071-Journal-YY-MM-DD.md. -->

- Plan opened; research workflow `wf_487ae6c4-73e` dispatched
