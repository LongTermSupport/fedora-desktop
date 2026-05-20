# Research Correction — Darktable AI Features Are Not in 5.4.1

**Date**: 2026-05-20
**Trigger**: After Phase 2 deployed, the user launched darktable and found **no
AI tab in Preferences**. Investigation showed the original `research.md` was
wrong about which darktable version has AI.
**Status of original `research.md`**: superseded on the version question — see
"What `research.md` got wrong" below. Its GPU-path (Gate C) detail is still
broadly accurate because it was always sourced from `master`.

---

## TL;DR

- **darktable 5.4.1 has no AI features at all.** No ONNX code, no `USE_AI`
  cmake option, no `tools/ai/`. Confirmed by inspecting the actual
  `release-5.4.1` source tarball *and* by upstream documentation.
- **AI features land in darktable 5.6.0**, which is **not released yet**
  (expected ~June 2026 on darktable's ~6-month cadence).
- The build flag `-DUSE_AI=ON` (or `./build.sh --enable-ai`) is real — but
  only on the `master` branch / the upcoming 5.6.0, not on 5.4.1.
- Plan 00039 pinned **5.4.1**, the latest *stable* release. The Phase 2 mock
  build succeeded mechanically and produced a working darktable 5.4.1 with the
  Sony A7V fix — but `-DUSE_AI=ON` was silently ignored, so it has no AI.

---

## Evidence

### 1. The `release-5.4.1` source tarball contains zero AI/ONNX code

Inspected `darktable-5.4.1.tar.xz` (the exact tarball the playbook builds,
SHA-512 pinned, from
`github.com/darktable-org/darktable/releases/download/release-5.4.1/`):

- No `cmake/modules/FindONNXRuntime.cmake`.
- No `tools/ai/` directory.
- No `USE_AI` / `enable-ai` / `onnxruntime` token anywhere in the source.
- The only `onnx` substring matches are base64 noise inside `.svg` pixmaps —
  zero genuine references in `.c` / `.h` / `.cmake` / `CMakeLists.txt`.
- `src/iop/` has only the classic, decade-old `denoiseprofile.c` /
  `rawdenoise.c` modules — not the AI "neural restore" module.

### 2. The build proved `USE_AI` is not a real option in 5.4.1

In `/var/lib/mock/fedora-43-x86_64/result/build.log`, cmake's configure
output ends with:

```
CMake Warning:
  Manually-specified variables were not used by the project:
    ...
    USE_AI
    USE_GEO
```

cmake **silently ignores** unknown `-D` variables — it does not error. So
`-DUSE_AI=ON` set a cache entry that nothing reads. `darktable --version`
on the installed build confirms it: the "Compile options" table lists
Exiv2/Lensfun/OpenMP/OpenCL/Lua/etc. with **no AI line**.

> Verification lesson: grepping the literal string `USE_AI=ON` out of the
> build log only confirms it was on the command line — it does **not**
> confirm cmake used it. The real check is cmake's "variables were not used"
> warning, or the `darktable --version` compile-options table.

### 3. AI is an official darktable 5.6.0 feature

`RELEASE_NOTES.md` on the `master` branch (the draft notes for the next
release) describes the AI subsystem as new in **5.6.0**:

- "Added optional AI subsystem (build with `-DUSE_AI=ON`)" — features
  disabled by default, enabled at runtime without restart.
- New **neural restore** module in the lighttable/darkroom sidebar covering
  three AI tasks: **raw denoise, image denoise, upscale**.
- Denoisers: NIND UNet, NAFNet, RawNIND UtNet2. Upscale: BSRGAN 2x/4x.
- Interactive AI **object mask** tool using SAM2.1 and SegNext models.
- Inference via the **ONNX Runtime** backend; GPU acceleration via CUDA,
  ROCm/MIGraphX, DirectML, OpenVINO, CoreML execution providers.

### 4. darktable 5.6.0 is not released yet

GitHub releases API (`api.github.com/repos/darktable-org/darktable/releases`),
fetched 2026-05-20:

| Tag             | Name                             | Published  | Status         |
| --------------- | -------------------------------- | ---------- | -------------- |
| `nightly`       | Darktable nightly build 20260520 | 2026-05-20 | **prerelease** |
| `release-5.4.1` | release 5.4.1                    | 2026-02-05 | full release   |
| `release-5.4.0` | release 5.4.0                    | 2025-12-21 | full release   |
| `release-5.2.1` | release 5.2.1                    | 2025-08-06 | full release   |
| `release-5.2.0` | release 5.2.0                    | 2025-06-21 | full release   |

- **No `release-5.6.0` exists.** Latest stable is 5.4.1.
- The only artifact with AI today is the `nightly` prerelease, built from
  `master`.
- Cadence (5.2.0 Jun 2025 → 5.4.0 Dec 2025 → ~6 months) puts **5.6.0 around
  June 2026**.
- This matches the playbook's own "warn if newer release" task, which was
  *skipped* on deploy — `releases/latest` returns `release-5.4.1`.

### 5. Build requirements for an AI-enabled build (master / 5.6.0)

From `tools/ai/README.md` and the pixls.us "Building darktable with AI" thread:

- Flag: `-DUSE_AI=ON`, or `./build.sh --enable-ai`.
- ONNX Runtime: darktable bundles a **CPU-only ORT** on Linux (loads ORT
  `1.24.4`). cmake auto-downloads it from GitHub at configure time unless a
  system `onnxruntime-dev` package is present; the auto-download can hit
  GitHub rate limits.
- Extra build dependency reported on Linux: **libarchive** dev package
  (`libarchive-devel` on Fedora) — not in the current 5.4.1 Fedora spec.
- Models are **not bundled** — downloaded at runtime from the Preferences →
  AI tab. Model repo: `github.com/darktable-org/darktable-ai`.
- GPU acceleration: separate GPU-enabled ORT install via the scripts in
  `tools/ai/` (see original `research.md` §4 — that detail was always
  sourced from `master` and remains valid for 5.6.0).

---

## What `research.md` got wrong

The original research drew its AI evidence from URLs on the **`master`
branch** but attributed it to the **`release-5.4.1`** tag:

- §3c claimed `Darktable-5.4.1-x86_64.AppImage` is built with `--enable-ai`.
  The `appimage-build-script.sh` it quoted is from `master`. The actual
  5.4.1 AppImage, built from the `release-5.4.1` tag, has no AI (5.4.1 has
  no AI code to enable).
- §3e claimed "the darktable cmake module `FindONNXRuntime.cmake`
  auto-downloads ONNX Runtime" — that file is on `master`; it does not
  exist in the 5.4.1 tarball.
- The "Source of truth" header (`@ master, release-5.4.1`) names both refs
  but the body never separates them — that conflation is the root error.

The plan's Gate-A table ("Upstream AppImage: YES" for AI) is therefore
wrong for 5.4.1 and correct only for `master` / a future 5.6.0 build.

---

## Implications for Plan 00039

- **The Phase 2 playbook cannot deliver AI while pinned to 5.4.1.** The flag
  is a no-op there.
- **The Sony A7V build-time fix is genuine and still valuable** — the
  `cameras.xml` + `cameras.xsd` overlay correctly bakes A7V support into a
  source-built RPM, independent of AI.
- The plan needs a real correction: either re-pin to 5.6.0 once released, or
  re-target `master`/nightly. The build infrastructure (mock, spec mutation,
  A7V xsd overlay) is reusable for either — only the version pin and a small
  BuildRequires addition (`libarchive-devel`) change.

### Three paths forward (decision pending with the user)

1. **Wait for darktable 5.6.0** (~June 2026). Re-pin the playbook to 5.6.0;
   `-DUSE_AI=ON` then genuinely produces AI. Stable, lowest-risk. The A7V
   overlay and mock build infra carry over unchanged.
2. **Build from `master` / nightly now.** AI works immediately, but it is a
   development snapshot — unstable, the AI feature itself is under active
   development, and the spec needs rework (non-release version string, the
   `libarchive-devel` BuildRequires, no Fedora dist-git spec for `master`).
3. **Install the upstream `nightly` AppImage** (built from `master` with AI).
   No build cost, quickest way to try AI — but a dev snapshot, a separate
   binary, and no Sony A7V baked in.

---

## Sources

- darktable RELEASE_NOTES (master / draft 5.6.0 notes):
  <https://github.com/darktable-org/darktable/blob/master/RELEASE_NOTES.md>
- darktable releases API (release status, dates):
  <https://api.github.com/repos/darktable-org/darktable/releases>
- darktable AI tooling + docs:
  <https://github.com/darktable-org/darktable/tree/master/tools/ai>
- darktable AI model repository:
  <https://github.com/darktable-org/darktable-ai>
- pixls.us — "Building darktable with AI":
  <https://discuss.pixls.us/t/building-darktable-with-ai/57790>
- pixls.us — "GPU acceleration for AI features in Darktable":
  <https://discuss.pixls.us/t/gpu-acceleration-for-ai-features-in-darktable-help-needed-testing-install-scripts/56941>
- Local evidence: `release-5.4.1` source tarball inspection;
  `/var/lib/mock/fedora-43-x86_64/result/build.log` (cmake "variables not
  used" warning); `darktable --version` compile-options table.
