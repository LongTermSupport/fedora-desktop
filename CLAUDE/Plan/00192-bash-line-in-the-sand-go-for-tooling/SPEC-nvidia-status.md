# nvidia-status: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md): `nvidia-status`, the report of whether the NVIDIA
proprietary driver installed by `play-nvidia.yml` is built, loaded and working, with Secure
Boot, Vulkan, OpenGL, VA-API and the kernel's recent NVIDIA errors. **Method: the legacy bash
is not an oracle.** This document states the intended behaviour as numbered, testable
expectations (`E*`). The bash (`scripts/nvidia-status.bash`, 430 code lines, at `d153a3a0a`) was
read only to find the requirements; where it was wrong, the expectation states the correct
behaviour and section 5 lists the departure (`B*`). The proof is the Go tests in
`src-go/internal/nvidiastatus` (every flow over a fake runner and a fake `/proc`) and
`src-go/cmd/nvidia-status`. What only the real hybrid-GPU laptop can prove is section 6.

The inventory marks it interactive; it is not. It asks nothing: it is a report printed for a
person, so [InteractiveScripts.md](../../InteractiveScripts.md)'s re-prompt rules have nothing
to apply to. It had no test suite.

## 1. Scope decisions

- **D1. Run from the checkout, as today.** No play installs it: `play-nvidia.yml` ends by
  telling the person to run `./scripts/nvidia-status.bash`. That path stays, as the wrapper
  that builds `src-go/cmd/nvidia-status` from the checkout and execs it (`run_go_command` in
  `scripts/lib/go-tool.bash`, as `gh-account-setup.bash`), so nothing is queued in
  `meta-deploy.list`. The bash body is gone and its row leaves `scripts/bash-limits.allowlist`;
  `scripts/go-binary-suites.tsv` gets `nvidia-status	qa-go`.
- **D2. External programs stay where they are the source.** `mokutil`, `rpm`, `modinfo`,
  `nvidia-smi`, `glxinfo`, `vulkaninfo`, `vainfo` and `journalctl` are asked; their output is
  parsed in Go (no `grep`, `cut` or `xargs`). The loaded modules are read from `/proc/modules`
  (what `lsmod` prints) and the akmods key's presence with a stat.
- **D3. One seam for processes.** Everything runs through `internal/execx`. Nothing runs as
  root and nothing is changed: every command only reads. So Ctrl-C needs no handling: it ends
  the run at once (the default signal action) with nothing to put back.
- **D4. Facts first, then the words.** One pass gathers the facts (each command once), a second
  renders the report from them, and the verdict is a function of the facts. `--check` gathers
  and judges only.

## 2. Command line and exit status

- E1. `nvidia-status [--check]`. `-h`/`--help` prints the usage on stdout, exit 0.
- E2. Any other argument, or a second one, is a usage error:
  `nvidia-status: <text> — see 'nvidia-status --help'.` on stderr, exit 64, before any command
  runs.
- E3. The exit status is the verdict (section 4): 0 when the driver is complete and functional,
  1 for every other verdict. A report that could not be written in full (stdout closed) is exit
  1 with `nvidia-status: cannot write the report: <reason>` on stderr.
