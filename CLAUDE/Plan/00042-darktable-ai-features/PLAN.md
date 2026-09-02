# Plan 00042: Darktable AI Features

**Status**: In Progress (AI path delivered via Phase 2-alt; Phase 3 GPU implemented)
**Created**: 2026-05-19
**Owner**: joseph
**Priority**: Medium

> Slimmed on 2026-09-02. The full original plan, including every progress
> note and hand-off, is kept verbatim in [PLAN_archive.md](PLAN_archive.md).
> Decision rationale and the as-built design are in [DECISIONS.md](DECISIONS.md).

## Overview

darktable's AI subsystem (interactive SAM2.1/SegNext object masks, neural
denoise, upscale) is backed by ONNX Runtime and ships only in darktable 5.6.0
and the `nightly` prerelease built from `master`. The stable 5.4.1 release
this plan first targeted contains no AI code at all, so the source-built RPM
from Phase 2 delivers only the Sony A7V fix baked in at build time.

Per decision D4 the two coexist: the stable, A7V-enabled `darktable` RPM
(built by `play-darktable-ai-build.yml`) and the AI nightly installed as a
separate `darktable-ai` app (`play-darktable-ai-appimage.yml`) with the A7V
`cameras.xml` overlaid and an isolated config directory. Phase 3 adds NVIDIA
GPU acceleration: `play-nvidia.yml` installs the CUDA Toolkit and cuDNN
system-wide, and `play-darktable-ai-gpu.yml` installs the GPU ONNX Runtime
for `darktable-ai`. All three playbooks pass QA and syntax-check; host deploy
and the CUDA-provider verification are pending.

Supporting documents: [DECISIONS.md](DECISIONS.md) (decisions D1–D5, as-built
design, mock root cause, risks), [research.md](research.md) (original research,
partly wrong about versions), and
[research-ai-version-correction.md](research-ai-version-correction.md) (the
corrected evidence trail).

## Goals

- Get darktable AI features (object masks, denoise, upscale) working on this
  Fedora 43 machine
- Deliver via the existing IaC pattern with no manual install steps
- Provide an opt-in NVIDIA GPU acceleration path that no-ops cleanly on the
  non-NVIDIA laptop
- Preserve the Sony A7V support in both the stable RPM and the AI build
- Document the irreducibly manual final step (Preferences, AI tab, enable,
  download models) in the playbook success message

## Non-Goals

