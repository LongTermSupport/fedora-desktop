# Branch triage 2026-11-03

Fetched and pruned origin first. Scope: 50 remote branches (less F44/F42/F43), 160 local branches not merged into origin/F44, 193 worktrees. Unique names classified: 154.

## SUPERSEDED (107)

branch | where | plan | ahead | last commit | cause | reason
--- | --- | --- | --- | --- | --- | ---
agent-a3328ca97499a43af-62acbc8f | local+remote | 00209/00148 | 594 | 2026-10-23 | d | its 2 unique commits (wsi-stream-server DBF) are in F44 as 0c723528b/9072675c7 (merge 0af0b579f); the rest is in spike
agent-a601e1980624b399e-dcd5c9cf | LOCAL-ONLY (wt .claude/worktrees/agent-a601e1980624b399e-dcd5c9cf) | 00209 | 715 | 2026-10-23 | b | tip 406d97c0a is in spike; worktree has a broken index (7075 files staged as deleted, nothing real); worktree kept because dirty
agent-aaca6016aa207ce9a-bc47ccb5 | local+remote (wt removed) | 00177 | 1 | 2026-10-15 | d | its one file is byte-identical in F44
chore/go-toolchain-1.26.10 | local+remote (wt removed) | - | 1 | 2026-10-21 | d | F44 already carries go.mod toolchain go1.26.10
ci/base-red-proof | local+remote (wt removed) | 00191 | 1 | 2026-11-02 | f | git cherry shows its commit is in F44; handoff journal 11-02: no longer needed, the owner can delete it
feat/bus-v2-n2g | LOCAL-ONLY (wt removed) | 00209 N2 | 286 | 2026-10-22 | d | ancestor of feat/bus-v2-n2 (kept as live)
feat/bus-v2-n2g-backup | LOCAL-ONLY (wt removed) | 00209 N2 | 247 | 2026-10-22 | d | ancestor of feat/bus-v2-n2 (kept as live)
feat/bus-v2-n2g-restore | LOCAL-ONLY (wt removed) | 00209 N2 | 250 | 2026-10-22 | d | ancestor of feat/bus-v2-n2 (kept as live)
feat/bus-v2-n2g-restore-verb | LOCAL-ONLY (wt removed) | 00209 N2 | 247 | 2026-10-22 | d | ancestor of feat/bus-v2-n2 (kept as live)
feat/bus-v2-n3e-goroutines | LOCAL-ONLY (wt removed) | 00209 N3 | 723 | 2026-10-26 | d | ancestor of feat/bus-v2-n3e (kept as live)
feat/bus-v2-n3e-seam | local+remote (wt untracked/worktrees/bus-v2-n3e-seam) | 00209 | 706 | 2026-10-23 | d | in spike; its 1 untracked report is already in spike; rescued anyway; worktree kept (dirty)
feat/ccy-teams-hot-add | local+remote | 00177 P8 | 1 | 2026-10-14 | e | hot add shipped via Plan 00203 M3 (F44); its one report file rescued
feature/ccy-hooks-daemon-plugin | local+remote | - | 3 | 2026-10-03 | d | every commit is in F44 under other hashes (git cherry all minus)
fix/ccy-lifecycle-daemon-launcher | remote | ccy | 1 | 2026-10-06 | d | re-landed in F44 as 3.82.1 (merge 244bd7c9f, same title)
task/run-bash-github-accounts-none | remote | 00082 | 1 | 2026-08-24 | d | its commit is in F44 under another hash (git cherry minus)

Spike-contained group (92 branches, cause d, plan 00209; reason: content is in spike/v2-merge-up (on origin) by patch-id or ancestry):

