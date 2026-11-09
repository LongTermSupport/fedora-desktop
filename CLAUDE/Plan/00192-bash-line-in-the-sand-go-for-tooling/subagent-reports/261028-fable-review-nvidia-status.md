# Fable review: nvidia-status port (`go-port/nvidia-status`, `1856af9bf` on `d153a3a0a`)

Reviewed: the branch diff, the old bash at `origin/F44`, `SPEC-nvidia-status.md`, `src-go/CLAUDE.md`,
`GoProjectRules.md`, the worker's report. Ran `go test -race` and `go vet` over both packages (green),
and the wrapper in the container: a full report through `| cat` (no escape codes, exit 1), `--check`
(silent, exit 1), `--bogus` (usage line on stderr, exit 64). Nothing in the branch was changed.

Verdict: **no blocker**. One section line gives a wrong next step on a real machine and should be
fixed before merge; the rest are follow-ups. Every claimed bash defect is real and its fix right.

## FIX-BEFORE-MERGE

### F1. Key file present, Secure Boot not enabled, key not enrolled: section 1 orders an enrolment the verdict does not want

`src-go/internal/nvidiastatus/report.go:118` (`mokKey`, second case) and `SPEC-nvidia-status.md` E8.

Scenario: a laptop with Secure Boot disabled in firmware, `play-nvidia.yml` run (it generates
`/etc/pki/akmods/certs/public_key.der` whenever Secure Boot was once on, and the file stays), key never
enrolled. `mokutil --test-key` says `is not enrolled`. Section 1 prints
`❌ akmods MOK Key: Not enrolled (requires reboot and enrollment)`, while the summary (correctly, judge
line `report.go:27` requires `sb == sbEnabled`) says complete and functional. The person reads a red
cross telling them to reboot and enrol a key that Secure Boot will never check. With the state unknown
(legacy BIOS: `EFI variables are not supported`) the same ❌ appears if `--test-key` still answers.

The bash had the identical line (it tested the file before looking at `SECURE_BOOT`), so this is a
departure to add (B14), not a regression. Fix: in `mokKey`, when `f.found == mokNotEnrolled` and
`f.sb != sbEnabled`, print ℹ `akmods MOK Key: Not enrolled (not needed: Secure Boot disabled)` for
disabled and ⚠ `… (Secure Boot state unknown)` for unknown; state it in E8; one test in
`TestMokKey` with `sb = SecureBoot disabled`, `keyFile = true`, `testKey = is not enrolled`, asserting
no ❌ and exit 0.

## FOLLOW-UP

### F2. The production timeout path is not the one the test exercises

`src-go/internal/nvidiastatus/app.go:141-156` (`probe`), `app_test.go` `TestHungCommandIsReported`.

The test's fake returns `context.DeadlineExceeded` as the error. The real `execx.OS.Run` never does:
`exec.Cmd.Wait` prefers the `*exec.ExitError` of the SIGTERM-killed child over the context error, so
production arrives with an `ExitError{Signal: SIGTERM}` and only `bounded.Err()` reveals the deadline.
`probe` does check `bounded.Err()` before the `ExitError` branch (I read it against `os/exec`'s
`Wait`), so the verdict is right, but nothing proves it: a later reorder of the two `if`s would print
`nvidia-smi killed by SIGTERM` instead of `did not answer within 30s` and no test would go red. Add a
test through `execx.OS` with a `sleep` child and an injectable bound (the project rule against a
real-time bound a test can shorten means the bound must be a field of `Env`, not the const), or at
least a fake that returns `ExitError{Signal: SIGTERM}` with an already-expired context.

Also worth a sentence in the spec: a `nvidia-smi` wedged in uninterruptible sleep (the usual way it
hangs on a dead GPU) ignores SIGTERM and SIGKILL alike, so `Wait` blocks past the 30s; E6's bound is
a bound on a process that can die. The bash had no bound at all, so this is still a gain.

### F3. "Installation incomplete, review the errors above" is vague for the commonest real failure

`src-go/internal/nvidiastatus/report.go:361` (`incomplete`), `judge` default case.

