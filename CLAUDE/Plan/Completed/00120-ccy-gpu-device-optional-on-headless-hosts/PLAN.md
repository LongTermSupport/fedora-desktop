# Plan 00120: ccy starts on a host with no GPU

**Status**: Complete
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

ccy hands every container `--device /dev/dri:/dev/dri` unconditionally. On a headless server
or a serial-console VM no DRM driver is loaded, `/dev/dri` does not exist, and podman aborts the
run before Claude starts: `Error: stat /dev/dri: no such file or directory`, exit 125. Every
desktop has a GPU, so the launcher never met this until it was provisioned onto a server.

The device exists for one consumer, hardware-accelerated browser rendering, which a host with
no GPU cannot use anyway. Its absence is a fact to adapt to, not a failure. The flags become a
pure function of the path (`gpu_device_flags` in `lib/common-pure.bash`): present, the container
gets the render nodes exactly as before; absent, it gets none and a debug line says so. Pure so
the no-GPU case is testable on a desktop and the with-GPU case on a server.

## Goals

- `ccy` starts on a host with no `/dev/dri`; a host with one is handed it as before.
- The decision is unit-tested (`scripts/test-ccy-gpu-device.bash`) and wired into `qa-all.bash`.
- `CCY_VERSION` bumped, changelog entry written.

## Non-Goals

- Software-rendering fallbacks inside the container for the headed browser; a headless host
  runs the headless browser modes only, which need no GPU.
- Any other device passthrough.

## Tasks

### Phase 1: Make the device optional

- [x] ✅ **Task 1.1**: `gpu_device_flags <path>` in `lib/common-pure.bash`; the launcher fills
  `GPU_DEVICE_FLAGS` from it and expands the array in the run argv.
- [x] ✅ **Task 1.2**: `scripts/test-ccy-gpu-device.bash` (present dir, absent path, plain file,
  mapfile shapes, launcher consumes the array); wired into `qa-all.bash`.
- [x] ✅ **Task 1.3**: `CCY_VERSION` 3.56.0; `docs/ccy-changelog.md` entry.

### Phase 2: Prove — BLOCKED BY Phase 1

- [x] ✅ **Task 2.1**: `qa-bash` (which shellchecks the launcher and library) and the other
  runnable gates green on the branch; see JOURNAL for the two container-environment gates.
- [x] ✅ **Task 2.2**: On a headless VM with no `/dev/dri`, after its provisioning run: the old
  argv reproduces the abort at the podman layer, the new argv runs the image, and a headless
  `ccy` passes the device stage (stopping only at the human-only token login).

## Success Criteria

- [x] `ccy` on a host without `/dev/dri` gets past the device stage instead of exiting 125.
- [x] `ccy` on a desktop still passes the GPU (the run argv carries the same two flags).
- [x] Unit test and the runnable `qa-all.bash` gates green.

## Delivery & Milestones

- 72a3c92 — the change, on branch `ccy-gpu-device-optional`.
- b315046 — merged into F44 (PR #43).