branch | where | ahead | last commit
--- | --- | --- | ---
agent-a332492e9109c2f20-c3c7a2da | LOCAL-ONLY (wt removed) | 592 | 2026-10-23
agent-a5eb2e3786295d052-c19a1fa9 | LOCAL-ONLY (wt removed) | 534 | 2026-10-23
agent-a5fca4182e2e660bb-c4b651cd | local+remote (wt removed) | 593 | 2026-10-23
feat/bus-v2-n1 | LOCAL-ONLY (wt removed) | 83 | 2026-10-22
feat/bus-v2-n1-hc-design | LOCAL-ONLY (wt removed) | 4 | 2026-10-21
feat/bus-v2-n1-hostchecks | LOCAL-ONLY (wt removed) | 6 | 2026-10-21
feat/bus-v2-n1-matrix | LOCAL-ONLY (wt removed) | 18 | 2026-10-21
feat/bus-v2-n1-paths | LOCAL-ONLY (wt removed) | 12 | 2026-10-22
feat/bus-v2-n1-proto | LOCAL-ONLY (wt removed) | 24 | 2026-10-22
feat/bus-v2-n2a | LOCAL-ONLY (wt removed) | 94 | 2026-10-22
feat/bus-v2-n2b | LOCAL-ONLY (wt removed) | 108 | 2026-10-22
feat/bus-v2-n2c | LOCAL-ONLY (wt removed) | 117 | 2026-10-22
feat/bus-v2-n2d | LOCAL-ONLY (wt removed) | 131 | 2026-10-22
feat/bus-v2-n2d-host | LOCAL-ONLY (wt removed) | 118 | 2026-10-22
feat/bus-v2-n2e | LOCAL-ONLY (wt removed) | 154 | 2026-10-22
feat/bus-v2-n2f | LOCAL-ONLY (wt removed) | 234 | 2026-10-22
feat/bus-v2-n2f-child | LOCAL-ONLY (wt removed) | 210 | 2026-10-22
feat/bus-v2-n2f-ctl | LOCAL-ONLY (wt removed) | 196 | 2026-10-22
feat/bus-v2-n2f-health | LOCAL-ONLY (wt removed) | 206 | 2026-10-22
feat/bus-v2-n2f-hostnet | LOCAL-ONLY (wt removed) | 195 | 2026-10-22
feat/bus-v2-n2f-units | LOCAL-ONLY (wt removed) | 196 | 2026-10-22
feat/bus-v2-n2f-wire | LOCAL-ONLY (wt removed) | 221 | 2026-10-22
feat/bus-v2-n3 | local+remote (wt removed) | 601 | 2026-10-23
feat/bus-v2-n3-guestreg | LOCAL-ONLY (wt removed) | 427 | 2026-10-23
feat/bus-v2-n3-probebound | LOCAL-ONLY | 595 | 2026-10-23
feat/bus-v2-n3-tagvet | LOCAL-ONLY | 538 | 2026-10-23
feat/bus-v2-n3a | LOCAL-ONLY (wt removed) | 253 | 2026-10-22
feat/bus-v2-n3a-bundle | LOCAL-ONLY (wt removed) | 223 | 2026-10-22
feat/bus-v2-n3a-cli | LOCAL-ONLY (wt removed) | 205 | 2026-10-22
feat/bus-v2-n3a-codecs | LOCAL-ONLY (wt removed) | 222 | 2026-10-22
feat/bus-v2-n3a-core | LOCAL-ONLY (wt removed) | 205 | 2026-10-22
feat/bus-v2-n3a-guest-check | LOCAL-ONLY (wt removed) | 240 | 2026-10-22
feat/bus-v2-n3a-guest-place | LOCAL-ONLY (wt removed) | 236 | 2026-10-22
feat/bus-v2-n3a-writer | LOCAL-ONLY (wt removed) | 223 | 2026-10-22
feat/bus-v2-n3b | LOCAL-ONLY (wt removed) | 410 | 2026-10-23
feat/bus-v2-n3b-d2 | LOCAL-ONLY (wt removed) | 337 | 2026-10-22
feat/bus-v2-n3b-fix3 | LOCAL-ONLY (wt removed) | 392 | 2026-10-22
feat/bus-v2-n3b-fix4 | LOCAL-ONLY (wt removed) | 398 | 2026-10-22
feat/bus-v2-n3b-fix5 | LOCAL-ONLY (wt removed) | 403 | 2026-10-23
feat/bus-v2-n3b-fix6 | LOCAL-ONLY (wt removed) | 409 | 2026-10-23
feat/bus-v2-n3b-fixa | LOCAL-ONLY (wt removed) | 379 | 2026-10-22
feat/bus-v2-n3b-fixb | LOCAL-ONLY (wt removed) | 374 | 2026-10-22
feat/bus-v2-n3b-p1 | LOCAL-ONLY (wt removed) | 307 | 2026-10-22
feat/bus-v2-n3b-p2 | LOCAL-ONLY (wt removed) | 306 | 2026-10-22
feat/bus-v2-n3b-p3 | LOCAL-ONLY (wt removed) | 306 | 2026-10-22
feat/bus-v2-n3b-p4 | LOCAL-ONLY (wt removed) | 331 | 2026-10-22
feat/bus-v2-n3b-r1 | LOCAL-ONLY (wt removed) | 345 | 2026-10-22
feat/bus-v2-n3b-r2 | LOCAL-ONLY (wt removed) | 355 | 2026-10-22
feat/bus-v2-n3b-r2b | LOCAL-ONLY (wt removed) | 359 | 2026-10-22
feat/bus-v2-n3b-r2c | LOCAL-ONLY (wt removed) | 364 | 2026-10-22
feat/bus-v2-n3c | LOCAL-ONLY (wt removed) | 417 | 2026-10-23
feat/bus-v2-n3c-docs | LOCAL-ONLY (wt removed) | 410 | 2026-10-23
feat/bus-v2-n3c-fix | LOCAL-ONLY (wt removed) | 372 | 2026-10-22
feat/bus-v2-n3c-fix3 | LOCAL-ONLY (wt removed) | 399 | 2026-10-22
feat/bus-v2-n3c-fixa | LOCAL-ONLY (wt removed) | 381 | 2026-10-22
feat/bus-v2-n3c-fixb | LOCAL-ONLY (wt removed) | 384 | 2026-10-22
feat/bus-v2-n3c-owed | LOCAL-ONLY (wt removed) | 416 | 2026-10-23
feat/bus-v2-n3c-p1 | LOCAL-ONLY (wt removed) | 235 | 2026-10-22
feat/bus-v2-n3c-p2 | LOCAL-ONLY (wt removed) | 242 | 2026-10-22
feat/bus-v2-n3c-p3 | LOCAL-ONLY (wt removed) | 327 | 2026-10-22
feat/bus-v2-n3c-p4 | LOCAL-ONLY (wt removed) | 331 | 2026-10-22
feat/bus-v2-n3c-p5 | LOCAL-ONLY (wt removed) | 327 | 2026-10-22
feat/bus-v2-n3c-p6 | LOCAL-ONLY (wt removed) | 347 | 2026-10-22
feat/bus-v2-n3c-race | LOCAL-ONLY (wt removed) | 406 | 2026-10-23
feat/bus-v2-n3c-w | LOCAL-ONLY (wt removed) | 368 | 2026-10-22
feat/bus-v2-n3d | LOCAL-ONLY (wt removed) | 458 | 2026-10-23
feat/bus-v2-n3d-d1 | LOCAL-ONLY (wt removed) | 368 | 2026-10-22
feat/bus-v2-n3d-d2 | LOCAL-ONLY (wt removed) | 365 | 2026-10-22
feat/bus-v2-n3d-d3 | LOCAL-ONLY (wt removed) | 371 | 2026-10-22
feat/bus-v2-n3d-d4 | LOCAL-ONLY (wt removed) | 371 | 2026-10-22
feat/bus-v2-n3d-d5 | LOCAL-ONLY (wt removed) | 380 | 2026-10-22
feat/bus-v2-n3d-d6a | LOCAL-ONLY (wt removed) | 381 | 2026-10-22
feat/bus-v2-n3d-d6b | LOCAL-ONLY (wt removed) | 386 | 2026-10-22
feat/bus-v2-n3d-fixa | LOCAL-ONLY (wt removed) | 442 | 2026-10-23
feat/bus-v2-n3d-fixb | LOCAL-ONLY (wt removed) | 445 | 2026-10-23
feat/bus-v2-n3d-fixc | LOCAL-ONLY (wt removed) | 456 | 2026-10-23
feat/bus-v2-n3d-mc | LOCAL-ONLY (wt removed) | 435 | 2026-10-22
feat/bus-v2-n3d-w | LOCAL-ONLY (wt removed) | 439 | 2026-10-23
feat/bus-v2-n3e-1 | LOCAL-ONLY (wt removed) | 406 | 2026-10-22
feat/bus-v2-n3e-10 | LOCAL-ONLY (wt removed) | 476 | 2026-10-23
feat/bus-v2-n3e-11 | LOCAL-ONLY (wt removed) | 463 | 2026-10-23
feat/bus-v2-n3e-2 | LOCAL-ONLY (wt removed) | 406 | 2026-10-22
feat/bus-v2-n3e-3 | LOCAL-ONLY (wt removed) | 402 | 2026-10-22
feat/bus-v2-n3e-4 | LOCAL-ONLY (wt removed) | 412 | 2026-10-22
feat/bus-v2-n3e-5 | LOCAL-ONLY (wt removed) | 419 | 2026-10-22
feat/bus-v2-n3e-6 | LOCAL-ONLY (wt removed) | 455 | 2026-10-23
feat/bus-v2-n3e-7 | LOCAL-ONLY (wt removed) | 460 | 2026-10-23
feat/bus-v2-n3e-8 | LOCAL-ONLY (wt removed) | 421 | 2026-10-22
feat/bus-v2-n3e-9 | LOCAL-ONLY (wt removed) | 463 | 2026-10-23
feat/bus-v2-n3e-beginrun | LOCAL-ONLY (wt removed) | 469 | 2026-10-23
feat/bus-v2-n3e-sl | LOCAL-ONLY (wt removed) | 468 | 2026-10-23
feat/bus-v2-remote-seats-note | local+remote (wt removed) | 602 | 2026-10-23

