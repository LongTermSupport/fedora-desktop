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

- [ ] ⬜ **Task 1.1**: `play-markless.yml` — replace `cd ~/Downloads` with a `mktemp -d`
  scratch dir (`trap 'rm -rf' EXIT`); removes the missing-dir dependency and the
  `rm -rf markless*` shared-dir hazard. Play stays `general`.
- [ ] ⬜ **Task 1.2**: `play-basic-configs.yml` — gate the fwupd task
  `when: provisioning_profile != 'server'` (precedent: the USB-audio task ~11 lines above),
  with a WHY comment (no firmware surface on a VM/headless target).
- [ ] ⬜ **Task 1.3**: `play-lxc-install-config.yml` — declare the deps it uses via
  `ansible.builtin.dnf`: `firewalld` (+ `python3-firewall`, start the daemon), `dnsmasq`,
  `iptables-nft`, `NetworkManager`. Widen the dnsmasq `fail_msg` to name both causes.
- [ ] ⬜ **Task 1.4**: `play-lxc-install-config.yml` — switch the `lxc-bash` clone to
  **HTTPS** (public repo), deleting the vault-passphrase assert, the passphrase temp file,
  the `ssh -T` probe, and the `always:` cleanup (~65 lines). Removes the GitHub-SSH hard dep.
- [ ] ⬜ **Task 1.5**: Run QA: `./scripts/qa-all.bash`; fix findings.

### Phase 2: Correct the container host (silent misbehaves)

- [ ] ⬜ **Task 2.1**: Add a `loginctl enable-linger {{ user_login }}` task (with
  `creates: /var/lib/systemd/linger/{{ user_login }}`) before the first `systemctl --user`
  in the core run — shape already in `play-rclone.yml`.
- [ ] ⬜ **Task 2.2**: `play-systemd-user-tweaks.yml` — give the handler + verify task the
  explicit `XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS` environment (shape in
  `play-prevent-ssh-suspend.yml`), then **delete the `ignore_errors: true`** and turn the
  verifier's `failed_when: false` into a real assertion.
- [ ] ⬜ **Task 2.3**: `play-podman.yml` — with linger in place, drop the `when:` so
  `podman.socket` is enabled unconditionally (or, minimum, make the skip a declared
  `when: provisioning_profile != 'server'`, not an accidental probe-fail).
- [ ] ⬜ **Task 2.4**: Run QA.

### Phase 3: Fail-fast edition preflight + reboot-persistence

- [ ] ⬜ **Task 3.1**: `play-AA-preflight-sanity.yml` — add an edition/flavour assertion that
  fails loud on an unsupported image (design the concrete signal; `VERSION_ID` is ruled out).
- [ ] ⬜ **Task 3.2**: `play-lxc-install-config.yml` F8 — persist the DOCKER-USER rules
  (firewalld direct/policy `permanent: true`, or a oneshot unit `After=docker.service`) so
  container egress survives reboot.
- [ ] ⬜ **Task 3.3**: (optional) enhancement gating — desktop multimedia + GUI/audio dev
  headers behind `when: provisioning_profile != 'server'`.
- [ ] ⬜ **Task 3.4**: Run QA.

### Phase 4: Review + hand-off (HOST-run test)

- [ ] ⬜ **Task 4.1**: Adversarial review pass over all play edits (this repo cannot run
  Ansible in the CCY container — QA is syntax/lint only); persist review notes in this folder.
- [ ] ⬜ **Task 4.2**: Commit (do NOT push — hand to the human to push + run the HOST test:
  the first `run.bash` server-profile execution on a fresh Cloud Base VM).

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
