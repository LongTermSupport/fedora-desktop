# Python Stack — Fedora 44 Migration Research (Plan 00050)

Fedora 44 (released 28 April 2026 — https://ostechnix.com/fedora-44-release-date-confirmed/) keeps **Python 3.14 as the system Python** — the 3.14 jump already happened in Fedora 43 (https://fedoraproject.org/wiki/Changes/Python3.14), and the F44 ChangeSet contains **no system-wide Python change** (https://fedoraproject.org/wiki/Releases/44/ChangeSet). F44 currently ships python3 3.14.4 (update 8 April 2026 — https://linuxsecurity.com/advisories/fedora/fedora-python3-14-2026-884eccdb03), with versioned 3.10–3.13 builds plus a python3.15 alpha preview available in the repos. Fedora has still **not** shipped the PEP 668 `EXTERNALLY-MANAGED` marker (the change stalled at "proposed" for F38 and never landed — https://fedoraproject.org/wiki/Changes/PythonMarkExternallyManaged), so `pip install --user` continues to work on F44. The Python-relevant disruption in F44 is the **toolchain**: GCC 16.1, binutils 2.46, glibc 2.43, CMake 4.2 (with ninja as the default `%cmake` generator) and LLVM 22 — these affect pyenv source builds and any pip packages that compile from source.

## PY-01: Stale pyenv version pins risk source-build failures under the GCC 16.1 toolchain

**Severity**: medium
**Area**: pyenv / play-python.yml

**Files**:

- `playbooks/imports/play-python.yml:9-12` — `pyenv_versions: 3.11.13, 3.12.11, 3.13.1`
- `playbooks/imports/play-python.yml:88-95` — `pyenv install -s {{ item }}` source build loop

**Concern**: All three pins are stale, and `3.13.1` (December 2024) is labelled "Latest stable" while upstream is at 3.13.14 (10 June 2026 — https://www.python.org/downloads/release/python-31313/). Fedora 44 moves to GCC 16.1/glibc 2.43 (https://fedoraproject.org/wiki/Releases/44/ChangeSet). The GCC 15 → C23 transition already broke source builds of older CPython point releases until upstream shipped compiler-compat fixes (https://gcc.gnu.org/gcc-15/porting_to.html, https://trofi.github.io/posts/326-gcc-15-switched-to-c23.html); GCC 16 is newer still, and old point releases like 3.13.1 carry none of the accumulated build fixes from the 13 subsequent 3.13.x releases. A failed `pyenv install` aborts the play (fail-fast), breaking the F44 deploy of `play-python.yml`. There is also a coherence gap: the system Python is 3.14, but no 3.14.x pyenv version is offered.

**Recommendation**: When branching for F44, bump `pyenv_versions` to the latest point releases of each line (3.11.x latest, 3.12.13+, 3.13.14+), correct the "Latest stable" comment, and consider adding a 3.14.x entry to mirror the system interpreter. Verify each `pyenv install` completes on an F44 host with GCC 16.1 before merging.

## PY-02: Pinned scipy 1.15.2 source-build workaround is obsolete and fragile on F44

**Severity**: medium
**Area**: speech-to-text / RealtimeSTT

**Files**:

- `playbooks/imports/optional/common/play-speech-to-text.yml:41` — `gcc-gfortran # Required to compile scipy from source (no pre-built wheel for Python 3.14)`
- `playbooks/imports/optional/common/play-speech-to-text.yml:212-223` — scipy-openblas32 install rationale ("Required on Python 3.14 where scipy 1.15.2 has no pre-built wheel")
- `playbooks/imports/optional/common/play-speech-to-text.yml:230-240` — pkg-config file written to `/tmp/scipy-openblas-pkgconfig`
- `playbooks/imports/optional/common/play-speech-to-text.yml:248-258` — `pip3 install --user scipy==1.15.2` source build

**Concern**: The whole block exists because scipy 1.15.2 had no cp314 wheel when F43 shipped. scipy 1.16.1 (27 July 2025) was the first release with Python 3.14 wheels, and current scipy (1.17.x) ships cp314 wheels broadly (https://scipy.org/news/, https://status.fedoralovespython.org/wheels_py314/). On F44 the pinned `scipy==1.15.2` still forces a from-source meson build — now against GCC 16.1/gfortran 16 and CMake 4.2, a toolchain that 1.15.2 was never tested against. Old scipy releases routinely fail to compile under newer compilers, so this task is the most likely hard failure in the speech-to-text deploy on F44 — and even if it builds, it is wasted effort given wheels now exist.

**Recommendation**: For F44, remove the scipy pin and the scipy-openblas32/pkg-config scaffolding (lines 212–258) and let RealtimeSTT's dependency resolution pull a cp314-wheeled scipy (>= 1.16.1). Re-evaluate whether `gcc-gfortran` (line 41) is still needed. Verify the full `play-speech-to-text.yml` run on an F44 host.

## PY-03: pip install --user flows depend on Fedora continuing to omit PEP 668 EXTERNALLY-MANAGED

**Severity**: low
**Area**: pip / PEP 668

**Files**:

- `playbooks/imports/optional/common/play-speech-to-text.yml:94-101` — faster-whisper + CUDA libs via `ansible.builtin.pip` with `extra_args: --user`
- `playbooks/imports/optional/common/play-speech-to-text.yml:220-223` — scipy-openblas32 `--user`
- `playbooks/imports/optional/common/play-speech-to-text.yml:255` — `pip3 install --user scipy==1.15.2`
- `playbooks/imports/optional/common/play-speech-to-text.yml:263-267` — RealtimeSTT `--user`
- `playbooks/imports/play-podman.yml:17-20` — podman-compose `--user`
- `playbooks/imports/optional/common/play-photography.yml:227-233` — rawpy `--user` fallback
- `files/home/.local/bin/wsi-stream:263` and `files/home/.local/bin/wsi-article:177` — error messages instructing `pip install --user RealtimeSTT`

**Concern**: PEP 668's `EXTERNALLY-MANAGED` marker blocks both system and `--user` pip installs (https://peps.python.org/pep-0668/). Fedora's change to ship it has been stalled since the F38 cycle and is **not** in the F44 ChangeSet (https://fedoraproject.org/wiki/Changes/PythonMarkExternallyManaged, https://discussion.fedoraproject.org/t/status-of-marking-the-base-python-environment-as-externally-managed-pep-668/95164), so these flows keep working on F44. However, six install paths across three playbooks share a single point of failure should Fedora ever adopt the marker (it remains an open upstream intent). The musiccast play already shows the resilient pattern (`virtualenv:` at `playbooks/imports/optional/hardware-specific/play-musiccast.yml:113-118`).

**Recommendation**: No change required for F44 itself. As migration polish, note the exposure in the plan and prefer venv/pipx-based installs for any new Python tooling; verify on a fresh F44 install that `/usr/lib64/python3.14/EXTERNALLY-MANAGED` is absent before relying on the `--user` paths.

## PY-04: PyTorch cp314 CUDA wheels — RealtimeSTT GPU path may silently fall back to CPU on a fresh F44 install

**Severity**: low
**Area**: speech-to-text / GPU

**Files**:

- `playbooks/imports/optional/common/play-speech-to-text.yml:91-101` — faster-whisper with CUDA support (nvidia-cublas-cu12, nvidia-cudnn-cu12)
- `playbooks/imports/optional/common/play-speech-to-text.yml:260-267` — RealtimeSTT install (pulls torch)
- `files/home/.local/bin/wsi-stream:688-693` — requests GPU, lets `AudioToTextRecorder` fall back internally

**Concern**: PyTorch only gained Python 3.14 support as a preview in 2.9, and as late as December 2025 users reported that **only CPU builds** of torch installed under Python 3.14 because CUDA cp314 wheels were missing (https://github.com/pytorch/pytorch/issues/169929, https://github.com/pytorch/pytorch/issues/156856). PyTorch 2.12.0 now ships cp314 wheels (https://github.com/pytorch/pytorch/releases), but whether pip resolves a CUDA-enabled torch for RealtimeSTT on Python 3.14 at F44 deploy time depends on the wheel index state. The repo's code deliberately falls back to CPU without failing (`wsi-stream:688-693`, and the faster-whisper script at `play-speech-to-text.yml:162-167`), so a CUDA-less torch would degrade transcription performance silently rather than loudly — at odds with the project's fail-fast preference.

**Recommendation**: On the first F44 deploy, verify `python3 -c "import torch; print(torch.cuda.is_available())"` in the user environment; if CUDA cp314 wheels are still patchy, pin torch to a known CUDA-enabled release in the playbook. Consider a post-install assertion task so a CPU-only fallback is surfaced, not silent.

## PY-05: pyenv build-dependency package names need re-verification on F44

**Severity**: low
**Area**: dnf packages / play-python.yml

**Files**:

- `playbooks/imports/play-python.yml:14-46` — pyenv/pip build dependency list (`zlib-devel`, `SDL2-devel`, `SDL2_image-devel`, `SDL2_mixer-devel`, `SDL2_ttf-devel`, `libnsl2`, `gdbm-libs`, `portmidi-devel`, `portaudio-devel`, …)
- `playbooks/imports/play-python.yml:68-74` — pyenv-installer curl-pipe (toolchain-independent, but depends on the above for builds)

**Concern**: Fedora has been migrating its compression stack to zlib-ng — pyenv's own guidance for recent Fedora recommends `zlib-ng-compat-devel` (https://github.com/pyenv/pyenv/wiki/Common-build-problems, https://stribny.name/posts/install-python-dev/); `zlib-devel` currently resolves via a virtual provide, but that mapping should be confirmed against F44's repos. Similarly, the SDL2 family is in transition towards SDL3/sdl2-compat in recent Fedora releases, so the four `SDL2*-devel` names plus `portmidi-devel`/`libnsl2`/`gdbm-libs` need a `dnf info` pass on F44 — a single renamed/retired package aborts the whole `dnf` task and therefore the play. No F44-specific removal of these packages is recorded in the ChangeSet (https://fedoraproject.org/wiki/Releases/44/ChangeSet), so this is a verification task rather than a known break.

**Recommendation**: Before the F44 deploy, run `dnf info` (read-only) against each package name in the F44 repos and substitute any renamed providers (e.g. `zlib-ng-compat-devel` if the `zlib-devel` provide disappears). Keep the list grouped with comments noting which consumer needs each dependency.

## PY-06: Repo Python consumers are stdlib-only and unaffected by the F43 to F44 Python transition

**Severity**: info
**Area**: scripts / pipx tools

**Files**:

- `files/home/.local/bin/wsi-stream:15-25`, `files/home/.local/bin/wsi-stream-server:14-24`, `files/home/.local/bin/wsi-model-manager:17-20`, `files/home/.local/bin/wsi-server-manager:12-19` — stdlib imports only (os, sys, json, socket, subprocess, threading, signal, pathlib, datetime, array)
- `files/usr/local/bin/manage-kernel-versions.py:14-21` — stdlib only (argparse, logging, re, subprocess, dataclasses)
- `run.bash:569-574` — bootstrap installs `python3`, `python3-pip`, `python3-libdnf5` (names unchanged on F44)
- `playbooks/imports/play-python.yml:48-66` — pipx-managed tools (pdm, huggingface_hub, semgrep) and dnf-managed `ruff`/`uv`
- `scripts/qa-python.bash:69` — `python3 -m py_compile` against system python

**Concern**: None material. Because F44 stays on Python 3.14 (https://fedoraproject.org/wiki/Releases/44/ChangeSet has no Python bump; https://linuxsecurity.com/advisories/fedora/fedora-python3-14-2026-884eccdb03 confirms 3.14.4), there is no interpreter ABI change: pipx venvs built on F43's 3.14 keep working after upgrade (no `pipx reinstall-all` churn), the stdlib-only scripts have no removed-module exposure, and `qa-python.bash`/`py_compile` behaviour is unchanged. The next interpreter break arrives with Fedora 45's expected Python 3.15 (https://fedoraproject.org/wiki/Changes/Python3.15) — a forward-looking watch-item for the F45 cycle, not F44.

**Recommendation**: No action for F44. Note in the plan that the pipx-reinstall/stdlib-removal checks become relevant again at F45 (Python 3.15).

## Sources

- https://fedoraproject.org/wiki/Releases/44/ChangeSet — F44 accepted changes (GCC 16.1, binutils 2.46, glibc 2.43, CMake 4.2 + ninja, LLVM 22; no Python bump)
- https://ostechnix.com/fedora-44-release-date-confirmed/ — F44 release 28 April 2026
- https://fedoraproject.org/wiki/Changes/Python3.14 — Python 3.14 landed system-wide in F43
- https://fedoraproject.org/wiki/Changes/Python3.15 — Python 3.15 targeted at a later release (F45 cycle)
- https://linuxsecurity.com/advisories/fedora/fedora-python3-14-2026-884eccdb03 — F44 python3 3.14.4 update (April 2026)
- https://fedoraproject.org/wiki/Changes/PythonMarkExternallyManaged — PEP 668 change stalled at proposed (F38), never shipped
- https://discussion.fedoraproject.org/t/status-of-marking-the-base-python-environment-as-externally-managed-pep-668/95164 — Fedora PEP 668 status discussion
- https://peps.python.org/pep-0668/ — PEP 668 semantics (blocks `--user` too)
- https://www.python.org/downloads/release/python-31313/ and https://devcenter.heroku.com/changelog-items/3639 — 3.13.13/3.13.14 and 3.14.4 current point releases
- https://gcc.gnu.org/gcc-15/porting_to.html and https://trofi.github.io/posts/326-gcc-15-switched-to-c23.html — C23 compiler-default breakage pattern for older C codebases
- https://scipy.org/news/ — scipy 1.16.1 first release with Python 3.14 wheels
- https://status.fedoralovespython.org/wheels_py314/ — cp314 wheel readiness table
- https://github.com/pytorch/pytorch/issues/156856 and https://github.com/pytorch/pytorch/issues/169929 — PyTorch Python 3.14 support and missing CUDA cp314 wheels
- https://github.com/pytorch/pytorch/releases — PyTorch 2.12.0 ships cp314 wheels
- https://github.com/pyenv/pyenv/wiki/Common-build-problems and https://stribny.name/posts/install-python-dev/ — pyenv build deps on recent Fedora (zlib-ng-compat-devel)
