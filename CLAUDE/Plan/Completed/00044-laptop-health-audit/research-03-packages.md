# Research 03: packages / repos / security audit

## Summary

The system is largely healthy at the package layer — `dnf check` returns clean (zero broken deps, zero stuck duplicates besides benign `gpg-pubkey` keys), no orphaned packages, no `snapd`, and only three COPRs all of which back currently-used tooling. The single significant issue is the **partial kernel 7.0.9 install from yesterday's IPU6 playbook run** (Plan 00043): `kernel-core-7.0.9` and `kernel-modules-core-7.0.9` are on disk but the matching `kernel-7.0.9`, `kernel-modules-7.0.9` and `kernel-modules-extra-7.0.9` are missing — meaning 7.0.9 cannot boot WiFi/BT/sound modules. There are **129 pending updates** including a **Critical Firefox/NSS CVE bundle and a Critical yelp CVE**, plus the kernel 7.0.6 / 7.0.9 security updates that would naturally repair the partial install if a `versionlock` on kernel 6.19.14 weren't currently pinning the system. `kernel-headers` is also drifting (stuck at 6.19.6 while running 6.19.14). One stale COPR (`phracek/PyCharm`) is still enabled despite PyCharm now being a Toolbox app.

## Broken / duplicate packages

`sudo dnf check` returns **zero output, exit 0** — no broken dependencies, no conflicts, no obsoletes, no stuck duplicates.

`rpm -qa | sort | uniq -d` reports 6 "duplicate" names: `gpg-pubkey`, `kernel`, `kernel-core`, `kernel-modules`, `kernel-modules-core`, `kernel-modules-extra`. These are **not** broken duplicates — `gpg-pubkey` legitimately has 17 entries (one per signing key), and the `kernel*` names are repeated because three kernel versions are installed in parallel (intentional, Fedora retains 3 by default). No multilib clashes or stuck pairs.

`package-cleanup` is not installed (no `yum-utils` / `dnf-utils`) so `--orphans` / `--dupes` were skipped; the `dnf check` and `dnf repoquery --extras` results cover the same ground.

`dnf repoquery --extras` (installed packages not in any enabled repo) returned 0 packages — every installed RPM still has a backing repo. Clean.

## Kernel package state

