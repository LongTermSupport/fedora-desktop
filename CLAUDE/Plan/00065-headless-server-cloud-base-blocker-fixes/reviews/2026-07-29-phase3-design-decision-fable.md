# Phase-3 Design Decision — Plan 00065 (T3.1 / T3.2 / F8)

Reviewed against a freshly-pulled `LongTermSupport/fedora-desktop` checkout at `/workspace/untracked/repos/fedora-desktop`, branch `F44`. CCY container — design/edit only; no Ansible run (`--syntax-check` only would be permitted, not exercised in this pass since no playbook edit was made). This document proposes exact task/file content; it does not itself modify any playbook.

**DECISION 1 (T3.1): allowlist-of-editions is wrong — gate on `/run/ostree-booted` (blocklist-of-atomic), single check, no `VARIANT_ID` allowlist.**

**DECISION 2 (T3.2 / F8): a systemd oneshot unit `lxc-docker-user-iptables-reconcile.service`, `After=/Requires=/PartOf=docker.service`, `WantedBy=multi-user.target`, driven by ONE shared idempotent script that both Ansible (immediate apply) and the unit (boot + docker-restart persistence) invoke — not firewalld, not iptables-save/restore.**

---

## DECISION 1 (T3.1): edition/flavour fail-fast preflight

### The reliable signal

Four candidate signals, evaluated against the constraint that this repo must accept **Workstation, Server, and Cloud Base** (and, by extension, any other traditional dnf/rpm Fedora spin — KDE/Xfce/etc. spins are not rpm-ostree) and reject only **rpm-ostree / atomic** variants (Silverblue, Kinoite, Fedora CoreOS, Fedora IoT, and uBlue-style atomic derivatives):

1. **`VARIANT_ID`** (`/etc/os-release`) — `workstation`/`server`/`cloud` on the traditional editions; `silverblue`/`kinoite`/`coreos`/`iot` on the atomic ones. Real and currently accurate, but it is an **enumeration that must be maintained forever**: Fedora ships new Atomic Desktop spins on its own cadence (Sericea/Sway, Onyx/Budgie, etc.), and uBlue-style rebased images routinely **customise `VARIANT_ID`** to their own brand or leave it as whatever the base image shipped, which can drift away from the values this repo would hardcode. An allowlist-of-supported values (`workstation`, `server`, `cloud`) is *almost* safe (unknown-value → reject, so a new atomic spin gets caught by omission) but it also catches every **future legitimate dnf-based spin** unless someone remembers to widen the allowlist — the wrong failure direction for a "don't reject desktop/server/cloud" mandate.
2. **`fedora-release-<edition>` RPM presence** — exists, but is a package-database check (`rpm -q`), one more layer removed from "is this box currently ostree-managed," and atomic images don't necessarily carry a `fedora-release-<edition>` RPM in the traditional sense (their release info is baked into the ostree commit, not a discrete queryable RPM in all cases).
3. **`/etc/system-release-cpe`** — a CPE string (`cpe:/o:fedoraproject:fedora:44`) that does **not** encode edition at all on modern Fedora; not useful here.
4. **`/run/ostree-booted`** — a zero-byte marker file created during early boot (by `ostree-prepare-root`/the ostree-aware initramfs) **if and only if the currently-running root filesystem is an OSTree deployment**. This is systemd's and multiple upstream tools' own canonical test for "am I on an atomic system" (it is deliberately kernel/boot-time truth, not a self-reported string in a text file an image builder could omit or customise away).

**Verdict: assert `not /run/ostree-booted exists`, and do NOT also gate on a `VARIANT_ID` allowlist.** The `stat` check is:

- **A positive, structural detector of the actual failure mode**, not a proxy for it. The reason atomic systems break this repo is mechanical: `dnf`/`rpm`/`package` tasks fail outright (`rpm-ostree` is a transactional image-layering tool, not `dnf`) and direct writes to `/etc` are either rejected or silently discarded on the next boot (the `/etc` on an ostree system is a per-deployment writable overlay, not the persistent target `copy`/`template`/`blockinfile` assume). `/run/ostree-booted` asserts precisely "is that mechanical assumption true," independent of what any edition ever calls itself.
- **Future-proof by construction.** A new Atomic Desktop spin, or a rebased uBlue image with a fully custom `VARIANT_ID`, is still booted via ostree — the kernel-truth marker still fires. No allowlist maintenance is required as Fedora's spin lineup evolves.
- **Cannot false-positive against desktop/server/cloud.** None of Workstation, Server, or Cloud Base is ostree-managed; the check has zero risk of rejecting a supported target, which is the plan's explicit hard constraint (Non-Goals: "Not changing the supported OS set").
- **Harder to spoof/misconfigure than a self-reported string field.** `VARIANT_ID` lives in a plain text file an image author writes; `/run/ostree-booted` is created by the boot process itself as a side effect of how the root filesystem is actually mounted.

