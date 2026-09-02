# Plan 00042 — Decisions and As-Built Design

Durable decision rationale and the as-built design for the darktable AI work.
Extracted from the original plan document ([PLAN_archive.md](PLAN_archive.md)),
which remains the verbatim historical record. Research evidence lives in
[research.md](research.md) (original pass, partly wrong about versions) and
[research-ai-version-correction.md](research-ai-version-correction.md) (the
corrected evidence trail).

## D1 — Implementation path for the AI-enabled binary

**Context**: Fedora's `darktable` RPM is not built with `-DUSE_AI=ON`. An
AI-enabled binary was wanted on Fedora 43, with Sony A7V support preserved in
the same binary.

**Options considered**:

1. **Local source-built RPM** — fork the Fedora spec, add `-DUSE_AI=ON`, apply
   the A7V `cameras.xml` as a build-time patch. Pros: single binary, dnf
   workflow, A7V baked in, no AppImage filesystem quirks. Cons: a full rebuild
   per darktable release; needs `mock` or `rpmbuild`.
2. **Upstream AppImage** — install as `darktable-ai` alongside the RPM. Pros:
   zero build cost, upstream-supported, CPU ONNX Runtime bundled. Cons:
   separate binary, cannot patch A7V into the bundled rawspeed without AppDir
   surgery.
3. **Wait for Fedora to enable `-DUSE_AI=ON`** — no timeline.

**Decision**: Option 1. An earlier same-day conclusion (AppImage) was based on
an overestimate of the build cost; `cmake/modules/FindONNXRuntime.cmake`
auto-downloads ONNX Runtime, so the source build is one cmake flag away from
the Fedora spec. Option 2 was retained as the Phase 2-alt fallback.

**Outcome**: the build mechanism works and the RPM is installed, but darktable
5.4.1 contains no AI code, so the RPM's value is the A7V fix only (see D4).

## D2 — Build host strategy

**Options**: `mock` (isolated chroot, reproducible, slower first run) versus
direct `rpmbuild` on the host (simpler, but BuildRequires become permanent on
the host).

**Decision**: `mock`. The user is added to the `mock` group and the mock step
uses `become_flags: -i` so a login shell re-reads groups without logout.

## D3 — GPU acceleration in this plan or a follow-up

**Decision**: CPU-only first; Phase 3 deferred until Phase 2 was verified.
Superseded by D4, under which the user asked for the GPU phase to proceed.

## A7V cameras.xml as a build-time overlay (with D1)

**Context**: `play-photography.yml` used to patch A7V support into
`/usr/share/darktable/rawspeed/cameras.xml` as a post-install overlay.

**Decision**: move the override into the local spec and delete the
`darktable-a7v` block from `play-photography.yml`. Implemented as `Source5:` +
a `cp %{SOURCE5} src/external/rawspeed/data/cameras.xml` line after
`%autosetup -p1` rather than a `Patch5:` unified diff: a diff would have to be
generated at run time from the stock file inside the tarball and the
rawspeed-develop replacement, which is brittle and adds a step with no
functional benefit. Source5+cp produces the same in-tree state.

## Spec mutations and the release tag

- `-DUSE_AI=ON` is added to the Fedora-branch `%cmake` invocation (regex
  anchored on `-DRAWSPEED_ENABLE_LTO=ON\n%endif`, which matches only the
  Fedora arm, not the RHEL arm).
- `Release: %autorelease` becomes `Release: 100.ai%{?dist}`. RPM
  version-compare on `1.ai.fc43` versus Fedora's `2.fc43` would rank the
  local build lower and `dnf upgrade` would silently downgrade to stock; the
  `100.` prefix sorts above any plausible `%autorelease`.
- `%autochangelog` is replaced with a static entry (no rpkg-macros dependency).
- A `grep -qE` task verifies each mutation landed before the build runs.
- The SRPM step has no `creates:` guard. It used to key on version-release,
  so a spec fix without a VR bump would be masked by the stale SRPM and mock
  would rebuild the old one. `rpmbuild -bs` is cheap; mock keeps its own guard.

## Mock build failure — root cause (evidence)

The first host build failed at rawspeed's `validate-cameras.xml` target:

```
src/external/rawspeed/data/cameras.xml:1345: Alias id 'EOS Hi' underruns minimum length 7
src/external/rawspeed/data/cameras.xml:17491: Camera make 'Phase One' not in enumeration set
src/external/rawspeed/data/cameras.xml fails to validate
gmake[2]: *** [...validate-cameras.xml...] Error 3
```

