# v2 landing triage (2026-11-10, read-only)

Read-only triage of every open pull request, the v2 branches with no pull request, what is left
before cutover, and the owner decisions open on #193. No commit, merge, checkout or push was made.
Measured against `origin/F44` at `43f62cf94`, after `git fetch`. The local F44 is 7 commits
ahead of origin and was not part of this.

Method: `gh pr list`, `git merge-tree --write-tree --name-only origin/F44 origin/<head>`,
`git rev-list --count`, `git cherry`, `git merge-base --is-ancestor`, and the comments on the PRs
and on #193.

## 1. Open pull requests

There are 13 open PRs, and every one is v2 work. CI (`qa-all`, gitleaks, contains-the-default-branch)
is **green on every head**. That green is against each PR's own last F44 merge, not against
today's F44.

The "Behind" column counts commits on origin/F44 that the head lacks. "caa8a" says whether the head
holds `caa8a861c`, the F44 push of 2026-11-09 that the coordinator asked to have merged down.

| PR   | Head ← base                        | Draft | Behind | caa8a  | Merges clean into F44?                                                                                                                  | Delivers                                                                                                                                                           | Last review                                                                                                              | State                                                                                                                                                                                                                                                                      |
| ---- | ---------------------------------- | ----- | ------ | ------ | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| #197 | `docs/install-path-plan` ← F44     | no    | 103    | yes    | **clean**                                                                                                                               | 00246 install-path plan (`INSTALL-PATH.md`, Task 8.10, Install path section)                                                                                       | coordinator **READY as a plan** (11-09); the three "should" items and the remote-inventory point folded in (`545865916`) | **Ready**                                                                                                                                                                                                                                                                  |
| #205 | `feat/install-stage-a-play` ← #197 | no    | 103    | yes    | **clean**                                                                                                                               | 00246 Task 8.8, install path stage A: `play-teamchat.yml`, `unitfiles` (Go unit texts and goldens), the tuwunel pin, the cross build, the TLS state root, tmpfiles | **none**                                                                                                                 | Needs a review. Probable fault: the play has `changed_when: true` on "Unpack the Tuwunel Release Asset", and F44's `command-always-changed` became blocking in `85b5b8d2c` after this branch's base. Run `qa-ansible` on the merged tree. The play has never run anywhere. |
| #209 | `feat/vmtest-links` ← #205         | no    | 178    | **no** | **conflicts**: `meta-deploy.list`, `host/tls_test.go`, `vmtest/app.go`, `vmtest/kit_test.go`                                            | 00246 Task 8.8: vmtest E67–E73 (links, `member-up --link`, the RAM sum, `member-ssh --as`) and `x30-triage.bash`                                                   | **none**                                                                                                                 | Needs work. Its base is 77 commits ahead of it, so merge #205 into it first; against #205 alone it conflicts only in `meta-deploy.list`. That file still carries old 00201 and 00249 entries from the stale base; keep only `x30-triage.bash`.                             |
| #200 | `fix/coordinator-notice-192` ← F44 | no    | 103    | yes    | **conflicts**: `GoProjectRules.md`, `rules/table.go`, `rules_test.go`                                                                   | 00209: #192, the coordinator claim (`proto.ClaimCoordinator`) and the change re-told, plus rule `project_coordinator_absence`                                      | coordinator FIX-FIRST (11-09 08:59); the fixes are pushed at `48d8c7901` and **not re-reviewed**                         | Needs a re-review and an F44 merge. The rule count is now 35 on F44. Owner or coordinator: accept or refuse the rule's three named allowances.                                                                                                                             |
| #196 | `docs/plan-00209-slim` ← F44       | no    | 265    | no     | **clean** (the merged PLAN.md is 32,822 bytes)                                                                                          | 00209 PLAN.md slim; Tasks 0.5 and 0.6 moved to `DONE-n0-n1.md`                                                                                                     | the coordinator approved the scope; `DONE-n0-n1.md` linked as ruled                                                      | **Ready**: merge it before the n3f chain touches PLAN.md again                                                                                                                                                                                                             |
| #173 | `feat/crossnet-s12-int` ← F44      | yes   | 103    | yes    | **conflicts**: `rules/table.go`, `rules_test.go` (adds `project_problem_error_text`)                                                    | S12 / X31: unattended leaf renewal                                                                                                                                 | **none**                                                                                                                 | Next for review (the coordinator's order starts with #173). Merge F44 in again (union of 35 and 1 rules, so 36). Un-draft.                                                                                                                                                 |
| #176 | `feat/crossnet-s13-int` ← #173     | yes   | 103    | yes    | **conflicts**: the same two rule files                                                                                                  | S13 / X32: a probe through the terminator                                                                                                                          | **none**                                                                                                                 | After #173                                                                                                                                                                                                                                                                 |
| #177 | `feat/crossnet-s14-int` ← #176     | yes   | 103    | yes    | **conflicts**: the same two rule files                                                                                                  | S14 / X33: rotating the CA                                                                                                                                         | **none**                                                                                                                 | After #176                                                                                                                                                                                                                                                                 |
| #172 | `feat/crossnet-s15-int` ← F44      | yes   | 103    | yes    | **clean**                                                                                                                               | S15 / X34: Element trusts the CA through the play (`play-agent-bus-element.yml`, `tasks/element-team-ca.yml`, `helpers/element_ca/nss.py`)                         | **none**                                                                                                                 | Independent of the stack. Its host run waits on a v2 https team. The new Python helper is against the Go-tooling rule's spirit ("Python that Ansible loads stays Python", so it is allowed). Review that point.                                                            |
| #166 | `feat/bus-v2-n3f-b-int` ← F44      | no    | 277    | no     | **conflicts**: `GoProjectRules.md`, 00209 `PLAN.md`, `member/cli/run.go`, `member/cli/verbs_test.go`, `rules/table.go`, `rules_test.go` | 00209 n3f-b: the reports room (adds `project_shared_due`)                                                                                                          | **none**. #158's hold (OW5 (b)) is satisfied: (b) is merged down the chain (11-06) and is on F44 through #158            | Needs F44 merged down the whole chain, with code conflicts against #128's member verbs, then a review                                                                                                                                                                      |
| #168 | `n3f-c1b-int` ← #166               | yes   | 277    | no     | the same 6 conflicts                                                                                                                    | n3f-c1: questions-room sync QS1–6, QS9, QS10                                                                                                                       | none                                                                                                                     | Behind #166                                                                                                                                                                                                                                                                |
| #171 | `n3f-c1c-int` ← #168               | yes   | 277    | no     | the same                                                                                                                                | QS7, QS8, QS6 tests                                                                                                                                                | none                                                                                                                     | Behind #168                                                                                                                                                                                                                                                                |
| #175 | `n3f-c1d-int` ← #171               | yes   | 277    | no     | the same                                                                                                                                | n3f-c1d: per-room floors, Refetch, status                                                                                                                          | none                                                                                                                     | Behind #171                                                                                                                                                                                                                                                                |
| #178 | `n3f-c2a-int` ← #175               | yes   | 277    | no     | the same                                                                                                                                | n3f-c2a: QX1, the index build and catch-up                                                                                                                         | the coordinator's hold (OW5) is now met                                                                                  | Behind #175. Its kill-and-resume and QS7 e2e tests are not written.                                                                                                                                                                                                        |

