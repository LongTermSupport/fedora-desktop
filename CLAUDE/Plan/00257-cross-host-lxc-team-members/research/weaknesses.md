# Architectural weaknesses of the agent team bus (v1 pingbus, v2 teamchat)

Research for Plan 00257. Read from `origin/F44` at `73bb544d5`. Every citation is `path:line` on
that commit. Abbreviations used below:

- `C:` = `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md`
- `B:` = `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-B-member.md`
- `SPEC:` = `CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/SPEC-cross-network.md`
- `P246:` = `CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/PLAN.md`
- `P209:` = `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md`
- `XN:` = `docs/agent-bus-cross-network.md`
- `tc/` = `src-go/internal/teamchat/`

"Forced" means the code (or a design rule the code enforces) leaves no other way. "Choice" means
an operator or deployment could do it differently with what exists.

## 1. One homeserver per team

- **Claim.** Each v2 team is its own Tuwunel process with its own port, its own state roots, its
  own TLS terminator, CA, firewall rule set and admin token. Federation is off, so teams never
  share a server or relay through each other.
- **Evidence.** Decision DC1 "one per team" (C:153-180, C:2831). Per-team units
  `teamchat-server@.service` and `teamchat-tls@.service` (`tc/unitfiles/unitfiles.go:68`, `:94-126`).
  One declared port per team, no range (C:430-435). Per-team state: `/var/lib/private/teamchat/<team>/`,
  `/var/lib/teamchat-admin/<team>/`, `/var/lib/teamchat-ctl/<team>/`, `/var/lib/teamchat-tls/<team>/`
  (C:204-222). One CA per team (SPEC:79-83). `allow_federation = false` (`tc/host/render.go:164`).
  v1 is the same shape: `agent-bus-hs@.service` per team (`files/usr/local/sbin/agent-bus-install:46-47`, `:526`).
- **Forced or choice.** Forced: the code has no shared-homeserver mode, and the design rejected it.
- **Consequence.** 100-135 MB RSS per team, linear in teams (C:159, C:172, C:2808; risk K1, C:2857);
  the memory pins in the play are unmeasured (`playbooks/imports/play-teamchat.yml:64-66`). Two
  units, one port, one firewall rule set and one CA per team; a human in N teams needs N Element
  profiles and windows (C:169). Each team is a separate single point of failure (section 7).

## 2. A bus VM per team

- **Claim.** Nothing in the code needs a VM per team. One host serves any number of teams.
- **Evidence.** `teamchat_teams` is a list applied on one inventory host
  (`playbooks/imports/play-teamchat.yml:46`, `:72`, `:100`); the play's default host is the desktop
  (`play-teamchat.yml:72`, C:634). Port and network checks are designed for several teams on one
  host (C:430-435, C:474-479). The only VM-per-bus topology found in the tree is the X30 lab
  scenario (`CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/x30-lab.bash:381`, `:388`).
- **Forced or choice.** Choice. It is the operator's habit (and the lab's topology), not a rule.
- **Consequence.** A VM per team doubles the per-team cost (a guest kernel and OS on top of the
  Tuwunel in section 1), and adds one more machine that can suspend (section 6).

## 3. LXC members must be on the homeserver's host

- **Claim.** An LXC guest can only be a member of a team whose homeserver runs on the same host.
- **Evidence.**
  - `lxc apply` runs as root on the homeserver host and calls the host's own `lxc-ls`/`lxc-info`/
    `lxc-attach` with no remote target (`tc/host/lxc/lxccmd/lxccmd.go:1-14`, `:37-40`).
  - It writes into the guest through the guest init's `/proc/<pid>/root` held as an fd
    (`tc/host/lxc/guestroot/root.go:1-16`, `:167-199`): this only works for a local process.
  - X5: while a team has a present LXC member its single `url` must be an address inside the local
    `lxcbr0` subnet, else `team apply` and `lxc apply` refuse it
    (`tc/declare/networks.go:54-79`, `tc/host/apply.go:432-438`, `tc/host/lxc/apply.go:405-419`;
    `play-teamchat.yml:57-58`). The guest source check requires the container's link to be the
    host bridge (`tc/host/lxc/guestfacts/guestfacts.go:268-271`).
  - Design: C:1769-1773 (3.9.1); the health tick probes LXC seats through the same root fd (C:1207-1208).
  - The units order themselves `Before=lxc.service` (`tc/unitfiles/unitfiles.go:38`, `:119-121`),
    so goodbyes at shutdown assume the containers share the host.
  - v1 `ccy-teams lxc-add` lists only this host's containers, as root through sudo
    (`src-go/internal/teams/lxcadd/adder.go:1-11`, `:46-47`, `:258`, `:357-361`).
