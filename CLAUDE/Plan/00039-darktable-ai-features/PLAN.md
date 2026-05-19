# Plan 00039: Darktable AI Features

**Status**: Research Complete — Awaiting Decision Gate
**Created**: 2026-05-19
**Owner**: joseph
**Priority**: Medium
**Estimated Effort**: 2–4 hours implementation after decision gate

## Overview

Darktable 5.x ships optional AI-powered features (object masks, denoise, upscale)
backed by ONNX Runtime. These require:

1. A darktable binary compiled with `-DUSE_AI=ON` (off by default)
2. AI features enabled in user preferences (off by default)
3. Models downloaded from the AI preferences tab (none bundled)
4. *(Optional)* A GPU-enabled ONNX Runtime library installed separately

**Research finding**: the Fedora-shipped `darktable` RPM is **not** compiled
with `-DUSE_AI=ON`. Neither is the Flathub Flatpak. The only Linux
distribution channel with AI compiled in is the **official upstream
AppImage** (`Darktable-5.4.1-x86_64.AppImage`), which is built with
`--enable-ai` and bundles a CPU-only ONNX Runtime via dlopen.

**Follow-up research finding**: building from source is **much simpler than
it first appeared**. The darktable cmake module `FindONNXRuntime.cmake`
auto-downloads ONNX Runtime if not found on the system — no extra build-time
packaging required. The Fedora spec is a 259-line file we can fork into a
local `darktable-ai.spec` (or COPR) with a one-line cmake change
(`-DUSE_AI=ON`). This is the cleanest path because:

- It is a single RPM — `darktable-ai` does not coexist with `darktable`, it
  *replaces* it
- The existing Sony A7V `rawspeed/cameras.xml` patch becomes a build-time
  patch in the same spec, eliminating the current post-install overlay
- Standard `dnf upgrade` workflow; no AppImage filesystem quirks
- Cost: ~10–20 min build per darktable release (currently 4×/year)

This plan now offers **two implementation paths** at the Phase 1 decision
gate — local source-built RPM (recommended) or upstream AppImage
(fallback). Either path enables CPU AI; both have the same downstream
GPU acceleration phase.

See [research.md](research.md) for the full evidence trail.

## Goals

- Get darktable AI features (object masks, denoise, upscale) working on this
  Fedora 43 machine
- Deliver via existing IaC pattern (`play-photography.yml` extension) — no
  manual install steps
- Provide an opt-in NVIDIA GPU acceleration path that does not break the
  default install if CUDA/cuDNN are absent
- Preserve the existing Sony A7V cameras.xml bodge (or document its loss
  cleanly if we choose to drop the RPM)
- Document the irreducibly manual final step (Preferences → AI → Enable →
  Download models) in the playbook's success message

## Non-Goals

- Publishing a public COPR for others to consume (we may use a private/local
  COPR, but maintaining a polished public one is out of scope)
- AMD or Intel GPU acceleration (no such hardware on either current laptop;
  add a future plan if/when that changes)
- Filing a Fedora RFE to enable `-DUSE_AI=ON` upstream (out of scope — could
  be a follow-up issue)
- Managing model files via Ansible (models are GUI-downloaded and live in
  `~/.config/darktable/ai/` per-user; Ansible-managing them adds no value)