**Cross-stack conflicts** found by merge-tree:

- #200 × the n3f chain: 8 files, including `member/team/team.go` and an add/add in the rule
  fixture `testdata/.../teamchat/proto/proto.go`.
- #200 × #177: `rules/table.go`.
- #209 × #177: `DESIGN-v2-C-host.md`, 00246 PLAN.md, `meta-deploy.list` and `host/tls_test.go`.

Every PR that adds a rule collides in `table.go` and the count test, one merge at a time.

**Reviews:** no review report for any of these PRs is in either plan's `subagent-reports/`.
The verdicts that exist are the coordinator's PR comments, quoted above. A `qa-reviewer` pass is
owed on all of them.

## 2. v2 branches with no open pull request

Every `rescue/*` and `spike/*` branch is gone from origin, as is `dbf/cfd-2`. What remains:

| Branch                                                          | Finding                                                                                                                                                                                                                                                                                                                                            |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `feat/install-stage-a-units`                                    | **Contained** in #205 and #209 (an ancestor of both). Deletable once #205 merges.                                                                                                                                                                                                                                                                  |
| `wip-rescue/bus-v2-n3b-rv2`, `-n3b-rv3`, `-n3c-rv2`, `-n3c-rv3` | **Patch-equivalent** to F44 (`git cherry` shows -)                                                                                                                                                                                                                                                                                                 |
| `wip-rescue/bus-v2-n3b-rva`, `-n3d-rva`, `-n3d-rvb`             | Each has one "WIP rescue" commit that adds one review report. The **blob is identical to F44's** copy (`261022-n3b-review-A.md`, `n3d-review-A.md`, `n3d-review-B.md`), so they hold no unique content. Deletable.                                                                                                                                 |
| `feat/bus-v2-m1-matrix`                                         | 8 of its 10 commits are patch-equivalent (the matrix client is on F44 as `teamchat/matrix`). The 2 that are not are `35acceab` and `ba521348` (the BUILD-BRIEF commits). `BUILD-BRIEF.md` is on F44. Task 7.2 says branches holding `35acceab` are not merged, and all its siblings are already `archive/*`. **Superseded**: archive or delete it. |
| `feat/release-version`                                          | **Unique work, and not v2.** Plan 00153 Tasks 5.1–5.4 (the release workflow, `release`, `releasever`, `cmd/release-plan`, 36 files, about 3.5k lines). It is in no PR and not on F44. It needs its own PR or a decision, outside this plan.                                                                                                        |

