# Plan 00110 — Design: full fresh-install lifecycle acceptance testing against VMs

This is the design document for Plan 00110. `PLAN.md` owns status and tasks; this file owns
the architecture, the decisions, and the evidence behind them.

---

## 0. What was verified, and what was not

Everything in this document that is stated as fact was checked. Everything that could not
be checked from a CCY container is marked **UNVERIFIED** and carries a Phase-0 triage task.

### Verified in this session

| Claim                                                                        | Evidence                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No VM tooling exists in this repo today                                      | `grep -rniE 'libvirt\|qemu\|virt-install\|virt-manager\|proxmox\|virsh\|vagrant\|kvm'` over `playbooks/ vars/ scripts/` returns nothing                                                                                                                         |
| `fedora_version: 44` is the single source of truth                           | `vars/fedora-version.yml:6`; read by 24 tracked files including `run.bash`, `scripts/fedora-upgrade.bash` and 8 playbooks                                                                                                                                       |
| `run.bash` is at v1.19.0 with the documented headless contract               | `run.bash:9`, `run.bash:757-943`                                                                                                                                                                                                                                |
| The vault password is required in **every** headless run                     | `docs/headless-provisioning.md:92`; `ansible.cfg:42` needs a readable `vault-pass.secret` to start at all                                                                                                                                                       |
| `RUN_BASH_GITHUB_ACCOUNTS=none` needs no token and no SSH passphrase         | `run.bash:877-879`, `docs/headless-provisioning.md:55-60`                                                                                                                                                                                                       |
| `RUN_BASH_GIT_REF` replaces `git pull` when set                              | `run.bash:1993-2004`                                                                                                                                                                                                                                            |
| The preflight play asserts the guest's Fedora major matches `fedora_version` | `playbooks/imports/play-AA-preflight-sanity.yml:22-27`                                                                                                                                                                                                          |
| Cloud Base is an explicitly supported target                                 | `playbooks/imports/play-AA-preflight-sanity.yml:52-53`                                                                                                                                                                                                          |
| `provisioning_profile` is derived from `systemctl get-default`               | `environment/localhost/group_vars/desktop.yml:25-26`                                                                                                                                                                                                            |
| 10 core plays are `gnome`-scoped and skipped on the server profile           | enumerated below, §5                                                                                                                                                                                                                                            |
| Many plays need `/run/user/<uid>/bus`                                        | `play-gnome-shell-extensions.yml:126`, `play-systemd-user-tweaks.yml:174-175`, `play-podman.yml:44-45`, `play-suspend-and-lid-policy.yml:185,209`, `play-prevent-ssh-suspend.yml:62`, `play-rclone.yml:209-210,412-413,617-618`, `play-container-watch.yml:125` |
| The extension gate is **vacuous** with no GNOME session                      | `helpers/gnome/extension_state.py:58-61` returns `Verdict.SKIP_NO_SESSION`; `helpers/gnome/verify_extension.py:111` then prints `EXT-OK` and exits 0                                                                                                            |
| The repo configures GDM autologin nowhere                                    | `grep -rn 'gdm\|autologin\|AutomaticLogin' playbooks/ fedora-install/ks.cfg files/` finds only `fedora-install/ks.cfg:691` `systemctl enable gdm.service`                                                                                                       |
| `fedora-install/ks.cfg` is interactive by design and not reusable for a VM   | `ks.cfg:13-70` opens `/dev/tty6` and prompts for an authorisation code, then WiFi/LUKS/user                                                                                                                                                                     |
| `fedora-install/setup-netinstall-boot.bash` is directly relevant prior art   | ISO discovery `:619-656`, conditional download `:658-697`, GPG + SHA-256 verification `:699-791`, `liveimg` squashfs extraction `:793-810`                                                                                                                      |
| `untracked/` is self-ignoring, so a spool there needs no new mount           | `untracked/.gitignore` is `*` + `!.gitignore`                                                                                                                                                                                                                   |
| CCY binds the checkout with a plain `-v "$PWD:/workspace"`                   | `files/var/local/claude-yolo/claude-yolo:1946`                                                                                                                                                                                                                  |
| `CLAUDE/Plan/**/logs/` and `CLAUDE/Plan/**/\*-runs/` are gitignored          | root `.gitignore` final stanza; `CLAUDE/Plan/.gitignore`                                                                                                                                                                                                        |
| Every Fedora 44 package this design names exists                             | queried against `https://mdapi.fedoraproject.org/f44/pkg/<name>` — see §9 Phase 2 for the versions                                                                                                                                                              |
| `libguestfs-tools-c` no longer exists on F44                                 | mdapi returns HTTP 400; the correct package is `guestfs-tools` 1.56.0-1.fc44                                                                                                                                                                                    |

### Verified upstream, live, from this container

All five of the freshness signals in §4 were fetched and parsed today. Sizes, values and
the exact URLs are in §4. The one-line summary: **`releases/44/COMPOSE_ID` is
`Fedora-44-20260422.1` and does not move, while `updates/44/.../repomd.xml`'s `<revision>`
is `1789172543` (2026-09-12T00:22:23Z) and moves constantly.** That asymmetry is the whole
answer to the owner's question 7.

### UNVERIFIED — Phase-0 triage items

None of these can be answered from a container with no `/dev/kvm`, no `virsh`, no
`qemu-img` and no systemd of its own. They are **tasks, not assumptions**, and every one of
them is a probe inside `triage.bash` per [PlanTriage.md](../../PlanTriage.md).

- U1 — whether the host has KVM, how much free space the lab filesystem has, and whether
  that filesystem supports `discard`/`fstrim` on the backing store.
- U2 — whether `qemu:///session` (rootless libvirt) gives a workable host→guest channel on
  this host, or whether `qemu:///system` is required. §2 picks session-first and names the
  fallback.
- U3 — the systemd `%f` specifier expansion inside a **`.path`** unit's `PathModified=`.
  The design depends on it; §6 names the non-template fallback.
- U4 — which `virt-install --video` / `--graphics` combination boots a GNOME 44 Wayland
  session cleanly under this host's libvirt.
- U5 — the exact comps environment id for a GNOME desktop install (`@^workstation-product-environment`
  is the expected value). The F44 comps file is `zstd`-compressed and this container has no
  `zstd` and no `zstandard` module; the upstream comps source at pagure returned 404 and the
  GitLab mirror returned 403. **Not asserted.** Confirmed on the host with
  `dnf group list --hidden`. Note §5 does **not** depend on this: the desktop base uses
  `liveimg`, not a package environment. It matters only for the fallback route.
- U6 — `virt-install 5.1.0-4.fc44`'s exact `--cloud-init` and `--unattended` flag surface.

---

## 1. Scope and shape

**One sentence:** a host-resident, Ansible-provisioned VM lab that builds a fresh Fedora
base once, snapshots it, and runs the repo's real bootstrap (`run.bash` headless) against a
copy-on-write clone of that snapshot for both the `server` and `desktop` profiles, producing
a transcript and a three-valued verdict that a CCY-sandboxed agent can request over a
filesystem bridge and read back.

**Three deliverable layers**, each useful on its own:

1. **The freshness engine** (`helpers/vmtest/`) — pure, stdlib-only, TDD'd Python answering
   "is our base current, and has upstream changed?" Lands first, needs no VM, runs in a
   container.
2. **The lab** (`playbooks/imports/optional/common/play-vm-test-lab.yml` +
   `files/home/.local/bin/vmtest`) — the host-side capability. A human runs `vmtest` directly.
3. **The bridge** (`files/home/.local/bin/vmtest-bridge-watcher` + systemd user units +
   `scripts/vmtest-request.bash`) — the container→host trigger. Strictly additive; the lab
   works fully without it.

**These are persistent deliverables, not plan scaffolding.** Per
[PlanWorkflow.md](../../PlanWorkflow.md) "transient vs. persistent", a user-facing tool under
`files/`, a helper under `helpers/`, a gate script under `scripts/` and a playbook all stay
useful after the plan completes, so they live in the repo tree. Only `triage.bash`,
`deploy.bash` and `acceptance.bash` are plan-local, and those conform to
[PlanScriptStandards.md](../../PlanScriptStandards.md) R1–R14.

**Naming.** `vmtest` for the tool, `helpers/vmtest/` for the package,
`play-vm-test-lab.yml` for the play, `vmtest-bridge` for the spool and unit family. No
invented jargon; the tool name is what it does.

---

## 2. Architecture

### 2.1 Components and where they live

