# Plan 00161: agent team bus (Matrix)

**Status**: In Progress
**Created**: 2026-10-06
**Owner**: joseph
**Priority**: High

Implements [fedora-desktop#59](https://github.com/LongTermSupport/fedora-desktop/issues/59).

## Overview

Agent sessions in separate containers have no way to signal each other, so cross-repository
work needs one orchestrator session that carries every repository's toolchain and
credentials. The owner asked to deliver the bus that issue #59 designs: a private Matrix
homeserver per team, installed by a play or a standalone installer on any Fedora desktop or
server, a standard-library CLI (`pingbus`) through which an agent sends and receives
**pings** (a closed verb plus a reference to a committed artefact), free text from the
team's humans aimed at specific agents, hooks that wake an idle session, a launch flag that
puts one ccy session in one or more teams, a seat in each (`ccy --teams <seat>@<team>[,…]`),
and an optional desktop client play. v1
teams live on this machine; teams spanning machines are a later phase (owner, 2026-10-07).

The owner's answers (Task 1.3) change two of the issue's rules: humans may send free text to
agents, and a team spans hosts and encapsulations. The rest hold: agent-to-agent traffic is
pings only, validated on send and on receive; nothing team-specific is committed to this
repository; opt-in per machine and per team.

## Goals

- A team's homeserver is installed on a Fedora desktop or server with one command (a play,
  or the standalone installer): federation off, sign-ups closed, listening only where the
  team's members can route to it.
- An agent that opts in (ccy session, the bare desktop, LXC, docker or a VM) can
  `pingbus send` and `pingbus wait`/`recv`, and an idle session is woken by a ping. An agent
  can be in several teams.
- Humans watch through Element, from a desktop or a phone, and can address free text to a
  specific agent.
- A versioned protocol specification lives in this repository and the CLI enforces it.
- The privacy acceptance checks in the issue are each proven by a check, not assumed.

## Non-Goals

- Federation, or a homeserver on the public internet.
- Homeservers on anything but Fedora (for now).
- Teams spanning machines (a member or homeserver on another host, over WireGuard): a later
  phase (owner, 2026-10-07; DESIGN.md D46). The design for it is kept, marked "later phase".
- Per-agent forge identities (issue open question 5: revisit later).
- An MCP server.

## Tasks

Detail of every task and milestone below, as it stood at the PLAN.md split, is in
[`MILESTONES-DONE.md`](MILESTONES-DONE.md); the journal has the history.

### Phase 1: Understand and design

- [x] ✅ **Tasks 1.1-1.10**: research, DESIGN.md and PROTOCOL.md, the owner's answers and
  seat/launch-flag decisions, all folded into [`DESIGN.md`](DESIGN.md) and
  [`PROTOCOL.md`](PROTOCOL.md); detail in [MILESTONES-DONE.md](MILESTONES-DONE.md).

### Phase 2: Build, by milestone (units U00-U33 in DESIGN.md section 12)

- [ ] 🔄 **M0 probes**: U00 (host), U01 (a logged-in child `claude`). Both built and run;
  corrections are folded into DESIGN.md. U01 complete (host run 26-10-07); H1 and H2 settled
  DESIGN.md 5.3. Open: H6 (Element Desktop is not installed yet, U27) and the libvirt guest
  owner step. Full text: [MILESTONES-DONE.md](MILESTONES-DONE.md).
- [x] ✅ **M1 host-to-host ping, through the installer**: U02-U11, U13-U17. Host run
  26-10-07: `deploy.bash` and `acceptance.bash` PASS, M1.1-M1.3 PASS. Detail:
  [MILESTONES-DONE.md](MILESTONES-DONE.md).