- **Forced or choice.** Forced by code at every layer above.
- **Consequence (not in the brief).** Because every bundle carries the one team `url`
  (`tc/admin/bundles.go:43-47`, `BaseURL: team.URL`), a team with a present LXC member hands its
  bridge-subnet address to *every* seat, including a seat issued for another machine, which cannot
  route to a NAT bridge on someone else's host. A team cannot today have both LXC members and
  members on other machines. This is the defect Plan 00257's ARCHITECTURE.md row X5 has to lift.

## 4. Seats are tied to the machine (and the checkout path) they run on

- **Claim.** A pool seat lives in one user's state directory on one machine, under a key derived
  from the checkout's real path. There is no verb to move a seat.
- **Evidence.** Pool layout under `~/.local/state/teamchat/seats/pool/<checkout key>/<team>/<seat>/`
  (C:1176-1183). Key = first 32 hex of SHA-256 of the real path (`tc/paths/ids.go:36-37`,
  `tc/paths/paths.go:685-719`). Allocation lock under `$XDG_RUNTIME_DIR` (`tc/paths/paths.go:783`,
  `tc/member/placeissued/place.go:21-25`). A checkout holds a team on exactly one homeserver URL and
  server name (`tc/seat/homeserver.go:16-20`, SPEC:55-57). LXC seats live in the guest user's home
  (C:1207-1208, C:1807). Admin verbs: `member add|return|park|retire|reissue|set`, no move
  (`tc/admin/run.go:157-174`).
- **Forced or choice.** Forced.
- **Consequence.** A re-clone at another path, a renamed directory, or the same work on a second
  machine is a new pool: the seat must be reissued and placed again. Changing the team's URL (bus
  host moved, its IP literal changed) is refused by every existing checkout (`ErrHomeserverClash`)
  unless the URL is a DNS name. Placement is guarded only by the owner's uid (DC19, C:2849; K12, C:2868).

## 5. Friction of adding a seat on another machine

Exact steps today, from XN:88-127, SPEC:24-48, P246:211:

1. Give the bus host a non-default route to the member's network (VPN or static route); a network
   only the default route covers is refused (SPEC:33-35, C:449-452). **Owner, host networking.**
2. Add the member network's CIDR to the team's `networks` in host_vars (SPEC:24-28). It is a gated
   field (SPEC:27-28): approve it, by `team approve` under password sudo (C:497, C:747) or by
   listing the team in `teamchat_approve_teams` (`play-teamchat.yml:48-54`).
3. Run the play on the bus host so `team apply` re-derives listen addresses, leaf SANs, the IP
   filter and the firewall rules (SPEC:29-40, SPEC:99-102). **Host run.** Firewalld must already
   run there (`play-teamchat.yml:70-71`); the bridge still sits in `trusted` (P246:211).
4. Install `teamchat` on the member machine (the ccy image kit, P209:390-391, or a play there).
5. On a terminal: `read -rs pw` the owner's sudo password, pipe it over ssh into
   `sudo -S teamchat-admin member add ... --credential password`, and pipe the issued document into
   `[ssh <member>] teamchat seat place-issued --checkout <dir>` (XN:93-107). **Owner tty only**: the
   password is typed; no NOPASSWD rule is installed (XN:93-95, XN:110-111; C:69 rule C10). The ssh
   uses the owner's own keys and agent (XN:91-92).
6. `place-issued` needs `HOME` and `XDG_RUNTIME_DIR` on the member machine
   (`tc/member/cli/place.go:19-21`), i.e. a logged-in user session, not a bare ssh.
7. The TLS CA travels inside the bundle (SPEC:103-106), but for a human's Element or Lightning on
   that machine the CA file must be delivered by hand and named as `ca_file` (XN:177-184).
   **Owner, manual copy.**
8. If step 5 fails after `member add`, redo it with `member reissue` (XN:118-119).

Ongoing: `team rotate-ca` is a hard cut; every non-LXC seat must be re-placed by hand with step 5
(`tc/admin/rotate.go:24-34`). The second-machine run (X30) has never been run (P246:143-145,
P246:224, XN:201-210).

## 6. What breaks when a host suspends

**The homeserver host suspends.**

- Every member of every team on it loses the bus: there is no second server, and no federation
  (section 1). Members' links retry 1 s doubling to 30 s and mark the team `down` after 15 s
  (B:1402-1404, B:1231).
- Sends do not block: a send the link cannot post is answered `"queued": true` and held in the
  seat store (`tc/member/engine/send.go:428-451`, XN:138-140). A send to a team whose roster was never
  read in this run exits 20 `unreachable` instead (`tc/member/engine/send.go:219-221`, `:249-251`).
  A one-shot script's queued message is left in the outbox when its session ends unless it used
  `--wait-contact`, which waits at most 10 s (`tc/member/engine/firstcontact.go:10-13`, `:35-41`).