```
repo (tracked, public)
├── vars/fedora-version.yml                       [exists]  the release under test
├── vars/vm-test-scenarios.yml                    [new]     the scenario manifest — SOURCE of the allowlist
├── helpers/vmtest/
│   ├── upstream.py                               [new]     pure: parse .treeinfo/COMPOSE_ID/releases.json/repomd
│   ├── freshness.py                              [new]     pure: TTL + signal policy -> current|refresh|reinstall|unknown
│   ├── scenarios.py                              [new]     pure: manifest parse + planned-check accounting
│   ├── probe_upstream.py                         [new]     thin executor: HTTP + marker lines
│   └── verdict.py                                [new]     pure: response-document assembly and state transitions
├── tests/helpers/vmtest/test_*.py                [new]     unittest, stdlib only, written FIRST
├── files/home/.local/bin/
│   ├── vmtest                                    [new]     the host CLI (bash, thin executor)
│   └── vmtest-bridge-watcher                     [new]     the oneshot validator/dispatcher
├── files/home/.config/systemd/user/
│   ├── vmtest-bridge@.path                       [new]
│   └── vmtest-bridge@.service                    [new]
├── files/home/.local/share/vmtest/
│   ├── guest-acceptance-server.bash              [new]     runs INSIDE the guest
│   ├── guest-acceptance-desktop.bash             [new]     runs INSIDE the guest
│   └── guest-cleanup.bash                        [new]     pre-snapshot hygiene, runs INSIDE the guest
├── fedora-install/ks-vm-desktop.cfg              [new]     non-interactive kickstart, VM only
├── playbooks/imports/optional/common/play-vm-test-lab.yml   [new]
├── scripts/vmtest-request.bash                   [new]     container-side requester/reader
└── docs/vm-acceptance-testing.md                 [new]     user documentation

host, outside the repo (never writable from the sandbox)
~/.local/share/vmtest/
├── images/<upstream artefact>.qcow2|.iso                   checksum-verified downloads
├── bases/<profile>-<ver>/base.qcow2                        FLAT, opened read-only by runs
├── bases/<profile>-<ver>/base.json                         fingerprint + provenance
├── runs/<run_id>/{overlay.qcow2,seed.iso,console.log}
├── scenarios.allowlist                                     deployed by Ansible — the authority
└── guest-acceptance-*.bash                                 deployed by Ansible — the authority
~/.config/vmtest-bridge/<slug>/policy                       MODE_<verb>=auto|deny
~/.local/state/vmtest-bridge/<slug>/service.log             audit log, OFF the shared mount

repo checkout, untracked (the shared surface)
untracked/vmtest-bridge/{tmp,requests,processing,responses,archive,quarantine,diagnostics}/
```

### 2.2 Data flow, container request to verdict

```
  CCY container (/workspace)                 host user session
  ──────────────────────────                 ─────────────────
  agent
    │ scripts/vmtest-request.bash run-scenario server-fresh-install
    │ 1. write JSON -> untracked/vmtest-bridge/tmp/<nonce>
    │ 2. mv -f      -> requests/20260913T114500Z-run-scenario-0123456789abcdef.json
    │                                            │ PathModified + PathExistsGlob
    │                                            ▼
    │                             vmtest-bridge@<slug>.service  (Type=oneshot)
    │                               a. filename regex
    │                               b. HARDCODED DENY LIST   (first, fails closed)
    │                               c. verb allowlist
    │                               d. argument ∈ deployed scenarios.allowlist
    │                               e. policy MODE_<verb>  (missing -> deny)
    │                               f. mv requests/ -> processing/
    │                               g. WRITE responses/<req>.response.json  state=accepted
    │                               h. build_argv()  — hardcoded array, no eval, no shell -c
    │                               i. systemd-run --user --scope  vmtest run <scenario>
    │                                            │           (outlives the oneshot)
    │                                            ▼
    │                                        vmtest
    │                               freshness gate -> clone overlay -> boot -> ssh
    │                               -> run.bash headless -> guest acceptance
    │                               -> transcript -> destroy overlay
    │                               heartbeat_at refreshed throughout
    │                                            │
    │ 3. poll responses/ ◀───────── atomic mktemp+mv: state=finished, verdict=pass|fail|error
    ▼
  exit 0 only on verdict==pass
```

**The container never touches the VM.** No network path, no libvirt socket, no SSH key, no
credential in the sandbox. It writes a name and reads a document. That is the entire
interface.

### 2.3 The trust boundary, stated precisely

The sandboxed agent can write anything into the bind-mounted checkout. The bridge exists so
that this does **not** become "the agent can run arbitrary code as the host user". Two
consequences the design is built around:

- **Nothing the host executes is read from the shared mount.** The verb allowlist, the
  scenario argument enumeration, the in-guest acceptance scripts and `vmtest` itself are all
  **Ansible-deployed copies** outside the checkout. `vars/vm-test-scenarios.yml` in the repo
  is the *source*; `~/.local/share/vmtest/scenarios.allowlist` is the *authority*. Changing
  the enumeration therefore requires a human to re-run the play — which is exactly this
  repo's IaC contract, not a new rule.
- **Drift between the two is loud, never silent.** `lab-status` reports both digests and an
  `allowlist_stale` boolean. A `run-scenario` for a scenario present in the repo but absent
  from the deployed allowlist returns `verdict: error` naming "re-run
  `play-vm-test-lab.yml` on the host" — it is never treated as an unknown verb and never
  silently skipped. This is the same shape as `scripts/qa-deployed-drift.bash`, which
  already enforces repo-vs-host agreement elsewhere in this repo, and the same shape as
  00079's `acceptance.bash` refusing to vouch for a binary that differs from the repo.

**The guest is a different matter, deliberately.** The thing under test is repo code, and
the guest runs it. But the guest gets its copy from **git, over HTTPS, at a pushed
commit** — never from the mount. See §3.4.

### 2.4 Rootless or rootful libvirt

`CLAUDE/ContainerEngines.md` is podman-first and rootless-first, so the default is
`qemu:///session`:

- no root, no new root-equivalent group membership;
- per-run user-mode networking with an explicit host-port forward to the guest's `:22`, so
  concurrent runs simply take different local ports;
- the only host→guest channel the harness needs is that one SSH port.

The cost is that `qemu:///session` still needs read/write on `/dev/kvm` (Fedora ships it
`root:kvm 0660`), so the play adds the user to the `kvm` group.

**Fallback, if U2 says session-mode networking or KVM permissions do not work here:**
`qemu:///system` with the stock `default` NAT network. That is rootful and is the reason it
is the fallback rather than the default. The decision is made by the Phase-0 triage report,
not by assumption.

---

## 3. The VM lifecycle state machine

### 3.1 States

```
                  ┌──────────────────────────────────────────────────────┐
                  │ upstream fingerprint probe (§4) — 5 cheap HTTP GETs  │
                  └───────────────────────┬──────────────────────────────┘
                                          │
   ABSENT ──install──▶ BUILT ──upgrade+cleanup──▶ CURRENT ──clone──▶ RUNNING ──assert──▶ VERDICT
      ▲                                      ▲        │                 │
      │                                      │        │                 └── destroy overlay (rm)
      │                        refresh (TTL-U or ─────┘
      │                        updates revision moved)
      │
      └──── reinstall (TTL-R, or an installer-identity change, or base sha256 mismatch)
```

There is no `REVERTED` state, because there is nothing to revert. See §3.3.

### 3.2 What is stored where

| Artefact               | Path                                        | Written by                                              | Read-only during a run |
| ---------------------- | ------------------------------------------- | ------------------------------------------------------- | ---------------------- |
| Upstream download      | `images/<name>`                             | `vmtest` fetch, sha256-verified against `releases.json` | yes                    |
| Base disk              | `bases/<profile>-<ver>/base.qcow2`          | base builder only                                       | **yes — enforced**     |
| Base fingerprint       | `bases/<profile>-<ver>/base.json`           | base builder only                                       | yes                    |
| Run overlay            | `runs/<run_id>/overlay.qcow2`               | the run                                                 | no                     |
| Seed / kickstart media | `runs/<run_id>/seed.iso`                    | the run                                                 | yes                    |
| Console capture        | `runs/<run_id>/console.log`                 | libvirt serial console                                  | no                     |
| Transcript + verdict   | `untracked/vmtest-bridge/archive/<run_id>/` | the run                                                 | no                     |

`base.json` holds: `fedora_version`, `profile`, `compose_id`, the `.treeinfo` artefact
hashes, the source artefact `sha256`, `installed_at`, `last_upgraded_at`, the
`updates_repomd_revision` at last upgrade, `base_sha256` (of `base.qcow2` itself), and
`recipe_digest` (a hash of the build script + kickstart + package list, so a change to how
the base is built invalidates it exactly like an upstream change does).

### 3.3 Snapshot mechanism — external backing chain, not libvirt internal snapshots

Two candidate mechanisms:

- **libvirt internal snapshots** (`virsh snapshot-create-as`, `virsh snapshot-revert`) keep
  deltas inside one qcow2 file. Every run opens the base file **read-write**. A crash or a
  bug during revert can take the base with it, snapshot chains inside a single file get
  slow, and parallel runs against one base are not possible.
- **External backing chain**: `qemu-img create -f qcow2 -F qcow2 -b <base> overlay.qcow2`.
  The base is attached `readonly` and never written. "Revert" is `rm overlay.qcow2`.

**Decision: external backing chain.** It is strictly better for this workload —

1. the base is structurally protected, not protected by care;
2. revert is an unlink, so it is O(1) and cannot half-complete;
3. parallel `server` and `desktop` runs over their own read-only bases are trivial;
4. it is what `virt-install --import` plus `qemu-img create -b` already gives;
5. a corrupted overlay costs one run, not the base.

Internal snapshots buy nothing this design needs and add a path by which the expensive
artefact can be destroyed.

