# Report: the agent team bus, zoomed out (owner, Q44)

The owner asked to "zoom out and look at the architectural weaknesses implied in this teamchat
system", and whether projects among his GitHub stars already solve this so we can crib from them.
This is the report. It changes no code. The evidence is in `research/`:

- [weaknesses.md](research/weaknesses.md): each weakness cited against F44.
- [prior-art-protocols.md](research/prior-art-protocols.md): A2A, agentgateway, substrate,
  wireproxy, keybox, holvi, bifrost.
- [prior-art-orchestrators.md](research/prior-art-orchestrators.md): multica, openrig,
  cli-agent-orchestrator, agent-orchestrator, orgtree, open-managed-agents.
- [prior-art-runtimes.md](research/prior-art-runtimes.md): teamclaude, dmux, herdr, emdash,
  hotseat, orca, Codeman, agent-of-empires, clarp, shpool.

[ARCHITECTURE.md](ARCHITECTURE.md) is the earlier, narrower note (per-seat placement against a
per-host proxy). This report supersedes its recommendation.

## 1. Weaknesses

Each item says whether the code forces it or it is only a deployment choice. Details and
citations are in weaknesses.md.

| #   | Weakness                                                                              | Forced?    | What it costs                                                                                                                                     |
| --- | ------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| W1  | One homeserver (Tuwunel) per team, with no federation                                 | Code (DC1) | 100-135 MB per team; per team also two units, a port, a CA and firewall rules; N Element profiles for N teams                                     |
| W2  | A bus VM per team                                                                     | Choice     | Nothing in the code requires it, and one host serves any number of teams. It is an extra machine that can suspend.                                |
| W3  | LXC members only on the homeserver's host                                             | Code       | `lxc apply` works through local root-fd writes and lxc-attach, and X5 ties the url to the local bridge                                            |
| W4  | **A team cannot mix LXC members and seats on other machines**                         | Code       | See section 2                                                                                                                                     |
| W5  | A seat is tied to one machine and the real path of one checkout                       | Code       | No move verb. A re-clone or a second machine means reissue and re-place.                                                                          |
| W6  | Adding a seat on another machine takes 6-8 steps, two of them at the owner's terminal | Code       | Route, CIDR, approve, play run, install, then `sudo -S` over ssh piped into `place-issued`, and the CA copied by hand for Element                 |
| W7  | The homeserver host is the one party everything waits on                              | Code       | Members queue and nobody blocks, but all of its teams stop while it sleeps. Nothing stops the default host, the desktop, from suspending.         |
| W8  | Size                                                                                  | Code       | 84k lines of Go in teamchat plus 23k in ccy-teams, about 10k lines of design, 21 owner decisions and 16 risks in area C alone. v1 runs alongside. |
| W9  | Docs disagree on what is deployable                                                   | Docs       | Plan 00209 marks the launcher done; Plan 00246 and the cross-network doc say it is not built                                                      |

## 2. The coordinator's B1: confirmed

B1 says that with an LXC member present, the team url must be an IP on the host's LXC bridge,
so a team cannot also hold seats on another machine. **That is right.**

- `declare.Input.BridgeURLProblem` (`src-go/internal/teamchat/declare/networks.go:54-79`)
  refuses a team url that is a name, or an address outside the bridge subnet, while any LXC
  member is present. `team apply` and `lxc apply` both enforce it.
- Every seat's bundle takes that one url as its base (`admin/bundles.go:46`,
  `BaseURL: team.URL`). A seat on another machine is therefore handed a NAT bridge address it
  cannot route to.

**Smallest fix.** Give LXC seats the bridge address, and leave `url` for every other seat.

- The homeserver already listens on the bridge while an LXC member is present
  (`networks.go:42-49`).
- The TLS leaf already lists every listen address (`host/leaf.go:152`, `pki/leaf.go:77`), so
  `https://<bridge address>:<port>` verifies against the pinned CA with no certificate change.

The change, in four parts:

1. Remove X5's refusal of `url`. `url` may then be a name, or any address the remote seats use.
2. For a seat of kind lxc, `Bundle` sets `BaseURL` to the bridge listen address and the team's
   port, and uses `url` for every other kind.
3. `lxc apply`'s guest reach check uses the same per-kind address.
4. Tests: one team holds both seat kinds and each bundle carries its own reachable url.

This touches `declare`, `admin/bundles.go` and the LXC reach check. Nothing changes on the wire.