## REVIEW-RECORD (11)

branch | where | plan | ahead | last commit | cause | reason
--- | --- | --- | --- | --- | --- | ---
research/bus-stack | local+remote (wt .claude/worktrees/research-bus-stack) |  | 1 | 2026-10-16 | c | only a review or research report; absent from F44; rescued
research/v2-scope | local+remote (wt .claude/worktrees/agent-a56196b500b8483c3-ebc131e4) |  | 1 | 2026-11-02 | c | only a review or research report; absent from F44; rescued
review/lxc-checkout-typed | local+remote (wt .claude/worktrees/agent-a3296ae066dcb6c32-cec4693f) |  | 1 | 2026-10-16 | c | only a review or research report; absent from F44; rescued
review/lxc-checkout-typed-final | local+remote (wt .claude/worktrees/agent-aba7b1c6f69f1fd91-f19c07f8) |  | 1 | 2026-10-19 | c | only a review or research report; absent from F44; rescued
review/lxc-checkout-typed-rereview | local+remote (wt .claude/worktrees/agent-abe52abf7c68867de-80fecc09) |  | 1 | 2026-10-19 | c | only a review or research report; absent from F44; rescued
review/pingbus-compact-before-wake | local+remote (wt .claude/worktrees/agent-a83a9b8f354f65fdc-f1f27ad3) | 00194 | 2 | 2026-10-19 | c | Fable review (FIX-FIRST) plus spec, plan and journal edits; absent from F44; rescued (6 files)
review/seat-broker-design | local+remote | 00197 | 2 | 2026-10-14 | c | Fable adversarial review of the seat-broker design; rescued (5 files)
review/teams-full-handles | local+remote (wt .claude/worktrees/agent-a374a700cff3fbf1e-a47bc016) |  | 1 | 2026-10-16 | c | only a review or research report; absent from F44; rescued
review/teams-full-handles-rereview | local+remote (wt .claude/worktrees/agent-a30eec90915437f23-6e0380b4) |  | 1 | 2026-10-19 | c | only a review or research report; absent from F44; rescued
review/teams-member-roles | local+remote (wt .claude/worktrees/agent-a39059387997cab08-da6adeb5) |  | 1 | 2026-10-19 | c | only a review or research report; absent from F44; rescued
review/teams-wait-play-lock | local+remote (wt .claude/worktrees/agent-a272344d38fd74df9-3c54b31f) |  | 1 | 2026-10-16 | c | only a review or research report; absent from F44; rescued

