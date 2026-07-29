# Plan 00065: headless server Cloud Base blocker fixes

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

## Overview

The headless `run.bash` server path (Plan 00063) is documented as supporting "Fedora
**Server or Cloud**" (`docs/headless-server-install.md`). A first live provisioning run
against a **minimal Fedora Cloud Base** image surfaced that the server path cannot actually
complete on Cloud Base today: three `general`-scope core plays each abort the whole run
(via `ansible.cfg` `any_errors_fatal = true`), and two more leave a container-oriented
server silently mis-configured. The root cause in every case is a **desktop/Server-edition
assumption that is neither declared as an IaC dependency nor profile-gated** — so it only
bites on the more-minimal Cloud image, deep in the run, often with a misdiagnosing message.

This plan fixes those assumptions **at the layer that needs them**, per the repo's own
"Missing Dependencies — Fail Fast, Fix in IaC" rule (`CLAUDE.md`): a play that needs a
package `ansible.builtin.dnf`-installs it; a dependency needed pre-Ansible by `run.bash`
itself is installed by `run.bash`. It also adds the missing **preflight edition/flavour
fail-fast** so an unsupported image is rejected loudly at play 1 instead of breaking at
play 18.

Provenance: findings came from an adversarial audit during a downstream Cloud Base
provisioning effort; all citations below are re-verified against this repo at the current
`F44` HEAD.

## Goals

- Make a fresh **Fedora Cloud Base** headless `run.bash` server run complete end-to-end,
  honouring the docs' existing "Server or Cloud" support claim.
- Fix the three whole-run **BLOCKERS** by declaring missing dependencies in IaC (not by
  probing, not by `failed_when: false`): fwupd, LXC packages, markless scratch dir.
- Fix the two container-relevant **silent misbehaves** (user-linger → oomd override inert;
  `podman.socket` skipped) so a headless container host is actually correct.
- Add a **preflight edition/flavour fail-fast** so an unsupported image is rejected at
  play 1 with an actionable message.
- Add a **curated "server-recommended" optional-play bundle** the operator can enable in one
  shot (e.g. `RUN_BASH_OPTIONAL_PLAYBOOKS=server-recommended`) instead of hand-listing plays.
- Every change passes `./scripts/qa-all.bash` and keeps the fail-fast / no-suppression rules.

## Non-Goals

- **Not** changing the supported OS set or removing any feature — LXC stays a first-class
  general play (it is load-bearing for downstream LXC-stack consumers); the fix is
  gating + dep-declaration, never removal.
- **Not** a server-hardening play (sshd baseline, fail2ball, unattended-upgrades) — that is
  greenfield, a separate future plan.
- **Not** the no-GitHub (`RUN_BASH_GITHUB_ACCOUNTS=none`) headless path — still deferred.
- **Not** a slimmer CCY server image variant — noted, out of scope here.

## Context & Background — the audited findings (all re-verify before editing)

Scope guard mechanism (`CLAUDE/AnsibleStyle.md` "Provisioning Profile Self-Guard"): a
`general` play runs on server; per-task GUI bits are gated `when: provisioning_profile != 'server'`; `qa-ansible.bash` Check 4 forbids the 2-task scope guard on a `general` play, so
none of these fixes may add that guard.

**BLOCKERS (abort the whole run under `any_errors_fatal`):**

1. **fwupd — `play-basic-configs.yml` (fwupd task, ~:315-343), import 3 of 31.** Unguarded
   `fwupdmgr get-devices` under bare `set -e`; `fwupd` is never installed and a VM has no
   firmware surface (rc 127 or rc 2). Earliest abort — masks everything downstream.
2. **LXC — `play-lxc-install-config.yml`, import 18 of 31.** `scope: general`, no guard.
   Installs only `lxc` + `lxc-templates` (~:52-56) but then hard-depends on packages it
   never declares: `firewalld` (~:92-98, zone bind), `dnsmasq` (~:385-399, hard assert that
   it launched), `iptables-nft` (the `iptables -I` calls), `NetworkManager` (~:162-166
   `nmcli` probe). Plus a vault-passphrase assert + `ssh -T git@github.com` + `git@` **SSH**
   clone of the **public** `lxc-bash` repo (~:187-253) — ~65 needless lines. Plus F8:
   DOCKER-USER egress rules are runtime-only and evaporate on reboot (~:287-362).
