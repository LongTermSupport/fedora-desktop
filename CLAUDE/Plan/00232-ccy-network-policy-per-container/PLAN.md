# Plan 00232: ccy network policy per container

**Status**: In Progress (built, deployed and ACCEPTED on the host at CCY 3.101.1; left: the final `qa-reviewer`, the owner's points, Task 3.6)
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

| #   | Fact                                                                                                                                                                                                                                                                               | Source                                                                                                         |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| F1  | With Podman and no network chosen, ccy sets `--network podman`, the shared rootless bridge                                                                                                                                                                                         | `claude-yolo:3336-3341`; Plan 00080 F1                                                                         |
| F2  | `--no-network` leaves the flag empty, so the session gets pasta                                                                                                                                                                                                                    | `claude-yolo:2661-2664`; Plan 00080 F2b                                                                        |
| F3  | `ccy --connect` runs `podman network connect` on a running session and records the network for later launches                                                                                                                                                                      | `claude-yolo:1166-1170`; Plan 00080 F31                                                                        |
| F4  | The launcher adds no capability and no `--privileged`; a live session's CapEff is podman's default set, without NET_ADMIN (bit 12) or SYS_ADMIN (bit 21)                                                                                                                           | `claude-yolo:3880-3920`; `/proc/self/status` in a live session                                                 |
| F5  | A live session's nameservers are pasta's link-local forwarder, then the host's resolvers, one of them in a private range                                                                                                                                                           | `/etc/resolv.conf` in a live session                                                                           |
| F6  | `host.containers.internal` is a link-local address inside the session                                                                                                                                                                                                              | `/etc/hosts` in a live session                                                                                 |
| F7  | The host's `/etc/hosts` entries are copied into the session, here more than a dozen names on the LXC bridge (dev sites)                                                                                                                                                            | `/etc/hosts` in a live session                                                                                 |
| F8  | The bus `base_url` is `http://<bus_ip>:<port>`, `<bus_ip>` on `agentbus0`, reached through pasta; the launcher reads it live; the installer refuses only loopback, link-local, multicast and unspecified addresses                                                                 | Plan 00161 DESIGN.md section 3.3; `helpers/agent_bus/checkout.py:134-145,494-505`; `agent-bus-install:194-207` |
| F9  | Every session without `--no-bus` carries the bus kit, and the owner can seat a running session later                                                                                                                                                                               | `lib/agent-bus-membership.bash:2-16`                                                                           |
| F10 | pingbus calls `https://api.github.com`                                                                                                                                                                                                                                             | `helpers/pingbus/forge.py:47`                                                                                  |
| F11 | Element is a host Flatpak; LXC, docker, VM and other-host members reach the homeserver themselves; a remote team's address would be in the seat bundle's `base_url`                                                                                                                | Plan 00161 DESIGN.md section 3.3; `helpers/agent_bus/admin.py:1046-1060`                                       |
| F12 | The startup system prompt is passed only when the launch has no prompt and no arguments                                                                                                                                                                                            | `claude-yolo:3476-3534`                                                                                        |
| F13 | `ccy.env` and `ccy.env.local` are sourced in the container only; `ccy.env.local` is bound read-only                                                                                                                                                                                | `entrypoint.sh` PROJECT-ENV block; `lib/common.bash:704-716`                                                   |
| F14 | The container's mounts are the workspace, the config import, `CCY_EXTRA_MOUNTS`, `.claude/ccy/mounts`, `ccy.env.local` and the membership directory; `~/.claude-tokens/ccy/` is not among them                                                                                     | `claude-yolo:2519-2545`; `lib/agent-bus-membership.bash:67`                                                    |
| F15 | A live session has no global IPv6 address, only link-local                                                                                                                                                                                                                         | `/proc/net/if_inet6` in a live session                                                                         |
| F16 | The repo's scanners refuse a dotted private address in any tracked file, so the ranges are written short (`10/8`) and expanded in code                                                                                                                                             | `scripts/git-hooks/pre-commit:73-77`; the hooks daemon's `private-ipv4` pattern                                |
| F17 | The host has nft 1.1, nsenter and rootless podman 5.8 on netavark, with pasta for the rootless namespace (`--no-map-gw`, a link-local `--dns-forward`, `--map-guest-addr` for `host.containers.internal`)                                                                          | triage P1, P7                                                                                                  |
| F18 | Plain `nsenter` into a created, initialised, not started container's network namespace is refused (EPERM); `podman unshare nsenter -t <pid> -n nft list ruleset` works, and its nameservers are read on the host before start                                                      | triage A2                                                                                                      |
| F19 | Every live session: not privileged, no `--cap-add`, podman's 11 default capabilities (CapEff `0x800405fb`, bits 12 and 21 clear); the image has no nft, ip or iptables                                                                                                             | triage P6, A4                                                                                                  |
| F20 | On the `podman` bridge (10.88/16) a session's nameservers are pasta's link-local forwarder, the LAN router (192.168/16) and two public resolvers; on a DNS-enabled project network (10.89.N/24) the only one is that network's gateway                                             | triage P6                                                                                                      |
| F21 | nft refused the candidate table at every apply ("conflicting intervals specified"): the host's own addresses lie inside the private ranges, and the bus address came twice; `nft -f` is atomic, so no table existed                                                                | triage A3 to A9                                                                                                |
| F22 | So A3's verdicts, A4's "still in place", A5's apply, A6 and A9 all measured an unfiltered container; the probe harness reported the failed apply and carried on                                                                                                                    | triage A3 to A9                                                                                                |
| F23 | P9's three `FAIL nft parses` lines were the harness, not the ruleset: an unprivileged `nft --check` in the host's namespace cannot open its netlink cache (EPERM); the test also had no overlapping input                                                                          | triage P9                                                                                                      |
| F24 | Unfiltered, the host's outbound-interface address is refused at once from a session (pasta copies it into the rootless namespace, where it is local and netavark's published-port DNAT answers), on the bridge and on pasta                                                        | triage A3 mode none, A7, A9                                                                                    |
| F25 | Unfiltered, a session reaches the host through `host.containers.internal` (169.254/16), a peer session on the bridge, DNS, the forge, the Claude API and the bus                                                                                                                   | triage A3 mode none                                                                                            |
| F26 | Every other host address (the LXC bridge, docker bridges, VPN and WireGuard clients, `agentbus0`) is in a default range; its global IPv6 addresses are public, and a session has no global IPv6 (F15 again); the host routes further private subnets over its VPN                  | triage P4, A1, A8                                                                                              |
| F27 | The seat bundles' `base_url` host is the `agentbus0` address (a /32 in 10/8), also their only `plain_http_hosts` entry; several teams are applied on the host, including LXC members                                                                                               | triage P5                                                                                                      |
| F28 | `podman start -ai` of a `--rm` container carries stdin and removes the container on exit; `podman network connect` succeeds on a running one                                                                                                                                       | triage A5, A6                                                                                                  |
| F29 | **A container can replace a read-only bound file**: the write is refused, but renaming the parent directory and writing a new file at the path both succeed, and the host then reads the container's file                                                                          | triage A10                                                                                                     |
| F30 | netavark's table lives in the shared rootless namespace (input, forward, NAT and isolation chains); no ccy table was there                                                                                                                                                         | triage A7                                                                                                      |
| F31 | The host's nft parses all three candidate rulesets and takes overlapping and duplicate elements; `auto-merge` folds the doubled bus address into one element                                                                                                                       | rerun P9, A3                                                                                                   |
| F32 | `podman unshare nsenter -t <pid> -n nft -f -` applies the table in an initialised, not started container, and `nft list` reads it back, on the bridge and on pasta                                                                                                                 | rerun A3, A5, A6, A9                                                                                           |
| F33 | Drop: every refused attempt waits out its 5 s timeout. TCP reset: refused in under 15 ms, but reads "Connection refused", like a service that is down. ICMP admin-prohibited: refused in under 15 ms; nc and ssh say "No route to host", curl exits 7 "Couldn't connect to server" | rerun A3                                                                                                       |
| F34 | Under every candidate DNS, the forge, the Claude API and the bus answer; the host alias, a host address off the outbound interface and a peer session are refused                                                                                                                  | rerun A3                                                                                                       |
| F35 | With exactly the container's bounding set, deleting the table, flushing the ruleset and deleting the default route are each refused (EPERM); the control with `net_admin` lists the table; the table is intact afterwards                                                          | rerun A4                                                                                                       |
| F36 | `create --rm`, `init`, apply, `start -ai`: the process's first connection is already filtered, exit status 75 reaches the caller, and the container is gone after                                                                                                                  | rerun A5                                                                                                       |
| F37 | The table survives `podman network connect`; the new project subnet is refused until the host adds it to the allow set, then answers                                                                                                                                               | rerun A6                                                                                                       |
| F38 | The host's global IPv6 addresses changed between P4 and A7, minutes apart (temporary addresses), so they are read at each launch; a session still has no global IPv6                                                                                                               | rerun P4, A7, A8                                                                                               |
| F39 | The checkout's seat bundles (`.claude/ccy/pingbus/seats/`) are inside the workspace the container writes, so a `base_url` there is the container's to change                                                                                                                       | rerun P5; `acceptance-checks.bash` `member_json`                                                               |
| F40 | Each probe container ignored SIGTERM (its PID 1 is `sleep` or a shell), so `podman rm -f` waited 10 s per container before SIGKILL                                                                                                                                                 | rerun log                                                                                                      |
| F41 | The first host address off the outbound interface was unreachable from the unpoliced control too (curl 7): it is the address pasta copies into the rootless namespace, so a refusal of it proves nothing                                                                           | acceptance run 1 `blocked`                                                                                     |
| F42 | A headless launch replaying a Quick Launch record with no saved network is `--no-network`, so it runs on pasta, and `podman network connect` refuses a pasta container ("pasta" is not supported), policy or not                                                                   | acceptance run 1 `connect`; `claude-yolo:1311-1312`                                                            |
| F43 | `pingbus inbox` lists only what is still pending, and never the text, so it could not show the probe's nonce; `pingbus history` reads the room and prints each received PEER item with its event id and text                                                                       | `helpers/pingbus/cli.py` `cmd_inbox`, `cmd_history`                                                            |
| F44 | The throwaway seated sessions take turns of their own, woken by the bus (on their membership notice, and they answered over the bus): harmless, a haiku turn each in a throwaway seat, but they empty their inboxes                                                                | acceptance run 1 `bus`; the coordinator's view of the room                                                     |
| F45 | `ccy --connect` in np232c's project also printed a refresh line for np232a and np232b: the connect is project-scoped, the refresh covers every live session of every project and recomputes each from its own networks, so an unchanged session keeps the same allow set (N9)      | acceptance run 2 `connect`; `internal/netpolicy/app.go` `refresh`, `refreshOne`                                |
| F46 | The seated headless sessions do not end within 60 s of their stdin closing, in both runs; the unseated np232c, driven the same way, ends at once (exit 0). The seated ones carry the bus delivery path (an inbox socket and a watcher); which part keeps them alive is not known   | acceptance runs 1 and 2 teardown; `helpers/pingbus/hooks.py` `session-start`                                   |

