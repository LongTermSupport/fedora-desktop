# Plan 00161 design revision 2: the owner's answers applied

Inputs: the owner's answers (journal 26-10-06, 13:35 and 13:50), the external spike's
evidence (anonymised), the wave-1 branches, and the earlier research and reviews. Edited:
`DESIGN.md` and `PROTOCOL.md`, rewritten in place; superseded text was deleted, not kept.
Nothing committed.

## What changed, by owner answer

| Answer                                                                                | Change                                                                                                                                                                                                                                                                                                                                                                     | Where                                |
| ------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| 1. Themed, multi-member, multi-team teams                                             | A team = one homeserver instance + one team room; humans declare it in a team file. An agent in several teams holds one bundle per team under `PINGBUS_HOME`, lists active ones in `PINGBUS_TEAMS`; output lines carry the team. Work-item room pairs are gone.                                                                                                            | DESIGN §2; PROTOCOL §12, §15         |
| 2. Member = repo + host + encapsulation; all first-class                              | One bundle shape for every type; a per-type table (ccy, bare desktop, server, LXC, docker, VM): where the bundle goes, how opt-in is expressed, how the homeserver is reached, what `allow_from` needs. Old D7 deferral deleted.                                                                                                                                           | DESIGN §5                            |
| 3. Human free text to addressed agents                                                | Humans and agents share the team room. A listed human's `m.text` that mentions an agent (or `@room`) is delivered as a `HUMAN` line with the human's name; everything else from humans is ignored or dropped. Control rooms, the warden, `!` commands, `on_behalf_of` and the note are deleted.                                                                            | DESIGN §7, D12, D14; PROTOCOL §7, §9 |
| 4. Mixed placement; play + standalone installer, Fedora only                          | `agent-bus-install` (Bash, root, non-interactive) is the one implementation; the core play calls it on every desktop; other projects run it from a pinned clone. Instances exist only where a team file declares one. Placement convention documented.                                                                                                                     | DESIGN §3.1, §3.2, §3.4              |
| 5. Routable address; private network is the access control; human account locked down | Loopback-only and the podman internal network are gone. Listen on loopback, a host `agentbus0` dummy address, and WireGuard addresses only; `allow_from` limited to in-kernel and WireGuard paths; enforced by firewalld and, independently, the unit's IP filter. TLS: none in v1, because every allowed path is in-kernel or WireGuard. Human lock-down list.            | DESIGN §3.3, §4, §9                  |
| 6. Prefix clearly not a domain name                                                   | `agent_bus` (spec: Java-package naming is a SHOULD; the Common Namespaced Identifier Grammar allows it; `_` cannot appear in a host name or TLD; Tuwunel accepts any type string, H4 confirms).                                                                                                                                                                            | PROTOCOL §2                          |
| 7. Test VM counts; handle names a seat                                                | P2 uses the VM; a bundle is a seat (one per checkout for ccy), enforced by the sync lock.                                                                                                                                                                                                                                                                                  | DESIGN §5.2, §10                     |
| 8. Role and membership in `ccy.env.local`                                             | ccy opt-in is `PINGBUS_TEAMS` (and `HOOKS_DAEMON_HOSTNAME`) in the untracked `ccy.env.local`; the entrypoint refuses to start on a broken bundle and adds `--plugin-dir`/`--settings`. No launcher flag, no host-side store, no deny-list or network change. The handle's `<host>` is the role. The dist template is ccy's own (`ccy_env_local_dist_text`); see "Applied". | DESIGN §5.3, D18, D19                |

## Other structural changes and why

- **Homeserver as a static binary under a hardened system unit, as the `agent-bus` user**
  (D1, D8). The kernel's per-unit IP filter (which the user manager cannot apply) gives "no
  egress" and "only allowed sources" by construction; no container engine on servers; the
  mode the spike proved. The team's secrets now belong to a user no agent runs as, which
  removes the old "host member trusted like a human" caveat except where an agent can
  `sudo` without a password.
- **Pings are `m.notice` with a structured `agent_bus.ping` key and a body that must equal
  its rendering** (D11). Humans now share the room, and custom event types are invisible
  in Element (and on phones), so a notice is the only way humans see pings without a relay.
  Trade-off, stated plainly in DESIGN §7 and §9: the server can no longer stop an agent
  posting free text (power levels are per event type); every receiver drops it, and P6
  proves that with raw `curl` posts.