3. **markless — `play-markless.yml` (~:24-35), import 26 of 31.** `cd ~/Downloads` (an XDG
   dir that exists only after a graphical login; the repo never creates it) inside `set -ex`.

**SILENT MISBEHAVES (run goes green, box is wrong — matters for a container host):**

4. **user linger absent.** No core play enables `loginctl enable-linger`, so every
   `systemctl --user` in a headless firstboot silently no-ops:
   - `play-systemd-user-tweaks.yml` (~:118-126 verify, ~:177-185 handler) — oomd override
     written but never loaded; the lone `ignore_errors: true` in the core tree hides it, and
     its verifier's `failed_when: false` reports success on an unapplied config.
   - `play-podman.yml` (~:23-35) — `podman.socket` silently skipped (no Docker-API socket).

**MISSING FAIL-FAST:** `play-AA-preflight-sanity.yml` asserts only Fedora + major version +
`provisioning_profile` — **no edition/flavour check**. Note: `VERSION_ID` cannot distinguish
edition (44 across Workstation/Server/Cloud — see `CLAUDE/Plan/00063-…` journal); a working
check needs an edition marker (`fedora-release-{server,cloud,workstation}` RPM or
`/etc/system-release-cpe`) — presence to be confirmed on a live box of each edition.

**ENHANCEMENTS (not blockers; fold in if cheap):** desktop multimedia + GUI/audio dev
headers installed on server (`play-rpm-fusion.yml`, `play-python.yml`); interactive `pause`
in `play-basic-configs.yml`/`play-AB-dnf-upgrade.yml` (confirmed non-blocking on true
non-interactive stdin — cosmetic).

## Tasks

### Phase 1: Unblock the run (the three whole-run aborts)

- [x] ✅ **Task 1.1**: `play-markless.yml` — replace `cd ~/Downloads` with a `mktemp -d`
  scratch dir (`trap 'rm -rf' EXIT`); removes the missing-dir dependency and the
  `rm -rf markless*` shared-dir hazard. Play stays `general`. `--syntax-check` rc=0.
- [x] ✅ **Task 1.2**: `play-basic-configs.yml` — gate the fwupd task
  `when: provisioning_profile != 'server'` (precedent: the USB-audio task ~11 lines above),
  with a WHY comment (no firmware surface on a VM/headless target). `--syntax-check` rc=0.
- [x] ✅ **Task 1.3**: `play-lxc-install-config.yml` — declared the deps it uses in the
  `Install Packages` task: `firewalld`, `python3-firewall`, `dnsmasq`, `iptables-nft`,
  `NetworkManager` (each with a WHY comment citing "Missing Dependencies — Fix in IaC"), and
  added an `Ensure firewalld is running` systemd task before the zone-bind (Cloud Base does
  not start firewalld by default, and the `immediate: true` zone-bind needs the live daemon).
  `--syntax-check` rc=0. Net −29 lines (dep list + start task, combined with 1.4).
- [x] ✅ **Task 1.4**: `play-lxc-install-config.yml` — switched the `lxc-bash` clone to
  **HTTPS** (public repo) via `ansible.builtin.git` + a `lineinfile` for completion, deleting
  the vault-passphrase assert, the passphrase temp file, the `ssh -T` probe, the `git@` SSH
  clone, and the `always:` cleanup (~65 lines → ~13). Removes the GitHub-SSH hard dep.
  `--syntax-check` rc=0.
- [ ] 🔄 **Task 1.5**: Run QA: `./scripts/qa-all.bash`; fix findings. NOTE: full `qa-all.bash`
  cannot run in the ballicom-infra CCY container (`ruff` not provisioned here — a
  fedora-desktop-HOST tool; missing-required-tool = hard fail by design). Per-play
  `ansible-playbook --syntax-check` DOES run and passed for 1.1/1.2/1.3/1.4 — the LXC play
  needed `ansible-galaxy collection install -r requirements.yml` first (its unchanged
  `community.general.copr`/`.modprobe` tasks need that collection, absent by default in this
  container); once installed, rc=0. Edits touch no Python. **Full `qa-all.bash` must be run on
  the HOST** alongside the live test.