A `VARIANT_ID` allowlist would be strictly worse here: it adds maintenance burden (must be updated as new atomic spins/rebases appear) without adding coverage the ostree marker doesn't already give, and it introduces a second, redundant gate that could reject a legitimate future dnf-based spin whose `VARIANT_ID` was never added to the list. One check, one clear failure mode, done.

### Exact task(s) — `playbooks/imports/play-AA-preflight-sanity.yml`

Insert as a new task immediately after the existing "Check Fedora" task (`play-AA-preflight-sanity.yml:21-27`) and before "Check provisioning_profile is a recognised value" (`:29-38`) — grouping it with the other "is this the Fedora this repo expects" checks:

```yaml
    - name: Check for an rpm-ostree / atomic Fedora variant
      ansible.builtin.stat:
        path: /run/ostree-booted
      register: _ostree_booted_marker

    - name: Assert this is not an rpm-ostree / atomic Fedora image
      ansible.builtin.assert:
        that:
          - not _ostree_booted_marker.stat.exists
        fail_msg: |
          This system is booted from an rpm-ostree / atomic Fedora image
          (detected via /run/ostree-booted) — e.g. Silverblue, Kinoite,
          Fedora CoreOS, Fedora IoT, or an Atomic Desktop / uBlue derivative.

          fedora-desktop is built entirely on dnf/rpm package installs and
          direct writes to /etc, both of which an ostree-managed root either
          rejects outright or silently discards on the next boot. Running
          this project against an atomic image WILL fail, and typically only
          deep into the run with a confusing, misdiagnosing error — this
          preflight exists to catch it here instead, at play 1.

          Supported: Fedora Workstation, Server, and Cloud Base — this one
          tree provisions both a desktop and a headless server (see
          provisioning_profile below); any traditional dnf/rpm-based Fedora
          spin works the same way.

          Not supported: any rpm-ostree-based Fedora variant.
```

Both tasks are plain modules (`stat` + `assert`), so none of the Ansible 2.19 `shell:` parser gotchas in `CLAUDE/AgentNotes.md` apply, and `--syntax-check` is sufficient to validate them structurally. No new dependency is introduced — `stat` is a core module already used elsewhere in this exact file's LXC play (`play-lxc-install-config.yml:22-25`, same `stat` + `assert` shape for the Docker-ordering guard) — this repeats an established, reviewed pattern rather than inventing a new one.

---

## DECISION 2 (T3.2 / F8): persisting the DOCKER-USER egress rules across reboot / docker restart

### Ruling out (a) and (c)

**(a) firewalld direct/`--permanent` rules — rejected, unreliable for this exact chain.** Docker owns the lifecycle of the `DOCKER-USER` chain: it creates it (if absent) and — per this play's own existing comment (`play-lxc-install-config.yml:292-294`) — **flushes/recreates it whenever `docker.service` (re)starts**. firewalld's `--direct` interface can insert a rule into an arbitrary chain, but it only **applies** its stored direct rules when firewalld itself starts or reloads — it has no mechanism to notice that some other daemon (dockerd) just flushed a chain out from under it, so it would not resync after a `systemctl restart docker`. That is exactly one of the two trigger conditions F8 names ("vanish on host reboot **or a Docker daemon restart**"), so firewalld direct rules solve at most half the problem. There is also a real boot-ordering hazard even for the reboot half: firewalld and docker.service have no dependency on each other by default, so if firewalld's reload runs before dockerd has (re)created `DOCKER-USER`, the direct-rule insert targets a chain that does not yet exist. Docker's own iptables management operates outside firewalld's bookkeeping by design (this is precisely why the play already has to reconcile the two by hand) — layering firewalld on top does not change that, it just adds a second system that also doesn't know when the chain was recreated.