B2 to B5 are about the estate's IaC and its pinned teamchat version, not this repository's code.

## 3. Prior art: what it answers

None of the 23 projects replaces teamchat outright. None combines several machines,
Claude Code sessions as members, humans in a chat client, and per-seat credentials under a
licence we can copy from. Several have solved our weak spots in small, copyable ways:

| Idea                                                                                                                                                                              | Seen in (licence)                                                                         | Answers                                                                    |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| **One-time join code**: the admin mints a short-lived, single-use code bound to one seat; the new machine redeems it over TLS for its own credential. No ssh, no sudo, no piping. | openrig (Apache-2.0), Orca (MIT), Key-Box (ideas only), teamclaude device-code flow (MIT) | W6, W3, W5                                                                 |
| **Outward-only members**: a member only dials out, and the server never reaches into it                                                                                           | multica (ideas only), orgtree (MIT), openrig                                              | W3: an LXC guest anywhere joins like any client, with no root-fd placement |
| **A seat is an address plus a secret, not a host**: the host is an attribute, and moving is a re-enrol                                                                            | openrig, orgtree, Substrate (Apache-2.0)                                                  | W5                                                                         |
| **The CA fingerprint pinned in the join code**, or a publicly trusted certificate on the relay                                                                                    | orgtree, Key-Box                                                                          | W6's CA-copy step                                                          |
| **The member as a systemd user unit** that comes back after a reboot or login                                                                                                     | multica                                                                                   | Supervision of remote and LXC seats without the host's root-fd unit        |
| **Suspend as a member state**: reconnect with capped backoff, stale state shown dimmed, deadlines judged on wake                                                                  | herdr (Apache-2.0), Substrate                                                             | W7, on the member side                                                     |
| **An instance id held with the credential**, so a cloned image must re-pair                                                                                                       | Orca                                                                                      | Cloned LXC and VM images                                                   |
| **Idle-only, confirmed delivery** into a Claude Code terminal                                                                                                                     | cli-agent-orchestrator (Apache-2.0), Codeman (MIT)                                        | Checking teamchat's wake path                                              |

Licences:

- The Apache-2.0, MIT and ISC projects can be copied with attribution.
- multica's licence carries extra conditions, and Key-Box has no licence, so take ideas only.
- open-managed-agents is AGPL-3.0: do not copy its code.

## 4. Recommendation: adapt (keep v2's core, change how seats join)

**Not replace.** Nothing in the stars does the whole job. A rewrite would throw away a working
Matrix core with TLS and per-seat tokens. openrig is the closest whole system (Apache-2.0), and
is worth one evaluation spike if the owner wants a replacement weighed.

**Not keep as is.** Every gap the owner hit (W3, W4, W6) comes from one root process on the bus
host doing both the issuing and the placing.

**The smallest path to adding seats freely between machines, LXC guests included:**

1. **Lift B1** (section 2). This is small Go. One team can then hold LXC members on its own host
   and seats on other machines.
2. **Join codes.**
   - On the bus host, `teamchat-admin member invite --team T [--seat N]` prints a single-use
     code that expires in minutes. The code carries the team url, the CA fingerprint and a
     one-time secret. Only the owner can mint one: root, with password sudo.
   - On any machine (a host, a ccy container, an LXC guest on any host), `teamchat seat join <code>` redeems it over the team's TLS endpoint, pinning the CA from the code. It then
     places the seat in the local pool as `place-issued` does today.
   - This replaces steps 5 to 7 of W6 and needs no ssh from the bus host.
3. **LXC on another host becomes an ordinary client.** Inside the guest, the `teamchat` static
   binary is copied in, and the seat joins with a code and runs as a systemd user unit. That host
   needs no root-fd placement, no `lxc apply` and no X5. The `lxc apply` path stays as it is for
   the bus host's own containers.
4. **The network stays the owner's** (direction 7). Admit each host's routed network as a CIDR.
   NAT guests appear as their host's address, so there is one admit per host, not per seat.
5. **Host the bus on an always-on machine** (the VM the owner named), not the desktop. That is
   the cheapest answer to W7. W1 (a server per team) can stay until memory actually hurts.

Order: step 1 is independent and unblocks the estate's mixed team at once. Steps 2 and 3 are the
owner's "per-container step like v1's", generalised to any machine. Each goes test-first, with
a spec, under Plan 00257 Phase 1-2 once the owner says yes.
