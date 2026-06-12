# Install & Bootstrap Path — Fedora 44 Migration Research (Plan 00050)

This dimension covers the kickstart/netinstall install path (`fedora-install/`), the bootstrap entry point (`run.bash`), the early DNF upgrade play, and container base images. Confirmed Fedora 44 baseline (live web research, 2026-06-12): **Fedora 44 was released on 2026-04-28** with GA compose **44-1.7**, shipping **GNOME 50**, **kernel 6.19.x at GA** (now rolled forward to 7.0.x via updates), and **Python 3.14** (sources: <https://fedoramagazine.org/announcing-fedora-linux-44/>, <https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new>). The F44 release ISOs are confirmed live on mirrors as `Fedora-Everything-netinst-x86_64-44-1.7.iso` and `Fedora-Workstation-Live-44-1.7.x86_64.iso` under `releases/44/{Everything,Workstation}/x86_64/iso/` (verified directly against <https://mirrors.kernel.org/fedora/releases/44/Everything/x86_64/iso/> and <https://mirrors.kernel.org/fedora/releases/44/Workstation/x86_64/iso/>). Install-relevant accepted F44 Changes: "Stop Creating Default Network Profiles By Anaconda" and "Unification of boot loader updates, phase 1" (<https://fedoraproject.org/wiki/Releases/44/ChangeSet>). Live media remains an EROFS image still named `LiveOS/squashfs.img` (change landed in F42: <https://fedoraproject.org/wiki/Changes/EROFSforLiveMedia>).

## BOOT-01: fedora_version 43 pin gates the whole bootstrap and hard-fails on F44

**Severity**: high
**Area**: install
**Files**: `vars/fedora-version.yml:5` (`fedora_version: 43`); consumed by `fedora-install/setup-netinstall-boot.bash:69-79` (`get_version_from_file`) and `:1176` (`TARGET_VERSION`); enforced by `run.bash:94` (live `VERSION_ID` detection) and `run.bash:809-823` (hard fail on mismatch); referenced by `playbooks/imports/play-AB-dnf-upgrade.yml:33-34`.

**Concern**: Every install/bootstrap flow keys off this single pin. With `fedora_version: 43`, `setup-netinstall-boot.bash` discovers and downloads **Fedora 43** ISOs (so the netinstall boot entry would reinstall F43, not F44), and `run.bash` on an upgraded F44 host exits with "Fedora version mismatch" at `run.bash:816-821`, blocking the entire deploy flow. Additionally, `run.bash:1412-1415` will flag all playbooks as untested for Fedora 44 until the per-version test facts are regenerated — expected first-run behaviour worth anticipating.

**Recommendation**: When cutting the F44 branch, bump `vars/fedora-version.yml` to `44` as the first commit (the file's own header says to do exactly this). No other code change is needed in the version plumbing itself — both consumers read the value dynamically.

## BOOT-02: ks.cfg hardcodes F43 in version marker and SETUP_BRANCH with no substitution on the build-iso path

**Severity**: medium
**Area**: install
**Files**: `fedora-install/ks.cfg:1` (`#version=F43`), `:556` (`SETUP_BRANCH="F43"` in chrooted %post pre-clone), `:597` (`SETUP_BRANCH="F43"` inside the generated autostart script), `:410` (`liveimg --url=...squashfs-FEDORA_VERSION.img` placeholder); substitution logic in `fedora-install/setup-netinstall-boot.bash:874-875`; unsubstituted path in `fedora-install/build-iso.bash:59` (`mkksiso --ks "$KS_FILE"` uses the raw repo ks.cfg).

**Concern**: The netinstall path is self-healing: `setup-netinstall-boot.bash:874` rewrites every `^SETUP_BRANCH=` line to `F${target_version}` and `:875` substitutes the squashfs placeholder, so once BOOT-01 is fixed the deployed ks.cfg targets F44. But the **build-iso.bash USB route embeds ks.cfg verbatim** — an ISO built from the F44 branch with stale in-repo values would clone the `F43` branch on a freshly installed F44 system (and the `squashfs-FEDORA_VERSION.img` placeholder is never substituted on that route at all — a pre-existing gap that becomes visible during migration). The `#version=F43` marker at line 1 is cosmetic but misleading.

**Recommendation**: On the F44 branch, update `ks.cfg:1` to `#version=F44` and both `SETUP_BRANCH` defaults to `"F44"`. Separately consider making `build-iso.bash` perform the same sed substitutions as `setup-netinstall-boot.bash` before calling `mkksiso`, so the USB route is no longer second-class.

## BOOT-03: F44 branch does not exist on origin so branch-per-version bootstrap cannot start

**Severity**: medium
**Area**: repos
**Files**: Remote check (`git ls-remote --heads origin`) shows only `F42` and `F43`. Dependent logic: `fedora-install/ks.cfg:564` (`git clone -b '${SETUP_BRANCH}'`), `:620` (autostart clone `-b '$SETUP_BRANCH'`); `run.bash:796` (clones the repo default branch with no `-b`), `run.bash:809-823` (fails unless branch version matches host); `docs/post-upgrade.md:53-71` (instructs `git checkout F<NEW_VERSION>`).

**Concern**: Every bootstrap entry point assumes a `F44` branch exists on GitHub once the host runs Fedora 44. The kickstart %post and autostart wrapper would fail to clone `F44`; `run.bash` clones the default branch and then hard-fails the version check if that branch still pins 43. `docs/post-upgrade.md:71` documents the intended holding pattern (stay on the old branch until the new one appears), so this is a deliberate gate — but the F44 migration is not actionable until the branch is published.

**Recommendation**: Create and push the `F44` branch (containing the BOOT-01/BOOT-02 bumps) as the first migration milestone, and update the GitHub default branch to `F44` once tested so `run.bash:796`'s bare clone lands on the right branch for fresh F44 installs.

## BOOT-04: F44 ISO discovery URLs and filename regexes verified compatible with an Anubis anti-bot caveat

**Severity**: low
**Area**: install
**Files**: `fedora-install/setup-netinstall-boot.bash:32` (`BASE_URL=https://download.fedoraproject.org/pub/fedora/linux/releases`), `:35-36` (filename prefixes), `:606-623` (`discover_netinstall_url` greps `Fedora-Everything-netinst-x86_64-44-[0-9.]+\.iso`), `:625-643` (`discover_workstation_url` greps `Fedora-Workstation-Live-44-[0-9.]+\.x86_64\.iso`).

**Concern**: Verified against a live mirror: F44 publishes `Fedora-Everything-netinst-x86_64-44-1.7.iso` and `Fedora-Workstation-Live-44-1.7.x86_64.iso` at `releases/44/Everything/x86_64/iso/` and `releases/44/Workstation/x86_64/iso/` — both regexes match unchanged, so the discovery code needs **no modification** beyond the BOOT-01 version bump. One operational caveat found during research: `dl.fedoraproject.org` now fronts directory listings with the Anubis anti-bot challenge ("Access Denied" to non-browser clients). The script scrapes a directory index via `curl -sfL` through the `download.fedoraproject.org` redirector; if the redirector ever lands on an Anubis-protected endpoint, discovery would die at `setup-netinstall-boot.bash:617-619` (a clean fail-fast, but a confusing one).

**Recommendation**: No code change required for F44. On the first F44 run of the script, confirm discovery succeeds; if Anubis interferes, switch `BASE_URL` to a plain mirror or use the Fedora mirrormanager metalink instead of HTML index scraping.

## BOOT-05: Live image is EROFS named squashfs.img already handled and needs liveimg verification on F44

**Severity**: low
**Area**: install
**Files**: `fedora-install/setup-netinstall-boot.bash:698` (extracts `LiveOS/squashfs.img` from the Workstation ISO), `:1003-1008` (verification accepts `squashfs|erofs` magic with the comment "Fedora switched Live images from squashfs to erofs"); `fedora-install/ks.cfg:410` (`liveimg --url=file:///mnt/fdinst/squashfs-FEDORA_VERSION.img`).

**Concern**: Since the F42 EROFSforLiveMedia change, the Workstation Live payload is an EROFS image that retains the `LiveOS/squashfs.img` filename — confirmed still the case for the F44 era (the image is an EROFS file, ~2.6 GB). The repo already extracts by that exact path and already accepts EROFS magic, so the F43-tested flow should carry over. Residual risk is only that a future compose renames the file (e.g. to `rootfs.img`), which would fail loudly at `setup-netinstall-boot.bash:699-703`.

**Recommendation**: No change needed for F44. During the first F44 `setup-netinstall-boot.bash` run, confirm `squashfs-44.img` extracts and verifies as EROFS, and that Anaconda's `liveimg` deploys it successfully.

## BOOT-06: F44 Anaconda stops creating default network profiles so kickstart network handling needs a first-boot check

**Severity**: low
**Area**: install
**Files**: `fedora-install/ks.cfg:382-383` (generated `network --bootproto=dhcp --device=link --activate --hostname=` fragment), `:505-536` (manual WiFi `.nmconnection` keyfile written in chrooted %post).

**Concern**: The accepted F44 Change "Stop Creating Default Network Profiles By Anaconda" means the installed system only gets NetworkManager profiles for devices explicitly configured during installation. This repo's kickstart explicitly configures one device (`--device=link`) and hand-writes the WiFi keyfile, so the primary path is unaffected. However, hosts with multiple wired NICs previously received default profiles for all of them from Anaconda; on F44 the extra NICs will rely on NetworkManager's runtime auto-profiles instead. Behaviour should be equivalent for this repo's laptop-focused use case, but it is an installer behaviour change in exactly this code path.

**Recommendation**: No code change anticipated. On the first F44 kickstart install, verify the WiFi connection and hostname survive into the installed system and that any secondary NICs still get connectivity.

## BOOT-07: Stale F43/F42 version examples in install docs and usage text

**Severity**: low
**Area**: docs
**Files**: `fedora-install/README.md:312` (example `Fedora-Everything-netinst-x86_64-43-1.1.iso`), `fedora-install/build-iso.bash:30-31` (usage examples pin 43-1.1 — and line 31's `Fedora-Workstation-Live-x86_64-43-1.1.iso` uses the *old pre-F42 naming order*, which no longer exists), `docs/post-upgrade.md:3` and `:67` (examples say "F42 → F43" / `git checkout F43`).

**Concern**: Nothing breaks — these are illustrative strings — but copy-pasting the `build-iso.bash:31` example filename will never match a real F44 (or even F43) download, and the post-upgrade guide examples will read one release stale.

**Recommendation**: As part of the F44 branch cut, refresh examples to `Fedora-Everything-netinst-x86_64-44-1.7.iso` / `Fedora-Workstation-Live-44-1.7.x86_64.iso` and "F43 → F44".

## BOOT-08: play-AB-dnf-upgrade.yml is not the release-upgrade flow and is version-agnostic and F44-safe

**Severity**: info
**Area**: upgrade
**Files**: `playbooks/imports/play-AB-dnf-upgrade.yml:28-79` (intra-release `dnf name: '*' state: latest`), `:51-53` (comment pinned to "Ansible 2.20 + Fedora 43 combinations"); the actual 43→44 release path is the manual checklist `docs/post-upgrade.md:7-12` which defers to Fedora's official `dnf system-upgrade` docs.

**Concern**: The dimension brief asked whether this play "targets 44" — it does not target any release: it contains no `releasever`, no `system-upgrade` plugin usage, and is an early intra-release package refresh. That is by design; `docs/post-upgrade.md:134-143` documents why the release upgrade itself is deliberately unscripted. On F44 the play works unchanged via the `ansible.builtin.dnf` module against dnf5. The only blemish is the stale "Fedora 43" wording in the comment at `:53`, and the kernel-versionlock interplay (`:10-15`) should be sanity-checked once F44's kernel series moves under it.

**Recommendation**: No functional change. Optionally refresh the `:51-53` comment wording on the F44 branch and re-verify the kernel half-install detection logic on the first F44 run.

## BOOT-09: CCY container images are not Fedora-version-coupled and explicitly out of scope

**Severity**: info
**Area**: containers
**Files**: `files/var/local/claude-yolo/Dockerfile:7` (`FROM rust:slim AS phpantom-builder`), `:19` (`FROM node:20-slim AS base`), `:220` (`FROM base AS full`); `.claude/ccy/Dockerfile:7-8` (`ARG BASE_IMAGE=claude-yolo:latest` / `FROM ${BASE_IMAGE}`).

**Concern**: None. The CCY image chain is Debian-based (`node:20-slim`, `rust:slim`) and the project-level CCY Dockerfile layers on `claude-yolo:latest`. No Fedora base image, no `releasever`, no Fedora repos appear anywhere in either Dockerfile. The F43→F44 migration does **not** require container image changes, and this dimension should not be over-scoped to include them.

**Recommendation**: No action for Fedora 44. (Node 20 LTS end-of-life is an unrelated maintenance track.)

## BOOT-10: Bootloader-update unification phase 1 is a forward-looking watch on the GRUB entry machinery

**Severity**: info
**Area**: bootloader
**Files**: `fedora-install/setup-netinstall-boot.bash:31` (`GRUB_ENTRY=/etc/grub.d/40_fedora_netinstall`), `:917-957` (writes the `/etc/grub.d` script, edits `/etc/default/grub`, runs `grub2-editenv` and `grub2-mkconfig -o /boot/grub2/grub.cfg`).

**Concern**: F44's accepted Change "Unification of boot loader updates, phase 1" begins moving bootloader payload updates towards a single tool (bootupd-style), decoupling RPM installs from `/boot`/`/boot/efi` writes. In F44 this does not remove the `grub2-mkconfig` + `/etc/grub.d` workflow on Workstation, so the script keeps working — but this machinery is the most exposed part of the repo if later phases change how `grub.cfg` is regenerated or where it lives. No repo breakage exists today; this is a watch item only.

**Recommendation**: No action for F44. Re-check this script's assumptions (grub2-mkconfig path, `/etc/grub.d` honouring, BLS `--unrestricted` interaction noted at `:901-903`) when F45+ phases of the unification land.

## Sources

- <https://fedoramagazine.org/announcing-fedora-linux-44/> — Fedora 44 release announcement (released 2026-04-28)
- <https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new> — F44 component versions (GNOME 50, kernel 6.19, Python 3.14)
- <https://mirrors.kernel.org/fedora/releases/44/Everything/x86_64/iso/> — live listing confirming `Fedora-Everything-netinst-x86_64-44-1.7.iso`
- <https://mirrors.kernel.org/fedora/releases/44/Workstation/x86_64/iso/> — live listing confirming `Fedora-Workstation-Live-44-1.7.x86_64.iso`
- <https://fedoraproject.org/wiki/Releases/44/ChangeSet> — accepted F44 Changes (Anaconda network profiles, bootloader unification phase 1, PackageKit-DNF5)
- <https://fedoraproject.org/wiki/Changes/EROFSforLiveMedia> — EROFS live media change (F42 onwards; file retains `squashfs.img` name)
- <https://sigwait.org/~alex/blog/2026/05/28/smdBC8.html> — 2026 confirmation that current Fedora live `LiveOS/squashfs.img` is an EROFS image
- <https://docs.fedoraproject.org/en-US/quick-docs/upgrading-fedora-offline/> — official `dnf system-upgrade` procedure deferred to by `docs/post-upgrade.md`
- <https://dl.fedoraproject.org/pub/fedora/linux/releases/44/Everything/x86_64/iso/> — observed Anubis anti-bot "Access Denied" for non-browser clients (BOOT-04 caveat)