**Chain depth is always exactly 1.** On a *refresh* (a `dnf upgrade` re-snapshot) the design
does **not** stack a second overlay and promote it. It copies the base, boots the copy
read-write, upgrades, runs the in-guest cleanup, shuts down, and atomically renames the
result into place. That costs one full base-sized write per refresh and guarantees that
every run's reads traverse exactly one backing hop. The alternative — growing the chain —
makes every subsequent run slower, forever, to save a write that happens on a TTL. Stated
so the trade is visible rather than implied.

**Pre-snapshot hygiene runs inside the guest, not through libguestfs.** `virt-sysprep`
would work but pulls in a libguestfs appliance launch and its own KVM requirements, and its
default operation set removes exactly the things the desktop base must keep (the created
user, the autologin drop-in). The in-guest `guest-cleanup.bash` does the narrow, explicit
thing: truncate `/etc/machine-id`, remove SSH host keys, clear the DNF cache, remove
`/var/lib/cloud/`, zero the logs, `fstrim -av`. `guestfs-tools` is still installed, but for
a different job — `virt-cat`/`virt-copy-out` to read `/var/log/` out of a guest that
**failed to boot**, which is the difference between a diagnosable failure and a shrug.

### 3.4 What the guest provisions from

The guest clones the **public repo over HTTPS** with `RUN_BASH_GITHUB_ACCOUNTS=none` and
`RUN_BASH_GIT_REF=<40-hex commit>`. Three reasons, all load-bearing:

1. It is the real path. `run.bash:1993-2004` shows `RUN_BASH_GIT_REF` *replaces* `git pull`;
   pinning a commit means the guest cannot drift onto a newer origin tip mid-run and make
   the transcript's `repo_commit` a lie.
2. It needs no GitHub credential in the guest (§10, secrets).
3. It forces the tested code to exist in git, so the green verdict points at something a
   human can review.

**A dirty or unpushed working tree is a hard error, not a fallback.** `vmtest` refuses to
start and names the fix (`git push`). Testing an unreviewable working tree and reporting a
commit hash for it is precisely the "partial result read as a complete one" class in
`CLAUDE/AgentNotes.md`. A `--local-tree` escape hatch exists on the **host CLI only**,
behind `plan_gate_change`-style confirmation, and is recorded in the transcript; it is not a
bridge verb and not a bridge argument.

---

## 4. Freshness policy, and the installer-change question

### 4.1 The owner's question 7, answered concretely

> *Is there a way to detect that the core Fedora installer has changed since we did our
> install?*

**Yes, there are three artefact-level mechanisms, and the most important thing to say about
them is that for a released Fedora they do not change — by design.** A Fedora GA tree is
frozen at compose; there are no point releases. So for `fedora_version: 44` while 44 is the
current release, the honest answer is that the installer *will not* change, and the value of
the check is that it *proves* this rather than assuming it. The premise becomes live in two
specific windows: when this repo opens a branch against a **Branched** release (pre-GA), and
when a **new release** appears.

Each signal below was fetched live today.

#### S1 — `COMPOSE_ID` (the sharpest, and the cheapest)

```
https://dl.fedoraproject.org/pub/fedora/linux/releases/44/COMPOSE_ID      -> Fedora-44-20260422.1
https://dl.fedoraproject.org/pub/fedora/linux/development/45/COMPOSE_ID   -> Fedora-45-20260913.n.0
https://dl.fedoraproject.org/pub/fedora/linux/development/rawhide/COMPOSE_ID -> Fedora-Rawhide-20260913.n.0
```

~20 bytes, one request. The GA value is a constant. The Branched and Rawhide values carry a
date and a nightly counter and move **every day** — so during an `F45`-branch development
window, which is exactly when fresh-install acceptance testing earns its keep, this is a
true daily installer-change signal.

#### S2 — `.treeinfo` `[checksums]` (artefact-level, no inference)

`https://dl.fedoraproject.org/pub/fedora/linux/releases/44/Everything/x86_64/os/.treeinfo`,
1,427 bytes, fetched in full:

```ini
[checksums]
images/boot.iso            = sha256:bd285201494dd0ba09b54d05ac707de1401668b8512a573edb5922dcf9d7067e
images/eltorito.img        = sha256:968be6cb726599011985b8f40ba47c16ae84f1cf91ed12b0a44d2c4563d7d92e
images/install.img         = sha256:c2571f26c8d46411f8700388f7ab61d8e27356f960430dcc476325b7157ac8b0
images/pxeboot/initrd.img  = sha256:ab26d5270b8aa5df60ea86cfdff76716531c095aff235e33d911974f35216d4a
images/pxeboot/vmlinuz     = sha256:4b37e4e542a62c580c751787848be6c99e6f908f6712c8c6da85516b8d541de2
[tree]
build_timestamp = 1776865868            # 2026-04-22T13:51:08+00:00
```

`install.img` **is** the Anaconda stage-2 runtime; `initrd.img`/`vmlinuz` are the installer
kernel. Comparing the stored hash against the live one is a direct answer to "did the
installer change", with no proxy and no inference.

#### S3 — `releases.json` (the only way to learn a new release exists)

`https://fedoraproject.org/releases.json`, 135,608 bytes, 378 entries, each with `link`,
`sha256` and `size`. The entries this lab uses:

| Artefact                                          | sha256 (prefix)   | size          |
| ------------------------------------------------- | ----------------- | ------------- |
| `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`   | `28680fe5…f90b7f` | 583,729,152   |
| `Fedora-Server-Guest-Generic-44-1.7.x86_64.qcow2` | `446c01f7…dd3f0e` | 952,762,368   |
| `Fedora-Everything-netinst-x86_64-44-1.7.iso`     | `bd285201…d7067e` | 1,217,329,152 |
| `Fedora-Workstation-Live-44-1.7.x86_64.iso`       | `1620295f…426ddf` | 2,851,612,672 |

Note the netinst ISO's hash is **byte-identical to `.treeinfo`'s `images/boot.iso`** —
S2 and S3 cross-confirm each other, and that confirms the netinst ISO *is* boot.iso.
`releases.json` serves `etag: "211b8-650854cf10400"` and
`last-modified: Tue, 28 Apr 2026 13:35:12 GMT`, so a conditional GET makes the repeat cost
a 304.

#### S4 — Bodhi release state (is our target still the shipping release?)

`https://bodhi.fedoraproject.org/releases/?rows_per_page=100`, 69,107 bytes:

```
F44  state=current  branch=f44      composed_by_bodhi=true
F45  state=pending  branch=f45      composed_by_bodhi=true
F46  state=pending  branch=rawhide  create_automatic_updates=true
```

This answers the question that actually drives this repo's branch model: when F44 leaves
`current`, the lab should say so out loud rather than keep testing a release nobody is on.

#### S5 — the updates repo revision (the signal that actually moves)

```
updates/44/Everything/x86_64/repodata/repomd.xml   <revision>1789172543</revision>  = 2026-09-12T00:22:23Z
releases/44/Everything/x86_64/os/repodata/repomd.xml <revision>1776864872</revision>  = 2026-04-22T13:34:32Z
```

7,055 bytes. The GA tree's revision is frozen; the updates revision moves constantly. **This
is what TTL-U keys on.** If the updates revision has not advanced since the base's last
upgrade, a refresh is provably a no-op — and is recorded as *checked and unnecessary*, not
silently skipped.

### 4.2 Signals examined and rejected, with reasons

| Signal                                                         | Why it is not used                                                                                                                                                                                                                                                                                            |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Live respins** (`pub/alt/live-respins/`)                     | Checked: F44 respins exist for Budgie, CINN, COSMIC, LXDE, LXQT, MATE, SOAS, XFCE and i3 — and **there is no Workstation/GNOME respin**, which is the only variant this repo's desktop path uses. No `*-CHECKSUM` file matched in the listing either. Unusable here.                                          |
| **`anaconda` package version**                                 | An updated `anaconda` RPM does not change install media; media is fixed at compose. It answers a different question from the one asked.                                                                                                                                                                       |
| **mdapi** (`https://mdapi.fedoraproject.org/f44/pkg/anaconda`) | Returns `44.30-2.fc44` with `"repo": "updates-testing"` — it reports the highest NEVRA across repos **including updates-testing**, which no default install receives. The `/f44-updates/` endpoint also returned `repo: testing`. It is a convenience API, not a contract. **Do not build the policy on it.** |
| **HTTP `Last-Modified` / `ETag` on an ISO**                    | Mirror-dependent. Three consecutive HEADs redirected to three different mirrors (`fedora.mirrorservice.org`, `ask4.mm.fcix.net`, `mirror.cov.ukservers.com`). Timestamps differ per mirror; content hashes do not.                                                                                            |
| **`fedora-release` package version**                           | Same class as `anaconda`: a post-install package, not the installer.                                                                                                                                                                                                                                          |

### 4.3 The fingerprint and the policy

A pure function in `helpers/vmtest/upstream.py` builds:

```
upstream_fingerprint = sha256(
    compose_id                              # S1
  + sorted(treeinfo["checksums"].items())   # S2
  + treeinfo["tree"]["build_timestamp"]     # S2
  + sha256 of each releases.json artefact this lab uses   # S3
  + bodhi_state_for(fedora_version)         # S4
)
package_revision = updates_repomd_revision  # S5, tracked separately — NOT in the fingerprint
```

`helpers/vmtest/freshness.py` then returns exactly one of four values:

