# Version Literals & Gating Logic — Fedora 44 Migration Research (Plan 00050)

This dimension audits every hardcoded Fedora version literal (42/43/44) and all version-gating logic that must move when this repo migrates from Fedora 43 to Fedora 44. Confirmed Fedora 44 baseline (live web research, 2026-06-12): **Fedora 44 released 28 April 2026** with **GNOME 50** and **kernel 6.19** at GA (6.19.14-300.fc44; the 7.0 line arrived via normal updates) — sources: [Fedora Magazine announcement](https://fedoramagazine.org/announcing-fedora-linux-44/), [Wikipedia Fedora release history](https://en.wikipedia.org/wiki/Fedora_Linux_release_history). Python remains at **3.14** on both F43 and F44 (no Python bump this cycle). NVIDIA already publishes a **fedora44** CUDA repo directory ([developer.download.nvidia.com/compute/cuda/repos/](https://developer.download.nvidia.com/compute/cuda/repos/)), so the variable-driven CUDA/RPM Fusion URLs in this repo will resolve once `fedora_version` is bumped. The repo currently sets `fedora_version: 43` in `vars/fedora-version.yml` and gates strictly on equality, so an F44 host hard-fails everywhere until the canonical value moves.

## VER-01: Canonical fedora_version value in vars/fedora-version.yml

**Severity**: high
**Area**: version-source
**Files**:

- `vars/fedora-version.yml:6` (`fedora_version: 43` — the single source of truth)
- Consumers that read it correctly: `playbooks/imports/play-AA-preflight-sanity.yml:25-26`, `run.bash:815-822`, `scripts/setup.bash:46-55`, `scripts/setup-rclone.bash:137-140`, `fedora-install/setup-netinstall-boot.bash:69-79`, `playbooks/imports/optional/hardware-specific/play-nvidia.yml:27-28,80-84`, `playbooks/imports/optional/hardware-specific/play-displaylink.yml:127`, `playbooks/imports/optional/common/play-darktable-ai-build.yml:54,75,91`

**Concern**: Every gate is a strict equality check (`distribution_major_version | int == fedora_version | int` in play-AA-preflight-sanity.yml:25; string compare in run.bash:816 and setup.bash:47). On a Fedora 44 host, the F43 branch refuses to run anything: `run.bash` exits at line 821, the preflight playbook asserts at line 25, and both setup scripts trip their mismatch branches. This is by design (branch-per-release), but it means the F44 branch is dead on arrival until this one value changes.

**Recommendation**: On the F44 branch cut, change `vars/fedora-version.yml` to `fedora_version: 44` as the first commit (the workflow already documented in `docs/development.md:56-78`). No gate-logic changes are needed — the equality gates are correct. Everything in this file's consumer list follows automatically; the remaining findings below are the literals that do NOT follow automatically.

## VER-02: Undefined warn function on the setup.bash version-mismatch path

**Severity**: high
**Area**: install-bootstrap
**Files**:

- `scripts/setup.bash:49-50` (calls `warn` twice), `scripts/setup.bash:13` (`set -euo pipefail`), `scripts/setup.bash:19-22` (helper definitions — no `warn`)
- Contrast: `scripts/setup-rclone.bash:36` (defines `warn()` correctly)

**Concern**: `setup.bash` defines `die`, `ok`, `header` and `check` but never `warn`. The only code path that calls `warn` is the Fedora version-mismatch branch (lines 47-55) — i.e. exactly the path exercised during an F43→F44 upgrade window (host already on 44, repo still targeting 43, or vice versa). Under `set -euo pipefail` the script aborts with `warn: command not found` instead of offering the documented "Continue anyway? [y/N]" escape hatch. This confirms and escalates Plan 00049 finding BSH-08: the bug is invisible today because versions match, and will fire on the first F44 host.

**Recommendation**: Define `warn()` in the helper block of `scripts/setup.bash` (mirroring `setup-rclone.bash:36`) before or alongside the F44 version bump, so the mismatch prompt actually works during the migration window. Research only — do not implement now.

## VER-03: Hardcoded F43 branch literals in kickstart and ISO build path

**Severity**: medium
**Area**: install-media
**Files**:

- `fedora-install/ks.cfg:1` (`#version=F43`), `fedora-install/ks.cfg:556` (`SETUP_BRANCH="F43"`), `fedora-install/ks.cfg:597` (second `SETUP_BRANCH="F43"` inside the autostart heredoc), `fedora-install/ks.cfg:601` (comment "terminals … for Fedora 43")
- Partial mitigation: `fedora-install/setup-netinstall-boot.bash:874` (`sed -i "s/^SETUP_BRANCH=.*/SETUP_BRANCH=\"F${target_version}\"/"` rewrites the deployed copy from `vars/fedora-version.yml`)
- NOT mitigated: `fedora-install/build-iso.bash:13` (`KS_FILE="${SCRIPT_DIR}/ks.cfg"` — embeds the repo file verbatim, no substitution)
- Cosmetic examples: `fedora-install/build-iso.bash:30-31` and `fedora-install/README.md:312` (43-x ISO filenames in usage text), `fedora-install/setup-netinstall-boot.bash:632` (comment example), `:818` (comment "F43→F44")

**Concern**: The `SETUP_BRANCH` literal appears twice in `ks.cfg` and is the branch a freshly kickstarted machine clones. `setup-netinstall-boot.bash` correctly templates both occurrences at deploy time from the version file, but `build-iso.bash` consumes `ks.cfg` as-is — an ISO built from the F44 branch with an un-bumped `ks.cfg` would install Fedora 44 and then clone and run the **F43** branch, tripping every VER-01 gate on first login. The ISO discovery URLs themselves are safe (built from `${version}` read from the version file — `setup-netinstall-boot.bash:74,608,627`).

**Recommendation**: On the F44 branch cut, bump both `SETUP_BRANCH="F43"` literals and the `#version=F43` header to F44. Longer term, consider having `build-iso.bash` apply the same `SETUP_BRANCH` substitution as `setup-netinstall-boot.bash:874` so `ks.cfg` has one less hand-maintained duplicate of the canonical version. Update the cosmetic 43-x ISO examples in the same sweep.

## VER-04: docker-in-lxc duplicates the canonical version as FEDORA_VERSION 43

**Severity**: medium
**Area**: lxc
**Files**:

- `files/var/local/docker-in-lxc:17` (`FEDORA_VERSION="43"  # Should match vars/fedora-version.yml`)
- `files/var/local/docker-in-lxc:333` (`lxc-create -n "$name" -t download -- -d fedora -r "$FEDORA_VERSION" -a amd64`)
- Cosmetic: `files/var/local/docker-in-lxc:55,95,320` (help/status text "Fedora 43")
- Deployment: `playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml:62-71` (plain `copy` + symlink — no templating)

**Concern**: This is a hand-maintained duplicate of the canonical value, flagged as such by its own comment. After the F44 bump it silently keeps creating Fedora **43** LXC containers (or fails outright if/when the 43 image is retired from the download server), with no gate to catch the drift — the script never cross-checks `vars/fedora-version.yml`.

**Recommendation**: Bump `FEDORA_VERSION` to 44 on the branch cut and verify the `download` template publishes a fedora/44 image at that time. Consider converting the deployment to `template:` so the value is injected from `fedora_version`, eliminating the duplicate.

## VER-05: Stale Fedora 42 references in core onboarding docs

**Severity**: medium
**Area**: docs
**Files**:

- `README.md:88` ("**Current branch targets: Fedora 42**"), `README.md:92-93` ("F42 - Fedora 42 (current)", "F43 - Fedora 43 (future)")
- `docs/installation.md:10,46,106,247,252,256` (Fedora 42 prerequisites, "Checks Fedora version matches target (Fedora 42)", `git checkout F42`, example outputs "Fedora release 42", "fedora_version: 42")
- `docs/features/speech-to-text.md:60` ("**Fedora 42** (this branch)")
- `docs/playbooks.md:320` ("Latest stable version for Fedora 42")
- `docs/fast-file-manager.md:262` ("If you're still on Fedora 42 when GNOME 50 releases…")

**Concern**: These are already one release stale on the F43 branch (confirmed by Plan 00049 DOC-07) and will be **two** releases stale on F44. The README "current branch targets" line and the installation guide's checkout instructions actively misdirect a fresh F44 install towards F42, contradicting the version gate that run.bash enforces.

**Recommendation**: In the F44 migration, sweep all "current version" claims to 44 in one pass, and reduce the number of places that state the current version (e.g. point readers at `vars/fedora-version.yml` / the branch name instead of repeating the number in six documents). Generic `F<N>` examples can stay generic.

## VER-06: Current-is-43 statements needing a routine bump

**Severity**: low
**Area**: docs
**Files**:

- `docs/README.md:317` ("**Current branch:** Fedora 43 (`F43`)"), `docs/README.md:333` (`git checkout F43  # or your version`)
- `docs/development.md:56,67,71,74-78` (branch-creation walkthrough using F43 as the example new branch)
- `docs/post-upgrade.md:3,55,67` ("e.g. F42 → F43" examples — generic, acceptable)
- `files/var/local/docker-in-lxc:55,95` (help text "Fedora 43" — covered functionally by VER-04)

**Concern**: Unlike VER-05 these are accurate today, but each states "43 is current" and so needs a deliberate bump on the F44 branch. The `docs/development.md` walkthrough is the actual recipe for creating the F44 branch — its example commands (`git checkout -b F43`, `gh repo edit --default-branch F43`) should be re-pointed or made version-neutral so the recipe doesn't age.

**Recommendation**: Bump "current" statements to 44 / F44 during the branch cut; prefer version-neutral phrasing (`F<NEW>`) in the development walkthrough so only genuinely current-version claims need touching next cycle.

## VER-07: GNOME Shell extension list verified against Shell 49 not 50

**Severity**: medium
**Area**: extensions
**Files**:

- `playbooks/imports/play-gnome-shell-extensions.yml:27` ("Verified compatibility status checked against GNOME Shell 49 (Fedora 43)") gating the extension id list at lines 28-48 (Blur my Shell 3193, Vitals 1460, AppIndicator 615, Clipboard Indicator 779, Just Perfection 3843, Tiling Shell 7065, Space Bar 5090)

**Concern**: Fedora 44 ships GNOME 50 ([Fedora Magazine](https://fedoramagazine.org/announcing-fedora-linux-44/)). The playbook installs each extension via `gnome-shell-extension-installer --yes --restart-shell`, which fetches whatever extensions.gnome.org serves for the running shell — so the version literal is a verification claim, not a gate. On F44 that claim is stale: any of the seven pinned extensions that has not yet published a Shell-50-compatible release will fail to install or load. (Deep per-extension compatibility checking belongs to the extensions dimension; this finding tracks the version literal and the re-verification obligation it encodes.)

**Recommendation**: During F44 migration, re-verify each listed extension against GNOME Shell 50 on extensions.gnome.org and update the line-27 comment to "GNOME Shell 50 (Fedora 44)", dropping or deferring any extension without Shell 50 support.

## VER-08: darktable build pins an f43 dist-git commit while gating on fedora_version

**Severity**: medium
**Area**: builds
**Files**:

- `playbooks/imports/optional/common/play-darktable-ai-build.yml:48` (`darktable_distgit_commit: "f35a6d0…"` pinned to the **f43** dist-git branch), `:13,45-46,78,156,365` (f43-branch comments and clone task), `:52-54` (`darktable_release_full: "….fc{{ fedora_version }}"`), `:75` (`mock_chroot: "fedora-{{ fedora_version }}-x86_64"`), `:91-93` (assert `ansible_distribution_major_version == fedora_version | string` — "builds for Fedora {{ fedora_version }} only")

**Concern**: The variable plumbing is correct, which makes the failure subtle: bumping `fedora_version` to 44 silently retargets the mock chroot to `fedora-44-x86_64` and the dist tag to `.fc44`, while the spec and patches still come from the **f43** dist-git commit pinned at line 48. The build either fails in the F44 chroot or produces an RPM built from an F43-era spec mislabelled as fc44.

**Recommendation**: When bumping to F44, re-pin `darktable_distgit_commit` to a known-good commit on the `f44` dist-git branch (https://src.fedoraproject.org/rpms/darktable/commits/f44), re-check the Patch0 staging list at lines 78+, and update the f43 comments. Treat this playbook as "must be deliberately re-validated", not auto-migrated.

## VER-09: Version-labelled GSK_RENDERER workaround predates F44

**Severity**: low
**Area**: workarounds
**Files**:

- `playbooks/imports/optional/common/play-fast-file-manager.yml:9,95,101,106-108` ("GSK_RENDERER=ngl Fix for Fedora 41/42", applied to `/etc/environment` whenever `apply_gsk_fix` is true — no version gate)
- `docs/fast-file-manager.md:9,21,57,239,262` (Fedora 41/42 framing; line 262 explicitly anticipates re-evaluation "when GNOME 50 releases")

**Concern**: The workaround is labelled for Fedora 41/42 GTK4 renderer slowness but is applied unconditionally (subject only to the `apply_gsk_fix` flag) and so will carry forward onto F44/GNOME 50, where forcing `GSK_RENDERER=ngl` may be obsolete or counterproductive against the current default renderer. The doc itself (line 262) flags GNOME 50 — which Fedora 44 now ships — as the re-evaluation trigger.

**Recommendation**: On F44, re-test GTK4/Nautilus startup latency without the override; either retire the block (and its `/etc/environment` entry via the existing Ansible marker) or relabel it with verified F44 applicability. Update the 41/42 labels either way.

## VER-10: run.bash dynamic gate and the untested-playbooks branch-cut process

**Severity**: low
**Area**: process
**Files**:

- `run.bash:94-95,378-379` (derives running version from `/etc/os-release` — correct, no literal), `run.bash:809-824` (equality gate against `vars/fedora-version.yml`)
- `run.bash:1408-1424` (untested-playbooks warning flow for "Fedora $fedora_version"), `playbooks/imports/optional/untested/` (currently contains only `README.md`)

**Concern**: No hardcoded literal here — this is the gating logic working as intended — but it encodes a branch-cut process obligation: the `untested/` directory is the mechanism for declaring optional playbooks not-yet-verified on the new release, and it is currently empty because everything has been verified on F43. An F44 branch that bumps `fedora_version` without repopulating `untested/` implicitly asserts every optional playbook works on Fedora 44.

**Recommendation**: As part of the F44 branch cut, decide deliberately which optional playbooks move into `playbooks/imports/optional/untested/` pending re-verification on Fedora 44 (the high-risk candidates are the hardware and build playbooks flagged in VER-07/VER-08 and the NVIDIA/DisplayLink plays). No code change to run.bash needed.

## VER-11: Forward-safe version comments that need re-verification only

**Severity**: info
**Area**: comments
**Files**:

- `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml:17,21,29` ("Fedora 43+ kernel ≥ 6.19" — F44 GA kernel is 6.19, so the condition still holds; [Fedora Magazine](https://fedoramagazine.org/announcing-fedora-linux-44/))
- `playbooks/imports/play-AB-dnf-upgrade.yml:53` ("Ansible 2.20 + Fedora 43" uname rationale comment)
- `playbooks/imports/optional/hardware-specific/play-nvidia.yml:4,44` ("recommended method for Fedora 43", "F43 rename of nvidia-vaapi-driver" — repo URLs are correctly `{{ fedora_version }}`-driven, and the NVIDIA CUDA `fedora44` repo directory is confirmed live)
- `playbooks/imports/optional/common/play-image-watermarking.yml:12` ("IM7 on Fedora 43+"), `playbooks/imports/optional/common/play-photography.yml:209` ("no python3-rawpy package as of F43")
- `playbooks/imports/optional/archived/README.md:11`, `docs/fast-file-manager.md:9`, `CLAUDE/GnomeShell.md:45` ("Fedora 42 has 48.7" — stale GNOME version claim in agent docs)

**Concern**: These are comments and prose encoding observations true at F42/F43 time. None of them gates execution, so nothing breaks on F44 — but each "as of F43" claim should be re-confirmed (e.g. is python3-rawpy still absent in F44? is the IPU6 guidance unchanged on 6.19/7.0 kernels?) and `CLAUDE/GnomeShell.md:45` should state GNOME Shell 50 to avoid misleading future agent sessions.

**Recommendation**: Forward-looking watch-items: re-verify each claim during the F44 sweep and refresh the wording to "Fedora 44" / "F44" where the observation still holds. No gating change required.

## Sources

- https://fedoramagazine.org/announcing-fedora-linux-44/ — Fedora 44 release announcement (28 April 2026; GNOME 50; kernel 6.19.14-300.fc44; KDE Plasma 6.6; MariaDB 11.8)
- https://en.wikipedia.org/wiki/Fedora_Linux_release_history — release dates and GNOME/kernel versions for F43 (GNOME 49) and F44 (GNOME 50)
- https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new — corroboration of GA kernel 6.19 and Python 3.14 continuity
- https://developer.download.nvidia.com/compute/cuda/repos/ — confirms the `fedora44` CUDA repo directory exists