### Phase 2: Correct the container host (silent misbehaves)

- [x] ✅ **Task 2.1**: `play-systemd-user-tweaks.yml` — added, as the play's first three
  tasks, a `getent` UID lookup, `loginctl enable-linger` (`creates:` on the linger marker),
  AND an explicit `ansible.builtin.systemd: name: user@UID.service, state: started`. Per the
  Fable design decision (`reviews/2026-07-29-phase2-design-decision-fable.md`), the explicit
  synchronous start is what makes the Phase-2 hardening deterministic: `enable-linger` only
  QUEUES the manager-start async, but the systemd module blocks on the Type=notify job until
  READY=1, so `/run/user/UID` + the private socket are guaranteed present at the next task.
- [x] ✅ **Task 2.2**: `play-systemd-user-tweaks.yml` — gave the reload handler + verify task
  the explicit `XDG_RUNTIME_DIR` (the var `systemctl --user` actually dials) + `DBUS_SESSION_BUS_ADDRESS` environment; **deleted the core tree's lone `ignore_errors: true`**;
  turned the verifier's `failed_when: false` into a real `assert` (`ManagedOOMMemoryPressure=auto`).
  Added a `meta: flush_handlers` before the verify so it reads the RELOADED user.slice (handlers
  otherwise flush at play end — the assert would trip on a first run otherwise).
- [x] ✅ **Task 2.3**: `play-podman.yml` — deleted the `dbus_session_check` probe-then-`when`
  anti-pattern; gave the `podman.socket` enable its own `become_user` + the same env block +
  a defensive `getent` (standalone-runnability), so the socket is enabled unconditionally,
  relying on T2.1's guarantee. Fails loud (not silently skips) on a never-provisioned box.
- [ ] 🔄 **Task 2.4**: Run QA. `ansible-playbook --syntax-check` rc=0 on both edited plays;
  no unannotated `failed_when: false`/`ignore_errors` remain (the two suppressions were
  removed, none added). Full `qa-all.bash` deferred to HOST (no ruff here).

### Phase 3: Fail-fast edition preflight + reboot-persistence

- [x] ✅ **Task 3.1**: `play-AA-preflight-sanity.yml` — added a `stat`+`assert` that fails
  loud at play 1 on an **rpm-ostree / atomic** Fedora image (Silverblue/Kinoite/CoreOS/IoT/
  uBlue). Signal = `/run/ostree-booted` (boot-truth marker of an ostree-managed root — the
  actual failure mechanism, since dnf/rpm + `/etc` writes break there), NOT a `VARIANT_ID`
  allowlist: `/run/ostree-booted` cannot false-reject Workstation/Server/Cloud and needs no
  per-spin maintenance. Design: `reviews/2026-07-29-phase3-design-decision-fable.md` D1.
