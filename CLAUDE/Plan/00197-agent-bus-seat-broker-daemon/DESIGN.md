# Plan 00197 design: the agent bus seat broker

The broker is a host daemon beside a team's homeserver. A client anywhere a team admits asks
it for a seat; the owner approves; only then is the seat minted, and only the requester can
collect it. This document is the design for Phases 2 and 3; owner decisions are in section
13, the task list in section 14 and the risks in section 15.

Addresses below are RFC 5737 examples. `<team>`, `<repo>`, `<container>` and `<user>` are
placeholders.

## 1. What exists today

- **Minting a seat** is `admin.add_member` (`helpers/agent_bus/admin.py`), run by the
  `agent-bus` zipapp (`/usr/local/lib/agent-bus/agent-bus.pyz`) as the `agent-bus` system
  user. That user already owns everything a seat needs, under `/var/lib/agent-bus/<team>/`
  (0700): `team.json`, `registry.json` (changed only through `registry.update_registry`,
  which holds an `fcntl` lock), `room_id`, and `secrets/` (`registration_shared_secret`,
  `admin.token`, `admin.password`). It talks to the homeserver on `127.0.0.1` only. The
  homeserver unit `agent-bus-hs@<team>` runs as the same user.
- **Root's only part** is the wrapper `files/usr/local/bin/agent-bus`: `runuser` to
  `agent-bus`, then, for `add-member --out=DIR`, placing the bundle as the sudo user. With
  `--out=-` it prints the bundle (`member.json`, `token`, `README`) as a tar.
- **The desktop launcher** (`agent-bus-seats.bash`) calls `agent-bus seat check|take`
  (Python, `helpers/agent_bus/checkout.py`, run as the user in the checkout), which runs
  `sudo agent-bus add-member --seat=<n> --out=-` and places the tar itself without following
  symlinks. sudo's password is today's approval.
- **LXC members** are declared in `agent_bus_lxc_members` and joined by root
  (`helpers/agent_bus_lxc/members.py`, through `lxc-attach`), which also copies the kit into
  the container and writes `~/.config/pingbus/env`. The unmerged branch
  `feat/ccy-teams-lxc-member` (Plan 00177 Phase 9) adds `ccy-teams lxc-add`, which writes
  that entry for the owner.
- **Reachability.** Each team's `allow_from` (plus the LXC bridge's subnet, added by
  `helpers/agent_bus/bridge.py`) is enforced by firewalld rich rules and the unit's
  `IPAddressAllow`. There is no TLS anywhere on the bus.

## 2. Threat model

**Who reaches the broker.** Every process in every LXC or podman container on a bridge some
team admits, every process on the desktop, and any WireGuard peer in an `allow_from`. A
network allowlist names addresses, not people: on a bridge, root in any container can also
send from another container's address and answer ARP for the host's, so it can spoof a
source and sit in the middle.

**What each endpoint gives an attacker who reaches it:**

| Endpoint              | Gain without the owner                                                             |
| --------------------- | ---------------------------------------------------------------------------------- |
| `POST /v1/requests`   | A pending request and one room notice; capped (section 4.6). Never an account.     |
| poll and collect      | Nothing: both need the requester's private key; collect needs an approval too.     |
| rejoin (`.../token`)  | Nothing: needs the private key bound to an approved seat.                          |
| enroll (`.../enroll`) | Nothing new: needs a seat's current valid Matrix token, which already is the seat. |
| `POST /v1/lookup`     | Which admitted teams cover a repo whose name it already knows.                     |
| the admin socket      | Not reachable: a Unix socket, root only (SO_PEERCRED uid 0).                       |

**Why reaching it never yields a seat (D1).** The broker mints only at collect, and collect
needs an approval that came from root (sudo) or from a team human in the room who typed the
request's pairing code. The code is computed from the requester's own public keys
(section 4.2), so it is known only where the request was made. A man in the middle that
swaps the keys changes the code; the owner types the code from the requester's screen, and
the broker refuses it. The bundle is sealed to the requester's key, so a sniffer on the
bridge learns nothing from the handover.

