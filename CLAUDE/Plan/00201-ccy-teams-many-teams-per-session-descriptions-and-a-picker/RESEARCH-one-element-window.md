# Research: one Element window for every team?

The owner's question: does a human need a separate Element window per team, or can one
window show every team they belong to, as Slack shows several workspaces? This note says
what was built and why, what Matrix and Element allow, and the options. No code changes.

## 1. What was built

| Thing         | Per team or shared                                                                                                                                                                                                                                                                                                                                                                                                    | Where it is set                                                                                                                                                                         |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Homeserver    | **One Tuwunel instance per team**, its own unit `agent-bus-hs@<team>`, port, `server_name`, database, secrets, admin and backups                                                                                                                                                                                                                                                                                      | 00161 DESIGN.md §1, §2 "One instance per team", **D3**                                                                                                                                  |
| Rooms         | **One invite-only, unencrypted room per team**, the only room the instance allows (`allow_room_creation = false`)                                                                                                                                                                                                                                                                                                     | DESIGN.md §1, **D63**; `helpers/agent_bus/render.py`                                                                                                                                    |
| Federation    | **Off** on every instance (`allow_federation = false`, `trusted_servers = []`); the unit's IP filter lets it talk only to loopback, its listen addresses and the team's `allow_from`                                                                                                                                                                                                                                  | `helpers/agent_bus/render.py`; DESIGN.md §3.3                                                                                                                                           |
| Human account | **One account per human per team** (power 50, never admin, password set and printed once by `agent-bus human password <team> <name>`)                                                                                                                                                                                                                                                                                 | DESIGN.md §2 "Humans", **D25**; docs/agent-bus.md "The human's account"                                                                                                                 |
| Element       | **One Element Desktop profile per team**: `~/.var/app/im.riot.Riot/config/Element-<team>/config.json` pins that team's homeserver (`default_server_config`, `disable_custom_urls`, no well-known, no identity server, integrations, Jitsi, analytics, room directory beyond the team's server), and a launcher runs `flatpak run im.riot.Riot --profile <team>`. Each profile is its own Electron instance and window | `playbooks/imports/optional/common/play-agent-bus-element.yml`; `helpers/agent_bus/element.py`; DESIGN.md §8; `ccy-teams` Ctrl-E (`src-go/internal/teams/login/login.go` `OpenElement`) |

So a human on three teams has three homeservers, three accounts, three Element profiles and
three windows (GNOME groups them under one dock icon, since they share a WM class).

## 2. Why it was built that way

- **D3**: one instance and one room per team "keeps a team's accounts, admin, backup and
  placement self-contained", so a team can move host without touching another (DESIGN.md §2).
  Placement matters for the later phase (D46): a team with a member on a server runs its
  homeserver beside that member.
- **Federation off** is part of the threat model (DESIGN.md §0, §3.3): the homeserver
  talks to nothing outside its team's sources, has no resolver, and P1 fails on any Tuwunel
  socket on a port other than its own.
- **Network scoping is per team**: each instance listens only on its team's addresses and
  admits only that team's `allow_from` (firewalld plus the unit's IP filter).
- **Per-team trust**: each homeserver's team record (written only by that team's `admin`)
  names the team's humans, roles and trusted org or repositories (D64); agents act on human
  text only from humans that record names.
