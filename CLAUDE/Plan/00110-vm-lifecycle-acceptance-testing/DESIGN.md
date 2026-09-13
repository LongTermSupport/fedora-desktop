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
| Cloud Base is an explicitly supported target                                 | `playbooks/imports/play-AA-preflight-sanity.yml:50-51`                                                                                                                                                                                                          |
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
| Every Fedora 44 package this design names exists                             | queried against `https://mdapi.fedoraproject.org/f44/pkg/<name>`. **Names only** — mdapi reports `updates-testing` NEVRAs (§4.2), so no version from it is quoted anywhere                                                                                      |
| `libguestfs-tools-c` no longer exists on F44                                 | mdapi returns HTTP 400; the correct package is `guestfs-tools` 1.56.0-1.fc44                                                                                                                                                                                    |

### Verified upstream, live, from this container

All five of the freshness signals in §4 were fetched and parsed today. Sizes, values and
the exact URLs are in §4. The one-line summary: **`releases/44/COMPOSE_ID` is
`Fedora-44-20260422.1` and does not move, while `updates/44/.../repomd.xml`'s `<revision>`
is `1789172543` (2026-09-12T00:22:23Z) and moves constantly.** That asymmetry is the whole
answer to the owner's question 7.

| Upstream claim                                                                         | Evidence                                                                                                                                                                                            |
| -------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The install tree has not moved for **142 days** while updates moved continuously       | compose timestamp `1776865868` (2026-04-22) vs updates revision `1789172543` (2026-09-12); delta 12,306,675 s = 142 whole days                                                                      |
| Every variant publishes a signed CHECKSUM whose **filename carries the compose label** | `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`, `Fedora-Server-44-1.7-x86_64-CHECKSUM`, `Fedora-Everything-44-1.7-x86_64-CHECKSUM`, `Fedora-Workstation-44-1.7-x86_64-CHECKSUM` — all four confirmed present |
| A ready-made Cloud qcow2 exists, removing Anaconda from the server fast path           | `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`, 583,729,152 B, sha256 `28680fe5…f90b7f`                                                                                                            |
| `download.fedoraproject.org` is a redirector and mirrors differ per request            | three consecutive HEADs resolved to `fedora.mirrorservice.org`, `ask4.mm.fcix.net` and `mirror.cov.ukservers.com`                                                                                   |
| `releases.json` supports conditional GET                                               | `etag: "211b8-650854cf10400"`, `last-modified: Tue, 28 Apr 2026 13:35:12 GMT`                                                                                                                       |

### Settled from documentation, after review — these were wrongly deferred

An earlier draft listed two of these as UNVERIFIED and pushed them to host probes. Both were
answerable from upstream documentation, and deferring a settleable question inflates the
unverified list until it stops signalling anything. Fetched and quoted:

- **The systemd `%f` specifier** (was U3). `systemd.unit(5)`: *"Unescaped filename. This is
  either the unescaped instance name (if applicable) with `/` prepended (if applicable) …
  This implements unescaping according to the rules for escaping absolute file system paths"*
  — the exact inverse of `systemd-escape --path`, and `[Path]` directives resolve specifiers.
  `PathModified=%f/untracked/vmtest-bridge/requests` is correct by construction. The Phase-0
  probe stays as **confirmation, not as the source of truth**.
- **`virt-install --cloud-init`** (was U6). The upstream manual gives the suboptions
  verbatim: `user-data=`, `meta-data=`, `network-config=`, `root-ssh-key=`,
  `root-password-file=`, `root-password-generate=on`, `disable=on`; bare `--cloud-init` maps
  to `root-password-generate=on,disable=on`; it *"generates a NoCloud ISO … attached to the VM
  as a CDROM device … only attached for the first boot"*. That last clause is why the fast
  path needs no separate seed-ISO build step.
- **The systemd rate-limit semantics** that produced §6.4's rewrite were likewise settled from
  `systemd.path(5)` rather than probed, and are quoted there.

### UNVERIFIED — Phase-0 triage items

What remains genuinely needs hardware, or a host this container is not. They are **tasks, not
assumptions**, and every one is a probe inside `triage.bash` per
[PlanTriage.md](../../PlanTriage.md).

- U1 — whether the host has KVM, how much free space the lab filesystem has, whether it
  supports `discard`/`fstrim`, and **whether it supports reflink** (`cp --reflink`), which
  §7 uses to make a base refresh O(1).
- U2 — whether `qemu:///session` (rootless libvirt) gives a workable host→guest channel on
  this host, or whether `qemu:///system` is required. §2 picks session-first and names the
  fallback.
- U4 — which `virt-install --video` / `--graphics` combination boots a GNOME 44 Wayland
  session cleanly under this host's libvirt.
- U5 — the exact comps environment id for a GNOME desktop install
  (`@^workstation-product-environment` is the expected value). The F44 comps file is
  `zstd`-compressed and this container has no `zstd` and no `zstandard` module; the upstream
  comps source at pagure returned 404 and the GitLab mirror returned 403. **Not asserted.**
  Confirmed on the host with `dnf group list --hidden`. §5 does **not** depend on it — the
  desktop base uses `liveimg` — so it matters only for the fallback route.
