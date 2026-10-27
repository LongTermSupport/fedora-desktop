# Plan 00232: ccy network policy per container

**Status**: In Progress (design accepted, Q1 to Q4 decided; host triage pending before the build)
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: Medium

## Overview

A ccy container can open a connection to anything the host can route to: the LAN, the host's
own services, other sessions. Only the absence of a credential stops a login (issue #114).
This plan gives every ccy container a network policy, declared per checkout on the host,
enforced outside the container, and impossible to change from inside it.

The owner decided the default (issue #114 ask 3): **block the private ranges and the host
itself, keep the internet**, and make a refusal understandable to the agent, by failing at once
and by telling every session its limits at start. The owner added a hard requirement: **do not
break agent teams**. Every path the team stack needs is always allowed or derived from the team
configuration, and the acceptance gate proves pingbus messages still flow both ways.

The plan is research-gated. Phase 1 establishes the facts with a host triage run; the design is
in [DESIGN.md](DESIGN.md) and is reviewed before any enforcement is built.

## Goals

- A per-checkout allow-list, read on the host before the container starts, never visible to or
  editable by the container (issue #83's host-side class)
- Enforcement the container cannot lift, with no root on the host
- The default "block privates" policy, with everything ccy itself needs derived automatically:
  DNS, the agent bus, team homeservers, joined project networks, `ccy --connect`
- A refusal that fails at once, and a `CCY NETWORK POLICY` block in every session's context
- A gate (issue #114 ask 4) that also sends and receives a pingbus message under the policy

## Non-Goals

- Filtering the public internet (the owner chose to keep it open)
- Policies for Element, LXC, docker or VM team members: they are not ccy containers
- Changing how `--network`, `--no-network` or `--connect` choose networks
- The general read-only paths feature of issue #83 (only the fact A10 measures is fed back)

## Context & Background

**Prior work.** Plan 00080 (now Completed as superseded by this plan) established that ccy
chooses the shared `podman` bridge over pasta to keep mid-session `ccy --connect`, and decided
on a network per session to stop sessions reaching each other. The default policy here refuses
the bridge's own subnet, which gives that isolation without per-session networks (DESIGN.md
section 4, decision Q4). Plan 00160 made `ccy.env.local`, sourced in the container and bound read-only; issue #83
asks for declared read-only paths. Plan 00161 built the agent bus.

### Facts

| #   | Fact                                                                                                                                                                                                               | Source                                                                                                         |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| F1  | With Podman and no network chosen, ccy sets `--network podman`, the shared rootless bridge                                                                                                                         | `claude-yolo:3336-3341`; Plan 00080 F1                                                                         |
| F2  | `--no-network` leaves the flag empty, so the session gets pasta                                                                                                                                                    | `claude-yolo:2661-2664`; Plan 00080 F2b                                                                        |
| F3  | `ccy --connect` runs `podman network connect` on a running session and records the network for later launches                                                                                                      | `claude-yolo:1166-1170`; Plan 00080 F31                                                                        |
| F4  | The launcher adds no capability and no `--privileged`; a live session's CapEff is podman's default set, without NET_ADMIN (bit 12) or SYS_ADMIN (bit 21)                                                           | `claude-yolo:3880-3920`; `/proc/self/status` in a live session                                                 |
| F5  | A live session's nameservers are pasta's link-local forwarder, then the host's resolvers, one of them in a private range                                                                                           | `/etc/resolv.conf` in a live session                                                                           |
| F6  | `host.containers.internal` is a link-local address inside the session                                                                                                                                              | `/etc/hosts` in a live session                                                                                 |
| F7  | The host's `/etc/hosts` entries are copied into the session, here more than a dozen names on the LXC bridge (dev sites)                                                                                            | `/etc/hosts` in a live session                                                                                 |
| F8  | The bus `base_url` is `http://<bus_ip>:<port>`, `<bus_ip>` on `agentbus0`, reached through pasta; the launcher reads it live; the installer refuses only loopback, link-local, multicast and unspecified addresses | Plan 00161 DESIGN.md section 3.3; `helpers/agent_bus/checkout.py:134-145,494-505`; `agent-bus-install:194-207` |
| F9  | Every session without `--no-bus` carries the bus kit, and the owner can seat a running session later                                                                                                               | `lib/agent-bus-membership.bash:2-16`                                                                           |
| F10 | pingbus calls `https://api.github.com`                                                                                                                                                                             | `helpers/pingbus/forge.py:47`                                                                                  |
| F11 | Element is a host Flatpak; LXC, docker, VM and other-host members reach the homeserver themselves; a remote team's address would be in the seat bundle's `base_url`                                                | Plan 00161 DESIGN.md section 3.3; `helpers/agent_bus/admin.py:1046-1060`                                       |
| F12 | The startup system prompt is passed only when the launch has no prompt and no arguments                                                                                                                            | `claude-yolo:3476-3534`                                                                                        |
| F13 | `ccy.env` and `ccy.env.local` are sourced in the container only; `ccy.env.local` is bound read-only                                                                                                                | `entrypoint.sh` PROJECT-ENV block; `lib/common.bash:704-716`                                                   |
| F14 | The container's mounts are the workspace, the config import, `CCY_EXTRA_MOUNTS`, `.claude/ccy/mounts`, `ccy.env.local` and the membership directory; `~/.claude-tokens/ccy/` is not among them                     | `claude-yolo:2519-2545`; `lib/agent-bus-membership.bash:67`                                                    |
| F15 | A live session has no global IPv6 address, only link-local                                                                                                                                                         | `/proc/net/if_inet6` in a live session                                                                         |
| F16 | The repo's scanners refuse a dotted private address in any tracked file, so the ranges are written short (`10/8`) and expanded in code                                                                             | `scripts/git-hooks/pre-commit:73-77`; the hooks daemon's `private-ipv4` pattern                                |

### Hypotheses (each settled by a named triage probe)

| #   | Hypothesis                                                                                                                                                    | Settled by |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| H1  | The host user can apply nft rules in a created, not yet started, container's namespace via `podman unshare nsenter`, without root, and plain `nsenter` cannot | A2         |
| H2  | The container cannot lift them: no nft or ip change succeeds inside, and the table is intact afterwards                                                       | A4         |
| H3  | One verdict fails in well under 2 s with a message distinct from "service down"                                                                               | A3         |
| H4  | `podman create --rm`, `init`, apply, `start -ai` runs the process with the policy in place and removes the container                                          | A5         |
| H5  | The table survives `podman network connect`, the new subnet is refused, and adding it from the host opens it                                                  | A6         |
| H6  | The same works for a pasta container (`--no-network`)                                                                                                         | A9         |
| H7  | DNS, the forge, the Claude API and the bus homeserver still answer under each candidate                                                                       | A3         |
| H8  | A container can replace a read-only bound file by moving its parent aside (bears on where a policy may live)                                                  | A10        |

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Read issue #114, issue #83, Plans 00080, 00160 and 00161, the launcher,
  and the agent-team stack's network paths; facts F1 to F16
- [x] ✅ **Task 1.2**: `triage.bash` (passive host facts P1 to P9; `--active` probes A1 to A10 on
  throwaway containers), with the ruleset generator `_netpolicy.inc.bash` and its tests
  `test-netpolicy-inc.bash`
- [x] ✅ **Task 1.3**: The gate, red first: `acceptance-policy.bash` with `acceptance-checks.bash`
  (caps, held, blocked with a control, allowed, context, and a pingbus message each way)
- [ ] ⬜ **Task 1.4**: HOST: run `triage.bash --active` (through meta-deploy); turn H1 to H8 into
  facts; settle the PENDING TRIAGE points of DESIGN.md

### Phase 2: Decision gate

- [x] ✅ **Task 2.1**: [DESIGN.md](DESIGN.md): the enforcement options, the default policy with every
  team path, where the policy lives, the refusal and the context, the implementation shape, owner
  questions Q1 to Q4
- [x] ✅ **Task 2.2**: The owner accepted the direction and decided Q1 to Q4 (DESIGN.md section 8):
  - **Q1**: an `lxc` allow keyword opens exactly the LXC dev-site addresses found on the host;
    refused unless a checkout asks
  - **Q2**: `CCY_NETWORK_ALLOW=off` exists; the session's context then says it has open reach
  - **Q3**: under Docker, run unprotected with a printed warning; the context says no policy
  - **Q4**: Plan 00080 closed as superseded by this plan (moved to `Completed/`)

### Phase 3: Build (after Task 1.4: the build waits for the host triage results)

- [ ] ⬜ **Task 3.1**: The Go policy package and `ccylaunch` subcommand, test first, including the
  `lxc` and `off` keywords and the Docker warning path
- [ ] ⬜ **Task 3.2**: The launcher's create, init, apply, start sequence; the capability guard;
  `ccy --connect`, `--disconnect` and `ccy-teams` widen and narrow the live table; the
  `CCY NETWORK POLICY` context and `/run/ccy/network-policy`; nft declared in
  `play-claude-yolo.yml`; `CCY_VERSION` bump, changelog, `docs/ccy.md`
- [ ] ⬜ **Task 3.3**: `deploy.bash` (the play, then two throwaway seated headless sessions);
  rename the gate to `acceptance.bash`; QA; `qa-reviewer`

## Dependencies

- Builds on: Plan 00161 (the agent bus), Plan 00160 (`ccy.env.local`)
- Related: issue #83 (read-only paths; A10 feeds it)
- Supersedes: Plan 00080 (`Completed/00080-ccy-session-network-isolation`)

## Success Criteria

- [ ] H1 to H8 settled by a host triage run
- [x] Design reviewed; Q1 to Q4 answered
- [ ] `acceptance-policy.bash` passes on two sessions under the default policy, including the
  pingbus message each way
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` verdict PASS

## Risks & Mitigations

| Risk                                                           | Impact | Probability | Mitigation                                                                         |
| -------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------- |
| The policy breaks agent teams                                  | H      | M           | Bus address on all ports and seat-bundle hosts derived automatically; pingbus gate |
| A session starts before its table is in place (fail open)      | H      | L           | Create, init, apply, verify, then start; any failure stops the launch              |
| A later change gives the container NET_ADMIN                   | H      | L           | Launch guard on capabilities; acceptance `caps` check                              |
| Projects that reach LXC dev sites or LAN services stop working | M      | H           | The session is told its limits and how the owner widens them; `lxc` keyword (Q1)   |
| DNS breaks because a resolver is private or link-local         | H      | H           | Nameservers derived from the container's own `resolv.conf`, port 53 only           |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00232-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research, triage, the red gate and the design written; host triage pending
