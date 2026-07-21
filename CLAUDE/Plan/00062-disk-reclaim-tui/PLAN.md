# Plan 00062: Disk Reclaim TUI

**Status**: In Progress
**Created**: 2026-07-21
**Owner**: joseph
**Priority**: Medium

## ⏸️ RESUME POINT (paused 2026-07-21 — user shutting down)

**The podman-store incident is RESOLVED.** The store loads, CCY is unblocked, all
16 containers still running — confirmed from captured reports, not assumed
(`untracked/reports/podman-store-repair-run.log` + `reclaim-podman-triage.log`).
Working tree clean; committed through **`a7e132d`**; **14 commits unpushed on
F44** (push when ready — never squash-merge, see AgentNotes).

**What was in flight when paused:** I asked the user to choose the scope of the
durable follow-up (Task 4.8) and they paused to shut down. **That decision is the
first thing to resume on.** The three options (grounded in the confirmed facts):

- **(A) Defer + build prevention (my recommendation).** Leave the latent orphans
  as-is (documented); build durable IaC prevention — close the CCY `common.bash`
  recovery gap (`files/var/local/claude-yolo/lib/common.bash`: cover the overlay
  store + volumes, use REAL root, since `podman unshare` can't fix a cross-map
  shift). Lowest risk to the live containers.
- **(B) Reconcile orphans now.** Add an opt-in offset-preserving `chown`
  (old `100000+N` → new `524288+N`, delta +424288) to the repair play; run as
  real root on the 1 orphan layer + 9 volumes AFTER confirming (via triage) none
  are mounted by a running container. Makes `df` clean + the Postgres data usable.
  Plus the prevention from (A).
- **(C) Stop.** Incident fixed; do nothing more now; leave 4.8 open.

**Do NOT re-derive these — already grounded (see CONFIRMED FACTS / RESIDUAL
OUT-OF-MAP ARTIFACTS below):**

- Trigger: subuid shift `100000:65536` → `524288:65536` orphaned old-map artifacts.
- Residual = **1 complete orphan overlay layer** `387e2af6…` (backs an image — do
  NOT delete) + **9 out-of-map volumes** (`brower-overlay_postgres_data` uid
  100069 = real Postgres data; 8 anonymous hex volumes uid 100998).
- Health gate is `podman info` + `podman images` (NOT `df` — `df` walks volume
  contents and false-negatives on out-of-map volumes even when the store is fine).

**Other open items (lower priority):**

- **Task 3.3** — (HOST) deploy + live-test the `reclaim` feature itself (walk the
  menu). The tool + `play-disk-reclaim.yml` are written; not yet menu-walked.
- **reclaim refinement (follow-up, not yet ticketed):** `reclaim`'s container
  action uses `podman system df` as its reachability probe, which now
  false-negatives (store healthy, but `df` fails on the out-of-map volumes) and
  wrongly points the user at the repair play. It should probe reachability with
  `podman info` and use `df` only for sizing — same lesson as the repair play.

**How to resume:** read this section + the two report files, then put the Task 4.8
decision back to the user (or proceed on the option they give). All plan-local
scripts are in this folder: `deploy.bash` (HOST wrapper, tees to
`untracked/reports/`), `triage.bash` (fact-finding), `podman-store-repair.yml` +
`repair-pass.yml` (the repair play). CCY reminder: edit/commit only in the
container; run Ansible on the HOST.

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

**✅ RESOLVED — store recovered (confirmed from captured reports, not assumed):**
After `deploy.bash` (report `untracked/reports/podman-store-repair-run.log`) and
a fresh `triage.bash`: `podman info` rc=0, `podman images` rc=0 (142 images incl.
ALL CCY tags), `podman ps` rc=0 (16 running). `overlay/tempdirs` absent. The
repair play's BEFORE `info` probe was ALREADY rc=0 — the store recovered the
moment `2737e78…` was removed in the prior run; only `df` (volume sizing) was
ever still failing, which the old df-gate misread as "still wedged". **CCY is
unblocked.** `podman system df` still rc=125 — expected: out-of-map volumes it
cannot size (see Task 4.8), NOT a store failure.

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
- **After removing `2737e78…` the error MOVED (2nd repair run):** the
  incomplete-layer blocker is cleared — podman no longer fails on the layer
  rename — and the failure surface is now a DIFFERENT out-of-map artifact, a
  **volume**: `Error: open storage/volumes/brower-overlay_postgres_data/_data: permission denied`. This proves the two failure surfaces are distinct.