A remote delete of an unmerged branch is the owner's (R-GIT-PUSH-DELETE-REMOTE).

## 3. Path to cutover, then cross-network

**What exists on F44:** the whole `teamchat` and `teamchat-admin` code, as inert code under fakes
(N1–N3e, n3f-a, #128's cross-network S1–S11 and B1–B3). The member kit with its own plugin and
skill, `teamchat:teamchat` (`member/kit`), is built and installed nowhere.

**What is missing before every session is on v2 and v1 can be removed:**

01. **The bus server install, stage A** (#197, then #205). It has never run. Its first run is the
    VM lab or the first real bus-server host's own IaC.
    - Task 8.10, `declare check` and the health IP drop-in, is not built.
    - Task 2.5's acceptance (H-C1–H-C6, H-C9–H-C15, the footprint, `teamchat-coexist` at the N2
      stage) has not run.
02. **The side rooms**, Plan 00211's questions and reports: the n3f chain #166–#178, then
    n3f-c2b, n3f-c2c, n3f-d (the questions verbs `ask`, `thread`, `questions`) and n3f-e (the
    skill text). None of these is built.
03. **Markdown rendering**, Plan 00241 Phase 3, Tasks 3.1–3.3. Nothing is built: `formatted_body`
    appears nowhere in `teamchat`. That covers v1's converter ported, `say` as HTML, and tables as
    HTML tables.
04. **Task 3.2/3.4**: the rest of n3e (the OF1 row, PT1's unreached points, seed 57 in #156), B
    2.15's acceptance, the member-cost bars, and an LXC member in the VM lab. Task 1.7's two minors
    are also open.
05. **Stage B**, Task 4.1: the v2 kit in the ccy image, with a CCY version bump. Not started.
06. **Stage C**, Task 4.2: the launcher (session mounts, `seat disconnected`, per-team system
    resolution, RS2/RS3). Not started.
07. **Task 4.3**: ccy-teams on v2 (the board, the flows, create and rename). Its callers are still
    needed for `declare/hostvars` and `sweep`.
08. **Task 4.4**: H-C7, H-C8 and the pilot. Gate B binds the ccy pool seat's host run.
09. **Task 5.1**: host `cc` members, under gate B.
10. **Phase 6, migration**: not specified (6.1). There is no `migrate` package on F44, no VM-lab
    `teamchat-migrate` or rollback scenario, and no pilot.
11. **pingbus compatibility.** v2 deliberately has none (direction 2). Everything that calls
    `pingbus` has to be moved to `teamchat` or retired at cutover, and none of it has been started:
    - the ccy image (`claude-yolo`, `entrypoint.sh`, `lib/agent-bus-*.bash`, the Dockerfile);
    - `play-agent-bus*.yml` and `tasks/claude-yolo-image.yml`;
    - the v1 plugin and skill under `files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/`;
    - `.claude/ccy`, the CLAUDE docs, `docs/agent-bus*.md`, and about 124 v1 Go files under
      `src-go/internal`.
12. **Task 6.3**: retire v1 (its own plan). That removes `helpers/pingbus`, `bus/pycompat`, the
    v1 Go, and the second seam owners.

**Cross-network, after or alongside the above:**

- #173, #176, #177 (S12–S14) and #172 (S15) need merging.
- #209's lab links need a host proof. `x30-triage.bash` is queued only on #209's branch, so it
  reaches `meta-deploy.list` only when #209 merges.
- Still to build: the lab play's subnet input (D1), the guest fixtures, and `x30-lab.bash`.
  `x30-lab.bash` covers the three networks, the relay-form issue with password sudo and outbound
  ssh blocked, the host move with a DNS url, and worker-to-worker traffic with the coordinator
  absent.
- Firewalld zones with a reject default. The LXC bridge leaves `trusted`.
- X30 itself, on the lab and then on the real bus-server host.

## 4. Landing order

The coordinator merges each step locally with `--no-ff`, runs a full `qa-all`, and pushes F44.
Every head below has to be merged forward after each step, because each step moves F44.

1. **#196** (docs, clean). **[coordinator]**
2. **#197** (docs, clean, READY). **[coordinator]**
3. **#205**: review it, merge F44 in, fix any `command-always-changed` finding, then merge.
   **[coordinator]** The first real run needs **[owner: VM-lab run]** or the bus host's own IaC.
4. **#173 → #176 → #177**, bottom up. Each needs F44 merged in (rule union), un-drafting, and
   review. **[coordinator]**
5. **#172**: review it, then merge. **[coordinator]** Its meta-deploy entry waits for a v2 https
   team. **[owner: host run]**
6. **#200**: re-review the FIX-FIRST items, merge F44 in, and settle the allowances
   **[owner/coordinator decision]**.
7. **#209**: merge #205 or F44 in, fix `meta-deploy.list`, review, merge. Then
   **[owner: host run]** of `x30-triage.bash` through meta-deploy.
8. **The n3f chain**, #166 → #168 → #171 → #175 → #178. Merge F44 down the whole chain (code
   conflicts with #128 and #200 in `member/cli` and `member/team`), then review each. Only #166
   is not a draft.

Owner-only steps:

- **Approval:** none of these needs the owner to approve on GitHub, because the coordinator's
  local merge and push records each PR as merged.
- **VM-lab and host runs:** stage A's first run, `x30-triage`, the X30 lab scenario, and Task
  2.5's H-C checks.
- **Gate B:** the first ccy pool seat (Task 4.4).
- **Branch deletions:** the remote deletes in section 2.

## 5. Owner decisions open on #193

The "seven" is lts-bob's comment of 11-09 14:08. Its heading says seven, but it lists **eight**
(D1–D8, from `subagent-reports/261109-vmlab-multinet-design-opus.md`). The builder said on 11-09
15:14 that it was proceeding on the recommendations, "change course on a veto". The recommendation
comes first in each line.

1. **D1, the lab subnets:** a host_vars mapping with no default (`vm_test_lab_networks`), or
   fixed ranges.
2. **D2, the network mechanism:** UDP point-to-point links between rootless guests (no host
   privilege change), or a libvirt network.
3. **D3, an all-guests RAM check:** add it (built in #209), or keep the per-guest 75% check.
4. **D4, disk:** keep the guard plus a scenario preflight of 22 GiB + 2 GiB per extra guest, or
   change the guard.
5. **D5, the play ledger's false record:** fix it as a defect class (record only local-host
   plays) with the callback disabled until then, or accept the record.
6. **D6, host-move order:** play without the team, restore, play with the team (and record the
   documented order separately), or the documented order, which mints a new CA.
7. **D7, password sudo in the lab:** a separate `issuer` user, the cloud user keeping NOPASSWD as
   the control channel, or something else.
8. **D8, the `/32` check and the permissive-zone refusal:** defer them and say in the verdict that
   they were not run, or build them first.

These are also open on the same issue, and the owner's:

- the S4 hostname rule: the next wider rule, or the narrowing named (Defence Before Fix);
- #164: `awaiting` refuses a transcript path heavy in `<>&`, or `sizeAwaiting` is raised;
- Task 8.7: CI allows the bind mount, or accepts the skip, and the old not-root skips;
- n2f-D12: accept the admin token's loopback race, or move to a unix socket;
- INT3.

## Faults met while triaging (dogfooding; to be logged by the coordinator)

- `hooks-daemon find-plan` prints a venv "interpreter drift" warning (the venv records Python
  3.14 and the interpreter is now 3.13) on every call. It still answers.
- R-PLAN-NUMBER-DISCOVERY blocked a content search: a `grep -rlE` over two plans'
  `subagent-reports/`, batched with a `find-plan` call; a later batch with a `grep` over
  `CLAUDE/Plan/*/PLAN.md` was blocked too. Neither was a plan-number scan. False positive.
- F44's "Branch hygiene" workflow fails on the last two F44 pushes (`fb8f9bec9`, `43f62cf94`).
  Not investigated.
- My own slip: one `git diff --stat ... | tail -n 1`, against the brief's no-tail rule. It was
  read-only and lost nothing.
