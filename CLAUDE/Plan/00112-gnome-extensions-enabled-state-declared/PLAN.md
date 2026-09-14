# Plan 00112: GNOME extensions — the enabled list as declared state

**Status**: In Progress
**Created**: 2026-09-13
**Owner**: Repo owner + Claude
**Priority**: High

## Overview

The desktop acceptance scenario from Plan 00110 (`vmtest run desktop-fresh-install`, run `20260913T204106Z-desktop-fresh-install`) found
that a fresh install ends with every extension the repo deploys installed,
compiled and loaded by GNOME Shell — `State: INITIALIZED` for all eight — and
**none of them enabled**. The session's `org.gnome.shell enabled-extensions`
holds only Fedora's stock background-logo. A real user has to open the
Extensions app and flip eight switches by hand, which is the manual step the
repo exists to remove. Existing hosts never showed it because their lists were
flipped long ago and gsettings persists.

The cause is in `play-gnome-shell-extensions.yml`. The seven extensions from
extensions.gnome.org are fetched by `gnome-shell-extension-installer`, whose
own `gnome-extensions enable` runs the instant the zip is extracted — before
the running shell has scanned the new directory — and whose failure is an `&&`
the installer does not propagate; the play inspects only the download marker.
The custom extension's enable task is the same call a moment after the copy,
carrying `failed_when: false`, and its verify helper deliberately exits 0 for
"pending Wayland reload". So a first run enables nothing and reports every
task ok. `gnome-extensions enable` asks the *shell* to enable; the shell can
only enable what it has already loaded.

The fix is to declare the state rather than request an action: each deployed
UUID must be present in `enabled-extensions`, written through gsettings (which
the shell reads at session start and watches live), so it holds whether or not
the running shell has loaded the extension yet. The desktop scenario is this
plan's acceptance test: it certifies `desktop-44` forward only when the check
`deployed-extensions-active` is green in the post-reboot session.

## Goals

- A fresh install (the desktop scenario) ends with every UUID the repo deploys
  `ACTIVE` in the session a user gets, with no manual step.
- The enable step is idempotent declared state, not a racy request: present
  UUIDs are kept, missing ones added, nothing removed that the user added.
- No `failed_when: false` on the enable path; a UUID the shell rejects
  (`OUT_OF_DATE`, `ERROR`) still fails the play through the existing verify
  helper, which now runs for every deployed UUID, not one.

## Non-Goals

- Changing which extensions the repo installs, or their versions.
- Replacing `gnome-shell-extension-installer`.
- Making the shell reload on Wayland; a fresh install still reboots as
  `run.bash` already says.

## Tasks

### Phase 1: The declared enabled list

- [x] ✅ **Task 1.1**: `helpers/gnome/enabled_extensions.py` — pure parse/merge/format
  of the GVariant list, plus `resolve_declared`, which confirms a declared UUID
  against disk. The side-effecting half is `apply_enabled_extensions.py`: it
  resolves a session bus (`session_bus.py`), refuses to write while
  `disable-user-extensions` is true, merges, writes, and **re-reads what it wrote**
  so a refused write fails rather than passing
- [x] ✅ **Task 1.2**: The play calls the applier once, after the schemas are
  compiled and the custom extension is copied, passing every UUID it deploys.
  `changed_when` keys on `GNOME-EXT-ENABLED-CHANGED`
- [x] ✅ **Task 1.3**: The `failed_when: false` enable task is gone;
  `helpers.gnome.verify_extension` now loops over every deployed UUID, taken from
  the applier's `GNOME-EXT-DEPLOYED` marker rather than re-derived from the play's
  own literals. Its pending-Wayland-reload rule is unchanged, and a live session
  that has not scanned an extension is now its own verdict rather than being
  reported as "no session"
- [x] ✅ **Task 1.4**: Confirmed — `run.bash`'s closing "System Reboot" step
  recommends a reboot on every path (interactive prompt, and the headless message
  whether or not `RUN_BASH_REBOOT=1`), so it already covers the case where this
  play changed the list. No change needed

### Phase 1b: The set is declared, not discovered (from the first qa-reviewer pass)

The first implementation read the population off
`~/.local/share/gnome-shell/extensions`, which is also where the **user's own**
extensions live. That made the play judge and re-enable extensions this repo does
not own, while still missing a partial install. `vars/gnome-shell-extensions.yml`
is the single source instead, and disk only confirms it.

