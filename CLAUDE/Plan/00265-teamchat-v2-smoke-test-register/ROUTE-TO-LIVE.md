# Plan 00265: from fixes on `v2-smoke` to running seats

Two parts. First the burndown for the owner's top priority, LXC members joining a v2 team (#425,
R22, R23). Then the route every other fix takes to the seats. "Checked" means read in this
repository's code or seen in a run; anything else is marked unchecked.

## 1. LXC members on v2: burndown

| #   | Step                                                                                                                                                                                 | State                                                                              | Who                                                    | Blocked by               |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- | ------------------------------------------------------ | ------------------------ |
| L1  | R23: `teamchat run --kind lxc --key` reads the key. Without it no v2 LXC session starts, on the homeserver's own host either.                                                        | Fixed and tested (U8, `90090205e`); on `ci/r23-lxc-key`, CI running; then F44.     | fedora-desktop seat                                    | CI                       |
| L2  | A seat bundle's url follows who places it: a person's `member add` gets the team `url` (a name); only the play's lxc apply gets the bridge address.                                  | Built and tested (U8, `bd79402b5`); not merged.                                    | fedora-desktop seat                                    | merge, then L1's route   |
| L3  | `teamchat seat place-lxc`: the container places the issued seat itself (keyed layout, one-seat desired membership).                                                                  | Built and tested (U8, `514ab0b85`); not merged.                                    | fedora-desktop seat                                    | merge                    |
| L4  | Docs: the off-host route, the hand-run launch line, the container-launcher contract (A3), sign-in (A2), the uid rule K15.                                                            | Written (U8); not merged.                                                          | fedora-desktop seat                                    | merge                    |
| L5  | Deploy: the new teamchat kit on the homeserver host (`play-teamchat.yml`).                                                                                                           | Not started.                                                                       | devops seat, by `meta-deploy.bash`                     | L1-L3 on F44             |
| L6  | The kit inside each container (`/usr/local/lib/teamchat/kit/<digest>/`, `current`, `/usr/local/bin/teamchat`), built for the container's architecture.                               | Not started. Today: the container's own provisioner copies it (owner question O1). | the container team's repository                        | L5 (a kit to copy); O1   |
| L7  | ~~A dedicated agent user whose uid is no host account's (K15).~~ Not needed (F22, R48): off-host nothing reads a host's accounts; the container's existing login, not root, will do. | Dropped. `seat place-lxc` accepts the login's other groups (R48).                  | -                                                      | -                        |
| L8  | The container's launcher wraps `teamchat run --claude --kind lxc --key <key> -- <its wrapper> claude ...` and stops looking for the v1 kit (its exit 78 today).                      | Not started. The contract is pinned by a test (A3).                                | the container team's repository                        | nothing (contract known) |
| L9  | The team's `url` is a name the containers' VM resolves, and its TLS terminator is reachable from there (`networks`, firewall).                                                       | Unchecked.                                                                         | the infrastructure inventory                           | nothing                  |
| L10 | Issue and place each seat: `teamchat-admin member add ... --kind lxc --credential token` on the homeserver host, piped by ssh into `teamchat seat place-lxc` in the container.       | Not started.                                                                       | the owner (sudo on the homeserver host) or devops seat | L5-L9; O4                |
| L11 | Live acceptance (below, A1-A5).                                                                                                                                                      | Not started.                                                                       | devops seat                                            | L10                      |

L6, L7, L8 and L9 can start now, in parallel with L1-L5.

### Quickest route to one working container seat

| Route                                                          | New, unproven pieces                                                                                                     | Steps                       | Cost                                                                                                                                               |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Other-host (Plan 00257 B.4, as built by U8)                    | L2 and L3 (tested, never run live); the kit copied into the container by hand (L6); the name and the reach (L9).         | L1-L11                      | None on the team. It is the final shape.                                                                                                           |
| Same-host: move that team's homeserver onto the containers' VM | Only L1 (one line, tested). The play's own lxc apply issues, places and installs the units, so L3, L6 and L10 fall away. | L1, L5, L7, L8, move, apply | Every existing seat of that team is reissued after the move. That VM's suspend takes the whole team down. A second move later, to the final shape. |

