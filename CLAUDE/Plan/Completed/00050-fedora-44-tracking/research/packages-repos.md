# Packages, Repositories & DNF — Fedora 44 Migration Research (Plan 00050)

Fedora 44 was released on **28 April 2026** (two weeks late), shipping **kernel 6.19.14**, **GNOME 50**, **KDE Plasma 6.6**, and — most relevant to this dimension — the **completed DNF4 → DNF5 migration**: PackageKit now uses the new libdnf5 backend and the system-upgrade tool is built directly into DNF5 (no separate plugin) ([Fedora Magazine](https://fedoramagazine.org/announcing-fedora-linux-44/), [OSTechNix](https://ostechnix.com/fedora-linux-44-released/), [9to5Linux](https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new), [F44 ChangeSet](https://fedoraproject.org/wiki/Releases/44/ChangeSet)). Third-party repo availability for F44 as of 2026-06-12: RPM Fusion **published** (with an early release-package bug), Docker CE **published**, NVIDIA CUDA **published (new GPG key)**, DisplayLink **published**, ganto/lxc4 COPR **published**, pgdev/ghostty COPR **NOT published**. Details and citations per finding below. This document is research only — no changes have been made.

## PKG-01: dnf5 rejects config-manager --enable in play-rpm-fusion

**Severity**: high
**Area**: repos / dnf5
**Files**: `playbooks/imports/play-rpm-fusion.yml:13`

**Concern**: Line 13 runs `dnf -y config-manager --enable fedora-cisco-openh264`. DNF5's config-manager plugin only supports the `addrepo` / `setopt` / `unsetopt` subcommands — the DNF4 `--enable` / `--set-enabled` flags are not implemented and the upstream request to add them remains open ([dnf5 config-manager docs](https://dnf5.readthedocs.io/en/latest/dnf5_plugins/config-manager.8.html), [dnf5 issue #1840](https://github.com/rpm-software-management/dnf5/issues/1840)). On Fedora 44, where the DNF5 migration is complete and `/usr/bin/dnf` is unconditionally dnf5, this command exits non-zero; combined with `set -euo pipefail` the whole shell block fails, halting `play-rpm-fusion.yml` — which is a core import in `playbooks/playbook-main.yml:17`, so the entire main deploy flow stops. (This is strictly already true on the F43 baseline, which also defaults to dnf5 — it must be fixed as part of, or before, the F44 work.) The `fedora-cisco-openh264` repo itself still exists in F44; no accepted F44 change removes it ([Fedora OpenH264 wiki](https://fedoraproject.org/wiki/OpenH264)).

**Recommendation**: When implementing the F44 migration, replace line 13 with the DNF5 syntax `dnf -y config-manager setopt fedora-cisco-openh264.enabled=1`. Verify the rest of the block under dnf5 (the `@core` / `group install multimedia` group specs are supported by dnf5 and need no change).

## PKG-02: Ghostty COPR pgdev/ghostty has no Fedora 44 builds

**Severity**: high
**Area**: repos / copr
**Files**: `playbooks/imports/play-terminal-emulators.yml:29-36` (`dnf copr enable -y pgdev/ghostty`, then `package: ghostty`); imported by `playbooks/playbook-main.yml:40`

**Concern**: As of June 2026 the `pgdev/ghostty` Copr publishes no `fedora-44-x86_64` chroot — this is a known upstream gap that blocks Fedora 44 upgrades for its users ([ghostty discussion #12586](https://github.com/ghostty-org/ghostty/discussions/12586), [Fedora Discussion](https://discussion.fedoraproject.org/t/silverblue-atomic-44-ghostty-updgrade/190468), [pgdev/ghostty Copr](https://copr.fedorainfracloud.org/coprs/pgdev/ghostty), [results dir listing](https://download.copr.fedorainfracloud.org/results/pgdev/ghostty/) shows no fedora-44 x86_64 chroot). On F44, `dnf copr enable -y pgdev/ghostty` fails (the project does not support the OS release), so the play — part of the core `playbook-main.yml` sequence — halts; even if the repo were force-enabled, no F44 `ghostty` package exists in it.

**Recommendation**: Before the F44 migration, pick a replacement source for Ghostty: the `alternateved/ghostty` Copr (tracks pgdev and supports newer releases, per [ghostty discussion #12586](https://github.com/ghostty-org/ghostty/discussions/12586)) or the official install route in [ghostty.org binary docs](https://ghostty.org/docs/install/binary#fedora). Update both the enable command and the `creates:` repo-file path in `play-terminal-emulators.yml:31-32`, and add the old `pgdev/ghostty` Copr to the orphan-removal loop in `playbooks/imports/play-ZZ-repo-cleanup.yml:30-40`. Re-check the Copr's F44 status at migration time — if pgdev has published F44 chroots by then, a no-op is acceptable.

## PKG-03: NVIDIA CUDA fedora44 repo exists but the GPG key file changed

**Severity**: medium
**Area**: repos / nvidia
**Files**: `playbooks/imports/optional/hardware-specific/play-nvidia.yml:78-87` (notably line 82 `baseurl` and line 84 `gpgkey: .../fedora{{ fedora_version }}/x86_64/1940C73E.pub`)

**Concern**: NVIDIA now publishes a `fedora44` CUDA repo (CUDA 13.3, driver 610.x) — it lagged the F44 release by several weeks (still absent in May 2026 per the [NVIDIA forum thread](https://forums.developer.nvidia.com/t/cuda-driver-for-fedora-44/368844)) but exists today ([repos index](https://developer.download.nvidia.com/compute/cuda/repos/), [fedora44/x86_64 listing](https://developer.download.nvidia.com/compute/cuda/repos/fedora44/x86_64/)). However the directory ships a **different public key file**: `73CD9B30.pub`. The playbook's hard-coded `1940C73E.pub` returns **HTTP 404** under `fedora44/x86_64/`, so after bumping `fedora_version` to 44 the `yum_repository` task writes a repo whose gpgkey URL is dead — the first `dnf` metadata/GPG import for `cuda-toolkit` (line 89-93) then fails.

**Recommendation**: When bumping to F44, change the `gpgkey` filename for the fedora44 repo to `73CD9B30.pub` (verify the fingerprint against NVIDIA's [CUDA installation guide](https://docs.nvidia.com/cuda/cuda-installation-guide-linux/index.html) at the time). Consider deriving the key URL from the repo's published `.repo` file rather than hard-coding the filename, and re-verify the driver-exclusion list (line 86) still matches the 610.x package names.

## PKG-04: RPM Fusion F44 release packages shipped, but early builds enabled Rawhide

**Severity**: low
**Area**: repos / rpmfusion
**Files**: `playbooks/imports/play-rpm-fusion.yml:11-12` (`$(rpm -E %fedora)` URLs — self-adapting); `playbooks/imports/optional/hardware-specific/play-nvidia.yml:27-30` (`fedora_version`-templated URLs, `disable_gpg_check: true`)

**Concern**: `rpmfusion-free-release-44.noarch.rpm` / `rpmfusion-nonfree-release-44.noarch.rpm` are published, so both install paths work once `fedora_version` is 44. However the initial F44 release packages (44-0.2) had a packaging bug that enabled the **Rawhide** repos and left the proper F44 repos disabled, causing depsolve chaos; this was corrected in release 44-3 ([Fedora Discussion thread](https://discussion.fedoraproject.org/t/fedora-44-and-rpmfusion/189911), [rpmfind F44 updates listing](https://rpmfind.net/linux/RPM/rpmfusion/free/fedora/updates/44/x86_64/index.html)). RPM Fusion also habitually lags Fedora GA by a few days — a same-day F44 install can 404. Note `disable_gpg_check: true` on the nvidia-play variant means a lagging/mis-published release RPM would install without signature verification — unchanged risk, but worth re-checking while touching these lines.

**Recommendation**: No structural change needed. At migration time, verify the installed `rpmfusion-*-release` is ≥ 44-3 and that `dnf repolist --enabled` shows `rpmfusion-free`/`-nonfree` for F44 (not rawhide). Consider a post-install assertion in the play that the enabled RPM Fusion repos match `$releasever`.

## PKG-05: Docker CE repo publishes Fedora 44 (historical laggard — verify at upgrade time)

**Severity**: low
**Area**: repos / docker
**Files**: `playbooks/imports/play-docker.yml:86-94` (adds `docker-ce.repo`, `$releasever`-keyed), `play-docker.yml:96-104` (installs docker-ce et al.); imported by `playbooks/playbook-main.yml:20`

**Concern**: `download.docker.com/linux/fedora/` now contains a `44/` tree, so the `$releasever`-based `docker-ce.repo` resolves on F44 ([Docker index](https://download.docker.com/linux/fedora/), [Docker install docs](https://docs.docker.com/engine/install/fedora/)). Docker has historically lagged new Fedora releases (users had to file issues such as [docker/for-linux #1560](https://github.com/docker/for-linux/issues/1560) requesting the F44 repo), which would have made `Install Docker` fail with empty metadata in the gap window. That window has closed.

**Recommendation**: No change required for F44. For future release migrations, check `https://download.docker.com/linux/fedora/<ver>/` exists before bumping `fedora_version`, since play-docker is in the core main flow.

## PKG-06: DisplayLink fedora-44 RPM is published — works after the version bump

**Severity**: low
**Area**: repos / displaylink
**Files**: `playbooks/imports/optional/hardware-specific/play-displaylink.yml:11-12` (pins `displaylink_version: v6.2.0-1`, `evdi_version: 1.14.16`), `play-displaylink.yml:127` (URL templated on `fedora_version`)

**Concern**: The latest displaylink-rpm release (v6.2.0-1, May 2026) ships `fedora-44-displaylink-1.14.16-1.github_evdi.x86_64.rpm` (confirmed via the GitHub releases API for [displaylink-rpm releases](https://github.com/displaylink-rpm/displaylink-rpm/releases/latest)). So the URL on line 127 resolves once `fedora_version: 44` is set — no availability gap today. The general pattern remains fragile: each Fedora bump silently depends on upstream having built a per-release asset, and the play installs with `disable_gpg_check: true` (line 128).

**Recommendation**: No availability action needed for F44 — just bump `fedora_version` and re-run; the play's built-in latest-version check (lines 51-101) will validate. Keep the per-release asset dependency in mind for F45.

## PKG-07: ganto/lxc4 COPR has a fedora-44-x86_64 chroot

**Severity**: low
**Area**: repos / copr / lxc
**Files**: `playbooks/imports/play-lxc-install-config.yml:47-55` (`community.general.copr: ganto/lxc4`, then installs `lxc`, `lxc-templates`); imported by `playbooks/playbook-main.yml:27`

**Concern**: The `ganto/lxc4` Copr already enables `fedora-44-x86_64` (alongside 42/43/rawhide) per the [Copr API project data](https://copr.fedorainfracloud.org/api_3/project?ownername=ganto&projectname=lxc4), so the repo enable will not fail on F44. Residual verification: (a) confirm the F44 chroot actually contains **succeeded** `lxc`/`lxc-templates` builds, not just an enabled chroot; (b) `community.general.copr` interacts with DNF Python bindings — it works on the dnf5-only F43 baseline today, but re-verify after the F44 bump in case the collection's dnf5 support shifts.

**Recommendation**: At migration time, check `https://download.copr.fedorainfracloud.org/results/ganto/lxc4/fedora-44-x86_64/` has current builds, then run the play unchanged. No repo edits expected.

## PKG-08: Versionlock straddles dnf4 plugin and dnf5 builtin

**Severity**: low
**Area**: dnf / versionlock
**Files**: `playbooks/imports/optional/common/play-advanced-kernel-management.yml:11-13` (installs `python3-dnf-plugin-versionlock`), `:92-93` (`dnf versionlock list`); `files/usr/local/bin/manage-kernel-versions.py:178,200,219,277-279` (drives `dnf versionlock` and tells users to install the dnf4 plugin); `playbooks/imports/play-AB-dnf-upgrade.yml:10-15` (relies on versionlocks being honoured)

**Concern**: In DNF5, `versionlock` is part of the main application and stores locks in `/etc/dnf/versionlock.toml` ([dnf5 versionlock docs](https://dnf5.readthedocs.io/en/latest/commands/versionlock.8.html)); `python3-dnf-plugin-versionlock` is the DNF4 plugin using `/etc/dnf/plugins/versionlock.list` ([dnf-plugins-core docs](https://dnf-plugins-core.readthedocs.io/en/latest/versionlock.html)). On the F44 (DNF5-complete) system the installed dnf4 plugin is inert dead weight: the `dnf versionlock` CLI calls all hit the dnf5 builtin and work, but the package install task buys nothing, and any historical locks left in the dnf4 `versionlock.list` are **not honoured by dnf5** — exactly the kind of silent decoupling the fail-fast rule exists to prevent. (Related: `community.general.dnf_versionlock` is dnf5-incompatible, [community.general #10237](https://github.com/ansible-collections/community.general/issues/10237) — the repo correctly avoids it.)

**Recommendation**: During the F44 pass, drop the `python3-dnf-plugin-versionlock` install task (and the corresponding hint at `manage-kernel-versions.py:279`), and add a one-off check that no stale dnf4 `versionlock.list` entries exist without a matching `/etc/dnf/versionlock.toml` entry on upgraded hosts.

## PKG-09: VirtualBox repo has a 44 directory — verify it is populated

**Severity**: low
**Area**: repos / virtualbox
**Files**: `playbooks/imports/optional/experimental/play-virtualbox-windows.yml:20-24` (installs Oracle's `$releasever`-keyed `virtualbox.repo`)

**Concern**: Oracle's `download.virtualbox.org/virtualbox/rpm/fedora/` index now lists a `44/` directory ([index](https://download.virtualbox.org/virtualbox/rpm/fedora/)), so the `$releasever` expansion resolves on F44. Oracle has historically lagged new Fedora releases and sometimes creates the directory before populating `x86_64` packages, which yields empty-metadata failures rather than 404s. This play is experimental/optional, so impact is contained.

**Recommendation**: Before first F44 use, confirm `https://download.virtualbox.org/virtualbox/rpm/fedora/44/x86_64/` contains `VirtualBox-7.x` packages; if Oracle lags, hold this optional play back rather than working around it.

## PKG-10: darktable mock build chroot must move to fedora-44-x86_64

**Severity**: low
**Area**: packages / mock build
**Files**: `playbooks/imports/optional/common/play-darktable-ai-build.yml:54` (`.fc{{ fedora_version }}` release suffix), `:75` (`mock_chroot: fedora-{{ fedora_version }}-x86_64`), `:91-93` (asserts host major version equals `fedora_version`)

**Concern**: Both the dist tag and the mock chroot are correctly templated on `fedora_version`, so the bump to 44 flows through automatically — but the build then depends on the host's `mock-core-configs` package shipping a `fedora-44-x86_64.cfg`. That config exists once the F44-era `mock-core-configs` update lands (it follows each Fedora release), so a host that has run `play-AB-dnf-upgrade.yml` on F44 will have it. The line 91 assertion already fail-fasts a mismatched host. No action beyond the central version bump; the rebuilt RPM gets `.fc44` and continues to outrank stock per the comment at lines 51-52.

**Recommendation**: After bumping `fedora_version`, simply rebuild; if mock reports an unknown chroot, upgrade `mock-core-configs` via the normal dnf-upgrade play first. No playbook edit anticipated.

## PKG-11: Version-agnostic third-party repos need no F44 change

**Severity**: info
**Area**: repos
**Files**: `playbooks/imports/play-vscode.yml:15` (packages.microsoft.com/yumrepos/vscode); `playbooks/imports/play-browsers.yml:39-48` (Chrome RPM + key), `:58-65` (Brave), `:80-87` (Vivaldi); `playbooks/imports/play-git-configure-and-tools.yml:97-99` (gh-cli repo); `playbooks/imports/optional/common/play-ddev.yml:82-92` (pkg.ddev.com, `gpgcheck=0`); `playbooks/imports/optional/common/play-cloudflare-warp.yml:10-16`; `playbooks/imports/play-ms-fonts.yml:20`

**Concern**: None of these repos key on `$releasever` or `fedora_version` — Cloudflare WARP's repo file, for example, uses a flat `https://pkg.cloudflareclient.com/rpm` baseurl ([repo file](https://pkg.cloudflareclient.com/cloudflare-warp-ascii.repo)), and VS Code, Brave, Vivaldi, Chrome, gh-cli and DDEV all serve a single stream for all Fedora/RHEL versions. F44 availability is therefore automatic; the only residual exposure is binary compatibility with F44's glibc 2.43 ([F44 ChangeSet](https://fedoraproject.org/wiki/Releases/44/ChangeSet)), which vendors handle upstream. DDEV's `gpgcheck=0` and the ms-fonts `rpm --nodigest -i` install are pre-existing trust decisions whose risk is unchanged by F44.

**Recommendation**: No migration action. Forward-looking watch-item only: after the first F44 deploy, spot-check `warp-cli`, `code`, `brave-browser`, `vivaldi-stable`, `gh` and `ddev` all install and run.

## PKG-12: DNF5 completion in F44 is compatible with the repo's Ansible toolchain

**Severity**: info
**Area**: dnf / ansible
**Files**: `run.bash:569-574` and `fedora-install/ks.cfg:492-496` (bootstrap installs `python3-libdnf5`); `playbooks/imports/play-basic-configs.yml:78` (keeps `python3-libdnf5` present); `ansible.builtin.dnf` usage throughout `playbooks/imports/**`

**Concern**: Fedora 44 finishes the DNF5 story — PackageKit switches to the libdnf5 backend and `dnf system-upgrade` is built in ([F44 ChangeSet](https://fedoraproject.org/wiki/Releases/44/ChangeSet), [Fedora Magazine](https://fedoramagazine.org/announcing-fedora-linux-44/)) — but this does not change anything for the repo: Fedora has defaulted to dnf5 since F41, the F43 baseline already runs the whole playbook set against dnf5, and Ansible's dnf/dnf5 path requires only `python3-libdnf5` ([ansible.builtin.dnf5 docs](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/dnf5_module.html), [ansible #84206](https://github.com/ansible/ansible/issues/84206)), which this repo installs at all three layers (kickstart, run.bash bootstrap, play-basic-configs). The genuinely affected raw-CLI call is covered by PKG-01; the remaining raw `dnf` invocations (`copr enable`, `config-manager addrepo`, `group install`, `install`) already use dnf5-valid syntax.

**Recommendation**: No action. Keep `python3-libdnf5` in the bootstrap path and prefer dnf5-native syntax in any new raw `dnf` shell blocks.

## Sources

- https://fedoramagazine.org/announcing-fedora-linux-44/
- https://ostechnix.com/fedora-linux-44-released/
- https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new
- https://itsfoss.com/news/fedora-44-release/
- https://fedoraproject.org/wiki/Releases/44/ChangeSet
- https://dnf5.readthedocs.io/en/latest/dnf5_plugins/config-manager.8.html
- https://github.com/rpm-software-management/dnf5/issues/1840
- https://fedoraproject.org/wiki/OpenH264
- https://discussion.fedoraproject.org/t/fedora-44-and-rpmfusion/189911
- https://rpmfind.net/linux/RPM/rpmfusion/free/fedora/updates/44/x86_64/index.html
- https://github.com/ghostty-org/ghostty/discussions/12586
- https://discussion.fedoraproject.org/t/silverblue-atomic-44-ghostty-updgrade/190468
- https://copr.fedorainfracloud.org/coprs/pgdev/ghostty
- https://download.copr.fedorainfracloud.org/results/pgdev/ghostty/
- https://ghostty.org/docs/install/binary#fedora
- https://developer.download.nvidia.com/compute/cuda/repos/
- https://developer.download.nvidia.com/compute/cuda/repos/fedora44/x86_64/
- https://forums.developer.nvidia.com/t/cuda-driver-for-fedora-44/368844
- https://docs.nvidia.com/cuda/cuda-installation-guide-linux/index.html
- https://download.docker.com/linux/fedora/
- https://docs.docker.com/engine/install/fedora/
- https://github.com/docker/for-linux/issues/1560
- https://github.com/displaylink-rpm/displaylink-rpm/releases/latest
- https://copr.fedorainfracloud.org/api_3/project?ownername=ganto&projectname=lxc4
- https://dnf5.readthedocs.io/en/latest/commands/versionlock.8.html
- https://dnf-plugins-core.readthedocs.io/en/latest/versionlock.html
- https://github.com/ansible-collections/community.general/issues/10237
- https://download.virtualbox.org/virtualbox/rpm/fedora/
- https://pkg.cloudflareclient.com/cloudflare-warp-ascii.repo
- https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/dnf5_module.html
- https://github.com/ansible/ansible/issues/84206