The A7V overlay copies the rawspeed-develop `cameras.xml` over the source
tree, but the build validates it with `xmllint --schema cameras.xsd` against
the older `cameras.xsd` bundled in the 5.4.1 tarball. The hand-off's three
suspects (chroot DNS via `--resolv-conf=off`, spec-mutation whitespace,
unsatisfiable BuildRequires) were all wrong: cmake configured, ONNX Runtime
downloaded, every BuildRequires resolved.

**Fix**: overlay the matching `cameras.xsd` from the same pinned rawspeed
commit (`Source6:` + a second `cp` line in `%prep`). The xsd is
build-validation-only, so there is zero runtime risk. Verified locally with
`xmllint`; the re-deploy reported `cameras.xml validates` and produced all
eight RPMs.

## The AI premise was wrong — 5.4.1 has no AI

darktable 5.4.1 contains no AI/ONNX code at all; cmake listed `USE_AI` under
"Manually-specified variables were not used by the project". AI ships in
darktable 5.6.0, unreleased at the time; only the `nightly` prerelease built
from `master` has it. `research.md` sourced its AI evidence from `master` but
attributed it to the `release-5.4.1` tag. Full evidence:
[research-ai-version-correction.md](research-ai-version-correction.md).

## D4 — Coexist: A7V RPM plus AI nightly

**Options**: wait for 5.6.0 and re-pin the RPM build (stable, lowest risk);
build from `master` now (no Fedora spec tracks `master`); install the upstream
`nightly` AppImage (no build, dev snapshot, separate binary).

**Decision** (user): install both. Keep the stable, A7V-enabled darktable RPM
and add the AI-capable nightly alongside it as `darktable-ai`. Proceed with
the GPU phase too, since the headline feature (SAM2.1/SegNext object masks)
is heavy on CPU.

**As built — `play-darktable-ai-appimage.yml`**:

- Installs the upstream nightly (built from `master` with `-DUSE_AI=ON`,
  `AI -> ENABLED` verified in its compile options). The stable RPM stays at
  `/usr/bin/darktable`.
- The nightly is rolling (asset filename embeds a git-describe string), so
  the current asset is discovered from the GitHub `releases/tags/nightly` API
  at run time. A marker file in the AppDir records the extracted build, so
  re-runs are no-ops until upstream publishes a newer nightly.
- **Sony A7V**: the nightly's bundled `cameras.xml` carries the ILCE-7M5 only
  as a non-functional stub, and darktable has no user-level `cameras.xml`
  override (`imageio_rawspeed.cc` reads only `{datadir}/rawspeed/cameras.xml`).
  So the AppImage is extracted to a stable AppDir (`/opt/darktable-ai/AppDir`)
  and the working A7V `cameras.xml` (pinned rawspeed `aa9028af`, the same
  file the RPM build uses) is overlaid. No `cameras.xsd` is needed at runtime.
  Verified: `darktable-rs-identify` fully decodes a real A7V `.ARW`.
- **Coexistence safety**: `files/usr/local/bin/darktable-ai` is a wrapper
  that runs the AppDir with an isolated `--configdir`/`--cachedir`
  (`~/.config/darktable-ai`), so the nightly cannot upgrade and corrupt the
  library database used by the stable RPM.

## GPU acceleration in a separate hardware-specific playbook

**Decision**: `playbooks/imports/optional/hardware-specific/play-darktable-ai-gpu.yml`,
because it pulls in heavy NVIDIA-only dependencies that machines without
NVIDIA should not pay for.

## Multi-laptop safety via PCI vendor ID detection

**Context**: two laptops share this config; one has NVIDIA, one does not.

**Options**: `nvidia-smi` presence (unreliable: can be installed without the
GPU, or absent with it); `lspci` for PCI vendor ID `10de` (ground truth);
a per-host inventory variable (explicit, but per-host maintenance).

**Decision**: `lspci` vendor ID, set as a fact at the top of the GPU playbook
and used as `when:` on every subsequent task. The inventory variable is a
future option if hardware detection becomes cross-cutting.

## D5 — Sourcing CUDA Toolkit and cuDNN without breaking the driver