- Auto-detect-and-enable preference changes (no good way to scribble into
  darktable's settings DB without race conditions; document the GUI step)

## Context & Background

Existing repo state relevant to this plan:

- `playbooks/imports/optional/common/play-photography.yml` installs the
  Fedora `darktable` RPM and applies a Sony A7V cameras.xml patch
- `playbooks/imports/optional/hardware-specific/play-nvidia.yml` installs
  NVIDIA driver + `xorg-x11-drv-nvidia-cuda` runtime libs (NOT the full CUDA
  Toolkit, which is required for GPU ORT)
- The same playbook already installs an AppImage cleanly (ART) — that pattern
  is the model for the darktable AppImage install
- The user has confirmed NVIDIA hardware via `play-nvidia.yml`

Full research is in [research.md](research.md) — read that first if you are
picking this plan up cold.

## Tasks

### Phase 1: Decision Gate (no implementation yet)

User-input questions that must be resolved before writing any playbook:

- [ ] ⬜ **D1**: Implementation path for the AI-enabled darktable binary?
  - **Option A — Local source RPM** (recommended): fork the Fedora spec,
    add `-DUSE_AI=ON`, apply the A7V cameras.xml as a build-time patch,
    build via `mock` (idempotent) or `rpmbuild`, install as the standard
    `darktable` package replacing the stock RPM. Single binary, A7V baked
    in, dnf workflow.
  - **Option B — Upstream AppImage**: install
    `Darktable-5.4.1-x86_64.AppImage` as `darktable-ai` alongside the
    Fedora RPM. No build cost; cannot patch A7V into AppImage's bundled
    rawspeed → A7V users keep using the RPM for non-AI work.
  - **Recommendation**: **Option A**. The build is trivial (cmake
    auto-downloads ONNX Runtime); ongoing cost is ~15 min per darktable
    release; A7V support is preserved without an overlay.
- [ ] ⬜ **D2** (only if D1=A): build host strategy?
  - **Option A1 — `mock`**: Fedora's standard tool for isolated RPM builds.
    Installed via Ansible, reproducible. Slower first run (downloads
    chroot), fast subsequent runs.
  - **Option A2 — direct `rpmbuild` on host**: simpler, faster, no chroot
    isolation. Risk: BuildRequires installed on host become permanent.
  - **Recommendation**: **mock**. The repo already values reproducibility;
    isolation prevents `*-devel` packages cluttering the host.
- [ ] ⬜ **D3**: Enable GPU acceleration phase (Phase 3) in this plan, or
  ship CPU-only AI first and add GPU as a follow-up plan?
  - Both laptops need to be handled. Laptop A (current) has NVIDIA; laptop B
    does not. The GPU playbook MUST detect hardware and no-op cleanly on
    non-NVIDIA machines.
  - **Recommendation**: **CPU-only first** (Phase 2 only), then Phase 3 in
    a second pass after Phase 2 is verified on both machines. Splitting
    the work makes regressions easier to attribute.

### Phase 2: Source-Built `darktable` RPM with AI (primary path, gated on D1=A)

Goal: produce and install an RPM identical to Fedora's stock `darktable` but
with `-DUSE_AI=ON` added to cmake flags and the A7V `cameras.xml` applied
as a build-time patch. Tag: `darktable-ai-build`.

Structure: new playbook
`playbooks/imports/optional/common/play-darktable-ai-build.yml`. The
existing `play-photography.yml` is left alone except to drop its A7V
overlay tasks (now redundant) and update its success message.

- [ ] ⬜ **T2.1**: Install build tooling (host, idempotent):
  - `mock` (D2=A1) or `rpm-build` + `rpmdevtools` (D2=A2)
  - Add `{{ user_login }}` to the `mock` group (D2=A1) — requires log-out
    or `newgrp mock`
- [ ] ⬜ **T2.2**: Fetch the upstream Fedora rawhide darktable spec at
  playbook run time:
  `https://src.fedoraproject.org/rpms/darktable/raw/rawhide/f/darktable.spec`
  to `/tmp/darktable.spec`. Pin a known-good commit hash (fetched
  fresh once per darktable release) for reproducibility — store the
  hash in a vars file alongside the version number.
- [ ] ⬜ **T2.3**: Apply spec mutations via `ansible.builtin.replace` /
  `lineinfile`:
  - Add `-DUSE_AI=ON` to the `%cmake` invocation
  - Bump `Release:` tag with a local suffix (e.g. `5.4.1-1.ai.fc43`) so
    `dnf` prefers it over the official one
  - Add the A7V `cameras.xml` patch as `Patch5:` and the corresponding
    `%autosetup` apply
- [ ] ⬜ **T2.4**: Drop the A7V cameras.xml override file into `~/rpmbuild/ SOURCES/` (or mock's source dir). Source: the same pinned rawspeed
  commit currently used by `play-photography.yml`. The patch should
  modify `src/external/rawspeed/data/cameras.xml` in the darktable
  source tree.
- [ ] ⬜ **T2.5**: Build the SRPM (`rpmbuild -bs darktable.spec`) and the
  binary RPMs (D2=A1: `mock --rebuild`; D2=A2: `rpmbuild -bb`). Use
  `args: creates:` so a re-run with the same version is a no-op.
- [ ] ⬜ **T2.6**: Install the resulting RPMs via `dnf`:
  - `darktable-5.4.1-1.ai.fc43.x86_64.rpm`
  - `darktable-tools-noise-*.rpm`
  - `darktable-tools-basecurve-*.rpm`
- [ ] ⬜ **T2.7**: Drop the now-obsolete A7V overlay tasks from
  `play-photography.yml` (the entire `darktable-a7v` block) — replaced
  by the build-time patch in T2.4. Update the playbook's final
  Description Block to mention that A7V support is now built-in via the
  custom AI-enabled build.
- [ ] ⬜ **T2.8**: `uri:` check against the upstream darktable releases API
  to warn if a newer version exists (pin update reminder, same pattern as
  ART/RapidRAW).
- [ ] ⬜ **T2.9**: Success message: explicit step-by-step Preferences →
  AI tab → enable → Download models for denoise / upscale / segmentation.

### Phase 2-alt: AppImage Install (fallback, gated on D1=B)

If D1=B is chosen instead, this replaces Phase 2 above. Mirror the existing
ART AppImage install block. Tag: `darktable-ai-appimage`.

- [ ] ⬜ **T2alt.1**: Pin variables in `play-photography.yml`:
  - `darktable_appimage_version: "5.4.1"`
  - `darktable_appimage_url: "https://github.com/darktable-org/darktable/releases/download/release-{{ darktable_appimage_version }}/Darktable-{{ darktable_appimage_version }}-x86_64.AppImage"`
  - `darktable_appimage_sha256: <fetch fresh and pin>`
  - `darktable_appimage_install_path: "/opt/Darktable-{{ darktable_appimage_version }}-x86_64.AppImage"`
- [ ] ⬜ **T2alt.2**: `stat:` check (skip download if already at target version)
- [ ] ⬜ **T2alt.3**: `get_url:` with `checksum: sha256:{{ ... }}` and `mode: "0755"`
- [ ] ⬜ **T2alt.4**: Symlink `/usr/local/bin/darktable-ai` →
  `darktable_appimage_install_path`. Distinct binary name; the RPM
  `darktable` stays untouched.
- [ ] ⬜ **T2alt.5**: Desktop entry `/usr/share/applications/darktable-ai.desktop`
  — distinct Name (`darktable (AI)`), distinct Icon.
- [ ] ⬜ **T2alt.6**: Icon — extract from AppImage or fetch from upstream
  repo; install as `/usr/share/pixmaps/darktable-ai.png`.
- [ ] ⬜ **T2alt.7**: `uri:` check against
  `https://api.github.com/repos/darktable-org/darktable/releases/latest`,
  warn if newer version available.
- [ ] ⬜ **T2alt.8**: Success message: mention `darktable-ai` runs alongside
  `darktable`; same Preferences-tab instructions as T2.9.

### Phase 3: NVIDIA GPU Acceleration (optional — gated on D3)

A new playbook: `playbooks/imports/optional/hardware-specific/play-darktable-ai-gpu.yml`.
Lives in `hardware-specific/` because it depends on detected hardware and
pulls in heavy CUDA Toolkit + cuDNN dependencies.

**Critical**: must be safe to include in `playbook-main.yml` regardless of
which laptop runs it. Non-NVIDIA machines (laptop B and any future
hardware) must hit a hardware-detection check at the very top and
no-op cleanly with a single informational `debug:` message.

- [ ] ⬜ **T3.0**: Hardware detection at the very top of the playbook —
  use `ansible.builtin.command: lspci -nn` registered as
  `lspci_out`, set `has_nvidia_gpu` fact via
  `'10de:' in lspci_out.stdout | lower` (10de is NVIDIA's PCI vendor ID).
  Every subsequent task in this playbook gets `when: has_nvidia_gpu`.
  The detection itself is `changed_when: false`. Add a leading
  `debug:` task that prints "no NVIDIA GPU detected, skipping
  GPU AI acceleration" when `not has_nvidia_gpu`.
  - Rationale: `nvidia-smi` is unreliable for detection because it can
    be installed on a machine without the GPU (cross-laptop config),
    and absent on a machine with a GPU (early boot before module
    loaded). PCI ID is the ground truth.
- [ ] ⬜ **T3.1**: After T3.0 gate, preflight: assert `play-nvidia.yml` has
  already run on this machine (probe for `akmod-nvidia` or
  `xorg-x11-drv-nvidia` package). Fail clearly with a remediation
  message if not.
- [ ] ⬜ **T3.2**: Install CUDA Toolkit 12.x (via RPM Fusion `cuda` or
  NVIDIA's official repo — investigate cleanest path)
- [ ] ⬜ **T3.3**: Install cuDNN 9.x (RPM Fusion `cuda-cudnn` if available,
  else direct from NVIDIA — needs investigation)
- [ ] ⬜ **T3.4**: Fetch ORT GPU manifest from
  `https://raw.githubusercontent.com/darktable-org/darktable/refs/heads/master/data/ort_gpu.json`
  AT PLAYBOOK RUN — do not pin in vars (upstream refreshes monthly via CI)
- [ ] ⬜ **T3.5**: Detect installed CUDA major.minor (replicates the
  `detect_cuda_version` cascade from the upstream script: nvcc → version.json →
  ldconfig libcudart) — Ansible `command:` + `register:` + `set_fact:`
- [ ] ⬜ **T3.6**: Pick the manifest entry whose `cuda_min`/`cuda_max` brackets
  the detected version
- [ ] ⬜ **T3.7**: `get_url:` the tarball, verify SHA256 from manifest entry
- [ ] ⬜ **T3.8**: `unarchive:` into a temp dir, `find:` for `libonnxruntime*.so*`,
  copy into `/home/{{ user_login }}/.local/lib/onnxruntime-gpu/`
- [ ] ⬜ **T3.9**: Clear `PT_GNU_STACK` RWE flag on the `.so` files —
  glibc 2.41+ refuses to dlopen libs with executable stack. Use `execstack -c`
  if available, else the Python ELF patch from the upstream script. **Wrap
  this in a probe-then-fail pattern with a fail-fast annotation.**
- [ ] ⬜ **T3.10**: Set `DT_ORT_LIBRARY` env var for the user (in
  `/etc/profile.d/darktable-ai-ort.sh` or similar) — picks up the lib without
  needing the preferences-tab "browse" step
- [ ] ⬜ **T3.11**: Print verification steps in the success message:
  - `darktable-ai -d ai 2>&1 | grep -E "execution provider|enabled successfully"`
  - Expected: `execution provider: CUDA` and `NVIDIA CUDA enabled successfully`
- [ ] ⬜ **T3.12**: Document fall-through behaviour: if any GPU step fails at
  runtime, darktable auto-retries on CPU — no playbook-level error needed

### Phase 4: Verification

(On host, not in CCY container — `pass-through` step.)

- [ ] ⬜ **V4.1**: Run `./scripts/qa-all.bash` against the playbook changes
- [ ] ⬜ **V4.2**: Deploy on host: `ansible-playbook playbooks/imports/optional/common/play-photography.yml`
- [ ] ⬜ **V4.3**: Confirm `darktable-ai` launches, AI tab present in preferences
- [ ] ⬜ **V4.4**: Download one model (denoise is smallest), confirm it works
  on a real RAW file
- [ ] ⬜ **V4.5**: *(If Phase 3 done)* Deploy `play-darktable-ai-gpu.yml`,
  confirm `darktable-ai -d ai` reports `CUDA` provider
- [ ] ⬜ **V4.6**: Smoke test denoise on a noisy ISO-6400 RAW — confirm
  perceptible quality improvement and GPU usage in `nvidia-smi`

### Phase 5: Documentation & Cleanup

- [ ] ⬜ **D5.1**: Update `docs/` (if a photography page exists) or add one
- [ ] ⬜ **D5.2**: Add troubleshooting block to the playbook itself:
  - "AI tab missing in preferences" → using RPM, not AppImage; launch
    `darktable-ai` instead
  - "Models won't download" → check network, check
    `~/.config/darktable/ai/` exists and is writable
  - "GPU not detected after Phase 3" → run `darktable-ai -d ai`, check that
    `libcudart.so.12` or `libcudart.so.13` is in `ldconfig -p`, verify cuDNN
    `libcudnn.so.9` resolvable
- [ ] ⬜ **D5.3**: Mark plan ✅ in this file, move to `Completed/` per repo
  convention, update `CLAUDE/Plan/README.md`

## Dependencies

- **Depends on**: `play-photography.yml` (extends it), `play-nvidia.yml`
  (Phase 3 only)
- **Blocks**: none currently
- **Related**: Plan 00037 (watermarking, sibling photography work), no overlap

## Technical Decisions

### Decision 1: Local source-built RPM is the primary path

**Context**: Fedora's `darktable` RPM is not built with `-DUSE_AI=ON`. We
need an AI-enabled binary on Fedora 43, and we want to preserve Sony A7V
support in the same binary.

**Options considered**:

1. **Local source-built RPM** — fork the Fedora spec, add `-DUSE_AI=ON`,
   apply the A7V `cameras.xml` as a build-time patch. Pros: single binary,
   dnf workflow, A7V baked in (no post-install overlay), no AppImage
   filesystem quirks. Cons: ~10–20 min build per darktable release; needs
   `mock` or `rpmbuild` installed.
2. **Upstream AppImage**. Pros: zero build cost; upstream-supported; CPU
   ORT already bundled. Cons: separate binary (cannot share preferences/
   plugins with the existing RPM); cannot patch A7V into AppImage's
   bundled rawspeed without invasive AppDir surgery; coexistence with
   the RPM adds cognitive overhead.
3. **Wait for Fedora to enable `-DUSE_AI=ON`** — no timeline; filing the
   RFE is a separate concern.

**Decision (REVISED 2026-05-19)**: Option 1 — local source-built RPM. The
earlier conclusion (AppImage) was based on an overestimate of the build
cost. Follow-up reading of `cmake/modules/FindONNXRuntime.cmake` showed
cmake auto-downloads ONNX Runtime, so the source build is one cmake flag
away from the existing Fedora spec. The A7V build-time patch is cleaner
than the current post-install `cameras.xml` overlay. Option 2 is retained
as a fallback path (Phase 2-alt) in case the source build proves
unworkable.

**Date**: 2026-05-19 (revised same-day after follow-up research)

### Decision 2: A7V cameras.xml as a build-time patch

**Context**: `play-photography.yml` currently patches A7V support into
`/usr/share/darktable/rawspeed/cameras.xml` as a post-install overlay (see
the `darktable-a7v` task block). If we move to a source build, this overlay
becomes redundant and should be cleaned up.

**Decision**: Convert the A7V cameras.xml override into a build-time
`Patch5:` in the local spec, sourced from the same pinned rawspeed commit
we currently overlay. Delete the entire `darktable-a7v` block from
`play-photography.yml` after T2.7. The build-time patch is the correct
place for this — the override was always a post-install hack to work around
our inability to control the build.

**Date**: 2026-05-19

### Decision 3: GPU acceleration in a separate playbook

**Context**: Should GPU ORT install live inside `play-photography.yml` or
in a separate hardware-specific playbook?

**Decision**: Separate playbook
(`playbooks/imports/optional/hardware-specific/play-darktable-ai-gpu.yml`).
Pulls in heavy NVIDIA-only deps (CUDA Toolkit 12.x, cuDNN 9.x — multi-GB
together) that machines without NVIDIA should not pay for;
`hardware-specific/` is the established home for such gating.

**Date**: 2026-05-19

### Decision 4: Multi-laptop safety via PCI vendor ID detection

**Context**: The user has two laptops — one with NVIDIA, one without. Both
share this Ansible config. The GPU playbook must be safe to invoke on the
non-NVIDIA machine without errors, package installs, or false-positive log
noise.

**Options considered**:

1. **`nvidia-smi` presence check** — unreliable: the binary can be installed
   on a machine without the physical GPU (cross-laptop config migration),
   and absent on a machine with a GPU (early boot, missing driver).
2. **`lspci` for PCI vendor ID `10de`** — ground truth from the PCI bus;
   only positive when an NVIDIA GPU is physically present.
3. **Inventory variable per host** — e.g., `has_nvidia_gpu: true` in
   `host_vars/<hostname>.yml`. Most explicit but requires per-host
   maintenance.

**Decision**: Option 2 (`lspci` vendor ID), set as a fact at the top of the
GPU playbook and used as `when:` on every subsequent task. Option 3 is a
future enhancement if hardware detection becomes a cross-cutting concern
across more playbooks.

**Date**: 2026-05-19

## Success Criteria

- [ ] `darktable-ai` launches from CLI and from GNOME activities
- [ ] `Preferences → AI` tab is visible
- [ ] At least one AI model downloads successfully and applies to a RAW image
- [ ] *(If Phase 3 done)* `darktable-ai -d ai` reports CUDA provider active
- [ ] *(If Phase 3 done)* `nvidia-smi` shows GPU usage during AI denoise/upscale
- [ ] No regression in the existing `darktable` RPM workflow (A7V still opens)
- [ ] `./scripts/qa-all.bash` passes on all playbook changes
- [ ] Plan committed alongside playbook changes per the
  "Plan Commit Rule" in CLAUDE.md

## Risks & Mitigations

| Risk                                                   | Impact | Probability | Mitigation                                                                                        |
| ------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------------------------------------- |
| AppImage filesystem permissions / FUSE issues          | Low    | Low         | ART AppImage already runs cleanly via same pattern; fuse2 is default on Fedora                    |
| AI models too large to fit in 4 GB VRAM                | Medium | Low         | Upstream falls back to CPU automatically; document this                                           |
| CUDA Toolkit install conflicts with existing CUDA libs | High   | Medium      | Phase 3 task T3.2 explicitly investigates RPM Fusion vs NVIDIA repo; preflight asserts            |
| cuDNN distribution restrictions (NVIDIA EULA)          | Medium | Medium      | Investigate RPM Fusion `cuda-cudnn`; fall back to direct download with documented EULA acceptance |
| Models silently fail to download (network/firewall)    | Low    | Low         | Document a troubleshooting block; check `~/.config/darktable/ai/`                                 |
| AppImage update process diverges from current version  | Low    | Medium      | Use the `uri:` API check (same pattern as ART/RapidRAW) to flag stale versions                    |

## Timeline

- **Phase 1 (decision gate)**: Awaiting user input — blocking
- **Phase 2 (source-built RPM, primary)**: 2–3 hours after Phase 1 closes
  (first-time mock chroot setup is the long pole; subsequent rebuilds are 10–20 min)
- **Phase 2-alt (AppImage, fallback)**: 1 hour, only if D1=B
- **Phase 3 (GPU acceleration, optional)**: 1–2 hours after Phase 2 deploys cleanly
- **Phase 4 (verification on host)**: 30 min, user-driven
- **Phase 5 (docs + cleanup)**: 30 min

Target completion: when the user has resolved D1–D3 and Phase 2 is deployed,
within one or two sessions. Phase 3 may slip to a separate session.

## Notes & Updates

### 2026-05-19 — initial pass

- Plan created. Initial research pass complete — captured in `research.md`.
- Key finding: Fedora RPM is NOT built with `-DUSE_AI=ON`. Only the upstream
  AppImage has AI compiled in on Linux. Flatpak also lacks it.
- No COPR rebuilds with AI exist — checked.
- GPU ORT install is fully Ansible-replicable from
  `tools/ai/install-ort-gpu.sh`. Manifest at `data/ort_gpu.json`.
- Awaiting Phase 1 decision gate before any code/playbook changes.

### 2026-05-19 — revision after user prompt

User asked three follow-up questions:

1. *AppImage may not support A7V?* — Correct; the AppImage uses its own
   bundled rawspeed and the existing post-install `cameras.xml` overlay
   does not affect it.
2. *How hard is it to build this ourselves and fix everything?* — Much
   easier than first estimated. `FindONNXRuntime.cmake` auto-downloads
   ONNX Runtime, so the build is one cmake flag away from the existing
   Fedora spec. A7V can ride along as a build-time patch.
3. *Multi-laptop hardware-detection for GPU acceleration.* — Yes, required.
   Laptop A has NVIDIA, laptop B does not; the GPU playbook must
   no-op cleanly on B.

Plan revisions made:

- Decision 1 flipped from AppImage to **local source-built RPM**. AppImage
  retained as Phase 2-alt fallback.
- Decision 2 added: A7V cameras.xml moves from post-install overlay to
  build-time patch in the same spec.
- Decision 4 added: hardware detection via PCI vendor ID `10de` (`lspci`).
  `nvidia-smi` rejected as a detection mechanism (false positives / negatives).
- Phase 2 rewritten for source build via `mock` (or `rpmbuild`); Phase 2-alt
  preserved for the AppImage path.
- Phase 3 task T3.0 added at the top: hardware gate via `lspci`, every
  subsequent task gated by `when: has_nvidia_gpu`, plus a leading
  `debug:` task for the no-op case on non-NVIDIA machines.