**(c) iptables-save/iptables-restore drop-in — rejected, wrong level of abstraction.** This would mean installing `iptables-services` (a new dependency this plan is elsewhere working to avoid introducing gratuitously) and snapshotting the **entire live ruleset** — including Docker's own dynamically-managed NAT/filter chains for whatever containers happen to be running at snapshot time — into a static file restored at boot **before** Docker has started and rebuilt its own chains fresh. Restoring a stale full-ruleset dump against chains Docker expects to own and recreate itself is a materially worse consistency hazard than inserting three well-scoped rules, and it is not naturally re-runnable/idempotent the way a small targeted script is (a snapshot taken at one point in time can silently go stale relative to Docker's own state). This is the right tool for "persist my whole custom firewall," not for "keep 3 specific rules in a chain Docker itself manages."

### Verdict: (b), consolidated into one script

A systemd oneshot unit, **`lxc-docker-user-iptables-reconcile.service`**, ordered after and bound to `docker.service`, is the right mechanism — but it should not duplicate the reconciliation logic that already lives inline in this play (`play-lxc-install-config.yml:295-352`: derive subnet → validate → 3× probe-then-insert). Duplicating that logic into a second, systemd-invoked copy would violate this repo's own DRY principle (`CLAUDE.md`: "Reference, don't duplicate") and create exactly the maintenance hazard where the live-apply logic and the persisted logic silently drift apart (e.g. someone changes the ACCEPT rule shape in one place and forgets the other, and the two paths disagree about "what correct looks like" after the next reboot).

**Deliverable shape: one idempotent script, called from two places.**

#### 1. `files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash` (new, mode `0755`, owner/group `root`)

```bash
#!/usr/bin/env bash
# ANSIBLE MANAGED — edit the source in the fedora-desktop repo
# (files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash) and redeploy
# via playbooks/imports/play-lxc-install-config.yml. Do not edit on the target.
#
# Re-applies the DOCKER-USER ACCEPT rules + POSTROUTING MASQUERADE rule that
# give LXC containers on lxcbr0 outbound connectivity through Docker's
# DOCKER-USER chain. These rules are RUNTIME-ONLY kernel state — they vanish
# on host reboot and whenever docker.service restarts (Docker flushes/
# recreates DOCKER-USER on daemon start). This script is the single source of
# truth for the rule set; it is invoked BOTH by play-lxc-install-config.yml
# during provisioning (immediate apply) AND by
# lxc-docker-user-iptables-reconcile.service, which is ordered/bound to
# docker.service so it reruns on every boot and every docker.service restart
# (the persist-across-reboot path).
#
# Idempotent: every rule is probed with `iptables -C` before insertion, so
# re-running never duplicates a rule. Prints a stable marker line so Ansible
# can derive changed_when without parsing free-form output.
set -euo pipefail

readonly bridge=lxcbr0

if ! iptables -n -L DOCKER-USER; then
    echo "lxc-docker-user-iptables-reconcile: DOCKER-USER chain does not exist" \
         "(is docker.service actually up?) — refusing to proceed" >&2
    exit 1
fi

subnet_cidr="$(ip -o -4 route show dev "$bridge" proto kernel | awk '{print $1}' | head -n1)"
if [ -z "$subnet_cidr" ]; then
    echo "lxc-docker-user-iptables-reconcile: could not derive ${bridge} subnet" \
         "from 'ip route show dev ${bridge} proto kernel' — is lxc-net running?" >&2
    exit 1
fi

changed=0

if ! iptables -C DOCKER-USER -i "$bridge" -j ACCEPT; then
    iptables -I DOCKER-USER -i "$bridge" -j ACCEPT
    changed=1
fi

if ! iptables -C DOCKER-USER -o "$bridge" -j ACCEPT; then
    iptables -I DOCKER-USER -o "$bridge" -j ACCEPT
    changed=1
fi

if ! iptables -t nat -C POSTROUTING -s "$subnet_cidr" ! -o "$bridge" -j MASQUERADE; then
    iptables -t nat -I POSTROUTING -s "$subnet_cidr" ! -o "$bridge" -j MASQUERADE
    changed=1
fi

if [ "$changed" -eq 1 ]; then
    echo "LXC-IPTABLES-RECONCILE-CHANGED"
else
    echo "LXC-IPTABLES-RECONCILE-NOOP"
fi
```

