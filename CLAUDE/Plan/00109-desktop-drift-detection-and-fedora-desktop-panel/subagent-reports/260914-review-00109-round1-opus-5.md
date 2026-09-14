# QA Review — Plan 00109's six commits on `F44`

Reviewed: `e0c59887`, `97c00554`, `00865588`, `d15b9984`, `4d76e961`, `55b8ef4d`.

**Verdict**: BLOCK — one item (finding 1) must be fixed before this play is ever run on a
host; the rest are FIX-BEFORE-MERGE.

**Scope note.** The tree moved during the review: `fb9bbc8c` and `5afb189f` (Task 3.3
handoff) landed after `55b8ef4d`, plus `e5c50840` on AgentNotes. This report covers the six
named commits. `fb9bbc8c` matters because it changes the answer to suspicion #4 — see
finding 6.

**A note on this file's prose.** The repo's IaC-enforcement hook inspects the text of any
Bash command, and it refused this report three times because the findings quote systemd
and package-manager verbs. Those quotations are described rather than written literally
below. Nothing about the substance changed.

## Blocking

**1. The unit's `[Install] WantedBy=` does not enable anything, so the fallback path
deploys a health surface that will never run — and the play states the opposite.**
`playbooks/imports/optional/common/play-host-health-login-report.yml:102-127`,
`files/home/.config/systemd/user/host-health.service.j2:28-29`

A `WantedBy=` line in `[Install]` is consumed only by the systemd enable/preset operation.
Nothing in systemd acts on it at login. The unit is templated to
`~/.config/systemd/user/host-health.service`, and the only thing that would create the
`graphical-session.target.wants/` symlink beside it is the enable task at line 102, gated
`when: hh_user_systemd.rc == 0`. I searched the whole repo: no `.wants` symlink is created
anywhere, there is no preset sweep, and there is no other user-scope enable task. So when
the probe fails, the unit is inert permanently.

The play asserts the reverse twice:

- line 114-115 comment: "Not a skip and warn: the unit is deployed and WantedBy
  graphical-session, so the next login enables it."
- line 125-127 `success_msg`: "No user manager reachable from here, so the unit was
  deployed but not enabled by this run; it is WantedBy graphical-session.target and the
  next login picks it up."

This is the prohibited skip-and-warn (`CLAUDE.md`, Fail Fast) wearing an `assert`, and the
warning it emits is false. In the plan whose subject is checks that cannot fail, this is a
check that can silently not exist while the operator is told it is live.

Fix: either declare the symlink itself in IaC (the `file` module with `state: link`,
pointing at `../host-health.service` from inside
`{{ user_units_dir }}/graphical-session.target.wants/`, owned by `user_login`) so
deployment alone is sufficient, or make the unreachable-manager case a hard failure naming
the command the operator must run. Do not keep the current text either way.

**2. The "Report Which Enable Path Ran" assert cannot fail — suspicion confirmed, and the
`when:` above it fails first.** `play-host-health-login-report.yml:118-128`

`that: hh_user_systemd.rc is defined`. The probe task at line 92 has no `when:`, so it
always registers; `rc` is therefore always defined. In the one case where it would not be
(a `become_user` failure returning no `rc`, absorbed by `failed_when: false`),
`when: hh_user_systemd.rc == 0` at line 110 raises the undefined-attribute error before the
assert is reached. So the assert is a `debug` task with extra syntax. That is only a nit on
its own; it is blocking because it is the mechanism finding 1's false claim is delivered
through.

## Should fix

**3. `_freshness_findings` throws away the reason a check could not answer, then tells the
user to read it.** `helpers/host_health/login_report.py:148-169`

