# Plan 00124: chrome install gpg failure on upgraded host

**Status**: In Progress
**Created**: 2026-09-15
**Owner**: joseph
**Priority**: High

## Overview

`run.bash` stops at Google Chrome on a host upgraded from Fedora 41 (issue #45).
The install fails its OpenPGP check, and there are **two independent causes**
stacked on top of each other — fixing the first exposed the second.

The first is dnf5. Handing dnf a package URL puts it in the synthetic
`@commandline` repo, and dnf5 validates a package against the keys configured *on
its repo*; `@commandline` has none. dnf4 consulted the RPM keyring instead, which
is why this worked for years. Declaring Google's repo and installing
`google-chrome-stable` from it fixes that, and the fix is confirmed landed — the
failure now names repo `google-chrome` rather than `@commandline`.

The second is key staleness. Google's `linux_signing_key.pub` is ONE primary
(`7721F63BD38B4796`) carrying eight signing subkeys, and the current Chrome package
is signed by subkey `FD533C07C264648F` — verified by extracting tag 268
(`RPMSIGTAG_RSAHEADER`) from the repo's own package. Both `rpm` and `dnf` decide a
key is "present" by its **primary** id, so a key imported under F41 is never
refreshed and the newer subkey never arrives. dnf5 then reports both halves of a
contradiction in one message, and both halves are true:

```
Public key "…/linux_signing_key.pub" is already present, not importing.
OpenPGP check for package "google-chrome-stable-…" from repo "google-chrome" has failed: Public key is not installed.
```

`rpm_key: state=present` cannot break that deadlock — it is the module whose
"already present" check *is* the problem. The stale key has to be removed and
re-imported, which means deciding first whether it is stale, so the play stays
idempotent.

## Goals

- `run.bash` installs Google Chrome on a host upgraded from F41, with signature
  checking left **on**.
- The key refresh is idempotent: a host whose key is already current reports no
  change, on this run and every run after it.
- The removal can only ever name the key being managed. It is identified by **id**,
  derived from the published key itself — never by a description match.

## Non-Goals

- Disabling `gpgcheck`, or any use of `disable_gpg_check`. The whole failure is a
  verification failure; turning verification off would hide it, not fix it.
- A generic "refresh every vendor key" mechanism. Chrome is the one key with this
  problem; the helper is reusable but nothing else is wired to it (YAGNI).
- Deleting a rotated old primary. If Google ever rotates, the new key is imported
  and the old one is left in the keyring, where it is harmless.

## Tasks

### Phase 1: Establish the facts

- [x] ✅ **Task 1.1**: Confirm which cause is live — issue #45's comment names repo
  `google-chrome`, so the dnf5 fix landed and a second cause remains.
- [x] ✅ **Task 1.2**: Identify the package's actual signer by extracting the RPM
  signature header from the repo's own package. Signer: `FD533C07C264648F`.
- [x] ✅ **Task 1.3**: Confirm that signer is a subkey of the published primary and
  not a different key — it is subkey 7 of 8 under `7721F63BD38B4796`.
- [x] ✅ **Task 1.4**: `triage.bash` — read-only host triage reporting all three
  outcomes per probe (failed / ran and found nothing / found something).

### Phase 2: The decision, as a tested helper

- [x] ✅ **Task 2.1**: `tests/helpers/rpm_keys/test_subkeys.py` first.
- [x] ✅ **Task 2.2**: `helpers/rpm_keys/subkeys.py` — `key_ids`, `read_key`,
  `read_armour`, `installed_envelopes`, `key_armour`, `needs_refresh`, `report`,
  `main`. Injected runner throughout. (`short_id` was here until Task 4.4 removed
  the package-name lookup that needed it.)
- [x] ✅ **Task 2.3**: Verify the gpg parsing against the **real** published key,
  not only fixtures — the one part unit tests cannot vouch for.

### Phase 3: Wire it into the play

- [x] ✅ **Task 3.1**: `play-browsers.yml` — fetch the key to `/etc/pki/rpm-gpg/`,
  run the helper, remove what it names, re-import.
- [x] ✅ **Task 3.2**: Point the repo's `gpgkey` at the local copy, so dnf validates
  against the same bytes the play reasoned about.
- [x] ✅ **Task 3.3**: Declare `gnupg2`, which both the key check and `rpm_key` itself
  hard-require and nothing in the repo installed — in this play and in
  `play-nvidia.yml`, which has the same `rpm_key` call and runs independently.
- [x] ✅ **Task 3.4**: A post-condition. The play re-asks the helper after the import
  and requires `none`, so a refresh that did not take is loud on the first run
  rather than an invisible erase-and-reimport loop reporting green.
- [x] ✅ **Task 3.5**: QA green, commit, push.

### Phase 3b: Review findings

`qa-reviewer` returned FIX-BEFORE-MERGE on `8efed4dd`+`b6e6f395`; both safety claims
this plan makes were demonstrably breakable. Report:
`untracked/agent-reports/260915-review-00124-opus-5.md` (untracked — raw agent output).

- [x] ✅ **Task 3b.1**: A two-certificate published key made the helper refresh, erase
  and re-import **for ever** — the primary was read from the first certificate
  while subkeys accumulated from all of them. Parsing now stops at the second
  `pub`. This is the exact churn the gate exists to prevent, and a vendor rotating
  a primary ships precisely that bundle.
- [x] ✅ **Task 3b.2**: The erase set was identity-checked for its FIRST member only,
  so a second package at the same short id was erased unexamined. `needs_refresh`
  now takes every package found at the id and owns the erase set, so only keys
  whose primary matches the published one can appear in it.
- [x] ✅ **Task 3b.3**: Expired and non-signing subkeys were counted. Five of Google's
  eight are expired; a host missing only those cannot fail for that reason, so
  refreshing would be churn justified by a false reason. gpg's validity and
  capability fields are now read.
- [x] ✅ **Task 3b.4**: `triage.bash` — its three-outcome probe reported "ran cleanly
  and found nothing" as `COMMAND FAILED` for every grep-terminated probe, and it
  ignored `CLAUDE/PlanScriptStandards.md` R1/R2/R4/R7/R9/R10 with no exception
  annotation. Rebuilt on `_planlib.inc.bash`: `plan_init`, a real
  `plan_require_host` guard (verified firing — it refuses in this container with a
  reason), `plan_gather_leg` per section so an unanswered question fails the run
  rather than passing quietly, and the probe bodies split into `probe-chrome.bash`
  per `CLAUDE/PlanTriage.md`. The probe now takes the exit status that means "ran
  fine, matched nothing" from its caller, so grep's exit 1 and `rpm -q`'s exit 1
  read as findings while exit ≥ 2 stays a failure. Repointed at Task 4.2: it checks
  the installed key carries `FD533C07C264648F` and that the deployed key file
  matches the published one.

### Phase 4: Confirm on the affected host

- [x] ✅ **Task 4.1**: Re-run on the F41-upgraded laptop; Chrome installs. Confirmed
  by the owner — the key check cleared and the run carried on past Chrome. This
  is the first end-to-end proof of the rpm half, which no container can give.

- [x] ✅ **Task 4.2**: **Answered on the host, 2026-09-17.** `deploy.bash` runs the play
  twice by design — leg 1 converges, leg 2 is this task's second run — and the two PLAY
  RECAPs are the evidence: `ok=14 changed=2 failed=0`, then `ok=14 changed=0 failed=0`.
  Both legs passed and `acceptance.bash` then rendered ACCEPTED in the same batch.

  This is the measurement the task's own warning demanded: the refresh path erases a key
  and re-imports it, so a broken idempotency gate would not fail — it would erase and
  re-import for ever while reporting green. `changed=0` on the second pass is what rules
  that out, and it could only ever be read on a real host. A second run reports the key
  tasks as **ok**, not changed.
  `triage.bash` section 2 answers this without needing the run watched: it identifies
  every installed `gpg-pubkey` package by the primary inside its armour, confirms the
  one carrying `7721F63BD38B4796` also carries `FD533C07C264648F`, and compares the
  deployed key file byte-for-byte against what Google publishes now. A table in the
  report maps each fact to the play task it predicts. Run it, then run the play —
  agreement is the evidence, rather than a human watching two runs and remembering
  what the first said.

  - **First attempt aborted, and the cause was in this plan's own code** — see
    Task 4.4. Re-run `deploy.bash` now that it is fixed.

### Phase 4b: The Fedora 44 package-naming break

- [x] ✅ **Task 4.4**: The Task 4.2 run failed at `Verify The Imported Google Key Is Now Current` with `RPM-KEY-ACTION import` on a host whose key was present and
  working. `installed_envelopes` selected packages by rpm's `%{version}` — the
  primary's short id under rpm 4 and 5, the key's **full fingerprint** under rpm 6,
  which is what Fedora 44 ships. Identity was decided from a string rpm renames
  between major versions. It now returns every installed `gpg-pubkey` package and
  `needs_refresh` decides identity from the primary parsed out of each key's own
  armour. `short_id` has no caller left and is deleted. `probe-chrome.bash` and
  `acceptance.bash` carried the same derivation and would have vouched for the fix
  while checking the wrong thing; both now select by armour too. The play is
  unchanged — its post-condition was correct and caught this exactly as designed.
- [x] ✅ **Task 4.3a**: `qa-reviewer` agent — FIX-BEFORE-MERGE, 8 findings, all acted
  on. Report:
  [subagent-reports/260915-qa-reviewer-opus-5.md](subagent-reports/260915-qa-reviewer-opus-5.md).
  The destructive path came back clean and both previously-broken safety claims were
  re-verified **by mutation** rather than by reading. What it found instead was that
  **Chrome's own `%post` re-adds the repository**: it writes
  `/etc/default/google-chrome` with `repo_add_once="true"` when absent, then rewrites
  `/etc/yum.repos.d/google-chrome.repo` with a NETWORK `gpgkey`. So on any host where
  Chrome had never been installed, Task 4.2 would have found `Add Google Chrome Repository` reporting **changed** on every run — and the play's comment claiming dnf
  could not validate against a different fetch of the key was false in exactly that
  state. The play now writes that file first and owns the repo outright
- [ ] ⬜ **Task 4.3b**: Close issue #45 — after Task 4.2 confirms on the host.

## Success Criteria

**The host criteria below are a script, not an instruction.** Run `deploy.bash` then
`acceptance.bash` in this folder — or `untracked/meta-deploy.bash` to run this plan
alongside the others waiting. `deploy.bash` runs the play twice, because idempotency is a
property of the second run and no `--check` pass can stand in for it; `acceptance.bash`
carries eight COVERAGE-registered checks and proves the key works by making `rpm` verify
a real package rather than by observing that the key is present. Closing the issue stays
yours: the ACCEPTED message says so rather than implying the gate covered it.

- [x] Chrome installs on the upgraded host with `gpgcheck` on.
- [ ] A repeat run is green and reports no change for the key tasks.
- [ ] `rpm -qa gpg-pubkey` afterwards holds a key whose primary is
  `7721F63BD38B4796` and which carries `FD533C07C264648F` — identified by its
  armour, under whatever name rpm gave the package.
- [ ] Nothing is removed on a host whose key was already current.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00124-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Cause 1 (dnf5 `@commandline`) fixed in `31191b5f`, confirmed landed by the
  issue #45 comment.
- Cause 2 (stale primary, missing subkey) — this plan.