- New seats cannot be created (fail fast, SPEC:60-62); admin verbs, Element and the health tick
  are down with the host.
- Nothing stops the homeserver host from suspending: no sleep inhibitor or `sleep.target`
  handling in teamchat or its plays (searched `tc/`, `play-teamchat.yml`, `play-agent-bus.yml`:
  not found). The default host is the desktop (`play-teamchat.yml:72`).

**A member host suspends.**

- The member's link detects the suspend on resume and reconnects (B:1232,
  `tc/member/teamsync/teamsync.go:414`, `tc/member/teamsync/session.go:491`).
- Peers see presence go `unavailable` then `offline` by the server's timers (B:2071-2073). The
  `disconnected` notice is posted by the seat sweep of the machine the seat lives on (B:1230), so
  for a seat on a suspended machine no notice is posted until it wakes.
- Pings to it time out from the sender's outbox by `due_ms` (B:1400-1401), so it blocks nobody.
- A stopped or missing LXC container is held back and the others applied (C:1821-1826).

**Is "an offline party never blocks anything" met?** For *members*, yes, by design and code
(C:1629-1633, SPEC:60-62, send queueing above). For the *homeserver host*, no: it is the one party
everything depends on, and its absence stops every team it hosts.

## 7. Single points of failure and recovery

- One homeserver per team, no replica, no federation (section 1).
- One host-wide health process for all teams (risk K11, C:2867).
- Restore is refused in the production build until host checks H-C5 and H-C15 pass
  (`tc/restore/unproven.go:3-5`, `tc/admin/wire/wire.go:64`, P246:168-172).
- Backups are not copied off the machine (DC14 "(a) not built", C:2844). The CA key lives only on
  the bus host and in its root-only backups (SPEC:79-83).
- **Choice or forced.** Forced by the current code; DC14 and DC8 are recorded owner decisions.

## 8. Credential and trust handling

- Any agent running as the owner can change the checkout, host_vars and version pins that root
  later builds and installs (risk K9, C:2865), stated, not solved.
- A host `cc` session can read every pool seat's password (C:1192-1194; K12, C:2868).
- A released seat's previous holder keeps a copy of its token (K13, C:2869).
- The health unit holds `CAP_SYS_PTRACE` (K14, C:2870).
- Networks are not a boundary between teams on one host: every ccy container and every bridged
  guest reaches every team that admits the bridge (C:474-479, K16, C:2872).
- Every member reaches Tuwunel from `127.0.0.1` through the terminator, so Tuwunel cannot tell
  members apart by address (SPEC:127-130).
- Remote issuing pipes the owner's sudo password over ssh stdin (XN:93-100).

## 9. Size and complexity

- `src-go/internal/teamchat/`: 328 production Go files, 84,154 lines; 464 test files, 125,326
  lines; 34 packages (`git ls-tree origin/F44`). About 15% of all production Go in `src-go/`
  (557,542 lines).
- `src-go/internal/teams/` (ccy-teams, serves v1 and v2): 83 files, 23,090 lines; tests 30,817.
- v1: `helpers/agent_bus` 7,080 and `helpers/agent_bus_lxc` 2,474 lines of Python;
  `files/usr/local/sbin/agent-bus-install` 1,188 lines of bash, held by the ratchet allowlist at
  985 code lines against a limit of 60 (`scripts/bash-limits.allowlist:44`).
- Plays: `play-agent-bus.yml` 427 lines, `play-teamchat.yml` 751 lines.
- v2 design text: DESIGN-v2.md 541, FRAME 170, A 2,267, B 2,540, C 3,060, D 1,413 lines (9,991),
  plus a 526-line plan. Area C alone records 21 owner decisions (DC1-DC21, C:2831-2851) and 16
  risks (K1-K16, C:2857-2872).
- **Consequence.** The cost of any structural change (such as Plan 00257's) is paid against a
  very large, closely specified surface. Two bus systems run side by side (v1 feature-frozen,
  P246:36), and v2 must avoid v1's ports (C:431-433).

## 10. Plan and docs drift on the install state

- XN:9-13 and XN:141-142 say the v2 launcher is not built and nothing runs live; P246:216-218 says
  stages B and C are not built. P209:390-400 marks Task 4.1 (kit in ccy image) and Task 4.2 (the
  launcher) done. At least one of these is stale. P246 and XN were last changed on F44 2026-11-15.
- **Consequence.** A reader cannot tell from the docs what is deployable today.