**What it does not protect.** The homeserver itself has no TLS: once a member runs, its
Matrix token crosses the bridge in clear, as today (docs/agent-bus.md, "Limits"). The broker
does not make that worse and does not fix it.

## 3. A member's identity comes from the checkout

The repository is the source of truth (owner direction). The requester derives the handle
`<repo>.<seat>+<host>.<type>` from its checkout by exactly the rule the ccy launcher uses
today:

- `<repo>` from the checkout's forge remote (`registry.normalise_repo`);
- `<host>` from the checkout's `.claude/ccy/ccy.env.local` `HOOKS_DAEMON_HOSTNAME`, parsed,
  never sourced, or `local` when it sets none (`checkout.seat_host`);
- `<seat>` by ccy's rule: a seat the checkout already holds a directory for, else a new one,
  numbered with the lowest number that no seat or active member of the same
  `<repo>+<host>.<type>` uses. The client sends `"seat": "new"`; the broker fills the number
  from the registry with the same rule and reserves it for the request's life;
- `<type>` from what runs: `podman` for a ccy session, `host` for `cc`, `lxc` for an agent
  in an LXC container.

The person approving is never asked to name a seat, a host word or a role; a new seat is a
`worker`, and roles change afterwards (D2).

**What the request carries, and what the broker checks.** The request names its repo, the
derived handle and three claims: the container, the user and the checkout path.

| Field          | The broker checks                                                                               |
| -------------- | ----------------------------------------------------------------------------------------------- |
| handle         | the protocol grammar; the repo, host and type parts agree with the fields; the type is allowed  |
| repo           | that the team covers it (`org` or `repos`, not `exclude`); otherwise refused before any notice  |
| seat           | a named seat must be free or the requester's own (else the request says it replaces a key, 6.2) |
| container      | compared with the DHCP name lxc-net's lease file gives the source address; a mismatch is shown  |
| user, checkout | grammar only (a user name; an absolute path the line reader keeps whole)                        |
| source address | inside the team's effective `allow_from`; the interface it arrived on is recorded               |

**The repo cannot be proven, and why that is acceptable.** The broker cannot see into the
container, so it cannot check that the checkout is that repository, and it never accepts a
gh credential to test push rights (a credential must never be sent to it). A requester can
lie about its repo, container, user and checkout. What holds instead:

- the owner approves facts they recognise (handle, container, user, checkout, source
  address and interface, the lease name) **and** types the code shown where the request was
  made, so an approval binds to a screen the owner is looking at, not to the claims;
- a seat's powers do not depend on its repo label: every member may refer to any file the
  team trusts, and every reference is checked at the forge when it is acted on. A false
  label misleads people reading the room; it does not widen what the member may do.

## 4. The flow

A device flow: request, pairing code, approval, collection.

### 4.1 Request