- **The warden is gone** (D14). Its two jobs were translating human commands (humans now
  address agents directly) and mirroring pings into control rooms (pings are now visible
  notices). Nothing is left for it.
- **The team record in room state** (`agent_bus.team`, written only by `admin`, the room's
  v12 creator) holds humans, roles and allowlists. One place for humans to change
  membership; member bundles carry no allowlist, so code in a member's checkout cannot widen
  what its agent accepts. The steward account merged into `admin`.
- **Wake mechanism weighed against the spike's inbox socket** (D16). Chosen: the socket as
  primary, driven by a `pingbus watch` that the plugin's SessionStart hook starts (it
  inherits the socket variables), sending a counts-only template; `pingbus wait` as the
  fallback where the socket is absent; the Stop hook as the guard. Reasons: wakes an idle
  session with no agent discipline, available now (the supervisor-template route needs an
  upstream release, so it is dropped), works under every launcher we control. Costs
  recorded: `--settings` only (project settings not honoured), admits any same-user process
  (no new boundary: such a process can already edit settings and hooks), a recent
  mechanism, so U01 pins it per Claude Code version and `status` shows the live path.
- **Plugin route decided by the wave-1 U01 result**: the phpantom-lsp copy registers no
  hooks; `--plugin-dir` does. Plugins now load by `--plugin-dir`; the settings-merge
  fallback is no longer needed.
- **`recv` re-fetches every item from the homeserver before printing** (the inbox is a
  cache), now without needing the sync lock, because a watcher usually holds it.
- **Unit list rebuilt**: 28 units (U00-U27) in five milestones: M0 probes, M1 host-to-host
  (now through the real installer, with a human message), M2 ccy members woken by the
  socket, M3 other encapsulations and another host (LXC, docker, VM; the standalone
  installer in a test VM; a WireGuard leg), M4 Element, deploy and acceptance (P1-P7 plus a
  backup/restore round trip). Probes: H1-H2 reachability from containers and guests, H3 the
  binary under the sandbox, H4 every API flow incl. the new event shapes, H5 backup and
  restore, H6 Element desktop, H7 the owner's phone.

## Wave-1 branches

- `wf-f0f65b6e-87f-2-30211d13` (U01): reused as U01's base; its result already decides the
  install route; extended with Stop/SessionEnd and the inbox-socket legs.
- `wf-f0f65b6e-87f-3-061c2adc` (U02): reused as U02's base. Stands: reference grammar and
  forms, handle and ID grammars, verb table shape, `Refusal`, the content checks, the
  table-driven tests, the doc-equals-constants test, the `link_check.py` code-span fix.
  Replaced: the namespace, event shape, room/control/roles markers, note, `on_behalf_of`,
  the warden sender class; the doc path becomes `docs/agent-bus-protocol.md`.
- `wf-f0f65b6e-87f-1-be409213` (U00 test only): MAC, scrubber, log-scan and pcap tests
  reused; namespace, subnet-picker, room-pair power-level and old `tuwunel.toml` tests
  dropped or re-pointed.

## Follow-ups outside these two files

- `PLAN.md` Phase 2 lists the old units (U00-U30, M3 "warden and control room"); it needs
  the new milestones from DESIGN §12. Its success criterion "no agent can send free text"
  should read "no agent's free text reaches another agent" (the server cannot block it;
  receivers drop it).
- The `ccy.env.local.dist` placeholder: ccy owns the dist (Plan 00160 Task 3.2); U19 adds
  the `PINGBUS_TEAMS` block there (see "Applied" below).

## Owner question left

One: whether an agent may answer a human in text (addressed to humans only, dropped by
every agent). The design defaults to no: agents answer with `ack`/`nack`/`done`/`blocked`
addressed to the human. Nothing in M0 or M1 waits on it.

## Applied: design review 2 (security, feasibility, scope)

Reviews: `261006-design-review-2-{security,feasibility,scope}-opus-5-5.md`. Every blocker
and every finding was accepted; none rejected. Where they landed:

| Finding                                                       | Change                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Sec B1: Element's token readable by same-user agents          | No agent runs as a user holding a human's session; bare-desktop members use a dedicated user; `config check`, `agent-bus-claude` and the Element play refuse; P8 (DESIGN §8, §9, D26)                                                                                                            |
| Sec B2: root and backups can instruct remote agents           | Human passwords set and printed once, never stored or backed up; `human_text: false` (`--no-human-text`); §0, §9 and the docs state what joining grants (D25, D27)                                                                                                                               |
| Sec B3, Feas B2, Scope B1: dist owned by ccy, #88 withdrawn   | U19 edits `ccy_env_local_dist_text`, raises the dist version, bumps `CCY_VERSION`, extends `test-ccy-env-local-dist.bash`; depends on Plan 00160 Task 3.3 (DESIGN §5.3, D18)                                                                                                                     |
| Feas B1: identical socket messages dropped                    | Monotonic notice number in the template; U01 measures the dedupe window (DESIGN §6, PROTOCOL §15)                                                                                                                                                                                                |
| Feas B3: unit gives up when WireGuard is late                 | `RestartSec=5s`, `StartLimitIntervalSec=0`, `After=`/`Wants=` on each listen interface's device unit (§3.5)                                                                                                                                                                                      |
| Feas B4: `agent-bus` cannot write into a home                 | Bundle is a tar on the `agent-bus` side's stdout; the root wrapper places it with `install -o "$SUDO_UID"` (§5.1)                                                                                                                                                                                |
| Feas B5, Scope B2, Sec F7: PLAN.md stale                      | Phase 2 now lists M0-M4 over U00-U28; success criterion reworded; Task 1.2's control room marked superseded                                                                                                                                                                                      |
| Scope B3: docker leg skipped silently                         | Every LXC, docker, VM and WireGuard leg reports SKIPPED-NEEDS-OWNER and blocks close (D28)                                                                                                                                                                                                       |
| Sec F1: hostname leak through handles                         | `<host>` is `HOOKS_DAEMON_HOSTNAME` or an explicit `--host`; never `CCY_HOST_HOSTNAME` (also the real name) or the hostname (D19, PROTOCOL §3)                                                                                                                                                   |
| Sec F2: reply fallback quotes agent text                      | Leading `>` block removed when `m.in_reply_to` is present; P6 case (PROTOCOL §7, §9)                                                                                                                                                                                                             |
| Sec F3, Feas §7 wording: `@room` claim                        | Corrected: the server does not limit it; the sender check does (DESIGN §4, §7; PROTOCOL §7)                                                                                                                                                                                                      |
| Sec F4: "no egress" overstated                                | Stated that `allow_from` hosts are reachable outward; P1 fails on any Tuwunel socket not on `<port>` (§3.3, §9, P1)                                                                                                                                                                              |
| Sec F5: checks under-prove                                    | P6 retitled and extended (agent `@room`, reply quote, forged ping, removed human, `human_text: false`); P7 extended; P8 added                                                                                                                                                                    |
| Sec F6: status under any state key                            | Receivers read a status only under its sender's key, 256-byte cap (PROTOCOL §8)                                                                                                                                                                                                                  |
| Feas probes: `+` handles, unknown keys, resolver, U01, H7, H1 | H4 creates a real `+` handle (fallback `=`), records the unknown-key warning (U16 fails on it); resolver via `TemporaryFileSystem` on Workstation and Server; U01 with a logged-in child; H7 records `m.mentions` and user-CA trust; H1 adds named networks                                      |
| Feas: watcher liveness, several teams, installer idempotency  | `flock` liveness, no PID files; one long-poll thread per team; restart only on a changed render, no download when the hash matches                                                                                                                                                               |
| Feas, Scope 6: U02 branch                                     | Not merged; take `protocol.py`, tests and the `link_check.py` fix; write `docs/agent-bus-protocol.md` afresh                                                                                                                                                                                     |
| Scope 1-4, 7-9                                                | U17 (M1) needs the installer, not the play (play moved to U22, M3); U24 split into U23/U24 with WireGuard cleanup; TLS its own conditional unit U26; `rotate-admin`, `show`, `peers`, `tail` deferred and `status` folded into U12; `registry.json` placed; §11 corrected; `--syntax-check` gate |
| Scope 5: LAN refusal narrows answer 5                         | Raised as owner question 2 rather than decided silently                                                                                                                                                                                                                                          |

Owner questions now: 1 (agent text to humans), 2 (LAN without TLS), 3 (Plan 00160 Task 3.3,
which U19 needs). First units: U00, U01, U02 in parallel.
