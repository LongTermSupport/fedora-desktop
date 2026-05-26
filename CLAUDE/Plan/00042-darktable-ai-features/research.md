# Research — Darktable AI Features on Fedora 43

> **⚠️ SUPERSEDED ON THE VERSION QUESTION (2026-05-20).** This document
> conflates the darktable `master` branch with the `release-5.4.1` tag. Its
> central claim — that darktable 5.4.1 has AI behind a build flag, and that
> `Darktable-5.4.1-x86_64.AppImage` is built with `--enable-ai` — is **wrong**.
> darktable 5.4.1 has **no AI code**; AI ships in **darktable 5.6.0**
> (unreleased, ~June 2026). The `FindONNXRuntime.cmake` / `--enable-ai` /
> `tools/ai/` evidence cited here all comes from `master`, not 5.4.1. See
> [research-ai-version-correction.md](research-ai-version-correction.md) for
> the corrected findings. The GPU-path detail in §4 was always sourced from
> `master` and remains broadly valid for a future 5.6.0 build.

**Date**: 2026-05-19
**Source of truth**: <https://github.com/darktable-org/darktable> @ master, release-5.4.1

This file captures everything learned during the initial research pass so the
PLAN can be evaluated against concrete facts rather than re-derived later.

---

## 1. What darktable's "AI features" actually are

Per the upstream README (section "AI features (optional)") darktable ships AI
support for:

- **Object masks** (segmentation — auto-select sky, subject, etc.)
- **Denoise**
- **Upscale**

The inference engine is **ONNX Runtime** (ORT). Models are **NOT bundled** — the
user downloads them at runtime from the **Preferences → AI** tab in the GUI.

GPU memory floor: 4 GB VRAM. If GPU inference fails (OOM, unsupported op, EP
crash), darktable falls back to CPU automatically.

---

## 2. The three gates between a user and working AI

| Gate                                                         | Provided by          | Linux state                                              |
| ------------------------------------------------------------ | -------------------- | -------------------------------------------------------- |
| **A. Binary compiled with `-DUSE_AI=ON`**                    | Distro / packager    | **Fedora RPM: NO. Flatpak: NO. Upstream AppImage: YES.** |
| **B. AI features enabled in preferences + model downloaded** | User, runtime GUI    | Manual, one-off per machine                              |
| **C. (Optional) GPU-enabled ONNX Runtime installed**         | User, install script | Manual, replicable in Ansible                            |

All three gates must be satisfied for GPU-accelerated AI. **Only A + B are
needed for CPU AI**, which already works "for free" if A is satisfied.

---

## 3. Gate A — which Linux build has AI compiled in?

### 3a. Fedora RPM (rawhide spec, version 5.4.1)

Source: `https://src.fedoraproject.org/rpms/darktable/raw/rawhide/f/darktable.spec`

```
%cmake \
    -DCMAKE_LIBRARY_PATH:PATH=%{_libdir} \
    -DUSE_GEO:BOOLEAN=ON \
    -DCMAKE_BUILD_TYPE:STRING=Release \
    -DBINARY_PACKAGE_BUILD=1 \
    -DBUILD_NOISE_TOOLS=ON \
    -DBUILD_CURVE_TOOLS=ON \
    -DRAWSPEED_ENABLE_LTO=ON
```

**No `-DUSE_AI=ON`.** The Fedora-shipped `darktable` package has zero AI code
compiled in. Enabling AI on this build is impossible — preferences will not
contain an AI tab.

### 3b. Flatpak (`org.darktable.Darktable`)

Source: `https://raw.githubusercontent.com/flathub/org.darktable.Darktable/master/org.darktable.Darktable.json`

```
"config-opts": [
  "-DCMAKE_BUILD_TYPE=RelWithDebInfo",
  "-DBINARY_PACKAGE_BUILD=1",
  "-DTESTBUILD_OPENCL_PROGRAMS=OFF"
]
```

**No `-DUSE_AI=ON`.** Same story — AI is not compiled in.