## LIVE-WORK (15)

branch | where | plan | ahead | last commit | cause | reason
--- | --- | --- | --- | --- | --- | ---
agent-a5ee80f767118a918-a448e021 | local+remote (wt .claude/worktrees/agent-a5ee80f767118a918-a448e021) | ccy pids-limit | 5 | 2026-10-23 | b | CCY_PIDS_LIMIT / --pids-limit absent from F44; 4 commits end at "review round 2" with no MERGE verdict; agent branch
agent-a81014c63f66ee6f8-c4d3bbe2 | local+remote (wt .claude/worktrees/agent-a81014c63f66ee6f8-c4d3bbe2) | 00185 DBF | 3 | 2026-10-23 | b | last commit titled "WIP ... round 2 not yet run"; rule ansible-umask-pinned absent from F44
agent-a8c69aa67110156ce-c48195fd | local+remote (wt .claude/worktrees/agent-a8c69aa67110156ce-c48195fd) | 00177 T7.6 | 6 | 2026-10-23 | b | last commit "(WIP: round 2 open)"; unknown-team asks-instead-of-failing absent from F44
agent-ad49d4886a70d25db-01b66214 | local+remote (wt .claude/worktrees/agent-ad49d4886a70d25db-01b66214) | ccy go-cache | 5 | 2026-10-23 | b | last commit "WIP ccy go-cache ... review round 4 B1"; internal/teams/gocache absent from F44
agent-ae87230b6e5b05508-967f0e77 | local+remote (wt .claude/worktrees/agent-ae87230b6e5b05508-967f0e77) | 00173 | 2 | 2026-10-13 | c | qa-reviewer fixes and "evening run accepted" only here (00173 journal 26-10-13 absent from F44); small
design/seat-broker | local+remote | 00197 | 4 | 2026-10-14 | a | DESIGN.md exists only here; PLAN status "design revised after the Fable review; the owner decisions next" (F44 shows Not Started)
feat/bus-v2-n2 | local+remote (wt untracked/worktrees/bus-v2-n2) | 00209 N2 | 287 | 2026-10-22 | c | N2 host side (backup/restore, Tuwunel RT1-RT4) finished with review round 2 MERGE; 45 commits not in spike; RestoreChild absent from spike. Local equals remote
feat/bus-v2-n3e | local+remote (wt untracked/worktrees/bus-v2-n3e) | 00209 N3 | 726 | 2026-10-26 | b | local is 11 commits ahead of origin (unpushed); 10 commits not in spike (goroutine review, delivery settle fix); one titled WIP
feat/bus-v2-n3f | LOCAL-ONLY (wt untracked/worktrees/bus-v2-n3f) | 00209 N3 | 543 | 2026-10-23 | c | LOCAL ONLY: SPEC-n3f (side rooms), 3 review rounds, "READY to build"; not in spike or F44
feat/bus-v2-s1-declare | LOCAL-ONLY (wt untracked/worktrees/bus-v2-s1-declare) | 00209 P7 | 19 | 2026-10-21 | f | stage-1 declare/seat reader with F44 merge report (finished); held because 35acceab ancestry needs a rebase first (Journal 26-10-21); LOCAL ONLY
feat/ccy-continue-compact | local+remote (wt .claude/worktrees/usage-bars/.claude/worktrees/agent-afa0bbc9ac4b1af2d-17461ed0) | 00191 T4.12 | 1 | 2026-11-02 | f | sibling of feat/ccy-compact-close (live, excluded); 1 commit 11-02, report absent from F44; fold into compact-close or merge
feat/pingbus-compact-before-wake | local+remote (wt .claude/worktrees/agent-afecb6522c7d1c208-b54b2464) | 00194 T2.0e | 1 | 2026-10-19 | f | coldwake.py, tests and SPEC not in F44; Fable review verdict FIX-FIRST at 6d3d61ab, the fixes never ran
go-port/agent-bus-install | local+remote | 00192 T3.2 | 19 | 2026-10-14 | a | 19 commits; 22 conflicts with F44; PLAN Task 3.2 paused; a qa-reviewer report says it merges separately after the owner test
plan-team-liaison-role | local+remote (wt .claude/worktrees/plan-team-liaison-role) | 00225 | 7 | 2026-10-23 | a | whole Plan 00225 lives only here (not in F44); status "decisions LD1-LD12 carry coordinator defaults the owner may change"; review FIX-FIRST, fixes not re-reviewed
worker/433-unowned-decisions | remote | 00139 | 4 | 2026-09-30 | f | 4 docs-correction commits (owner-decision attribution sweep), 24 files F44 has since changed; Plan 00139 In Progress; low value, needs a rebase