- **`podman system df` is the wrong health gate:** `df` walks each volume's
  `_data` to compute its size, so it aborts on ANY out-of-map volume even when
  the layer store loads fine. `podman info` / `images` / `run` (and therefore
  CCY) do NOT touch volume contents. The store-load blocker was the overlay
  layer, now removed; the residual `df` failure is a VOLUME-sizing issue, not a
  store failure. Gate switched to `podman info` + `podman images`.
- **The out-of-map volume holds real data** (a Postgres `_data` dir), so it is
  REPORTED, never deleted. Making it usable again is a deliberate subuid
  migration (Task 4.8), separate from unblocking the store.

**RESIDUAL OUT-OF-MAP ARTIFACTS — the full set (now CONFIRMED from triage), for
Task 4.8. None block the store/CCY; all are latent (they only bite if the image
is run / the volume is mounted under the new map):**

- **1 orphan overlay layer** `387e2af6408cdac84de0426ff580af7558aad8d7e342d2ddacbef7a03a514fbe`
  (uid 100000, 2025-11-10). podman did NOT name it incomplete and the store loads
  with it present → it is a **complete layer backing an image** (earlier
  UNCONFIRMED now resolved). Lists fine; would fail to MOUNT under the new map.
  Must NOT be deleted blindly.
- **9 out-of-map volumes** (`df` aborts at the first; triage enumerates all):
  - `brower-overlay_postgres_data` — uid **100069** (2026-01-13), a Postgres data
    dir (real data).
  - **8 anonymous** hex-named volumes — uid **100998** (all 2026-04-01): `4e038cbf…`,
    `4f335c1e…`, `5e1e7adb…`, `645db9db…`, `77488eec…`, `8b120095…`, `a0ebb0b9…`,
    `b904facc…`.
  - For contrast, the CURRENT-map volumes are fine: `discreet-booking_postgres_data`,
    `discreet-booking_postgres_run`, `boxer-images_database_data` (uid 524357).
- **The offset is exact and deterministic:** old `100000+N` → new `524288+N`
  (delta +424288). E.g. `100069`→`524357` — the very uid the healthy postgres
  volumes carry. This makes an offset-preserving `chown` a clean, non-destructive
  migration (Task 4.8 option).

**Still UNCONFIRMED (low importance):**

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

- [x] ✅ **Task 4.7b**: (HOST) 2nd repair run — regex fix WORKED (`removed: ['2737e78…']`), but the play still failed its assert because the error MOVED to
  an out-of-map VOLUME (`open volumes/brower-overlay_postgres_data/_data: permission denied`) and the assert was gating on `podman system df` — which
  walks volume contents and so fails on that volume even though the layer store
  now loads. Diagnosis grounded in the pasted play output (see CONFIRMED FACTS).

- [x] ✅ **Task 4.7c**: Correct the play + add the wrapper (this iteration):

  - Health gate switched from `podman system df` to **`podman info` + `podman images`** (load the store, do NOT walk volumes) — the right "store loads"
    signal; `df` demoted to informational.
  - **Bounded self-iteration** (`repair-pass.yml`, `podman_repair_max_passes=6`):
    each pass probes `info`, removes any named incomplete layer as root, loops
    until the store loads — clears multiple incomplete layers in ONE run.
  - Out-of-map volumes are **detected + reported, never deleted** (they hold real
    data → Task 4.8).
  - **`deploy.bash`** wrapper runs the play and tees full output to
    `untracked/reports/podman-store-repair-run.log` (user steer). `triage.bash`
    extended to enumerate out-of-map volumes. QA green; `--syntax-check` clean.