No `Python helper under helpers/` extraction is required here even though the script has conditionals: the "complex logic → TDD helper" rule in `playbooks/CLAUDE.md` targets **inline** Ansible `shell:`/`command:` module content parsed by the 2.19 `split_args` splitter — a standalone script file invoked via `ansible.builtin.command:` (or, at boot, by `ExecStart=`) is never passed through that splitter at all, so the rule's rationale does not apply. This is the same category of artifact as the repo's existing deployed executables (`ssh-suspend-guard`, `manage-kernel-versions.py`, `wsi`, etc.) — a checked-in, `set -euo pipefail`, fail-fast script under `files/usr/local/bin/`, reviewed by `qa-bash.bash`/shellcheck like any other repo-owned bash. No `2>/dev/null` is used on the `iptables -C` probes (would trip `error_hiding_blocker`/the repo's own no-silent-failure rule) — the "Bad rule" stderr line `iptables -C` prints when a rule is absent is expected, informative, and the explicit `if !` check is what drives the real logic, matching the endorsed "probe-then-fail, result explicitly checked" pattern already used by this same play's existing tasks.

#### 2. `files/etc/systemd/system/lxc-docker-user-iptables-reconcile.service` (new, mode `0644`)

```ini
[Unit]
Description=Reconcile DOCKER-USER iptables rules for LXC bridge egress
Documentation=file:///usr/local/bin/lxc-docker-user-iptables-reconcile.bash
After=docker.service
Requires=docker.service
PartOf=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/lxc-docker-user-iptables-reconcile.bash
StandardOutput=journal
StandardError=journal
SyslogIdentifier=lxc-docker-user-iptables-reconcile

[Install]
WantedBy=multi-user.target
```

- `Requires=docker.service` + `After=docker.service`: at boot, `multi-user.target` pulling in this unit (via `WantedBy=`) also pulls in and orders `docker.service` first — so `DOCKER-USER` exists by the time `ExecStart` runs (Docker creates/populates its chains early in its own startup, before dockerd signals ready).
- `PartOf=docker.service`: systemd propagates **stop and restart** actions from `docker.service` onto this unit — so `systemctl restart docker` (which flushes `DOCKER-USER`) also restarts this unit, rerunning `ExecStart` and reinserting the rules. This is what makes the design actually answer the "or a Docker daemon restart" half of F8, not just the reboot half.
- `Type=oneshot` + `RemainAfterExit=yes`: the unit reports "active" after a successful one-shot run (standard idiom for a reconcile-on-trigger job with no long-running process), and re-triggers cleanly on the next `PartOf`-propagated restart.
- File deployment matches the exact precedent already in this repo for a non-templated unit — `files/etc/systemd/system/displaylink-dock-recovery.service`, deployed via plain `ansible.builtin.copy:` (`playbooks/imports/optional/hardware-specific/play-displaylink.yml:250-257`) — no Jinja substitution is needed in either the script or the unit (the subnet is derived live at run time, exactly as the current inline tasks already do), so `copy:` is correct here rather than `template:` (per the file-placement convention: templates only when a file needs variable substitution).

#### 3. Ansible tasks — replace the current derive/validate/3×probe-insert block

In `play-lxc-install-config.yml`, **keep** "Assert DOCKER-USER iptables chain exists" (`:295-298`) as an early, Ansible-native fail-fast signal, then **replace** "Derive LXC subnet CIDR..." / "Validate LXC subnet CIDR..." / the three probe-then-insert pairs (`:300-352`) with:

```yaml
    - name: Deploy DOCKER-USER iptables reconciliation script
      ansible.builtin.copy:
        src: "{{ root_dir }}/files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash"
        dest: /usr/local/bin/lxc-docker-user-iptables-reconcile.bash
        owner: root
        group: root
        mode: "0755"
      tags: [lxc_iptables]

    - name: Apply DOCKER-USER iptables rules now (this session)
      ansible.builtin.command: /usr/local/bin/lxc-docker-user-iptables-reconcile.bash
      register: _lxc_iptables_reconcile
      changed_when: "'LXC-IPTABLES-RECONCILE-CHANGED' in _lxc_iptables_reconcile.stdout"
      tags: [lxc_iptables]

    - name: Deploy DOCKER-USER iptables reconciliation systemd unit (reboot/docker-restart persistence)
      ansible.builtin.copy:
        src: "{{ root_dir }}/files/etc/systemd/system/lxc-docker-user-iptables-reconcile.service"
        dest: /etc/systemd/system/lxc-docker-user-iptables-reconcile.service
        owner: root
        group: root
        mode: "0644"
      tags: [lxc_iptables]

    - name: Enable + start the DOCKER-USER iptables reconciliation unit
      ansible.builtin.systemd:
        name: lxc-docker-user-iptables-reconcile.service
        daemon_reload: true
        enabled: true
        state: started
      tags: [lxc_iptables]
```

`state: started` (not `restarted`) is deliberate and mirrors this play's own existing "ANS-09" comment convention for `lxc.service` (`:77-78`): the immediate-apply task above has already reconciled the live ruleset for *this* session, so the unit only needs to be guaranteed enabled and started at least once — it does not need to be bounced on every Ansible run.

**Ordering answer, explicitly:** yes — the immediate `iptables -I`-equivalent apply (now the "Apply DOCKER-USER iptables rules now" task calling the shared script) stays the **immediate-apply path** for the current provisioning run, and the new unit is purely the **persist-across-reboot / persist-across-docker-restart** path. They are not redundant: the Ansible task guarantees correctness right now (including on a box where the unit hasn't been installed yet, i.e. first run); the unit guarantees the same correctness holds after Ansible is no longer running.

### Honest risk assessment — this warrants HOST-test iteration, not a confident one-shot

This is genuinely one of the riskier deliverables in this plan, and should not be treated as done on design confidence alone:

- The `Requires=`/`After=`/`PartOf=docker.service` interaction is standard systemd idiom, but its correctness here rests on an assumption — that Docker's own chain creation completes strictly before `docker.service` is reported "active" — that is very likely true (Docker sets up its iptables chains early in its startup sequence, before its API/readiness signal) but has not been verified live against this repo's actual Fedora/Docker package versions.
- `PartOf=` propagating a **restart** (not just a stop) onto a `Type=oneshot`/`RemainAfterExit=yes` unit is correct systemd semantics, but is exactly the kind of interaction that "looks right on paper" and deserves a live `systemctl restart docker` + `iptables -n -L DOCKER-USER` check rather than being trusted from design review alone.
- Recommend the Phase 4 HOST test explicitly exercise, in addition to the plan's existing "reaches ALL DONE" criterion:
  1. A genuine `reboot` with an LXC container already configured — confirm outbound connectivity from inside the container works **without re-running Ansible**.
  2. `systemctl restart docker` while an LXC container is running — confirm `journalctl -u lxc-docker-user-iptables-reconcile.service` shows a fresh successful run and `iptables -n -L DOCKER-USER` shows the ACCEPT rules again, then confirm container egress.
  3. A fresh Cloud Base firstboot (the plan's actual target profile) specifically, since minimal-image boot ordering/timing is the one variable this design cannot fully reason about from a desktop-shaped mental model.

If any of these surface a timing gap, the fix is very likely a small addition (e.g. `ExecStartPre=` a bounded `until iptables -n -L DOCKER-USER` wait, still fail-fast with a cap) rather than a redesign — but that should be confirmed live, not assumed.

---

## Implementation checklist

1. **`play-AA-preflight-sanity.yml`** — insert the `stat` + `assert` pair (Decision 1) after "Check Fedora", before "Check provisioning_profile is a recognised value". Run `ansible-playbook --syntax-check playbooks/imports/play-AA-preflight-sanity.yml`.
2. **New file** `files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash` (mode `0755` in git, `set -euo pipefail`, content above). Run `bash -n` / shellcheck via `qa-bash.bash`.
3. **New file** `files/etc/systemd/system/lxc-docker-user-iptables-reconcile.service` (content above).
4. **`play-lxc-install-config.yml`** — keep the existing "Assert DOCKER-USER iptables chain exists" task; replace "Derive LXC subnet CIDR…" / "Validate LXC subnet CIDR…" / the three probe-then-insert pairs with the four tasks in Decision 2 §3 (deploy script → apply now → deploy unit → enable+start). Run `ansible-playbook --syntax-check playbooks/imports/play-lxc-install-config.yml`.
5. Run `./scripts/qa-all.bash` (Phase 3 Task 3.4) — note the known container limitation already logged in this plan (no `ruff`; run the full gate on the HOST).
6. Update `PLAN.md` Task 3.1/3.2 status once implemented; leave 3.3/4.x untouched (out of scope for this design pass).
7. **Do not skip the HOST test callouts above** for T3.2 — this is the one deliverable in this pass where design review is not a substitute for a live reboot + live `docker restart` check.