Running: **`6.19.14-200.fc43`** (the versionlock'd kernel). `/lib/modules` contains all three: `6.19.14-200`, `7.0.4-100`, `7.0.9-104`.

| Subpackage             | 6.19.14-200 | 7.0.4-100 | 7.0.9-104      |
| ---------------------- | ----------- | --------- | -------------- |
| `kernel`               | ✅          | ✅        | ❌ **MISSING** |
| `kernel-core`          | ✅          | ✅        | ✅             |
| `kernel-modules`       | ✅          | ✅        | ❌ **MISSING** |
| `kernel-modules-core`  | ✅          | ✅        | ✅             |
| `kernel-modules-extra` | ✅          | ✅        | ❌ **MISSING** |

This is exactly the Plan 00043 fallout: the akmod-intel-ipu6 install dragged the 7.0.9 `kernel-core` + `kernel-modules-core` in to give `akmods` something to build against, but `kernel`, `kernel-modules` and `kernel-modules-extra` never landed → WiFi (iwlwifi), Bluetooth (btusb) and most hardware drivers are absent for 7.0.9. Booting 7.0.9 would brick networking.

**`kernel-headers` drift:** installed at **6.19.6-200.fc43** while the running kernel is **6.19.14-200.fc43**, and an update is pending to **7.0.6-100.fc43** (which would obsolete the 6.19.6 build per `dnf check-update` lines 134-135). Anything compiling against `linux/uapi` headers (DKMS modules, BPF programs, kernel-version-specific Go/Rust code) is using mismatched headers right now.

**akmod / kmod inventory:**

```
akmod-intel-ipu6-0.0-24.20250909git4bb5b4d.fc43      ← built for 7.0.9 only
akmod-v4l2loopback-0.15.3-1.fc43                     ← built for 7.0.9 only
akmods-0.6.2-9.fc43                                  ← framework
kmod-intel-ipu6-7.0.9-104.fc43.x86_64-0.0-24...      ← orphan: target kernel half-installed
kmod-v4l2loopback-7.0.9-104.fc43.x86_64-0.15.3-1...  ← orphan: target kernel half-installed
kmod-34.2-2.fc43 / kmod-libs-34.2-2.fc43             ← unrelated, userspace kmod tool
```

Both built kmod artefacts target `7.0.9-104` only — there is no `kmod-intel-ipu6-6.19.14-...` or `kmod-v4l2loopback-6.19.14-...` present, so on the actually-running kernel **both extensions are inert**. v4l2loopback is needed for any loopback-camera workflow; intel-ipu6 is needed for the MIPI webcam.

## Security advisories

`dnf advisory list --security` lists **45 advisory rows** covering **8 distinct advisory IDs**:

| Severity  | Count (rows) | Advisories                                                                                                                                                                                        |
| --------- | -----------: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Critical  |           11 | FEDORA-2026-7c3b91a2bc (yelp), FEDORA-2026-cd20332935 (firefox+nspr+nss bundle), FEDORA-2026-d29bd1ad07 (evince)                                                                                  |
| Important |           30 | FEDORA-2026-3f85a4eba7 (kernel 7.0.9), FEDORA-2026-cccb681166 (kernel 7.0.6), FEDORA-2026-599dafe4ae (python3-click), FEDORA-2026-6384a3cf14 (dnsmasq), FEDORA-2026-dfde5fc92a (freerdp/libwinpr) |
| Moderate  |            2 | FEDORA-2026-89f45c355d (expat), FEDORA-2026-a8100094df (uv)                                                                                                                                       |
| Low       |            2 | FEDORA-2026-f1f87b465a (SDL2_image)                                                                                                                                                               |
| None      |            1 | FEDORA-2026-d4d8ae2bdc (rsync)                                                                                                                                                                    |

**Standouts**: the **Firefox 151 / NSS 3.123.1 Critical bundle** is the most urgent — browser-facing remote code execution territory. **yelp 49.1 Critical** is a recent GNOME help-viewer CVE chain. **evince 48.1 Critical** is PDF parsing. All three are sitting in pending updates, blocked only by the user actually running `dnf upgrade`.

## Pending updates

`dnf -q check-update` returned **exit 100** with **129 packages** ready to upgrade. Notable groups:

- **Kernel chain** (would resolve the 7.0.9 partial install): `kernel-headers 7.0.6` (obsoletes 6.19.6), `kernel-modules 7.0.9`, `kernel-modules-extra 7.0.9`, `kernel-tools 7.0.9`, `kernel-tools-libs 7.0.9`, `python3-perf 7.0.9`. **Note**: the `kernel` versionlock at 6.19.14 will block these from installing while in place — `dnf upgrade` will print conflicts.
- **Critical security**: firefox 151, firefox-langpacks, nspr 4.38.2, nss/nss-softokn/nss-tools/nss-util 3.123.1, yelp 49.1, yelp-libs, evince-djvu/libs 48.1.
- **Bulk no-op-ish**: 36× kf6-\* @ 6.26.0 (KDE Frameworks 6 bump — large package set), 19× libreoffice-\* @ 25.8.7.3.
- **3rd-party**: brave 1.90.122, chrome 148, vivaldi 7.9, code 1.120.0, docker-ce 29.5.0, cloudflare-warp.
- **Multimedia**: ffmpeg-free 7.1.4 + all the libav\*-free siblings.

No package is being "held back" beyond the versionlock'd kernel. No broken `dnf upgrade` plan — exit was 100 (= updates available), not 1 (= error).

## Repo state

20 enabled repos. All are intentional:

| Repo                                                         | Assessment                                                                                                                                                                                             |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `fedora`, `updates`, `fedora-cisco-openh264`                 | Core Fedora. Required.                                                                                                                                                                                 |
| `rpmfusion-{free,nonfree,*-updates,*-nvidia-driver,*-steam}` | Standard RPM Fusion set. Required for multimedia and (latent) NVIDIA / Steam. NVIDIA + Steam repos are enabled but no NVIDIA driver or Steam package is installed — they cost only a metadata refresh. |
| `brave-browser`, `google-chrome`, `vivaldi`, `code`          | Vendor browser/IDE repos for installed apps. All in use.                                                                                                                                               |
| `docker-ce-stable`                                           | In use (DDEV stack).                                                                                                                                                                                   |
| `gh-cli`                                                     | In use.                                                                                                                                                                                                |
| `cloudflare-warp-stable`                                     | `cloudflare-warp` is installed and has a pending update — repo is in use.                                                                                                                              |
| `ddev`                                                       | In use.                                                                                                                                                                                                |
| `copr:ganto/lxc4`                                            | Backs LXC tooling. In use (Plan 00033 / LXC role).                                                                                                                                                     |
| `copr:pgdev/ghostty`                                         | Backs ghostty terminal. In use.                                                                                                                                                                        |
| `copr:phracek/PyCharm`                                       | **Stale** — PyCharm is now JetBrains Toolbox-managed; no `pycharm-*` RPM is installed. The COPR can be removed (`dnf copr disable phracek/PyCharm`).                                                   |

`/etc/yum.repos.d/_copr*` matches the three COPRs above — no orphaned `_copr` files.

## Flatpaks / containerised apps

12-row `flatpak list` output. **No duplicates with RPM-installed apps** — the only user-facing flatpak is `com.slack.Slack 4.49.89` (flathub). The rest are SDK runtimes, codec extensions, and the `org.chromium.Chromium` placeholder (which appears to be an empty Fedora-provided ref with no actual Chromium installed via flatpak — Brave/Chrome/Vivaldi are the actual browsers, via RPM).

`org.freedesktop.Platform.GL.default 26.0.5` appears twice — this is benign branch overlap (i386 + x86_64 or two arches of the GL runtime), not an orphan. `org.fedoraproject.Platform 44` runtime is one major behind the host Fedora 43 → 44 — also benign, fedora runtime ahead of host. No abandoned runtimes that need pruning.

**Snap:** not installed (`rpm -q snapd` → "not installed", `which snap` → empty). Good — staying off snap on Fedora.

## dnf history depth

83 transactions on record. Of the last 30 days (since 2026-04-22):

- 11× `dnf -y upgrade` (regular system upgrades).
- ~16× `ansible dnf5 module` calls (playbook-driven, expected).
- Final two entries (IDs 82, 83) on **2026-05-21 18:24** are the suspect ones: `dnf -y install --nogpgcheck --disabler...` (truncated by table width) — likely the IPU6 playbook installing akmod/kmod packages with `disable_gpg_check`. These are the transactions that pulled in the partial 7.0.9 kernel set via dependency.
- Transaction 81 (`ansible dnf5 module`, **2026-05-21 18:21**, 71 packages altered) is the IPU6 main install — worth `sudo dnf history info 81` to see exactly which packages came in as `dependency` reason.

No surprise per-package installs by `--Reason: dependency--` are evident at the summary level; details require `dnf history info` per id.

## Versionlock

```
Package name: kernel
evr = 6.19.14-200.fc43
```

One lock, on the **running** kernel — added 2026-05-10 22:58. This is doing two jobs: (1) preventing autoreboot into untested kernels, (2) currently masking the broken 7.0.9 from upgrade. **The lock must be released as part of the Plan 00043 Path A recovery** before `dnf upgrade` can complete the 7.0.9 install (or before downgrade/remove of 7.0.9 can be attempted).

## Recommended actions

Ranked by risk × ease:

1. **Resolve the partial kernel 7.0.9 install** — covered by Plan 00043. Either complete the install (`dnf versionlock delete kernel; dnf upgrade kernel kernel-modules kernel-modules-extra kernel-headers`) or remove the half-installed 7.0.9 (`dnf remove kernel-core-7.0.9-104 kernel-modules-core-7.0.9-104`). Either path also un-orphans the two `kmod-*-7.0.9-...` packages.
2. **Apply Critical security updates** — at minimum `firefox`, `nss*`, `yelp*`, `evince-*`. These are browser/help/PDF RCE classes and shouldn't wait. Can be done independently of the kernel decision via `dnf upgrade --exclude='kernel*'`.
3. **Disable stale `copr:phracek/PyCharm`** — PyCharm is Toolbox-managed now. `dnf copr disable phracek/PyCharm` then `rm /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:phracek:PyCharm.repo`. Zero risk.
4. **Decide on the `kernel` versionlock** — either keep it pinned to a specific tested version (and explicitly bump when ready) or release it. Leaving it pinned to 6.19.14 silently while 7.0.x is the supported stream defers all kernel security updates.
5. **Apply remaining updates** — 129 packages, dominated by KF6 6.26.0 (36 pkgs) and LibreOffice 25.8.7.3 (19 pkgs). After the security bundle, a plain `dnf upgrade` clears it.
6. **Install `dnf-utils` / `dnf-plugins-core`** — to get `package-cleanup`, `dnf repoquery --leaves`, and richer history queries for future audits. Optional but useful.

## Out-of-scope

- Per-package security-CVE deep dive (CVE IDs not enumerated — `dnf advisory info <id>` would be needed per advisory).
- File-level orphan scan (no `rpm -Vva` / `package-cleanup --orphans` was run because the latter requires `dnf-utils`).
- Comparing installed package set against playbook intent (= what *should* be installed per the Ansible inventory). That's a different audit.
- Firmware (`fwupd`) updates — not a `dnf` concern, separate flow.
- Flatpak update check (`flatpak remote-ls --updates`) — Slack/SDK runtimes likely have updates but not in scope here.
- Toolbox / podman image hygiene — separate research.