- [x] ✅ **Task 3.2**: `play-lxc-install-config.yml` F8 — persist the DOCKER-USER ACCEPT +
  MASQUERADE rules across reboot AND docker restart via a systemd oneshot unit
  `lxc-docker-user-iptables-reconcile.service` (`After=/Requires=/PartOf=docker.service`,
  `WantedBy=multi-user.target`) driven by ONE shared idempotent script
  (`files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash`) that BOTH Ansible (immediate
  apply) and the unit (boot + docker-restart persistence) invoke. Chose this over firewalld
  direct rules (can't resync after docker flushes DOCKER-USER — solves only the reboot half)
  and iptables-save/restore (snapshots Docker's own dynamic chains — wrong abstraction). The
  inline derive/validate/3×probe-insert block was replaced by deploy-script → apply-now →
  deploy-unit → enable+start. Design: `reviews/2026-07-29-phase3-design-decision-fable.md` D2.
  **HOST-test-warranting** (design flagged, not a confident one-shot): the `PartOf=`
  restart-propagation onto a `Type=oneshot` unit and the docker-chain-creation-before-active
  ordering assumption must be exercised live (reboot + `systemctl restart docker` + container
  egress checks) — folded into Phase 4's HOST criteria below.
- [ ] ⬜ **Task 3.3**: (optional) enhancement gating — desktop multimedia + GUI/audio dev
  headers behind `when: provisioning_profile != 'server'`. Deferred (enhancement, not a
  blocker; needs its own `play-rpm-fusion.yml`/`play-python.yml` task audit).
- [ ] 🔄 **Task 3.4**: Run QA. `ansible-playbook --syntax-check` rc=0 on both edited plays;
  `bash -n` + shellcheck (present in this container) both PASS on the new reconcile script.
  Full `qa-all.bash` deferred to HOST (no ruff here).

### Phase 4: Review + hand-off (HOST-run test)

- [ ] ⬜ **Task 4.1**: Adversarial review pass over all play edits (this repo cannot run
  Ansible in the CCY container — QA is syntax/lint only); persist review notes in this folder.
- [ ] ⬜ **Task 4.2**: Commit (do NOT push — hand to the human to push + run the HOST test:
  the first `run.bash` server-profile execution on a fresh Cloud Base VM). Beyond "reaches
  ALL DONE", the HOST test MUST exercise the F8 persistence unit (design-flagged as
  not-a-confident-one-shot):
  1. `reboot` with an LXC container configured → confirm container outbound connectivity
     **without re-running Ansible**.
  2. `systemctl restart docker` with a container running → `journalctl -u lxc-docker-user-iptables-reconcile.service` shows a fresh successful run and
     `iptables -n -L DOCKER-USER` shows the ACCEPT rules again → confirm container egress.
  3. Confirm the ostree preflight (T3.1) does NOT reject the Cloud Base target (it must pass).
     If a boot-ordering timing gap appears, the likely fix is a bounded `ExecStartPre=` wait
     (`until iptables -n -L DOCKER-USER`, capped) — confirm live, do not assume.

### Phase 5: Server-recommended optional-play bundle (feature)

Today `RUN_BASH_OPTIONAL_PLAYBOOKS` requires the operator to hand-list plays. Add a curated
"recommended for a server" set selectable in one shot, so a stock headless dev/server box is
one keyword away.

- [ ] ⬜ **Task 5.1**: Design the selection mechanism — a named bundle keyword
  (`server-recommended`) that `run.bash` expands, and/or a per-play "server-recommended"
  marker the resolver reads. Prefer one source of truth; must remain overridable/extendable
  by an explicit play list.
- [ ] ⬜ **Task 5.2**: Curate the **generically** server-useful set (dev toolchains + CLI
  helpers that make sense on ANY headless Fedora server — NOT org-specific picks). Candidate:
  `play-golang`, `play-rust-dev`, `play-distrobox`, `play-network-tools`, `play-rclone`,
  `play-open-command`, `play-compression-helpers`, `play-disk-reclaim`,
  `play-advanced-kernel-management`, `play-container-watch`, `play-claude-devtools`,
  `play-collaboration` (tmate). Deliberately exclude opinionated/credentialed ones
  (`play-lastpass`, `play-ddev`) and VPN clients — those stay explicit opt-ins. All must be
  `scope: general` (a `gnome` play in the bundle would silently no-op on server).
- [ ] ⬜ **Task 5.3**: Wire `run.bash` to accept the bundle keyword; document it in
  `--help-run-headless` + `docs/headless-*.md`. Every bundle play must itself be headless-safe
  (Phases 1-3 are a prerequisite — no point recommending a play that aborts the run).
- [ ] ⬜ **Task 5.4**: Run QA.

> Note: this is the **upstream, generic** notion of "server-recommended". A downstream
> consumer can still append its own org-specific plays via an explicit
> `RUN_BASH_OPTIONAL_PLAYBOOKS` list on top of (or instead of) the bundle.

## Success Criteria

- [ ] A fresh Fedora Cloud Base headless `run.bash` server run reaches `ALL DONE` (the human
  HOST test).
- [ ] `./scripts/qa-all.bash` passes for every edit (bash + ansible syntax + patterns).
- [ ] No new `failed_when: false` / `ignore_errors` without a `# FAIL-FAST-OK:` justification;
  the one existing `ignore_errors` in the core tree is removed, not re-justified.
- [ ] An unsupported edition is rejected at preflight (play 1), not deep in the run.

## Delivery & Milestones

- Recovery cron (this session, non-durable): `bd15a27d`.
- Scaffolded: plan 00065 created.
