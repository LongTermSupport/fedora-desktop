# Plan 00062: Disk Reclaim TUI

**Status**: In Progress
**Created**: 2026-07-21
**Owner**: joseph
**Priority**: Medium

## Overview

The repo previously provisioned no dedicated disk-usage or cleanup tooling into
Fedora — the only disk-reclaim command was the narrow `raw-prune` (orphaned RAW
photos). This plan adds a general-purpose disk-reclamation capability: an Ansible
play that installs disk analysers plus a custom pure-bash TUI, `reclaim`, that
reports what is using disk and runs targeted cleanup actions semi-automatically.

`reclaim` is a dependency-light, menu-driven tool following
`CLAUDE/InteractiveScripts.md` (strict validation, friendly recovery, bounded
retries, clean EOF exit, confirm-before-destructive with a safe default) and
`CLAUDE/StderrHygiene.md` (interactive UI → stderr; the `report` subcommand's
stdout is the payload). Every destructive action shows what it will do and
defaults to "No"; it can also launch richer analysers (`ncdu`, `baobab`).

## Goals

- Add `play-disk-reclaim.yml` installing `ncdu`, `duf`, `trash-cli`, and `baobab`
  (GUI, desktop-only).
- Ship `reclaim` — a pure-bash TUI covering: biggest directories, biggest files,
  dnf autoremove + clean, old-kernel removal, journal vacuum, container prune
  (podman/docker), flatpak unused runtimes, `~/.cache` clear, empty trash, and a
  safe sweep.
- Comply with the repo's interactive-script and stderr-hygiene standards and pass
  `./scripts/qa-all.bash`.

## Non-Goals

- No automatic/unattended destructive cleanup — every action confirms (default No).
- No systemd timer / scheduled cleanup (on-demand tool only).
- Not wired into `playbook-main.yml` — standalone opt-in like
  `play-container-watch.yml` (wiring it into the main run is a user decision, see
  Delivery notes).

## Ground Rules (methodology — in force for this plan)

Adopted after a hallucinated "full disk" cause was asserted as fact. Non-negotiable:

1. **Never assume; never hallucinate.** Every data point that drives a decision
   must be confirmed from a real source, not inferred or filled in.
2. **Ground truth comes from plan-local triage scripting.** Facts about the live
   host are established by `triage.bash` (read-only, re-runnable, writes its
   report to `untracked/reports/` which the agent reads directly) — not from
   memory or narrative.
3. **Separate fact from hypothesis, always.** State confirmed facts and
   unconfirmed hypotheses in different buckets; never let a hypothesis harden
   into an asserted cause.
4. **Under uncertainty, extend triage — don't guess.** If a needed data point is
   missing, add a probe to `triage.bash` and have the user run it again. Iterate
   until the picture is complete; the user can run it as many times as needed.
5. **Confirm outcomes before claiming them.** "Fixed" / "works" is only stated
   after triage confirms it (e.g. `podman system df` rc=0 in the report), never
   assumed from having applied a change.

## Tasks

### Phase 1: Custom tool

- [x] ✅ **Task 1.1**: Write `files/home/.local/bin/reclaim` (pure-bash TUI:
  report header, 10 actions, capability detection, bounded/EOF-safe input,
  confirm-before-destructive, stderr-hygiene, `report`/`--help`/`--version`).
- [x] ✅ **Task 1.2**: Non-interactive escape hatch — `reclaim report` prints a
  stdout payload; unknown args exit 2.

### Phase 2: Playbook

- [x] ✅ **Task 2.1**: Write `playbooks/imports/optional/common/play-disk-reclaim.yml`
  (`scope: general`, installs CLI tools; `baobab` gated `when:     provisioning_profile != 'server'`; deploys `reclaim`; shebang + exec bit).

### Phase 3: QA + deploy

- [x] ✅ **Task 3.1**: Run QA: `./scripts/qa-all.bash` (403 files, exit 0; 0
  findings in `reclaim`).
- [x] ✅ **Task 3.2**: Container smoke test (`--version`, bad-arg exit 2,
  `report` stdout payload, `bash -n`).
- [ ] ⬜ **Task 3.3**: (HOST, not CCY) deploy + live-test — run the play, then
  `reclaim report` and walk the menu actions on the real desktop.

### Phase 4: Incident — rootless podman store wedged (surfaced while testing reclaim)