| Verdict     | Condition                                                                                                                                                   | Action                                         |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `current`   | fingerprint matches, `package_revision` unchanged **or** `now - last_upgraded_at < TTL-U`                                                                   | run against the existing base                  |
| `refresh`   | fingerprint matches, and (`package_revision` advanced and `now - last_upgraded_at >= TTL-U`)                                                                | boot rw, `dnf -y upgrade`, cleanup, re-flatten |
| `reinstall` | fingerprint differs, **or** `now - installed_at >= TTL-R`, **or** `base_sha256` mismatch, **or** `recipe_digest` changed, **or** Bodhi state left `current` | full rebuild from upstream media               |
| `unknown`   | any signal could not be fetched or parsed                                                                                                                   | **block** — see §4.4                           |

Defaults: **TTL-U = 7 days**, **TTL-R = 90 days**. Both live in `vars/vm-test-scenarios.yml`
so they are tracked, reviewable and branch-specific.

Total probe cost: 20 B + 1,427 B + 7,055 B + 135,608 B + 69,107 B ≈ **213 KB cold**, and
about **9 KB warm** once `releases.json` and Bodhi return 304. Sub-second on any normal link.
Cheap enough to run on **every** scenario, which is the point — freshness is evaluated per
run, not on a timer, so a base can never be stale-by-schedule-drift.

### 4.4 When the signal is unavailable — the fail-fast case

This is the failure mode that matters, and the repo's #1 rule decides it.

- The probe **never** falls back to "assume current" and **never** silently uses the
  existing base.
- `lab-status` prints `freshness: unknown` with the failing URL and the transport error, and
  exits non-zero.
- `run-scenario` refuses: `state: finished`, `verdict: error`,
  `failure.stage: "freshness"`,
  `failure.reason: "upstream freshness signal unavailable (<url>: <error>) — cannot prove the base is current"`,
  and `failure.operator_action` naming the override.
- The override is `vmtest run <scenario> --accept-stale-base`, **host CLI only, typed by a
  human**, recorded verbatim in `evidence.overrides` in the response and in the transcript
  header. It is not a verb, not a verb argument, and not reachable from the sandbox.

This deliberately makes a network outage block the lab rather than degrade it. A green run
against a base whose currency cannot be established is exactly the defect this repo has been
burned by repeatedly (`CLAUDE/AgentNotes.md`, "A partial result read as a complete one").
Blocking is the correct trade, and it is stated here so it is a decision rather than an
accident.

---

## 5. The desktop-profile problem

### 5.1 What a desktop run adds

The `server` profile skips every `gnome`-scoped play. In `playbook-main.yml` that is
**10 of 30 core plays**:

`play-browsers.yml`, `play-comms.yml`, `play-firefox.yml`, `play-gnome-shell.yml`,
`play-gnome-shell-extensions.yml`, `play-gsettings.yml`, `play-ms-fonts.yml`,
`play-terminal-emulators.yml`, `play-toolbox-install.yml`, `play-vscode.yml`.

So a desktop scenario is not "the server scenario with a screen" — it is a third more of the
product.

### 5.2 What actually breaks headlessly, measured not guessed

- **The user session bus.** `/run/user/<uid>/bus` is referenced directly at
  `play-gnome-shell-extensions.yml:126`, `play-systemd-user-tweaks.yml:174-175`,
  `play-podman.yml:44-45`, `play-suspend-and-lid-policy.yml:185,209`,
  `play-prevent-ssh-suspend.yml:62`, `play-rclone.yml:209-210,412-413,617-618` and
  `play-container-watch.yml:125`. With no login session, that socket does not exist.
- **The extension gate goes vacuous.** `helpers/gnome/extension_state.py:58-61` returns
  `Verdict.SKIP_NO_SESSION` when no session is available, and
  `helpers/gnome/verify_extension.py:111` then prints `EXT-OK` and exits 0. **A desktop run
  with no GNOME session would therefore report the repo's own extension verification as
  green while asserting nothing** — textbook row-13 of the `AgentNotes.md` table. This is
  the single strongest argument for driving the desktop test through a real session rather
  than over plain SSH.
- **A GUI-presence branch reads the controller's environment.**
  `play-toolbox-install.yml:31` computes `has_display` from
  `lookup('env','DISPLAY') != '' or lookup('env','WAYLAND_DISPLAY') != ''`. Under
  `transport=local` the controller is the guest, so the branch taken depends on *how the run
  was launched* — an SSH invocation with no display takes a different path from a session
  invocation.

### 5.3 How the desktop VM is built and driven

1. **Media.** `Fedora-Workstation-Live-44-1.7.x86_64.iso` (2,851,612,672 bytes, sha256
   `1620295f…426ddf` from `releases.json`) plus a new, fully non-interactive
   `fedora-install/ks-vm-desktop.cfg`, installed offline via `liveimg` from the ISO's
   `LiveOS/squashfs.img` — the same route `fedora-install/setup-netinstall-boot.bash:793-810`
   and `ks.cfg:429` already use, and the reason the base build needs the network only for a
   small `%post`.

   The existing `fedora-install/ks.cfg` is **not reusable**: `ks.cfg:13-70` switches to
   `/dev/tty6` and interactively prompts for an authorisation code, then WiFi, LUKS and user
   details. The VM kickstart is a separate file, and that is a feature — the shipped
   installer keeps its interactive safety prompts.

   Verified constraint: `releases/44/Workstation/x86_64/os/.treeinfo` returns **HTTP 404** —
   there is no Workstation install *tree*. The only two routes to a GNOME desktop are the
   Live ISO squashfs (chosen) or the `Everything` tree plus a package environment (the
   fallback, and the only reason U5 matters).

2. **GDM autologin is configured by the harness, not by the repo.** The kickstart writes
   `/etc/gdm/custom.conf` with `[daemon] AutomaticLoginEnable=True`. Verified: the repo
   configures autologin nowhere — the only GDM reference in the whole tree is
   `fedora-install/ks.cfg:691` `systemctl enable gdm.service`. **This is a deliberate,
   harness-only divergence from what a real user gets, and it is printed in the header of
   every desktop transcript**, because a difference between what was tested and what ships
   must be visible next to the verdict, not buried in a design document.

3. **The provisioning run is launched *into* the session, not beside it.** After autologin
   settles, the harness SSHes in and dispatches the work as a transient unit inside the
   logged-in session:

   ```
   systemd-run --user --wait --collect --pipe --setenv=RUN_BASH_... -- ./run.bash
   ```

   so `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR`, `WAYLAND_DISPLAY` and `XDG_SESSION_TYPE`
   are the **real session's**, not a hand-exported approximation. The plays already
   defend themselves by exporting a computed bus address, but a defensive export is not the
   same environment a user has, and `play-toolbox-install.yml:31` is the proof.

4. **Graphics.** `virtio-gpu` under KVM gives GNOME a working DRM driver;
   `llvmpipe` software rendering works but is slow enough to distort a timing-sensitive test.
   **Which exact `--video`/`--graphics` combination boots GNOME 44 cleanly here is U4** — a
   Phase-0 probe, not an assertion.

### 5.4 Asserted, evidence-only, and not assertable

|                                            | What                                                                                                                                                                                                                        | Mechanism                                                                                                                                                                |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Asserted**                               | `systemctl get-default` is `graphical.target`, and the repo's own detection resolved `provisioning_profile` to `desktop`                                                                                                    | mirrors `environment/localhost/group_vars/desktop.yml:25-26`                                                                                                             |
|                                            | A graphical session exists, is Wayland, is active and is local                                                                                                                                                              | `loginctl show-session -p Type -p Active -p Remote`                                                                                                                      |
|                                            | The session bus answers                                                                                                                                                                                                     | `busctl --user status`                                                                                                                                                   |
|                                            | GNOME Shell is running, and its major version                                                                                                                                                                               | `gnome-shell --version`                                                                                                                                                  |
|                                            | Every UUID the repo deploys reports `State: ACTIVE`, not merely *enabled*                                                                                                                                                   | `gnome-extensions info <uuid>` — the check `verify_extension.py` **cannot** make without a session                                                                       |
|                                            | Every `dconf`/`gsettings` key the repo writes reads back the expected value, read as the user through the session bus                                                                                                       | `gsettings get` / `dconf read` inside the session                                                                                                                        |
|                                            | Every package the repo names is installed; every flatpak remote and app is present                                                                                                                                          | `rpm -q`, `flatpak list`                                                                                                                                                 |
|                                            | `failed=0 unreachable=0` in the PLAY RECAP, with `ok`/`changed` recorded                                                                                                                                                    | parsed from the run transcript                                                                                                                                           |
|                                            | **Every play in `playbook-main.yml` actually ran** — recap play count vs. manifest                                                                                                                                          | catches a mis-detected profile silently skipping 10 plays; this is the coverage-substitution guard from `AgentNotes.md`                                                  |
| **Evidence only**                          | A screenshot of the guest framebuffer                                                                                                                                                                                       | `virsh screenshot` into the run directory. It proves the compositor produced a frame. It does **not** prove the frame is correct, and the transcript says so next to it. |
|                                            | Console log, journal export, `systemctl --failed`                                                                                                                                                                           | collected always, asserted only where a named unit is expected active                                                                                                    |
| **Not assertable without a human looking** | That anything *looks* right — theme, fonts, icon rendering, HiDPI scaling, colour                                                                                                                                           | a gsettings key can read back correctly while the visual result is wrong                                                                                                 |
|                                            | Extension *behaviour* — dash-to-dock intellihide actually hiding behind an overlapping window (the exact thing `play-gnome-shell-extensions.yml` records as observed by eye), workspace-switch feel, space-bar single-click | `State: ACTIVE` proves loaded, not correct                                                                                                                               |
|                                            | Anything hardware-shaped — DisplayLink, NVIDIA, IPU6 webcam, HD audio, thermals, lid and suspend behaviour                                                                                                                  | a VM has none of that hardware; these are `optional/hardware-specific/` plays and are out of scope **by construction**, which is different from being forgotten          |
|                                            | Input — keyboard layout (`caps:none`), shortcuts, press-and-hold speech-to-text                                                                                                                                             | needs a real input device and a human                                                                                                                                    |
|                                            | Anything needing credentials the VM must not hold — VPN, Last.fm, rclone remotes, GitHub SSH                                                                                                                                | see §10                                                                                                                                                                  |
|                                            | Stability over a working day                                                                                                                                                                                                | a run is minutes                                                                                                                                                         |