### Hypotheses (each settled by a named triage probe)

Run 1 settled H8; the rerun settled H1 to H7, all confirmed.

| #   | Hypothesis                                                                                                                                                    | Settled by | Verdict                                                   |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------------------- |
| H1  | The host user can apply nft rules in a created, not yet started, container's namespace via `podman unshare nsenter`, without root, and plain `nsenter` cannot | A2, A3     | **confirmed** (F18, F32)                                  |
| H2  | The container cannot lift them: no nft or ip change succeeds with the container's own capabilities, and the table is intact afterwards                        | A4         | **confirmed** (F35)                                       |
| H3  | One verdict fails in well under 2 s with a message distinct from "service down"                                                                               | A3         | **confirmed** for ICMP admin-prohibited, nc and ssh (F33) |
| H4  | `podman create --rm`, `init`, apply, `start -ai` runs the process with the policy in place, carries its exit status and removes the container                 | A5         | **confirmed** (F28, F36)                                  |
| H5  | The table survives `podman network connect`, the new subnet is refused, and adding it from the host opens it                                                  | A6         | **confirmed** (F37)                                       |
| H6  | The same works for a pasta container (`--no-network`)                                                                                                         | A9         | **confirmed** (F32, F34)                                  |
| H7  | DNS, the forge, the Claude API and the bus homeserver still answer under each candidate                                                                       | A3         | **confirmed** (F34)                                       |
| H8  | A container can replace a read-only bound file by moving its parent aside (bears on where a policy may live)                                                  | A10        | **confirmed** (F29)                                       |

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Read issue #114, issue #83, Plans 00080, 00160 and 00161, the launcher,
  and the agent-team stack's network paths; facts F1 to F16