- Publishing a public COPR for others to consume
- AMD or Intel GPU acceleration (no such hardware on either laptop)
- Filing a Fedora RFE to enable `-DUSE_AI=ON` upstream
- Managing model files via Ansible (GUI-downloaded, per-user)
- Auto-enabling the AI preference (no safe way to write darktable's settings DB)

## Context & Background

- `playbooks/imports/optional/common/play-photography.yml` installs the Fedora
  `darktable` RPM; its former A7V post-install overlay moved into the source
  build (T2.7)
- `playbooks/imports/optional/hardware-specific/play-nvidia.yml` owns the RPM
  Fusion NVIDIA driver and, since Phase 3, the CUDA Toolkit and cuDNN
- The user has two laptops sharing this config; only one has an NVIDIA GPU

## Tasks

### Phase 1: Decision Gate

- [x] ✅ **D1**: Implementation path — local source-built RPM (mock), AppImage
  kept as the fallback. Rationale in [DECISIONS.md](DECISIONS.md#d1--implementation-path-for-the-ai-enabled-binary).
- [x] ✅ **D2**: Build host strategy — `mock`.
- [x] ✅ **D3**: CPU-only first; Phase 3 deferred (later reopened under D4).

### Phase 2: Source-Built `darktable` RPM (A7V baked in)

Playbook: `playbooks/imports/optional/common/play-darktable-ai-build.yml`.
Spec mutations, release-tag reasoning and the mock failure root cause are in
[DECISIONS.md](DECISIONS.md#spec-mutations-and-the-release-tag).

- [x] ✅ **T2.1**: Install build tooling (`mock`, user in the `mock` group,
  `become_flags: -i` on the mock step)
- [x] ✅ **T2.2**: Clone Fedora dist-git at pinned f43 commit
  `f35a6d085e8f130f6eaa10976b353333602c2bbd`; tarball SHA-512 pinned in vars
- [x] ✅ **T2.3**: Spec mutations (`-DUSE_AI=ON`, `Release: 100.ai%{?dist}`,
  static changelog, A7V overlay) with a `grep -qE` verification task
- [x] ✅ **T2.4**: A7V `cameras.xml` overlay as `Source5:` + `cp` in `%prep`
  (deviation from the planned `Patch5:`, see DECISIONS.md)
- [x] ✅ **T2.5**: Build SRPM then binary RPMs via `mock --rebuild --enable-network`. First run failed at `validate-cameras.xml`; fixed by
  overlaying the matching `cameras.xsd` (`Source6:`) and dropping the SRPM
  `creates:` guard
- [x] ✅ **T2.6**: Install the RPMs via `dnf` (`darktable-5.4.1-100.ai.fc43`
  plus `-tools-noise` and `-tools-basecurve` confirmed on host)
- [x] ✅ **T2.7**: Drop the `darktable-a7v` block and `rawspeed_*` vars from
  `play-photography.yml`; success message points at the build playbook
- [x] ✅ **T2.8**: `uri:` check against the upstream releases API to warn on a
  newer version
- [x] ✅ **T2.9**: Success message with the Preferences, AI tab, download
  models steps

### Phase 2-alt: AppImage Install — the AI delivery path (D4)

The original task list below was written for a pinned-release AppImage inside
`play-photography.yml` and is superseded: the release AppImage has no AI. The
delivered implementation is the standalone
`playbooks/imports/optional/common/play-darktable-ai-appimage.yml`, which
installs the rolling nightly, extracts it to `/opt/darktable-ai/AppDir`,
overlays the A7V `cameras.xml`, and runs via the isolated-config wrapper
`files/usr/local/bin/darktable-ai`. Design in
[DECISIONS.md](DECISIONS.md#d4--coexist-a7v-rpm-plus-ai-nightly). Original
tasks kept for the record:

- [ ] ⬜ **T2alt.1**: Pin `darktable_appimage_*` variables in `play-photography.yml`
- [ ] ⬜ **T2alt.2**: `stat:` check to skip download at target version
- [ ] ⬜ **T2alt.3**: `get_url:` with sha256 checksum and `mode: "0755"`
- [ ] ⬜ **T2alt.4**: Symlink `/usr/local/bin/darktable-ai` to the AppImage
- [ ] ⬜ **T2alt.5**: Desktop entry `darktable-ai.desktop` with distinct name and icon
- [ ] ⬜ **T2alt.6**: Icon extracted from the AppImage or fetched upstream
- [ ] ⬜ **T2alt.7**: `uri:` check against the releases API for a newer version
- [ ] ⬜ **T2alt.8**: Success message covering coexistence and the AI tab steps

### Phase 3: NVIDIA GPU Acceleration

The task list below predates the D5 resolution and is superseded by the
three-part as-built design (CUDA and cuDNN in `play-nvidia.yml` gated by
`nvidia_install_cuda`; GPU ONNX Runtime in
`playbooks/imports/optional/hardware-specific/play-darktable-ai-gpu.yml`
hardware-gated on PCI vendor `10de`; GPU self-detection in the
`darktable-ai` launcher). See
[DECISIONS.md](DECISIONS.md#d5--sourcing-cuda-toolkit-and-cudnn-without-breaking-the-driver).
Original tasks kept for the record:

- [ ] ⬜ **T3.0**: Hardware detection via `lspci -nn` and PCI vendor `10de`;
  every later task `when: has_nvidia_gpu`; informational `debug:` on no-op
- [ ] ⬜ **T3.1**: Preflight that `play-nvidia.yml` has run (driver package present)
- [ ] ⬜ **T3.2**: Install CUDA Toolkit 12.x
- [ ] ⬜ **T3.3**: Install cuDNN 9.x
- [ ] ⬜ **T3.4**: Fetch the ORT GPU manifest (`data/ort_gpu.json`) at run time
- [ ] ⬜ **T3.5**: Detect installed CUDA major.minor
- [ ] ⬜ **T3.6**: Pick the manifest entry bracketing the detected version
- [ ] ⬜ **T3.7**: `get_url:` the tarball and verify its SHA256
- [ ] ⬜ **T3.8**: Unarchive and copy `libonnxruntime*.so*` into the user lib dir
- [ ] ⬜ **T3.9**: Clear the `PT_GNU_STACK` RWE flag on the `.so` files
- [ ] ⬜ **T3.10**: Export `DT_ORT_LIBRARY` for the user
- [ ] ⬜ **T3.11**: Print verification steps (`darktable-ai -d ai` CUDA provider)
- [ ] ⬜ **T3.12**: Document runtime fall-through to CPU

### Phase 4: Verification

(On host, not in the CCY container.)

- [x] ✅ **V4.1**: `./scripts/qa-all.bash` bash and python stages pass;
  `ansible-playbook --syntax-check` passes
- [x] ✅ **V4.2**: `play-darktable-ai-build.yml` deployed on host,
  `PLAY RECAP … failed=0`
- [ ] ⬜ **V4.3**: Confirm `darktable-ai` launches with the AI tab in
  Preferences. *User-driven.*
- [ ] ⬜ **V4.4**: Download one model (denoise is smallest) and confirm it
  works on a real RAW file
- [ ] ⬜ **V4.5**: Deploy `play-nvidia.yml` then `play-darktable-ai-gpu.yml`;
  confirm `darktable-ai -d ai` reports the CUDA provider
- [ ] ⬜ **V4.6**: Smoke test denoise on a noisy ISO-6400 RAW; confirm quality
  improvement and GPU usage in `nvidia-smi`

### Phase 5: Documentation & Cleanup

- [ ] ⬜ **D5.1**: Update `docs/` (photography page) or add one
- [ ] ⬜ **D5.2**: Add a troubleshooting block to the playbook (AI tab
  missing, models will not download, GPU not detected after Phase 3)
- [ ] ⬜ **D5.3**: Rename `play-darktable-ai-build.yml` to reflect that it
  builds the A7V RPM, mark the plan Complete, move to `Completed/`, update
  `CLAUDE/Plan/README.md`

## Dependencies

- **Depends on**: `play-photography.yml`, `play-nvidia.yml` (Phase 3)
- **Blocks**: none
- **Related**: Plan 00037 (watermarking, sibling photography work), no overlap

## Success Criteria

A7V source-build criteria, met:

- [x] ✅ Source-built `darktable` RPM installs and replaces stock (A7V baked in)
- [x] ✅ No regression in the `darktable` RPM workflow (A7V still opens)
- [x] ✅ Plan committed alongside playbook changes per the Plan Commit Rule

AI criteria, blocked on the darktable 5.6.0 release for the RPM path and
awaiting host verification on the nightly path:

- [ ] 🚫 `darktable` launches with an **AI** tab in Preferences
- [ ] 🚫 Interactive AI object-mask tool (SAM2.1/SegNext) works on a RAW
- [ ] 🚫 At least one AI model downloads successfully and applies to a RAW
- [ ] 🚫 *(If Phase 3 done)* `darktable -d ai` reports CUDA provider active
- [ ] 🚫 *(If Phase 3 done)* `nvidia-smi` shows GPU usage during AI denoise/upscale

## Risks & Mitigations

See the risk table in [DECISIONS.md](DECISIONS.md#risks-and-mitigations).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00042-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 2 build playbook delivered; Ansible 2.19 task-name parse fix in `1e74bdb`
- Mock build root-caused (`cameras.xsd` schema mismatch) and fixed; A7V RPM installed on host
- AI premise corrected: 5.4.1 has no AI; D4 chose coexistence with the nightly
- `play-darktable-ai-appimage.yml` and the `darktable-ai` wrapper delivered
- Phase 3 GPU path implemented across `play-nvidia.yml`, `play-darktable-ai-gpu.yml` and the launcher; host deploy pending
- Plan slimmed; history in [PLAN_archive.md](PLAN_archive.md)