- The Element profile per team is a **consequence** of the separate homeservers and
  accounts, not a separate decision: Element Desktop holds one logged-in account per
  profile, and the profile config pins one homeserver (`disable_custom_urls`). No decision
  records "one window per team" as a goal in itself. D17 (Element unconfined, P4 proves its
  traffic) and D26 (no agent runs as a user holding a human's Matrix session) apply to
  every profile alike.

## 3. Matrix and Element, plainly

- A Matrix **account** lives on one homeserver. It sees the rooms it has joined: rooms on
  its own homeserver, and rooms on other homeservers **only through federation**.
- A **Space** is a room that lists other rooms, shown in Element as a sidebar icon, much
  like a Slack workspace. Spaces group rooms **that one account can join**; they cannot
  group rooms of different accounts.
- **Element Web and Element Desktop hold one logged-in account per instance.** Several
  accounts in one window is a long-standing feature request, not built. `--profile <name>`
  is Element Desktop's answer: a separate data directory, a separate login, a separate
  window. That is what the play uses.
- **Element X** (the newer mobile client) is the one Element client with some multi-account
  work; it is not the desktop app the play installs. VERIFY its state if the phone phase
  (D47) is resumed.
- **Other Matrix clients do support several accounts in one window**, with an account
  switcher: Fractal (GNOME, Flathub `org.gnome.Fractal`) and NeoChat (KDE, Flathub
  `org.kde.neochat`) at least; FluffyChat too. In Fractal and NeoChat one account's rooms
  are shown at a time and the switcher changes account, which is close to Slack's
  workspace switcher. VERIFY each on the host before relying on it (version, plain-HTTP
  homeserver, no well-known lookup when the URL is typed, what it sends where).

## 4. Options

### A. Keep the design; make the windows easier (status quo plus)

- **Changes:** nothing on the server side. Possible small additions: one "Agent teams"
  launcher or a `ccy-teams` action that opens every team's profile at once; a clear note
  in docs/agent-bus.md that each team is its own window, like a separate Slack sign-in.
- **Isolation cost:** none.
- **Effort:** small.
- **Gives the owner:** still one window per team, but one action opens them all and the
  dock groups them.

### B. One shared homeserver, a room (and Space) per team, one human account

- **Changes:** reverses D3. One instance holds every team: one `admin`, one database, one
  backup, one listen and `allow_from` set (the union of every team's networks). The team
  record moves from per-homeserver to per-room; pingbus, the protocol's addressing, the
  installer, backups and restore, `remove`, the applied-state stamps (D65) and every
  acceptance check assume one team per homeserver and would all change. Agents would stay
  members of only their team's room, so the server would still stop an agent reading
  another team's room.
- **Isolation cost:** high. Every agent account is on one server with every other team's
  agents (the user directory shows them, and a later bug or misconfiguration in room
  membership crosses teams). One root of trust for every team: whoever holds the shared
  backup or the host can instruct every agent of every team. A network admitted for one
  team (an LXC bridge, a WireGuard peer) reaches the homeserver of all of them. A team can
  no longer move host on its own (the later phase, D46).
- **Effort:** very large: a redesign of Plan 00161's core.
- **Not recommended.**

### C. Keep per-team homeservers, federate them, one human account on a "home" homeserver

- **Changes:** federation on; each team homeserver accepts the human's account from the
  home homeserver (the team record names a full MXID on another server); the human joins
  every team room, grouped in a Space, from one Element profile.
- **Isolation cost:** high. Homeservers would open connections to each other, which the
  unit filter and P1 forbid today; federation expects TLS with certificates (v1 has none,
  U26 is conditional); each team room's full history is copied to the human's home
  server; the home server's root and backups become able to post as the human in every
  team (the D26 concern, moved to a new place); trusted-server and key fetching needs a
  resolver the units do not have.
- **Effort:** large: TLS, federation config, a third kind of homeserver, new probes and
  acceptance checks.
- **Not recommended.**

### D. Keep the server design; use a multi-account Matrix client

- **Changes:** the play also installs (or installs instead) a client with an account
  switcher, such as Fractal; the human signs in to each team's account in it. Nothing on
  the homeservers changes; accounts, passwords and `ccy-teams` sign-in stay as they are.
  `ccy-teams` "Open in Element" would gain or switch to "Open in Fractal".
- **Isolation cost:** low. Each team keeps its own homeserver, account and token, as now.
  The tokens are in the user's home, as Element's are, so D26 and the `config check`
  refusal must also cover the new client's data directory (the refusal checks Element's
  folder today, `config.ELEMENT_FLATPAK_DIR`). What is lost is the play-written lock-down:
  Fractal and NeoChat have no `config.json` like Element's to pin the homeserver and turn
  off the identity server, integrations and the rest, so the human types the homeserver
  URL and a P4-style traffic capture must prove the client calls only that address.
- **Effort:** medium: a probe of the client (traffic, plain HTTP, well-known behaviour,
  token storage path), a play change, the D26 refusal extended, `ccy-teams` wording, docs.
- **Gives the owner:** one window, a switcher between teams, much like Slack.

### E. Element Desktop's `--profile` alone

This is what is built (option A without additions). It cannot put two accounts in one
window.

## 5. Recommendation

1. **Keep one homeserver and one human account per team** (D3, federation off, per-team
   network scoping); options B and C undo the design's main isolation for a convenience.
2. **Now:** option A, a one-step "open every team" launcher and a sentence in
   docs/agent-bus.md, so the owner knows the window per team is Element's limit, not a
   bus rule.
3. **Next, if the owner wants one window:** option D, starting with a probe of Fractal (and
   NeoChat as the fallback) against a team homeserver, recording its traffic, its token
   location and its account switcher, before any play change; the D26 refusal is extended
   to that client's data directory in the same change.
