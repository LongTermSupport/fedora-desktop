# nvidia-status: the Fable review's follow-ups F2-F7

Task 3.3 of Plan 00192. Source: `subagent-reports/261028-fable-review-nvidia-status.md`. Work on
`plan/00192-nvidia-status-follow-ups` (from F44 `4512a56c5`); commit hash in the branch log.
Every fix was tested first: the new tests were seen red (journal, `action` entries), then green.
The tool still only reads; the one new read is `/proc/driver/nvidia/version` through the
existing `Env.Proc`, and the one new command is a second `vainfo`.

## Fixed

- **F2, the production timeout path.** `TestTimeoutIsNamedNotTheKill` drives `probe` with a
  runner that does what `execx.OS` does at a deadline: it waits for the context, then returns
  `ExitError{Signal: SIGTERM}`, not the context's error. It runs in a `testing/synctest` bubble,
  so the production 30s constant passes at once and no bound becomes a field (the
  `project_realtime_bound` rule stays satisfied without a `processBounds` row). Red was shown by
  swapping the two `if`s in `probe`: the old `TestHungCommandIsReported` stayed green, the new
  test failed with `nvidia-smi killed by SIGTERM`. A comment at the `if` names the test. The
  spec's E6 now says the bound holds only for a process that can die (a D-state `nvidia-smi`).
- **F3, version mismatch reads as reboot (E26, E25.5, B15).** With `nvidia` loaded, the loaded
  version is the first `N.N.N` of the `NVRM version:` line of `/proc/driver/nvidia/version` (as
  `play-nvidia.yml` reads it); installed is the built version (`modinfo`), else `akmod-nvidia`'s
  single version. Section 3 shows the loaded version, ⚠ when it differs. The new verdict
  `REBOOT REQUIRED: NEW DRIVER INSTALLED`, with both versions (`unknown` where unread), holds
  when they differ or when `nvidia-smi` failed saying `version mismatch`, unless both versions
  are known and equal. It comes after "still building" and before the not-loaded reboot. A
  working driver with a newer one installed stays complete, with a line naming the reboot.
  Also fixed (the review's second half of F3, B19): with no akmods key file and no key enrolled
  under Secure Boot, the MOK steps now start with running `play-nvidia.yml`.
- **F4, NVIDIA's VA-API driver (E22a, B16).** A second `vainfo` with `LIBVA_DRIVER_NAME=nvidia`
  in its environment, judged like the default one under `VA-API (NVIDIA driver)`, with a
  `Try:` hint when it fails; not asked when `libva-nvidia-driver` is known missing. Neither line
  changes the verdict (as before for VA-API). The parser is unproven on real NVIDIA output: H6.
- **F5, section 6 against section 2 (E19, B17).** `vulkaninfo` absent with `vulkan-loader`
  unknown now says `could not tell whether Vulkan is installed (rpm failed, section 2)`.
- **F6, i2c noise (E23a, B18).** `nvidia-gpu <addr>: i2c timeout error` lines are left out of the
  error list and counted in one ℹ line; with nothing else, the section is ✅ `None found`.
- **F7, INVENTORY.md.** Both rows no longer carry `I`; the section-2 row says why. The markdown
  formatter hook realigned that file's tables on the edit, hence the large diff.

## Docs

`SPEC-nvidia-status.md` (D2, E6, E19, E22a, E23a, E25, E26, B15-B19, H5, H6), PORTS.md's
nvidia-status paragraph, PLAN.md's Task 3.3 bullet, the package doc and `src-go/CLAUDE.md`
(vainfo is asked twice, `/proc/driver/nvidia/version` read). Coverage 97.1%; `golangci-lint run`
reports nothing.

## Host

Nothing deployed (the tool runs from the checkout), so `meta-deploy.list` is unchanged. New
**H6 is the host agent's, read-only**: section 7's `VA-API (NVIDIA driver)` line against
`LIBVA_DRIVER_NAME=nvidia vainfo` by hand, section 3's loaded version against
`/proc/driver/nvidia/version`, and any `nvidia-gpu` i2c lines in the E23a count. The mismatch
verdict on real hardware joins H5 (the owner's: it needs a driver upgrade before its reboot).
