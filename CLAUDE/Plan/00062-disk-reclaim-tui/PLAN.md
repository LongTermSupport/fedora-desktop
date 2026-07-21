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
- **Ownership numbers (the smoking gun):** the incomplete layer dir is owned by
  **UID/GID 100000** (dated Nov 6 2025); joseph's current `/etc/subuid` +
  `/etc/subgid` map is **`524288:65536`** (covers 524288–589823). 100000 is
  OUTSIDE that map → unmapped in any userns → only real root can remove it.
- **Trigger (grounded in host data + repo source):** this repo's
  `playbooks/imports/play-docker.yml` changed joseph's subuid range. Its own
  comment (lines 108–114): an earlier version hand-wrote a `100000:65536` block;
  the current version REMOVES it, reverting to Fedora's auto-allocated
  `524288:65536`. Layers built under the old 100000 mapping (like `2737e78…`)
  are thereby orphaned. RETRACTED the earlier "full disk" hallucination.
- **CCY recovery gap (grounded in source):** `common.bash` documents this
  failure class ("a sub{u,g}id range change", line 61) but its
  `purge_stale_buildah_cache` only covers the buildah cache (not the overlay
  layer store) and falls back to `podman unshare`, which cannot fix a cross-map
  shift (proven here — 100000 ∉ 524288 map).
- **Blast radius is SMALL (histogram):** of ~1650 overlay layer dirs, **1648 are
  owned by host uid 1000 (normal, in-map)** and only **2 are owned by 100000**
  (orphaned). This is a bounded 2-layer problem, not store-wide corruption. One
  of the 2 is the incomplete `2737e78…`; the other is a second 100000-owned layer
  podman has not named.

**UNCONFIRMED — do NOT assert (pending grounded facts):**

- **Identity/nature of the 2nd orphaned layer** (incomplete vs a complete layer
  backing an image). `triage.bash` extended to list both orphan ids + mtime.
  Matters because the repair removes ONLY podman-named incomplete layers — a
  complete orphan must NOT be deleted (it backs an image) and instead informs
  Task 4.8.

- Whether `reclaim` itself modified the store. v1.0.0 offered `podman system prune -af` AFTER `df` had already failed (store already unloadable), so
  `reclaim` SURFACED the wedge; any further contribution is unproven. The design
  flaw — poking a store it could not read — is owned and fixed regardless.

- [x] ✅ **Task 4.1**: `reclaim` v1.0.1 — `system df` is the reachability probe; a
  failed probe surfaces the reason and SKIPS the prune (docker too).

- [x] ✅ **Task 4.2**: `reclaim` v1.0.2 — detect the `stale temp dir` signature and
  point at the repair play instead of poking the store.

- [x] ✅ **Task 4.3**: `CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml`
  — removes the incomplete overlay layers podman names in its own output +
  `overlay/tempdirs` as **real root**, probes before/after, asserts the store
  loads. Safe with live containers (no prune/reset). Optional
  `-e podman_repair_remove_image=`. Relocated from `playbooks/imports/` into the
  plan folder (user steer): it is plan-local incident recovery, not durable IaC.

- [x] ✅ **Task 4.4**: `triage.bash` (plan-local) — read-only fact-finding that
  WRITES its report to `untracked/reports/` (bind-mounted → agent reads directly);
  extended with `/etc/subuid`/`/etc/subgid` + incomplete-layer ownership capture.

- [x] ✅ **Task 4.5**: (HOST) run `triage.bash` → trigger ESTABLISHED from data:
  layer owned by 100000, current map `524288:65536`, and `play-docker.yml`'s own
  comment confirms it removed a hand-managed `100000:65536` block. See CONFIRMED
  FACTS above.

- [ ] ⬜ **Task 4.6**: (HOST) run extended `triage.bash` once more to capture the
  overlay ownership histogram (blast radius) → agent reads it.

- [x] ✅ **Task 4.7a**: (HOST) FIRST repair run no-op'd — `Incomplete layers removed this run: []` and store still rc=125. Root cause CONFIRMED from data,
  not guessed: podman's logrus text formatter escapes the inner quotes, so the
  literal stderr bytes are `Found incomplete layer \"<id>\"` (backslash-quote),
  but the regex matched a **bare** quote (`layer "([0-9a-f]{64})"`) → 0 matches →
  nothing removed. Proven by replaying the exact stderr line from the triage log:
  old regex → `[]`, new regex → `['2737e78…']`. Fixed to
  `incomplete layer[^0-9a-f]*([0-9a-f]{64})` (anchor on phrase, skip any non-hex
  run up to the id — robust to podman's quoting). `--syntax-check` clean.

- [ ] ⬜ **Task 4.7b**: (HOST) re-run the fixed, relocated repair play →
  `ansible-playbook CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml` →
  then run `triage.bash` again → confirm `podman system df` rc=0 and `ccy`
  starts. If a NEW incomplete-layer id appears (the 2nd 100000-owned orphan is
  also incomplete), re-run once more; if the store loads with the 2nd orphan
  still present, it is a COMPLETE layer backing an image → input to Task 4.8.

- [ ] ⬜ **Task 4.8**: Preventive/durable fixes, scope decided strictly from the
  4.6 blast-radius facts — candidates (NOT yet committed to): (a) a migration
  step that reconciles orphaned out-of-map layers when the subuid range changes;
  (b) close the CCY `common.bash` recovery gap (cover the overlay store; use real
  root, since `podman unshare` can't fix a cross-map shift). Decide with the user.

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
- `podman-store-repair.yml` — incident-recovery Ansible playbook (HOST-only;
  removes podman-named incomplete overlay layers + stale tempdirs as real root;
  no prune/reset; safe with live containers). Plan-local, not durable IaC. Being
  outside `playbooks/`, it is not covered by `qa-ansible-syntax`; check it with
  `ansible-playbook --syntax-check CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml`.

## Delivery & Milestones

- Feature: `reclaim` tool + `play-disk-reclaim.yml` (`7a6f5f4`).
- Incident + fix: `play-podman-store-repair.yml`, `reclaim` v1.0.1/1.0.2,
  `triage.bash` (`8869fe4`, `990d5f4`, `6e44ca2`, `e5052bb`).
- Correction: retracted the hallucinated "full disk" cause (`e5052bb`); Ground
  Rules adopted.
- Repair-play regex fix + relocation into the plan folder: the first HOST run
  no-op'd (`removed: []`) because the id-extraction regex matched a bare quote
  while podman's logrus escapes it (`\"`); fixed to skip any non-hex run, proven
  against the real stderr bytes; `reclaim` v1.0.3 pointer updated.
- Pending (HOST, blocked on grounded facts): Tasks 3.3, 4.5, 4.6, 4.7.