- [x] ✅ **M2 ccy members, the idle session woken**: U12, U18-U20, U29-U33. Accepted on the
  host 26-10-09 (M2.0-M2.10). Detail: [MILESTONES-DONE.md](MILESTONES-DONE.md).
  - [ ] 🔄 Defect: the 26-10-15 acceptance re-run failed M2.9, a plain headless `ccy` carrying
    the seat of the checkout's previous `--teams` session. Cause: Quick Launch replayed the
    saved team on a launch nobody answered (Plan 00177 Task 7.2), against D38. Fixed 26-10-15
    test first (CCY 3.97.3): only a person's yes to the offer, or `--teams`, takes a team.
    Pending: the ccy play and acceptance.bash on the host.
    [report](subagent-reports/261015-m29-seat-leak-opus.md)
- [ ] 🔄 **M3 other encapsulations on this host, the play**: U21-U23. U24 (a team on another
  machine over WireGuard) is a later phase (DESIGN.md D46). U21, U22 and U23 (LXC, docker,
  VM) are built and accepted on the host (U23.vm 26-10-12); `play-agent-bus.yml` passed its
  host legs 26-10-07.
  - [ ] ⬜ README.vm's bridge route (a qemu:///system guest on a libvirt bridge, its subnet in
    `allow_from`) is documented but no acceptance exercises it. Decide: prove it by IaC (a
    bridged lab guest) or narrow README.vm to the passt route. Proposed: one bridged lab guest by IaC
    serves this and P2's VM leg; [DESIGN-bridged-lab-guest.md](DESIGN-bridged-lab-guest.md),
    five questions for the owner.
  - [x] ✅ `ccy-teams` (owner, 2026-10-12): built and deployed 26-10-13; the owner created a
    real team with `ccy-teams create` on 26-10-13 and it worked.
  - [ ] 🔄 U22.lxc: the owner's existing LXC containers join a team by IaC (owner,
    2026-10-12). Built: `agent_bus_lxc_members` (`localhost.yml.dist`), applied by
    `play-agent-bus.yml` through `helpers/agent_bus_lxc/members.py`, `test_members.py`.
    Pending: a host run with a real container declared (journal 26-10-12).
  - [x] ✅ Defect: the 26-10-15 acceptance run at 14058c62 failed M1.2, "expected exactly one HUMAN line, got ['HUMAN', 'REPLY']". Cause: Plan 00201 M3 made `recv` print a REPLY line after every HUMAN and PEER line; the host ran an older pingbus at 70ef1d02, so the check never met it. `acceptance_check.py` now requires each HUMAN or PEER line followed by its own REPLY (same team and event, command `say --to` the sender); a PING has none. Its test fixtures also lacked the `copy` field of PING and PEER lines (Plan 00196) and are fixed. Pending: a host re-run.
  - [ ] 🔄 Defect: the 26-10-15 acceptance re-run failed U23.lxc, `env: 'pingbus': Permission denied` (exit 126). Cause: the host run had umask 077 (its run directories turned 0700 on
    26-10-15); sudo and lxc-attach carried it into the member, where the acceptance's kit
    copy made `/usr/local/share/agent-bus` and `kit/` 0700. The installer and `members.py`
    already set 0755 explicitly. Fixed 26-10-15: `U23_KIT_UNPACK_PY` sets every kit directory
    0755, `test_u23_kit_unpack.py`. Pending: acceptance.bash on the host.
    [report](subagent-reports/261015-u23-lxc-exec-opus.md)
