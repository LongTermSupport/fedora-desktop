# Plan 00246: agent bus v2 landing and cross network

**Status**: In Progress (split out of Plan 00209 Phase 8 on the repository owner's decision of 2026-11-03; Plan 00209 stays the plan of record for v2's design and build, this plan is the landing of `teamchat` on F44 and the cross-network membership that follows it)
**Created**: 2026-11-03
**Owner**: joseph
**Priority**: High

## Overview

Plan 00209 designed and built v2 of the agent bus, `teamchat`, a separate system in Go beside
v1. The owner (2026-11-02): "yes we should push v2 and get it done", the minimum v2 with
cross-network membership (Plan 00209 direction 7). `spike/v2-merge-up` is F44 with the finished
v2 branches (`feat/bus-v2-n2`, `feat/bus-v2-n3e`, `feat/bus-v2-n3f`) merged in; its pull request
(124) carries Tasks 8.1 to 8.7 and 8.9. Task 8.8 then makes a member on another machine able to
join.

The task numbers are Plan 00209's Phase 8 numbers, kept so that every reference to "Task 8.x"
(the pull request, the journal, GoProjectRules.md) still resolves. The design, the specs and the
decisions stay in Plan 00209's folder; this plan links them and does not copy them: the
[plan of record](../00209-agent-bus-v2-in-go/PLAN.md) (direction 7 is Task 8.8's source), the
[design](../00209-agent-bus-v2-in-go/DESIGN-v2.md) and the
[Matrix spec](../00209-agent-bus-v2-in-go/SPEC-matrix.md) (M50 and M51).

## Goals

- v2 is on F44 with CI green: the project rules, the suppression record, gofmt, the helper
  tests, gitleaks and the Go tests (Tasks 8.1 to 8.7).
- A team declares the networks its members may be on, and a member on another machine joins
  over TLS with the team's CA pinned (Task 8.8).

## Non-Goals

- v2's design, its other milestones and v1's retirement: Plan 00209.
- An overlay network: the bus supports several routable ranges and does not route them (Plan
  00209 direction 7). Plan 00220's overlay is not part of the bus.
- Any v1 feature work: v1 is feature-frozen.

## Tasks

### Phase 8: landing v2 on F44

The owner (2026-11-02): "yes we should push v2 and get it done", the minimum v2 with
cross-network membership. `spike/v2-merge-up` began as `feat/bus-v2-n3e` merged onto F44. Its CI
run found the items below, largest to smallest. Each is fixed on that branch under Defence Before
Fix. F44 is merged in before every push, and GitHub CI is the gate.

- [x] ✅ **Task 8.1**: The 40 project-rule findings the tree had (the 29 the first plan listed,
  with 14 duration and 11 realtime ones where it counted 13 and 10, and F44's new liveness
  rule's 9):
  - 14 `project_duration_overflow`, in the teamchat member/client, nolink, forge, link/server,
    engine/proof, engine/timeout, matrix/client and fakehs/sync: fixed;
  - 11 `project_realtime_bound`: 8 fixed by handing the real waits to `clock.System()`; the 3
    sites in `teamchat/clock` are the package's `realClocks` rows, the owner's decision of
    2026-11-03 (`internal/teamchat/clock` is teamchat's one home for real timers);
  - 5 `project_goroutine_end` in F44 code (`ccc/guard.go` (2), `rclonetail/app.go`,
    `reclaim/ask.go`, the `ui/pathinput` test) and 1 `project_command_failure_dropped`
    (`member/guest/check.go`): fixed;
  - 9 `project_liveness_word`: the roster state is renamed `enrolled` (confirmed by the owner);
    the 2 Matrix presence `online` sites are allowance rows in `livenessAllowances` for
    `internal/teamchat/matrix` and its `fakehs`, the Matrix specification's value (SPEC-matrix
    M50 and M51), the owner's decision of 2026-11-03.
  - The second owners the v2 merge brought into `project_seam_commands` are accepted for now
    (the owner's decision 5 on pull request 124): `lxccmd` beside the v1 owner of `lxc-ls`,
    `lxc-info` and `lxc-attach`, `teamchat/units` beside the v1 owner of `systemctl`, and a
    `dynamicNames` row for a run-time program inside a wrapper. Plan 00209 Task 6.3 collapses
    them to one owner each when v1 is retired.
- [x] ✅ **Task 8.2**: The gosec exclusions v2 added to `.golangci.yml` had no exception in the
  project record. The owner's decision of 2026-11-03: record G401 and G505 (Synapse's
  registration MAC) and the test-file G301, G302 and G306 exclusions in `qa/record.toml`,
  raising the ratchet ceiling by exactly what they need and citing the decision in each entry;
  fix every G304 site in code (the host state, team lock and host_vars files, and the test
  exclusions') and, with them, the lxc tests' G101; where one cannot be fixed, stop and say why
  in the pull request, never record it. The ten tagliatelle `case.overrides` are one rule, v2's
  own JSON formats and the Matrix wire are `snake_case`, stated once above them in
  `.golangci.yml`.
- [x] ✅ **Task 8.3**: gofmt: name the files with a local go-qa run, then format them.
- [x] ✅ **Task 8.4**: Two helper tests:
  - `agent_bus_lxc/test_derive`: on the CI runner, the claude process it finds has an empty
    start time;
  - `plan_acceptance/test_cited_constant_copy_rule`: one finding in Plan 00209's folder.
- [x] ✅ **Task 8.5**: 13 gitleaks findings (12 when the plan was written), all fake keys in test
  fixtures. Allowlist them the way F44 allowlisted its own, never by weakening the scan.
- [x] ✅ **Task 8.6**: In the merge, CLAUDE/QA.md, CLAUDE/GoProjectRules.md, src-go/CLAUDE.md and
  Plans 00194 and 00209 took F44's side of each conflicting hunk. The v2 lines that were lost are
  back, merged by meaning. One is left out on purpose: v2's teamchat-only text and fixtures for
  `project_realtime_bound`, which F44's module-wide rule replaced.
- [ ] 🔄 **Task 8.7**: Pull request 124 (to F44, assigned to the owner's account, reviewed by
  `qa-reviewer`, its findings fixed). CI green, then ready for review. Only the owner merges it.
  The first CI run of the Go tests found tests that pass only as root; they are fixed as a class
  (`project_test_chown`, the owner seams in `homefs`, `proof` and `guestroot`, `mkdirAt` under a
  strict umask), and the JOURNAL says how. A fourth cause, a bind-mount test the runner denies, is
  a skip at the `reexec` seam. CI is green and the pull request is ready for review. Still open,
  the owner's: whether CI allows the mount or accepts the skip, the older tests that skip when not
  root, and an unprivileged gate; and the `qa-reviewer` pass, owed once the agent limit resets.
- [ ] 🔄 **Task 8.9**: The finished v2 work the branch lacked, which the owner asked for on pull
  request 124: `feat/bus-v2-n2` (the reviewed backup and restore work), `feat/bus-v2-n3e` (11
  newer commits) and `feat/bus-v2-n3f` (the side-rooms spec) merged into `spike/v2-merge-up`,
  conflicts resolved by meaning, every gate re-run. Merged; the three branches are contained, and
  are deleted, locally and on GitHub, once F44 holds them (the remote delete is refused until
  then).
- [ ] 🔄 **Task 8.8** (branch `spike/v2-cross-network`, draft pull request 128, based on F44 since 124 merged on 2026-11-05; the
  spec is [SPEC-cross-network.md](SPEC-cross-network.md), the code it starts from is mapped in
  [RESEARCH-cross-network-code-map.md](RESEARCH-cross-network-code-map.md)): Cross-network
  membership, direction 7 of Plan 00209, started on a branch cut from the head of pull request
  124 without waiting for its merge (the owner wants cross-network teams as soon as possible): a
  team declares the networks its members may be on
  as a list of CIDRs, the one source for its listen addresses, `allow_from`, the unit's IP
  filter, the firewall rules and the seat URL check; a seat is keyed on the homeserver URL;
  transport is TLS with the team's own CA pinned in the member bundle (plain HTTP only on
  loopback); a remote seat's token is issued over ssh; and a seat handle never carries a real
  hostname. These replace Plan 00209 Task 0.6's "plain HTTP only inside a mesh tunnel".
  The owner's decision 10 on pull request 124 asks for a detector for "a fake process made by
  running a tool under another name", once this task has started: it is issue 126.
  State: S1 to S6, S7a, S8, S9 (`teamchat seat place-issued`) and S10 are built and committed on
  the branch. S7b is built: the terminator (package `tlsterm`), its verb `teamchat tls`, the
  project rule `project_transport_home` with the terminator as its third owner, and the unit
  `teamchat-tls@` (its drop-in with the IP filter, the socket bind and the leaf as credentials;
  the homeserver bound to loopback only; apply, serve and remove for the second unit, X18a to
  X18c). The health tick reads the terminator unit's state and records a problem when it is not
  active; a probe through the terminator is not built (X18c). S11 is built: Element's check and
  apply say the CA must be trusted and where it is (X20; whether the Flatpak build honours an added
  CA is unchecked), `docs/agent-bus-cross-network.md`, and X29's acceptance test. The owner's
  slices after S11 (SPEC-cross-network.md section 6) are built, each in its own pull request
  stacked on this one: S12 unattended leaf renewal (X31, #173), S13 a probe through the
  terminator (X32, #176), S14 rotating the CA (X33, #177) and S15 Element trusts the CA through
  the play (X34, #172). The coordinator's review of 128 (2026-11-07) found three things the
  pull request called built that no production path reached, and they are fixed on this
  branch: B1 the team backup carries the CA and a restore keeps it (backup manifest version 2);
  B2 (S16) the production System serves the team CA, so https bundles can be issued; B3 both
  teamchat drop-ins deny what they do not allow (`IPAddressDeny=any`, `SocketBindDeny=any`),
  judged by `TestEveryUnitTextDeniesWhatItDoesNotAllow` and X29. S2 (the default url is https,
  plain http only to a loopback address) and S3 (a url's port must be the team's) are fixed
  too. Still open from that review: S1, S4 to S8 and the nits. Then X30, the second-machine run,
  a host or VM-lab check that needs the host play and the launcher (the install path, #193
  item 2), which the coordinator queues. S12's health tick renews the leaf by apply's own step
  (`host.LeafRenewer`, with the project rule `project_problem_error_text`); the health unit's
  write paths must include the team's TLS directory when that unit is written (report
  `subagent-reports/261106-crossnet-s12-s13-opus-report.md`). S13's health tick probes each
  listen address through the terminator with the team CA (`matrix.Reach`), one problem per
  silent address (report `subagent-reports/261106-crossnet-s13-opus-report.md`). S15's Element
  play places the team CA and checks it from inside Element's sandbox; whether Element's
  Chromium honours the store is the host check, and two owner questions (SPEC 6 and 7) stand.

## Success Criteria

- [ ] Pull request 124 is merged by the owner with CI green, the Go tests run by CI.
- [ ] The decision cited in each new `qa/record.toml` entry is the owner's of 2026-11-03.
- [ ] A member on a second machine joins a team over TLS, its token issued over ssh, and a
  connection from a network the team did not declare is refused (Task 8.8).

## Delivery & Milestones

Git is the source of truth for when; the activity log is in JOURNAL/.

- Pull request 124: `spike/v2-merge-up` to F44 (Tasks 8.1 to 8.7).
- Task 8.8: its own branch and pull request, cut from the head of pull request 124.
