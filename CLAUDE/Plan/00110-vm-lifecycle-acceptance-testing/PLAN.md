# Plan 00110: VM lifecycle acceptance testing

**Status**: In Progress
**Created**: 2026-09-13
**Owner**: joseph
**Priority**: High

## Overview

This repo provisions Fedora desktops and servers, and has no automated proof
that a fresh install works. Verification has always meant a human finding a
spare machine, so it mostly does not happen: Plans 00063, 00079 and 00092 are
all stalled on "Blocked — HOST ACTION", one of them since July, with the code
written and only the proof outstanding. A repo whose entire product is a
provisioning run cannot demonstrate that the run succeeds.

This plan builds full fresh-install lifecycle acceptance testing against VMs,
for both the `server` and `desktop` provisioning profiles. A freshly installed
VM is snapshotted once and reused as the base for every run, so a test pays for
provisioning rather than for an OS install. The base is kept current by a
DNF-update-and-re-snapshot cycle under a TTL, with a longer TTL — and, where a
reliable upstream signal exists, a detected change to the Fedora install tree
itself — forcing a full reinstall.

It runs on the host, where the hypervisor is, but is triggerable from inside a
CCY container through a host-action bridge: a file-spool request, a systemd
`--user` path unit, and a closed verb allowlist. The container never reaches the
VM and holds no credentials; it asks for a named scenario and reads back a
transcript.

## Goals

- A repeatable, first-class acceptance run that installs Fedora fresh in a VM,
  provisions it with this repo, and returns a machine-readable verdict.
- Cover both `server` and `desktop` provisioning profiles.
- Reuse a snapshotted base install so a run does not pay for an OS install.
- Keep that base current against upstream packages, and force a rebuild when it
  is too old or when the install tree it came from has changed.
- Make the whole thing triggerable from a CCY container without granting the
  container a socket, an SSH key, or any credential.
- Fail loudly and distinguishably: a run that did not execute must never be
  readable as a run that passed.

## Non-Goals

- No cloud provider. The hypervisor is local.
- No replacement for the existing plan-local `acceptance.bash` scripts; this is
  the lifecycle harness they can be run inside.
- No change to `run.bash`'s headless contract, which is reused as-is.
- No arbitrary command execution from the container. The verb set is the
  security boundary and stays closed.
- No assertion of things only a human can judge on a screen; where the desktop
  profile cannot be checked headlessly, the design says so rather than
  pretending otherwise.

## Tasks

Task detail lives in [DESIGN.md](DESIGN.md) §9; this is the tracking view.
Phase 1 needs no VM and can run in parallel with Phase 0. **U8 selects the LUKS
unlock route, so Phase 5 cannot start until Phase 0 answers it.**

### Phase 0: Host triage and the decision gate (HOST) — complete

- [x] ✅ **T0.1**: `triage.bash` on `_planlib.inc.bash` — probe U1, U2, U4, U5,
  U7, U8 plus KVM, reflink, space, `virtiofsd`
- [x] ✅ **T0.2**: Decision gate — KVM present and headroom for three bases;
  refuse rather than run a TCG-emulated desktop nobody will wait for
- [x] ✅ **T0.3**: Record answers in `JOURNAL/`; correct DESIGN.md where reality
  differs

### Phase 1: The freshness engine (no VM; runs in a container) — complete

- [x] ✅ **T1.1**: `helpers/vmtest/upstream.py`, test-first — artefact identity
  and package revision from `.treeinfo`, `COMPOSE_ID`, `releases.json`, Bodhi,
  `repomd.xml`
- [x] ✅ **T1.2**: `helpers/vmtest/freshness.py`, test-first — the §4.4 policy,
  parametrised over the **whole** readable/unreadable matrix and asserted total
- [x] ✅ **T1.3**: `helpers/vmtest/scenarios.py` — manifest parsing, planned-check
  accounting
- [x] ✅ **T1.4**: `helpers/vmtest/probe_upstream.py` — thin executor, marker
  lines on stdout, diagnostics on stderr
- [x] ✅ **T1.5**: `vars/vm-test-scenarios.yml` — the tracked manifest
- [x] ✅ **T1.6**: QA; commit. **This phase alone answers "has upstream moved?"
  as a runnable command**