- E4. `--check` prints nothing on stdout or stderr (apart from E2 and E3's errors) and exits with
  the verdict.
- E5. The report goes to stdout (it is the payload). Colour only when stdout is a terminal and
  `NO_COLOR` is unset; otherwise plain text with the same marks (`✅`, `❌`, `⚠`, `ℹ`).
- E6. Every command runs under a fixed bound of 30 seconds. One that does not answer in time
  (`nvidia-smi` hangs on a wedged GPU) is reported as `<program> did not answer within 30s` in
  its section, and the run goes on.

## 3. The sections (in this order, each a heading and a rule line)

### 1. Secure Boot and MOK

- E7. `mokutil --sb-state`, stdout and stderr together: a line `SecureBoot enabled` is Enabled
  (✅); `SecureBoot disabled` is Disabled (⚠); anything else, a failure or mokutil missing, is
  `Could not determine` (⚠) with the exit status and the output (or `(none)`), and the line
  `This is NOT the same as 'disabled' — the state is unknown`.
- E8. When `/etc/pki/akmods/certs/public_key.der` exists: `mokutil --test-key <that path>`
  saying `is already enrolled` is enrolled (✅); `is not enrolled` is not enrolled (❌, requires
  reboot and enrollment); anything else is unknown (⚠, with mokutil's words), and counts as
  uncertain, never as not enrolled.
- E9. When it does not exist: with Secure Boot enabled, `mokutil --list-enrolled`; a failure is
  `Could not check` (⚠, uncertain, `This is NOT the same as 'no keys enrolled'`); otherwise the
  count of `SHA1 Fingerprint:` lines: above 0 is ⚠ `akmods key file not found, but N MOK key(s) enrolled` (uncertain), 0 is ❌ not found and no keys enrolled. With Secure Boot disabled: ℹ
  not needed. With the state unknown: ⚠ `Cannot say — Secure Boot state is unknown` (uncertain).

### 2. Packages

- E10. Each package is one `rpm -q --queryformat '%{VERSION}\n' <name>`: exit 0 is installed with
  its version (several installed versions joined by `, `); exit 1 with `is not installed` in the
  output is not installed; anything else is `could not query rpm` (⚠) with rpm's words.
  `akmod-nvidia` (NVIDIA driver), `xorg-x11-drv-nvidia-cuda` (CUDA/NVENC/NVDEC) and
  `libva-nvidia-driver` (VA-API) missing are ❌; `vulkan-loader` and `vulkan-tools` missing are ⚠.

### 3. Kernel modules

- E11. `modinfo -F version nvidia`: exit 0 with a version is built (✅ `Version <v>`, `unknown`
  when empty); a failure whose words say `not found` is not built (❌); any other failure is
  ⚠ could not check (built unknown).
- E12. `/proc/modules`, matched by exact module name (the first field): `nvidia`, `nvidia_drm`,
  `nvidia_modeset`, `nvidia_uvm`. The loaded ones are listed (✅) and the missing ones named. The
  driver counts as loaded only when `nvidia` itself is. None loaded is ❌ (requires reboot). An
  unreadable `/proc/modules` is ⚠ could not read, and counts as not loaded.
- E13. With Secure Boot enabled and `nvidia` loaded: ✅ `Secure Boot signature: Valid`, and an
  uncertain or not-enrolled MOK key becomes enrolled (`Enrollment confirmed by loaded signed modules`, ℹ), since the kernel loads no unsigned module under Secure Boot.

### 4. Driver functionality

- E14. `nvidia-smi` not on `PATH` is ❌ command not found. Otherwise
  `nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader`: exit 0 is ✅
  Working and, per line (one per GPU), `GPU`, `Driver` and `VRAM` with the fields trimmed;
  a failure is ❌ `Failed to communicate with GPU` with its output.

### 5. OpenGL

- E15. `glxinfo` not on `PATH` is ⚠ not available (`mesa-demos`, installed by play-nvidia.yml).
  Otherwise `glxinfo -B` once. A failure is ⚠ `Could not query` with `Usually no display/GL context (headless or SSH session), not a driver fault`, and no vendor verdict.
- E16. Vendor, renderer and version are the text after `OpenGL vendor string:`,
  `OpenGL renderer string:` and `OpenGL version string:`, trimmed (colons inside kept).
- E17. Hybrid graphics: the renderer names Intel, Radeon or AMD (any case) and E14 worked.
- E18. A vendor naming NVIDIA (any case) is ✅ with renderer and version. Otherwise, hybrid is ⚠
  `OpenGL default GPU: <vendor> (integrated graphics)` with the renderer and the PRIME offload
  line `__NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia <command>`; not hybrid is ❌
  `(expected: NVIDIA Corporation)` with the renderer.

### 6. Vulkan

- E19. `vulkaninfo` not on `PATH`: ❌ `install vulkan-tools` when `vulkan-loader` is installed,
  else ⚠ not installed. Otherwise `vulkaninfo --summary` once; a failure is ❌ `Failed to initialize` with the first line of its stderr.
- E20. Each device is a `deviceName = <name>` line, with the `apiVersion = <v>` of the same
  device block. A device naming NVIDIA is ✅ Working, listing every device with its API version;
  none is ❌ `(expected: NVIDIA device)` listing the devices found (or `none`).

### 7. Hardware video acceleration

- E21. `libva-nvidia-driver` from E10 (no second query): installed ✅, missing ❌ with the hint
  to run `play-nvidia.yml`.
- E22. `vainfo` not on `PATH` is ⚠ not available (`libva-utils`, installed by play-nvidia.yml).
  Otherwise `vainfo`, stdout and stderr together: exit 0 with a `VAProfile` line is ✅ `Driver loaded` with the `Driver version:` text; exit 0 with none is ⚠ no profiles; a failure is ⚠
  `Could not initialise` with its last line. The driver named is libva's default, which on a
  hybrid laptop is the integrated GPU's; the line says `(default driver)`.

### 8. Kernel errors this boot

- E23. `journalctl -k -b -p err --no-pager` (no `-g`). The lines naming `nvidia` or `NVRM` (any
  case), leaving out journalctl's `-- ... --` marker lines, the last 5 shown under ⚠ `Recent kernel errors found`; none is ✅ `None found`.
- E24. A failure, or anything on its stderr (no permission to the system journal, no journal
  files), is ⚠ `Could not read the kernel journal in full` with the first stderr line and the
  hint `sudo journalctl -k -b -p err --no-pager`; never `None found`.

## 4. The verdict (the summary and the exit status)

- E25. In this order, the first that holds:
  1. **Complete and functional** (exit 0): `nvidia` loaded and `nvidia-smi` worked. The words
     say Secure Boot enabled (with `Modules: Loaded and signed`), disabled, or unknown; then
     Vulkan working, OpenGL (NVIDIA renderer, or integrated under hybrid), the hybrid note, and
     a missing `libva-nvidia-driver`.
  2. **MOK enrollment required**: Secure Boot enabled and the key definitely not enrolled (E8,
     E9) — the four steps (reboot, enroll at the blue screen, the vault's `mok_password`).
  3. **Driver not installed**: `akmod-nvidia` not installed — run `play-nvidia.yml`.
  4. **Kernel module still building**: built is no (E11) — check with `modinfo -F version nvidia`, reboot when done.
  5. **Reboot required**: built, not loaded.
  6. **Installation incomplete**: everything else (loaded but `nvidia-smi` fails, or a fact that
     could not be read) — review the errors above.

## 5. Departures from the bash (`B*`)

- B1. The VA-API package is `libva-nvidia-driver`, the name `play-nvidia.yml` installs (Fedora
  43 renamed `nvidia-vaapi-driver`). The bash asked for the old name, so on Fedora 43 and later
  it always reported the package missing, the "side issue" Plan 029 recorded, and the play was
  never at fault.
- B2. The kernel journal: `journalctl -g` exits 1 when nothing matches, so the bash reported
  `Could not read the kernel journal` on every healthy machine. The Go filters itself (E23),
  also matches `NVRM` (the driver's own prefix, which the bash missed), reads this boot only
  (the bash showed errors from any past boot as recent), drops `-- No entries --`, and treats
  a journal it may not fully read as unread (E24). The hint is a `journalctl` command, not
  `sudo $0`, which would build the Go as root.
- B3. `--check` printed lines (`Secure Boot: Enabled`, the GPU, renderer and version lines, and
  others the bash did not guard); now it prints nothing (E4).
- B4. Unknown arguments were ignored; now a usage error (E2), and `--help` exists.
- B5. `lsmod | grep "^nvidia"` counted the driver as loaded when only `nvidia_drm` (any name
  starting `nvidia`) was; now exact names, and the driver is loaded only with `nvidia` (E12).
- B6. A `--test-key` answer the bash did not recognise left the key as "not enrolled", which
  then told the person to enrol it; now uncertain (E8).
- B7. With the Secure Boot state unknown the summary said `Secure Boot disabled`; it now says
  unknown (E25).
- B8. A missing `akmod-nvidia` was reported as `KERNEL MODULE STILL BUILDING`; now its own
  verdict (E25.3). A `modinfo` failure other than "not found" is no longer read as not built.
- B9. `vainfo`'s exit status was ignored (a failure read as `no profiles`), and its driver was
  shown as if NVIDIA's (E22). `vulkaninfo --summary` ran three times and `nvidia-vaapi-driver`
  was queried twice; each once now.
- B10. Colour escapes were written into a pipe; now only on a terminal (E5).
- B11. No command had a bound; a hung `nvidia-smi` hung the report (E6).
- B12. Hints point at the play that installs the tool (`mesa-demos`, `libva-utils`,
  `vulkan-tools` are all in `play-nvidia.yml`), not "add to playbook".
- B13. The summary branch for Secure Boot on and off was duplicated with different orders; one
  order now (E25). A loaded, working driver whose `modinfo` cannot see it is complete, not
  "still building".
- B14. A key file whose key is not enrolled printed `❌ Not enrolled (requires reboot and enrollment)` whatever the Secure Boot state, a wrong next step with Secure Boot off. It is a
  fault only with Secure Boot on; off, the key is "Not needed"; unknown, a warning says it
  matters only if Secure Boot is on (E8).

## 6. Host checks (`H*`)

Only the hybrid-GPU laptop with the driver installed shows these. Each is read-only unless it
says otherwise.

- H1 (host agent, read-only). `./scripts/nvidia-status.bash` in a terminal: every section
  reports, the verdict is complete and functional, exit 0; section 2 shows `libva-nvidia-driver`
  installed (B1); section 8 says `None found` or lists real lines, never `Could not read`
  unless the user truly cannot read the system journal (B2). Paste the output.
- H2 (host agent, read-only). `./scripts/nvidia-status.bash --check; echo $?` prints only `0`;
  `./scripts/nvidia-status.bash | cat` has no escape codes.
- H3 (host agent, read-only). Over SSH or in a session with no display: section 5 says `Could not query`, not an NVIDIA fault, and the verdict is unchanged.
- H4 (host agent, read-only). The real outputs match the parsers: `vulkaninfo --summary` lists
  the NVIDIA device with its API version, `glxinfo -B` gives the three strings, `vainfo` names
  its default driver, `mokutil --sb-state` and `--test-key` give the words E7 and E8 key on.
  Any mismatch is a fault of the parser, to fix here.
- H5 (owner). The not-functional verdicts on real hardware (a fresh install before its reboot:
  reboot required; MOK enrollment pending): only reached by installing or upgrading the driver
  and rebooting, which change driver state. Optional; the fakes cover the logic.