## ABANDONED (21)

branch | where | plan | ahead | last commit | cause | reason
--- | --- | --- | --- | --- | --- | ---
agent-a04b38615e2ea28f3-b7c5ee50 | LOCAL-ONLY | 00209 | 726 | 2026-10-26 | f | duplicate alias ref at the same SHA (4b452fd07) as feat/bus-v2-n3e, which is kept
agent-adde6661246077373-794f2dd9 | LOCAL-ONLY | 00209 | 726 | 2026-10-26 | f | duplicate alias ref at the same SHA (4b452fd07) as feat/bus-v2-n3e, which is kept
feat/agent-bus-v2-design-2 | local+remote (wt .claude/worktrees/agent-ac5d0762eedf12856-f7b3e167) | 00209 | 2 | 2026-10-21 | e | early v2 design/build brief; F44 holds the rewritten versions; holds 35acceab (history to be rewritten, Journal 26-10-21)
feat/bus-v2-m0-plan-OLD-ds2 | LOCAL-ONLY | 00209 | 7 | 2026-10-21 | e | the OLD M0 plan, replaced by the N-series plan; LOCAL ONLY
feat/bus-v2-m1-matrix | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-matrix) | 00209 M1 | 11 | 2026-10-21 | e | M-series replaced by N-series; "decode v1 formats, useful to stage 1 at most" (n0-integration report); LOCAL ONLY
feat/bus-v2-m1-proto | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-proto) | 00209 M1 | 7 | 2026-10-21 | e | M-series replaced by N-series; "decode v1 formats, useful to stage 1 at most" (n0-integration report); LOCAL ONLY
feat/bus-v2-m1-proto-envelope | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-proto-envelope) | 00209 M1 | 8 | 2026-10-21 | b | M1 slice superseded by N-series; dirty tree = sub-agent stopped mid-slice (uncommitted Go + fixtures, rescued); LOCAL ONLY
feat/bus-v2-m1-proto-limits | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-proto-limits) | 00209 M1 | 8 | 2026-10-21 | b | M1 slice superseded by N-series; dirty tree = sub-agent stopped mid-slice (uncommitted Go + fixtures, rescued); LOCAL ONLY
feat/bus-v2-m1-proto-ping | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-proto-ping) | 00209 M1 | 7 | 2026-10-21 | b | M1 slice superseded by N-series; dirty tree = sub-agent stopped mid-slice (uncommitted Go + fixtures, rescued); LOCAL ONLY
feat/bus-v2-m1-proto-rooms | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-proto-rooms) | 00209 M1 | 7 | 2026-10-21 | b | M1 slice superseded by N-series; dirty tree = sub-agent stopped mid-slice (uncommitted Go + fixtures, rescued); LOCAL ONLY
feat/bus-v2-m1-state | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-state) | 00209 M1 | 13 | 2026-10-21 | e | M-series replaced by N-series; "decode v1 formats, useful to stage 1 at most" (n0-integration report); LOCAL ONLY
feat/bus-v2-m1-state-locks | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m1-state-locks) | 00209 M1 | 10 | 2026-10-21 | e | M-series replaced by N-series; "decode v1 formats, useful to stage 1 at most" (n0-integration report); LOCAL ONLY
feat/bus-v2-m2-hooks | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m2-hooks) | 00209 M2 | 11 | 2026-10-21 | e | PLAN 00209: "feat/bus-v2-m2-hooks is superseded"; last commit says WIP at the context budget; LOCAL ONLY
feat/bus-v2-m2-hooks-golden | LOCAL-ONLY (wt untracked/worktrees/bus-v2-m2-hooks-golden) | 00209 M2 | 8 | 2026-10-21 | e | PLAN 00209: "feat/bus-v2-m2-hooks is superseded"; last commit says WIP at the context budget; LOCAL ONLY
feature/ccy-token-expiry-colours | remote | ccy | 1 | 2026-05-07 | e | May 2026; token expiry display exists in F44 (token-management)
go-qa-ci/adopt | local+remote (wt .claude/worktrees/agent-aca5e949a69f06822-4582ef55) | 00191 T3.3 | 4 | 2026-10-15 | e | Task 3.3 is ticked done in F44; qa-all is Go now; owner said not to lock into go-qa-ci
skip-docker-env | remote | 00091 | 1 | 2026-09-01 | e | PR #40 closed as superseded (Plan 00091 journal); play-docker.yml and RUN_BASH_SKIP_DOCKER gone from F44
wf-f0f65b6e-87f-1-be409213 | local+remote | 00161 | 1 | 2026-10-06 | b | subject: "the triage probe test the stopped wave-1 agent left"; Plan 00161 superseded by v2
wf-f0f65b6e-87f-2-30211d13 | local+remote | 00161 | 1 | 2026-10-06 | e | Plan 00161 U01/U02; DESIGN.md says U02 "not merged"; v1 bus plan superseded by Plan 00209
wf-f0f65b6e-87f-3-061c2adc | local+remote | 00161 | 1 | 2026-10-06 | e | Plan 00161 U01/U02; DESIGN.md says U02 "not merged"; v1 bus plan superseded by Plan 00209
worktree-ccy-reboot-restore | local+remote | 00123 | 13 | 2026-09-16 | e | PR #47 closed (Plan 00139 journal); Plan 00123 folder exists only here, rescued (15 files)