- [x] ✅ **Task 4.7d**: (HOST) ran `deploy.bash` → `triage.bash`. CONFIRMED from
  the captured reports: `podman info` + `podman images` rc=0 (all CCY tags
  present), `podman ps` rc=0 (16 running). Store recovered, CCY unblocked. `df`
  still fails on the out-of-map volumes (expected). Full residual artifact set
  captured (see RESIDUAL OUT-OF-MAP ARTIFACTS): 1 complete orphan layer + 9
  out-of-map volumes. **The incident is resolved.**

- [ ] ⬜ **Task 4.8**: Durable follow-up (incident already resolved; this is the
  "won't recur / clean up the latent orphans" work). Scope is a USER DECISION
  because it touches a live store with important work and includes a data volume.
  The grounded options (from the confirmed artifact set):

  - **(A) Defer reconciliation (recommended default).** Leave the 1 orphan layer +
    9 out-of-map volumes as latent orphans (they only bite if you re-run that
    image / re-mount those volumes). Document them. Do the PREVENTION work only.
  - **(B) Reconcile now** via an opt-in offset-preserving `chown` (old `100000+N`
    → new `524288+N`) as real root, on the 1 layer + 9 volumes — makes `df` clean
    and all data usable again. Precondition: confirm none are mounted by a running
    container first (the running stacks use the in-map volumes, so likely safe,
    but verify — never assume). The 8 anonymous `100998` volumes may just be junk;
    if so, deleting them is an alternative (needs user confirmation — data).
  - **(C) Prevention (do regardless of A/B):** close the CCY `common.bash`
    recovery gap (cover the overlay store + volumes; use REAL root, since
    `podman unshare` can't fix a cross-map shift — proven here) and/or make
    `play-docker.yml`'s subuid change migrate old artifacts instead of silently
    orphaning them.

## Success Criteria

- [x] `./scripts/qa-all.bash` passes.
- [x] `reclaim --help` / `report` / bad-arg behave correctly.
- [ ] Play deploys the tool + packages on the HOST and the menu actions work.
- [x] Podman store recovered: the captured report shows `podman info` +
  `podman images` rc=0 (all CCY tags present) and 16 containers running
  (confirmed from the report, not assumed). (`podman system df` still fails on
  out-of-map volumes — a Task 4.8 concern, not a store failure.)
- [x] Incident trigger established from grounded triage facts (not asserted):
  subuid range shift `100000:65536` → `524288:65536` orphaned old-map artifacts.

## Plan-Local Scripts

- `triage.bash` — read-only fact-finding for the podman-store incident; writes
  `untracked/reports/reclaim-podman-triage.log` (agent reads it directly). Run
  repeatedly as triage is extended.
- `deploy.bash` — HOST-only wrapper that runs the repair play and tees FULL
  output to `untracked/reports/podman-store-repair-run.log` (the agent reads it
  directly). Pass-through args reach `ansible-playbook` (e.g.
  `-e podman_repair_remove_image=…`, `-e podman_repair_max_passes=N`). **Run the
  repair via this**, not the play directly, so the result is always captured.
- `podman-store-repair.yml` — incident-recovery Ansible playbook (HOST-only).
  Gates on `podman info` + `podman images` (store-load signal, not `df`);
  self-iterates removing podman-named incomplete overlay layers as real root
  (`repair-pass.yml`, bounded by `podman_repair_max_passes`); removes stale
  tempdirs; REPORTS out-of-map volumes without deleting them; no prune/reset;
  safe with live containers. Plan-local, not durable IaC. Being outside
  `playbooks/`, it is not covered by `qa-ansible-syntax`; check with
  `ansible-playbook --syntax-check …/podman-store-repair.yml`.
- `repair-pass.yml` — the single-pass task file included in the play's bounded
  repair loop (probe `info` → remove named incomplete layers as root → mark
  loaded). Not a standalone playbook.

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