Scenario: `dnf upgrade` brings a new `akmod-nvidia`; the running kernel still has the old module;
`nvidia-smi` fails with `Driver/library version mismatch`. Section 3 ✅ built, ✅ loaded; section 4 ❌
with the mismatch line; summary: incomplete. The right action is a reboot. The facts already hold the
built version (`modinfo`), the package version (`rpm`) and, when it works, nvidia-smi's; when built
differs from the package version, or `nvidia-smi` says `version mismatch`, say so and name the reboot
(the play itself does this three-version comparison at its end). Same vagueness for the
`mokRequired` words when the key file does not exist and no key is enrolled (E9's ❌): "enrol at the
blue screen" assumes `mokutil --import` was done; the step there is to run the play.

### F4. Section 7 verifies the package, not the NVIDIA VA-API driver

`report.go:245-275` (`video`), spec E22. On every hybrid laptop the default libva driver is the
iGPU's, so section 7 can never show that `libva-nvidia-driver` works; a missing `nvidia-drm.modeset=1`
or `NVD_BACKEND` leaves it broken with ✅ on the package line. The worker left this out deliberately
(unproven on the host); after H4 passes, a second probe `LIBVA_DRIVER_NAME=nvidia vainfo` (through
`execx.Cmd` env) reported as its own ⚠/✅ line would make the section mean what its title says.

### F5. When rpm itself cannot run, section 6 contradicts section 2

`report.go:201-205` (`vulkan`, first two cases). Seen in the container run: section 2 says
`Vulkan runtime: could not query rpm`, section 6 says `vulkaninfo: Not installed (Vulkan support not configured)`. With `vulkan-loader` state `unknown` and `vulkaninfo` absent, say `could not tell (rpm failed, section 2)`, as section 7 already does for `libva-nvidia-driver`.

### F6. Journal filter counts the `nvidia-gpu` i2c noise as driver errors

`sections.go:92` (`nvidiaLine`). Hybrid laptops log `nvidia-gpu 0000:01:00.3: i2c timeout error` at
err priority on most boots (the USB-C controller, not the driver). It is shown under
`Recent kernel errors found` with ⚠. The bash's `-g nvidia` did the same, so no regression; a later
pass could name that line as benign so the section's one ⚠ means something.

### F7. INVENTORY.md still marks the tool interactive

`INVENTORY.md:53` and `:209` carry `I` for `scripts/nvidia-status.bash`; the spec's opening says it
is not. The row should say so (or note the spec's finding) so the inventory and the spec agree.

### F8. Test fixtures name one laptop's hardware

`world_test.go:61,64,83-92` name a specific NVIDIA laptop GPU model and Intel platform code, and
`app_test.go` asserts them. The public-repo rule lists machine identifiers (hosts, users, paths,
containers), not hardware models, so this is not a leak; but a generic model (`NVIDIA GeForce RTX 3050 Laptop GPU`, `Intel(R) Iris(R) Xe Graphics`) says the same and describes nobody's machine.

## OK (checked, no change needed)

- **B1 real, fix right.** `play-nvidia.yml:60` installs `libva-nvidia-driver`; the bash queried
  `nvidia-vaapi-driver` twice (sections 2 and 7) so on F43+ it always said missing. The Go asks the
  play's name once (`TestEachCommandRunsOnce` pins no old-name call).
- **B2 real, fix right.** `journalctl -g` exits 1 with no match, so the bash's `if ! RECENT_ERRORS=$(…)`
  printed `Could not read the kernel journal` on every clean boot. The Go runs without `-g`, filters
  `nvidia|NVRM` itself, reads this boot only, drops `-- … --` markers, and treats stderr (the
  not-in-`wheel` hint, `No journal files were found.`) as not-fully-read, never `None found`.
- **B5 real:** bash `grep -q "^${mod}"` with `mod=nvidia` matched `nvidia_drm`, `nvidiafb`; Go matches
  field 1 of `/proc/modules` exactly and counts the driver loaded only on `nvidia`.
- **B6 real:** an unrecognised `--test-key` answer left `MOK_ENROLLED=0`, which under Secure Boot
  became `ACTION REQUIRED: MOK ENROLLMENT`; now uncertain, verdict falls through.
- **B7 real:** `SECURE_BOOT=0` on unknown put the summary in the "Secure Boot disabled" branch; now
  the complete words say unknown, and E13's proof only fires on `sbEnabled`.
- **B8 real:** `MODULE_EXISTS=0` was tested before anything about the package; a host with no
  `akmod-nvidia` was told to wait 5-10 minutes. Now `driverMissing` precedes `building`.
- **Parsers against real output.** `mokutil --sb-state` (`SecureBoot enabled|disabled`, the EFI
  message on BIOS), `--test-key` (`is already|not enrolled`), `rpm -q --queryformat` (`package X is not installed`, exit 1, stdout), `modinfo` (`Module nvidia not found.`; newer kmod's `Module alias … not found` still matches), `nvidia-smi --format=csv,noheader` (three comma fields), `glxinfo -B`
  (the three `… string:` lines), `vulkaninfo --summary` (`GPUn:` then tab-indented `key = value`),
  `vainfo` (`vainfo: Driver version:`, `VAProfile…` rows), `journalctl` (`-- No entries --` on stdout)
  all match the formats these tools print on Fedora. Fixtures look like real output. H4 remains the
  proof on the laptop.
- **Unknown reads as unknown, never healthy.** Every probe failure is a `tri`/`outcome` with a
  `problem`, each section says what it could not ask, and `judge` sends every unknown to
  `incomplete` (exit 1). `TestVerdictOrder` pins the order. Fail-fast holds without aborting the
  report, which is right for a diagnostic.
- **Exit codes:** 0 complete, 1 any other verdict or unwritable stdout, 64 usage; `term.ExitCode`
  throughout, `os.Exit` only in `main`. `--check` is silent on both streams.
- **Stderr hygiene:** report on stdout, the two diagnostics on stderr, colour only on a tty and
  without `NO_COLOR`.
- **Ctrl-C with no handling is acceptable:** nothing is changed, every child is in the foreground
  process group and gets the terminal's SIGINT with the parent; nothing to restore.
- **Wrapper:** `run_go_command` execs (signals reach the binary, exit code is the wrapper's);
  `set -euo pipefail`; 16 lines, inside the bash limits; the allowlist row is gone. Go reaches every
  host through `playbook-main.yml` → `play-basic-configs.yml` → `play-golang.yml`, so a host that ran
  `play-nvidia.yml` has the toolchain; no regression against the bash needing nothing. Exit 2 from a
  failed build does not collide with the tool's codes.
- **Docs and plan:** PLAN.md Task 3.3, PORTS.md, `src-go/CLAUDE.md` (tool list, package, build
  section), `docs/playbooks.md`, `go-binary-suites.tsv`, two journal entries (red at 021333Z, green at
  022134Z) are consistent with the code. `play-nvidia.yml:428` still points at the same path.
  Nothing queued in `meta-deploy.list`, correctly (no play changes).
- **Install-specific content:** none in tracked files beyond F8's hardware model.