## Remote branches the owner can delete (31)

SUPERSEDED, REVIEW-RECORD (rescued) and ABANDONED only. Never the LIVE-WORK or excluded ones.

```
git push origin --delete agent-a3328ca97499a43af-62acbc8f agent-a5fca4182e2e660bb-c4b651cd agent-aaca6016aa207ce9a-bc47ccb5 chore/go-toolchain-1.26.10 ci/base-red-proof feat/bus-v2-n3 feat/bus-v2-n3e-seam feat/bus-v2-remote-seats-note feat/ccy-teams-hot-add feature/ccy-hooks-daemon-plugin fix/ccy-lifecycle-daemon-launcher task/run-bash-github-accounts-none research/bus-stack research/v2-scope review/lxc-checkout-typed review/lxc-checkout-typed-final review/lxc-checkout-typed-rereview review/pingbus-compact-before-wake review/seat-broker-design review/teams-full-handles review/teams-full-handles-rereview review/teams-member-roles review/teams-wait-play-lock feat/agent-bus-v2-design-2 feature/ccy-token-expiry-colours go-qa-ci/adopt skip-docker-env wf-f0f65b6e-87f-1-be409213 wf-f0f65b6e-87f-2-30211d13 wf-f0f65b6e-87f-3-061c2adc worktree-ccy-reboot-restore
```

## Local branches for the owner