The design does **not** attempt image-diffing or OCR of screenshots. That would convert an
honest "not assertable" into a brittle assertion that fails on a font-rendering change, and
the resulting red would carry no information.

---

## 6. The bridge

Built on the published host-action-bridge pattern (`ltscommerce.dev/articles/host-action-bridge`).
What follows is the specialisation, not a restatement.

### 6.1 Spool

`untracked/vmtest-bridge/` inside the checkout, with `tmp/ requests/ processing/ responses/ archive/ quarantine/ diagnostics/`. `untracked/.gitignore` is `*` + `!.gitignore`, so this is
invisible to git and needs **no new mount** — `.claude/ccy/mounts` says this project declares
no extra mounts and should not, and this design keeps that true.

Request filename: `YYYYMMDDTHHMMSSZ-{verb}-{nonce}.json`, validated by
`^[0-9]{8}T[0-9]{6}Z-[a-z-]+-[0-9a-f]{16}\.json$`. Response: `{request}.response.json`.
Writes are `mktemp` on the same filesystem then `mv -f`. Malformed input goes to
`quarantine/`, never deleted.

### 6.2 The verb set — five verbs, and the reasoning for each exclusion

| Verb             | Argument        | Enumeration                        | What it does                                                                                            |
| ---------------- | --------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `list-scenarios` | none            | —                                  | emits the deployed scenario manifest: ids, profile, what each asserts, planned check count              |
| `lab-status`     | none            | —                                  | base inventory, fingerprints, freshness verdict per base, free disk, running domains, `allowlist_stale` |
| `run-scenario`   | one scenario id | **deployed** `scenarios.allowlist` | the full lifecycle for that scenario                                                                    |
| `refresh-base`   | profile         | `server` \| `desktop` \| `all`     | apply the freshness policy now                                                                          |
| `abort-run`      | none            | —                                  | terminate the single in-flight run and finalise its response as `verdict: error, stage: aborted`        |

Argument grammar is `^[a-z][a-z0-9_-]*$` **and** membership in the enumeration. The regex
alone is not the control; the enumeration is.

**Deliberately NOT verbs:**

- **`run-playbook <name>`** — the argument space is every file under `playbooks/`, each of
  which can do anything the host user can. This is `exec` wearing a domain-specific name. The
  article hardcodes `exec` and `shell:*` into an unoverridable deny list for exactly this
  shape, and the same deny list here covers `exec`, `shell`, `sh`, `bash`, `run`, `eval`,
  `system`, `ansible`, `ansible-playbook`. **Checked first, before the allowlist, fails
  closed.**
- **`ssh-to-vm` / `console`** — an interactive shell is an interactive shell. The article's
  security model forbids them outright.
- **`destroy-vm <name>`** — an unbounded name argument on a destructive verb. Teardown is a
  step inside `run-scenario` and `refresh-base`; it is never separately requestable.
- **`fetch-run-log <run-id>`** — run ids are unbounded, so it cannot be enumerated. It is
  also unnecessary: the transcript is already written into the shared spool, so the agent
  reads it as a file. A verb that adds an unbounded argument to achieve something the
  filesystem already does is pure boundary erosion.
- **`set-ttl` / `set-policy`** — a sandbox that can extend its own TTL can make a stale base
  look fresh. TTLs live in the tracked `vars/vm-test-scenarios.yml` and reach the host only
  through a playbook run a human performed.
- **`build-base --from-url <url>`** — a URL argument is a host-side network fetch chosen by
  the sandbox. The image source is derived from `vars/fedora-version.yml` plus
  `releases.json`, never from a request.
- **Any verb taking free text at all.**

### 6.3 Validation order (fails closed at every step)

```
1. filename matches the regex                     -> else quarantine/ + a quarantine response
2. HARDCODED DENY LIST                            -> else reject   (checked BEFORE the allowlist)
3. verb ∈ {list-scenarios, lab-status, run-scenario, refresh-base, abort-run}
4. argument ∈ the DEPLOYED enumeration            (never read from the shared mount)
5. policy MODE_<verb> == auto                     (missing / unknown / unreadable -> deny)
6. single-flight lock                             -> else reject with "a run is in flight"
7. mv requests/ -> processing/                    (atomic claim)
8. WRITE the accepted response                    (BEFORE dispatch — see 6.4)
9. build_argv() — a hardcoded array; arguments only ever as discrete argv elements
10. systemd-run --user --scope
```

Policy file: `~/.config/vmtest-bridge/<slug>/policy`, `MODE_<verb>=auto|deny`. Missing file,
missing key, unreadable file and unrecognised value all resolve to `deny`.

Units: `vmtest-bridge@.path` (`PathModified=` + `PathExistsGlob=` on `requests/`) and
`vmtest-bridge@.service` (`Type=oneshot`), with `StartLimitIntervalSec=60`,
`StartLimitBurst=50` and a `RuntimeMaxSec` cap on the **oneshot** — not on the scope, which
must outlive it. Audit log: `~/.local/state/vmtest-bridge/<slug>/service.log`, off the shared
mount so the agent cannot edit its own record.

The `<slug>` is `systemd-escape --path <checkout>`; `%f` in the unit expands it back. **U3**
is confirming that `%f` expands correctly inside a `.path` unit's `PathModified=`. If it does
not, the fallback is a concrete (non-template) unit pair rendered per checkout by the play
from `{{ root_dir }}` — the same result with more files.

### 6.4 The response contract — making "never ran" undeniable

This repo has a documented defect class: *a partial result read as a complete one*
(`CLAUDE/AgentNotes.md`, 18 recorded instances). A green result that cannot prove it executed
is precisely that. The contract below is designed so the naive read fails closed.

```json
{
  "schema": 1,
  "request": "20260913T114500Z-run-scenario-0123456789abcdef.json",
  "verb": "run-scenario",
  "argument": "server-fresh-install",
  "state": "accepted | running | finished | rejected",
  "verdict": null,
  "run_id": "20260913T114501Z-server-fresh-install",
  "accepted_at": "2026-09-13T11:45:01Z",
  "started_at": null,
  "heartbeat_at": "2026-09-13T11:47:20Z",
  "finished_at": null,
  "checks":   { "planned": 27, "total": null, "passed": null, "failed": null, "skipped": null },
  "evidence": {
    "transcript": "untracked/vmtest-bridge/archive/<run_id>/transcript.log",
    "transcript_sha256": null,
    "base": { "profile": "server", "name": "server-44", "base_sha256": "...", "compose_id": "Fedora-44-20260422.1",
              "installed_at": "...", "last_upgraded_at": "...", "freshness": "current" },
    "guest": { "boot_id": null, "machine_id": null, "os_release": null, "kernel": null },
    "repo":  { "commit": "7b8df3e...", "branch": "F44", "dirty": false },
    "playbook_recap": null,
    "divergences": ["gdm-autologin-enabled-by-harness"],
    "overrides": []
  },
  "failure": null
}
```

Rules that carry the weight:

01. **`verdict` is `null` until `state == "finished"`.** A consumer that reads `verdict`
    without checking `state` gets `null`, which is not truthy. The naive read fails closed.
02. **`checks.planned` is written from the manifest before the first check runs; `checks.total`
    is written at the end. `total != planned` is `verdict: error`, never `pass`.** This is the
    direct antidote to `AgentNotes.md` row 13 ("unexercised branches as passes") and to the
    coverage-substitution note ("a total cannot show you a substitution"). A harness that
    crashed after 4 of 27 checks, all passing, reports `error`.
03. **`skipped` is a first-class counter and a skipped check is never counted as passed.**
    `verdict: pass` requires `passed + skipped == total` *and* `skipped` is enumerated by name
    in the transcript, so a run that quietly stopped asserting is visible.
04. **`evidence.guest.boot_id`** is read from `/proc/sys/kernel/random/boot_id` **inside the
    guest**. A `pass` with a null boot id could not have booted anything, and the value is
    unique per boot, so a replayed or cached response is detectable.
05. **`evidence.playbook_recap`** carries the guest's own PLAY RECAP. A `pass` with `ok: 0` is
    an error by construction.
