# Plan 00163: ccy forwards one key from the owner's ssh-agent, not the whole agent

**Status**: In Progress
**Created**: 2026-10-08
**Owner**: joseph
**Priority**: High

## Overview

A ccy session gets SSH either as a key file (mounted, then unlocked in the container, which
asks for a passphrase) or as the owner's whole ssh-agent (`--ssh-agent`, every key in it
reachable from the container). A headless launch cannot type a passphrase, so a
passphrase-protected key file refuses it; forwarding the whole agent would hand every key to
every container, which the owner rejects. Plan 00161's U20 acceptance found this: its
headless seats reuse this checkout's saved Quick Launch choice, a key file with a passphrase.

The owner asked for ssh-agent support "as a first class way to get [a] specific key". So ccy
forwards a **filtered agent**: a socket of its own, in front of the owner's agent, that lists
and signs with only the chosen key and refuses everything else (adding, removing, listing
other keys, locking, extensions). No passphrase is stored or typed; the key stays in the
owner's agent; the container can use that one key and no other.

The research behind this (how an unattended session already gets one key elsewhere: a
vaulted passphrase file through the session-restore askpass, server profile only) is kept
untracked, as it concerns a private repository.

## Goals

- A launch whose selected key file needs a passphrase, when the host's ssh-agent holds that
  same key (matched by public key), gets the filtered agent offering that one key instead of
  the file: headless and interactive alike, with no prompt.
- The container's ssh, git and commit signing work with it exactly as with a forwarded agent
  holding one key.
- Saved Quick Launch choices and restore records keep naming the key file; nothing the owner
  has saved changes.

## Non-Goals

- Changing plain `--ssh-agent` (the whole agent, as today).
- Storing a passphrase on the desktop (the server's restore askpass is unchanged).

## Tasks

### Phase 1: the filtered agent

- [x] ✅ **Task 1.1**: a stdlib Python helper, test first: listens on a new owner-only socket,
  relays to `SSH_AUTH_SOCK`, answers REQUEST_IDENTITIES with only the allowed public key,
  forwards SIGN_REQUEST only for that key blob, and answers FAILURE to every other message
  (add, remove, lock, unlock, extensions). Tests against a real `ssh-agent` holding two keys.
  `helpers/ssh_agent_filter/ssh_agent_filter.py` (keys allowed by SHA256 fingerprint),
  `tests/helpers/ssh_agent_filter/test_ssh_agent_filter.py`.
- [x] ✅ **Task 1.2**: its lifetime: started by the launcher before the container, on the
  runtime directory, and stopped with the session (including a restored or detached one).
  Decide and record how in this plan's journal, from how the launcher runs the container.
  A child of the launcher, stopped by its EXIT trap and `cleanup`, and by itself when the
  launcher's pid is gone (journal decision, T1.2). Installed by `play-claude-yolo.yml`'s
  shared-library loop beside the launcher's libraries.

### Phase 2: the launcher

- [x] ✅ **Task 2.1**: when a selected key file needs a passphrase and the host agent holds
  its public key, mount the filtered agent's socket (as `--ssh-agent` mounts the agent) in
  place of the key file; say so in one line. Otherwise behave as today. CCY version bump.
  `ccy_agent_forward_select` in `lib/ssh-handling.bash` decides for the whole selection
  (every held passphrase key, when that leaves none needing a passphrase, never beside
  `--ssh-agent`); `ccy_agent_filter_start`, the headless, restart and restore checks
  (`ccy_restart_keys_unattended`) and U20 all take it. The fingerprint comes from the key
  file itself and a disagreeing `.pub` refuses the match. CCY 3.87.1 (review fixes).
- [x] ✅ **Task 2.2**: commit signing with the filtered agent signs with that key (the
  forwarded-agent path in `ssh-handling.bash`), and a key the agent does not hold still
  fails as today. `configure_git_signing` takes the one-key agent's socket and names the
  key's public half (`key::`) as that agent offers it.
- [x] ✅ **Task 2.3**: `docs/ccy.md` and `docs/ccy-changelog.md`; `--help`.
- [x] ✅ **Task 2.4**: Plan 00161's U20 prerequisite accepts a passphrase key that the agent
  running meta-deploy holds, and says to `ssh-add` it otherwise. It calls the installed
  ccy's own `ccy_restart_keys_unattended` over the whole saved selection.

### Phase 3: proof

- [x] ✅ **Task 3.1**: `qa-reviewer` over the plan diff, findings resolved: FIX-BEFORE-MERGE
  (the whole-selection decision, the `.pub` match, version coverage, four nits), all fixed in
  CCY 3.87.1. [report](subagent-reports/261008-qa-reviewer-opus.md).
- [ ] 🧑 **Task 3.2**: HOST: Plan 00161's acceptance (M2) through meta-deploy, with the key in
  the owner's agent; the seats push and sign with it. M2.0–M2.10 passed on 26-10-09 with
  every seat launched headless on the owner's passphrase key and no prompt. Plan 00161's
  M2.0b now proves the rest inside a seat (one key listed, `ssh-add -D` and a second key
  refused, a signature made); it runs on the next meta-deploy. A push is not exercised:
  M2's sessions have no network.

## Success Criteria

- [ ] Inside a session, `ssh-add -L` lists exactly the chosen key; adding, removing or using
  any other key fails.
- [x] A headless launch with a passphrase key the host agent holds starts with no prompt
  (Plan 00161 M2, 26-10-09).
- [ ] `./scripts/qa-all.bash` green.

## Delivery & Milestones

- Delivery commits are recorded here as they land.