135 local branches with a deletable verdict remain; git branch -d refuses them (unmerged to HEAD), so deleting needs -D, the owner call.
LOCAL-ONLY among them (no remote copy; -D loses the commits for good, push first if wanted) (108): agent-a332492e9109c2f20-c3c7a2da agent-a5eb2e3786295d052-c19a1fa9 agent-a601e1980624b399e-dcd5c9cf feat/bus-v2-n1 feat/bus-v2-n1-hc-design feat/bus-v2-n1-hostchecks feat/bus-v2-n1-matrix feat/bus-v2-n1-paths feat/bus-v2-n1-proto feat/bus-v2-n2a feat/bus-v2-n2b feat/bus-v2-n2c feat/bus-v2-n2d feat/bus-v2-n2d-host feat/bus-v2-n2e feat/bus-v2-n2f feat/bus-v2-n2f-child feat/bus-v2-n2f-ctl feat/bus-v2-n2f-health feat/bus-v2-n2f-hostnet feat/bus-v2-n2f-units feat/bus-v2-n2f-wire feat/bus-v2-n2g feat/bus-v2-n2g-backup feat/bus-v2-n2g-restore feat/bus-v2-n2g-restore-verb feat/bus-v2-n3-guestreg feat/bus-v2-n3-probebound feat/bus-v2-n3-tagvet feat/bus-v2-n3a feat/bus-v2-n3a-bundle feat/bus-v2-n3a-cli feat/bus-v2-n3a-codecs feat/bus-v2-n3a-core feat/bus-v2-n3a-guest-check feat/bus-v2-n3a-guest-place feat/bus-v2-n3a-writer feat/bus-v2-n3b feat/bus-v2-n3b-d2 feat/bus-v2-n3b-fix3 feat/bus-v2-n3b-fix4 feat/bus-v2-n3b-fix5 feat/bus-v2-n3b-fix6 feat/bus-v2-n3b-fixa feat/bus-v2-n3b-fixb feat/bus-v2-n3b-p1 feat/bus-v2-n3b-p2 feat/bus-v2-n3b-p3 feat/bus-v2-n3b-p4 feat/bus-v2-n3b-r1 feat/bus-v2-n3b-r2 feat/bus-v2-n3b-r2b feat/bus-v2-n3b-r2c feat/bus-v2-n3c feat/bus-v2-n3c-docs feat/bus-v2-n3c-fix feat/bus-v2-n3c-fix3 feat/bus-v2-n3c-fixa feat/bus-v2-n3c-fixb feat/bus-v2-n3c-owed feat/bus-v2-n3c-p1 feat/bus-v2-n3c-p2 feat/bus-v2-n3c-p3 feat/bus-v2-n3c-p4 feat/bus-v2-n3c-p5 feat/bus-v2-n3c-p6 feat/bus-v2-n3c-race feat/bus-v2-n3c-w feat/bus-v2-n3d feat/bus-v2-n3d-d1 feat/bus-v2-n3d-d2 feat/bus-v2-n3d-d3 feat/bus-v2-n3d-d4 feat/bus-v2-n3d-d5 feat/bus-v2-n3d-d6a feat/bus-v2-n3d-d6b feat/bus-v2-n3d-fixa feat/bus-v2-n3d-fixb feat/bus-v2-n3d-fixc feat/bus-v2-n3d-mc feat/bus-v2-n3d-w feat/bus-v2-n3e-1 feat/bus-v2-n3e-10 feat/bus-v2-n3e-11 feat/bus-v2-n3e-2 feat/bus-v2-n3e-3 feat/bus-v2-n3e-4 feat/bus-v2-n3e-5 feat/bus-v2-n3e-6 feat/bus-v2-n3e-7 feat/bus-v2-n3e-8 feat/bus-v2-n3e-9 feat/bus-v2-n3e-beginrun feat/bus-v2-n3e-goroutines feat/bus-v2-n3e-sl agent-a04b38615e2ea28f3-b7c5ee50 agent-adde6661246077373-794f2dd9 feat/bus-v2-m0-plan-OLD-ds2 feat/bus-v2-m1-matrix feat/bus-v2-m1-proto feat/bus-v2-m1-proto-envelope feat/bus-v2-m1-proto-limits feat/bus-v2-m1-proto-ping feat/bus-v2-m1-proto-rooms feat/bus-v2-m1-state feat/bus-v2-m1-state-locks feat/bus-v2-m2-hooks feat/bus-v2-m2-hooks-golden

## Why they were left: counts by cause

cause | count
--- | ---
(a) BLOCKED-ON-HUMAN | 3
(b) AGENT-STOPPED-MIDWAY | 11
(c) DONE-NOT-MERGED | 14
(d) MERGED-NOT-DELETED (merged into spike or F44 under other hashes) | 104
(e) SUPERSEDED-BY-REDESIGN | 15
(f) OTHER | 7

Dominant case: (d). The bus-v2 branches are slices merged into spike/v2-merge-up and never deleted.

## Examples with evidence per cause

(a) BLOCKED-ON-HUMAN
- design/seat-broker: PLAN status "design revised after the Fable review; the owner decisions next"
- plan-team-liaison-role: PLAN status "decisions LD1-LD12 carry coordinator defaults the owner may change"
- go-port/agent-bus-install: Plan 00192 qa-reviewer report, merges separately after the owner test; PLAN Task 3.2 paused
- The owner sheet owner-decisions-2026-10-30.md names no branch in the classified set (only F44-arm and spike/v2-merge-up, both excluded)

(b) AGENT-STOPPED-MIDWAY
- agent-a81014c63f66ee6f8-c4d3bbe2: last commit "WIP Plan 00185 DBF ... (round 2 not yet run)"
- agent-a8c69aa67110156ce-c48195fd: last commit "... (WIP: round 2 open)"
- agent-ad49d4886a70d25db-01b66214: last commit "WIP ccy go-cache ... review round 4 B1"
- feat/bus-v2-m1-proto-ping/-envelope/-limits/-rooms: dirty trees with uncommitted Go and fixtures
- agent-a601e1980624b399e-dcd5c9cf: 7075 files staged as deleted (broken index); wf-...-1: "the triage probe test the stopped wave-1 agent left"
- No saved reply under untracked/agent-reports names any of these branches or their hashes, so the cause comes from commit subjects and tree state