- [x] ✅ **Task 1.5**: `vars/gnome-shell-extensions.yml` — every deployed UUID, in
  three groups, read by the play, the VM acceptance check and the secret scanner.
  No consumer keeps a copy, and all three enumerate **every** group, so a fourth
  cannot be silently half-adopted

  - [x] ✅ One consumer *did* keep a copy: the custom-extension deploy task spelled
    the UUID in its `src` and `dest`. Measured, not narrated: a rename in the vars
    file alone deployed the **old** directory and the applier then hard-failed with
    `declared-extension-not-deployed` — loud, not silent, but a failure whose cause
    sat two files from its symptom. Now a loop over `gnome_shell_extensions.custom`,
    which is what the header always claimed, with per-file `mode` and `owner`/`group`
    per AnsibleStyle (this closes Plan 00049's EXT-13)

- [x] ✅ **Task 1.6**: dash-to-dock joins the declared set with
  `/usr/share/gnome-shell/extensions` as a second search path. A VM run's own
  evidence showed it installed and never enabled, which made the play's
  `intellihide-mode` write a no-op on every fresh install

- [x] ✅ **Task 1.7**: `disable-user-extensions` is read before anything is written.
  True, it defeats every extension whatever the list holds, so the play would
  otherwise go green over a session with nothing enabled

- [x] ✅ **Task 1.8**: The pre-commit secret scanner derives an allowlist from the
  vars file at scan time — a GNOME extension UUID is shaped exactly like an email
  address. Only values under a `uuid` key in that one tracked file are exempt, each
  an anchored whole-token literal, and `scripts/test-secret-scan.bash` now covers
  it: a real address still flags, including on a line that also holds a UUID. The
  **anchors and `re.escape` are pinned separately** — without them the exemption
  silently widens to a substring match and every earlier case still passed, so each
  is held by a case that fails if only that property is dropped

  - [x] ✅ The scanner's own comment no longer carries worked-example UUIDs. Real
    ones made its source committable only for as long as those extensions stayed
    declared; an invented one would be an address-shaped literal the function has
    no reason to exempt. It describes the shape instead
  - **Deferred, with the reason recorded** rather than left as a carried nit:
    `hook_extension_uuid_allowlist` does not call `enabled_extensions.validate_uuid`.
    Measured across all **five** shapes `validate_uuid` rejects: an empty value is
    silently skipped; a space, a comma or a **carriage return** each emit a live
    allowlist entry; and **only** a newline-bearing one fails — at the *filter*
    (`grep: Trailing backslash`, GNU grep's wording), not at the builder, which
    returns 0 in all five. So the direction is safe — none of
    them widens the exemption to cover a real address — but what is missing is an
    operator message, not a guard, and "hard-fails, confirmed" is not what the code
    does. Fixing it edits a live public-repo security gate and needs its own control
    fixture, which is a poor thing to bolt onto a plan already blocked elsewhere

- [x] ✅ **Task 1.9**: A live session that has no record of a UUID is
  `PENDING_SCAN`, not `SKIP_NO_SESSION`. `gnome-extensions info` exits non-zero for
  both, and conflating them let a nine-iteration gate report OK having judged
  nothing. `session_bus.py` is shared, so the applier and the verifier can no longer
  disagree about which session they are looking at

- [x] ✅ **Task 1.10**: The verdict split reached the helper and stopped there — every
  non-failing verdict still exits 0, nothing read `gse_verify`, and Ansible does not
  print a command task's stdout without `-v`, so at **play** level nine judged and nine
  unjudged were still byte-identical. `Assert Every Deployed Extension Produced A Readable Verdict` now consumes the results and reports `COVERAGE: n of m judged against a live session`. It fails only on a verdict it cannot read — `pending_scan`
  before the reboot is legitimate and must not fail a fresh install — so the gate that
  *proves* the outcome remains the post-reboot acceptance check.

  - [x] ✅ The **population** is pinned against `declared_extension_uuids`, not against
    the verify results alone. Both counts derive from `gse_verify.results`, so on their
    own they agree at zero: a `when:` on the verify task left the gate reporting
    `COVERAGE: 0 of 0` and passing — the fix reproducing the defect it was written for
  - [x] ✅ Verified against Ansible's own templar on **eight** shapes, the expressions
    read out of the play rather than retyped so the harness cannot drift from the task
    it vouches for. Pass: all-healthy, fresh-install all-`pending_scan`, nothing
    declared. **Fail**: an unreadable verdict, a short loop, `results: []`, a register
    with no `results` key, `gse_verify` undefined

- [x] ✅ **Task 1.9**: **The same defect in the sibling play.** `play-container-watch.yml`
  still asked the running shell to enable its extension, with `failed_when: false` on the
  probe, the disable AND the enable, then a `debug` saying the extension "will be enabled
  on next GNOME session start". Nothing enabled it later: a failed `enable` never wrote
  the key, so that sentence was false in the same way this plan's own was. The dance
  existed to force a reload, which on Wayland cannot work at all — only a logout reloads
  extension JavaScript. Replaced with this plan's `apply_enabled_extensions` route, which
  merges without removing and re-reads the key to prove the write took, so it needs no
  `failed_when`: the operation is its own probe. Three prohibited suppressions and one
  prohibited skip-and-warn removed; `--syntax-check` and `qa-all.bash` green.
  Found by the Phase 4 extension survey in Plan 00109, not by a gate

### Phase 2: Acceptance

- [ ] ⬜ **Task 2.1**: HOST — the operator's step. `triage.bash`, then `deploy.bash`,
  then `deploy.bash` again (idempotent: no change on the second run), then
  `triage.bash` again. The two `triage-runs/` reports are the evidence that the
  host's own list gained the deployed UUIDs and lost nothing.
  **Also re-run `play-vm-test-lab.yml`** — Task 2.2 cannot mean anything until the
  host's deployed guest checker matches this plan's version of it

- [ ] 🚫 **Task 2.2**: `vmtest run desktop-fresh-install` against the pushed
  commit; `deployed-extensions-active` green in the post-reboot session; run id
  recorded here; `desktop-44` certified forward by the passing run.
  **Blocked on Task 2.1's lab redeploy, not on the code.** Two runs have gone
  green and neither certifies this:

  - `20260914T085408Z-desktop-fresh-install` — pass 16/16 against `cb88ec4e`,
    `COVERAGE: 8 of 8`. Predates every Phase 1b fix
  - `20260914T100220Z-desktop-fresh-install` — pass 16/16 against `d307ed28`,
    but `COVERAGE: **8 of 1** declared ACTIVE`. Nine are declared. `vmtest` copies
    the checker from the **host's deployed copy**, not the guest's checkout, and
    that copy predates this plan: it counted `id: <n>` lines in the *play*, which
    Task 1.5 moved into `vars/gnome-shell-extensions.yml`. Expected collapsed to
    `0 + 1` and eight actives cleared it. A run in flight at `20260914T110446Z`
    against `be73d3b0` inherits the same stale checker

  The harness defect is [Plan 00117](../00117-vmtest-acceptance-script-version-gate/PLAN.md).
  It is not this plan's to fix, but it is this plan's blocker

- [x] ✅ **Task 2.3**: QA green, and `qa-reviewer` over the diff across five rounds.
  Each round found something real, and the recurring shape was mine rather than the
  code's: a fix that reached the helper and stopped at the play (round 1), a gate
  that passed `0 of 0` on an empty population — the fix reproducing its own subject
  (round 2), two sentences claiming "measured" that were not (round 4), and an
  exhaustive-sounding sweep that covered four of five shapes (round 5). Reports in
  [subagent-reports/](subagent-reports/)

## Success Criteria

- [ ] `vmtest run desktop-fresh-install` verdict `pass`, 16/16, in the session
  after the reboot.
- [ ] Re-running the play on a host with extra user-enabled extensions removes
  none of them and reports no change.
- [x] No `FAIL-FAST-OK` annotation remains on the enable path. Both enable paths: the
  declared-state route this plan built, and `play-container-watch.yml`, which was still
  on the mechanism this plan replaced (Task 1.9). The one remaining `failed_when` under
  `playbooks/imports/play-gnome-shell-extensions.yml` is on the *installer* and is the
  permitted probe-then-fail form — its `rc not in [0, 2]` is an explicit check.
- [x] `./scripts/qa-all.bash` passes — 856 files.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00112-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
