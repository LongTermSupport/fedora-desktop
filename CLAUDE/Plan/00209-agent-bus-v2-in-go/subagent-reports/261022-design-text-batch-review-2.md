# Plan 00209: design text batch review, round 2 (8c18dee2c..2e8e10594)

Verdict: **MERGE**. 0 blocking, 1 low (owed follow-up).

## Round 1 findings, re-checked

01. A 3.2: closed. The invented NOTICE is gone. Step 4 now lists `proto` under the roster's
    `features`, so by 3.1 a returning old kit treats its own team as untrusted and names the
    upgrade. This matches FRAME 127-129 and is kind-neutral.
02. `coordinator_wanted`: closed.
    - C 3.11.3 gains `"coordinator_wanted": null | {"handle", "fix"}`, as team state that is never
      a problem.
    - C 3.6.4 points at it.
03. Member-scoped problems: closed.
    - The schema example shows both forms.
    - A bullet makes `member` optional, as SPEC-n2e SR1 does.
    - The launch-failed and disconnected bullets name `member`.
    - C 3.11.2 step 3 says the invited coordinator's problem carries `member`.
    - The exit rule reads "with no `member`".
04. C32: closed (`offline`, or `deferred` with no handle yet).
05. C 3.9.2 step 03: closed (the line is spelled out in full).
06. The mid-phase output and record status: closed.
    - C now says the line prints `DEFERRED ...`, and the record status is `offline` when a handle
      exists, else `deferred`.
    - SPEC-n3 LX6 matches it ("its output line stays the `DEFERRED`").
07. C 3.12.2 steps 05 and 09: closed.
    - Step 05 lists `rooms.json` in `restore.secure`'s stdin.
    - Step 09 says "the rooms now in the control state (the tar's, swapped in at step 8)".
08. BD2: closed (`--kind other`). All three `run --plain` examples in B now carry `--kind`.
09. Exit 1: closed. B 2.13 lists 1, and B 2.2.4 row 1 reads "a bug, or an I/O failure the verb
    cannot recover".
10. The ledger `--team` clause: closed. It moved to the `reply` sentence at B:1841, and the B 2.4.1
    paragraph reads cleanly.

Nothing else in the diff regressed. The table reflows in B and C changed no cell text beyond the
fixes above.

## New finding

1. **Low (owed, not blocking): `SPEC-n2e.md:20-21` (SR1), against `DESIGN-v2-C-host.md` 3.11.3.**
   - SR1's decoder is strict: "every key of the example and no other". C's example now has
     `coordinator_wanted`, but SR1 does not name it, and neither does the built `status` codec in
     n2e.
   - No writer emits the key yet, so nothing breaks today. But the first writer that sets it would
     produce records the reader refuses.
   - Fix: when B4's code is built (n2e view or N4), amend SR1 and the codec in the same slice:
     `coordinator_wanted` is null or `{handle, fix}`, the handle passes its grammar, and it is never
     counted by `TeamProblems()`. Until then, record it as owed in the audit's "Left" list.
