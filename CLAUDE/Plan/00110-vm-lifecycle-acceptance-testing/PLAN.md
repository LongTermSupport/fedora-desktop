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

<!-- Populated from DESIGN.md once the design is reviewed and settled. -->

- [ ] 🔄 **Task 0.1**: Design the system in full — architecture, VM lifecycle
  state machine, freshness policy, the bridge verb set, and the performance
  plan. Output: [DESIGN.md](DESIGN.md), adversarially reviewed until solid.

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