The client makes two key pairs and keeps their private halves only on its own side: an
Ed25519 **member key** (signing; the seat's lasting identity, section 6) and an X25519
**sealing key** for this request. It sends the handle, the claims and both public keys,
signed with the member key. The broker answers with a request ID, the resolved handle (a new
seat's number filled in), the expiry and the poll interval. It posts the room notice
(section 5.2) and writes an audit line.

### 4.2 The pairing code

`code = Crockford-base32(SHA-256("agent-bus-pair/1" || team || member_pub || sealing_pub))`,
the first 50 bits, shown as `XXXXX-XXXXX`. The client computes it from its own keys; the
broker computes it from the keys it received and never sends it. The client prints it to
stderr with the handle, and keeps the request ID and keys in a pending file beside the
bundle directory, so a rerun resumes the same request and code.

How it stops a wrong approval: two requests pending at once (the owner's and an attacker's)
have different codes, and the owner only ever sees the code of the one they started. An
approval names a request by its code, so typing the owner's code can only approve the
owner's request. A key swapped in transit gives a different code; the typed one is refused.

### 4.3 Approval and refusal

An approval carries the code; the broker finds the team's pending request with that code
and marks it approved, recording who approved and how. A request is refused by its ID (in
`ccy-teams`) or by a reply to its notice (section 5.2); a refusal needs no code, since it
can only take something away. Five wrong codes against a team within 600 s refuse every
request pending in it and say so in the room.

### 4.4 Collection

The client polls (signed, long-poll up to 30 s). Once approved, it collects (signed). Only
then does the broker run `add-member --seat=<n> --out=-` (section 7.2), seal the tar to the
sealing key (X25519 with an ephemeral broker key, HKDF-SHA256, AES-256-GCM, the request ID
and handle as associated data) and answer. Collection is one-shot: the request closes, and
a second collect answers `410`. The client decrypts, places the bundle (directory 0700,
files 0600, no symlink followed) and writes the member key as `member.key` (0600) in the
bundle directory.

### 4.5 Expiry, replay and what is left behind

- A pending request expires 600 s after it was made; an approved one not collected within
  300 s lapses. Both are closed, audited, and leave no account, no registry change and no
  token, because nothing is minted before collect.
- Requests live in the broker's memory only. A broker restart drops them; the client gets
  `404` and starts again with a new code.
- Every signed call carries a timestamp (within 60 s of the broker's clock) and a 128-bit
  nonce, kept for 120 s; a repeat is refused. Collect closes its request.
- A room event is acted on once: the broker keeps its sync position in its state file and
  ignores events older than the request they name. Edits (`m.replace`) are ignored, as in
  protocol §7.

### 4.6 Limits against flooding

| Limit                                             | Value              | Beyond it        |
| ------------------------------------------------- | ------------------ | ---------------- |
| request body                                      | 4096 bytes         | `413`            |
| pending requests per source address / team / host | 2 / 8 / 32         | `429`, no notice |
| new requests per source address                   | 5 per 600 s        | `429`            |
| polls per request                                 | one open long-poll | `429`            |
| rejoins per handle                                | 6 per 3600 s       | `429`            |
| connections open / header and body read time      | 64 / 10 s          | dropped          |

A burst of refusals is one audit line per source per 60 s and at most one room notice per
team per 3600 s ("requests from 192.0.2.10 refused: too many"). A flood can delay the
owner's own request (a source spoofing others' addresses can take their slots); it cannot
produce a seat.

### 4.7 Revocation

`revoke <team> <handle>` parks the member (token revoked by the zipapp's `park-member`) and
drops its key binding, so it cannot rejoin without a new approval. `remove` runs
`remove-member` and drops the binding. Both are audited and posted in the room.

## 5. Where approval happens

**First: root, through sudo** (the desktop launcher and `ccy-teams`). It is needed before
anything is cut over, because the desktop's own seats must go through the broker, and it is
the strongest proof of a human there is on the desktop: sudo's password, as today.
**Second: the team's room in Element**, for an owner away from the desktop.

### 5.1 ccy-teams and the desktop launcher

`agent-bus-broker ctl` speaks to the admin socket and refuses to run as anyone but root.

- `sudo agent-bus-broker ctl approve <team> --code <code>` approves. The desktop launcher
  runs it itself with the code it computed (`sudo -n` when unattended, as `seat take --no-prompt` does today), so a desktop seat still costs one sudo and nothing else.
- `ccy-teams` lists each team's pending requests as rows under the team ("asks to join"),
  with the facts of section 3; Enter offers approve (asks for the code shown by the
  requester) or refuse. Reading the list uses `ctl list <team>`, added to the read-only
  sudoers rule; approve and refuse ask for the password.

A process running as the desktop user (a `cc --teams` agent) can reach neither surface
without the password.

### 5.2 The room

The broker posts, as the team's `admin` account, an `m.notice` per request:

> Seat request R-4F2K for team `<team>`: `<repo>.3+local.lxc`, container `<container>`
> (its lease says `<container>`), user `<user>`, checkout `/home/<user>/<repo>`, from
> 192.0.2.10 on lxcbr0, key `SHA256:9f1c…`. To approve, send `approve` and the code shown
> where the request was made. To refuse, reply `refuse` to this message. Expires 14:32 UTC.

pingbus drops everything `admin` sends (protocol §9, step 4), so agents never act on it.
The code is not in the notice (decision 1).

**How the broker authenticates a human of the team.** It syncs the room with the admin
token, filtered to `m.room.message`. A message counts only when its sender is a user ID in
the team's `humans` (federation is off and registration closed, so the homeserver minted
every such ID), the account is neither locked nor deactivated (admin API), the body (reply
fallback removed, as protocol §7) is exactly `approve <code>` or, as a reply to a notice,
`refuse`, and it is not an edit. A reaction is never an approval: it carries no code and is
one tap on whichever message is under the thumb. The broker answers each command in the
room ("approved by @<name>", "no pending request has that code").

What remains: anyone holding a team human's Matrix session can approve. To approve their
own request they must also be the requester; a holder of a human session can already
instruct every agent in the team, which is worse than a seat.

## 6. Re-joins

### 6.1 The identity is the member key

An approved seat is bound to the public half of the member key the requester made. A
requester that still holds it gets a fresh token without asking: `POST /v1/seats/<team>/<handle>/token`, signed with the member key, carrying a new sealing key.
The broker returns a parked seat (`add-member --seat`) or rotates an active one
(`rotate-token`) and seals the result. A seat token is not the identity: it is a bearer
secret, sent in clear to the homeserver on every call, and it changes on every rotation.

Where the key lives: in the bundle directory. For a ccy seat that is
`<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/member.key`, which survives every
container start (podman containers are made new each launch; the checkout persists). For
an LXC agent it is `~/.config/pingbus/<team>/member.key`.

### 6.2 A rebuilt container, a lost key, a removed seat

The key is gone, so it is a new request and a new approval (D1: a rebuilt container is a
new container). Its handle is derived the same way, so it usually names a seat that exists;
the notice then says "replaces the key of `<handle>`, last used <time>", and approving drops
the old binding and rotates the token, cutting off the old holder. `agent-bus seat remove`
deletes the seat directory and with it the key, so it revokes too (decision 9).

### 6.3 Enrolling a key with a valid token

`POST /v1/seats/<team>/<handle>/enroll`, signed with a new member key and carrying the
seat's current Matrix token, binds that key when the handle has none: the broker checks
the token with `/whoami` against the handle's user ID and that the member is active. This
is the migration route for every member made before the broker (section 11); after it, the
key rules. It is audited and posted in the room (decision 8).

## 7. The process

### 7.1 Go, one daemon per host

`src-go/cmd/agent-bus-broker` (daemon `serve`, and `ctl`) and `src-go/internal/seatbroker`.
One daemon serves every team on the host: all teams' state already belongs to one user, so
an instance per team would add ports and units without separating anything. The unit,
`agent-bus-broker.service`:

- `User=agent-bus`, `NoNewPrivileges`, `ProtectSystem=strict`,
  `ReadWritePaths=/var/lib/agent-bus`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, the
  homeserver unit's resolver lockout, `RuntimeDirectory=agent-bus-broker` for the admin
  socket;
- `IPAddressDeny=any`, `IPAddressAllow=` loopback (the homeserver), the bus address and the
  union of the present teams' effective `allow_from`; `SocketBindAllow=tcp:<port>`;
- restarted by the play when a team's file or the binary changes.

**The service user** is the existing `agent-bus` (decision 4). It already holds the
registration secret, the admin token and the registry; a separate user would need group
access to all three, which widens them. The cost: a broker compromise is the team's root of
trust, as a homeserver compromise is today.

### 7.2 Reuse the Python admin code, through the zipapp, as the same user

Recommended (decision 5): the broker runs
`/usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz <command> <team> ...` with a
fixed argument list (`add-member --seat=<n> --out=-`, `rotate-token`, `park-member`,
`remove-member`, `set-role`, `list`) through the `execx` seam, and reads the tar from stdout.
No privilege changes hands: the zipapp already runs as `agent-bus`. Reasons:

- `admin.py` and `registry.py` are tested and still serve the owner's `sudo agent-bus`;
  two writers of `registry.json` in two languages would have to agree on its lock and
  format for ever. One writer keeps one rule.
- The broker's own code stays what is new: the network protocol, the code, sealing, limits,
  approvals and the audit log.
- Porting the admin code to Go is a later plan of its own, when it is next reworked
  (CLAUDE.md: a standalone Python CLI is ported when reworked).

The room surface needs a small Go Matrix client (sync with a filter, send a notice, read an
account's status); that is new code either way.

## 8. Where it listens, and how a client finds it

- **Addresses.** The host address of every bridge whose subnet a present team admits
  (`lxcbr0`'s, read from the live interface as `bridge.py` does), plus every present team's
  `listen` addresses (the bus address, a WireGuard address). Never `0.0.0.0` or `::`.
- **Port.** One fixed port for the host, `agent_bus_broker_port` (decision 6), opened by
  firewalld rich rules for the same sources, recorded and removed like the homeserver's.
- **Per request**, the source must be inside the named team's effective `allow_from`;
  otherwise `404`, the same answer as for a team that does not exist, so teams cannot be
  listed from outside.
- **No TLS.** There is no certificate the client could check without a prior secret. The
  pairing code authenticates the request and sealing protects the handover (section 2).
- **Finding it.** `--broker URL`, else `AGENT_BUS_BROKER` in the environment, else the
  default gateway's address on the fixed port (in an LXC container the gateway is the
  host's bridge address). On the desktop the client uses the bus address. Whatever answers
  is not trusted: the code check holds even if the gateway is someone else.

## 9. The API

JSON over HTTP/1.1. Errors are `{"error": "<word>", "reason": "<text>"}`, never holding a
secret. Signed calls carry `Authorization: AgentBusSig key=<fingerprint>, ts=<ms>, nonce=<b64>, sig=<b64>`, an Ed25519 signature over method, path, ts, nonce and the body's
SHA-256.

| Call                                    | Who                 | Does                                                                   |
| --------------------------------------- | ------------------- | ---------------------------------------------------------------------- |
| `POST /v1/requests`                     | anyone admitted     | a request (body signed); `201` with ID, handle, expiry, poll interval  |
| `GET /v1/requests/{id}?wait=30`         | the requester       | `pending`, `approved`, `refused`, `expired`                            |
| `POST /v1/requests/{id}/collect`        | the requester       | the sealed bundle; `409` before approval, `410` after collect or lapse |
| `POST /v1/seats/{team}/{handle}/token`  | the bound key       | a sealed new token (rejoin)                                            |
| `POST /v1/seats/{team}/{handle}/enroll` | holder of the token | binds a key to a seat that has none                                    |
| `POST /v1/lookup`                       | anyone admitted     | the admitted teams that cover a repo (for a bare `ccy --teams`)        |
| `ctl list <team>`                       | root (socket)       | pending requests and seats with their key fingerprints                 |
| `ctl approve <team> --code <code>`      | root                | approves the pending request with that code                            |
| `ctl refuse <team> <request-id>`        | root                | refuses it                                                             |
| `ctl revoke` / `remove` / `set-role`    | root                | section 4.7; roles by the zipapp's `set-role` (D2)                     |

## 10. The clients

**`agent-bus-seat`**, a static Go binary (`CGO_ENABLED=0`) in the member kit, so it runs in
any container with the kit. It is Go because the client needs Ed25519, X25519 and AES-GCM,
which Python's standard library lacks and the helpers may not import.

- `agent-bus-seat join --team <team> --type <type> --checkout <dir> --into <dir> [--env <file>] [--broker URL] [--wait <s>]`:
  derives the handle (section 3); rejoins when `<into>` holds a member key bound to it,
  enrolls when it holds a valid bundle and no key, otherwise requests, prints the code and
  waits. Places the bundle; with `--env`, adds the team to `PINGBUS_TEAMS` and sets
  `HOOKS_DAEMON_HOSTNAME`. Exit 0 placed, 64 usage, 69 broker unreachable, 75 still
  pending at `--wait` (a rerun resumes), 77 refused or expired, 78 anything else.
- `agent-bus-seat obtain ... --out=-`: the same, printing the bundle's tar, for the host's
  `seat take` until it is ported.

**The desktop.** `agent-bus seat take` keeps its placement code and swaps one call:
`sudo agent-bus add-member --out=-` becomes `agent-bus-seat obtain` (request, `sudo [-n] agent-bus-broker ctl approve` with the code, collect). `seat remove` calls `ctl revoke`.
Then `seat check|choose|take|list|remove` are ported to Go inside `agent-bus-seat` (spec
first), `agent-bus-seats.bash` calls it, and `checkout.py` goes: one code path for the
desktop and the container (CCY version bump). `ccy-teams` gains the pending rows and uses
`ctl` for role, park and remove. Hot-add (Plan 00177 Phase 8) creates its seat through
`obtain`, then writes its join marker as designed.

**`ccy --teams` in an LXC container** runs the same launcher code; with no `ctl` on the
machine it waits for the owner instead of calling sudo, printing the code and the handle.

**The in-container launcher another repository installs** must, as the agent user, in the
checkout: run `agent-bus-seat join --team <team> --type lxc --checkout <dir> --into ~/.config/pingbus/<team> --env ~/.config/pingbus/env`, stop on a non-zero exit with its
code, then run `pingbus config check`, then start Claude Code as
docs/agent-bus-lxc-members.md "Starting sessions" says. Its provisioner installs the kit,
which now includes `agent-bus-seat`.

## 11. State, the audit log, and retiring the LXC play

**State.** The registry stays `/var/lib/agent-bus/<team>/registry.json`, changed only by
the zipapp. The broker adds `broker.json` beside it (handle to member-key fingerprint and
public key, how and when bound; the room sync position), written atomically under its own
lock. A binding whose handle the registry no longer has is dropped on read. The broker never
writes `localhost.yml`.

**Audit log.** `/var/lib/agent-bus/<team>/audit.jsonl`, one JSON object per line: time,
event (`request`, `approve`, `refuse`, `expire`, `lapse`, `collect`, `mint-failed`, `rejoin`,
`enroll`, `revoke`, `remove`, `flood`, `wrong-code`), request ID, handle, claims, source
address and interface, key fingerprint, and the approver (`root`, or the human's user ID and
event ID). Opened `O_APPEND`; the installer sets the append-only attribute (`chattr +a`, which
only root can clear), so the broker can add but not rewrite. It is in the team's backup
archive and holds no secret.

**Retiring `agent_bus_lxc_members`** (Phase 3). The play's two member tasks,
`helpers/agent_bus_lxc/members.py` and its tests, the `localhost.yml.dist` example and the
docs go. Once gone, the play stops with a message when `agent_bus_lxc_members` is still
set, naming the migration below; it never ignores it. `ccy-teams lxc-add` is not merged
(decision 10).

**Migration of existing LXC members.** Their handles (`<repo>.<container>+<host>.lxc`)
stay valid. In each container, `agent-bus-seat join` finds the existing bundle with a valid
token and enrolls a key (section 6.3); `ccy-teams` shows which seats have a key. Then the
owner deletes the entries from `localhost.yml`. New members take ccy's numbered rule.

## 12. Room for automatic approval by range (D1), not built

An approval is a record `{kind, by}`; the kinds built are `root` and `human`. A later kind
`range` would come from a team file key (say `auto_approve_from: [<cidr>]`), checked when the
request is made and audited like the others. It must then account for what section 2 says:
a container's root can send from another address on the bridge, so a range is only as
strong as the bridge's anti-spoofing filter, which that later work would have to add.

## 13. Decisions for the owner

01. **The code is shown only where the request was made**; the notice and ccy-teams show the
    facts and ask for it to be typed. Recommended: yes. Showing it on both sides lets an
    owner who does not compare approve the wrong request.
02. **Who may approve in the room**: every human in the team's `humans`. Recommended. The
    alternative is a named subset.
03. **Room approval on for every team by default.** Recommended: on; a team file key turns
    it off.
04. **The broker runs as `agent-bus`.** Recommended (section 7.1).
05. **Reuse the zipapp; port the admin code later.** Recommended (section 7.2).
06. **One broker per host on one fixed port**, `agent_bus_broker_port`. Recommended; the
    value is set in the SPEC.
07. **The broker is installed on the desktop only** (by `play-agent-bus.yml`, which builds
    Go); other homeserver hosts keep `sudo agent-bus add-member`. Recommended: yes, until
    another project asks for it.
08. **Enrolling a key with a valid token** (section 6.3). Recommended: allowed, only for a
    handle with no key.
09. **`seat remove` revokes**, so the next launch asks again (sudo on the desktop).
    Recommended: yes.
10. **Do not merge `feat/ccy-teams-lxc-member`**; keep `agent_bus_lxc_members` working
    until Phase 3. Recommended. Merging it now gives a stopgap that Phase 3 deletes.
11. **Revocation from the room**: not offered; ccy-teams only. Recommended: a hijacked
    human session should not be able to cut members off silently.
12. **The provisioner installs the kit in LXC containers** after the retirement. Recommended
    (the plan's non-goal); docs/agent-bus-lxc-members.md says so.
13. **`/v1/lookup`** for bare `ccy --teams` in a container. Recommended: yes, admitted
    sources only.

## 14. Tasks (to replace the plan's Phases 2 and 3)

### Phase 2: Build (spec first, tests first)

- **2.1 SPEC-seat-broker.md**: numbered expectations for every call, limit, state change,
  exit code and audit line in this design.
- **2.2 The broker core** (`internal/seatbroker`), through fakes for the clock, the zipapp
  and the network. Expectations include: a request never approved causes zero zipapp runs;
  a wrong code is refused and counted; the code for swapped keys differs; collect twice
  gives `410`; an expired or lapsed request leaves the registry and `broker.json`
  unchanged; a replayed nonce is refused; each limit in 4.6 answers as stated; the source
  check answers `404` for an unknown and an unadmitted team alike.
- **2.3 Installed**: the unit, the firewall rules, the listen computation and
  `agent_bus_broker_port`, built and installed by `play-agent-bus.yml`; the read-only
  sudoers rule gains `ctl list`. Expectation: `systemctl show` lists the allow and bind
  properties; nothing listens on a wildcard.
- **2.4 `agent-bus-seat`** in the kit, with `join` and `obtain`. Expectations: the handle
  matches `checkout.py`'s for the same checkout (the Python suite's cases); a bundle is
  placed 0700/0600 with no symlink followed; each exit code.
- **2.5 The desktop path**: `seat take` uses `obtain`, `seat remove` uses `ctl revoke`;
  `ccy-teams` pending rows with approve and refuse. Expectation: `ccy --teams <team>` on the
  desktop asks sudo once and starts, as today.
- **2.6 The room surface**: notice, `approve <code>`, `refuse`. Expectations: a non-human
  sender, a locked human, an edit, a reaction and a wrong code approve nothing.
- **2.7 Rejoin and enroll.** Expectations: a bound key gets a new token with no approval; a
  revoked seat's key gets `403`; enroll binds once and refuses a second key.
- **2.8 Fable review** of the build; findings fixed.

### Phase 3: Cut-over

- **3.1 The seat commands in Go** inside `agent-bus-seat`; `agent-bus-seats.bash` calls it;
  `checkout.py` deleted (CCY version bump).
- **3.2 Migration and retirement** (section 11). Expectation: with `agent_bus_lxc_members`
  set, the play stops naming the migration.
- **3.3 Host acceptance** through `meta-deploy.bash`: a desktop seat; an LXC `ccy --teams`
  approved from ccy-teams and from Element; a refused and an expired request leave the
  registry unchanged; seats, bindings and the audit log survive a reboot. qa-reviewer over
  the plan.

## 15. Open risks

- **No TLS on the bus.** The broker's handover is sealed, but members' Matrix tokens still
  cross bridges in clear; a container root that can spoof ARP can take them.
- **Code grinding by a man in the middle.** An attacker must find keys whose 50-bit code
  equals the victim's within the 600 s expiry. A longer code costs the owner more typing.
- **A team human's Matrix session approves.** A `cc --teams` agent beside the owner's
  Element could approve its own request (section 5.2).
- **Broker compromise is the team's root of trust**: a network-facing Go service holding
  the admin token and registration secret. Mitigated by a small surface, body caps, the unit
  sandbox and the standard library only.
- **Claims are unverifiable** (section 3): the repo, user and checkout are what the
  requester says.
- **Source spoofing on a bridge** skews per-source limits and would weaken any later range
  policy.
- **Two languages on one task** until the admin code is ported: the zipapp's exit codes and
  stdout become a contract the broker's tests must pin.
- **The other repository must change**: it installs the kit with `agent-bus-seat` and calls
  `join` before starting a session.