- **U7 — whether CCY can run inside the guest at all** (added in review, and load-bearing for
  §8's 00092 claim): can `ccy` start with no Claude credential and no config import
  (`claude-yolo:1947` mounts `/tmp/claude-config-import`), can a *synthetic* token be placed
  in PID 1's environment through ccy's own token store, and does `ccy --rebuild` succeed with
  rootless podman inside a VM. Probed on the host first, then in the guest.
- **U8 — how the LUKS root is unlocked unattended at every boot** (§5.3a). Three sub-questions,
  and the answer selects between routes rather than merely confirming one: does the
  `systemd-ask-password` prompt reliably reach a serial console **with `plymouth.enable=0`
  set** — `ks.cfg:444` ships `rhgb quiet`, and with Plymouth active the prompt goes to the
  display and never to `ttyS0`, which would silently reduce the wedge matcher to a bare
  timeout (the chosen route, and its guard); does libvirt in `qemu:///session` mode support a vTPM via
  `swtpm` here (the rejected route's blocker — `swtpm-tools` exists on F44, but its
  availability to a session-mode domain is unchecked); and does the harness's prompt-matching
  reliably distinguish a LUKS wedge from an ordinary slow boot.

Numbering is deliberately not compacted: U3 and U6 are cited elsewhere in this document's
history and in the journal, and silently reusing their numbers for different questions would
make those references wrong.

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
│   ├── freshness.py                              [new]     pure: revision-primary policy -> current|refresh|reinstall|unknown
│   ├── scenarios.py                              [new]     pure: manifest parse + planned-check accounting
│   ├── spool.py                                  [new]     symlink-safe, TOCTOU-safe spool I/O (§6.3) — O_NOFOLLOW/openat/renameat
│   ├── probe_upstream.py                         [new]     thin executor: HTTP + marker lines
│   └── verdict.py                                [new]     pure: response assembly, state transitions, HMAC signing
├── tests/helpers/vmtest/test_*.py                [new]     unittest, stdlib only, written FIRST
├── files/home/.local/bin/
│   ├── vmtest                                    [new]     the host CLI (bash, thin executor)
│   ├── vmtest-bridge-watcher                     [new]     the oneshot validator/dispatcher (drives spool.py)
│   └── vmtest-bridge-heartbeat                   [new]     timer-driven liveness writer (§6.5)
├── files/home/.config/systemd/user/
│   ├── vmtest-bridge@.path                       [new]
│   ├── vmtest-bridge@.service                    [new]
│   ├── vmtest-bridge-heartbeat@.timer            [new]     NOT path-triggered — survives a failed .path unit
│   └── vmtest-bridge-heartbeat@.service          [new]
├── files/home/.local/share/vmtest/
│   ├── guest-acceptance-server.bash              [new]     runs INSIDE the guest (both server bases)
│   ├── guest-acceptance-desktop.bash             [new]     runs INSIDE the guest
│   └── guest-cleanup.bash                        [new]     pre-snapshot hygiene, runs INSIDE the guest
├── fedora-install/ks-vm-desktop.cfg              [new]     non-interactive kickstart, VM only (btrfs + LUKS, per-run throwaway passphrase — §5.3a)
├── fedora-install/ks-vm-server.cfg               [new]     non-interactive kickstart for the server-full base
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
    │ scripts/vmtest-request.bash run-scenario server-fast-provision
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

`qemu:///session` still needs read/write on `/dev/kvm`. An earlier draft said Fedora ships
it `root:kvm 0660` and had the play add the user to the `kvm` group; Phase-0 triage measured
`root:kvm 0666`, set by systemd's own `50-udev-default.rules`, so no group membership is
needed and the running user can open it without a re-login. The play asserts that
`/dev/kvm` is openable read-write by the running user and fails loud if it is not.

**Fallback, if U2 says session-mode networking or KVM permissions do not work here:**
`qemu:///system` with the stock `default` NAT network. That is rootful and is the reason it
is the fallback rather than the default. The decision is made by the Phase-0 triage report,
not by assumption.

---

## 3. The VM lifecycle state machine

### 3.1 States

```
        ┌────────────────────────────────────────────────────────────────┐
        │ upstream probe (§4) — 5 cheap HTTP GETs, EVERY run             │
        │   artefact_identity  unreadable ─▶ unknown  (BLOCK, §4.4)      │
        │                      differs    ─▶ reinstall                   │
        │   package_revision   advanced   ─▶ refresh                     │
        │                      unreadable ─▶ TTL-U backstop, degraded=true│
        └──────────────────────────────┬─────────────────────────────────┘
                                       │
   ABSENT ──install──▶ CURRENT ──clone──▶ RUNNING ──run.bash + assert──▶ VERDICT
      ▲                  ▲   ▲              │                              │
      │                  │   │              └── destroy overlay (rm)       │
      │                  │   │                                             │
      │                  │   └───── re-flatten AFTER the run, and ONLY when │
      │                  │          play-AB's transaction changed packages  │
      │                  │          AND guest_seen_revision >= probe_seen   │
      │                  └─────────────────────────────────────────────────┘
      │                        (§4.4a — the run IS the refresh probe;
      │                         there is no separate refresh boot)
      │
      └──── reinstall: artefact_identity differs, recipe_digest changed,
            base sha256 mismatch, or TTL-R as a backstop
            (NOT Bodhi leaving `current` — that warns; §4.4)
```

There is no `REVERTED` state, because there is nothing to revert (§3.3), and no `BUILT`
state distinct from `CURRENT`, because there is no longer a refresh boot between them
(§4.4a). The same machine runs for all three bases of §3.5; only the cost of the `install`
edge differs.

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

**Exception, and it is not optional: a scenario that handles a real secret writes its
transcript and console log OFF the mount**, to `~/.local/share/vmtest/runs/<run_id>/`, and its
shared-spool response carries only the verdict plus a pointer a human can follow. This repo
has no run-log secret scrubber — `CLAUDE/Plan/.gitignore` says so explicitly and
[PlanScriptStandards.md](../../PlanScriptStandards.md) R4 is built on the same fact. Putting a
PAT-bearing run's transcript in `untracked/vmtest-bridge/archive/` would have built a
credential channel from the host into the sandbox, through the very component designed to
prevent one. `server-github-token` is host-CLI-only and never reaches the bridge, so it has no
need of the shared path at all.

`base.json` holds: `fedora_version`, `profile`, `compose_id`, the `.treeinfo` artefact
hashes, the source artefact `sha256`, the compose label, `installed_at`, `last_upgraded_at`,
`last_upgraded_revision` — the revision the **guest** saw, never the probe's (§4.4a) — with the mirror that served it and the `refresh_state` (`complete` | `incomplete`),
`base_sha256` (of `base.qcow2` itself), and `recipe_digest` (a hash of the build script +
kickstart + package list, so a change to how the base is built invalidates it exactly like an
upstream change does).

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

### 3.5 Three bases, not two — and the fast path is not a fresh install

The server profile gets **two** bases, because a Cloud Base image and an Anaconda install are
not the same system and must not be allowed to stand in for one another.

Cadence has **two independent axes** and an earlier draft collapsed them into one column,
which read as "`server-full` only runs on reinstall". It does not: like every base it is
*rebuilt* rarely and *refreshed* and *run* on the ordinary schedule.

| Base (`base.name`) | `base.kind` | Built from                                                                           | Proves                                                                                                 | **Rebuilt**                              | **Refreshed** | **Run by scenarios**                        |
| ------------------ | ----------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ---------------------------------------- | ------------- | ------------------------------------------- |
| `server-fast-44`   | `fast`      | `Fedora-Cloud-Base-Generic-<v>-<label>.qcow2` → cloud-init → `run.bash`              | **the repo's provisioning** on a Fedora Cloud box                                                      | on `reinstall` (cheap, so also casually) | §4.4a         | every run                                   |
| `server-full-44`   | `full`      | Anaconda from the **Server** tree → `run.bash`                                       | **the repo's provisioning on an Anaconda-installed Fedora Server**, a materially different starting OS | on `reinstall` only (expensive)          | §4.4a         | its own scenarios, on the ordinary schedule |
| `desktop-44`       | `full`      | Anaconda (Everything netinst) + Workstation Live squashfs via `liveimg` → `run.bash` | **the repo's own installer shape** plus desktop provisioning                                           | on `reinstall` only (expensive)          | §4.4a         | its own scenarios, on the ordinary schedule |

**The distinction, stated plainly so a fast-path pass can never be read as a fresh-install
pass.** A Cloud Base image is *not* the output of an Anaconda run: different package set,
different defaults (cloud-init present, `systemd-firstboot` semantics, no `cockpit`,
different firewall and SSH defaults), and **no partitioning or kickstart exercise at all**.
So `server-fast` proves the product — which is what Plan 00063 is actually about — and
proves nothing whatsoever about installing Fedora.

**And that distinction is bound in the data, not only in this paragraph.** An earlier draft
said `evidence.base.profile` carried it — but both server bases have `profile: server`, so the
field could not distinguish them and the separation existed purely in prose and scenario
names. A separation that only a careful reader enforces is not a separation. Three mechanisms
now carry it:

- **`base.kind`** (`fast` | `full`) and a **distinct `base.name`** per base, both in
  `base.json` and in every response's `evidence.base`.
- Each scenario in `vars/vm-test-scenarios.yml` declares the **`base:`** it requires by name.
  `vmtest` resolves that name and nothing else.
- A `server-full-provision` request whose required base is absent is
  **`verdict: error, stage: base`**, naming the missing base — **never** a silent
  substitution of `server-fast-44`, and never a run that quietly proves the cheaper claim
  while wearing the expensive one's name.

**Why `server-full` still earns its place even though the repo ships no server installer.**
The repo's only installer (`fedora-install/ks.cfg` + `setup-netinstall-boot.bash`) installs
**Workstation** via `liveimg`; there is no server kickstart to test. So `server-full` is not
testing the repo's installer — the `desktop` base is the one that does that. It is testing
that the product provisions correctly from an **Anaconda-installed Fedora Server**, which
Plan 00063's own criterion names ("a real or VM Fedora Server **or Cloud** box") and which
differs from Cloud Base in exactly the defaults that provisioning touches. Worth building;
not worth building often.

**The economics are deliberately asymmetric, and the layout follows.**

- `server-fast` is **nearly free** — its base is an import plus an upgrade. It can be
  rebuilt casually, and a `refresh` on it may simply be a rebuild, whichever the freshness
  verdict makes cheaper. (No base has a refresh boot of its own — §4.4a.)
- `desktop` is **expensive** — Anaconda plus a 2.8 GB squashfs unpack — so it must be reused
  aggressively. This is where the read-only base plus copy-on-write overlay of §3.3 earns
  its keep: every desktop run costs an overlay, never an install.
- `server-full` sits between the two and is built on the `reinstall` transition only.

Retention reflects this: a failed `desktop` base build is kept for diagnosis, while
`server-fast` is discarded and rebuilt without ceremony.

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

**This is measurable, and it is measured.** The F44 install tree's compose timestamp is
`1776865868` = 2026-04-22; the F44 updates repo revision is `1789172543` = 2026-09-12. That
is a **142-day gap in which the install tree has not moved once**, while the updates repo
moved continuously.

**The design consequence, stated so it cannot be misread: a "force a full reinstall when the
installer changes" trigger keyed to `.treeinfo` or `COMPOSE_ID` will never fire during a
release's lifetime, and that is correct behaviour, not a broken check.** It fires when
`fedora_version` changes — which `vars/fedora-version.yml:6` already states directly and for
free, so the branch's own version file is the real reinstall driver on a GA release, and the
upstream artefact check is the thing that *proves the branch and the media still agree*. The
signal is **correctly inert**, and §4.4 keeps TTL-R as a backstop precisely so an inert
signal is never the only thing standing between the lab and an indefinitely old base.

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
is the primary refresh trigger — not a clock** (§4.4). If the revision has not advanced since
the base's last upgrade, a refresh is provably a no-op and is recorded as *checked and
unnecessary*, never silently skipped. If it has advanced, the base refreshes regardless of
how recently it was last touched, which is the property a TTL cannot give.

### 4.2 Signals examined and rejected, with reasons

| Signal                                                         | Why it is not used                                                                                                                                                                                                                                                                                            |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Live respins** (`pub/alt/live-respins/`)                     | Checked: F44 respins exist for Budgie, CINN, COSMIC, LXDE, LXQT, MATE, SOAS, XFCE and i3 — and **there is no Workstation/GNOME respin**, which is the only variant this repo's desktop path uses. No `*-CHECKSUM` file matched in the listing either. Unusable here.                                          |
| **`anaconda` package version**                                 | An updated `anaconda` RPM does not change install media; media is fixed at compose. It answers a different question from the one asked.                                                                                                                                                                       |
| **mdapi** (`https://mdapi.fedoraproject.org/f44/pkg/anaconda`) | Returns `44.30-2.fc44` with `"repo": "updates-testing"` — it reports the highest NEVRA across repos **including updates-testing**, which no default install receives. The `/f44-updates/` endpoint also returned `repo: testing`. It is a convenience API, not a contract. **Do not build the policy on it.** |
| **HTTP `Last-Modified` / `ETag` on an ISO**                    | Mirror-dependent. Three consecutive HEADs redirected to three different mirrors (`fedora.mirrorservice.org`, `ask4.mm.fcix.net`, `mirror.cov.ukservers.com`). Timestamps differ per mirror; content hashes do not.                                                                                            |
| **`fedora-release` package version**                           | Same class as `anaconda`: a post-install package, not the installer.                                                                                                                                                                                                                                          |

### 4.3 Which signal is authoritative for which decision

The signals are not interchangeable and must not be pooled. They answer two different
questions, and each drives exactly one transition:

> **Artefact-identity signals drive `reinstall`. The updates-repo revision drives `refresh`.
> Never the other way round.**

| Question                                                | Signal                                                                                                                                                                                                                                         | Drives      |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------- |
| *Is the base built from the currently published media?* | artefact identity — the compose label in the artefact/CHECKSUM name (`44-1.7`), the `sha256` from `releases.json`, and `.treeinfo`'s `install.img` / `initrd.img` / `vmlinuz` hashes, each verified against the variant's signed CHECKSUM file | `reinstall` |
| *Are the packages layered on top of it current?*        | `updates/<v>/Everything/x86_64/repodata/repomd.xml` `<revision>`                                                                                                                                                                               | `refresh`   |

Per base:

**Each base names exactly ONE install tree.** An earlier draft wrote "the Server/Everything
tree" for `server-full`, which is not a tree — it is two, and they ship different installers.
Measured today:

| `.treeinfo` entry           | Server tree | Everything tree |             |
| --------------------------- | ----------- | --------------- | ----------- |
| `images/pxeboot/vmlinuz`    | `4b37e4e5…` | `4b37e4e5…`     | same        |
| `images/eltorito.img`       | `968be6cb…` | `968be6cb…`     | same        |
| `images/boot.iso`           | `ae20c06b…` | `bd285201…`     | **differs** |
| `images/install.img`        | `f0564dcf…` | `c2571f26…`     | **differs** |
| `images/pxeboot/initrd.img` | `dec8bd9b…` | `ab26d527…`     | **differs** |

`install.img` is the Anaconda stage-2 runtime, so a Server install and an Everything install
literally run different installers. Naming both would have made the identity ambiguous and a
rebuild from the other tree would have matched nothing.

| Base          | Built from — ONE tree/artefact                                                                     | Reinstall authority                                                                                                           | Refresh authority    |
| ------------- | -------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `server-fast` | `Fedora-Cloud-Base-Generic-<v>-<label>.x86_64.qcow2`                                               | artefact name + `sha256` from `releases.json`, cross-checked against the signed `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`         | updates `<revision>` |
| `server-full` | the **Server** tree, `releases/<v>/Server/x86_64/os/`                                              | that tree's `.treeinfo` `[checksums]` + `Fedora-Server-44-1.7-x86_64-CHECKSUM`                                                | updates `<revision>` |
| `desktop`     | the **Everything** tree (netinst, boots Anaconda) + the Workstation Live squashfs (payload) — §5.3 | Everything's `.treeinfo` `[checksums]` + Live ISO `sha256`, against `Fedora-Everything-…` and `Fedora-Workstation-…-CHECKSUM` | updates `<revision>` |

`desktop` deliberately hashes **both** artefacts, because §5.3 needs both and a change to
either changes what was installed. Everything (not Server) is the desktop's tree because that
is what the repo's own installer uses (`setup-netinstall-boot.bash:621`).

All four signed CHECKSUM files were confirmed present today:
`Fedora-Cloud-44-1.7-x86_64-CHECKSUM`, `Fedora-Server-44-1.7-x86_64-CHECKSUM`,
`Fedora-Everything-44-1.7-x86_64-CHECKSUM`, `Fedora-Workstation-44-1.7-x86_64-CHECKSUM`.

The compose label `44-1.7` appears in those filenames, **but the label is read from
`releases.json`'s `link` field, not from a directory listing.** An earlier draft called the
listing "the cheapest staleness check available"; it is Apache-generated HTML, not a
contract, and `releases.json` — which this design already fetches and parses — carries the
same label in a structured field. Adding an HTML scraper to `upstream.py` to re-derive a fact
already in hand would be a second, weaker source for the same thing.

The fingerprint a base records at build time is therefore per-base, not global:

```
artefact_identity = sha256( compose_label + artefact_name + artefact_sha256
                          + (treeinfo[checksums] for the Anaconda bases only)
                          + bodhi_state_for(fedora_version)
                          + recipe_digest )          # drives reinstall
package_revision  = updates_repomd_revision           # drives refresh — NOT part of the above
```

### 4.4 The policy — total over its inputs, and fail-closed per signal

**The policy must be total.** An earlier draft defined `unknown` as "**no** freshness signal
could be read at all" and claimed a partial outage "still resolves through the TTL
backstops". Trace the partial case: `artefact_identity` unreadable, `package_revision`
readable. `reinstall` requires identity to **differ** — and a value that could not be read
cannot differ — so `reinstall` never fires, nothing else matches, and the base is used as
`current` for up to TTL-R **without its media identity ever being checked**. That is
fail-open, in the section whose whole subject is failing closed.

**And the test written to catch it would have passed.** T1.2's "`unknown` can never collapse
into `current`" exercises the total-outage case, which does return `unknown`. The collapse
lived in the partial case, spelled "backstop". A check that verifies the path it was thinking
about while the load-bearing path goes unexamined is `CLAUDE/AgentNotes.md`'s documented
class, and here it appeared inside this plan's own anti-regression test. T1.2 is rewritten
accordingly (§9).

So the policy is now defined **per signal**, and every combination of readable/unreadable has
a named outcome:

| `artefact_identity` | `package_revision` | Verdict                                                                                                                              | Why                                                                                                      |
| ------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| unreadable          | *either*           | **`unknown` → block**                                                                                                                | media identity is unchecked; a base whose provenance cannot be established is not certifiable at any age |
| differs             | *either*           | `reinstall`                                                                                                                          | the media changed                                                                                        |
| matches             | advanced           | `refresh`                                                                                                                            | new packages exist                                                                                       |
| matches             | unchanged          | `current`                                                                                                                            | nothing to do                                                                                            |
| matches             | unreadable         | `refresh` if `now - last_upgraded_at >= TTL-U`, else `current` — **both carry `freshness.degraded: true`** and a `divergences` entry | the TTL backstop applies *only* here; the resulting refresh has no `probe_seen` to compare against, so see the rule below |

`reinstall` additionally fires on `recipe_digest` changed or `base_sha256` mismatch (neither
needs the network), and on `now - installed_at >= TTL-R` as the backstop.

**A TTL-backstopped refresh cannot be judged complete, and is never judged incomplete.**
§4.4a's completeness test compares the guest-seen revision against the probe-seen one — but
this is the branch where there *is* no probe value. So such a refresh records the
**guest-seen** revision as `last_upgraded_revision`, sets `refresh_state: degraded`, and
carries `freshness.degraded: true` through to the response. It is never marked `complete`
(nothing established that the guest's mirror was current) and never `incomplete` (nothing
established that it was not) — inventing either would be a verdict the data does not support.

**Unreadable identity is not a realistic partial outage anyway**, which is why blocking on it
costs nothing: `COMPOSE_ID`, `.treeinfo` and `repomd.xml` all come from
`dl.fedoraproject.org`. An outage that takes one takes the others. The case worth designing
for is the reverse — and that one has the TTL backstop, with `degraded` stamped on the record
so a human reading a green verdict can see the check was weakened.

**Bodhi leaving `current` warns; it does not rebuild.** An earlier draft made it a `reinstall`
trigger. Nothing upstream changes when F44 becomes `archived` — the media is byte-identical,
so the rebuild would burn a full base build to produce the same bytes. It now raises a loud
`lab-status` warning and a `divergences` entry (`release-not-current`) saying the lab is
testing a release nobody is on, which is the actual information, and leaves the decision to a
human.

**The revision is the trigger; the TTL is a backstop.** An earlier draft had them the other
way round — refresh only when the revision advanced *and* TTL-U had expired — which is wrong
in both directions: it leaves a stale base in service inside an unexpired window after a large
update lands, and it schedules pointless `dnf upgrade` cycles when nothing upstream moved.

Defaults: **TTL-U = 7 days**, **TTL-R = 90 days**, both in `vars/vm-test-scenarios.yml` so
they are tracked, reviewable and branch-specific. Neither is a primary trigger.

### 4.4a The refresh has no boot of its own — the acceptance run is the probe

An earlier draft made a refresh "two steps with a gate between them": boot the base
read-write, `dnf -y upgrade`, re-flatten only if packages changed. That was redundant, and
the redundancy is visible in the repo. `playbooks/imports/play-AB-dnf-upgrade.yml:72-77` is
`scope: general` — play 2 of `playbook-main.yml`, running on **both** profiles in **every**
run — and it already does the work:

```yaml
    - name: Upgrade all packages to latest available
      ansible.builtin.dnf:
        name: "*"
        state: latest
        update_only: false
      register: dnf_upgrade
```

The transaction result is **already captured**, today, by the product under test. So the
design stops paying for a second boot to learn something the run is about to compute anyway:

1. Every scenario run performs the upgrade on its **overlay**, as it always did.
2. The guest acceptance script reports play-AB's changed-package count **and the revision the
   guest actually saw** into `evidence`.
3. `vmtest` schedules the base re-flatten **after** the run — or lazily before the next one —
   and only when that count was non-zero.

Zero extra boots, and the affordability objection to revision-primary disappears by
construction rather than by damping: as written before, the first run per base after each
near-daily regeneration paid a whole extra boot-and-DNF cycle before it started, across three
bases.

**B7 — and this is why step 2 says *the revision the guest actually saw*.** The probe reads
`dl.fedoraproject.org`. The guest's `dnf` resolves through the metalink to somewhere else
entirely. Measured today against
`mirrors.fedoraproject.org/metalink?repo=updates-released-f44&arch=x86_64`: **45 distinct
mirror hosts offered, `dl.fedoraproject.org` not among them, and the pool advertising three
different repodata timestamps — `1789001523`, `1789088208`, `1789174328`, spanning about two
days.** So a guest can be handed a mirror two days behind the host the probe read. Its
transaction then changes nothing, and recording the **probe's** revision as `last_upgraded`
would mark a base current at a revision it never received — stale, and provably marked fresh,
until the revision advances again.

The defence is to compare probe-seen against **guest-seen**, which is the one comparison that
can detect this:

- `base.json` records `last_upgraded_revision` as the revision the **guest's own cached
  `repomd.xml`** reported, never the probe's.
- `guest_seen >= probe_seen` → the refresh is **complete**. A zero-package transaction here is
  genuinely *checked and unnecessary*.
- `guest_seen < probe_seen` → the refresh is **incomplete**, not unnecessary. It is recorded
  as such, the base is not marked current, and it is re-run.
- The guest's mirror is recorded next to the revision, so the record says which mirror the
  claim rests on.

This also makes §7's pinned `baseurl` a pure performance knob rather than a correctness
dependency: pin whichever mirror is fast, because a mirror that has not caught up produces
`incomplete` and a retry, not a false pass. Mandating the canonical host for the transaction
would work too, but it would hand every package byte to Fedora's origin server and throw away
the DNF-cache win for no additional safety.

**Correction to the mirror paragraph this replaces.** It said a backwards *probe* revision was
mirror lag and should be retried. The probe does not use the `download.` redirector — it reads
`dl.fedoraproject.org` directly (§4.1) — so a backwards probe revision means the **canonical
host** went backwards, which is `unknown` territory and blocks. The redirector-variance
observation was true (three HEADs, three mirrors) but it was about artefact *downloads*, and
it was attached to the wrong mechanism.

Total probe cost: 20 B + 1,427 B + 7,055 B + 135,608 B + 69,107 B ≈ **213 KB cold**, and
about **9 KB warm** once `releases.json` and Bodhi return 304. Sub-second on any normal link.
Cheap enough to run on **every** scenario, which is the point — freshness is evaluated per
run, not on a timer, so a base can never be stale-by-schedule-drift.

### 4.5 When media identity cannot be established — the fail-fast case

This is the failure mode that matters, and the repo's #1 rule decides it. It is reached only
when `artefact_identity` cannot be read (§4.4) — including, but not limited to, a total
outage. A readable identity with an unreadable revision does **not** reach here: it resolves
through TTL-U and is stamped `degraded`.

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

1. **Media — TWO artefacts, because the Live ISO cannot boot an installer.** An earlier draft
   of this design said the desktop base was built from the Workstation Live ISO plus a
   kickstart. **That was wrong and would not have booted.** `liveimg` is an *Anaconda*
   directive, and the Workstation Live ISO boots a live GNOME session, not a
   kickstart-driven installer. Something else has to boot Anaconda.

   The repo's own prior art says so in its header, and the design had cited the wrong part of
   it. `fedora-install/setup-netinstall-boot.bash:5-13`:

   > *"downloads the Fedora netinstall ISO and Workstation Live ISO, extracts squashfs.img
   > from the Live ISO … extracts vmlinuz + initrd to /boot from the netinstall ISO …
   > **Anaconda boots from the netinstall ISO** (`inst.stage2=hd:LABEL=FDINST:/netinstall.iso`)
   > **and deploys the Workstation Live filesystem via liveimg** (squashfs.img)."*

   So the desktop base needs **both**:

   | Artefact                                                            | Size            | Role                                                                                               |
   | ------------------------------------------------------------------- | --------------- | -------------------------------------------------------------------------------------------------- |
   | `Fedora-Everything-netinst-x86_64-44-1.7.iso` (= `images/boot.iso`) | 1,217,329,152 B | **boots Anaconda** — supplies `images/pxeboot/vmlinuz`, `initrd.img` and the `install.img` stage 2 |
   | `Fedora-Workstation-Live-44-1.7.x86_64.iso`                         | 2,851,612,672 B | **payload only** — `LiveOS/squashfs.img` is extracted and handed to `liveimg`                      |

   Concretely, under `virt-install`: attach the netinst ISO as the boot `--cdrom` (or use
   `--location <netinst.iso>` so libvirt extracts the kernel/initrd itself) and attach a
   **second, harness-built ISO** carrying `squashfs.img` plus the kickstart as an additional
   `--disk device=cdrom`. The kickstart then mounts that second device in `%pre` and points
   `liveimg --url=file:///…/squashfs.img` at it — structurally the same trick as
   `ks.cfg:429` pointing at the `%pre`-mounted `FDINST` partition, with a cdrom in place of a
   disk partition. Serving `squashfs.img` over HTTP from the host to a `liveimg --url=http://…`
   is the alternative; it avoids building the second ISO but adds a host service to the
   critical path. **Which of the two is used is a Phase-5 decision, made after U4;** the
   design fixes the requirement (Anaconda boots from netinst, squashfs reaches it somehow),
   not the mechanism.

   The existing `fedora-install/ks.cfg` is **not reusable as-is**: `ks.cfg:13-70` switches to
   `/dev/tty6` and interactively prompts for an authorisation code, then WiFi, LUKS and user
   details. The VM kickstart is a separate file, and that is a feature — the shipped installer
   keeps its interactive safety prompts.

#### 5.3a LUKS — the blocker is every boot, not the install

An earlier draft said the LUKS layout had "a passphrase prompt with nowhere to go in an
unattended VM" and dropped encryption from the VM kickstart. **That located the problem at
the wrong point in the lifecycle, and the fix that followed from it was therefore wrong too.**

The passphrase *is* supplied to Anaconda. Both partition branches pass it inline
(`ks.cfg:359-360` for the FDINST-present case, `:377-378` for the first-install case):

```
part btrfs.01 --fstype=btrfs --size=1 --grow --encrypted --luks-version=luks2 --passphrase="…"
```

So **the install is unattended-capable and there is no install-time prompt to answer.** The
blocker is **every boot afterwards**: a LUKS2-encrypted root must be unlocked before the root
filesystem exists, so the VM comes up at a `cryptsetup` prompt on the console and stays
there — *before* userspace, so none of the in-guest transcript machinery is running to say
why. A naive harness would record a bare timeout with no cause, which is the same
uninformative-failure class §6.5 exists to prevent for the bridge. And unlike that one, this
is **guaranteed on the first desktop run**, not a rare edge.

Three ways to clear it:

|     | Approach                                                      | Fidelity cost                                                        | Why not chosen / chosen                                                                                                                                                                                |
| --- | ------------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | **Drive the passphrase over the VM's serial console at boot** | lowest — the **partition stanza** stays identical to the shipped one | **Chosen.** The desktop base is the only profile that tests the repo's real installer, so divergence there costs exactly the thing the path exists to prove. Requires `plymouth.enable=0` — see below. |
| 2   | vTPM + `systemd-cryptenroll`/clevis                           | changes post-install state; needs a vTPM in the domain               | Closest to how an encrypted desktop *should* behave, but it adds an unverified dependency (**U8**) and alters the installed system after the install being tested. Not chosen.                         |
| 3   | Enrol a keyfile into the LUKS header post-install             | highest — furthest from what ships                                   | Kept as the **fallback** if U8 shows console prompting is not reliably drivable. Simplest mechanically.                                                                                                |

**The passphrase is generated per run and is a throwaway.** It is never the estate's, never
`KS_LUKS1`'s value, and its value appears in no tracked file — including this one. It is
generated into the run directory, used by the harness, and discarded with the overlay.

**Plymouth blocks route 1 unless it is disabled.** `ks.cfg:444` sets
`bootloader --append="rhgb quiet"`, so Plymouth runs in the initramfs and takes over the
password agent: the prompt is drawn on the virtio-gpu display and
`systemd-ask-password-console` never writes to `ttyS0`. Route 1 therefore requires
**`plymouth.enable=0`** on the VM kernel command line, recorded in `evidence.divergences` as
`plymouth-disabled`.

**And the second-order effect is the dangerous one.** Without that flag the prompt never
reaches the serial console, so the matcher below sees nothing and the run degrades into
precisely the bare timeout this subsection exists to forbid — the guard silently becoming a
no-op, which is this repo's documented defect class. The matcher's own coverage is therefore
part of **U8 sub-question 1**, not an assumption.

**The harness must name this failure, not time out into silence.** The serial console is
captured from the first instant of boot (`console=ttyS0` plus `plymouth.enable=0` on the
kernel command line). If the guest does not reach userspace, the collected console is matched
against the known `cryptsetup`/`systemd-ask-password` prompt, and the verdict is
`failure.stage: boot`, `failure.reason: "wedged at the LUKS passphrase prompt"` with the
console excerpt inline — never a generic `boot timeout`. A condition we know about in advance
and still report as "timed out" would be a self-inflicted diagnostic blind spot.

**Consequence for the discharge map, stated rather than buried:** whichever option is used,
the desktop base is **not a byte-exact reproduction of a shipped install**. Option 1 is the
closest available — the kickstart is unchanged and only the *unlock* is automated — but the
unlock path differs from a human typing a passphrase, and that goes in
`evidence.divergences` as `luks-unlock-automated` alongside `gdm-autologin-enabled-by-harness`.

Verified constraint: `releases/44/Workstation/x86_64/os/.treeinfo` returns **HTTP 404** —
there is no Workstation install *tree*, which is precisely why the netinst tree has to
supply the installer and the Live ISO can only supply the payload. The fallback route is
the `Everything` tree plus a package environment (the only reason U5 matters).

2. **GDM autologin is configured by the harness, not by the repo.** The kickstart writes
   `/etc/gdm/custom.conf` with `[daemon] AutomaticLoginEnable=True`. Verified: the repo
   configures autologin nowhere — the only GDM reference in the whole tree is
   `fedora-install/ks.cfg:691` `systemctl enable gdm.service`. **This is a deliberate,
   harness-only divergence from what a real user gets, and it is printed in the header of
   every desktop transcript**, because a difference between what was tested and what ships
   must be visible next to the verdict, not buried in a design document.

3. **The provisioning run is launched *into* the session — and `systemd-run --user` is not
   quite that.** An earlier draft claimed `systemd-run --user` gives "the real session's"
   environment. **It does not, and the difference matters here.** A transient user unit runs
   under the **user manager**, which is per-user, not per-session. It holds
   `DBUS_SESSION_BUS_ADDRESS` and `XDG_RUNTIME_DIR` (per-user), and holds
   `WAYLAND_DISPLAY`/`DISPLAY` only because gnome-session imports them into the manager. But
   `XDG_SESSION_TYPE`, `XDG_SESSION_ID` and `XDG_SEAT` are **logind per-session** values, and
   a transient user unit belongs to *no* logind session. Any play reading those — and §5.4's
   own `loginctl show-session` assertion — would see something a logged-in user does not.

   So the primary mechanism is the one that genuinely is the session: a **GNOME autostart
   entry**, `~/.config/autostart/vmtest-provision.desktop`, so the run is launched by
   `gnome-session` itself and inherits the full session environment by construction, with its
   output redirected to a file the harness collects over SSH. `systemd-run --user --wait --collect --pipe` is the fallback when autostart proves awkward to sequence.

   **A Phase-5 probe pins the difference rather than assuming it away** (T5.3b): run
   `env` under both mechanisms and under a real interactive session inside the autologin
   guest, diff the three, pin the expected set, and list every residual variable in
   `evidence.divergences`. The plays already defend themselves by exporting a computed bus
   address, but a defensive export is not the environment a user has, and
   `play-toolbox-install.yml:31` is the proof that the difference reaches a real branch.

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
`quarantine/`, never *silently* deleted — but quarantine is under the same retention sweep as `runs/` (§9 T6.4), because an unbounded directory on the shared mount that the sandbox can write to is a trivial disk-fill. Entries age out on a floor-and-count policy and every eviction is recorded in the off-mount audit log.

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

### 6.3 The spool is sandbox-controlled — every host write must assume it is hostile

An earlier draft of this design treated the spool as a mailbox. **It is a directory tree the
sandbox can restructure at will**, and the host writes into it as the host user. That makes
three defences mandatory rather than optional, and none of them is expressible safely in the
`mktemp` + `mv -f` bash idiom the published article uses.

**D1 — directory symlink substitution is the serious one.** The sandbox can
`rm -rf responses && ln -s ~/.config responses`, or point `archive/<run_id>/` at `~/.ssh`,
`~/.bashrc`, or `~/.config/vmtest-bridge/` (the policy directory). The watcher's `mv -f` then
lands the file *inside the link target*, as the host user. `mv -f` protects the final
component — `rename()` replaces a symlink rather than following it — but it does nothing
about a symlinked **parent**, and a transcript written with `>` follows symlinks and truncates.
That is an arbitrary-file-overwrite primitive on the host account: the exact boundary the
bridge exists to hold.

**Ownership checks cannot substitute.** CCY binds the checkout with a plain
`-v "$PWD:/workspace"` (`claude-yolo:1946`) under rootless podman, so container root maps to
the host user's uid. Everything in the spool is already owned by the host user. That mapping
is what makes the bridge convenient, and it is what removes ownership as a control.

**Reaching the spool root is itself the first attack surface, and `O_NOFOLLOW` alone does not
cover it.** `O_NOFOLLOW` constrains only the **final** component of an open, so a single open
of `<checkout>/untracked/vmtest-bridge` still traverses `untracked/` — which the sandbox owns
and can replace with a symlink. The root must be reached by a **component-wise walk**:

```
fd = open(checkout, O_PATH|O_DIRECTORY|O_NOFOLLOW)
for component in ("untracked", "vmtest-bridge"):
    fd = openat(fd, component, O_PATH|O_DIRECTORY|O_NOFOLLOW)   # refuse on ELOOP *or* ENOTDIR
    assert S_ISDIR(fstat(fd).st_mode)                           # and confirm, do not infer
```

**The errno here is not the obvious one, and a test expecting `ELOOP` would pass on a
symlink.** `O_PATH|O_NOFOLLOW` on a symlink **succeeds** — that combination is precisely how
one obtains a descriptor to the link itself — so `O_NOFOLLOW` does not raise. What rejects it
is `O_DIRECTORY`, because the fd refers to a symlink rather than a directory, and it fails
with **`ENOTDIR`**. Refuse on **either** errno, and `fstat` for `S_ISDIR` rather than
inferring success from the absence of an error.

That is `openat2(RESOLVE_NO_SYMLINKS)` semantics built from `os.open(..., dir_fd=…)`, which
Python's standard library provides. `openat2` has **no stdlib binding**, and `helpers/` is
stdlib-only by an in-stone rule (`helpers/CLAUDE.md`), so the walk is written out rather than
reaching for a third-party wrapper or a `ctypes` syscall. Every subsequent operation uses the
resulting pinned root fd and never a path string.

The defence, per directory, at every use — **all seven, none omitted**:

| Directory           | Host action                          | Defence                                                                                                                                                                                                                                                                                                                                     |
| ------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `requests/`         | scan, read, claim                    | `openat` from the pinned root fd, refusing on `ELOOP` **or `ENOTDIR`** and confirming `S_ISDIR`. Each request file is opened `O_NOFOLLOW`, then `fstat`-checked for **`S_ISREG`** and against a **size cap** before any read — a FIFO would block the watcher indefinitely and an oversized file would exhaust it, and neither is a request |
| `processing/`       | `renameat` target                    | `renameat` relative to the pinned fds, never a path string                                                                                                                                                                                                                                                                                  |
| `responses/`        | create                               | `O_CREAT\|O_EXCL\|O_NOFOLLOW` relative to the pinned fd, then `renameat`                                                                                                                                                                                                                                                                    |
| `archive/<run_id>/` | create dir + write transcript        | `mkdirat` relative to the pinned fd, refuse if the run-id dir exists; the **run scope holds the resulting fd open for its whole life** — see below                                                                                                                                                                                          |
| **`diagnostics/`**  | **heartbeat writes, every interval** | same pinned-fd discipline. **Omitted from an earlier draft's table, and it was the worst omission**: §6.5's liveness writer is a *timer-driven, repeated* write to a fixed path, which makes it the most attractive symlink target in the spool and the one a naive implementation would hit soonest                                        |
| `quarantine/`       | move malformed input                 | `renameat` relative to the pinned fd                                                                                                                                                                                                                                                                                                        |
| `tmp/`              | **never opened by the host at all**  | the sandbox's staging area; the host's only interaction is that requests arrive *from* it by the sandbox's own `mv`                                                                                                                                                                                                                         |

**The long-lived writers pin too, not just the oneshot.** The watcher is short-lived, but the
`systemd-run --user --scope` run and the heartbeat timer are not: they write into a
sandbox-writable tree repeatedly, over minutes, which is exactly the window a
check-then-write gap is exploitable in. Both resolve their directory **once**, at start, via
the component-wise walk, and hold the fd for their lifetime — every later write is
`openat`/`renameat` against that fd. A re-resolution mid-run would reopen the hole the
pinning closes.

**A symlinked spool directory is a refusal, not a quarantine.** Quarantining implies
recoverable malformed input; a symlinked `responses/` or `diagnostics/` is an attempt on the
host account. The watcher logs it to the **off-mount** audit log, writes nothing into the
spool, and exits non-zero so the condition is visible in `systemctl --user status`.

**D2 — the request body is read exactly once.** The earlier ordering (validate → `mv` →
build argv) left `processing/` on the shared mount and never said where argv came from, so
"re-read the claimed file" was a legal implementation and the sandbox could swap the argument
between validation and dispatch. Now: **one `read()` into memory; every check runs against
that buffer; argv is built from that buffer; the file is never re-opened.** Additionally the
**verb in the filename must equal the verb in the body**, or the request is rejected — two
independent encodings of the same fact, so disagreement is itself a signal.

**D3 — this is a Python helper, not bash.** `O_NOFOLLOW`, `openat`/`renameat` against a
pinned directory fd, and read-once JSON parsing are not safely expressible in a bash watcher,
and parsing JSON in bash would need `jq` — which was not in the package list and now does not
need to be. `helpers/vmtest/spool.py` is TDD'd and stdlib-only, exactly as
`playbooks/CLAUDE.md`'s "Complex Logic → TDD Helper" requires. The bash watcher becomes the
thin executor the repo's rules already prescribe.

### 6.4 Validation order (fails closed at every step)

```
 1. pin the spool root dirfd; realpath+O_NOFOLLOW every spool dir  -> else REFUSE (off-mount log, exit non-zero)
 2. filename matches the regex                                     -> else quarantine/ + a quarantine response
 3. read the body ONCE into memory                                 -> all later steps use this buffer only
 4. HARDCODED DENY LIST                                            -> else reject  (BEFORE the allowlist)
 5. filename verb == body verb                                     -> else reject
 6. verb ∈ {list-scenarios, lab-status, run-scenario, refresh-base, abort-run}
 7. argument ∈ the DEPLOYED enumeration      (never read from the shared mount)
 8. watcher-side rate limit, from the off-mount audit log          -> else reject "rate limited", exit 0
 9. policy MODE_<verb> == auto               (missing / unknown / unreadable -> deny)
10. single-flight lock                                             -> else reject "a run is in flight"
11. renameat requests/ -> processing/                              (atomic claim, pinned fd)
12. WRITE the accepted response, HMAC-signed                       (BEFORE dispatch — see 6.6)
13. build_argv() from the step-3 buffer — hardcoded array, discrete argv elements
14. systemd-run --user --scope
```

Policy file: `~/.config/vmtest-bridge/<slug>/policy`, `MODE_<verb>=auto|deny`. Missing file,
missing key, unreadable file and unrecognised value all resolve to `deny`.
`MODE_refresh-base` ships as **`deny`** by default: §9's T6.3 argues that an unannounced
disk-churning rebuild on the owner's workstation is a surprise, and a bridge verb that
triggers one on request contradicts that. The human enables it per host if they want it.

**Rate limiting lives in the watcher, because in systemd it is a kill switch, not a
throttle.** `systemd.path(5)` states it outright:

> *"Unlike other service failures, the error condition that the start rate limit is hit is
> propagated from the service unit to the path unit and causes the path unit to fail as well,
> thus ending the loop."*

and for the path unit's own limit:

> *"If the limit is hit, the unit is placed into a failure mode, and will not watch the paths
> anymore until restarted."* (`TriggerLimitIntervalSec=` defaults to 2 s,
> `TriggerLimitBurst=` to 200.)