**Context**: `play-nvidia.yml` installs the RPM Fusion driver
(`akmod-nvidia`) plus `xorg-x11-drv-nvidia-cuda` libs, but not the CUDA
Toolkit runtime (`libcudart`, `libcublas`, `libcufft`) and not cuDNN.

**Options**:

- **negativo17** — has `cuda` and `cudnn` RPMs but ships its own NVIDIA
  driver that conflicts with `akmod-nvidia`. Rejected.
- **NVIDIA official CUDA repo** — `cuda-toolkit` (not the umbrella `cuda`
  package) leaves the distro driver alone. A `fedora43` repo exists.
- **RPM Fusion** — does not package the full toolkit or cuDNN (EULA).

**Decision**: CUDA Toolkit from NVIDIA's official Fedora 43 repo; cuDNN 9
from NVIDIA's official `nvidia-cudnn-cu13` wheel (Fedora has no NVIDIA cuDNN
dnf repo). Per user direction ("the nvidia play should handle nvidia; don't
assume darktable is the only thing that needs CUDA"), CUDA is installed as a
system capability in `play-nvidia.yml`, not per-app.

**As built (three parts)**:

- **`play-nvidia.yml`** (gated by `nvidia_install_cuda`, default `true`):
  configures the NVIDIA CUDA repo via `yum_repository` with an `exclude` of
  every driver/kmod package and asserts afterwards that the RPM Fusion driver
  survived; installs `cuda-toolkit`; installs cuDNN 9 from the pinned wheel
  (9.22.0.52 + sha256) into `/opt/cudnn`; writes
  `/etc/ld.so.conf.d/nvidia-cuda.conf` and runs `ldconfig`.
- **`play-darktable-ai-gpu.yml`**: hardware-gated on PCI vendor `10de`;
  preflights CUDA/cuDNN/darktable-ai; fetches darktable's `ort_gpu.json` at
  run time (upstream refreshes it, so it is not pinned); selects the
  NVIDIA/linux entry matching the detected CUDA major; downloads and
  SHA256-verifies the GPU ONNX Runtime tarball; installs the libs to
  `~/.local/lib/onnxruntime-cuda/`; clears the `PT_GNU_STACK` RWE flag with
  `execstack` (glibc 2.41+ refuses to dlopen executable-stack libs).
  Idempotent via a versioned-`.so` guard.
- **`files/usr/local/bin/darktable-ai`**: self-detects
  `~/.local/lib/onnxruntime-cuda/libonnxruntime.so` and exports
  `DT_ORT_LIBRARY` + `LD_LIBRARY_PATH` when present, CPU fallback otherwise.
  No global `/etc/profile.d` change; GPU wiring stays scoped to `darktable-ai`.

GPU ONNX Runtime reference data (from `ort_gpu.json`): ORT 1.25.1; CUDA 12.x
tarball `onnxruntime-linux-x64-gpu-1.25.1.tgz` (sha256
`ddfc4ca4ccc9cd5345d3820edab710ee84e749569d052eed92c42693d3b448a8`); CUDA 13.x
tarball `onnxruntime-linux-x64-gpu_cuda13-1.25.1.tgz` (sha256
`ebc14e1290db2a30a7bb415bd1c3e1390a7816bb4db87677dc36d071ed22833c`).

## Playbook naming

`play-darktable-ai-build.yml` builds the A7V RPM, not an AI build. A rename
to `play-darktable-a7v-rpm.yml` is the tidy end state, deferred to avoid
churn until the remaining phases close.

## Risks and mitigations

| Risk                                                   | Impact | Probability | Mitigation                                                                                      |
| ------------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------------------------------- |
| AppImage filesystem permissions / FUSE issues          | Low    | Low         | AppImage is extracted to an AppDir, so FUSE is not used at runtime                              |
| AI models too large to fit in 4 GB VRAM                | Medium | Low         | Upstream falls back to CPU automatically; document this                                         |
| CUDA Toolkit install conflicts with existing CUDA libs | High   | Medium      | NVIDIA repo with driver packages excluded; post-install assert on the RPM Fusion driver         |
| cuDNN distribution restrictions (NVIDIA EULA)          | Medium | Medium      | Official NVIDIA wheel, pinned with sha256                                                       |
| Models silently fail to download (network/firewall)    | Low    | Low         | Troubleshooting block; check `~/.config/darktable-ai/`                                          |
| Nightly build changes shape or breaks                  | Low    | Medium      | Isolated config dir protects the stable library; re-run the playbook to move to a newer nightly |