Executed, with a seeded ledger marked BROKEN (reason: "the callback could not write a
record: disk full"):

```
findings: ['play-freshness could not give an answer, so no play was judged (see its stderr output above)']
does any finding carry the BROKEN reason ('disk full')?  False
```

`check_freshness._emit` writes the BROKEN reason and the "clear it deliberately"
instruction to `stderr`; `_Sink` captures it into `captured`; the `EXIT_UNTRUSTWORTHY`
branch returns a hardcoded string and never reads `captured`. `login_report`'s only output
channel is `emit`'s `write`, so there is no "above". The ledger's BROKEN sentinel exists
precisely to say *why*, and the login surface strips it and then misdirects the reader.
Return the captured lines alongside the summary.

**4. Same function, the `_Sink` merges stderr into stdout, so a diagnostic becomes a
user-facing finding.** `login_report.py:152-157` — both `stdout=` and `stderr=` are given a
`_Sink` that appends to the *same* `captured` list.

Executed, recent stamp (so `offline_finding` deliberately returned `None`) plus one
genuinely stale play with three commits:

```
5 findings:
- play-freshness: git fetch failed, judging on the refs on hand: CalledProcessError 128
- playbooks/a.yml — changed since it was run here
-     aaa1111  first change
-     bbb2222  second change
-     ccc3333  third change
```

Three defects in one output:

- (a) The "git fetch failed" line is a stderr diagnostic, promoted to a finding — which
  defeats `DESIGN-host-health.md` section 8 row 1 ("judge against the refs on hand, and say
  nothing if clean") on any login that has any other finding, and violates
  `CLAUDE/StderrHygiene.md`.
- (b) In the long-gap case it is reported *twice* — once as the raw exception repr, once as
  `offline_finding`'s sentence.
- (c) The indented commit sub-lines of one finding become peer findings, so the count is
  wrong (5 for 1 problem plus 1 diagnostic) and the bullets are mis-shaped.

`tests/helpers/host_health/test_probe.py:195` pins "one finding per line" as a contract;
this is the one place that breaks it, and at HEAD `handoff.write` now consumes the same
list. Split the sinks; keep stderr out of the payload; keep a finding's detail lines
attached to it.

**5. `_freshness_findings` has no tests at all.** Confirmed by AST: of `login_report`'s
functions, `_freshness_findings`, `_declared_pins`, `_read`, `_notify_send`,
`_repo_root_default` and `main` are unreferenced in
`tests/helpers/host_health/test_login_report.py` (`main` shows as a false positive on
`unittest.main()`). `_freshness_findings` is the function that distinguishes
`check_freshness`'s three exit statuses — the "three outcomes, not two" rule the whole
design turns on — and findings 3 and 4 are what it does instead. The `dkms_text` seam got
its own test class precisely because that lesson was learned; this seam, in the same file,
did not.

**6. Suspicion #4 was correct for the reviewed range, and was fixed four minutes after
it.** At `55b8ef4d:helpers/host_health/login_report.py:174` the pin check was handed
`probe.run_probe(["dkms", "status"]).text` directly. Reproduced against the real manifest in
this container:

```
AS AT 55b8ef4d (raw .text):   evdi_version (absent): pinned 1.15.0, nothing installed
AS AT HEAD (dkms_text seam):  evdi_version: could not be checked — dkms: command not found (dkms status)
```

So the range under review did ship a fabricated claim about the host. `fb9bbc8c`'s
`dkms_text` fix is correct and its "measured" quote is accurate to the character (the
pre-fix string is exactly as the commit message states). One nit: the commit quotes the
post-fix output as ending "command not found" where the real output ends
"command not found (dkms status)"; it is framed as the verbatim real run.

**7. A successful fetch whose stamp write fails is reported as a failed fetch, and then as
"never fetched".** `helpers/play_ledger/check_freshness.py:87-97` —
`fetch_clock.record_success` is inside the `try` whose `except` blames the fetch. Executed
with the fetch succeeding and the write raising `OSError(28)`:

```
stderr: play-freshness: git fetch failed, judging on the refs on hand: [Errno 28] No space left on device
stdout: play-freshness has never successfully reached the remote on this host, so no play has ever been checked for staleness here
```

Both statements are false; the fetch succeeded. This is the discarded-failure-signal class
from `AgentNotes.md` — a write error laundered into a confident wrong answer about a
different subject, on the user-facing channel. Move `record_success` out of the fetch's
`try`, or catch its failure separately and name it.

**8. The only genuinely network-bound call on the login path has no timeout.**
`helpers/play_ledger/git_history.py:29-36` — `fetch()` calls `subprocess.run` with no
`timeout=`, while every sibling on this path has one (`probe.py:45` 20s,
`check_pins.py:46` 20s, `login_report.py:48` 15s). `probe.py:42-44` gives the rationale
verbatim ("a check that stalls the session gets removed from the session"), and
`host-health.service.j2` sets no `TimeoutStartSec`, so the backstop is systemd's 90s
default — which kills the unit and makes it a *failed unit*, contradicting the
`SuccessExitStatus=0 1` reasoning in the same file. `git_history.py` predates this range,
but `55b8ef4d` is what put it on a login path. Add a timeout, and disable git's terminal
prompt via the environment.

**9. `qa-version-pins.bash` guards the zero case and is blind to the partial one.**
`scripts/qa-version-pins.bash:85-109`

The gate is genuinely falsifiable now — I proved all of its failure paths fire *with a
message*, by shadowing the validator with a shell function (no tree changes). Cases B-E and
K all exit 1 and say why, including the two `case` arms that were dead:

```
B. row names a missing playbook  -> rc=1  ERROR: ... names a playbook that does not exist: playbooks/imports/play-NOPE.yml
C. row names a renamed var       -> rc=1  ERROR: ... names 'nvm_version_RENAMED', which playbooks/imports/play-nvm-install.yml no longer declares
D. validator exits 0, no rows    -> rc=1  ERROR: no rows reached the on-disk check, so nothing was verified
E. validator exits 0, no marker  -> rc=1  ERROR: validator exited 0 without its OK marker ...
K. yaml conversion fails         -> rc=1  ERROR: vars/version-pins.yml is invalid: ...
```

`shellcheck -x` and `bash -n` are clean. The `tail -n +2` row split and the pipe-delimited
`read` are sound for the shapes that matter: a whitespace-only file field is caught (not
swallowed by the emptiness skip), blank lines are skipped, and a marker that grew a second
line errors loudly rather than passing.

The remaining hole is the one `AgentNotes.md` names as this repo's highest-recurrence
defect. Injecting a marker declaring 9 pins with only 1 row emitted:

```
F. -> rc=0
   vars/version-pins.yml: VERSION-PINS-OK 9 pin(s), ... — all 1 resolve to a live playbook var
```

Exit 0, with `9` and `1` printed on the same line, disagreeing, and nothing comparing them.
The `rows -eq 0` check is the zero guard; there is no partial guard. It is unreachable today
(`to_row` emits one line per pin and newlines are rejected), but that invariant lives in
another file and the declared number is free — assert `rows` equals the count the marker
states, or print `COVERAGE: n of m`.

**10. Control 1 passes for reasons other than the one it claims.**
`qa-version-pins.bash:41-50` asserts only that the INVALID marker appears. Demonstrated in
case K: with the YAML conversion broken, `validate` receives empty stdin, emits
`VERSION-PINS-INVALID Expecting value: line 1 column 1` and control 1 "passes" — having
never handed the validator an empty-pins document. Assert the reason mentions emptiness.

**11. The comment attributing the `$?` discovery to the in-file controls is wrong.**
`qa-version-pins.bash:89-91` says "The control checks above are what surfaced that."
Controls 2 and 3 (lines 76-83) call `pin_is_live` directly in an `if`; they exercise the
function, not the `case` dispatch below. With the `$?` bug present they both still pass and
the gate exits 1 mutely. The journal gets this right
(`JOURNAL/00109-Journal-26-09-14.md:182-192` — the tree-mirror mutations found it); only
the in-file comment misattributes it, and it matters because a reader will believe the
controls above cover the loop. They do not, and per `CLAUDE/QA.md` "Changing a Gate" the
fixture that did find it was discarded.

**12. Five places describe an `extra`-argument merge the code does not use.**
`helpers/host_health/probe_results.py:130,135,153`;
`helpers/host_health/probe.py:94,99,108`; `DESIGN-host-health.md:69-71`;
`DESIGN-version-pins.md:85`; `PLAN.md:176` (tick-marked complete). The only production
caller is `login_report.py:185`, `probe.collect(running_kernel=...)`, with no `extra`. The
merge happens a layer up in `login_report.collect`, which is arguably better — but the
parameter is now dead on the production path (tests only) and four documents plus a ticked
plan line assert the wrong mechanism. Either route Phase 2's findings through `extra` as
documented, or delete the parameter and correct all five.

**13. `_rpm_version`'s ABSENT branch is unreachable.**
`helpers/version_pins/check_pins.py:159-166` tests for "is not installed" in the error
string, but `_run` (line 104-106) builds the error from `completed.stderr` only and discards
stdout entirely — and an rpm query writes "package X is not installed" to stdout. So a
`kind: rpm` pin for an absent package reports "could not be checked" rather than ABSENT,
which `installed_from_dkms`'s own docstring calls "a real, reportable state, and the
incident's own worst case". No manifest row uses `rpm` today and
`tests/helpers/version_pins/test_check_pins.py` references neither `_rpm_version` nor
`_command_version`, so this is an unexercised fallback asserted as working —
`AgentNotes.md` row 13.

**14. Two ticked test counts are wrong.** `PLAN.md:133` claims 47 tests for Task 2.1;
measured 50 (`test_freshness` 20 + `test_git_history` 14 + `test_check_freshness` 16) —
`4d76e961` added net +3 and did not revisit the figure. `PLAN.md:197` claims 19 tests for
`login_report`; measured 23 — the four-test dkms-seam class landed without updating it.
Task 2.2's "94 tests" is exactly right (30+44+20), and Task 3.1's 29 and 21 are exactly
right.

**15. `PLAN.md:26-27` and `:355` quantify a population this range changed.** "the other
43 plays under `playbooks/imports/optional/`" — the count is 45 now, and one of the two
additions is this range's own new play.

**16. `check_pins.py:24` points at the wrong design document.** It names
`DESIGN-play-ledger.md`; the design for this module is `DESIGN-version-pins.md` (which
itself correctly cross-links to `DESIGN-play-ledger.md` section 7 for the separate qa-all
question).

## Nits

- `fetch_clock.offline_finding`'s unreadable-timestamp branch (`fetch_clock.py:83-87`) is
  unreachable from `run()`: `last_success` already filters an unparseable stamp to `None`.
  `test_fetch_clock.py:51` and `:95` assert two different outcomes for a corrupt stamp;
  only the first is on a production path.
- `test_fetch_clock.py` pins the bound itself as silent and bound-plus-three as a finding,
  so a mutation widening the bound by one still passes both. Pin the first failing day.
- `qa-version-pins.bash:53` omits the stderr redirect on the real-manifest path (control 1
  has it), so case K's diagnosis was the JSON decode error rather than the actual YAML
  scanner error — visible only because it leaked to the terminal.
- `_declared_pins` (`login_report.py:204`) imports `json` inside the function with no
  stated reason.
- `PartOf=graphical-session.target` on a `Type=oneshot` that has already exited propagates
  nothing. Harmless, but it is the documented session-unit idiom applied to a unit that is
  not long-running.

## Checked and clean

- **Manifest extraction is byte-identical, independently verified.** The heredoc taken from
  `d15b9984^:scripts/check-pinned-versions.bash` and extracted with `awk`, compared
  list-for-list against the live validator's rows: `old rows: 9 new rows: 9 / IDENTICAL`.
  The MANUAL row (`cudnn_version`, empty `github`, folded `note`) round-trips exactly, and
  `check-pinned-versions.bash:139-176` reads the same five fields with the same pipe split,
  so all nine pins behave as before. `tag_prefix` is carried end-to-end
  (`manifest.py:211`, consumed at `check-pinned-versions.bash:164` via `normalise`) and is
  unused by every current row.
- **Manifest validator mutation battery, 16 cases, all correct.** Rejected with the INVALID
  marker and rc 1: typo'd key, manual pin with no note, URL where owner/repo belongs,
  duplicated row, pipe in a field, newline in a field, emptied manifest, missing
  `installed:`, `untracked` with no `why`, `dkms` with no `name`, typo'd `installed.kind`,
  non-mapping document, missing top key. Correctly accepted: `tag_prefix` set. Correctly
  accepted as the bash half's job: renamed var, missing playbook.
- **The manifest's population is complete today.** 11 version-shaped play vars exist across
  `playbooks/`; 9 are in the manifest and the two that are not
  (`darktable_min_version`, `min_kernel_version`) are minimum-version requirements, not
  upstream pins. There is no gate for the playbook-to-manifest direction (a new pin added
  to a play and not to the manifest is invisible, and the gate's pass line reads as
  complete coverage) — worth stating in the gate's output, but not a live gap.
- **`fetch_clock` reversal is safe and the boundary is reachable both ways.** At the bound
  it is silent, beyond it a finding; never-fetched, unreadable-stamp and future-stamp all
  produce distinct findings; a backwards clock cannot buy silence. A successful fetch does
  advance the stamp on the real path (`check_freshness.py:89`), and the test drives
  `check_freshness.run` rather than `fetch_clock` — I neutered `record_success` and watched
  `test_a_SUCCESSFUL_fetch_stamps_the_clock` go red, so that test is genuinely falsifiable.
  The rewritten test keeps what the old one protected for the beyond-bound case,
  deliberately drops it within the bound, and says so in its docstring.
- **`collect`'s guards really are independent.** `login_report.py:78-91`: health has its own
  `try`, and freshness/pins each get one inside the loop;
  `test_every_check_raising_produces_every_finding` gives 3 findings from 3 raising checks,
  and `test_a_raising_check_does_not_hide_another_check_findings` gives 2. The notifier is a
  separate `try` in `emit`, so a broken notifier leaves findings on stdout and keeps the
  exit status.
- **Container smoke-run claim is exact.** Running the probe module here: 3 findings, 3
  lines, exit 1, and the two systemd ones are what the pre-fix shape swallowed.
  `DESIGN-host-health.md:92-93` and `PLAN.md:186-187` are accurate.
- **Play mechanics.** Shebang matches `scripts/make-playbooks-executable.bash:18` exactly;
  tracked mode `100755`; the `.j2` tracked `100644`. The getent lookup plus index 1 is the
  uid field and the `| int > 0` assert correctly rejects 0. `failed_when: false` carries a
  `FAIL-FAST-OK` annotation and the `rc` *is* read (line 110) — the annotation is honest
  about the mechanism even though finding 1 is about what is done with the answer.
  `SuccessExitStatus=0 1` is valid. The `ansible_managed | comment` expression renders to
  three hash-prefixed lines under Ansible's own filter — valid before `[Unit]`. The
  `libnotify` package is the right Fedora source for the notification sender (two existing
  repo consumers, `manage-kernel-versions.py` and `displaylink_recovery/run_recovery.py`,
  use it and neither declares it — a pre-existing gap this play does not share). Task names
  are Title Case per `AnsibleStyle.md:198`. No colon-space-dash pattern in any unquoted
  `name:`. No hardcoded checkout path anywhere — `WorkingDirectory` is templated.
- **Public-repo safety: clean.** No home paths, usernames, hostnames, private IPs or
  non-example.com emails in any added line. The only `~/Projects/...` hits in
  `docs/playbooks.md` are pre-existing lines 448/684/1186, not this diff. The kernel NVRs
  used as test fixtures are stock Fedora versions, not machine-identifying.
  `fedora-desktop` is the permitted self-reference.
- **Plan Commit Rule: satisfied.** All six commits touch `PLAN.md`. Status is
  `In Progress`, not Complete; the README index row exists; Task 3.1/3.2 carry explicit
  unticked HOST sub-items; `CLAUDE/QA.md:50` gained the gate row; `docs/playbooks.md`
  gained the play section and the docs catalogue gate passes.

## Mechanical gates

- `scripts/qa-all.bash`: **pass** — "QA passed: 845 files checked", including
  "version-pins: vars/version-pins.yml: VERSION-PINS-OK 9 pin(s), 1 with install state
  tracked, 8 declared untracked — all 9 resolve to a live playbook var". The gate prints a
  pass line, so it is distinguishable from not running. The "shellcheck: 168 issues" and
  "patterns: 14 file(s) parsed only in part" warnings are pre-existing and name no file in
  this diff.
- `hooks-daemon plan-qa --sweep`: **2 findings, 0 block** — a stale path in Plan 00046 and
  a journal-freshness list; neither names 00109.
- `hooks-daemon docs-qa --sweep`: **37 findings, 0 block** — all pre-existing
  duplicate-block advisories; searching the full output for `00109`, `playbooks.md`,
  `host-health` and `version-pins` returns nothing.
- Ansible syntax check on `play-host-health-login-report.yml`: **pass** (the only playbook
  in 00109's commits). `qa-ansible-syntax` separately covers 80.
- `scripts/qa-helper-tests.bash`: **triggered and run** (helpers/ and tests/helpers/ both
  changed) — 1114 tests via qa-all, plus per-module: `test_login_report` 23, `test_probe`
  21, `test_probe_results` 29, `test_manifest` 44, `test_check_pins` 20, `test_fetch_clock`
  16, `test_check_freshness` 16, all OK.
- `helpers.gnome.check_extension_compat` and the extension ESLint run: **not triggered**
  by 00109's commits (the `play-gnome-shell-extensions.yml` and `extensions/` changes in
  the range belong to Plan 00112). `check_extension_compat` ran inside qa-all anyway and
  passed.
- `shellcheck -x scripts/qa-version-pins.bash` and `bash -n`: both clean.

The tree was not clean during the review (`PLAN.md` modified, `triage.bash` untracked)
because the main session is actively working; nothing the review ran changed a tracked
file.