- [x] ✅ **Task 1.2**: `triage.bash` (passive host facts P1 to P9; `--active` probes A1 to A10 on
  throwaway containers), with the ruleset generator `_netpolicy.inc.bash` and its tests
  `test-netpolicy-inc.bash`
- [x] ✅ **Task 1.3**: The gate, red first: `acceptance-policy.bash` with `acceptance-checks.bash`
  (caps, held, blocked with a control, allowed, context, and a pingbus message each way)
- [x] ✅ **Task 1.4**: HOST: the first `triage.bash --active` run, read into facts F17 to F30.
  It settled H8 and the passive facts (P5, P6), and found the candidate table refused by nft
  (F21), so no enforcement probe measured a policy (F22). Fixed in the plan scripts:
  - the sets take `auto-merge`, and the unit tests feed the host's shape (an address inside a
    range, the bus address twice) to `nft --check`, which now runs in a throwaway user and
    network namespace (F23)
  - a failed apply stops the run (`must`), instead of the probes after it reading as filtered
  - A4 runs the host's nft and ip in the container's namespace with only the container's own
    capabilities (`setpriv` bounding set), with a control that adds `net_admin`
  - the host targets are `host.containers.internal` and an address off the outbound interface
    (F24), in the triage and in the gate; A5 also checks exit status 75; `nc -v`
