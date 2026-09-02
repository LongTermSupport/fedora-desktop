> Archived on 2026-09-02: superseded by the lean PLAN.md alongside. Kept verbatim as the historical record.

# Plan 00042: Darktable AI Features

**Status**: In Progress (AI path delivered via Phase 2-alt; Phase 3 GPU implemented)
Per **D4**, `darktable-ai` (the AI nightly, A7V verified) is installed
alongside the stable A7V darktable RPM. Phase 3 adds NVIDIA GPU
acceleration: `play-nvidia.yml` now installs the CUDA Toolkit + cuDNN
system-wide, and `play-darktable-ai-gpu.yml` installs the GPU ONNX Runtime.
All three playbooks pass QA + syntax-check (2026-05-21); pending host
deploy + `darktable-ai -d ai` CUDA-provider verification. See
[research-ai-version-correction.md](research-ai-version-correction.md).
**Created**: 2026-05-19
**Owner**: joseph
**Priority**: Medium
**Estimated Effort**: 2–4 hours implementation after decision gate

## Overview

> **⚠️ 2026-05-20 CORRECTION — read this first.** The premise below (that
> darktable 5.4.1 has AI behind a build flag) is **wrong**. darktable's AI
> features ship in **darktable 5.6.0**, which is **not released yet**
> (~June 2026). darktable 5.4.1 — the version this plan pinned and built —
> contains **no AI/ONNX code at all**, so `-DUSE_AI=ON` is silently ignored.
> The original research conflated the `master` branch with the
> `release-5.4.1` tag. Full corrected evidence:
> [research-ai-version-correction.md](research-ai-version-correction.md).
> The Sony A7V build-time fix (Phase 2) is unaffected and genuinely done.
> The text below is kept for history; treat it as superseded.

Darktable's AI subsystem (object masks, denoise, upscale) is backed by ONNX
Runtime. It requires:

1. A darktable binary compiled with `-DUSE_AI=ON` (off by default) —
   **only meaningful on darktable ≥ 5.6.0; the flag does nothing on 5.4.1**
2. AI features enabled in user preferences (off by default)
3. Models downloaded from the AI preferences tab (none bundled)
4. *(Optional)* A GPU-enabled ONNX Runtime library installed separately

**What the AI subsystem delivers** (per darktable 5.6.0 `RELEASE_NOTES.md`):

