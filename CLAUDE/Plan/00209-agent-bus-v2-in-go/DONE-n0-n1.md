# N0 and N1: the finished tasks, in full

Moved out of `PLAN.md` (Tasks 0.1 to 0.4 and 1.1 to 1.4a) so the plan stays within its size limit;
the text is unchanged. `PLAN.md` keeps each task's tick and a line saying what it was.

## Phase 0: N0, the design

- **Task 0.1**: Research v1 (`RESEARCH-*.md`); [DESIGN.md](DESIGN.md), reviewed by Fable and
  reworked once (`subagent-reports/261019-*`); M0's bookkeeping (`subagent-reports/261021-m0-*`).

- **Task 0.2**: The frame ([DESIGN-v2-FRAME.md](DESIGN-v2-FRAME.md)) and the four area
  designs A-D, each reviewed adversarially (`subagent-reports/261021-n0-*`).

- **Task 0.3**: The integration: [DESIGN-v2.md](DESIGN-v2.md), the chapters edited at their
  seams (section 3), 17 contradictions settled (section 5), the team id and INT3; DESIGN.md marked
  superseded (`subagent-reports/261021-n0-integration.md`).

- **Task 0.4**: The N0 review: the four integrated adversarial reviews (1 blocker, 37 majors,
  FIX-FIRST), the four-theme fix pass (`subagent-reports/261021-n0-merge.md`), verification in two
  scopes (`261021-n0-verify-ab.md`, `261021-n0-verify-cd.md`; 10 new majors), round 2
  (`261021-n0-r2-*.md`), the re-check (`261021-n0-recheck.md`; 5 majors), the qa-reviewer pass
  (`261021-n0-qa-reviewer.md`), round 3 (`261021-n0-r3-fix.md`) and the narrow check, verdict MERGE
  (`261021-n0-r3-check.md`); its two launch-line minors are fixed and its third is carried to N1
  (Task 1.7).

## Phase 1: N1, foundations

- **Task 1.1**: `internal/teamchat/paths`, the one source of every name and path (D 3.3).
  Built on `feat/bus-v2-n1-paths`, 100% coverage. Review round 3 was FIX-FIRST; its five findings
  are fixed in cb3736fa, and a narrow check of them is MERGE (`261021-n1-paths.md`,
  `261021-n1-paths-check.md`).
- **Task 1.2**: The static gate `teamchat-keeps-off-v1` with its v1 name list (D 4.1), in
  `qa-changed.bash` and `qa-all.bash` before the first v2 package merges. The merge into
  `feat/bus-v2-n1` adds to `allowed.tsv` the three rows for the matrix replay test
  (`261021-n1-paths.md`), and that merge is gated on `qa-teamchat-separate` exit 0.
- **Task 1.3**: `teamchat/proto`, test first: the wire, the roster type, the verdict and
  room-trust check, kinds, references, limits, the envelope, the shared corpus (A 2-13), and the
  room specs exported as data (seam 13). Built on `feat/bus-v2-n1-proto`, reviewed to MERGE in
  two rounds; its 57 decisions and their design-text fixes are in
  `subagent-reports/261021-n1-proto.md`.
- **Task 1.4**: A's host checks HC-A1 to HC-A7 (host or VM lab), before `proto` fixes the
  rules they decide. The server checks (HC-A1, HC-A2's server half, HC-A4, HC-A6, HC-A7, H-C12
  to H-C14) are judged in the container against the pinned Tuwunel 1.9.4 binary (the asset's
  sha256 is the pin's) by the plan-folder probe `hca_probe.py`: HC-A1 and H-C13 fail on one fact
  each, with design-text fixes proposed (`261021-n1-hostchecks.md`, the run in
  `261021-n1-hostchecks-run.md`). The Element checks (HC-A2's display, HC-A3, HC-A5) are owner
  steps in A 14's Element row, after gate A. The lasting regression form of HC-A1 and H-C13 is
  the `tuwunel`-tagged Go test (SPEC-matrix M44, `internal/teamchat/matrix/tuwunel_test.go`),
  which runs in the container against the same binary; `hca_probe.py` stays the record of the
  run that decided the design. **Gate A** (Task 0.5): a check that would leave the
  `teamchat` name in a unit or path on the host runs in the VM lab until the owner has confirmed
  the irreversible decisions.
- **Task 1.5**: The matrix client and `fakehs`, built and reviewed on
  `feat/bus-v2-m1-matrix` (58c7b6e1): rebuilt off 35acceab (Phase 7), placed under
  `internal/teamchat/` (D 3.3), merged with its first caller (N2 or N3), not alone.
  Built and reviewed on `feat/bus-v2-n1-matrix` (9f42a773; review MERGE in round 3, `261021-n1-matrix.md`):
  the move, `LoginDevice`, `Logout`, `DeleteDevice`, room account data, `Redact` and presence, with fakehs
  following the Task 1.4 facts. M44, the tagged test, was run in the container against the
  pinned Tuwunel 1.9.4 binary (`-race`, with `-count=3`) and passes, HC-A1's presence case included,
  once its config carried v2's login limits and `db_pool_workers = 4` (`261021-n1-matrix.md`,
  section "The M44 run"; review in `261021-n1-m44-review.md`). Its only red check is GO-2026-6809 (Task 1.6). It stays on the
  N1 branch until its first caller (N2 or N3) is ready to merge with it.
- **Task 1.4a**: The design-text fixes Task 1.4's checks call for, for the design pass
  (`261021-n1-hostchecks.md` findings 1, 2 and 4): presence never uses `set_presence=offline`
  (A 6.6, A 14, A 15, A 16, B 2.6 step 09) and C 3.2.4 sets both presence timers and
  `server_user_localpart`; DC16's take chosen from the options given, with C 3.7.3 step 3 and C
  15's seats row changed to match; A 16 notes that HC-A2's display, HC-A3 and HC-A5 are owner
  steps after gate A, and that 7.5 and 10.4 in `proto` stay provisional until HC-A3 is run.
  Done: H-C13 makes a take log in, log out, then log in; only the link syncs, with presence
  timers of 120 s and a further 60 s; restore_epoch is checked after each sync answer, before
  anything is saved (`261021-n1-hc-design.md`, narrow check MERGE in
  `261021-n1-hc-design-check.md`).