So the earlier draft's `StartLimitIntervalSec=60` / `StartLimitBurst=50` did not throttle a
noisy agent — **it would have converted throttling into permanent, silent death of the
bridge**, with no oneshot left running to write the "rejected, rate limited" response the
design promised, and no remedy short of a human typing `systemctl --user reset-failed`. From
the sandbox that is indistinguishable from "the bridge was never installed". This design
inherited the flaw from the published article's pattern; it is corrected here, and the
correction is the reason §6.5 exists at all.

Therefore:

- the **real** limit is step 8, counted by the watcher from the off-mount audit log, which
  writes a `rejected` response and **exits 0** so neither unit ever enters `failed`;
- **`StartLimitIntervalSec=0`**, which disables start-rate limiting on the service. Leaving
  `StartLimitBurst` merely "high" was not enough either: a sandbox `touch` loop re-triggers at
  machine speed, so any finite burst is reached, and reaching it fails the service *and* the
  path unit (the quote above). Disabling the interval removes the kill switch that the loop
  would otherwise aim at;
- **`TriggerLimitBurst=0`**, which disables path-unit trigger limiting outright. Setting it
  "high" is not enough and an earlier draft was wrong to say so: the trigger limit is
  *"enforced before the service activation is enqueued"*, so a sandbox `touch` loop in
  `requests/` trips it **without the watcher ever running**, and no amount of watcher-side
  limiting can cover a path the watcher is never on. The busy-loop protection it would have
  given is provided instead by the watcher **draining every pending request in one
  activation** rather than one per trigger — so a burst of N requests costs one activation,
  not N — plus a short **debounce sleep at the end of each activation**, so a loop that keeps
  writing during the drain is absorbed by the next pass rather than causing an immediate
  re-trigger;
