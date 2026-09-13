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
- [ ] ⬜ **T2.3b**: deploy `files/home/.local/bin/vmtest` `0755` (the CLI is
  Phase 3 work; the deploy task lands with it)
- [ ] ⬜ **T2.4**: deploy the in-guest scripts to `~/.local/share/vmtest/`
  (they are Phase 3/5 work; the deploy task lands with them)
- [x] ✅ **T2.5**: `docs/vm-acceptance-testing.md`, a row in `docs/playbooks.md`,
  links from `docs/README.md`
- [x] ✅ **T2.6**: QA; deployed on the host via `deploy.bash`; idempotent on the
  second run; commit

### Phase 3: Server fast path and the first real scenario

- [ ] ⬜ **T3.1**: `server-fast` base builder from the official Cloud qcow2
- [ ] ⬜ **T3.2**: `vmtest run server-fast-provision`
- [ ] ⬜ **T3.3**: `guest-acceptance-server.bash` with `planned` declared up front
- [ ] ⬜ **T3.4**: The negative scenarios — **the falsifiability proof**
- [x] ✅ **T3.5**: Plan-local `deploy.bash` (HOST) — landed with Phase 2; it runs the lab play
- [ ] ⬜ **T3.6**: QA; commit

### Phase 3b: Server full path (Anaconda), release-gated

- [ ] ⬜ **T3b.1**: `server-full` base from the **Server** install tree
- [ ] ⬜ **T3b.2**: `vmtest run server-full-provision`
- [ ] ⬜ **T3b.3**: Record which server base ran, in transcript and `evidence.base`
- [ ] ⬜ **T3b.4**: QA; commit

### Phase 4: The bridge

- [ ] ⬜ **T4.1**: Spool layout and request/response schema
- [ ] ⬜ **T4.2**: `helpers/vmtest/spool.py`, test-first — the §6.3 defences
- [ ] ⬜ **T4.3**: `vmtest-bridge-watcher` — thin executor
- [ ] ⬜ **T4.4**: `vmtest-bridge@.path`/`.service` and the policy file
- [ ] ⬜ **T4.5**: `vmtest-bridge-heartbeat@.timer` — the §6.5 liveness signal
- [ ] ⬜ **T4.6**: Response state machine, HMAC signing, heartbeat
- [ ] ⬜ **T4.7**: `scripts/vmtest-request.bash` — container-side requester
- [ ] ⬜ **T4.8**: Bridge selftest — every rejection path rejects **and** responds
- [ ] ⬜ **T4.9**: Liveness selftest — wedge the unit, assert "bridge wedged"
  rather than "timeout"
- [ ] ⬜ **T4.10**: QA; commit

### Phase 5: Desktop base and desktop scenario (gated on U8)

- [ ] ⬜ **T5.1**: `fedora-install/ks-vm-desktop.cfg` — non-interactive, `liveimg`
- [ ] ⬜ **T5.1b**: LUKS boot unlock — `console=ttyS0` **and `plymouth.enable=0`**;
  test the wedge matcher with Plymouth *enabled* to prove it reports the wedge
- [ ] ⬜ **T5.2**: The boot medium — **both** artefacts, the gap B5 exposed
- [ ] ⬜ **T5.3**: Desktop base builder
- [ ] ⬜ **T5.3b**: Session-environment probe inside the autologin guest
- [ ] ⬜ **T5.4**: `vmtest run desktop-fresh-install`
- [ ] ⬜ **T5.5**: `guest-acceptance-desktop.bash`
- [ ] ⬜ **T5.6**: `virsh screenshot` evidence, with the evidence-not-assertion
  boundary held
- [ ] ⬜ **T5.7**: QA; commit

### Phase 6: Freshness automation, retention and guards

- [ ] ⬜ **T6.1**: Wire the Phase-1 policy into `vmtest`
- [ ] ⬜ **T6.2**: `refresh-base` — **no refresh boot**, the run's own transaction
  is the probe
- [ ] ⬜ **T6.2a**: Tests for the two traps this section was built from
- [ ] ⬜ **T6.2b**: Host-side DNF cache, plus the periodic **cache-cold** scenario
- [ ] ⬜ **T6.3**: Nightly freshness probe that **only reports**
- [ ] ⬜ **T6.4**: Disk-space floor
- [ ] ⬜ **T6.5**: QA; commit

### Phase 7: Discharge and review

- [ ] ⬜ **T7.1**: Run the scenarios; record which 00063/00079/00092 criteria
  actually discharge
- [ ] ⬜ **T7.2**: Update those three plans in the same commits
- [ ] ⬜ **T7.3**: QA, then `qa-reviewer` over the full diff

### Phase 8: Design (complete)

- [x] ✅ **T8.1**: Design the system in full and harden it adversarially.
  [DESIGN.md](DESIGN.md), 2,149 lines. Four review rounds, eleven blocking
  findings, all verified at source; three were introduced by earlier rounds'
  fixes, and three were the same defect — a check that cannot fail. Certified
  implementable against blob `85fd9f2e`. Reports in `subagent-reports/`.

## Success Criteria

- [ ] A single entry point runs the full lifecycle for a named profile and
  returns a verdict that names what it actually executed.
- [ ] Both `server` and `desktop` profiles are covered.
- [ ] A run reuses the base snapshot; no run pays for a full OS install unless
  the freshness policy demands one.
- [ ] The base is rebuilt when either TTL expires, and the policy's decision is
  visible in the run output rather than implicit.
- [ ] The bridge exposes only enumerated scenario verbs; no verb takes a
  free-form command, and an unknown verb fails closed.
- [ ] A harness failure (VM never booted, provisioning never started) is
  reported distinctly from a product failure.
- [ ] `./scripts/qa-all.bash` passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00110-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