Decisions in this phase are made ONLY from confirmed triage facts (see Ground
Rules). Facts and hypotheses are kept strictly separate below.

**CONFIRMED FACTS** (source: `triage.bash` on host joseph-p14, report at
`untracked/reports/reclaim-podman-triage.log`):

- `podman system df` / `info` / `images` fail with rc=125; `podman ps` returns
  rc=0 and lists all live containers (2 dev stacks + 6 CCY sessions) — running
  work is unaffected; only NEW store operations fail.
- podman names the blocker itself: one **incomplete overlay layer**
  `2737e78309acf71fba40aeef01dd7fb7340e1648ff516a98c89177ea0714bfca` it tries to
  stage-delete on every load and cannot —
  `rename overlay/<id> overlay/tempdirs/…: permission denied`.
- The user AND `podman unshare` (namespaced root, bound to the sub-UID map) get
  permission denied on those files; only real host root can remove them.
- Disk is NOT full: `/home` 77% used, 361G free.

**UNCONFIRMED — do NOT assert (pending grounded facts):**

- WHY the layer's UIDs are outside the user's `/etc/subuid` map (the trigger).
  Candidates only: killed process / crash / reboot, or a subuid-range change.
  RETRACTED earlier hallucination: "interrupted pull during a full disk" — the
  user never had a full disk. `triage.bash` extended to dump `/etc/subuid` +
  the layer's numeric owning UIDs to settle this from data.

- Whether `reclaim` itself modified the store. v1.0.0 offered `podman system prune -af` AFTER `df` had already failed (store already unloadable), so
  `reclaim` SURFACED the wedge; any further contribution is unproven. The design
  flaw — poking a store it could not read — is owned and fixed regardless.

- [x] ✅ **Task 4.1**: `reclaim` v1.0.1 — `system df` is the reachability probe; a
  failed probe surfaces the reason and SKIPS the prune (docker too).

- [x] ✅ **Task 4.2**: `reclaim` v1.0.2 — detect the `stale temp dir` signature and
  point at the repair play instead of poking the store.

- [x] ✅ **Task 4.3**: `playbooks/imports/play-podman-store-repair.yml` — removes
  the incomplete overlay layers podman names in its own output + `overlay/tempdirs`
  as **real root**, probes before/after, asserts the store loads. Safe with live
  containers (no prune/reset). Optional `-e podman_repair_remove_image=`.

- [x] ✅ **Task 4.4**: `triage.bash` (plan-local) — read-only fact-finding that
  WRITES its report to `untracked/reports/` (bind-mounted → agent reads directly);
  extended with `/etc/subuid`/`/etc/subgid` + incomplete-layer ownership capture.

- [ ] ⬜ **Task 4.5**: (HOST) run `triage.bash` → agent establishes the trigger
  from the subuid/ownership data (no assertion until then).

- [ ] ⬜ **Task 4.6**: (HOST) run the repair play → run `triage.bash` again →
  confirm `podman system df` rc=0 and `ccy` starts.

- [ ] ⬜ **Task 4.7**: If the trigger is a repo-managed subuid change, add a
  durable preventive fix (scope TBD strictly from 4.5 facts).

## Success Criteria

- [x] `./scripts/qa-all.bash` passes.
- [x] `reclaim --help` / `report` / bad-arg behave correctly.
- [ ] Play deploys the tool + packages on the HOST and the menu actions work.
- [ ] Podman store recovered: `triage.bash` reports `podman system df` rc=0 and
  `ccy` starts (confirmed from the report, not assumed).
- [ ] Incident trigger established from grounded triage facts (not asserted).

## Plan-Local Scripts

- `triage.bash` — read-only fact-finding for the podman-store incident; writes
  `untracked/reports/reclaim-podman-triage.log` (agent reads it directly). Run
  repeatedly as triage is extended.

## Delivery & Milestones

- Feature: `reclaim` tool + `play-disk-reclaim.yml` (`7a6f5f4`).
- Incident + fix: `play-podman-store-repair.yml`, `reclaim` v1.0.1/1.0.2,
  `triage.bash` (`8869fe4`, `990d5f4`, `6e44ca2`, `e5052bb`).
- Correction: retracted the hallucinated "full disk" cause (`e5052bb`); Ground
  Rules adopted.
- Pending (HOST, blocked on grounded facts): Tasks 3.3, 4.5, 4.6, 4.7.
