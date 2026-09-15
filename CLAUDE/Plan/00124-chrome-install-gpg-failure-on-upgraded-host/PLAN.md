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
- [x] ✅ **Task 2.2**: `helpers/rpm_keys/subkeys.py` — `key_ids`, `short_id`,
  `read_key`, `read_armour`, `installed_envelopes`, `key_armour`,
  `needs_refresh`, `report`, `main`. Injected runner throughout.
- [x] ✅ **Task 2.3**: Verify the gpg parsing against the **real** published key,
  not only fixtures — the one part unit tests cannot vouch for.

### Phase 3: Wire it into the play

- [x] ✅ **Task 3.1**: `play-browsers.yml` — fetch the key to `/etc/pki/rpm-gpg/`,
  run the helper, remove what it names, re-import.
- [x] ✅ **Task 3.2**: Point the repo's `gpgkey` at the local copy, so dnf validates
  against the same bytes the play reasoned about.
- [ ] ⬜ **Task 3.3**: QA green, commit, push.

### Phase 4: Confirm on the affected host

- [x] ✅ **Task 4.1**: Re-run on the F41-upgraded laptop; Chrome installs. Confirmed
  by the owner — the key check cleared and the run carried on past Chrome. This
  is the first end-to-end proof of the rpm half, which no container can give.
- [ ] ⬜ **Task 4.2**: A second run reports the key tasks as **ok**, not changed.
- [ ] ⬜ **Task 4.3**: `qa-reviewer` agent, then close issue #45.

## Success Criteria

- [x] Chrome installs on the upgraded host with `gpgcheck` on.
- [ ] A repeat run is green and reports no change for the key tasks.
- [ ] `rpm -qa gpg-pubkey` afterwards holds a Google key carrying
  `FD533C07C264648F`.
- [ ] Nothing is removed on a host whose key was already current.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00124-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Cause 1 (dnf5 `@commandline`) fixed in `31191b5f`, confirmed landed by the
  issue #45 comment.
- Cause 2 (stale primary, missing subkey) — this plan.