### 3c. Upstream AppImage (`Darktable-5.4.1-x86_64.AppImage`)

Source: `https://raw.githubusercontent.com/darktable-org/darktable/master/tools/appimage-build-script.sh`

```bash
./build.sh --enable-ai --build-dir ./build/ --prefix /usr --build-type Release \
    $@ --install -- "-DBINARY_PACKAGE_BUILD=1 -DBUILD_CURVE_TOOLS=ON ..."
```

And in the same script:

```bash
ORT_LIB_DIR=$(cmake -LA -N ../build 2>/dev/null | grep ONNXRuntime_LIB_DIR:PATH | cut -d= -f2)
cp -a "$ORT_LIB_DIR"/libonnxruntime*.so* ../AppDir/usr/lib/
```

**YES** — the official upstream AppImage is built with `--enable-ai` AND
bundles a CPU-only ONNX Runtime via dlopen. This is the only Linux distribution
of darktable 5.4.1 with AI features available out of the box.

### 3d. Fedora COPR alternatives

Searched COPR (`copr.fedorainfracloud.org/api_3/project/search?query=darktable`)
— **no COPR exists that ships darktable with `-DUSE_AI=ON`**. Existing COPRs:

- `germano/darktable` — marked "Don't use" by the Fedora maintainer himself
- `mjg/darktable` — experimental
- `mkrupcale/darktable` — for CR3 raw support, not AI
- A long tail of stale F23/F28 backports

### 3e. Local source build (added after follow-up research)

After being prompted to evaluate this seriously, the build-from-source path
turns out to be **much simpler than it first appeared**.

Source: `https://raw.githubusercontent.com/darktable-org/darktable/master/cmake/modules/FindONNXRuntime.cmake`

The cmake module that resolves ONNX Runtime at configure time has four
search modes, in order:

1. `src/external/onnxruntime/` — manual pre-install in source tree
2. `_deps/onnxruntime/` — auto-download destination (re-use prior download)
3. System-installed `libonnxruntime-dev`-style package
4. **Auto-download from upstream GitHub releases** (current default version: `1.24.4`)
   unless `-DONNXRUNTIME_OFFLINE=ON` is passed

This means a source build needs **no extra build-time dependencies** beyond
what the Fedora spec already pulls in — cmake just downloads ORT during
configure. The bundled ORT (~250 MB) ends up at `_deps/onnxruntime/lib/`
and the produced binary loads it via dlopen (`ORT_LAZY_LOAD`).

The `build.sh --enable-ai` upstream helper is simply a wrapper that adds
`-DUSE_AI=ON` to the cmake invocation. There is no separate AI codepath
in the build system — one flag, one rebuild.

### Conclusion for Gate A

Two viable paths, ranked by cost:

1. **Local source build with `-DUSE_AI=ON`** — fork the Fedora spec, add
   the cmake flag, apply the A7V cameras.xml patch as a build-time
   patch (cleaner than the current post-install overlay), build via
   `mock` or `rpmbuild`. ~10–20 min build per release. Single RPM, no
   AppImage tradeoffs, A7V support baked in.
2. **Upstream AppImage** — `Darktable-5.4.1-x86_64.AppImage`. Zero build
   cost; can't easily patch the bundled rawspeed for A7V.

The previously-feared "track every dep over time" cost is overstated: the
Fedora spec is already well-maintained, and our delta is one cmake flag
plus one cherry-picked patch. We rebase the spec from Fedora rawhide each
darktable release. Total ongoing cost: ~15 min per major darktable release
(currently four per year).

---

## 4. Gate C — GPU ONNX Runtime install (NVIDIA path)

The user has NVIDIA (confirmed via `playbooks/imports/optional/hardware-specific/play-nvidia.yml`,
which installs `xorg-x11-drv-nvidia-cuda` and `xorg-x11-drv-nvidia-cuda-libs`).

### 4a. Prerequisites (from `tools/ai/README.md`)