- reaching the remaining limit means something is badly wrong, and §6.5 makes that visible
  instead of silent;
- `RuntimeMaxSec` caps the **oneshot**, never the `systemd-run --user --scope`, which must
  outlive it.

Audit log: `~/.local/state/vmtest-bridge/<slug>/service.log`, off the shared mount so the
agent can neither edit its own record nor forge the rate-limit history that step 8 reads.

The `<slug>` is `systemd-escape --path <checkout>`, and `%f` in the unit expands it back.
This is **settled from documentation, not deferred to a probe**: `systemd.unit(5)` defines
`%f` as *"the unescaped instance name (if applicable) with `/` prepended … This implements
unescaping according to the rules for escaping absolute file system paths"* — the exact
inverse of `systemd-escape --path`, and `[Path]` directives resolve specifiers. The Phase-0
probe remains as confirmation, not as the source of truth. If it ever disagreed, the fallback
is a concrete (non-template) unit pair rendered per checkout from `{{ root_dir }}`.

### 6.5 Liveness — telling "busy" from "the bridge is dead"

A path unit that has failed is silent in exactly the way a path unit that was never installed
is silent, and the sandbox cannot tell them apart from inside. So liveness is not left
implicit:

- A **separate `systemd --user` timer** — deliberately *not* path-triggered, so it cannot be
  taken down by the same failure — writes
  `untracked/vmtest-bridge/diagnostics/bridge-heartbeat.json` on a fixed interval, carrying
  the wall clock, the `ActiveState`/`Result` of both `vmtest-bridge@<slug>.path` and
  `.service`, the current single-flight state, and the remedy command as a literal string.
- `scripts/vmtest-request.bash` **reads the heartbeat before it writes a request**. A stale
  heartbeat means the bridge daemon is not running; a fresh heartbeat reporting
  `path_unit: failed` means the bridge is wedged and prints the exact
  `systemctl --user reset-failed …` line to hand to a human. Neither is ever reported as a
  timeout, and neither is ever reported as a `fail`.
- **The heartbeat reports; it does not repair.** Auto-resetting a failed unit would restore
  service while hiding that the limit was hit, which is the "skip and warn" pattern
  `CLAUDE.md` bans. With step 8 doing the real limiting, reaching a systemd limit is a genuine
  defect and should stop being invisible, not stop being true.

### 6.6 The response contract — making "never ran" undeniable

This repo has a documented defect class: *a partial result read as a complete one*
(`CLAUDE/AgentNotes.md`, 20 recorded instances: rows 0, 0b, 1-9, 9b, 10-17). A green result that cannot prove it executed
is precisely that. The contract below is designed so the naive read fails closed.