06. **`heartbeat_at`** is refreshed by the scope while running. A consumer seeing
    `state: running` with a heartbeat older than the threshold knows the host process died —
    a run never sits at `running` forever with no way to tell.
07. **Silence is never an outcome.** The `accepted` response is written *before* dispatch
    (step 8 of §6.3), and every rejection writes a `rejected` response too. "Request consumed,
    nothing came back" cannot happen; "no response file at all" means the watcher never ran,
    which is a distinct and checkable state the container-side reader reports as `unknown`.
08. **Three-valued verdict**: `pass` (the product ran and every assertion held), `fail` (the
    product ran and an assertion did not), `error` (the harness could not complete). `failure.stage`
    names where — `freshness | allowlist | base | clone | boot | ssh | provision | assert | collect | aborted`. `error` never renders as `fail`, and neither ever renders as `pass`.
09. **`evidence.divergences`** lists every known difference between the tested system and a
    real install (harness-set autologin, throwaway vault password, absent hardware). It sits
    next to the verdict, not in a document.
10. **The transcript's sha256 is recorded in both the shared response and the off-mount audit
    log.** Tampering with the shared copy is detectable by comparing them.

`scripts/vmtest-request.bash` (container side) exits `0` **only** on
`state == "finished" && verdict == "pass"`. Timeout, `unknown`, `rejected`, `fail` and
`error` are all non-zero and each prints a distinct reason.

### 6.5 Known limits, stated rather than discovered

Multi-second dispatch latency; a fixed verb set that only a host playbook run can widen; rate
limiting that will reject a burst; no human-confirmation mode (a verb is `auto` or `deny`,
never "ask"); single-flight, so a second `run-scenario` while one is in flight is rejected
rather than queued.

---

## 7. Performance plan

Costs below are **expected machine wall-clock on a modern NVMe workstation with KVM**, not
scheduling estimates and not measurements — nothing here could be measured from a container
with no `/dev/kvm`. They exist to make the optimisation choices auditable, and Phase-3 and
Phase-5 replace them with measured values recorded in the run transcripts.

| Operation                      | Mechanism                                                         | Expected                                           | Why this number                                                  |
| ------------------------------ | ----------------------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------------------- |
| Freshness probe                | 5 HTTP GETs, conditional                                          | < 1 s                                              | 213 KB cold, ~9 KB warm (measured sizes, §4.3)                   |
| Fetch Cloud Base qcow2         | `curl -z` + sha256                                                | first: minutes; after: ~0                          | 583,729,152 B; a 304 on re-check                                 |
| Fetch Workstation Live ISO     | `curl -z` + sha256                                                | first: minutes; after: ~0                          | 2,851,612,672 B                                                  |
| Build **server** base          | import qcow2 + cloud-init first boot + `dnf -y upgrade` + cleanup | a few minutes                                      | **no Anaconda at all** — the official qcow2 *is* a fresh install |
| Build **desktop** base         | `virt-install` + kickstart + `liveimg` squashfs                   | tens of minutes                                    | Anaconda plus a 2.8 GB squashfs unpack; network only for `%post` |
| Refresh a base                 | boot rw, `dnf -y upgrade`, cleanup, re-flatten                    | a few minutes                                      | dominated by the update backlog plus one base-sized write        |
| Create a run overlay           | `qemu-img create -f qcow2 -F qcow2 -b`                            | < 1 s                                              | CoW; the new file is a few hundred KB                            |
| Boot a guest                   | KVM + virtio                                                      | seconds (server), tens of seconds to GDM (desktop) |                                                                  |
| Provision, **server** profile  | `run.bash` headless, ~20 plays                                    | tens of minutes                                    | DNF-dominated                                                    |
| Provision, **desktop** profile | `run.bash` headless, ~30 plays incl. 10 gnome                     | longer than server by roughly half again           | DNF + flatpak + extension installs                               |
| Assert + collect               | in-guest acceptance + transcript pull                             | a couple of minutes                                |                                                                  |
| Destroy                        | `virsh destroy` + `rm overlay.qcow2`                              | < 2 s                                              | the base is untouched, so there is nothing to undo               |

### Optimisations, each with its justification

- **Skip Anaconda entirely for the server profile.** `Fedora-Cloud-Base-Generic-<v>.qcow2`
  is an official, signed, freshly-installed disk image, and
  `play-AA-preflight-sanity.yml:52-53` already names Cloud Base as supported. Importing it
  removes the single most expensive step from the profile that matters most to Plan 00063.
  This is the largest single win in the design.
- **qcow2 backing chain, depth 1.** §3.3. Clone cost is O(1), revert cost is an unlink, and
  concurrent runs share one read-only base.
- **Flat base, never a growing chain.** §3.3. Pays one base-sized write per refresh to keep
  every run's reads at one backing hop, rather than paying a growing read penalty forever.
- **Conditional downloads.** `curl -z` against the cached artefact, exactly as
  `fedora-install/setup-netinstall-boot.bash:658-697` already does, so a re-check is a 304.
- **Offline install for the desktop base.** `liveimg` from the ISO's squashfs means the
  Anaconda phase does not download a package set, so base build time does not scale with
  link speed or mirror mood.
- **`discard=unmap` on the virtio-blk device plus `fstrim -av` in the pre-snapshot
  cleanup.** Keeps the base sparse, which makes the base-sized write in a refresh smaller
  and the whole lab tree smaller. Gated on U1 (the backing filesystem must support discard).
- **Parallelism across profiles, bounded by RAM not by cleverness.** The two bases are
  separate read-only files, so a `server` and a `desktop` run can proceed together. The lab
  refuses to start a run that would take committed guest RAM past a configured ceiling, and
  says so, rather than letting the host swap. Within a profile, runs are single-flight so
  the transcripts stay attributable.
- **vCPU and RAM sizing per profile**, from `vars/vm-test-scenarios.yml`: server small,
  desktop larger (GNOME plus a browser plus VS Code). The DNF-dominated phases are
  I/O-and-network-bound, not CPU-bound, so over-allocating vCPUs buys little and costs the
  host's own responsiveness. Sized from the triage report, not guessed.
- **Memory ballooning (`virtio-balloon`) is enabled but not relied upon.** It lets the host
  reclaim idle guest RAM between phases. It is not load-bearing: the RAM ceiling above is
  the actual control, because a balloon that fails to inflate must not turn into a
  swap-thrashing "slow pass".
- **`cache=none` + `io=native` (or `io_uring`) on the overlay.** Avoids double-caching guest
  writes in the host page cache during the DNF-heavy phases.
- **No tmpfs/ramdisk for the run overlay.** Tempting, and rejected: a desktop provisioning
  run writes multiple GB, so a ramdisk would evict the host's own page cache and could OOM
  the workstation the owner is using. The overlay lives on disk with `discard`; that is the
  right trade for a lab that runs on someone's daily driver. Stated as a decision because
  the brief asked about it.
- **`virt-install --unattended` is not used.** It drives osinfo's generated kickstart, which
  this design does not want — the kickstart is a tracked, reviewable file in
  `fedora-install/`, because it is part of what the lab tests. `--unattended` would hide it.
  `--cloud-init` **is** used for the server profile (U6 pins the exact flags).
- **`--boot loader=...` with `edk2-ovmf` (UEFI).** Matches how the repo's own installer
  partitions (`ks.cfg:356,372` create `/boot/efi`), so the test resembles a real install
  rather than a legacy-BIOS convenience.

---

## 8. What this proves, and what it does not

Mapped onto the three plans stuck on "Blocked — HOST ACTION". **Deliberately conservative.**

### Plan 00063 — headless `run.bash` server/cloud provisioning

| Success criterion                                                                | Verdict                                                                                                                                                                                                                                                          |
| -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Provisions a headless Fedora Server or Cloud box end-to-end with zero prompts    | **Dischargeable.** `server-fresh-install` is exactly this, with a transcript.                                                                                                                                                                                    |
| Every missing required value fails fast naming the fix, never hangs              | **Already discharged elsewhere, not by this.** 00063 Task 2.8 records 10 preflight gates passing in-container via `runuser -u nobody`. The VM adds nothing here and should not claim to.                                                                         |
| A failed main or optional playbook makes a headless run exit non-zero            | **Dischargeable**, by the negative scenario `server-optional-play-missing`: set `RUN_BASH_OPTIONAL_PLAYBOOKS` to a play that does not exist and assert the non-zero exit and the named error (`run.bash:686`). Uses only supported inputs; changes no repo code. |
| No secret bytes enter the environment or cloud-init `user-data`                  | **Dischargeable, and genuinely new.** Assertable in-guest by grepping `/var/lib/cloud/instance/user-data.txt` and the process environment of the run.                                                                                                            |
| GitHub auth works non-interactively via a scoped token; SSH-only git auth        | **Only via the opt-in, human-gated `server-fresh-install-github` scenario** (§10). Not reachable from the bridge.                                                                                                                                                |
| Desktop interactive `./run.bash` is unchanged (Task 3.2)                         | **Not dischargeable.** It is an *interactive* path; no VM proves a human's prompt experience. The lab proves the desktop *profile* provisions, which is a different and also valuable thing. This criterion stays open, or the owner re-scopes it.               |
| `qa-all.bash` passes; version bumped; no new `2>/dev/null`, `\|\| true` or `sed` | unchanged by this plan                                                                                                                                                                                                                                           |