- Pascal-or-newer NVIDIA GPU (compute 6.0+)
- Driver 525+ — already satisfied by `akmod-nvidia` from `play-nvidia.yml`
- **CUDA Toolkit 12.x or 13.x** — NOT installed by `play-nvidia.yml`. The
  Fedora `xorg-x11-drv-nvidia-cuda` package ships only the *runtime libraries*
  needed by Vulkan/NVENC, not the developer toolkit.
- **cuDNN 9.x** — NOT installed by `play-nvidia.yml`. This is a separate
  NVIDIA download requiring a developer-account-style EULA acceptance for the
  upstream package; the RPM Fusion `cuda-cudnn` package partially fills the gap.

### 4b. The manifest and which package we'd download

Source: `https://raw.githubusercontent.com/darktable-org/darktable/master/data/ort_gpu.json`

For NVIDIA on Linux x86_64:

| CUDA range | ORT version | URL                                           | Size   | SHA256                                                             |
| ---------- | ----------- | --------------------------------------------- | ------ | ------------------------------------------------------------------ |
| 12.0–12.99 | 1.25.1      | `onnxruntime-linux-x64-gpu-1.25.1.tgz`        | 250 MB | `ddfc4ca4ccc9cd5345d3820edab710ee84e749569d052eed92c42693d3b448a8` |
| 13.0–13.99 | 1.25.1      | `onnxruntime-linux-x64-gpu_cuda13-1.25.1.tgz` | 200 MB | `ebc14e1290db2a30a7bb415bd1c3e1390a7816bb4db87677dc36d071ed22833c` |

The install script (`tools/ai/install-ort-gpu.sh`) extracts `libonnxruntime*.so*`
from the tarball into `~/.local/lib/onnxruntime-gpu/` (vendor-specific
`install_subdir` from the manifest).

### 4c. What the install script does (Ansible-replicable steps)

1. Detect GPU vendor (NVIDIA via `nvidia-smi`, AMD via `/opt/rocm`, Intel via `lspci`).
2. Detect CUDA major.minor via `nvcc`, `/usr/local/cuda/version.json`, or `ldconfig -p | grep libcudart`.
3. Read manifest, select package whose `cuda_min`/`cuda_max` brackets the
   detected CUDA version.
4. Download tarball, verify SHA256.
5. Extract `libonnxruntime*.so*` to `~/.local/lib/onnxruntime-gpu/`.
6. Clear `PT_GNU_STACK` RWE flag on the `.so` files (glibc ≥ 2.41 refuses to
   dlopen libs with executable stack). **Fedora 43 ships glibc 2.40 currently
   but glibc 2.41 is imminent — do not skip this step.**
7. Print path for user to paste into Preferences → AI, or to set as
   `DT_ORT_LIBRARY` env var.

All seven steps are trivially replicated in Ansible with `get_url`, `unarchive`,
`find`, and a tiny `command:` step for the execstack fix.

### 4d. Verification commands

```bash
# Verify ORT detection from darktable side:
darktable -d ai

# Expected log lines on success:
#   [darktable_ai] loaded ORT 1.25.1 from '...libonnxruntime.so.1.25.1'
#   [darktable_ai] execution provider: CUDA
#   [darktable_ai] NVIDIA CUDA enabled successfully on device 0: <gpu name>
```

---

## 5. Gate B — preferences

After AppImage install (Gate A) the user must:

1. Open darktable preferences (`Ctrl+,`)
2. Go to **AI** tab
3. Enable AI features
4. Click **Download models** for the features they want (denoise, upscale,
   segmentation)
5. (If Gate C done) Click **detect** to point darktable at the GPU ORT lib

These are GUI-only — no Ansible-managed configuration here. Document in the
playbook output and in the user-facing docs.

Model storage location (worth knowing for backup purposes): models are
typically stored under `~/.config/darktable/ai/` or `~/.cache/darktable/`
(needs confirmation from a live install).

---

## 6. Existing repo context that constrains the implementation