```json
{
  "schema": 1,
  "request": "20260913T114500Z-run-scenario-0123456789abcdef.json",
  "verb": "run-scenario",
  "argument": "server-fast-provision",
  "state": "accepted | running | finished | rejected",
  "verdict": null,
  "run_id": "20260913T114501Z-server-fast-provision",
  "accepted_at": "2026-09-13T11:45:01Z",
  "started_at": null,
  "heartbeat_at": "2026-09-13T11:47:20Z",
  "finished_at": null,
  "checks":   { "planned": 27, "total": null, "passed": null, "failed": null, "skipped": null },
  "evidence": {
    "transcript": "untracked/vmtest-bridge/archive/<run_id>/transcript.log",
    "transcript_sha256": null,
    "base": { "profile": "server", "kind": "fast", "name": "server-fast-44",
              "built_from": "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2", "compose_label": "44-1.7",
              "base_sha256": "...", "compose_id": "Fedora-44-20260422.1",
              "installed_at": "...", "last_upgraded_at": "...",
              "last_upgraded_revision": 1789172543, "last_upgraded_mirror": "…",
              "freshness": "current", "freshness_degraded": false,
              "refresh_state": "complete" },
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

03. **`skipped` is a first-class counter, a skipped check is never counted as passed, and an
    all-skipped run cannot pass.** An earlier draft required only
    `passed + skipped == total`, which `passed = 0, skipped = total` satisfies — row 13 of the
    `AgentNotes.md` table reappearing at the verdict layer, in the very rule written to
    prevent it. `verdict: pass` now requires **all** of: `failed == 0`,
    `passed + skipped == total`, `total == planned`, **`passed >= 1`**, and
    `skipped <= max_skipped` where `max_skipped` is declared **per scenario** in
    `vars/vm-test-scenarios.yml`. `skipped` is enumerated by name in the transcript either
    way, so a run that quietly stopped asserting is visible even while still under its cap.

04. **`evidence.guest.boot_id`** is read from `/proc/sys/kernel/random/boot_id` **inside the
    guest**. A `pass` with a null boot id could not have booted anything, and the value is
    unique per boot, so a replayed or cached response is detectable.

05. **`evidence.playbook_recap`** carries the guest's own PLAY RECAP. A `pass` with `ok: 0` is
    an error by construction.

06. **`heartbeat_at`** is refreshed by the scope while running. A consumer seeing
    `state: running` with a heartbeat older than the threshold knows the host process died —
    a run never sits at `running` forever with no way to tell.

07. **Silence is never an outcome.** The `accepted` response is written *before* dispatch
    (step 12 of §6.4), and every rejection writes a `rejected` response too. "Request consumed,
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

11. **Every response is HMAC-signed — and the signature is verifiable only on the HOST.** The
    spool is sandbox-writable, so nothing stops the sandbox authoring
    `responses/<request>.response.json` itself. The watcher and the run scope therefore sign
    each response with a key in `~/.config/vmtest-bridge/<slug>/response.key` (`0600`,
    host-only, never on the mount), with the request nonce inside the signed payload so a
    previous run's response cannot be replayed.

    **An earlier draft then had `scripts/vmtest-request.bash` — which runs *in the
    container* — verify that signature and report `forged`. That is not achievable, and
    claiming it was worse than claiming nothing.** Verification needs the key; the key is
    host-only by construction; and giving the container the key would let the sandbox forge
    signatures, destroying the property. The check could never have run. A reader that
    *appears* to verify and cannot is precisely the shape this document spends §6.6 arguing
    against — it would have been a vacuous check wearing a security claim.

    So the split is explicit:

    - **`vmtest verify <run-id>` is a HOST command.** It is where signature verification
      happens, it is the only place it can happen, and it is what a human runs when a verdict
      matters.
    - **`scripts/vmtest-request.bash` (container) does not verify and does not pretend to.**
      It reports `signature: present, not verifiable from inside the sandbox` and prints the
      `vmtest verify` command plus the off-mount audit-log path. It reads the response for
      *content*, and says plainly that content is unauthenticated at its vantage point.

    **Stated honestly: none of this makes a dishonest agent honest.** An agent that wanted to
    mislead could simply not run the request and describe a result. What the signature buys is
    that a human, on the host, can tell a response the host wrote from one it did not.
    **The off-mount `~/.local/state/vmtest-bridge/<slug>/service.log` is the verdict of
    record**, and both tools print its path with every verdict so there is always somewhere to
    look that the sandbox cannot reach.

`scripts/vmtest-request.bash` (container side) exits `0` **only** on
`state == "finished" && verdict == "pass"`, and always states that the result is
host-verifiable but not sandbox-verifiable. Timeout, `unknown`, `rejected`, `fail` and
`error` are all non-zero and each prints a distinct reason, as does a stale or
`path_unit: failed` heartbeat (§6.5). `forged` is **not** an outcome it can report — only
`vmtest verify` on the host can reach that conclusion.

### 6.7 Known limits, stated rather than discovered

Multi-second dispatch latency; a fixed verb set that only a host playbook run can widen; rate
limiting that will reject a burst; no human-confirmation mode (a verb is `auto` or `deny`,
never "ask"); single-flight, so a second `run-scenario` while one is in flight is rejected
rather than queued.

Two residuals worth naming rather than discovering:

- **A wedged path unit needs a human.** If either unit does reach `failed`, recovery is
  `systemctl --user reset-failed vmtest-bridge@<slug>.path .service` typed on the host. §6.5's
  heartbeat reports the condition and prints that command, but it deliberately does not run
  it — self-repair would restore service while hiding that the limit was hit.
- **A determined `touch` loop still costs throughput.** `TriggerLimitBurst=0`,
  `StartLimitIntervalSec=0`, drain-all-per-activation and the end-of-activation debounce
  together remove the *wedge*, not the *load*: a sandbox writing continuously keeps the
  watcher busy and delays real requests behind rate-limit rejections. The bridge degrades
  loudly under that rather than dying silently, which is the property being bought; it is not
  an abuse-proof queue.

---

## 7. Performance plan

**There is deliberately no duration column.** An earlier draft carried one, populated with
vague elapsed-time phrases. Those are time estimates, which `R-PLAN-TIME-ESTIMATE` blocks in
plan-directory documents, and they would have been guesses in any case — nothing here is
measurable from a container with no `/dev/kvm`. The table below records what each operation
*does* and what dominates its cost, which is what makes the optimisation choices auditable.
Phase 3 and Phase 5 record **measured** values in the run transcripts, and those measurements
are the only numbers that belong anywhere.

| Operation                                     | Mechanism                                                                                                                                              | What dominates the cost                                                                                 |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| Freshness probe                               | 5 HTTP GETs, conditional                                                                                                                               | network round-trips only; 213 KB cold, ~9 KB warm (measured sizes, §4.4)                                |
| Fetch Cloud Base qcow2                        | `curl -z` + sha256 vs `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`                                                                                            | 583,729,152 B on first fetch; a 304 thereafter                                                          |
| Fetch netinst ISO (**boots Anaconda**, §5.3)  | `curl -z` + sha256 vs `Fedora-Everything-44-1.7-x86_64-CHECKSUM`                                                                                       | 1,217,329,152 B on first fetch; a 304 thereafter                                                        |
| Fetch Workstation Live ISO (**payload only**) | `curl -z` + sha256 vs `Fedora-Workstation-44-1.7-x86_64-CHECKSUM`                                                                                      | 2,851,612,672 B on first fetch; a 304 thereafter                                                        |
| Build **server-fast** base                    | import qcow2 + `--cloud-init` first boot + `dnf -y upgrade` + cleanup                                                                                  | the upgrade backlog; no Anaconda — but see §3.5, this is **not** an Anaconda install                    |
| Build **server-full** base                    | `virt-install` + Anaconda from the **Server** tree (§4.3 — one tree per base)                                                                          | package download and install; the largest beneficiary of the DNF cache below                            |
| Build **desktop** base                        | `virt-install`, netinst boots Anaconda, `liveimg` unpacks the Live squashfs                                                                            | the 2.8 GB squashfs unpack; network only for `%post`                                                    |
| Refresh a base                                | **no boot of its own** — the run's own `play-AB-dnf-upgrade.yml` transaction is the probe (§4.4a); re-flatten afterwards only when it changed packages | the re-flatten alone, and it is O(1) where reflink is available; a no-change cycle costs nothing at all |
| Create a run overlay                          | `qemu-img create -f qcow2 -F qcow2 -b`                                                                                                                 | nothing — CoW, the new file is a few hundred KB                                                         |
| Boot a guest                                  | KVM + virtio                                                                                                                                           | firmware plus `systemd` unit startup; desktop additionally waits for GDM autologin to settle            |
| Provision, **server** profile                 | `run.bash` headless, ~20 plays                                                                                                                         | DNF — package download and install, hence the cache below                                               |
| Provision, **desktop** profile                | `run.bash` headless, ~30 plays incl. 10 gnome                                                                                                          | DNF plus flatpak plus extension installs                                                                |
| Assert + collect                              | in-guest acceptance + transcript pull                                                                                                                  | the assertion set itself; bounded and small                                                             |
| Destroy                                       | `virsh destroy` + `rm overlay.qcow2`                                                                                                                   | nothing — the base is untouched, so there is nothing to undo                                            |

### Optimisations, each with its justification

- **A host-side DNF cache — the largest win, and an earlier draft missed it entirely.** Every
  run of every scenario re-downloads the whole package set from a mirror, and DNF dominates
  the cost of both provisioning rows above *and* the `server-full` base build. Two mechanisms,
  used together:

  - **A persistent DNF cache exported into the guest over virtiofs** (`virtiofsd` is packaged
    for F44 — verified), mounted at the guest's cache directory with `keepcache=1` set in
    `/etc/dnf/dnf.conf`. A second run finds the RPMs already local. One cache directory **per
    profile**, never shared between concurrently running guests, because DNF's cache is not
    designed for concurrent writers and a lock fight would turn a performance win into a
    flaky failure.
  - **Pin a single mirror `baseurl` instead of the metalink.** This removes mirror-selection
    variance from the critical path, makes the cache hit-rate stable, and — separately and
    just as usefully — removes metalink flakiness from the *verdict*, so a mirror having a bad
    day cannot present as a product failure.

  **Both are divergences from a cold install and are recorded as such** in
  `evidence.divergences` (`dnf-cache-shared`, `dnf-baseurl-pinned`). A cached run does not
  prove the repo works against a fresh mirror fetch, and the transcript must not let that be
  assumed. A periodic cache-cold run (no virtiofs mount, metalink restored) is the check on
  the check.

- **Skip Anaconda on the server *fast* path.** `Fedora-Cloud-Base-Generic-<v>.qcow2` is an
  official, signed disk image, and `play-AA-preflight-sanity.yml:50-51` already names Cloud
  Base as supported. Importing it removes an entire OS install from the profile that matters
  most to Plan 00063 — **and it buys that speed by not testing the installer, which is why
  §3.5 keeps a second server path rather than substituting this one for it.**

- **qcow2 backing chain, depth 1.** §3.3. Clone cost is O(1), revert cost is an unlink, and
  concurrent runs share one read-only base.

- **`cp --reflink=auto` for the refresh copy, where the filesystem supports it.** On btrfs or
  reflink-capable XFS, copying the base for a refresh is O(1) metadata rather than a
  base-sized write — which removes the main cost argument against the flat-base decision of
  §3.3. Fedora Workstation defaults to btrfs and this repo's own kickstart builds btrfs
  (`ks.cfg:359-364`), so the host is likely to support it; `--reflink=auto` falls back to a
  full copy rather than failing, and which path was taken is recorded. The alternative, where
  reflink is unavailable, is to boot an overlay read-write and `qemu-img convert -O qcow2`
  overlay→new base in a single sparse pass. Gated on U1.

- **Do not hash the multi-GB base on every run.** An earlier draft verified `base_sha256`
  before every clone; on a multi-GB file that is a per-run tax paid to detect something that
  changes almost never. Per run: size, mtime and `qemu-img check`. Full sha256: on refresh,
  on reinstall, and on `lab-status --deep`. A mismatch at either level still refuses (§10) —
  only the *frequency* of the expensive check changes.

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

- **`cache=unsafe` on the overlay, not `cache=none`.** An earlier draft chose `cache=none` to
  avoid double-caching. That is the wrong trade twice over: the cache mode applies to the
  whole backing chain, so `none` also forbids the host page cache from sharing the
  **read-only base** across parallel runs and across repeated boots — the one place caching is
  pure profit — and DNF/RPM are fsync-heavy, which `none` makes expensive. The overlay is
  disposable by construction, so there is nothing for `cache=unsafe` to lose: if the host
  crashes mid-run the overlay is garbage, and a garbage overlay is deleted, not recovered.
  The base cannot be damaged because no run opens it for writing (§3.3). Pair with
  `io=io_uring` and create overlays with `-o lazy_refcounts=on`, which removes refcount
  metadata writes from the hot path at the cost of a repair pass after an unclean shutdown —
  again, a cost that only applies to a file we would have thrown away.

- **No tmpfs/ramdisk for the run overlay.** Tempting, and rejected: a desktop provisioning
  run writes multiple GB, so a ramdisk would evict the host's own page cache and could OOM
  the workstation the owner is using. The overlay lives on disk with `discard`; that is the
  right trade for a lab that runs on someone's daily driver. Stated as a decision because
  the brief asked about it.

- **`virt-install --unattended` is not used.** It drives osinfo's generated kickstart, which
  this design does not want — the kickstart is a tracked, reviewable file in
  `fedora-install/`, because it is part of what the lab tests. `--unattended` would hide it.
  **`--cloud-init` is used for the server fast path**, and its contract is confirmed from the
  upstream manual rather than deferred to a probe: it generates a NoCloud ISO, attaches it as
  a CDROM **for the first boot only**, and takes the suboptions `user-data=`, `meta-data=`,
  `network-config=`, `root-ssh-key=`, `root-password-file=`, `root-password-generate=` and
  `disable=on`. Because `virt-install` builds the seed ISO itself, the fast path needs
  neither `cloud-localds` nor a hand-rolled `genisoimage` step.

- **`--boot uefi`.** The documented spelling; an earlier draft wrote
  `--boot loader=… edk2-ovmf`, which is the low-level form. UEFI matches how the repo's own
  installer partitions (`ks.cfg:356,372` create `/boot/efi`), so the test resembles a real
  install rather than a legacy-BIOS convenience.

- **`--cpu host-passthrough`.** Exposes the host CPU's full feature set instead of the
  conservative default model. Free under KVM, and it matters for the compile and compression
  work a provisioning run does.

---

## 8. What this proves, and what it does not

Mapped onto the three plans stuck on "Blocked — HOST ACTION". **Deliberately conservative.**

### Plan 00063 — headless `run.bash` server/cloud provisioning

| Success criterion                                                                | Verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Provisions a headless Fedora **Cloud** box end-to-end with zero prompts          | **Dischargeable by the fast path.** `server-fast-provision` on the Cloud Base image, with a transcript. This is the "or Cloud box" half of the criterion, and it is the half the cheap path covers.                                                                                                                                                                                                                                                                        |
| Provisions a headless Fedora **Server** box end-to-end with zero prompts         | **Dischargeable only by `server-full-provision`** on an Anaconda-installed Server base (§3.5). The fast path does **not** discharge this: a Cloud Base image is not a Fedora Server install. Release-gated, so it is proved less often — but it is proved.                                                                                                                                                                                                                 |
| Every missing required value fails fast naming the fix, never hangs              | **Already discharged elsewhere, not by this.** 00063 Task 2.8 records 10 preflight gates passing in-container via `runuser -u nobody`. The VM adds nothing here and should not claim to.                                                                                                                                                                                                                                                                                   |
| A failed main or optional playbook makes a headless run exit non-zero            | **Dischargeable — but NOT by the scenario an earlier draft named.** See the correction below.                                                                                                                                                                                                                                                                                                                                                                              |
| No secret bytes enter the environment or cloud-init `user-data`                  | **NOT dischargeable by the default scenarios.** See the correction below. Dischargeable only by `server-github-token`.                                                                                                                                                                                                                                                                                                                                                     |
| GitHub auth works non-interactively via a scoped token; SSH-only git auth        | **Only via the opt-in, human-gated `server-github-token` scenario** (§10). Not reachable from the bridge.                                                                                                                                                                                                                                                                                                                                                                  |
| Secret files unlinked after use; no `ssh-agent` left behind (Tasks 2.2, 2.4)     | **Only via `server-github-token`** — the same vacuity as the row above, and it was claimed for the default path in an earlier draft. With `GITHUB_ACCOUNTS=none` there is no token file and no SSH passphrase file to unlink and no `ssh-agent` to leave behind, so the assertions would pass by absence. The vault-password file is the one secret the default path does create, so *its* unlink is genuinely assertable there; everything else needs the token scenario. |
| Desktop interactive `./run.bash` is unchanged (Task 3.2)                         | **Not dischargeable.** It is an *interactive* path; no VM proves a human's prompt experience. The lab proves the desktop *profile* provisions, which is a different and also valuable thing. This criterion stays open, or the owner re-scopes it.                                                                                                                                                                                                                         |
| `qa-all.bash` passes; version bumped; no new `2>/dev/null`, `\|\| true` or `sed` | unchanged by this plan                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

#### Two corrections to this map — the plan's own defect class, caught in review

**The negative scenario did not exercise the code path it vouched for.** An earlier draft
proposed `server-optional-play-missing`: name an optional play that does not exist and watch
the run exit non-zero. But `run.bash:684-686` is **name resolution** — it `hl_abort`s
*before any playbook runs*. The criterion (00063 Task 2.6 / D7) is that a **failed** playbook
propagates, which is `run.bash:700-702`. Proving argument validation and labelling it failure
propagation is `AgentNotes.md` row 14 exactly, written into the plan whose purpose is to
attack that class. Replaced with two scenarios that use only supported inputs and change no
repo code:

- `server-main-playbook-fails` — set `RUN_BASH_PROVISIONING_PROFILE` to an unrecognised
  value. `run.bash:2632-2634` forwards it verbatim as `-e provisioning_profile=…` with no
  validation of its own, so `play-AA-preflight-sanity.yml:55-58` fails play 1 of
  `playbook-main.yml` and the failure must propagate out of `run.bash`.
- `server-optional-playbook-fails` — **and the obvious choice for this is wrong.** Every
  hardware-assert play under `optional/hardware-specific/` that fails on absent hardware
  (`play-displaylink.yml`, `play-ipu6-webcam.yml`, `play-musiccast.yml`) is `gnome`-scoped, so
  on the server profile its scope guard `end_play`s before any assertion runs — the scenario
  would have **skipped, proving nothing**, while looking like a passing negative test. Use
  `play-nvidia.yml` instead: it is `scope: general` (`:12`) so it runs on the server profile,
  and its first assertion is the MOK vault check at `:203-211`, which **cannot** hold here —
  §10's throwaway vault writes a fresh `localhost.yml` with no vault-encrypted values, so
  `mok_password` is definitionally absent. It runs and fails, reaching `run.bash:698-701`.
  (Cost: it installs driver and CUDA packages before reaching the assert. If that assertion
  ever stops failing, the fallback is to move this scenario to the desktop profile, where the
  gnome-scoped hardware plays do run.)

`server-optional-play-missing` is kept as well, correctly relabelled: it proves *argument
validation*, which is a different criterion and a real one.

**The secrets criterion is vacuous on the default path.** An earlier draft called it
"dischargeable, and genuinely new". The default scenarios run
`RUN_BASH_GITHUB_ACCOUNTS=none`, so there is **no PAT and no SSH passphrase in the guest** —
grepping for secrets that were never supplied proves nothing about the criterion, which is
about those two secrets. A green grep here would be a check that passes because the thing it
searches for does not exist. It is dischargeable only by `server-github-token`, where real
secrets are present, and the map now says so.

**Net: Task 3.1 becomes dischargeable; Task 3.2 does not.** Counting the rows above rather
than asserting a total: **two** criteria become machine-provable by the default scenarios
(Cloud end-to-end; failure propagation, via the replacement scenarios), **one** more by the
release-gated full path (Server end-to-end), and **three** by the opt-in token scenario
(GitHub auth; no secret bytes; secret-file and `ssh-agent` teardown). One was never VM work
(interactive), one was already discharged in-container (preflight fail-fast), and one is
unchanged by this plan (QA). The teardown row moved out of the default column in this round
for the same reason the secrets row did — with no token and no passphrase supplied, there is
nothing to unlink and nothing to leave behind, so the assertions would have passed by
absence. Task 3.1's wording — "a real or VM Fedora Server **or** Cloud box" — is satisfied at
two different cadences, and the plan must record which, not just that it went green.

**Stale text in 00063 that T7.2 should fix while it is there.** 00063's Non-Goals and Task 1.6
still say the `RUN_BASH_GITHUB_ACCOUNTS=none` path is deferred to a follow-up. It shipped —
`run.bash:487-515` writes the fresh no-identity `localhost.yml`, and `run.bash:877-879`
documents the behaviour. Every default scenario in this design depends on it, so the plan text
contradicting the code is not a cosmetic issue.

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

**Two feasibility assumptions in the above were asserted, not verified — now U7.** They are
load-bearing enough that if either fails, most of this section's claim collapses:

- **Can `ccy` start at all in a guest with no Claude credential and no config import?** The
  launcher mounts `/tmp/claude-config-import` (`claude-yolo:1947`) and manages a token pool.
  Starting a session with a *synthetic* token requires that token to reach PID 1's
  environment through ccy's own token store — the mechanism is not checked anywhere in this
  design.
- **Does `ccy --rebuild` work inside the guest?** It builds a container image with rootless
  podman inside a VM. Plausible, unverified.

Both become **U7**, probed in Phase 0 on the host (where a CCY container already exists) and
re-probed in the guest during the 00092 scenario. A design that claims the biggest unlock in
§8 on the strength of two unchecked assumptions would be making exactly the bet this plan
exists to stop.

**And one genuine conflict with §3.4, which needs the owner.** Step 5 needs
`CCY_CHILD_CLAUDE=1` in the project's **tracked** `.claude/ccy/ccy.env`, where it is
commented out at every pushed commit. But §3.4 says the guest provisions from a pinned
commit, and refuses a dirty tree, precisely so the transcript's `repo_commit` is not a lie.
The two cannot both hold.

**Recommendation: add `CCY_CHILD_CLAUDE` to the launcher's environment passthrough.** An
earlier draft offered three options and ranked this one last, on the grounds that it was "a
change to another plan's shipped feature". Re-measured, that reasoning does not survive:

- **The container side already works.** `entrypoint.sh:362` reads `${CCY_CHILD_CLAUDE:-}`
  from the environment *after* sourcing `ccy.env`, and does not care where the value came
  from. Nothing there needs changing.
- **The gap is one line.** The launcher's `-e` passthrough list
  (`files/var/local/claude-yolo/claude-yolo:3048-3061`) simply does not carry the name. It
  already passes `CCY_CLAUDE_WRAPPER`, `CCY_NO_SUPERVISOR` and others by exactly this
  pattern, so the change is additive and idiomatic rather than novel.
- **It is not what 00092 declined.** That plan's Non-Goals rule out *"a host launcher flag
  (`ccy --child-claude`)"* — a new precedence layer with its own UX and its own
  documentation. Passing through an environment variable the entrypoint already reads is a
  different thing. **Precedence, stated the right way round:** `entrypoint.sh:340` sources
  `ccy.env` *after* the container environment is in place, and the template line is a plain
  `export` (`.claude/ccy/ccy.env:36`), so an **uncommented `ccy.env` entry overrides the
  environment** — the env supplies the value only when `ccy.env` is silent. That is the
  existing behaviour, unchanged; the passthrough adds a way to set the flag without editing a
  tracked file, not a new layer above one.
- The cost is honest and small: it touches `files/var/local/claude-yolo/claude-yolo`, so it
  requires a **CCY version bump** per `CLAUDE.md`'s critical rule, and that bump belongs in
  whichever plan lands it.

It is also the only option that removes the conflict rather than recording it forever. The
alternatives, kept for the record and both worse:

- **Push a branch with the flag on** and pin that commit. Consistent with §3.4 and needs no
  code change, but it must be pushed to the **public remote** for §3.4's "exists in git and is
  reviewable" to hold, it adds a branch that exists only for testing, and every such run
  carries a `tested-on-test-branch` divergence. This is the fallback if the owner would rather
  not touch CCY from this plan.
- **Let the guest edit the tracked file** and record a `ccy-env-child-claude-enabled`
  divergence with the diff inline. Available today, needs nothing from anyone, and pays a
  permanent fidelity cost on every run to avoid a one-line change. Last resort.

**Net, with the above caveats: Task 6.4 steps 1–4 and 6 become dischargeable; step 5 becomes
dischargeable for every invariant using a synthetic token, conditional on U7; the
host-cleanliness half of I1 remains a host action.**

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
  filesystem type on the lab path; whether discard/`fstrim` works there; **whether
  `cp --reflink=always` succeeds there** (U1, and the difference between an O(1) and a
  base-sized refresh); libvirt present or absent; `systemd --user` linger state;
  `qemu:///session` networking and a host→guest port forward (U2); `virsh screenshot` and the
  `--video` options (U4); `dnf group list --hidden` for the GNOME environment id (U5);
  **whether `ccy` starts with no credential and whether `ccy --rebuild` works (U7)**;
  **whether a `systemd-ask-password` prompt reaches a serial console, and whether `swtpm`
  vTPM is usable from a `qemu:///session` domain (U8)**;
  `virtiofsd` present, for the DNF cache export of §7.

  Confirmation-only probes, whose answers are already settled from documentation (§0) and
  which must therefore be reported as *confirmations* rather than as findings: the `%f`
  specifier inside a scratch `.path` unit, and `virt-install --cloud-init=?`.