**Net: Task 3.1 becomes dischargeable; Task 3.2 does not.** Four of the eight criteria become
machine-provable by the default scenarios, one more by the opt-in scenario, one was never
VM work, and one was already discharged in-container.

### Plan 00079 — podman container control

Task 3.3b needs `deploy.bash` then `acceptance.bash` on the HOST. The honest position has
two halves:

- **The mechanical half is dischargeable.** A VM (either profile — `play-podfreeze.yml` and
  `play-claude-yolo.yml` are both `general`-scoped) can deploy and run the acceptance script.
- **The fleet-dependent half needs care, and a naive VM run would reproduce the exact defect
  00079 spent three rounds eliminating.** Checks 9/9b assert that `--ccy --dry-run` covers
  the *whole fleet*, and 13b exercises `select_identity`'s disclosure across identity axes.
  On a fresh VM there is no fleet, so those checks would `skip()` — and 00079 Task 3.3d
  explicitly made skipped branches count as skipped rather than passed, so the run would
  correctly report reduced coverage rather than a false green. Correct, but not a discharge.
- **It becomes exercisable with a synthetic fleet.** `podfreeze` selects by **label**, not by
  a container being a real Claude session (00079 Tasks 1.1 and 1.4 added `ccy=true`,
  `ccy-project`, `ccy-github`, `ccy-token`, `ccy-ssh-keys`). A scenario can start a few
  `sleep infinity` containers carrying those labels across two identity values, plus one
  legacy-named `<project>_yolo` container with no labels, and then run acceptance. No Claude
  credential is required for any of that.
- **And the distinction must be kept.** A synthetic fleet proves the tool's *logic*. It does
  not prove the host's *actual* fleet is fully labelled — which is the specific thing check 9
  exists for, because the live fleet once had an unlabelled member (`AgentNotes.md` row 3).

**Net: partially dischargeable, and only with a scenario that builds the fleet explicitly.
The live-host coverage claim is not dischargeable by any VM, and this design does not claim
it.**

### Plan 00092 — CCY child-claude spawn mode

This is the largest unlock, for a reason the plan itself states.

- Task 6.4 steps 1–4 (`triage`, `deploy`, `ccy --rebuild`, `triage` again) are ordinary host
  actions a VM can perform: `play-claude-yolo.yml` is core and `general`-scoped, so every
  scenario gets CCY; podman inside the guest needs no nesting beyond the VM itself.
- Step 6 (`acceptance.bash` with the flag **off**) needs no credential at all — I6's absent
  case is about artefacts not existing. **Dischargeable.**
- Step 5 (flag **on**) needs a token-shaped value in PID 1's environment, not a *valid* one.
  The wrapper recovers from `/proc/1/environ` and I1/I2/I3/I6/I7 are all about where a
  token-shaped value does and does not appear. A **synthetic, invalid** token exercises every
  one of them, including Task 6.7's never-yet-run `PRESENT` cases. The single criterion that
  needs a live token — "a child `claude -p` returns a real completion" — is **already marked
  done** in 00092 ("Proved before the wrapper existed, and the wrapper reproduces it").
- **The blocker the VM removes outright:** 00092 is currently gated on *rotating the owner's
  OAuth token*, because I1 would otherwise report a real leak in a 2026-09-02 host
  transcript. A throwaway VM holds a synthetic token that is worthless, so that gate
  disappears.
- **What the VM cannot do:** discharge the host's I1. The guest's `/workspace` and `/root`
  are fresh, so I1 goes green there while saying nothing whatsoever about the host's
  transcripts. The VM proves *the feature does not leak*; only the host can prove *the host
  is clean*. Those are different claims and the transcript must print both.

**Net: Task 6.4 steps 1–4 and 6 become dischargeable; step 5 becomes dischargeable for every
invariant using a synthetic token; the host-cleanliness half of I1 remains a host action.**

### The overall honest summary

This design turns "we have never proved a fresh install works" into "a fresh install is
proved on every run, for both profiles, against a base whose currency is proved too". It does
**not** turn every host-gated task into a VM task. Three things stay host-only by nature:
anything about the operator's actual machine state, anything interactive, and anything
needing a real credential.

---

## 9. Phased delivery

Ordered so something useful lands before any VM exists. Lift into `PLAN.md` with the repo's
status icons; they are omitted here to keep this document plain text.

### Phase 0 — Host triage and the decision gate

- **T0.1** Write `triage.bash` on `_planlib.inc.bash` (R1 bootstrap, `plan_mode gather`,
  `plan_require_host`, `plan_start_log auto`, report into `PLAN_RUN_DIR` per R10). Probes:
  KVM presence and `/dev/kvm` permissions; CPU virtualisation flags; free space and
  filesystem type on the lab path; whether discard/`fstrim` works there; libvirt present or
  absent; `systemd --user` linger state; `qemu:///session` networking and a host→guest port
  forward (U2); the `%f` specifier inside a scratch `.path` unit (U3); `virsh screenshot` and
  the `--video` options (U4); `virt-install --cloud-init` flag surface (U6);
  `dnf group list --hidden` for the GNOME environment id (U5).
- **T0.2** Decision gate on the triage report: KVM available, and enough headroom for two
  bases plus run overlays. Without KVM, TCG emulation makes a desktop run impractical and the
  lab must refuse rather than run something nobody will wait for.
- **T0.3** Record the answers to U1–U6 in the plan's `JOURNAL/`, and correct this document
  where reality differs.

### Phase 1 — The freshness engine (no VM needed, runs in a container)

- **T1.1** Write `tests/helpers/vmtest/test_upstream.py` first, then
  `helpers/vmtest/upstream.py`: parse `.treeinfo`, `COMPOSE_ID`, `releases.json`, the Bodhi
  releases document and `repomd.xml` into an `UpstreamFingerprint`. Pure functions over
  fixture text; no network in the unit tests.
- **T1.2** Test-first, then `helpers/vmtest/freshness.py`: the four-valued policy of §4.3,
  with a test asserting that `unknown` can never collapse into `current`.
- **T1.3** Test-first, then `helpers/vmtest/scenarios.py`: manifest parsing and the
  planned-check accounting of §6.4.
- **T1.4** `helpers/vmtest/probe_upstream.py` — thin executor, does the HTTP, prints stable
  marker lines (`VMTEST-FRESHNESS-*`), diagnostics to stderr per
  [StderrHygiene.md](../../StderrHygiene.md), `--fixture-dir` for offline tests.
- **T1.5** `vars/vm-test-scenarios.yml` — the tracked manifest: scenario ids, profile, guest
  sizing, TTL-U, TTL-R, planned check counts.
- **T1.6** `./scripts/qa-all.bash`; commit. **This phase alone answers the owner's question 7
  as a runnable command.**

### Phase 2 — The lab playbook

- **T2.1** `playbooks/imports/optional/common/play-vm-test-lab.yml`, `scope: general`,
  `root_dir` pattern, `vars_files` on `vars/fedora-version.yml`. Installs (all verified
  present on F44): `libvirt-daemon-kvm` 12.0.0-3, `libvirt-daemon-config-network` 12.0.0-3,
  `libvirt-client` 12.0.0-3, `virt-install` 5.1.0-4, `qemu-kvm` 10.2.2-1, `qemu-img` 10.2.2-1,
  `edk2-ovmf` 20260812-8, `guestfs-tools` 1.56.0-1, `cloud-utils` 0.33-13, `xorriso` 1.5.8-2,
  `osinfo-db` 20251212-1, `lorax` 44.7-1, `swtpm-tools` 0.10.2-1. (Note `libguestfs-tools-c`
  does **not** exist on F44 — mdapi returns 400.)
- **T2.2** Enable the libvirt user session, the network, `loginctl enable-linger`, and `kvm`
  group membership. Create `~/.local/share/vmtest/` with explicit `owner/group/mode` on every
  file task.
- **T2.3** Deploy `files/home/.local/bin/vmtest` `0755`, and render
  `~/.local/share/vmtest/scenarios.allowlist` from `vars/vm-test-scenarios.yml` — the
  authority of §2.3.
- **T2.4** Deploy the in-guest scripts (`guest-acceptance-*.bash`, `guest-cleanup.bash`) to
  `~/.local/share/vmtest/`, out of the shared mount.
- **T2.5** Document: a row in `docs/playbooks.md`, a new `docs/vm-acceptance-testing.md`,
  linked from `docs/README.md`.
- **T2.6** `./scripts/qa-all.bash`; commit.

### Phase 3 — Server base and the first real scenario

- **T3.1** Base builder for `server`: resolve the Cloud Base artefact name from
  `releases.json` for the `fedora_version` in the version file, download with `curl -z`,
  verify sha256 (reusing the shape of `setup-netinstall-boot.bash:699-791`, including the
  GPG-verified CHECKSUM path where the artefact has one), import, cloud-init first boot,
  `dnf -y upgrade`, `guest-cleanup.bash`, flatten, write `base.json`.
- **T3.2** `vmtest run server-fresh-install`: freshness gate, overlay, boot, SSH, headless
  `run.bash` at the pinned commit, in-guest acceptance, transcript, verdict, destroy.