The same-host route has fewer untried pieces, and the play does the placement. If the team is
used mostly by those containers and the suspend is acceptable for now, it is the quicker way to
one working seat. Otherwise, the other-host route avoids a second move. Both need L1 first. The
owner's call; my recommendation is to do L6-L9 now (they are needed either way, except L6) and
decide at L5.

### Owner questions (plain words)

- **O1.** Who puts the teamchat kit into a container on another machine: the container team's own
  setup copies it (works today), a play of this repository installs it from the host, or a
  released kit archive?
- **O2.** After a team's certificate rotation, the reissue list leaves out off-host LXC seats. May
  this be fixed now? It is small.
- **O3.** Is a code-analysis rule worth building for "a launch flag the code accepts but never
  uses" (how R23 happened), or is the test enough?
- **O4.** An off-host LXC seat's token travels in the ssh pipe from the homeserver host to the
  container, as a pool seat's password does. Is that acceptable for a container seat?

### Needed now from the other repositories, in parallel

- **The container team's repository:** a dedicated agent user (L7); the launcher change (L8); a
  plan to copy the kit (L6, pending O1).
- **The infrastructure agents:** confirm the team url resolves and the terminator is reachable from the containers'
  VM (L9); if the owner picks the same-host route, the declaration move.

## 2. Every other fix: merge, build, restart

### Batches to F44

Each batch goes `ci/<topic>` → CI green → F44, as the protected-branch flow requires.

1. **Now:** R23 alone (`ci/r23-lxc-key`).
2. **Next, as each lands:** U2 (R1, R2, R21: workers receive their coordinator's messages) and
   U8's L2-L4. Neither waits for the rest.
3. **Then:** U4 (#417, a seat resumes its conversation) before any restart is asked for, because
   today a restart of a team seat can start it fresh.
4. **Then the rest:** U1, U3, U5, U6, U7, as they land.

### How a machine gets the build

- **ccy seats** (podman containers): teamchat is built into the ccy image from the commit that
  last changed its sources (`files/var/local/claude-yolo/Dockerfile`, `image-source-paths`).
  `play-claude-yolo.yml` rebuilds the image. A running session keeps the old image until it is
  restarted. Checked in the Dockerfile; whether the play rebuilds without a CCY version bump is
  unchecked, and CI on the first batch will say.
- **The homeserver host and LXC members on it:** `play-teamchat.yml` assembles and deploys the kit;
  lxc apply copies it into same-host containers.
- **The home dev VMs:** the nightly self-update (03:30, up to 30 minutes late, per the devops
  seat) or a play the devops seat runs sooner. The exact plays for each machine are queued in
  `CLAUDE/Plan/meta-deploy.list` with each batch, and run by `./CLAUDE/Plan/meta-deploy.bash`.

### What needs a restart

Every teamchat fix takes effect only when the session's link restarts. For a ccy seat that means
restarting the session on the new image. A restart of a team seat today can lose its conversation
(#417, R4) unless the conversation id is named by hand, so: restarts only after U4 lands, or each
by the devops seat's gated route (dry run must show `--resume <id>`).

### Live acceptance, in one pass

| Fix           | Check                                                                                                   | Accept                                                      |
| ------------- | ------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| R1 (U2)       | Coordinator `say --to <worker>` to a worker issued without human text.                                  | The worker's session shows the delivery.                    |
| R10 (U6)      | A human edits a questions post; the coordinator replies to the edit's id.                               | Sent, in the original post's thread.                        |
| R12 (U6)      | A worker runs `teamchat verify '<id>' --room questions` on a human's questions post.                    | `verified`, kind human, same text.                          |
| R14 (U7)      | `teamchat status` on seat A while B runs; then B ended cleanly; then B's `teamchat run` killed with -9. | online; offline within seconds; unavailable by about 150 s. |
| R23, R22 (U8) | Section 1, L11: the five steps of `reports/U8.md` "Live acceptance".                                    | As written there.                                           |

Rows are added here as each unit lands.