- [ ] 🔄 **M4 Element, deploy and acceptance (privacy checks P1-P8)**: U25-U28 (U26, TLS,
  only if probe H7 needs it). U25 (`element.py`, `play-agent-bus-element.yml`,
  `agent_bus_element_teams`) built, reviewed and fixed. Its host run is pending: the owner
  sets `agent_bus_element_teams`, then U27's `meta-deploy.bash` entry runs the play and the
  H6 checks.
  - [ ] ⬜ P2's VM leg (DESIGN.md section 10): needs a guest whose source address is outside
    `allow_from`, so a bridged guest (a passt guest's traffic leaves from the host itself).
    To be designed, provisioned by IaC like U23's member (owner, 2026-10-12): never an owner
    step, never SKIPPED-NEEDS-OWNER.
  - [ ] 🔄 Agent-to-agent free text (owner, 2026-10-12; DESIGN.md D63, reverses D13): built;
    acceptance M1.4 host run ACCEPTED 26-10-13. Room creation refused: acceptance M1.7 added
    26-10-13 (a member's `createRoom` must get 403 `M_FORBIDDEN`). Host run done at F44 70ef1d02 (every leg passed), before the integration merge. It runs again after the merge, from the branch's meta-deploy list, against the goodbye-aware message counts.
  - [ ] 🔄 Org trust and outside references (owner, 2026-10-13; DESIGN.md D64): built with unit
    tests; deployed 26-10-13; M1.5 and M1.6 pass on the host. Pending: acceptance.bash again
    (D66 deployed 26-10-13); the owner's own testing comes first. Every member must run the
    new pingbus before any team gains `org` (protocol section 1).
    - [x] ✅ An optional `exclude` list of org repositories in the team file, checked before
      trust: built 26-10-13 (refused offline as `excluded`, on send and receive; container 2.53).
      [report](subagent-reports/261013-p161-forge-sonnet.md)
    - [ ] 🔄 Forge rate (DESIGN.md D66): built and deployed 26-10-13. The forge credential is
      gh's login only, required of every member; the acceptance logs its throwaway members in
      with the desktop user's own gh login (owner decision, 2026-10-13, kept over a review's
      suggestion of a stub gh for the harness's non-ccy members). The repository answer is
      kept in the cache file for an hour (26-10-13: a repeat ping costs no call). A GitHub
      rate refusal gets its own drop reason (`forge-rate`, not `unresolved`). Pending:
      acceptance.bash. Its F44 run at 70ef1d02 passed every leg (M1.1-M1.7,
      M2.0-M2.10, U23 lxc/docker/vm), before the integration merge; it runs again after the
      merge, from the branch's meta-deploy list, against the goodbye-aware message counts.
  - [x] ✅ Applied state and team changes by the play (DESIGN.md D65): built and deployed
    26-10-13; the owner created a real team with `ccy-teams create` on 26-10-13 and it worked.
  - [x] ✅ Defect: `ccy-teams apply` always read every team "out of date: config" where lxcbr0
    exists, because the play stamped the digest of the bridge-augmented team file and
    `declare.py status` digested the bare entry. Fixed 26-10-15: `bridge.py` owns the bridge
    name and a live `ip` probe; `status` digests `bridge.effective(...)` too; `agent_bus_lxc_bridge`
    and the facts path are gone. The create wizard's failure line no longer claims "not installed".

## Success Criteria

- [ ] Several ccy sessions in one checkout, each launched with `--teams <seat>@<team>` on
  its own seat, exchange a `review` ping and an `ack`; the idle one is woken; a later
  session in a seat continues as its member and reads its history; two sessions cannot
  hold one seat, and one launch cannot take two seats of one team; a plain `ccy` in the
  same checkout is on no team (owner, 2026-10-07: DESIGN.md D34, D40, D44, D48).
- [ ] A human's message addressed to one agent in Element reaches that agent, marked as
  from that human; no other agent's `pingbus` delivers it; an agent's free text reaches
  only the agents it names, as a peer's `PEER` line, and every message is in the one team
  room (D63).
- [x] Agents in LXC, docker and a VM on this host are in the same team as the desktop's
  members and exchange a ping (U23).
- [ ] The homeserver makes no outbound connection and answers only on the addresses the
  team's members use (checked, per the issue's privacy list).

Later phase, not required to close this plan (owner, 2026-10-07; DESIGN.md D46):

- An agent in a different encapsulation on another host (LXC or VM) is in the same team and
  exchanges a ping (U24, over WireGuard).

## Delivery & Milestones

- <!-- delivery commit hashes -->
