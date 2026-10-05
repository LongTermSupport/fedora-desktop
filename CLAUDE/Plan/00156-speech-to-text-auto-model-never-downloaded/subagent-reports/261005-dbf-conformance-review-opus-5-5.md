**Verdict for `ready-wait-ignores-child-exit`: conforms, with two non-blocking gaps and some record-keeping gaps. I found nothing blocking.** The reviewer branch is not the one under review, so I made no changes to it. I used my own worktree detached at each commit instead of adding scratch worktrees, then switched back.

**What I could not check against the source.** I could not find the method specification (sections 3, 4, 7 and 8) on this host. I did not read it, so every finding below is judged against the checklist you gave me and carries no clause citation. The earlier Plan 00139 review names it as "Defence Before Fix method specification 1.0.1".

## Reproduction

- **Red, at abf1395c:** `bash scripts/qa-ready-wait-rules.bash` exits 1 with 8 findings: `wsi-stream:793`, `tmate-share:18`, `tmate-full:19`, `test-self-update-cycle:447`, `test-wsi-stop-grace:232`, `test-ccy-sessions-reboot:507`, `test-ccy-session-registry:340` and the Plan 00055 `acceptance.bash:459`. The message prints the stable id `ready-wait-ignores-child-exit` and points at CLAUDE/QA.md.
- **Green, at 91c9d541:** the same gate exits 0 ("passed: 592 failed: 0"). Both rule halves self-test against their fixtures on every run.
- **Unit tests:** `tests.helpers.ready_wait.test_bash_ready_waits` and `tests/speech_to_text/test_server_start.py` both pass.
- **Not run:** `qa-all.bash`. I checked its wiring by reading the diff: `ready-wait-rules` is a hard gate calling the same script.

## Earlier classes

- `scripts/qa-speech-to-text-rules.bash` is red at 6515dd7c: 24 findings, covering both `dropdown-label-carries-explanation` and `model-present-without-weights`.
- It is green at the branch tip I reviewed (my worktree is at F44, 2165b111, which has the same gate): 246 passed, 0 failed.
- I did not review those two classes further.

## Findings, most severe first

1. **Two instances are left unfixed with no referral upward (medium).**
   - **What was left:** #8 `helpers/vmtest/serial_console.py` and #15 `Completed/00111 insulation-steps.bash`. The report and the QA.md page give reasons for each, and PLAN.md says "14 of 16 fixed".
   - **What is missing:** the owner-referral content you listed. No count fixed or remaining as a formal item, no statement of what stopped the fixing, no "what to try next", and no Phase 4 task.
   - **Within the practitioner's authority:** #8 looks fixable. vmtest could pass the domain name to the helper, which would let it tell a dead QEMU from one still starting.
   - **To resolve:** either fix #8, or add a Phase 4 entry that refers both instances to the owner.
2. **The rule is narrower than the search showed (medium).** Search instances #6 (`docker-in-lxc`, after `lxc-start`) and #7 (`vmtest`, after `virsh start`) are shell waits for something this code started. The rule does not reach them because the start is not a `&`, and the report says that is "knowledge about the tool". The fixes are present and correct, but a later copy of the same shape would pass the gate.
   - **To resolve:** either add `lxc-start`, `virsh start` and `virt-install` as arming starts in the bash helper, or name this as the next wider rule not built, with the reason. The QA.md "where no rule reaches" list does state the reason in prose, but not as that named choice.
3. **No record of which instances were examined individually and which got the pattern (low).** The report gives a table of fixes and evidence but does not say this. The harness fixes #11 to #14 and tmate #3 and #4 are evidently by pattern. The record should say so.
4. **No instance count with sweep scope as a single statement (low).** The facts are scattered: 217 Python files, 371 shell scripts, 16 search instances, 8 rule findings. The final summary should say all four together.
5. **The reconciliation is not in the rule's favour (low).**
   - **What the report shows:** the rule found nothing the search missed, and the search found 8 things the rule does not reach (#2, #5 to #10, #15).
   - **What is recorded:** each gap has a reason, which is acceptable.
   - **Wording:** "reconciled in the rule's favour" should be stated as a decision, not implied by the table.
6. **The Ansible retry condition for `systemctl is-active` (low, risk only).** In the firewalld and ydotool plays, `until: rc in [0, 3]` treats an `activating` unit as dead, because `is-active` returns 3 for it. This is probably safe for `Type=dbus` and `Type=simple`, which become active when the start returns. It is worth a comment or a different probe. The sweep verified these plays only by `--syntax-check`.
7. **The Plan 00055 `acceptance.bash` fix is untested (low).** It runs only on the host and its evidence is shellcheck. The report says so honestly.
8. **A toolchain gap is correctly reported (no action).** The DBF plugin is not installed (Task 4.1).

## What I checked and found conforming

- **Order of work:**
  - The red commit abf1395c carries only the defence and no instance fixes.
  - It is followed by the sweep-fix 8965aa3b, then by 91c9d541, the wsi-stream fix with a reproducing test.
  - The report says a control run against the old `wsi-stream` fails 4 of the 6 test cases.
- **Independent search:**
  - It was committed on F44 in 6515dd7c, on 2026-10-04, before the rule's commit on 2026-10-05.
  - It states "no detector or rule directory was opened".
  - It lists seven search forms written before searching.
  - It has a table per technique ("what text search found that reading could not have checked", and the reverse).
- **No suppressions:** no baseline, `nosemgrep`, `noqa` or `shellcheck disable` was added. The fixes use real `kill -0`, `ps -p` and `wait`, with liveness read before readiness.
- **Rule permanence and wiring:**
  - `qa-all.bash` runs it as a hard gate.
  - The gate fails if a Python file it handed to semgrep is not scanned (exit 2).
  - The QA.md page is shipped with the project.
- **Calibration:** the report shows the bash rule's false positives on the way to the final form (24, 19, 12, then 7 bash and 1 Python).
- **Incidental:** the branch predates 00159, so a diff against current F44 shows 00159 as deleted. That is a base difference, not a change made by the branch.

This review was returned as a reply and saved here by the coordinator. The red and green gate outputs (`rw-red.out`, `rw-green.out`, `stt-red.out`, `stt-green.out`) were scratch files in the reviewer's own worktree and were not kept.