- [x] ✅ **Task 1.5**: HOST: rerun `triage.bash --active`, read into F31 to F40; H1 to H7
  confirmed; DESIGN.md section 10 settles the verdict (ICMP admin-prohibited) and the build
  points. The probes now remove their containers with `podman rm -f -t 0` (F40)

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

### Phase 3: Build

DESIGN.md section 7 is the shape, as section 10 settles it. In order, each test first:

- [x] ✅ **Task 3.1**: The Go policy package `internal/netpolicy` (pure): parse the host file
  and `CCY_NETWORK_ALLOW` (addresses, prefixes, names with `:port`, the `lxc` and `off`
  keywords); the default ranges from octets; the derived allows (nameservers from a
  `resolv.conf`, the `agentbus0` address, project network subnets, the `lxc` addresses from
  the host's hosts file inside `lxcbr0`'s prefix); normalisation (dedupe, drop an entry
  another covers); the table, with the ICMP admin-prohibited verdict
- [x] ✅ **Task 3.2**: The `ccy-network-policy` command: `launch` (create, init, the
  capability guard, apply through an injected runner, `podman unshare nsenter -t <pid> -n nft -f -`, verify by reading the table back, start); `refresh` (replace every live
  session's allow set whole in one transaction); `where`; the context text; tested through
  fakes
- [x] ✅ **Task 3.3**: The launcher: its container call through `ccy-network-policy launch`
  (the `CCY NETWORK POLICY` block for every session and `/run/ccy/network-policy`; the
  unavailable path's warning); `ccy --connect` and `--disconnect` refresh the live tables;
  nft declared in `play-claude-yolo.yml`, the command built and installed there;
  `CCY_VERSION` bump (3.101.0), changelog, `docs/ccy.md`; the difftest goldens re-recorded
- [ ] 🔄 **Task 3.4**: `deploy.bash` (the play); the gate renamed `acceptance.bash`, which
  launches the two throwaway seated headless sessions itself (np232a and np232b of the team the
  checkout has a seat in), judges them and removes them, since a session must not outlive the
  script that can remove it; the `meta-deploy.list` entry (ahead of `changed-plays`); QA;
  `qa-reviewer` (FIX-BEFORE-MERGE, its seven should-fix findings fixed red first: the refresh
  skips unprotected sessions, the probes must measure, a `connect` check on a third throwaway session in its own throwaway project, so `--connect` reaches no live session, the queue order,
  IPv6 neighbour discovery, the way out named, the tokens given to the container alone);
  the review of the fixes
- [x] ✅ **Task 3.5**: HOST: the `meta-deploy.bash` run; read the acceptance into the plan.
  Run 1: triage, deploy and triage after passed (the policy is live); acceptance REJECTED on
  three checks, each the check's own design (F41 to F44), fixed red first against fakes in
  `test-acceptance-checks.bash`:
  - `blocked`: only the host addresses the unpoliced control reaches are put to the session;
    none is COULD NOT ESTABLISH (exit 2), which `acceptance.bash` now passes on
  - `connect`: np232c is launched on the `podman` network, as a plain launch is; and
    `ccy --connect` to a session with no network now says it cannot join one and to relaunch
    with `--network` (CCY 3.101.1, `scripts/test-ccy-network-disconnect.bash`); a headless
    replay of no saved network now joins `podman` as an interactive yes does, not
    `--no-network` (`scripts/test-ccy-teams.bash`), so restored team sessions can `--connect`
  - `bus`: arrival is read from the receiver's `pingbus history` by the sent event id
  - the rerun: `deploy.bash` (CCY 3.101.1), then `acceptance.bash`: **ACCEPTED**, every check
    passing (caps, held and context on both sessions; blocked against every host address the
    control reached; allowed; connect; the bus message each way in about 2 s); no throwaway
    container left, no live session relaunched. Two findings, each decided:
    - F45, the refresh reaching other projects' sessions: intended (DESIGN.md section 10,
      `docs/ccy.md` Network policy)
    - F46, the seated sessions not ending on input close: a session behaviour, not the
      acceptance's driving; the teardown's wording now says so, and Task 3.6 finds the cause
- [ ] **Task 3.6**: Why a seated headless session does not end when its stdin closes (F46).
  A plan-local probe on one throwaway seated headless session: close its input, then list the
  container's processes (is claude alive, is the watcher), and repeat with the watcher stopped
  first. If Claude Code holds the session open for its inbox socket, a headless seated session
  is ended by the launcher or the driver, and that is documented; if the watcher does, it is
  fixed to end with the session. Not a gate on the policy: the acceptance removes them
- [x] ✅ **Task 3.7**: Follow-up regression of Tasks 3.3/3.4: `ccy-sessions --list` showed every
  ccy session as "no container" (CPU 0%), because `sessprobe` knew only `<engine> run --name`
  and every launch is now `ccy-network-policy launch --name X ... -- <engine> run ...` with a
  `podman start --attach --interactive X` child. It now reads both shapes (and the
  `start NAME` positional name past start's flags); the policy's attach argv is one function
  (`netpolicy.AttachArgs`). Guard: `TestSessionsFindEveryRecordedLaunch` in
  `internal/ccylaunch/difftest` feeds every recorded launch in the launcher's goldens to
  `SessionContainers` and to `lib/docker-health.bash`'s zombie pgrep pattern, red on the old
  recogniser (26 of 26 launches missed). Deployed by `play-claude-yolo` (the queued
  `changed-plays` entry)

## Dependencies

- Builds on: Plan 00161 (the agent bus), Plan 00160 (`ccy.env.local`)
- Related: issue #83 (read-only paths; A10 feeds it)
- Supersedes: Plan 00080 (`Completed/00080-ccy-session-network-isolation`)

## Success Criteria

- [x] H1 to H8 settled by a host triage run (H8 settled by run 1; H1 to H7 by the rerun)
- [x] Design reviewed; Q1 to Q4 answered
- [x] `acceptance.bash` passes on two sessions under the default policy, including the
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

- Research, triage, the red gate and the design written
- Host triage run 1 read (F17 to F30); the refused table fixed in the plan scripts
- Host triage rerun read (F31 to F40): H1 to H7 confirmed, design settled (DESIGN.md section 10)
- Acceptance rerun ACCEPTED on the host at `cad6b5053` (CCY 3.101.1); F45 and F46 decided