- **T0.2** Decision gate on the triage report: KVM available, and enough headroom for
  **three** bases (§3.5) plus run overlays. Without KVM, TCG emulation makes a desktop run
  impractical and the lab must refuse rather than run something nobody will wait for.

- **T0.3** Record the answers to U1, U2, U4, U5, U7 and U8 in the plan's `JOURNAL/`, and
  correct this document where reality differs. U8 in particular **selects** the LUKS route of
  §5.3a rather than merely confirming it, so Phase 5 cannot start until it is answered.

### Phase 1 — The freshness engine (no VM needed, runs in a container)

- **T1.1** Write `tests/helpers/vmtest/test_upstream.py` first, then
  `helpers/vmtest/upstream.py`: parse `.treeinfo`, `COMPOSE_ID`, `releases.json`, the Bodhi
  releases document, and `repomd.xml` into a per-base `artefact_identity` plus a separate
  `package_revision`. The compose label comes from `releases.json`'s `link` field — **no HTML
  directory-listing parser**, per §4.3. Pure functions over fixture text; no network in the
  unit tests.

- **T1.2** Test-first, then `helpers/vmtest/freshness.py`: the policy of §4.4.

  **The test population is the whole readable/unreadable matrix, not a set of cases someone
  thought of.** An earlier draft listed three hand-picked assertions, and the most important
  of them — "`unknown` can never collapse into `current`" — **would have passed against a
  policy that did exactly that**, because it only ever exercised the total-outage case while
  the fail-open lived in the partial case. Enumerating the input space is the fix; adding a
  fourth hand-picked case is not (`AgentNotes.md` row 9b: *"replacing a stale enumeration with
  a fresher enumeration is not the fix; deriving the set is"*).

  So: parametrise over `artefact_identity ∈ {matches, differs, unreadable}` ×
  `package_revision ∈ {unchanged, advanced, unreadable}` × `TTL-U ∈ {expired, unexpired}` ×
  `TTL-R ∈ {expired, unexpired}`, assert a named verdict for **every** cell, and assert the
  function is **total** — no input combination may fall through. Then, individually
  demonstrated to go red when the guard is removed:

  - identity unreadable → `unknown`, for every value of the other three axes (B6);
  - an advanced revision → `refresh` **even inside an unexpired TTL-U** (the round-1
    inversion; a test exercising only the expired-TTL case would have passed against the
    wrong policy);
  - revision unreadable + TTL-U unexpired → `current` **with `degraded: true`**, never a bare
    `current`;
  - a probe revision that has gone **backwards** → `unknown`, not a retry: the probe reads the
    canonical host, so a regression there is not mirror lag (§4.4a);
  - Bodhi leaving `current` → a warning and a `divergences` entry, **not** `reinstall`.

- **T1.3** Test-first, then `helpers/vmtest/scenarios.py`: manifest parsing and the
  planned-check accounting of §6.6.

- **T1.4** `helpers/vmtest/probe_upstream.py` — thin executor, does the HTTP, prints stable
  marker lines (`VMTEST-FRESHNESS-*`), diagnostics to stderr per
  [StderrHygiene.md](../../StderrHygiene.md), `--fixture-dir` for offline tests.

- **T1.5** `vars/vm-test-scenarios.yml` — the tracked manifest: scenario ids, the **`base:`
  each scenario requires by name** (§3.5, the field that makes base substitution an error
  rather than a silent downgrade), profile, guest sizing, TTL-U, TTL-R, planned check counts
  and per-scenario `max_skipped`.

- **T1.6** `./scripts/qa-all.bash`; commit. **This phase alone answers the owner's question 7
  as a runnable command.**

### Phase 2 — The lab playbook

- **T2.1** `playbooks/imports/optional/common/play-vm-test-lab.yml`, `scope: general`,
  `root_dir` pattern, `vars_files` on `vars/fedora-version.yml`. Installs, by **name only**:
  `libvirt-daemon-kvm`, `libvirt-daemon-config-network`, `libvirt-client`, `virt-install`,
  `qemu-kvm`, `qemu-img`, `edk2-ovmf`, `guestfs-tools`, `virtiofsd`, `cloud-utils`, `xorriso`,
  `osinfo-db`, `lorax`, `swtpm-tools`.

  **No versions are pinned or quoted, deliberately.** Every name above was confirmed to exist
  on F44 via mdapi, but §4.2 of this same document says mdapi reports the highest NEVRA across
  repos *including* `updates-testing` — and it does: `qemu-kvm` answered from
  `updates-testing`, several others from `updates`. Quoting those versions as "verified
  present on F44", as an earlier draft did, would cite a source this design had already
  disqualified, two sections apart. **Existence is proven; the version list was not what a
  default install gets.** The play uses `ansible.builtin.package` with names, which is the
  repo's own convention (`CLAUDE/AnsibleStyle.md`) and needs no version anyway.

  (`libguestfs-tools-c` does **not** exist on F44 — mdapi returns 400 — so `guestfs-tools` is
  the correct name. That one is a name fact, not a version fact, and stands.)

- **T2.2** Enable the libvirt user session, the network, and `loginctl enable-linger`. Create
  `~/.local/share/vmtest/` with explicit `owner/group/mode` on every file task. Assert that
  `/dev/kvm` is openable read-write by the running user and **fail loud** if it is not (§2.4:
  triage measured it `0666`, so no `kvm` group membership and no re-login are needed; the
  assertion is what catches a host where that is not so).

- **T2.3** Deploy `files/home/.local/bin/vmtest` `0755`, and render
  `~/.local/share/vmtest/scenarios.allowlist` from `vars/vm-test-scenarios.yml` — the
  authority of §2.3.

- **T2.4** Deploy the in-guest scripts (`guest-acceptance-*.bash`, `guest-cleanup.bash`) to
  `~/.local/share/vmtest/`, out of the shared mount.

- **T2.5** Document: a row in `docs/playbooks.md`, a new `docs/vm-acceptance-testing.md`,
  linked from `docs/README.md`.

- **T2.6** `./scripts/qa-all.bash`; commit.

### Phase 3 — The server fast path and the first real scenario

- **T3.1** Base builder for `server-fast`: resolve the Cloud Base artefact name from
  `releases.json` for the `fedora_version` in the version file, download with `curl -z`,
  verify sha256 against the signed `Fedora-Cloud-<v>-1.N-x86_64-CHECKSUM` (reusing the shape
  of `setup-netinstall-boot.bash:699-791`, including its GPG verification of the clearsigned
  CHECKSUM), import, cloud-init first boot, `dnf -y upgrade`, `guest-cleanup.bash`, flatten,
  write `base.json` recording the compose label, artefact sha256, the updates `<revision>`
  and **which mirror answered**.

- **T3.2** `vmtest run server-fast-provision`: freshness gate, overlay, boot, SSH, headless
  `run.bash` at the pinned commit, in-guest acceptance, transcript, verdict, destroy.

- **T3.3** `guest-acceptance-server.bash` — the assertion set, with `planned` declared up
  front and `skipped` enumerated by name. Shared by both server bases; the base identity
  comes from `evidence.base`, so one script serves both and cannot silently conflate them.

- **T3.4** The negative scenarios — **the falsifiability proof**, which makes the harness go
  red on purpose. Per `AgentNotes.md`: "break the fix on purpose and watch the new test go
  red, or you have not tested it." Three, not one, because they prove three different things
  (§8):

  - `server-main-playbook-fails` — unrecognised `RUN_BASH_PROVISIONING_PROFILE` fails
    `play-AA-preflight-sanity.yml:55-58`, i.e. play 1 of `playbook-main.yml`, and the failure
    must propagate out of `run.bash`.
  - `server-optional-playbook-fails` — a `hardware-specific/` play that **runs and fails** on
    a VM, reaching `run.bash:700-702`.
  - `server-optional-play-missing` — a name that does not resolve, reaching
    `run.bash:684-686`. This proves **argument validation**, and is labelled as such; an
    earlier draft mislabelled it as failure propagation.

  Each must be shown to produce `verdict: fail` (not `error`), with `failure.stage: provision`
  and the guest's own non-zero exit in the transcript.

- **T3.5** Plan-local `deploy.bash` (HOST, `plan_mode deploy`, `plan_gate_change`) chaining
  into `acceptance.bash`, both on `_planlib.inc.bash`.

- **T3.6** `./scripts/qa-all.bash`; commit.

### Phase 3b — The server full path (Anaconda), release-gated

Separated from Phase 3 so the cheap path lands and starts earning first, and so the
fast/full distinction of §3.5 is a delivery boundary rather than only a paragraph.

- **T3b.1** Base builder for `server-full`: Anaconda from the **Server** install tree (`releases/<v>/Server/x86_64/os/`) with
  `fedora-install/ks-vm-server.cfg`, verified against that tree's `.treeinfo` and
  `Fedora-Server-<v>-<label>-x86_64-CHECKSUM`. **One tree, named** — the Server and Everything
  trees ship different `install.img`, `initrd.img` and `boot.iso` (§4.3), so "or" would have
  made the base's identity unresolvable. Reuses `setup-netinstall-boot.bash`'s **ISO
  discovery and verification functions** as prior art — not the script itself, which sets up
  a GRUB boot entry and repartitions the operator's own disk and is not VM-applicable.
- **T3b.2** `vmtest run server-full-provision`, on the shared `guest-acceptance-server.bash`.
- **T3b.3** Assert in the transcript header, and in `evidence.base`, which of the two server
  claims a given verdict supports — so a fast-path pass can never be cited for the other.
- **T3b.4** `./scripts/qa-all.bash`; commit.

### Phase 4 — The bridge

- **T4.1** Spool layout and the request/response schema, documented in
  `docs/vm-acceptance-testing.md`.

- **T4.2** Test-first, then `helpers/vmtest/spool.py` — the §6.3 defences. This lands
  **before** the watcher, because the watcher is only safe if this exists. Tests must include
  the attacks, not just the happy path: a symlinked `responses/`, a symlinked
  `archive/<run_id>/`, a symlinked **`diagnostics/`**, a symlinked **`untracked/`** (the
  component-walk case), a `requests/` entry that is a symlink, **a FIFO and an oversized file
  in `requests/`**, a body swapped between validation and dispatch, and a filename verb that
  disagrees with the body verb. Each must be demonstrated to fail the check when the defence
  is removed.

  **Assert on the refusal, not on a particular errno.** `O_PATH|O_NOFOLLOW` on a symlink
  succeeds, so a test expecting `ELOOP` would pass against a symlink while proving nothing;
  the rejection comes from `O_DIRECTORY` as `ENOTDIR`, and the `S_ISDIR` confirmation is what
  makes it certain (§6.3).

- **T4.3** `files/home/.local/bin/vmtest-bridge-watcher` — thin executor over `spool.py`, with
  the §6.4 validation order including the **watcher-side** rate limit at step 8.

- **T4.4** `vmtest-bridge@.path` / `.service` and the policy file, deployed and enabled by
  the play with the escaped-path instance name, **`TriggerLimitBurst=0`** and
  **`StartLimitIntervalSec=0`** (§6.4 — a hostile `touch` loop reaches any finite limit, and
  reaching one wedges the path unit), and `MODE_refresh-base=deny` shipped as the default.

- **T4.5** `vmtest-bridge-heartbeat@.timer` / `.service` and the writer — the §6.5 liveness
  path, deliberately not path-triggered.

- **T4.6** The response state machine, HMAC signing and heartbeat in
  `helpers/vmtest/verdict.py`, with the `accepted` stub written before dispatch, and the
  `passed >= 1` / `max_skipped` pass rule of §6.6 rule 03.

- **T4.7** `scripts/vmtest-request.bash` — the container-side requester and reader. Checks the
  heartbeat *before* writing a request; exits 0 only on `finished` + `pass`; states that the
  signature is present but **not verifiable from inside the sandbox** and prints the
  `vmtest verify <run-id>` line; prints the off-mount audit-log path with every verdict.

- **T4.8** A bridge selftest proving each rejection path rejects **and** produces a response:
  bad filename, denylisted verb, unknown verb, verb/body disagreement, unknown argument,
  `MODE=deny`, missing policy file, watcher rate limit, in-flight lock,
  symlinked spool directory (which refuses rather than responds — assert *that* distinction).
  Modelled on 00092's `selftest-probes.bash`, which found two real defects in its own probes
  on first run.

- **T4.9** A liveness selftest: stop the `.path` unit, put it in `failed`, and assert that
  `vmtest-request.bash` reports "bridge wedged" with the remedy command — **not** a timeout
  and **not** a `fail`. This is the test for the defect B1 found; without it the fix is
  unproven.

- **T4.10** `./scripts/qa-all.bash`; commit.

### Phase 5 — Desktop base and desktop scenario

- **T5.1** `fedora-install/ks-vm-desktop.cfg` — fully non-interactive, `liveimg`-based, UEFI
  partitioning, **btrfs WITH the LUKS wrapper kept** (§5.3a: the install is unattended-capable
  because `ks.cfg:359-360` and `:377-378` pass the passphrase inline; only the *boot* needs
  automating), a per-run throwaway passphrase, GDM autologin, and a header stating plainly
  that it is for VM testing only and is not the shipped installer.
- **T5.1b** The LUKS boot unlock of §5.3a: `console=ttyS0` **and `plymouth.enable=0`** on the
  kernel command line (without the second, `ks.cfg:444`'s `rhgb quiet` leaves Plymouth owning
  the password agent and nothing reaches the serial console), `plymouth-disabled` recorded in
  `evidence.divergences`, serial capture from the first instant of boot, the passphrase driven
  in at the prompt, and — the part that must be tested by wedging it on purpose — a guest that
  does not reach userspace reports `failure.stage: boot`,
  `reason: wedged at the LUKS passphrase prompt` with the console excerpt, **never** a bare
  timeout. Test the matcher itself with Plymouth left enabled, to prove it reports the wedge
  rather than degrading into the timeout it exists to replace.
- **T5.2** **The boot medium, which B5 showed was missing entirely.** Fetch and verify *two*
  artefacts (§5.3): the **netinst ISO**, which boots Anaconda, and the **Workstation Live
  ISO**, from which `LiveOS/squashfs.img` is extracted as the `liveimg` payload. Then decide
  and implement how the squashfs reaches the installer — a second `--disk device=cdrom`
  carrying squashfs + kickstart and mounted in `%pre`, or an HTTP URL served from the host —
  and record which was chosen and why.
- **T5.3** Desktop base builder: `virt-install` booting the netinst kernel/initrd
  (`--location` or `--cdrom`) with the kickstart, wait for the install, `guest-cleanup.bash`
  **keeping** the created user and the autologin drop-in, flatten, write `base.json`.
- **T5.3b** **The session-environment probe (S1).** Inside the autologin guest, capture `env`
  under (a) a GNOME autostart entry, (b) `systemd-run --user`, and (c) a real interactive
  session; diff all three; pin the expected set; and enumerate every residual difference in
  `evidence.divergences`. This decides which dispatch mechanism T5.4 uses rather than
  assuming, and it exists because an earlier draft claimed `systemd-run --user` gives "the
  real session's environment" when it does not (`XDG_SESSION_TYPE`, `XDG_SESSION_ID` and
  `XDG_SEAT` are logind per-session; a transient user unit is in no logind session).