### Phase 2: The lab playbook

- [x] ✅ **T2.1**: `play-vm-test-lab.yml`, `scope: general` — libvirt/qemu stack
  by name only, no pinned versions
- [x] ✅ **T2.2**: linger, lab tree, `/dev/kvm` openability assertion (no `kvm`
  group — Phase 0 measured `0666`)
- [x] ✅ **T2.3a**: `scenarios.json` and `scenarios.allowlist` rendered from
  `vars/vm-test-scenarios.yml` through `validate_manifest.py`; the allowlist
  exists only when a scenario is runnable
- [x] ✅ **T2.3b**: deploy `files/home/.local/bin/vmtest` `0755`
- [x] ✅ **T2.4**: deploy the in-guest scripts to `~/.local/share/vmtest/`
  (`guest-cleanup.bash`, `guest-acceptance-server.bash`; the desktop one lands with Phase 5)
- [x] ✅ **T2.5**: `docs/vm-acceptance-testing.md`, a row in `docs/playbooks.md`,
  links from `docs/README.md`
- [x] ✅ **T2.6**: QA; deployed on the host via `deploy.bash`; idempotent on the
  second run; commit

### Phase 3: Server fast path and the first real scenario

- [x] ✅ **T3.1**: `server-fast` base builder from the official Cloud qcow2 (`vmtest fetch`/`build-base`; built on the host)
- [x] ✅ **T3.2**: `vmtest run server-fast-provision` (pass, 13/13 checks, `RUN-BASH-EXIT 0`, against a pinned pushed commit; acceptance run of 2026-09-13)
- [x] ✅ **T3.3**: `guest-acceptance-server.bash` with `planned` declared up front (13 checks; manifest agrees)
- [x] ✅ **T3.4**: The negative scenarios — **the falsifiability proof** (all three `fail` at `provision`, each with its own message in the transcript; `acceptance.bash` VERDICT: PASS)
- [x] ✅ **T3.5**: Plan-local `deploy.bash` (HOST) — landed with Phase 2; it runs the lab play
- [x] ✅ **T3.6**: QA; commit

### Phase 3b: Server full path (Anaconda), release-gated