- **T3.3** `guest-acceptance-server.bash` — the assertion set, with `planned` declared up
  front and `skipped` enumerated by name.
- **T3.4** The negative scenario `server-optional-play-missing`. **This is the falsifiability
  proof**: it makes the harness go red on purpose. Per `AgentNotes.md` — "break the fix on
  purpose and watch the new test go red, or you have not tested it."
- **T3.5** Plan-local `deploy.bash` (HOST, `plan_mode deploy`, `plan_gate_change`) chaining
  into `acceptance.bash`, both on `_planlib.inc.bash`.
- **T3.6** `./scripts/qa-all.bash`; commit.

### Phase 4 — The bridge

- **T4.1** Spool layout and the request/response schema, documented in
  `docs/vm-acceptance-testing.md`.
- **T4.2** `files/home/.local/bin/vmtest-bridge-watcher` with the §6.3 validation order.
- **T4.3** `vmtest-bridge@.path` / `.service` and the policy file, deployed and enabled by
  the play with the escaped-path instance name.
- **T4.4** The response state machine and heartbeat in `helpers/vmtest/verdict.py`, with the
  `accepted` stub written before dispatch.
- **T4.5** `scripts/vmtest-request.bash` — the container-side requester and reader; exits 0
  only on `finished` + `pass`.
- **T4.6** A bridge selftest proving each rejection path rejects **and** produces a response:
  bad filename, denylisted verb, unknown verb, unknown argument, `MODE=deny`, missing policy
  file, rate limit, in-flight lock. Modelled on 00092's `selftest-probes.bash`, which found
  two real defects in its own probes on first run.
- **T4.7** `./scripts/qa-all.bash`; commit.

### Phase 5 — Desktop base and desktop scenario

- **T5.1** `fedora-install/ks-vm-desktop.cfg` — fully non-interactive, `liveimg`-based, UEFI
  partitioning, GDM autologin, a clear header stating it is for VM testing only and is not
  the shipped installer.
- **T5.2** Desktop base builder: fetch and verify the Workstation Live ISO, `virt-install`
  with the kickstart, wait for the install to complete, `guest-cleanup.bash` keeping the user
  and the autologin drop-in, flatten, `base.json`.
- **T5.3** `vmtest run desktop-fresh-install`, dispatching the provisioning run **into the
  session** with `systemd-run --user --wait` per §5.3.
- **T5.4** `guest-acceptance-desktop.bash` — the §5.4 assertion set, including
  `gnome-extensions info <uuid>` `State: ACTIVE` for every deployed UUID and the
  recap-play-count coverage check.
- **T5.5** `virsh screenshot` evidence capture, with the evidence-not-assertion boundary
  written into the transcript beside it.
- **T5.6** `./scripts/qa-all.bash`; commit.

### Phase 6 — Freshness automation, retention and guards

- **T6.1** Wire the Phase-1 policy into `vmtest`: every `run-scenario` evaluates freshness
  first and refuses on `unknown`.
- **T6.2** `refresh-base` and the flat re-snapshot of §3.3.
- **T6.3** A `systemd --user` timer running a nightly freshness **probe that only reports** —
  it writes a status file and never rebuilds unattended. An unannounced disk-churning rebuild
  on the owner's workstation is a surprise; a status file is not.
- **T6.4** Disk-space floor (accounting for the 2x base-size a rebuild needs) and guest-RAM
  ceiling, both refusing loudly rather than cleaning up and continuing. Retention sweep of
  `runs/` keeping the last N **plus every failed run**.
- **T6.5** `./scripts/qa-all.bash`; commit.

### Phase 7 — Discharge and review

- **T7.1** Run the scenarios; record in this plan's `JOURNAL/` which 00063 / 00079 / 00092
  criteria went green, each against its transcript and run id.
- **T7.2** Update those three plans' `PLAN.md` files in the same commits, per the Plan Commit
  Rule.
- **T7.3** `./scripts/qa-all.bash`, then the `qa-reviewer` agent over the plan's full diff;
  resolve every BLOCK and FIX-BEFORE-MERGE finding.

---

## 10. Risks and failure modes

| Risk                                                             | How it is handled                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **A broken VM run read as a failing product**                    | Three-valued verdict with a `failure.stage`. `error` means the harness could not complete (no boot, SSH timeout, no upstream signal, stale allowlist); `fail` means the product ran and an assertion failed. `error` never renders as `fail`, neither ever renders as `pass`. And `checks.planned` vs `checks.total` catches the subtler case: a harness that died after 4 of 27 checks with all 4 green reports `error`, not `pass`.                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| **Disk-space exhaustion**                                        | A free-space floor is checked before every run **and** before every refresh, and the refresh floor accounts for the 2x base size a flat rebuild needs. Overlays are CoW so a run costs its writes, not a base. Retention keeps the last N runs plus every failure. The lab refuses loudly; it never deletes something to keep going.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| **Snapshot-chain corruption**                                    | Chain depth is 1 and the base is attached **read-only** to every run, so no run can write it. Refresh writes a new file and renames atomically. `base.json` records `base_sha256`; every run verifies it before cloning and refuses on mismatch — so silent bit-rot surfaces as a refusal, not as strange failures.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| **A stale base silently in use**                                 | Freshness is evaluated on **every run**, not on a timer. `unknown` blocks. The only override is host-CLI, human-typed, and recorded in `evidence.overrides` in the response and in the transcript header. The base fingerprint and its freshness verdict appear in every response.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| **The sandbox edits the allowlist or an assertion script**       | Neither is read from the shared mount on the host path. Ansible-deployed copies are the authority; drift is reported with both digests and refused with a named remedy, never silently used.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| **Secrets on a throwaway VM**                                    | The default scenarios use `RUN_BASH_GITHUB_ACCOUNTS=none` (no PAT, no SSH passphrase — `run.bash:877-879`) and a **per-run randomly generated vault password that decrypts nothing**: with `RUN_BASH_CONFIG_SOURCE=none`, `run.bash:487-513` writes a fresh `localhost.yml` with no vault-encrypted values, while `ansible.cfg:42` still needs a readable `vault-pass.secret`. So the guest holds exactly one secret and it is worthless. The token-bearing scenario is opt-in, host-CLI only, uses a dedicated throwaway GitHub account's short-lived PAT delivered over the SSH channel into a tmpfs file — **never** via cloud-init `user-data`, per `docs/headless-provisioning.md:127-131` — and revokes it afterwards. It is not in the bridge's argument enumeration, because a sandboxed agent asking the host to put a PAT into a VM is the precise shape the bridge exists to prevent. |
| **No KVM / nested virtualisation unavailable**                   | Phase-0 decision gate. TCG emulation is roughly an order of magnitude slower; a desktop run becomes something nobody waits for. The lab refuses rather than producing a result hours late that nobody reads.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| **The harness passes green having asserted nothing**             | Three structural defences: the negative scenario (T3.4) that must go red; the bridge selftest (T4.6) proving each rejection path rejects; and `planned` vs `total` vs `skipped` accounting in every response.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| **Fidelity divergence read as fidelity**                         | Every known divergence — harness-set GDM autologin, throwaway vault password, absent hardware, synthetic CCY fleet, synthetic OAuth token — is enumerated in `evidence.divergences` beside the verdict, so a reader sees what the green does not cover without opening a design document.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| **`run.bash` self-updating mid-run**                             | `run.bash:1993-2004`: when `RUN_BASH_GIT_REF` is set, the declared ref replaces `git pull`. Scenarios always pin a 40-hex commit, so the guest cannot drift onto a newer origin tip and make `evidence.repo.commit` a lie.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| **A request that is never answered**                             | The `accepted` response is written before dispatch; a heartbeat is refreshed while running; a terminal response is always written; rejections write responses too. The container-side reader times out into `unknown`, never into `pass`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| **Bridge rate-limit exhaustion wedging the lab**                 | `StartLimitIntervalSec=60` / `StartLimitBurst=50` on the oneshot; a rejected request still gets a `rejected` response naming the limit, so a throttled agent learns why instead of waiting on silence.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| **libvirt or the guest leaves a domain running after a failure** | `vmtest` owns teardown in a trap armed for EXIT and INT/TERM/HUP; `lab-status` lists orphan domains and overlays, and the next run refuses to start while an orphan from a different run exists rather than quietly reaping it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

---

## 11. Open decisions for the owner

1. **Rootless (`qemu:///session`) or rootful (`qemu:///system`) libvirt.** Design default is
   rootless per `CLAUDE/ContainerEngines.md`; the fallback is decided by the Phase-0 triage
   report (U2), not by preference.
2. **TTL defaults.** 7 days (update) and 90 days (rebuild) are proposed, not measured. They
   live in a tracked file and are trivially changed.
3. **Whether the token-bearing GitHub scenario is built at all.** It is the only way to
   discharge 00063's token criteria, and it is the only part of this design that puts a real
   credential in a guest. Recommended: build it, keep it off the bridge, require a dedicated
   throwaway account.
4. **Whether 00063 Task 3.2 ("desktop interactive `run.bash` is unchanged") is re-scoped.**
   It is not VM-provable as written. The lab proves the desktop *profile* provisions; it
   cannot prove an interactive prompt experience.
5. **Whether the synthetic-fleet scenario for 00079 is worth building** given that it proves
   the tool's logic but not the live host's fleet labelling.
