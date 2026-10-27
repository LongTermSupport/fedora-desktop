# Plan 00230: Optional plays: fail fast, ask for missing settings, friendly errors

**Status**: In Progress
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: High

## Overview

The owner ran the optional plays on a consumer laptop install (`./run.bash --consumer`) and hit
four defects, each found only by running there: play-nvidia asserted the Secure Boot MOK
password near the end, after installing the driver (fixed in 6b99b1247); the fix's
`ansible-vault encrypt_string` ran outside the checkout, so ansible.cfg's relative vault
password file was not found, and `no_log` hid why (2b074fb8a); play-qobuz hard-failed on empty
Last.fm details (76b5b9094); play-photography died on a 404 icon download (6192e52fc).

The owner's request: "generally improve UX on these plays, interdependencies/fail fast, human
friendly error handling, check for config/vault, create on demand. DBF this stuff." This plan
runs the Defence Before Fix method over those defects: each class gets a rule in the static
Ansible gate (`src-go/qa/ansible`, `scripts/qa-ansible.bash`), proven red on the original
defect and green on the fix, every instance is fixed, and a shared task file asks for a
missing secret and saves it vault-encrypted, so no play hand-rolls that sequence again.

## Goals

- A rule per writable defect class, failing (not warning) in `qa-ansible.bash`, each with a
  stable ID that resolves to a section of `CLAUDE/QA.md`.
- Every instance in first-party Ansible fixed, or referred to the owner with its count.
- One shared task file for "ask for a secret twice, vault-encrypt it, save it to
  localhost.yml, use it for this run", used by every play that needs a secret it can ask for.

## Non-Goals

- Redesigning a play or changing what it installs.
- `run.bash`, the consumer-mode summary and the sudo-once work (Plan 00228).

## Tasks

### Phase 1: Defences (red)

- [x] ✅ **Task 1.1**: Attribute the defects to classes and dispatch the independent search
  ([SEARCH-independent.md](SEARCH-independent.md)).
- [x] ✅ **Task 1.2**: Rules in `src-go/qa/ansible/playux.go`, proven against
  `tests/fixtures/ansible-play-ux/` (`setting-checked-late`, `vault-secret-by-hand`,
  `ansible-cli-without-chdir`, `no-log-hides-failure`, `probe-before-install`,
  `module-built-for-running-kernel`).
- [x] ✅ **Task 1.3**: Commit the defences red, before their instances are fixed.

### Phase 2: Sweep and fix

- [x] ✅ **Task 2.1**: Shared task files: `tasks/ask-and-save-secret.yml` (with its attempt file),
  `tasks/secure-boot-mok-password.yml` and `tasks/boot-kernel-version.yml`.
- [ ] 🚫 **Task 2.2**: Fix every instance the rules and the search found. 36 of 40 fixed; 4 (the
  `github_ssh_passphrase` checks in play-claude-yolo and play-github-cli-multi, two core
  plays outside this plan's brief) wait for the owner (journal, P2).
- [x] ✅ **Task 2.3**: Document each rule in `CLAUDE/QA.md`.

### Phase 3: Review and report

- [ ] ⬜ **Task 3.1**: Conformance review and the qa-reviewer agent (the coordinator's run).
- [x] ✅ **Task 3.2**: The DBF record: the journal (P1, P2) and the worker's reply to the
  coordinator.
- [ ] ⬜ **Task 3.3**: Host run: `changed-plays` in `meta-deploy.list`.

## Success Criteria

- [ ] `scripts/qa-ansible.bash` green on the tree (red only on the 4 referred findings), red on
  each original defect (done: 6b99b1247^, 2b074fb8a^, 08ea913a5^).
- [x] `ansible-playbook --syntax-check` passes on every play touched.
- [ ] QA passes (`./scripts/qa-changed.bash`; `./scripts/qa-all.bash` by the coordinator).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00230-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Five rules committed red: 720d576e7
- Shared asker and MOK step, rules widened by the search: 1fd803864
- module-built-for-running-kernel committed red: 6aa6cf70a
