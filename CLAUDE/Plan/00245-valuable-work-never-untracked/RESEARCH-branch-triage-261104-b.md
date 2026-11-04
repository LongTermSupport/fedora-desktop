# Branch triage 261104, batch B (34 remote branches)

Method: `git cherry`, `git merge-tree --write-tree` against origin/F44, per-file blob comparison
against origin/F44 and against origin/spike/v2-merge-up (the v2 landing line, 857 commits ahead of
F44, which has F44 merged in), and the plan text that names each branch. Read-only on git.

Key finding: the v2 agent-bus line (n2, n3, n3e, n3f, remote-seats-note) is wholly contained in
origin/spike/v2-merge-up. The M1 and M2 milestone branches were built on the retired
`internal/bus/{proto,state}` and `internal/pingbus/hooks` layout, which neither F44 nor the spike
carries; Plan 00209 (Task 7.2 and the 00213 Task 3.1 note) drops them "where v2 supersedes them".
The three `fix/*` WIP rescues are stale-tree reversions, not work.

| Branch                                     | Verdict         | Evidence (one line)                                                                                                                                                                      | Plan                |
| ------------------------------------------ | --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- |
| origin/feat/bus-v2-m1-proto-rooms          | SUPERSEDED      | `internal/bus/proto` absent from F44 and spike; `teamchat/proto` (00209 Task 1.3) replaces it; 00209 Task 7.2 drops it; tip is a WIP rescue                                              | 00209               |
| origin/feat/bus-v2-m1-state                | SUPERSEDED      | `internal/bus/state` absent from F44 and spike; 00209 Task 7.2 drops m1-state\*; 12 patches not on F44                                                                                   | 00209               |
| origin/feat/bus-v2-m1-state-locks          | SUPERSEDED      | same layout as m1-state, 9 patches, a cut of the same work; 00209 Task 7.2                                                                                                               | 00209               |
| origin/feat/bus-v2-m2-hooks                | SUPERSEDED      | PLAN.md 00213 Task 3.1 note states "feat/bus-v2-m2-hooks is superseded"; v2 hooks are `teamchat hook`                                                                                    | 00209, 00213        |
| origin/feat/bus-v2-m2-hooks-golden         | SUPERSEDED      | ancestor of m2-hooks (850 golden fixtures for the superseded hooks port)                                                                                                                 | 00209, 00213        |
| origin/feat/bus-v2-n2                      | SUPERSEDED      | ancestor of origin/spike/v2-merge-up (and spike/v2-cross-network), the v2 landing line; 264 patches not on F44                                                                           | 00209               |
| origin/feat/bus-v2-n3                      | SUPERSEDED      | ancestor of n3e, n3e-seam, remote-seats-note and the spike; nothing of its own                                                                                                           | 00209               |
| origin/feat/bus-v2-n3e                     | SUPERSEDED      | ancestor of origin/spike/v2-merge-up; nothing of its own                                                                                                                                 | 00209               |
| origin/feat/bus-v2-n3e-seam                | SUPERSEDED      | n3e plus one WIP-rescue commit adding a single report file, which the spike already holds                                                                                                | 00209               |
| origin/feat/bus-v2-n3f                     | SUPERSEDED      | ancestor of origin/spike/v2-merge-up (spec-only commits on top of n3e, all in the spike)                                                                                                 | 00209               |
| origin/feat/bus-v2-remote-seats-note       | SUPERSEDED      | ancestor of n3e and of the spike                                                                                                                                                         | 00209, 00220        |
| origin/feat/bus-v2-s1-declare              | IN-F44          | Stage 1 landed by the F44 merge "Plan 00209 stage 1 seat-read failure fix"; F44 has `bus/declare`, `bus/seat`, `internal/declare` and later fixes; only a stage-1 merge report is unique | 00209               |
| origin/feat/ccy-teams-hot-add              | SUPERSEDED      | own change is plan text and one report; hot add and remove landed on F44 by Plan 00203 M3 (CCY 3.99.8)                                                                                   | 00177, 00194, 00203 |
| origin/feat/matrix-clients-element-install | FINISHED        | `tasks/element-desktop.yml` shared by two plays plus plan 00244 journal, merge-tree clean, F44 merged in today                                                                           | 00244               |
| origin/feat/pingbus-compact-before-wake    | SUPERSEDED      | Plan 00194 Task 2.0e is marked cancelled and folded into 00209 Task 3.1 (Go SessionEnd hook); v1 is feature-frozen                                                                       | 00194, 00209        |
| origin/feat/release-version                | FINISHED        | `internal/releasever` plus the release-workflow design and owner decisions D1-D4; 605 added lines, merge-tree clean                                                                      | 00153               |
| origin/feature/ccy-token-expiry-colours    | IN-F44          | F44's token-management.bash already has `colorize_expiry` and `token_expiry_label` (a later form); May branch                                                                            | none                |
| origin/fix/ccy-lifecycle-daemon-launcher   | IN-F44          | F44 entrypoint.sh already accepts a `*/claude-supervise` launcher (later form of the 3.79.2 fix)                                                                                         | none                |
| origin/fix/ccy-teams-launch-start-window   | FINISHED        | fake-clock fix plus semgrep rule `bash-test-time-window` (Defence Before Fix), 101 added lines, merge-tree clean                                                                         | 00194               |
| origin/fix/lxc-checkout-typed              | JUNK            | one WIP-rescue commit on a parent already in F44; 59 of 73 modified files are older F44 versions, 14 deleted; reverts newer work                                                         | 00203               |
| origin/fix/qa-changed-gate-args            | FINISHED        | qa-changed passes each qa-gates.tsv row's arguments, red test first; 3 files, merge-tree clean                                                                                           | 00245               |
| origin/fix/teams-full-handles              | JUNK            | one WIP-rescue commit on a parent already in F44; 80 of 85 modified files are older F44 versions, 7 deleted (removes the heading feature)                                                | 00201, 00204        |
| origin/fix/teams-wait-play-lock            | JUNK            | one WIP-rescue commit on a parent already in F44; 91 of 119 modified files are older F44 versions, 34 deleted                                                                            | 00201, 00203        |
| origin/go-port/agent-bus-install           | SUPERSEDED      | Plan 00192 Task 3.2 records it on hold: v2 `teamchat-admin` replaces the installer; F44 still runs the bash installer; 24+ conflicts                                                     | 00192, 00209        |
| origin/go-qa-ci/adopt                      | SUPERSEDED      | F44 already adopted go-qa-ci at a later pin (58e6f29ec, e60b218cb); `scripts/qa-go.bash` retired there                                                                                   | 00191               |
| origin/plan-team-liaison-role              | WORTH-FINISHING | Plan 00225 exists nowhere on F44 or the spike; Not Started, review r5 ended FIX-FIRST with fixes not re-reviewed; conflict only in Plan/README.md                                        | 00225               |
| origin/rescue/bus-v2-m1-proto-envelope     | SUPERSEDED      | WIP rescue of the retired `bus/proto` slice (envelope); `teamchat/proto` replaces it                                                                                                     | 00209               |
| origin/rescue/bus-v2-m1-proto-limits       | SUPERSEDED      | same, limits slice                                                                                                                                                                       | 00209               |
| origin/rescue/bus-v2-m1-proto-ping         | SUPERSEDED      | same, ping slice; a subset of the rooms rescue                                                                                                                                           | 00209               |
| origin/rescue/bus-v2-m1-proto-rooms        | SUPERSEDED      | same, rooms slice; the largest of the four                                                                                                                                               | 00209               |
| origin/research/bus-stack                  | FINISHED        | Plan 00203 research report (462 lines) not on F44 or the spike; conflicts only in one day-journal file                                                                                   | 00203               |
| origin/research/v2-scope                   | FINISHED        | `SCOPE-REVIEW.md` for Plan 00209, one new file, not on F44 or the spike, merge-tree clean                                                                                                | 00209               |
| origin/review/lxc-checkout-typed           | FINISHED        | docs-only review report (FIX-FIRST), one new file, merge-tree clean; low value, its subject already reached F44                                                                          | 00203               |
| origin/review/lxc-checkout-typed-final     | FINISHED        | docs-only final review report, one new file, merge-tree clean; low value, same subject                                                                                                   | 00203               |

Counts: SUPERSEDED 19, FINISHED 8, IN-F44 3, JUNK 3, WORTH-FINISHING 1.

## Uncertain

- The SUPERSEDED v2 branches (n2, n3, n3e, n3e-seam, n3f, remote-seats-note) are safe to drop only
  once origin/spike/v2-merge-up lands; until then they are ancestors of the one live carrier, not
  independent work. The spike itself conflicts with the current F44 (it merged F44 earlier).
- M1/M2 `bus/proto` and `bus/state` carry a shared test corpus (python-parity) that the v2
  `teamchat/proto` task may want to reuse; I judged from Plan 00209 text, not by reading the code.
  Keep rescue/bus-v2-m1-proto-rooms and feat/bus-v2-m1-state if the owner wants the corpus.
- The `fix/*` JUNK rescues: 14, 5 and 28 modified files have blobs not seen in F44 history; the
  samples I diffed were stale-base edits, but I did not read all of them.
- The two review/lxc-checkout-typed reports and research/bus-stack are records, not code; whether
  they deserve a home in the plan folder is the coordinator's call.
- fix/qa-changed-gate-args: an active agent of that name exists in this session, so it may be
  mid-work on the same change.