- `playbooks/imports/optional/common/play-photography.yml` already installs the
  Fedora `darktable` RPM AND patches its `rawspeed/cameras.xml` for Sony A7V
  support. **If we replace the RPM with the AppImage, the A7V cameras.xml
  bodge must be moved or duplicated** — the AppImage has its own bundled
  rawspeed and ignores `/usr/share/darktable/rawspeed/cameras.xml`.
- The ART AppImage in the same playbook is the established pattern for
  AppImage installs: download to `/opt/<name>-<version>.AppImage`, symlink
  into `/usr/local/bin/`, install icon + desktop entry. Reuse this verbatim.
- The user has NVIDIA — GPU acceleration is realistic. Cards bought from 2016
  onward (Pascal) are supported.

---

## 7. Open questions for Phase 1

1. **Replace RPM or coexist?** Options:
   - (a) Uninstall the Fedora RPM, install AppImage only
   - (b) Keep RPM, install AppImage alongside (e.g., `darktable-ai` shim)
   - (c) Skip AppImage entirely, file a Fedora RFE to add `-DUSE_AI=ON`
2. **A7V cameras.xml bodge** — needs new patching path for the AppImage's
   internal rawspeed data, or we live without A7V support on the AppImage and
   tell the user to keep the RPM for that camera.
3. **GPU vs CPU** — is the user willing to invest in CUDA Toolkit + cuDNN
   (~3 GB download) for GPU acceleration, or is CPU inference fast enough on
   their hardware?
4. **AppImage vs build-from-source RPM in a personal COPR** — has the user
   already considered the maintenance trade-off?

These are decision-gate questions for the PLAN's Phase 1.

---

## 8. Useful URLs and commands (for the implementing playbook)

```
# Upstream AppImage download
https://github.com/darktable-org/darktable/releases/download/release-5.4.1/Darktable-5.4.1-x86_64.AppImage
https://github.com/darktable-org/darktable/releases/download/release-5.4.1/Darktable-5.4.1-x86_64.AppImage.zsync

# GPU ORT manifest (always fetch fresh — pinned per ORT release):
https://raw.githubusercontent.com/darktable-org/darktable/refs/heads/master/data/ort_gpu.json

# GPU ORT install script (reference only — replicate in Ansible):
https://raw.githubusercontent.com/darktable-org/darktable/refs/heads/master/tools/ai/install-ort-gpu.sh

# AppImage update check API:
https://api.github.com/repos/darktable-org/darktable/releases/latest

# Verify provider on user side:
darktable -d ai 2>&1 | grep -i "execution provider\|loaded ORT\|enabled successfully"
```

---

## 9. Risks identified during research

| Risk                                                        | Severity | Notes                                                                                                                                                                                              |
| ----------------------------------------------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AppImage's bundled rawspeed lacks Sony A7V support          | Medium   | The A7V bodge in `play-photography.yml` patches `/usr/share/darktable/rawspeed/cameras.xml`; AppImage uses its own bundled copy. Need to either patch inside the AppImage or accept lost coverage. |
| CUDA Toolkit install requires NVIDIA repo + EULA acceptance | Medium   | `cuda-toolkit` is ~3 GB; need to use the NVIDIA official repo or RPM Fusion `cuda` package. RPM Fusion's `xhmikosr/cuda` and the official NVIDIA CUDA repo are both feasible.                      |
| `glibc 2.41` execstack rejection                            | Medium   | The install script handles this; Ansible playbook must replicate the `execstack -c` / Python ELF patch step. Fedora 43 currently ships glibc 2.40 but 2.41 is coming.                              |
| Models download from upstream, no caching                   | Low      | Models are fetched once via the GUI. If upstream is down, no model = no AI. Acceptable.                                                                                                            |
| AppImage GPU fall-through quirks                            | Low      | AppImage's bundled ORT is CPU-only; darktable's `DT_ORT_LIBRARY` env var (or preferences "browse" button) overrides it transparently.                                                              |
| AppImage filesystem sandbox (FUSE)                          | Low      | Fedora has fuse2 by default; AppImage runs fine. Already validated by the ART AppImage in `play-photography.yml`.                                                                                  |