(c) DONE-NOT-MERGED
- review/lxc-checkout-typed*, review/teams-*: each is a finished Fable review report never merged
- feat/bus-v2-n2: review round 2 MERGE; tip "merge n2g"; never carried into spike
- feat/bus-v2-n3f: "spec narrow check (READY to build)"
- agent-ae87230b6e5b05508-967f0e77: "evening run accepted; qa-reviewer fixes" for Plan 00173

(d) MERGED-NOT-DELETED (merged into spike or F44 under other hashes)
- feat/bus-v2-n3a ... n3e-11 and the rest of the slices: git cherry vs spike shows no plus lines
- agent-a3328ca97499a43af-62acbc8f: 2 unique commits are in F44 (0c723528b, 9072675c7)
- feature/ccy-hooks-daemon-plugin, task/run-bash-github-accounts-none: git cherry vs F44 all minus
- fix/ccy-lifecycle-daemon-launcher: re-landed as ccy 3.82.1 (244bd7c9f)

(e) SUPERSEDED-BY-REDESIGN
- feat/bus-v2-m2-hooks: PLAN 00209 line 129 "is superseded"
- skip-docker-env: Plan 00091 journal "PR #40 closed as superseded"
- go-qa-ci/adopt: Plan 00191 Task 3.3 ticked in F44
- worktree-ccy-reboot-restore: Plan 00139 journal "belonged to closed PR #47"
- wf-f0f65b6e-87f-3-061c2adc: Plan 00161 DESIGN.md "not merged"

(f) OTHER
- agent-a04b38615e2ea28f3-b7c5ee50 and agent-adde...: duplicate refs at the n3e tip
- feat/bus-v2-s1-declare: Journal 00209 26-10-21, 35acceab ancestry needs a rebase before push
- feat/pingbus-compact-before-wake: Fable verdict FIX-FIRST, fixes never ran
- ci/base-red-proof: handoff journal 11-02 "the owner can delete it"

## Age since last commit (unique items, excluded omitted)

bucket | count
--- | ---
0-2 | 3
3-7 | 0
8-14 | 124
>14 | 27

ABANDONED: {'8-14': 14, '>14': 7}
SUPERSEDED: {'8-14': 101, '>14': 5, '0-2': 1}
LIVE-WORK: {'8-14': 9, '>14': 5, '0-2': 1}
REVIEW-RECORD: {'>14': 10, '0-2': 1}

## Live-work ranked by value

1. feat/bus-v2-n3e (local 11 commits ahead of origin, unpushed) with aliases agent-a04.../agent-adde... - Plan 00209, push first
2. feat/bus-v2-n2 - finished N2 host side, not in spike (check whether spike intends to carry it)
3. feat/bus-v2-n3f - LOCAL ONLY, reviewed spec ready to build
4. agent-a5ee80f7 pids-limit, agent-a8c69aa6 unknown-team (Plan 00177 T7.6), agent-ad49d488 go-cache, agent-a81014c6 DBF umask - real ccy/ansible features absent from F44, stopped at review rounds 2-4
5. plan-team-liaison-role (Plan 00225), design/seat-broker (Plan 00197) - plan material found nowhere else; need owner decisions
6. go-port/agent-bus-install (Plan 00192 T3.2, 22 conflicts), feat/pingbus-compact-before-wake (FIX-FIRST), feat/bus-v2-s1-declare (LOCAL ONLY)
7. agent-ae87230b (Plan 00173 journal), feat/ccy-continue-compact, worker/433-unowned-decisions (low)

## Actions taken and rescue

- 98 clean worktrees removed (no --force) for SUPERSEDED branches; 2 dirty ones kept (agent-a601... broken index, feat/bus-v2-n3e-seam). git worktree prune run.
- 2 local branches deleted with git branch -d (feature/ccy-hooks-daemon-plugin, ci/base-red-proof); the other superseded locals are unmerged to HEAD so -d refused.
- Rescued under untracked/wip-rescue/: patches and untracked files of 5 dirty worktrees (bus-v2-m1-proto-envelope/limits/ping/rooms, bus-v2-n3e-seam); the review, research and design files of 12 branches; Plan 00123 (15 files) from worktree-ccy-reboot-restore; the Plan 00177 hot-add report.
- Dirty worktrees left in place: m1-proto-envelope/limits/ping/rooms (uncommitted slice work of an abandoned design, rescued), agent-a601 (debris), n3e-seam (1 untracked report, already in spike).
- Excluded and untouched: fix/pingbus-human-routing (3 files modified within 3 hours), feat/ccy-compact-close, spike/v2-merge-up, F44-arm, arm/ci-arm64-install, arm/* (Plan 00243), the nested agent-a5e07c91 worktree; F43 local (version branch).
- origin/pr/32 is a ghost ref (listed by git branch -r, unresolvable); owner can drop it with git remote prune or git update-ref -d refs/remotes/origin/pr/32.