- **Interactive AI object-mask tool** — SAM2.1 + SegNext models. Point at a
  subject/sky/object and the model generates a selection mask. *(This is the
  user's headline priority.)*
- **Neural restore module** — raw denoise, image denoise, upscale (NIND UNet,
  NAFNet, RawNIND UtNet2; BSRGAN 2x/4x).

**Original (superseded) plan thesis**: fork the Fedora `darktable` spec, add
`-DUSE_AI=ON`, build a local RPM that replaces stock `darktable` with the A7V
fix baked in. The build mechanism works and is done — but on 5.4.1 it yields
a darktable with no AI. The same mechanism re-pinned to 5.6.0 (once released)
*would* produce AI; that is the cleanest forward path.

See [research-ai-version-correction.md](research-ai-version-correction.md) for
the corrected evidence trail, and [research.md](research.md) for the original
(partly-wrong) research pass.

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

- [x] ✅ **D1**: Implementation path for the AI-enabled darktable binary?
  - **Option A — Local source RPM** (recommended): fork the Fedora spec,
    add `-DUSE_AI=ON`, apply the A7V cameras.xml as a build-time patch,
    build via `mock` (idempotent) or `rpmbuild`, install as the standard
    `darktable` package replacing the stock RPM. Single binary, A7V baked
    in, dnf workflow.
  - **Option B — Upstream AppImage**: install
    `Darktable-5.4.1-x86_64.AppImage` as `darktable-ai` alongside the
    Fedora RPM. No build cost; cannot patch A7V into AppImage's bundled
    rawspeed → A7V users keep using the RPM for non-AI work.
  - **Decision**: **Option A — local source-built RPM**.
- [x] ✅ **D2** (only if D1=A): build host strategy?
  - **Option A1 — `mock`**: Fedora's standard tool for isolated RPM builds.
    Installed via Ansible, reproducible. Slower first run (downloads
    chroot), fast subsequent runs.
  - **Option A2 — direct `rpmbuild` on host**: simpler, faster, no chroot
    isolation. Risk: BuildRequires installed on host become permanent.
  - **Decision**: **Option A1 — mock**.
- [x] ✅ **D3**: Enable GPU acceleration phase (Phase 3) in this plan, or
  ship CPU-only AI first and add GPU as a follow-up plan?
  - Both laptops need to be handled. Laptop A (current) has NVIDIA; laptop B
    does not. The GPU playbook MUST detect hardware and no-op cleanly on
    non-NVIDIA machines.
  - **Decision**: **CPU-only first** — Phase 2 only this round; Phase 3
    deferred to a follow-up session after Phase 2 is verified on both
    laptops.

### Phase 2: Source-Built `darktable` RPM with AI (primary path, gated on D1=A)

Goal: produce and install an RPM identical to Fedora's stock `darktable` but
with `-DUSE_AI=ON` added to cmake flags and the A7V `cameras.xml` applied
as a build-time patch. Tag: `darktable-ai-build`.

Structure: new playbook
`playbooks/imports/optional/common/play-darktable-ai-build.yml`. The
existing `play-photography.yml` is left alone except to drop its A7V
overlay tasks (now redundant) and update its success message.

- [x] ✅ **T2.1**: Install build tooling (host, idempotent):
  - `mock` (D2=A1)
  - Add `{{ user_login }}` to the `mock` group — playbook handles this and
    uses `become_flags: -i` on the mock step so login-shell re-reads
    groups (no logout required).
- [x] ✅ **T2.2**: Clone Fedora dist-git for darktable at a pinned f43 commit
  (`f35a6d085e8f130f6eaa10976b353333602c2bbd`) for spec + Patch0 +
  `sources` file. SHA-512 for the source tarball pinned in playbook vars
  from the dist-git `sources` lookaside entry.
- [x] ✅ **T2.3**: Apply spec mutations via `ansible.builtin.replace` /
  `lineinfile`:
  - Add `-DUSE_AI=ON` to the Fedora-branch `%cmake` invocation (regex
    anchors on `-DRAWSPEED_ENABLE_LTO=ON\n%endif` which only matches the
    Fedora arm, not the RHEL/8 arm).
  - Replace `Release: %autorelease` with `Release: 100.ai%{?dist}` —
    the `100.` prefix ensures our build sorts above stock Fedora
    releases (single-digit `%autorelease`) so `dnf upgrade` will not
    silently downgrade to stock.
  - Replace `%autochangelog` with a static entry (no rpkg-macros dependency).
  - Add A7V `cameras.xml` overlay — see T2.4 note for Patch5 →
    Source5+cp deviation.
  - `grep -qE` verification task confirms each of the four mutations
    landed before the build runs.
- [x] ✅ **T2.4**: A7V `cameras.xml` overlay — implemented as `Source5:` +
  a `cp %{SOURCE5} src/external/rawspeed/data/cameras.xml` line injected
  after `%autosetup -p1`. **Deviation from original plan**: the original
  task called for `Patch5:`, but a unified diff would have to be
  generated at run time from both the stock cameras.xml (inside the
  tarball) and the rawspeed-develop replacement — brittle and adds a
  diff-generation step with no functional benefit. Source5+cp produces
  the same in-tree state (the rawspeed cameras.xml replaces the stock
  one before `%build`) with far less complexity.
- [x] ✅ **T2.5**: Build SRPM (`rpmbuild -bs`) then binary RPMs via
  `mock --rebuild --enable-network`. **Succeeds as of the 2026-05-20
  re-deploy** — `validate-cameras.xml` now reports `cameras.xml validates`, `USE_AI=ON` is confirmed in the cmake configure step, and
  all eight RPMs (3 installed + tools + debuginfo/debugsource) are in
  `/var/lib/mock/fedora-43-x86_64/result/`.
  - **Root cause of the 2026-05-19 mock failure — FOUND & FIXED
    2026-05-20.** The build reached 4–18%, then failed at rawspeed's
    `validate-cameras.xml` target
    (`gmake[2]: *** [...validate-cameras.xml...] Error 3`). The A7V
    overlay `cp`s the rawspeed-*develop* `cameras.xml` over the source
    tree; the build then runs `xmllint --schema cameras.xsd cameras.xml`,
    and the *older* `cameras.xsd` bundled in the darktable 5.4.1 tarball
    rejects the newer xml — `cameras.xml:17491` make `Phase One` is not
    in the schema enum set (it has `Phase One A/S`), and
    `cameras.xml:1345` alias `EOS Hi` is shorter than the schema's
    minLength 7. **None of the three handoff suspects applied**: cmake
    configured cleanly, ONNX Runtime downloaded fine over the network,
    every BuildRequires resolved. The validation failure was the *only*
    error in `build.log`.
  - **Fix**: overlay the matching `cameras.xsd` from the same pinned
    rawspeed commit alongside `cameras.xml` — `Source6:` declaration +
    a second `cp` line in `%prep`. Verified locally with `xmllint`: the
    develop `cameras.xml` validates cleanly against the develop
    `cameras.xsd` (sha256 `a5ce338…`). The xsd is build-validation-only
    (not compiled, not used at runtime), so there is zero runtime risk;
    the develop `cameras.xml` itself is already runtime-proven against
    darktable 5.4.1 by the now-retired post-install overlay.
  - **Also fixed**: the SRPM step's `creates:` guard was removed. It
    keyed on the version-release, so a spec fix that does not bump the
    VR (exactly this case) would be masked by the stale SRPM and mock
    would rebuild the *old* SRPM. `rpmbuild -bs` is cheap (~seconds);
    mock keeps its own `creates:` guard and the block is gated on
    `dt_build_needed`.
- [x] ✅ **T2.6**: Install the resulting RPMs via `ansible.builtin.dnf`.
  Confirmed installed on the host after the 2026-05-20 re-deploy:
  - `darktable-5.4.1-100.ai.fc43.x86_64`
  - `darktable-tools-noise-5.4.1-100.ai.fc43.x86_64`
  - `darktable-tools-basecurve-5.4.1-100.ai.fc43.x86_64`
- [x] ✅ **T2.7**: Dropped the entire `darktable-a7v` task block and the
  `rawspeed_*` / `darktable_cameras_xml_*` vars from
  `play-photography.yml`. Replaced the Sony A7V paragraph in the success
  message with a pointer to `play-darktable-ai-build.yml`. Added a brief
  comment in the tasks block (where the overlay used to live) explaining
  the migration and where A7V support now comes from.
- [x] ✅ **T2.8**: `uri:` check against the upstream darktable releases API
  to warn if a newer version exists (pin update reminder, same pattern as
  ART/RapidRAW).
- [x] ✅ **T2.9**: Success message: explicit step-by-step Preferences →
  AI tab → enable → Download models for denoise / upscale / segmentation.

### Phase 2-alt: AppImage Install — ✅ DONE (the AI delivery path, per D4)

> **2026-05-20**: this phase became the actual AI delivery path. The task
> list below is **superseded** — it was written for a *pinned release*
> AppImage and to live inside `play-photography.yml`. The real implementation
> is a standalone playbook, `play-darktable-ai-appimage.yml`, targeting the
> rolling **nightly** (the release AppImage has no AI). It extracts the
> AppImage to a stable AppDir and overlays the working A7V `cameras.xml`.
> See the 2026-05-20 D4 implementation note at the end of this file. Original
> task list kept for history.

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

- [x] ✅ **V4.1**: Ran `./scripts/qa-all.bash` — bash (193 files) and python
  (8 files) checks pass. The cameras.xsd fix is a YAML-only playbook edit;
  `ansible-playbook --syntax-check` passes. (`qa-all.bash` overall exit was
  non-zero only because `semgrep` is not installed on this host and there
  are pre-existing shellcheck findings in unrelated bash files — neither
  introduced by this change.)
- [x] ✅ **V4.2**: Deployed on host via
  `ansible-playbook playbooks/imports/optional/common/play-darktable-ai-build.yml`
  (2026-05-20, `PLAY RECAP … failed=0`). Note: the verification playbook
  is `play-darktable-ai-build.yml`, not `play-photography.yml` — the
  earlier reference predated the D1=A decision.
- [ ] ⬜ **V4.3**: Confirm `darktable` launches (the AI RPM *replaces* the
  stock `darktable` binary — there is no separate `darktable-ai` command on
  the D1=A path) and the **AI** tab is present in Preferences. *User-driven.*
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

A7V source-build criteria — **met**:

- [x] ✅ Source-built `darktable` RPM installs and replaces stock (A7V baked in)
- [x] ✅ No regression in the `darktable` RPM workflow (A7V still opens)
- [x] ✅ Plan committed alongside playbook changes per the "Plan Commit Rule"

AI criteria — **blocked on the darktable 5.6.0 release** (not achievable on
5.4.1; revisit once the AI path decision is made):

- [ ] 🚫 `darktable` launches with an **AI** tab in Preferences
- [ ] 🚫 Interactive AI object-mask tool (SAM2.1/SegNext) works on a RAW
- [ ] 🚫 At least one AI model downloads successfully and applies to a RAW
- [ ] 🚫 *(If Phase 3 done)* `darktable -d ai` reports CUDA provider active
- [ ] 🚫 *(If Phase 3 done)* `nvidia-smi` shows GPU usage during AI denoise/upscale

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

### 2026-05-19 — Phase 2 implementation

- Decision gate closed: D1=A (source-built RPM), D2=A1 (mock), D3=defer
  Phase 3 to a follow-up session.
- New playbook drafted at
  `playbooks/imports/optional/common/play-darktable-ai-build.yml`.
- Pinned `darktable_distgit_commit: f35a6d085e8f130f6eaa10976b353333602c2bbd` (current HEAD of Fedora
  `f43` branch) and `darktable_tarball_sha512` from the f43 `sources`
  lookaside file.
- Release tag bumped from the original plan's `1.ai.fc43` to
  `100.ai.fc43`. RPM version-compare on `1.ai.fc43` vs Fedora's
  `2.fc43` would have ranked our build LOWER (numeric `1` < `2`),
  causing `dnf upgrade` to silently downgrade to stock. The
  `100.ai.fc43` prefix sorts higher than any plausible single- or
  double-digit `%autorelease` from Fedora.
- T2.4 implemented as `Source5:` + `cp` line rather than `Patch5:`
  (deviation rationale captured in the task entry above).
- `play-photography.yml` overlay deletion (T2.7) is pending user
  confirmation before commit — per the execution prompt
  "Confirm before deleting the overlay block."
- Phase 3 (`play-darktable-ai-gpu.yml`) remains untouched in this
  session.

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

### 2026-05-20 — Phase 2 deploy failed, debug handoff to desktop-level agent

First host deploy of `play-darktable-ai-build.yml` failed in the mock
step (T2.5). A separate Ansible 2.19 YAML parse error in the task name
was fixed first (commit `1e74bdb`, working-tree clean now) — the
remaining failure is real.

**What works on host (verified by user's deploy attempt):**

- Preflight, install build tooling, mock-group add — all green
- Workspace tree creation — green
- Fedora f43 dist-git clone (commit `f35a6d0…`) — green
- Spec copy + Patch0 stage + tarball download (SHA-512 verified) +
  A7V cameras.xml download (SHA-256 verified) — green
- All four spec mutations + grep verification — green (the verify task
  passed, which means each of `Release: 100.ai%{?dist}`, `Source5: cameras.xml.a7v`, the cp line, and `-DUSE_AI=ON \` all landed in the
  spec)
- `rpmbuild -bs` SRPM build — green (the `.src.rpm` exists at
  `/var/tmp/darktable-ai-build/rpmbuild/SRPMS/darktable-5.4.1-100.ai.fc43.src.rpm`)

**What fails:**

- `mock --rebuild --root fedora-43-x86_64 --resultdir /var/lib/mock/fedora-43-x86_64/result --enable-network <srpm>` exits
  non-zero (rc=1) after ~100 s.
- Bootstrap chroot succeeds, minimal buildroot installs, then mock
  invokes `rpmbuild -bb --noclean --target x86_64 --nodeps /builddir/build/SPECS/darktable.spec` inside the chroot — that
  command is the one that fails.
- Mock's stderr to Ansible doesn't include the rpmbuild output. The
  actual failure reason is in:
  - `/var/lib/mock/fedora-43-x86_64/result/build.log` (rpmbuild
    stdout/stderr — %prep, %build, %install)
  - `/var/lib/mock/fedora-43-x86_64/result/root.log` (chroot population
    - BuildRequires resolution)

**Top suspect — DNS in chroot is off despite `--enable-network`:**

Mock's stderr from the host run shows the systemd-nspawn invocation
included `--resolv-conf=off`. That means cmake's `FindONNXRuntime.cmake`,
which fetches `https://github.com/microsoft/onnxruntime/releases/...`
during the build, gets no DNS and the build fails before it produces a
darktable binary.

If build.log shows "Could not resolve host" or "FindONNXRuntime: download
failed", the fix is one of:

1. Add `--config-opts=use_host_resolv=True` to the mock invocation in
   the playbook, OR
2. Add `--bind=/etc/resolv.conf` to the mock invocation, OR
3. Set `config_opts['use_host_resolv'] = True` in
   `/etc/mock/site-defaults.cfg` (system-wide change, prefer playbook
   options).

**Second suspect — spec mutation off-by-one on whitespace:**

The cmake-flag insertion regex anchors on the exact whitespace pattern
in the f43 spec. If something shifted, the resulting `%cmake \` block
could be missing a line-continuation backslash. The verify task only
checks four substrings exist, not that they are in the right cmake
block. Read
`/var/tmp/darktable-ai-build/rpmbuild/SPECS/darktable.spec` and inspect
the `%build` section's `%cmake \` block visually — every line except
the last should end with ` \` and the last should be
`-DRAWSPEED_ENABLE_LTO=ON` (no trailing backslash). Our `-DUSE_AI=ON \`
should sit BETWEEN `-DBUILD_CURVE_TOOLS=ON \` and
`-DRAWSPEED_ENABLE_LTO=ON`.

**Third suspect — BuildRequires unsatisfiable on f43:**

The spec lists `cmake(libavif) >= 0.9.3`, `libheif-devel >= 1.13.0`,
`libjxl-devel >= 0.7.0` etc. If f43 ships a version too old, root.log
will show "no providing package found" for that BuildRequires line.

**For the next agent picking this up:**

1. `cat /var/lib/mock/fedora-43-x86_64/result/build.log` — paste the
   last ~100 lines, this almost certainly identifies the failure.
2. If DNS, patch the mock invocation in
   `playbooks/imports/optional/common/play-darktable-ai-build.yml`
   (task "Build binary RPMs via mock") — add the appropriate flag.
3. If spec mutation, dump the rendered spec and fix the regex.
4. If BuildRequires, bump the pinned `darktable_distgit_commit` or
   amend the spec via another mutation task to soften the version
   constraint.

After fix: re-run the playbook. The SRPM is already built so that
step `creates:`-skips; only mock re-runs. To force a fresh mock
attempt after fixing config, run `mock --scrub=all --root fedora-43-x86_64` first (clears the chroot cache) — but this is rarely
needed unless you suspect chroot corruption.

The plan's Phase 2-alt (AppImage fallback) remains a viable retreat
path if the source build proves more trouble than it's worth — single
file install, no chroot, no BuildRequires, ~140 MB. Lose A7V baked in;
keep ART-style separate-binary install pattern.

### 2026-05-20 — mock failure root-caused and fixed

Read `/var/lib/mock/fedora-43-x86_64/result/build.log`. The build
reached 4–18% and then failed at a single target — rawspeed's
`validate-cameras.xml`:

```
src/external/rawspeed/data/cameras.xml:1345: Alias id 'EOS Hi' underruns minimum length 7
src/external/rawspeed/data/cameras.xml:17491: Camera make 'Phase One' not in enumeration set
src/external/rawspeed/data/cameras.xml fails to validate
gmake[2]: *** [...validate-cameras.xml...] Error 3
```

**The handoff's three suspects were all wrong.** cmake configured
fine (compilation got to 18%), ONNX Runtime downloaded fine over the
network, every BuildRequires resolved. The mock `--resolv-conf=off`
red herring was just nspawn's default — `--enable-network` worked.

**Actual cause**: T2.4's A7V overlay `cp`s the rawspeed-*develop*
`cameras.xml` over the source tree, but the build's
`validate-cameras.xml` step runs `xmllint --schema cameras.xsd cameras.xml` against the *older* `cameras.xsd` bundled in the
darktable 5.4.1 tarball. The newer xml has entries the older schema
rejects. The retired post-install overlay never hit this because it
ran after the build (no validation at install time).

**Fix applied to `play-darktable-ai-build.yml`:**

1. Overlay the matching `cameras.xsd` from the same pinned rawspeed
   commit (`rawspeed_cameras_xsd_*` vars, a `Source6:` declaration,
   and a second `cp` line in `%prep`). Confirmed locally with
   `xmllint` that develop `cameras.xml` validates against develop
   `cameras.xsd`. The xsd is build-validation-only — not compiled,
   not used at runtime — so no runtime risk.
2. Removed the SRPM step's `creates:` guard. It keyed on the
   version-release, so this very fix (a spec change with no VR bump)
   would have been masked by the stale SRPM, and mock would have
   rebuilt the *old* SRPM. `rpmbuild -bs` is cheap; mock retains its
   own guard.

**Next**: re-run `play-darktable-ai-build.yml` on the host. The stale
SRPM and the failed mock result dir do not need manual cleanup — the
spec is re-staged and re-mutated every run, the SRPM is now always
rebuilt, and mock's binary-RPM `creates:` guard sees no RPM so it
re-runs. `mock --scrub=all` is not needed (BuildRequires unchanged,
chroot cache is fine).

### 2026-05-20 — Phase 2 complete: build succeeds, RPM installed

Re-ran `play-darktable-ai-build.yml` on the host after the cameras.xsd
fix. `PLAY RECAP: ok=25 changed=11 failed=0`. Verified:

- `validate-cameras.xml` build step now reports `cameras.xml validates`
  (the exact target that failed before).
- `USE_AI=ON` confirmed in the cmake configure step of `build.log`.
- All eight RPMs produced in `/var/lib/mock/fedora-43-x86_64/result/`.
- Installed on the host: `darktable-5.4.1-100.ai.fc43`,
  `darktable-tools-noise-5.4.1-100.ai.fc43`,
  `darktable-tools-basecurve-5.4.1-100.ai.fc43`.

Phase 2 (T2.1–T2.9) is complete. Remaining work:

- **V4.3–V4.6** (user-driven GUI): launch `darktable`, open Preferences
  → AI tab, enable AI features, download a model (denoise is smallest),
  confirm it applies to a RAW. The AI RPM *replaces* the stock
  `darktable` — there is no separate `darktable-ai` binary on this path.
- **Phase 3** (NVIDIA GPU acceleration): deferred per decision D3, to a
  follow-up session.
- **Phase 5** (docs + cleanup): D5.1/D5.2/D5.3 still open.

> **Note (corrected below)**: the "USE_AI=ON confirmed" claim in this
> entry is wrong — see the 2026-05-20 AI-premise entry. The grep only
> matched the literal flag on the cmake command line; cmake actually
> listed `USE_AI` as an unused variable.

### 2026-05-20 — AI premise wrong: 5.4.1 has no AI; AI is a 5.6.0 feature

The user launched the installed build and found **no AI tab** in
Preferences. Investigation (documented in full in
[research-ai-version-correction.md](research-ai-version-correction.md)):

- darktable **5.4.1 has no AI/ONNX code at all** — verified by inspecting
  the `release-5.4.1` source tarball (no `FindONNXRuntime.cmake`, no
  `tools/ai/`, no `USE_AI` option, zero genuine `onnx` references).
- `build.log` confirms it: cmake listed `USE_AI` under "Manually-specified
  variables were not used by the project". cmake silently ignores unknown
  `-D` flags — `-DUSE_AI=ON` was a no-op.
- darktable's AI subsystem ships in **darktable 5.6.0**, per the upstream
  `RELEASE_NOTES.md`. 5.6.0 is **not released** — the GitHub releases API
  shows latest stable = `release-5.4.1`; only a `nightly` prerelease (from
  `master`) has AI. 5.6.0 is expected ~June 2026.
- Root cause of the plan error: `research.md` sourced its AI evidence from
  the `master` branch but attributed it to the `release-5.4.1` tag.

**What is genuinely done**: the Phase 2 build mechanism — mock chroot
build, spec mutation, the A7V `cameras.xml`+`cameras.xsd` build-time
overlay — works, and `darktable-5.4.1-100.ai.fc43` is installed with Sony
A7V support baked in. That value stands. The "AI" in the build is absent
because there is no AI in 5.4.1 to enable.

**The AI goal is blocked on the darktable 5.6.0 release.** Open decision
on how to proceed (a new decision gate, call it **D4**):

- **D4-A — Wait for 5.6.0** (~June 2026). Re-pin `darktable_version` to
  5.6.0; `-DUSE_AI=ON` then genuinely works. The mock build infra and the
  A7V overlay carry over unchanged (add a `libarchive-devel` BuildRequires
  per the upstream AI build notes). Stable, lowest-risk.
- **D4-B — Build from `master`/nightly now.** AI works immediately, but
  it is an unstable development snapshot; the spec needs rework (no Fedora
  dist-git spec tracks `master`).
- **D4-C — Install the upstream `nightly` AppImage.** No build; quickest
  way to try AI today; dev snapshot, separate binary, no A7V baked in.

The user's headline priority is the **AI object-mask tool** (SAM2.1 /
SegNext) — confirmed to be one of the 5.6.0 AI features. That raises the
value of Phase 3 (GPU acceleration), since SAM2.1 is heavy on CPU.

**Playbook naming**: `play-darktable-ai-build.yml` currently builds a
non-AI darktable. Rename / re-scope deferred until D4 is decided.

### 2026-05-20 — D4 decided: coexist (A7V RPM + AI nightly), AI path shipped

**Decision D4** (user): install **both** — keep the stable, A7V-enabled
darktable RPM, *and* add an AI-capable darktable alongside it. Try the AI
even though it is a development build. Proceed with the GPU phase too.

**Implementation — `play-darktable-ai-appimage.yml`** (new playbook):

- Installs the upstream darktable **nightly** as a separate `darktable-ai`
  app. The nightly is built from `master` with `-DUSE_AI=ON`; verified
  `AI -> ENABLED` in its compile options. The stable darktable RPM is
  untouched at `/usr/bin/darktable`.
- The nightly is a rolling release (asset filename embeds a git-describe
  string), so the playbook discovers the current asset from the GitHub
  `releases/tags/nightly` API at run time. A marker file inside the AppDir
  records which build is extracted, so re-runs are no-ops until upstream
  publishes a newer nightly.
- **Sony A7V**: the nightly's bundled rawspeed `cameras.xml` carries the
  A7V (ILCE-7M5) only as a non-functional stub
  (`supported="unknown-no-samples"`, no sensor data). darktable has **no
  user-level `cameras.xml` override** — confirmed from source
  (`imageio_rawspeed.cc` reads only `{datadir}/rawspeed/cameras.xml`). So
  the playbook **extracts the AppImage to a stable AppDir**
  (`/opt/darktable-ai/AppDir`) and overlays the working A7V `cameras.xml`
  — the same pinned rawspeed file (`aa9028af`) the RPM build uses. No
  `cameras.xsd` needed (runtime does not validate).
- **Verified**: `darktable-rs-identify` on a real A7V `.ARW` from the
  user's photo library returns rc=0 with full decode — model ILCE-7M5,
  7032×4688, black/white levels, Bayer CFA. A7V genuinely works.
- **Coexistence safety**: `darktable-ai` is a wrapper
  (`files/usr/local/bin/darktable-ai`) that runs the AppDir with an
  **isolated** `--configdir`/`--cachedir` (`~/.config/darktable-ai`), so
  the nightly cannot upgrade — and thereby corrupt — the library database
  used by the stable RPM. The AI build starts with its own empty library.

**Phase 2-alt status**: ✅ done (superseded task list T2alt.1–T2alt.8; see
the banner under that heading).

**Still open**:

- **Phase 3 — NVIDIA GPU acceleration** (user explicitly wants it). The AI
  models, especially the SAM2.1 object-mask tool, are heavy on CPU. The
  nightly bundles a CPU-only ONNX Runtime; GPU needs a separate
  GPU-enabled ORT (`~/.local/lib/onnxruntime-cuda/`), CUDA Toolkit 12.x/
  13.x, and cuDNN 9.x, with darktable pointed at it via `DT_ORT_LIBRARY`.
  See research.md §4 (sourced from `master`, still valid).
- Phase 5 docs/cleanup; `play-darktable-ai-build.yml` rename (it builds the
  A7V RPM, not an AI build — a `play-darktable-a7v-rpm.yml` rename is the
  tidy end state, deferred to avoid churn).

### 2026-05-20 — Phase 3 (GPU) research; CUDA/cuDNN sourcing is a decision gate

**Repo baseline (source of truth — `play-nvidia.yml`)**: installs the RPM
Fusion NVIDIA driver (`akmod-nvidia`) plus `xorg-x11-drv-nvidia-cuda` and
`xorg-x11-drv-nvidia-cuda-libs`. Those give the driver and the
driver-bundled CUDA libraries, but **not** the CUDA Toolkit runtime
(`libcudart`/`libcublas`/`libcufft`) and **not** cuDNN. Phase 3 must add
those two — and must do so **holistically**: without disturbing the RPM
Fusion driver `play-nvidia.yml` owns.

**GPU ONNX Runtime** (darktable `data/ort_gpu.json` + `tools/ai/install-ort-gpu.sh`):

- ORT **1.25.1**. CUDA 12.x → `onnxruntime-linux-x64-gpu-1.25.1.tgz`
  (sha256 `ddfc4ca4ccc9cd5345d3820edab710ee84e749569d052eed92c42693d3b448a8`,
  ~250 MB). CUDA 13.x → `onnxruntime-linux-x64-gpu_cuda13-1.25.1.tgz`
  (sha256 `ebc14e1290db2a30a7bb415bd1c3e1390a7816bb4db87677dc36d071ed22833c`,
  ~200 MB). URLs under `github.com/microsoft/onnxruntime/releases/`.
- Installs `libonnxruntime*.so*` to `~/.local/lib/onnxruntime-cuda/`.
- CUDA version detection cascade: `nvcc` → `/usr/local/cuda/version.json`
  → `version.txt` → `ldconfig -p | grep libcudart`.
- Needs the `PT_GNU_STACK` RWE→clear fix on the `.so` files (`execstack -c`,
  else an ELF `p_flags` patch clearing `PF_X`).
- darktable finds the lib via the `DT_ORT_LIBRARY` env var (or Preferences
  → AI → "detect"). The GPU playbook will export it in `/etc/profile.d/`.

**Decision gate D5 — how to source CUDA Toolkit runtime + cuDNN 9 on
Fedora 43 without breaking the RPM Fusion driver:**

- **negativo17** (`fedora-nvidia.repo`) — self-sustained, has `cuda` +
  `cudnn` RPMs, *but* it also ships its own NVIDIA driver that conflicts
  with the RPM Fusion `akmod-nvidia` already installed. Usable only if the
  driver packages are excluded — fragile, not cleanly holistic.
- **NVIDIA official CUDA repo** — installing the `cuda-toolkit` /
  `cudnn` meta-packages (NOT the umbrella `cuda` package) leaves the
  distro driver alone. Cleanest *if* NVIDIA publishes a `fedora43` repo
  (NVIDIA's repos lag Fedora releases — needs checking).
- **RPM Fusion** — does not package the full CUDA Toolkit or cuDNN
  (cuDNN's EULA precludes it), so RPM Fusion alone cannot supply Phase 3.

This is a genuine gate: a CUDA repo that pulls a second NVIDIA driver
would destabilise the display driver (plan Risk table: High impact).
Resolve D5 before writing `play-darktable-ai-gpu.yml`.

### 2026-05-21 — Phase 3 implemented (GPU acceleration)

**D5 resolved.** CUDA Toolkit from NVIDIA's official Fedora 43 repo
(verified to exist); cuDNN 9 from NVIDIA's official `nvidia-cudnn-cu13`
wheel (Fedora has no NVIDIA cuDNN dnf repo). Both NVIDIA sources; neither
pulls a competing driver. negativo17 rejected (ships its own driver).

Per user direction — "the nvidia play should handle nvidia; don't assume
darktable is the only thing that needs CUDA; do it properly" — CUDA is
installed as a **system capability** in `play-nvidia.yml`, not bundled
per-app.

**Part A — `play-nvidia.yml`** (gated by new var `nvidia_install_cuda`,
default `true`, mirroring `nvidia_install_vulkan`):

- Configures the NVIDIA CUDA Fedora repo via `yum_repository` with an
  `exclude` of every driver/kmod package — it can never replace the RPM
  Fusion `akmod-nvidia` driver. A post-install task asserts the RPM
  Fusion driver survived.
- Installs `cuda-toolkit` (the driverless toolkit meta-package).
- Installs cuDNN 9 from NVIDIA's official wheel (pinned 9.22.0.52 +
  sha256) into `/opt/cudnn`.
- Writes `/etc/ld.so.conf.d/nvidia-cuda.conf` and runs `ldconfig`.

**Part B — `play-darktable-ai-gpu.yml`** (new,
`hardware-specific/`): hardware-gated on PCI vendor `10de` (no-ops on the
non-NVIDIA laptop). Preflights CUDA/cuDNN/darktable-ai; fetches darktable's
`ort_gpu.json` at run time; selects the NVIDIA/linux entry matching the
detected CUDA major; downloads + SHA256-verifies the GPU ORT tarball;
installs the libs to `~/.local/lib/onnxruntime-cuda/`; clears the
`PT_GNU_STACK` RWE flag with `execstack`. Idempotent via a versioned-`.so`
guard.

**Part C — `files/usr/local/bin/darktable-ai`**: the launcher now
self-detects `~/.local/lib/onnxruntime-cuda/libonnxruntime.so` and exports
`DT_ORT_LIBRARY` + `LD_LIBRARY_PATH` when present; CPU fallback otherwise.
No `/etc/profile.d` global env change — GPU wiring stays scoped to
`darktable-ai`.

All three playbooks pass `ansible-playbook --syntax-check` and
`./scripts/qa-all.bash` (bash + python). **Pending**: host deploy of
`play-nvidia.yml` then `play-darktable-ai-gpu.yml`, and the
`darktable-ai -d ai` CUDA-provider check (V4.5/V4.6).

This supersedes the original Phase 3 task list T3.0–T3.12 above (which
predated the D5 resolution); the three-part design here is the
as-built record.