- [x] ✅ **T3b.1**: `server-full` base from the **Server** install tree (`ks-vm-server.cfg` + `render_kickstart.py`; Anaconda cmdline mode over serial from the verified netinst ISO; `server-full-44` built in eleven minutes, tree checksums in its identity)
- [x] ✅ **T3b.2**: `vmtest run server-full-provision` (pass, 13/13, `RUN-BASH-EXIT 0`; run `20260913T185356Z-server-full-provision`)
- [x] ✅ **T3b.3**: Record which server base ran, in transcript and `evidence.base` (header `on base server-full-44 (full, server)`, `evidence.base.kind=full`; the acceptance leg's regex pins the pass to the full base)
- [x] ✅ **T3b.4**: QA; commit

### Phase 4: The bridge

- [x] ✅ **T4.1**: Spool layout and request/response schema (documented in `docs/vm-acceptance-testing.md`)
- [x] ✅ **T4.2**: `helpers/vmtest/spool.py`, test-first — the §6.3 defences (attacks in the tests; nine defences mutation-checked red)
- [x] ✅ **T4.3**: `vmtest-bridge-watcher` — thin executor (`bridge_watcher.py` drains and answers; `bridge_run.py` is the dispatched scope body; `bridge_heartbeat.py` the §6.5 writer — the units that invoke them are T4.4/T4.5)
- [x] ✅ **T4.4**: `vmtest-bridge@.path`/`.service` and the policy file (deployed and enabled by the play with the escaped-path instance; `TriggerLimitBurst=0`, `StartLimitIntervalSec=0`; `MODE_refresh-base=deny` default; live request answered end to end)
- [x] ✅ **T4.5**: `vmtest-bridge-heartbeat@.timer` — the §6.5 liveness signal (timer + `bridge_heartbeat.py`; heartbeat observed live)
- [x] ✅ **T4.6**: Response state machine, HMAC signing, heartbeat (`verdict.py`; the watcher and units that write them are T4.3–T4.5)
- [x] ✅ **T4.7**: `scripts/vmtest-request.bash` — container-side requester (`helpers/vmtest/request.py`; heartbeat first, distinct exit per outcome, states what it cannot verify; `vmtest verify <run-id>` is the host half)
- [x] ✅ **T4.8**: Bridge selftest — every rejection path rejects **and** responds (`selftest-bridge.bash`: 11 cases green against the live bridge, incl. the hostile-spool refusal and the watcher rate limit)
- [x] ✅ **T4.9**: Liveness selftest — wedge the unit, assert "bridge wedged" (`selftest-liveness.bash`: wedged reported with the remedy, exit 6, nothing written; the remedy restores service)
  rather than "timeout"
- [x] ✅ **T4.10**: QA; commit (every Phase-4 landing was QA'd and pushed in its own commit)

### Phase 5: Desktop base and desktop scenario (gated on U8)

- [x] ✅ **T5.1**: `fedora-install/ks-vm-desktop.cfg` — non-interactive, `liveimg`
  from the Live ISO found by content in `%pre`; btrfs subvolumes under LUKS2;
  `inst.stage2` pinned to the netinst label so the initrd cannot boot the Live squashfs
- [x] ✅ **T5.1b**: LUKS boot unlock — `console=ttyS0` **and `plymouth.enable=0`**;
  the prompt answered over the serial socket by `serial_console.py`, matched by
  phrase (the real prompt has no trailing colon); a stall is named
  `failure.stage: boot` with the console excerpt inline (unit-tested, `test_serial_console`)
- [x] ✅ **T5.2**: The boot medium — **both** artefacts, the gap B5 exposed
  (`base.json` names every artefact; a one-artefact record was caught as `reinstall`
  by the gate on the first build, JOURNAL 21:05)
- [x] ✅ **T5.3**: Desktop base builder (`desktop-44` built in ten minutes; passphrase
  `0600` beside the base; session runner installed into the base)
- [x] ✅ **T5.3b**: Session-environment probe inside the autologin guest
  (`evidence.guest.session_env_vars` / `session_only_vars` on every desktop run: the
  transient unit lacked only `_`, the two `GIO_LAUNCHED_*` and `JOURNAL_STREAM`)
- [x] ✅ **T5.4**: `vmtest run desktop-fresh-install` — three runs, each to
  `RUN-BASH-EXIT 0` inside the session; the run reboots the guest after `run.bash`
  as it asks and judges the new session (runs `20260913T195546Z`,
  `20260913T202035Z`, `20260913T204106Z-desktop-fresh-install`)
- [x] ✅ **T5.5**: `guest-acceptance-desktop.bash` — 16 checks, manifest agrees; the
  extension check iterates the UUIDs the repo deploys and requires `ACTIVE`. It
  **fails on the product**: all eight are `INITIALIZED`, never enabled (JOURNAL
  22:06) — the lab's first real finding, fixed in its own plan
- [x] ✅ **T5.6**: `virsh screenshot` evidence, with the evidence-not-assertion
  boundary held (`screenshot.png`, 1280×800, in the run directory; never a check)
- [x] ✅ **T5.7**: QA; commit

### Phase 6: Freshness automation, retention and guards

- [x] ✅ **T6.1**: Wire the Phase-1 policy into `vmtest` (`freshness_gate.py`, consulted by `vmtest run` before every clone; landed early with Phase 3)
- [x] ✅ **T6.2**: `refresh-base` — **no refresh boot**, the run's own transaction
  is the probe (`refresh.py`: the run's upgrade result + guest-seen revision → current | stale | incomplete | unknown; a passing `current` run certifies `base.json` forward with no boot; `stale` names `vmtest refresh-base`, which for a fast base is a re-import — the provisioned overlay is never flattened into a base, see JOURNAL 19:35)
- [x] ✅ **T6.2a**: Tests for the two traps this section was built from (B7 lag → `incomplete` never `current`; B6 unreadable identity → `unknown`, in `test_freshness`)
- [ ] ⏸️ **T6.2b**: Host-side DNF cache, plus the periodic **cache-cold** scenario
  — deferred: every scenario today runs cache-cold against the guest's own
  mirror, so the cold path is the only path and is exercised on every run; the
  cache is an optimisation to add once run time, not correctness, is the problem
- [x] ✅ **T6.3**: Nightly freshness probe that **only reports** (`vmtest freshness-status` → `freshness-status.txt`; `vmtest-nightly.timer` at 03:30; a base needing a rebuild shows as the unit failing, never as a rebuild)
- [x] ✅ **T6.4**: Disk-space floor (`retention.py`: floor + RAM ceiling refused before every run and rebuild; `vmtest sweep` keeps the last N passing runs and every failed one, drops failed fast builds, bounds `quarantine/` and `responses/` through the pinned spool; every eviction logged)
- [x] ✅ **T6.5**: QA; commit (each Phase-6 landing QA'd and pushed in its own commit)

### Phase 7: Discharge and review

- [x] ✅ **T7.1**: Run the scenarios; record which 00063/00079/00092 criteria
  actually discharge — 00063 (headless `run.bash` on a server guest) is discharged
  by `server-fast-provision` and `server-full-provision`; 00079 and 00092 are
  **not**: their open criteria are host-side `acceptance.bash` runs behind a
  `ccy --rebuild`, which no guest scenario exercises (the guest only proves the
  launcher is deployed)
- [x] ✅ **T7.2**: Update those three plans in the same commits — 00063 ticked
  with the run ids (Tasks 2.5, 2.8, 3.1 and two criteria); 00079/00092 left as
  they are, nothing of theirs discharged
- [ ] ⬜ **T7.3**: QA, then `qa-reviewer` over the full diff

### Phase 8: Design (complete)

- [x] ✅ **T8.1**: Design the system in full and harden it adversarially.
  [DESIGN.md](DESIGN.md), 2,149 lines. Four review rounds, eleven blocking
  findings, all verified at source; three were introduced by earlier rounds'
  fixes, and three were the same defect — a check that cannot fail. Certified
  implementable against blob `85fd9f2e`. Reports in `subagent-reports/`.

## Success Criteria

- [x] A single entry point runs the full lifecycle for a named profile and
  returns a verdict that names what it actually executed (`vmtest run <scenario>`;
  the response carries the base, its kind, the commit, the checks and the divergences).
- [x] Both `server` and `desktop` profiles are covered (server-fast, server-full and
  desktop bases; the desktop scenario's fail is the product's, see T5.5).
- [x] A run reuses the base snapshot; no run pays for a full OS install unless
  the freshness policy demands one (overlays on every run; the full bases were each
  installed once).
- [x] The base is rebuilt when either TTL expires, and the policy's decision is
  visible in the run output rather than implicit (`==> freshness: <verdict>` first,
  `evidence.base.freshness`; the run's own upgrade is the refresh probe).
- [x] The bridge exposes only enumerated scenario verbs; no verb takes a
  free-form command, and an unknown verb fails closed (selftest: 11 rejection paths
  reject and respond).
- [x] A harness failure (VM never booted, provisioning never started) is
  reported distinctly from a product failure (`error` with `failure.stage`
  boot/ssh/fetch/provision/collect against `fail` at `provision`/`assert`).
- [x] `./scripts/qa-all.bash` passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00110-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- `9cf465ef` — plan and reviewed design
- `21d39cad` — Phase 1: freshness engine, upstream probe live
- `979a74d4` — Phase 0: host triage, gate PROCEED
- `43feb43d` — Phase 2: the lab play, deployed and idempotent
- `8c4ee87e` — Phase 3: negative scenarios, `acceptance.bash`
- `4f4006e5` — Phase 4: bridge and liveness selftests green against the live bridge
- `3a13f8e1` — Phase 6: disk floor, RAM ceiling, retention sweep, nightly reporter
- `9acaa007` — Phase 7: discharge mapped, Plan 00063 ticked with run ids
- `392ec1dd` — Phase 3b closed; Phase 5 landed (desktop kickstart, serial unlock, session runner)
- `a271552f` — Phase 5: the desktop run judged in the post-reboot session