- **T5.4** `vmtest run desktop-fresh-install`, dispatching the provisioning run into the
  session by whichever mechanism T5.3b selected.
- **T5.5** `guest-acceptance-desktop.bash` — the §5.4 assertion set, including
  `gnome-extensions info <uuid>` `State: ACTIVE` for every deployed UUID and the
  recap-play-count coverage check.
- **T5.6** `virsh screenshot` evidence capture, with the evidence-not-assertion boundary
  written into the transcript beside it.
- **T5.7** `./scripts/qa-all.bash`; commit.

### Phase 6 — Freshness automation, retention and guards

- **T6.1** Wire the Phase-1 policy into `vmtest`: every `run-scenario` evaluates freshness
  first and refuses on `unknown`.

- **T6.2** `refresh-base` and the flat re-snapshot of §3.3, built on §4.4a: **no refresh boot
  of its own.** The guest acceptance script reports `play-AB-dnf-upgrade.yml`'s
  `dnf_upgrade` changed-package count and the **guest-seen** revision into `evidence`;
  `vmtest` re-flattens after the run only when that count was non-zero **and**
  `guest_seen >= probe_seen`; `guest_seen < probe_seen` records `refresh_state: incomplete`
  and re-runs rather than marking the base current. Uses `cp --reflink=auto` where U1 says the
  filesystem supports it.

