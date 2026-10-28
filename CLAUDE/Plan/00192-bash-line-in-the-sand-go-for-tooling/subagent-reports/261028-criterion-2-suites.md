# Plan 00192 criterion 2: every merged port's existing suite, against the Go

Criterion 2: "Every port in Phase 3 passes its existing suite against the Go binary before
cut-over", where a replaced oracle meets it (a golden replay, or expectation tests standing for
a suite that tested the bash's internals). Read at `06e151cc7`.

How "had a suite" was established: every `scripts/test-*` and `tests/**` test file at
`af2c5935d` (the last commit before the plan, 224 files) was searched for each ported tool's
name, every hit read to tell an invocation from a mention, and the suites added during the plan
(`test-qa-changed.bash`, added `ac8183cd9`) and the ones deleted under `scripts`, `tests` and
`src-go/qa` since (`git log --diff-filter=D`) were added. `scripts/go-binary-suites.tsv` names
what drives each deployed Go command now.

## Verdicts

| Port                                                                               | Existing suite                                                                                                                                                      | Against the Go, or replaced                                                                                                                                                                                                                                     | Holds |
| ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| freeze (podfreeze, lxcfreeze)                                                      | `test-podfreeze`, `test-lxcfreeze`, `test-freezelib` (white-box), deleted at `35f56ef9f`                                                                            | Golden replay of the shipped bash tools' recorded output (`internal/freeze/golden`) plus Go tests (criterion text, Task 3.1)                                                                                                                                    | yes   |
| qa-changed                                                                         | `test-qa-changed.bash` (black box)                                                                                                                                  | Run against the Go binary before the cut-over: `QA_CHANGED_BIN`, the 73 original cases passed against bash and Go alike (`subagent-reports/261014-qa-changed-port-sonnet.md`, Leg 1); plus the golden replay of 118 scenarios                                   | yes   |
| qa-all                                                                             | `test-qa-helper-summary.bash`, `tests/helpers/qa_environment/test_gate_call_sites.py` (white-box), deleted at `239e07e5c`                                           | Golden replay of the legacy script (142 scenarios) plus the Go tests that took the summary suite's cases (criterion text, SPEC-qa-all)                                                                                                                          | yes   |
| qa-ansible                                                                         | `test-qa-ansible-failfast.bash` (black box)                                                                                                                         | Drives the built tool: 18 passed, 0 failed (JOURNAL 26-10-14, 14:50, and GREEN after the review fixes)                                                                                                                                                          | yes   |
| qa-docs                                                                            | `test-qa-docs-exit-codes.bash` (black box); `tests/helpers/docs/test_link_check.py` (unit tests of the Python checker), deleted at `97fe96d25`                      | The exit-code suite drives the built tool (JOURNAL 26-10-15 qa-docs entry; SPEC-qa-docs section 4, the contract suite); the Python checker's unit tests are replaced by the D1-D25 expectation tests in `src-go/qa/docs` (`links_test.go` D8-D16 for the links) | yes   |
| qa-ready-wait-rules                                                                | `tests/helpers/ready_wait/test_bash_ready_waits.py` (unit tests of the Python shell half), deleted at `0512299db`                                                   | Replaced: SPEC-qa-ready-wait-rules B1, "the cases ported (R14-R24)", `src-go/qa/readywait/cases_test.go`                                                                                                                                                        | yes   |
| the other 16 qa gates (1)                                                          | none: no test file at `af2c5935d` invokes them (hits are mentions in comments and fixture text); `tests/fixtures/ansible-pause` is qa-ansible-pause's own self-test | Nothing to run; each has its spec's expectation tests. The Python helpers some of them call (`version_pins`, `vmtest` manifest, `github_scopes`, `qa_environment`) keep their own tests, untouched                                                              | yes   |
| vmtest                                                                             | four suites deleted at `2c9878497`: `test-vmtest-member`, `-host-only-gate`, `-reboot-dispatch`, `-build-dir-cleanup`                                               | See the next section: the member and host-only cases now run against the Go binary, 81 of 81; reboot-dispatch and build-dir-cleanup are white-box, their cases in Go tests. `test-vmtest-kernel-selection` and `-prepare-record` test guest scripts and stay    | yes   |
| vmtest prune-runs, sweep (Task 3.10)                                               | none (new verbs)                                                                                                                                                    | Nothing to run                                                                                                                                                                                                                                                  | yes   |
| ftp-camera                                                                         | none: no test file names it; SPEC-ftp-camera: the proof is the Go tests; `go-binary-suites.tsv` `ftp-camera qa-go`                                                  | Nothing to run                                                                                                                                                                                                                                                  | yes   |
| watermark                                                                          | none: no test file names it; `go-binary-suites.tsv` `watermark qa-go` (SPEC-wsi-watermark section 6)                                                                | Nothing to run                                                                                                                                                                                                                                                  | yes   |
| wsi                                                                                | `test-wsi-stop-grace.bash` (black box, plus two static checks on the bash's `kill` sites)                                                                           | Builds and drives the Go `wsi`: 120 passed (`subagent-reports/261015-wsi-watermark-port-opus.md`); the two static checks read bash that no longer exists (SPEC-wsi-watermark section 6)                                                                         | yes   |
| gh-account-setup                                                                   | `test-run-bash-gh-scopes.bash`: `run.bash` cases (black box) and bash functions extracted with awk (white-box)                                                      | SPEC-gh-account-setup D4: the extracted cases are Go tests (E24, E26); the suite drives the built command through the wrapper, 52 passed, 0 failed, before the merge (`9e404ae6b`; JOURNAL 26-10-23, 03:42), 59 after review S2                                 | yes   |
| ssh-with-key, scp-with-key, ssh-copy-id-with-key                                   | none: SPEC-ssh-with-key, "They had no test suite"; no test file names them                                                                                          | Nothing to run                                                                                                                                                                                                                                                  | yes   |
| open, rclone-cache-status, rclone-tail, setup-rclone, reclaim, nord, nvidia-status | none (PORTS.md, "it had no suite"; confirmed by the search)                                                                                                         | Nothing to run (reclaim and the two rclone tools were also compared with the bash over fakes)                                                                                                                                                                   | yes   |

(1) qa-nokill-containerwatch, qa-patterns, qa-python, qa-ansible-syntax, qa-bash, qa-discovery,
qa-deployed-drift, qa-js, qa-ansible-pause, qa-gh-scopes, qa-plan-script-logging, qa-toolchain,
qa-speech-to-text-rules, qa-version-pins, qa-vmtest-manifest, qa-helper-tests; none had a suite
of its own. Not counted: `qa-go.bash` (retired, not ported)
and `agent-bus-install` (Task 3.2, not merged; its suite stays with the bash).

## vmtest's four deleted suites

Restored from `2c9878497^` into scratch and read. All four load the bash and call its functions,
so none runs against a binary as written. Two of them, though, check only what a caller of the
CLI sees (exit status, stdout markers, the commands sent to virsh, virt-install, qemu-img and
ssh, the files left in the lab), and replace bash functions only to stand in for the lab and the
Python helpers. Those two were re-pointed at the binary
([vmtest-blackbox/blackbox-vmtest.bash](../vmtest-blackbox/blackbox-vmtest.bash)): each case
label and assertion is the suite's, and the stand-ins move to the binary's own seams (the same
four stubs on `PATH`, `curl` and `gpg2` stubs that fail, fake `validate_manifest`,
`probe_upstream` and `freshness_gate` modules through `VMTEST_HELPERS_LIB`, a real lab tree
under `VMTEST_HOME`). It runs under `unshare -rm` so the lab can be a tmpfs (the disk guard
wants 22 GiB free) and `/usr/share` an overlay with a tmpfs upper layer (the binary's fixed GPG
key directory), with nothing written outside scratch.

Run against `go build ./cmd/vmtest` at `06e151cc7`: **81 passed, 0 failed**; against the
cut-over build of `2c9878497`, the same (see Limit). The first run
failed 21 cases, all from a fake of mine (the freshness verdict line with a tab where the real
gate prints a space); fixed in the fake to match `helpers/vmtest/freshness_gate.py`, which shows
the harness can fail.

| Suite                           | Kind                                                                                           | Result against the Go                                                                                                                                                                                                                                                                                                                   | Go tests that carry it (SPEC-vmtest section 6)                                         |
| ------------------------------- | ---------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `test-vmtest-member`            | black box (sources the bash only to replace the lab and helper functions)                      | every case passes: names refused; member-down three ways; member-up boots, exists, gate refuses, guest stops before SSH, virt-install fails, SIGTERM in the SSH wait (143, guest destroyed and undefined, overlay and port gone); ensure-base three ways; member-ssh five ways                                                          | `member_test.go`, `freshness_test.go` (E53-E59, E4)                                    |
| `test-vmtest-host-only-gate`    | function unit test (extracts `host_only_preflight`), but every case is visible at `vmtest run` | every case passes through `vmtest run server-github-token --github-*`: the acceptance case gets past the gate to the base check; the bridge, the two lists, the missing account, token and passphrase, unreadable, empty, wrong mode, bad account grammar each refused before it; no secret echoed; nothing reached ssh or virt-install | `hostonly_test.go` (E30)                                                               |
| `test-vmtest-reboot-dispatch`   | white-box: extracts `reboot_guest` and `guest_prepare`, two inner steps of `run`               | not runnable against a binary: reaching them from outside needs a faked guest answering the whole run protocol                                                                                                                                                                                                                          | `steps_test.go` TestE41\*, TestE40GuestPrepare\*; `guest_test.go` TestE23\*, TestE24\* |
| `test-vmtest-build-dir-cleanup` | white-box: calls `build_dir_finished` and `teardown_build` on directories the test makes       | not runnable against a binary (no build runs)                                                                                                                                                                                                                                                                                           | `build_test.go` TestE21BuildDirectoriesAcrossOneRefresh, TestE27\*                     |

`go test ./internal/vmtest ./cmd/vmtest` passes at `06e151cc7`.

Adaptations, none a defect:

- `member-up m1` with no base key exits 64 in the Go, 1 in the bash: SPEC-vmtest E2 (a wrong
  argument count is a usage error, 64). The check asserts 64.
- The host-only "missing --github-account/--github-token-file/--github-ssh-passphrase-file"
  cases leave the option off the command line, as a user would. Given an empty value
  (`--github-account ""`) the Go exits 64 by E2 ("needs a value"); one added check pins that.
- ensure-base's "rebuilt" and "built": the bash suite replaced `cmd_build_base` with a log line,
  so those cases checked only the dispatch and exited 0. Against the binary the real build runs:
  the check is that it says it is building and reaches the probe's media list ("the probe listed
  no artefact"), then exits 1 because the fakes publish nothing, with no download attempted.

Defects found: none. Nothing was changed in `src-go`.

## Limit

Nothing records a run of the vmtest suites against the Go before the cut-over. This run is
after it, but it was made against the build that replaced the bash as well as today's: the
binary built from `git archive 2c9878497 src-go` (the cut-over commit) also passes **81 of 81**,
with two fakes fitted to that commit's helper protocol (`BB_CUTOVER=1`: its validator marker
had no `audience` field, and it asked `retention floor` before a member boots, which the bash
suite's helper stand-in answered 0). So the bash suites' black-box expectations held of the Go
that shipped at the cut-over and still hold. `blackbox-vmtest.bash` is a one-off check run by
hand, as `reclaim-parity/parity.py` is; no gate runs it, and the Go tests above are what holds
these cases on each change.
