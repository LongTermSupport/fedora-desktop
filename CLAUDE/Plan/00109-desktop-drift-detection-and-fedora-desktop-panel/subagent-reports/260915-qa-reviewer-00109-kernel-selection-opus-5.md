# qa-reviewer — commit `5066569c`, the kernel-selection gate

Transcribed on the reviewer's behalf: the agent is read-only and could not write the file
itself. Verdict **BLOCK**, one blocking finding, five should-fix, six nits.

Scope: `git diff f1645dde..5066569c` — `select_second_kernel` extracted out of
`guest-prepare-server-host-health-kernel-change.bash`, the new
`scripts/test-vmtest-kernel-selection.bash` gate, and the QA/plan/design updates.

## BLOCKING

**The refactor moved the package transaction out of `set -e`.** The commit lifted
`sudo -n dnf -y install "kernel-${wanted}"` from script top level, where errexit was live,
into a function the caller invokes as `target_kernel="$(select_second_kernel …)"` — and
bash does **not** inherit errexit into a command substitution without
`shopt -s inherit_errexit`, which this repo sets nowhere. Measured on bash 5.2.15: the old
shape exits 1, the new shape runs on and exits 0.

A failed transaction therefore either died at the `vmlinuz` check blaming `/boot` for a
package failure — the very defect this commit fixed for `rpm` — or, on a guest that
already carried two kernels, **succeeded silently** with `PREPARED_WANTED_KERNEL` naming a
kernel nothing ever downloaded.

## Should fix

1. `sort -Vr` is never exercised: every stub list was already in descending order, so
   deleting both sorts survives all twelve cases. Case 1a's comment claims otherwise; it
   only kills the *ascending* mutant.
2. Case 4 asserts the same refusal string as case 3. "Every kernel-core the repos offer
   is X" is false for an empty query, so the test cements a misdiagnosis of the same class
   the commit fixed.
3. Case 10 asserts only "non-zero and no marker" — a truncated heredoc, a renamed function
   or a `set -u` error all pass it. `$work/trace10/die` holds the answer and is never read.
   (The `die` + `set -e` propagation reasoning itself was confirmed correct by probe.)
4. The stub returns two NEVRAs per `repoquery`; nothing establishes that the real tool does
   without `--showduplicates`. Case 1 — the likelier lab case — rests entirely on it, and
   §8.2 disclosed the *format* as unverified but not the *multiplicity*. That is the
   overclaim.
5. `STUB_INSTALL_RC` is a dead knob: no case sets it, and none could while the harness ran
   the function without errexit… it advertises exactly the coverage finding #1 shows is
   missing.

## Nits

`/boot` default never exercised (`${2:-/bot}` would pass all twelve); the function's doc
comment omits `[boot-dir]`; the rpm query merges stderr into candidates while dnf routes it
to a file; `QA.md` "five `test-*` suites" stale at eighteen; `passed: N` with nothing
asserting what N should be; case 10 depends on case 9's leftover `$work/boot`.

## Confirmed clean

The no-grubby case holds up and is meaningful (`sort`/`cat` symlinks justified, failed
substitution caught by the `dnf-install` branch). No env-prefix leakage between cases.
Acceptance-first ordering genuinely catches a truncated extraction. Stderr hygiene of the
function clean. Public-repo scan clean. British English clean. Placement correct. Plan
Commit Rule satisfied. QA.md 7 + 25 = 32 verified against the live run.
`kernel-<v>-<r>.<arch>` **is** valid NEVRA spec form, and `uname -r` **does** match
`kernel-core`'s tags for stock kernels.

Gates run: `qa-all.bash` PASS (888 files); `plan-qa --sweep` 0 block / 2 advise, both
pre-existing; `shellcheck -S style` clean on both files.

## Disposition

| Finding                            | Action                                                                                 |
| ---------------------------------- | -------------------------------------------------------------------------------------- |
| BLOCKING — transaction status lost | Fixed. Reproduced first. Every guest-changing command now carries `\|\| die`, and the  |
|                                    | header records that `set -e` covers nothing in this function                           |
| BLOCKING — why the tests missed it | My first answer was WRONG and is corrected below the table                             |
| 1 — sorts unfalsifiable            | Fixed. Both stub lists arrive oldest-first; a mutant deleting both sorts now dies      |
| 2 — case 4 cements a misdiagnosis  | Fixed. An unusable repository and "only the running kernel" are told apart, with       |
|                                    | different messages, and case 4 fails if it sees case 3's                               |
| 3 — case 10 too weak               | Fixed. It reads `trace10/die` and requires the refusal to have been reached            |
| 4 — `--showduplicates` undisclosed | Fixed both ways: the flag is passed, the stub now HONOURS it rather than ignoring it,  |
|                                    | and §8.2 states the multiplicity assumption alongside the format one                   |
| 5 — `STUB_INSTALL_RC` dead         | Fixed. Reachable once the harness was faithful; case 6a drives it, 6b the bootloader   |
| Nits                               | All fixed except the rpm/dnf stderr asymmetry, which was also fixed — rpm's stderr now |
|                                    | goes to the same file dnf's does                                                       |

### Correction to my own diagnosis

I first recorded that twelve tests missed the blocking defect because **the harness ran
the function in a `( … )` subshell, where errexit is live**, and that moving to a command
substitution is what made the case expressible. That is wrong, and it reached the commit
message of `5d5c8d36`, the design doc, PLAN.md and the journal before I checked it.

This suite deliberately runs without `set -e`. A `( … )` subshell therefore inherited
errexit **off** — the same state a command substitution gives it. Measured by reverting
`run_case` to the old shape with the new case present: `a-failed-install-refuses` catches
the blocking mutant perfectly well.

The real reason is duller. **No case ever set `STUB_INSTALL_RC`.** It sat in the stub file
from the first draft, and the one command that actually changes the guest had its exit
status unexamined by anything. A knob I wrote, looked at repeatedly, and never asked why
nothing used.

The command-substitution shape was kept — matching the fixture's call syntax exactly is
worth a little on its own — but it fixed nothing, and the comment that claimed otherwise
has been replaced with one that says so.

Found while acting on the above, not raised by the reviewer: `grubby --set-default` and
`record` had the identical discarded-status shape as the install, and are guarded too.

Twelve mutants now run against this gate, all killed, including one for the blocking
defect and one for each should-fix. Fifteen cases; the suite refuses to report a pass if
the case count is not what it expects.