- **T6.2a** Tests for the two traps this section was built out of, each demonstrated to go red
  when the guard is removed:

  - a guest whose mirror lags the probe must produce `incomplete`, **never** "checked and
    unnecessary" (B7);
  - `artefact_identity` unreadable with `package_revision` readable must produce `unknown`,
    **never** `current` — the partial-outage case the earlier T1.2 could not see (B6).

- **T6.2b** The host-side **DNF cache** of §7: the per-profile cache directory, the
  `virtiofsd` export, `keepcache=1` and the pinned mirror `baseurl` in the guest, all recorded
  in `evidence.divergences` as `dnf-cache-shared` and `dnf-baseurl-pinned`.

  Plus the check on the check, given a name and a cadence so it cannot decay into an intention:

  - scenario id **`server-fast-provision-cold`** — the same scenario with the virtiofs mount
    absent and the metalink restored, declared in `vars/vm-test-scenarios.yml` like any other;
  - cadence: **on every `reinstall` of its base, and at least once per TTL-U window**,
    whichever comes first — so the cold path is exercised on the same clock that governs base
    freshness rather than on someone remembering;
  - **every cached run's response carries `evidence.last_cold_pass`** — the run id and
    timestamp of the most recent passing cold run. A cached `pass` whose `last_cold_pass` is
    older than the cadence is reported as `degraded`, not as an ordinary pass. Without that
    back-reference a cached green says nothing about whether the repo still works against a
    real mirror, which is the whole reason the cold scenario exists.

- **T6.3** A `systemd --user` timer running a nightly freshness **probe that only reports** —
  it writes a status file and never rebuilds unattended. An unannounced disk-churning rebuild
  on the owner's workstation is a surprise; a status file is not.

- **T6.4** Disk-space floor (accounting for the 2x base-size a rebuild needs, less where
  reflink applies) and guest-RAM ceiling, both refusing loudly rather than cleaning up and
  continuing. Retention sweep covering `runs/` (keep the last N **plus every failed run**),
  failed **desktop** base builds (kept for diagnosis, per §3.5's asymmetry; a failed
  `server-fast` base is discarded), and **`quarantine/`**, which is on the shared mount and
  would otherwise be an unbounded sandbox-writable directory — a trivial disk-fill. Every
  eviction is recorded in the off-mount audit log, so retention is never silent deletion.

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

| Risk                                                                  | How it is handled                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **A broken VM run read as a failing product**                         | Three-valued verdict with a `failure.stage`. `error` means the harness could not complete (no boot, SSH timeout, no upstream signal, stale allowlist); `fail` means the product ran and an assertion failed. `error` never renders as `fail`, neither ever renders as `pass`. And `checks.planned` vs `checks.total` catches the subtler case: a harness that died after 4 of 27 checks with all 4 green reports `error`, not `pass`.                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| **Disk-space exhaustion**                                             | A free-space floor is checked before every run **and** before every refresh, and the refresh floor accounts for the 2x base size a flat rebuild needs. Overlays are CoW so a run costs its writes, not a base. Retention keeps the last N runs plus every failure. The lab refuses loudly; it never deletes something to keep going.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| **Snapshot-chain corruption**                                         | Chain depth is 1 and the base is attached **read-only** to every run, so no run can write it. Refresh writes a new file and renames atomically. `base.json` records `base_sha256`; every run verifies it before cloning and refuses on mismatch — so silent bit-rot surfaces as a refusal, not as strange failures.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| **A stale base silently in use**                                      | Freshness is evaluated on **every run**, not on a timer, and the trigger is the updates-repo revision rather than a clock (§4.4). The policy is **total over its inputs**: every readable/unreadable combination has a named verdict, an unreadable `artefact_identity` is `unknown` and blocks, and a TTL-backstopped `current` is stamped `freshness.degraded: true` rather than passing as an ordinary one. The only override is host-CLI, human-typed, and recorded in `evidence.overrides`.                                                                                                                                                                                                                                                                                                                                                                                                 |
| **A lagging GUEST mirror certifying a base the guest never received** | The probe reads `dl.fedoraproject.org`; the guest's `dnf` resolves through the metalink, which today offers **45 mirror hosts, none of them the canonical one, advertising repodata across ~2 days**. So `base.json` records `last_upgraded_revision` as the revision the **guest** saw, never the probe's: `guest >= probe` is a complete refresh, `guest < probe` is `refresh_state: incomplete` and a re-run (§4.4a). This is the one comparison that can see the condition — probe-to-probe cannot.                                                                                                                                                                                                                                                                                                                                                                                          |
| **A regressing PROBE revision misread as mirror lag**                 | It is not lag: the probe reads the canonical host directly, so a backwards value there means the origin went backwards. That resolves to `unknown` and blocks. An earlier draft treated it as a retry, which protected a path that does not exist while leaving the real one (above) open.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| **A cheap Cloud-image pass cited as proof of a fresh install**        | The two claims are separated at the base, the scenario, the transcript header and — bound in the data rather than in prose — `evidence.base.kind` and `evidence.base.name` (§3.5; `profile` is `server` for both and cannot distinguish them). A Cloud Base image exercises no partitioning and no kickstart, so `server-fast-provision` is never presented as installer evidence; `server-full-provision` and the `desktop` base are what carry that claim.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| **The sandbox edits the allowlist or an assertion script**            | Neither is read from the shared mount on the host path. Ansible-deployed copies are the authority; drift is reported with both digests and refused with a named remedy, never silently used.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| **Secrets on a throwaway VM**                                         | The default scenarios use `RUN_BASH_GITHUB_ACCOUNTS=none` (no PAT, no SSH passphrase — `run.bash:877-879`) and a **per-run randomly generated vault password that decrypts nothing**: with `RUN_BASH_CONFIG_SOURCE=none`, `run.bash:487-513` writes a fresh `localhost.yml` with no vault-encrypted values, while `ansible.cfg:42` still needs a readable `vault-pass.secret`. So the guest holds exactly one secret and it is worthless. The token-bearing scenario is opt-in, host-CLI only, uses a dedicated throwaway GitHub account's short-lived PAT delivered over the SSH channel into a tmpfs file — **never** via cloud-init `user-data`, per `docs/headless-provisioning.md:127-131` — and revokes it afterwards. It is not in the bridge's argument enumeration, because a sandboxed agent asking the host to put a PAT into a VM is the precise shape the bridge exists to prevent. |
| **No KVM / nested virtualisation unavailable**                        | Phase-0 decision gate. TCG emulation is roughly an order of magnitude slower; a desktop run becomes something nobody waits for. The lab refuses rather than producing a result hours late that nobody reads.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| **The harness passes green having asserted nothing**                  | Three structural defences: the negative scenarios (T3.4) that must go red; the bridge selftest (T4.8) proving each rejection path rejects; and `planned` vs `total` vs `skipped` accounting in every response.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| **Fidelity divergence read as fidelity**                              | Every known divergence — harness-set GDM autologin, throwaway vault password, absent hardware, synthetic CCY fleet, synthetic OAuth token — is enumerated in `evidence.divergences` beside the verdict, so a reader sees what the green does not cover without opening a design document.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| **`run.bash` self-updating mid-run**                                  | `run.bash:1993-2004`: when `RUN_BASH_GIT_REF` is set, the declared ref replaces `git pull`. Scenarios always pin a 40-hex commit, so the guest cannot drift onto a newer origin tip and make `evidence.repo.commit` a lie.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| **A request that is never answered**                                  | The `accepted` response is written before dispatch; a heartbeat is refreshed while running; a terminal response is always written; rejections write responses too. The container-side reader times out into `unknown`, never into `pass`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| **Bridge rate-limit exhaustion wedging the lab**                      | An earlier draft's answer here was *wrong in the dangerous direction*: it put the limit in `StartLimitBurst=` on the oneshot and promised a "rejected response naming the limit". `systemd.path(5)` propagates a hit start limit to the **path unit**, which then fails and stops watching — so no oneshot would run, no response would be written, and the bridge would be permanently and silently dead until a human ran `systemctl --user reset-failed`. Now: the limit lives in the watcher (§6.4 step 8) and rejects with a response while exiting 0; `TriggerLimitBurst=0` and `StartLimitIntervalSec=0` remove the systemd kill switches a hostile loop would otherwise aim at, with drain-all-per-activation plus a debounce providing the busy-loop protection instead; and §6.5's independent heartbeat timer makes a wedged path unit visible and names the `systemctl --user reset-failed` remedy. T4.9 tests it by wedging the unit on purpose.                                                                                                                                  |
| **The host overwriting its own files through a hostile spool**        | The sandbox can replace any spool directory with a symlink to `~/.ssh`, `~/.bashrc` or the policy directory, and ownership checks cannot help because rootless podman maps container root onto the host uid. Defence is structural (§6.3): pinned directory fds, `openat`/`renameat`, `O_NOFOLLOW\|O_EXCL`, `realpath` containment, and a **refusal** rather than a quarantine when a spool directory is a symlink. It lives in a TDD'd `helpers/vmtest/spool.py` whose tests include the attacks, because it is not safely expressible in the `mv -f` bash idiom.                                                                                                                                                                                                                                                                                                                               |
| **A request mutated between validation and dispatch**                 | The body is read **once** into memory; every check and the argv build use that buffer; the file is never re-opened (§6.3 D2). The verb is additionally encoded twice — filename and body — and disagreement is a rejection.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| **The sandbox forging its own passing response**                      | Responses are HMAC-signed with a host-only key off the mount, and the signature is checked by **`vmtest verify <run-id>` on the HOST** — the container reader cannot verify it and does not pretend to (§6.6 rule 11). Stated honestly: this does not make a dishonest agent honest. It lets a human, on the host, tell a response the host wrote from one it did not, and the off-mount audit log remains the verdict of record.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| **libvirt or the guest leaves a domain running after a failure**      | `vmtest` owns teardown in a trap armed for EXIT and INT/TERM/HUP; `lab-status` lists orphan domains and overlays, and the next run refuses to start while an orphan from a different run exists rather than quietly reaping it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

---

## 11. Open decisions for the owner

1. **Rootless (`qemu:///session`) or rootful (`qemu:///system`) libvirt.** Design default is
   rootless per `CLAUDE/ContainerEngines.md`; the fallback is decided by the Phase-0 triage
   report (U2), not by preference.
2. **TTL defaults.** 7 days (update) and 90 days (rebuild) are proposed, not measured. Both
   are now **backstops only** (§4.4), so the cost of getting them wrong is much lower than in
   the earlier draft where they were triggers. They live in a tracked file.
3. **How often `server-full` is actually rebuilt.** §4.1 shows its reinstall signal is
   correctly inert for a whole release, so in practice it rebuilds when `fedora_version`
   changes or when TTL-R expires. If 90 days feels too rare for the one path that proves
   Anaconda-installed Fedora Server, the lever is TTL-R, not the signal.
4. **Whether the token-bearing GitHub scenario is built at all.** It is the only way to
   discharge 00063's token criteria, and it is the only part of this design that puts a real
   credential in a guest. Recommended: build it, keep it off the bridge, require a dedicated
   throwaway account.
5. **Whether 00063 Task 3.2 ("desktop interactive `run.bash` is unchanged") is re-scoped.**
   It is not VM-provable as written. The lab proves the desktop *profile* provisions; it
   cannot prove an interactive prompt experience.
6. **Whether the synthetic-fleet scenario for 00079 is worth building** given that it proves
   the tool's logic but not the live host's fleet labelling.
7. **Approve adding `CCY_CHILD_CLAUDE` to the CCY launcher's environment passthrough.** The
   flag lives in a tracked file that is commented out at every pushed commit, which conflicts
   with §3.4's pinned-commit rule. §8 now gives **one** recommendation rather than three
   options: add the name to the `-e` list at `claude-yolo:3048-3061` (the entrypoint already
   reads it at `entrypoint.sh:362`, and this is not the launcher *flag* 00092 declined), with
   the CCY version bump that change requires. The only decision needed is whether this plan
   may touch CCY; if not, the fallback is a test branch on the public remote carrying a
   `tested-on-test-branch` divergence.
8. **Which LUKS boot-unlock route the desktop base uses** (§5.3a). The design chooses serial
   console automation because it keeps the **partition stanza** identical to the shipped one
   (the VM kickstart is a separate, per-run-rendered file either way), with a post-install
   keyfile as the fallback and vTPM rejected as primary. It costs one recorded divergence,
   `plymouth-disabled`. U8 settles whether the
   chosen route is drivable; the owner may prefer the fallback's simplicity over the fidelity.

---

## 12. A repo defect found on the way, which is not this plan's to fix

Review confirmed §5.2's vacuous-extension-gate finding at line level, and found it is broader
than this design stated. `helpers/gnome/verify_extension.py:69-78` treats **any** non-zero
exit from `gnome-extensions info` as "no session" — so a missing `gnome-extensions` binary, a
broken D-Bus, *or a UUID that does not exist at all* lands in the same bucket, and
`extension_state.py:58-62` → `:41-46` then makes it `EXT-OK` with exit code 0.
`play-gnome-shell-extensions.yml:155` documents this as intended.

That is a `gnome`-scoped play whose only live-state assertion passes on any box without a
GNOME session, and it is **independent of this plan** — it is true on the owner's real
desktop today, not merely in a VM. It wants its own plan: either fail when
`provisioning_profile == desktop` and no session is reachable, or emit a distinct `EXT-SKIP`
marker that the play counts and reports, never `EXT-OK`.

Recording it here rather than quietly widening this plan's scope, and rather than letting it
be found a third time.
