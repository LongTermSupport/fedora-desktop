# Plan 00197 design: the agent bus seat broker

The broker is a host daemon beside a team's homeserver. A client anywhere a team admits asks
it for a seat; the owner approves; only then is the seat minted, and only the requester can
collect it. On the desktop, root mints directly through the broker's socket. This document is
the design for Phases 2 and 3, revised after the Fable review
(`subagent-reports/261014-fable-review-design.md`); owner decisions are in section 13, the
task list in section 14 and the risks in section 15.

Addresses below are RFC 5737 examples. `<team>`, `<repo>`, `<container>` and `<user>` are
placeholders.

## 1. What exists today, and what the broker adds

- **Minting a seat** is `admin.add_member` (`helpers/agent_bus/admin.py`), run by the
  `agent-bus` zipapp (`/usr/local/lib/agent-bus/agent-bus.pyz`) as the `agent-bus` system
  user. That user already owns everything a seat needs, under `/var/lib/agent-bus/<team>/`
  (0700): `team.json`, `registry.json` (changed only through `registry.update_registry`,
  which holds an `fcntl` lock), `room_id`, and `secrets/` (`registration_shared_secret`,
  `admin.token`, `admin.password`). It talks to the homeserver (Tuwunel, unit
  `agent-bus-hs@<team>`, same user) on `127.0.0.1` only.
- **Root's only part** is the wrapper `files/usr/local/bin/agent-bus`: `runuser` to
  `agent-bus`, then, for `add-member --out=DIR`, placing the bundle as the sudo user. With
  `--out=-` it prints the bundle (`member.json`, `token`, `README`) as a tar.
- **The desktop launcher** (`agent-bus-seats.bash`) calls `agent-bus seat check|take`
  (Python, `helpers/agent_bus/checkout.py`, run as the user in the checkout), which runs
  `sudo agent-bus add-member --seat=<n> --out=-` and places the tar itself without following
  symlinks. sudo's password is today's approval.
- **LXC members** are declared in `agent_bus_lxc_members` and joined by root
  (`helpers/agent_bus_lxc/members.py`, through `lxc-attach`), which also copies the kit into
  the container and writes `~/.config/pingbus/env`. The branch `feat/ccy-teams-lxc-member`
  (Plan 00177 Phase 9) adds `ccy-teams lxc-add`, which writes that entry for the owner.
- **Reachability.** Each team's `allow_from` (plus the LXC bridge's subnet, added by
  `helpers/agent_bus/bridge.py`) is enforced by firewalld rich rules and the unit's
  `IPAddressAllow`. There is no TLS anywhere on the bus.

**An LXC container on this host can already be served without the broker.** The
`lxc-attach` push (`members.py`, and `ccy-teams lxc-add` from Plan 00177 Phase 9) delivers a
bundle with no network, no crypto and the owner's sudo as approval. The broker deliberately
reinvents that path for one reason: its new value is the requester the host cannot attach
to (a podman `ccy` session inside an LXC container, a WireGuard peer, a container the
desktop's `lxc-*` tools do not see), plus the same `ccy --teams` experience everywhere: ask
from inside, approve once, start.

## 2. Threat model

**Who reaches the broker.** Every process in every LXC or podman container on a bridge some
team admits, every process on the desktop, and any WireGuard peer in an `allow_from`. A
network allowlist names addresses, not people: on a bridge, root in any container can send
from another container's address and answer ARP for the host's, so it can spoof a source
and sit in the middle.

**What each endpoint gives an attacker who reaches it:**

| Endpoint            | Gain without the owner                                                         |
| ------------------- | ------------------------------------------------------------------------------ |
| `POST /v1/requests` | A pending request and one room notice; capped (section 4.6). Never an account. |
| poll and collect    | Nothing: both need the request's private key; collect needs an approval too.   |
| `POST /v1/lookup`   | Confirms which admitted teams cover a repo whose name it guessed.              |
| the admin socket    | Not reachable: a Unix socket, root only (SO_PEERCRED uid 0).                   |

**Why reaching it never yields a seat (D1).** The broker mints only at collect (or on root's
`ctl mint`), and collect needs an approval that came from root (sudo) or, where a team turns
it on, from a team human in the room who typed the request's pairing code. The code is
computed from the request's own public keys (section 4.2), so it is known only where the
request was made. A man in the middle that swaps the keys changes the code; the owner types
the code from the requester's screen, and the broker refuses it.

**A man in the middle posing as the broker.** A container root that answers ARP for the
gateway can answer the requester itself: accept the request, report `approved`, and seal to
the requester's (public) key a bundle naming its own homeserver and a team in which it is
the human. Sealing gives confidentiality only. So the broker signs every response with a
long-term Ed25519 key whose public half is in the member kit, which root (or the
provisioning repository) puts into the container (section 8.1). The client refuses an
unsigned or wrongly signed answer, and the signature binds the sealed bundle to the
request. The same anchor can later pin a homeserver key when TLS is added.

**Any process of the desktop user can read the Element session.** A human's Element token
lives in the desktop user's profile (`helpers/agent_bus/element.py` says so), so any process
running as that user (a `cc` session with bypass permissions, a script, a browser extension)
can post as that human. That is why room approval is off by default (decision 3, section
5.2), and why, when it is on, a request from the host itself can be approved only through
sudo.

**What it does not protect.** The homeserver itself has no TLS: once a member runs, its
Matrix token crosses the bridge in clear, as today (docs/agent-bus.md, "Limits"). The broker
does not make that worse and does not fix it.

## 3. A member's identity comes from the repo checkout

The repository is the source of truth (owner direction): the handle
`<repo>.<seat>+<host>.<type>` is derived from the checkout, never chosen by the approver.

- `<repo>` from the checkout's forge remote (`registry.normalise_repo`);
- `<host>` from the checkout's `.claude/ccy/ccy.env.local` `HOOKS_DAEMON_HOSTNAME`, parsed,
  never sourced, or `local` when it sets none (`checkout.seat_host`).

What `<type>` and `<seat>` are depends on which of two different members is asking:

| Member                                           | Type     | Seat                                                                                                                     | Bundle directory                                   |
| ------------------------------------------------ | -------- | ------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------- |
| a `ccy` session, on the desktop or in LXC        | `podman` | numbered, ccy's rule (`checkout.seat_choices`): a seat the checkout holds, else the lowest no seat or active member uses | `<checkout>/.claude/ccy/pingbus/seats/<team>/<n>/` |
| `cc` on the desktop                              | `host`   | numbered, the same rule                                                                                                  | as above                                           |
| an `agent-bus-claude` member of an LXC container | `lxc`    | named: the container's name (`members.py` `default_seat`)                                                                | `~/.config/pingbus/<team>/`                        |

`ccy` inside an LXC container is podman inside LXC (`pingbus suggest-handle` reports
`podman` there), so it is the first row, not the third: its bundle lives in the checkout and
its seat is numbered, exactly as on the desktop. An `agent-bus-claude` member is a long-lived
agent of the container itself; its named seat keeps its handle stable across a rebuild of
the container and keeps every existing member's handle (`<repo>.<container>+<host>.lxc`)
valid. Both rules live on: `checkout.py`'s for `podman` and `host`, `members.py`'s named
seat for `lxc`. The Go client and broker implement both, tested against the Python suites'
cases.

A numbered request sends `"seat": "new"` when the checkout holds no seat; the broker fills
the number from the registry by the same rule and reserves it (section 7.3). The approver is
never asked to name a seat, a host word or a role; a new seat is a `worker`, and roles change
afterwards (D2).

**What the request carries, and what the broker checks.**

| Field          | The broker checks                                                                               |
| -------------- | ----------------------------------------------------------------------------------------------- |
| handle         | the protocol grammar; the repo, host and type parts agree with the fields; the type is allowed  |
| repo           | that the team covers it (`org` or `repos`, not `exclude`); otherwise refused before any notice  |
| seat           | a numbered seat the checkout holds, `new`, or (type `lxc`) a named seat                         |
| container      | compared with the DHCP name lxc-net's lease file gives the source address (path pinned in SPEC) |
| user, checkout | grammar only (a user name; an absolute path the line reader keeps whole)                        |
| source address | inside the team's effective `allow_from`; the interface it arrived on is recorded               |

When the handle names a seat that exists, the request replaces that seat's token (section
6); the notice and ccy-teams say so.

**The repo cannot be proven, and why that is acceptable.** The broker cannot see into the
container, so it cannot check that the checkout is that repository, and it never accepts a
gh credential to test push rights. A requester can lie about its repo, container, user and
checkout. What holds instead:

- the owner approves facts they recognise (handle, container, user, checkout, source
  address and interface, the lease name) **and** types the code shown where the request was
  made, so an approval binds to a screen the owner is looking at, not to the claims;
- a seat's powers do not depend on its repo label: every member may refer to any file the
  team trusts, and every reference is checked at the forge when it is acted on. A false
  label misleads people reading the room; it does not widen what the member may do.

## 4. The network flow

A device flow, for requesters that are not root on the host: request, pairing code,
approval, collection. Root on the desktop does not use it (section 5.1).

### 4.1 Request

The client makes two key pairs for this request only and keeps their private halves in its
pending file: an Ed25519 **request key** (signs the request's own calls) and an X25519
**sealing key**. Both are discarded once the bundle is placed or the request ends; there is
no lasting member key. It sends the handle, the claims and both public keys, signed with the
request key. The broker answers with a request ID, the resolved handle (a new seat's number
filled in), the expiry and the poll interval. It posts the room notice when room approval is
on (section 5.2) and writes an audit line.

### 4.2 The pairing code

`code = Crockford-base32(first 50 bits of SHA-256("agent-bus-pair/1" || team || request_pub || sealing_pub))`,
10 characters shown as `XXXXX-XXXXX`. The client computes it from its own keys; the broker
computes it from the keys it received and never sends it. The client prints it to stderr
with the handle and keeps the request ID and keys in a pending file (0600) beside the bundle
directory, so a rerun resumes the same request and code. The decoder upper-cases, drops `-`
and spaces, and maps `O` to `0` and `I`, `L` to `1`: that is why Crockford's alphabet was
chosen.

How it stops a wrong approval: two requests pending at once (the owner's and an attacker's)
have different codes, and the owner only ever sees the code of the one they started. An
approval names a request by its code, so typing the owner's code can only approve the
owner's request. A key swapped in transit gives a different code; the typed one is refused.

### 4.3 Approval and refusal

An approval carries the code; the broker finds the team's pending request with that code
and marks it approved, recording who approved and how. If two pending requests in the team
have the same code, the approval is refused as `ambiguous` and the broker says so; it never
picks one. A wrong code is answered, counted and audited; after five within 600 s the broker
posts one notice, and refuses nothing (codes reach it only from root and from team humans,
so a refusal rule would defend against nothing and let a typo cancel the owner's own
request).

A request is refused by its ID (in `ccy-teams`) or by a reply to its notice (section 5.2); a
refusal needs no code, since it can only take something away.

### 4.4 Collection and sealing

The client polls (signed, long-poll up to 30 s). Once approved, it collects (signed). Only
then does the broker run the mint (section 7.2), seal the tar and answer. Collection is
one-shot: the request closes, and a second collect answers `410`. The client verifies the
broker's signature, decrypts, and places the bundle (directory 0700, files 0600, no symlink
followed).

Sealing, pinned so an implementer has nothing to pick:

- a fresh X25519 ephemeral broker key per response; shared secret with the sealing key;
- HKDF-SHA256, empty salt, `info = "agent-bus-seal/1" || broker_sign_pub || ephemeral_pub || sealing_pub || request_id || handle`,
  32 bytes out, so a ciphertext cannot be re-bound to another request, recipient or broker;
- AES-256-GCM with a 12-byte all-zero nonce, correct because each derived key seals exactly
  one message; associated data `request_id || handle`;
- the response body carries `ephemeral_pub` and the ciphertext, and is signed like every
  response (section 9).

### 4.5 Expiry, replay and what is left behind

- A pending request expires 600 s after it was made; an approved one not collected within
  300 s lapses. Both are closed, audited, and leave no account, no registry change and no
  token, because nothing is minted before collect.
- Requests live in the broker's memory only. A broker restart drops them; the client gets
  `404` and starts again with a new code.
- Every signed call carries a timestamp (within 60 s of the broker's clock) and a 128-bit
  nonce, kept for 120 s; a repeat is refused. The nonce window must stay at least twice the
  allowed skew; the SPEC ties the two so one is never tuned without the other.
- A room event is acted on once: the broker keeps its sync position in its state file and
  ignores events older than the request they name. Edits (`m.replace`) are ignored, as in
  protocol §7.

### 4.6 Limits against flooding

Pending requests are pooled **per arrival interface**, so a flood on `lxcbr0` cannot
exhaust the bus address or a WireGuard interface. Root's path (`ctl mint`, `ctl approve`)
is outside every pool.

| Limit                                                         | Value              | Beyond it        |
| ------------------------------------------------------------- | ------------------ | ---------------- |
| request body                                                  | 4096 bytes         | `413`            |
| pending per source address                                    | 2                  | `429`, no notice |
| pending per (interface, handle prefix `<repo>+<host>.<type>`) | 2                  | `429`, no notice |
| pending per (interface, team) / per interface                 | 8 / 32             | `429`, no notice |
| new requests per source address                               | 5 per 600 s        | `429`            |
| polls per request                                             | one open long-poll | `429`            |
| connections open / header and body read time                  | 64 / 10 s          | dropped          |

Audit lines for refused floods are capped **per team** (one `flood` line per team per 60 s,
with counts), not per source, so spoofed sources cannot grow the log; room notices are at
most one per team per 3600 s.

Honestly: a container root on a bridge can send from many spoofed addresses, which defeats
every per-address limit, and can hold that bridge's pool full by repeating requests. What
holds is that it cannot produce a seat, cannot block the desktop (root mints outside the
pools) or another interface, and cannot grow the audit log without bound.

### 4.7 Revocation

There is no broker-side identity to revoke. `park-member` revokes the token and `remove`
runs `remove-member`, whether through `ctl` or through `sudo agent-bus` directly; both are
authoritative because nothing outside the token can bring a seat back without a new
approval. Through `ctl` they are audited and posted in the room.

## 5. Where approval happens

**First: root, through sudo** (the desktop launcher and `ccy-teams`): the strongest proof of
a human there is on the desktop, sudo's password, as today. **Second, opt-in per team: the
team's room in Element**, for an owner away from the desktop.

### 5.1 Root: `ctl mint` on the desktop, `ctl approve` for requests

`agent-bus-broker ctl` speaks to the admin socket and refuses to run as anyone but root.

- **The desktop mints directly.** `sudo agent-bus-broker ctl mint <team> --repo <repo> --host <host> --type podman|host --seat <n|new> [--address <addr>] --out=-`:
  root asks, the broker runs the mint through its per-team serialisation and prints the tar,
  exactly the shape `sudo agent-bus add-member --out=-` gives today. No code, no sealing, no
  pool slot; audited as `approve {kind: root}` plus `collect`. `--address` defaults to the
  bus address; any other value must be one of the team's `listen` addresses (the zipapp
  checks). `ccy` and `ccy-teams` on the desktop use this; so does hot-add (Plan 00177
  Phase 8). The device flow is for requesters that are not root on the host.
- **Approving a request.** `sudo agent-bus-broker ctl approve <team> --code <code>`.
  `ccy-teams` lists each team's pending requests as rows under the team ("asks to join"),
  with the facts of section 3; Enter offers approve (asks for the code shown by the
  requester) or refuse. Reading the list uses `ctl list <team>`, added to the read-only
  sudoers rule; approve, refuse and mint ask for the password.

A process running as the desktop user cannot reach either without the password.

### 5.2 The room (off by default)

A team file key, `room_approval: true`, turns it on; its documentation states the exposure
in one sentence: any process running as the desktop user can read a human's Element session
and approve with it. With it off, the broker posts no notices and acts on no room message.

When on, the broker posts, as the team's `admin` account, an `m.notice` per request:

> Seat request R-4F2K for team `<team>`: `<repo>.3+local.podman`, container `<container>`
> (its lease says `<container>`), user `<user>`, checkout `/home/<user>/<repo>`, from
> 192.0.2.10 on lxcbr0. To approve, send `approve` and the code shown where the request was
> made. To refuse, reply `refuse` to this message. Expires 14:32 UTC.

pingbus drops everything `admin` sends (protocol §9, step 4), so agents never act on it.
The code is not in the notice (decision 1).

**How the broker authenticates a human of the team.** It syncs the room with the admin
token, filtered to `m.room.message`. A message counts only when its sender is a user ID in
the team's `humans` (federation is off and registration closed, so the homeserver minted
every such ID), the account is neither locked nor deactivated (admin API, pinned in SPEC),
the body (reply fallback removed, as protocol §7) is exactly `approve <code>` or, as a reply
to a notice, `refuse`, and it is not an edit. A reaction is never an approval. The broker
answers each command in the room naming the approver ("approved by @<name>"), so a
self-approval is at least visible.

**A request from the host itself is never approved from the room**, only through sudo: a
process of the desktop user that made a request (its source is an address of the host)
could otherwise read the Element session and approve it. What remains: whoever holds a
team human's session can approve a container's request whose code they learn; a holder of
a human session can already instruct every agent in the team, which is worse than a seat.

## 6. Rejoining

There is no member key. A client whose bundle's token still works needs nothing, as ccy
does today: the seat bundle and token are reused at every start. A client whose token fails
(rotated, parked or removed by an admin) asks again with a new request and needs a new
approval; that is the case that must require one.

When a request names a seat that exists, the notice and ccy-teams say "replaces the token
of `<handle>` (active, last seen …)" or "(parked)". On approval the broker parks an active
seat first, then runs `add-member --seat=<n>`, which returns the parked seat with a new
token, so the old holder is cut off.

**A rebuilt container** has lost its bundle, so it makes a new request and needs a new
approval (D1: a rebuilt container is a new container):

- an `agent-bus-claude` member (`lxc`) derives the same named seat, so its request replaces
  that seat's token;
- a `ccy` session (`podman`) whose checkout survived finds its seat directory and asks for
  that seat; one whose checkout was rebuilt too gets the next free number by ccy's rule,
  and the old seat stays until the owner parks it in ccy-teams.

`agent-bus seat remove` parks the seat and deletes its directory, so the next launch asks
again (sudo on the desktop, the owner for a container).

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
- `BindsTo=` and `After=` the device units of every interface it listens on, including the
  bridge's (`sys-subsystem-net-devices-lxcbr0.device`), computed as the homeserver's drop-in
  computes them (`render.py` `device_unit`), so it binds only once the bridge exists and
  restarts when `lxc-net` recreates it;
- restarted by the play, and by `agent-bus-install team` (which is also run by hand), when a
  team's file, its `allow_from` or the binary changes.

**The service user** is the existing `agent-bus` (decision 4). It already holds the
registration secret, the admin token and the registry; a separate user would need group
access to all three, which widens them. The cost: a broker compromise is the team's root of
trust, as a homeserver compromise is today.

### 7.2 Reuse the Python admin code, through the zipapp, as the same user

Recommended (decision 5): the broker runs
`/usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz <command> <team> ...` with a
fixed argument list (`add-member --repo=<repo> --host=<host> --type=<type> --seat=<n> --address=<addr> --out=-`,
`park-member`, `remove-member`, `set-role`, `list`) through the `execx` seam, and reads the
tar from stdout. No privilege changes hands. The team, repo, host and seat grammars allow no
value beginning with `-`, so the argv cannot be injected. Reasons:

- `admin.py` and `registry.py` are tested and still serve the owner's `sudo agent-bus`;
  two writers of `registry.json` in two languages would have to agree on its lock and
  format for ever. One writer keeps one rule.
- The broker's own code stays what is new: the network protocol, the code, sealing,
  signing, limits, approvals and the audit log.
- Porting the admin code to Go is a later plan of its own.

The zipapp's argv, exit codes and stdout become a contract: pinned as golden tables shared
with `tests/helpers/agent_bus/`, so a change on either side fails a test (Task 2.2).

**`--address` for the mint.** `admin._member_address` accepts only one of the team's
`listen` addresses, and the broker's `lxcbr0` host address is not one, so the arrival
address is never passed. The broker maps the interface a request arrived on to the bundle's
`base_url`: a bridge, or the bus address itself, gives the bus address (what `members.py`
passes today; a bridge member reaches it routed through the host, and the bridge subnet is
already in `allow_from`); a WireGuard `listen` address gives that address. A request whose
interface maps to no `listen` address of the team is refused before any notice.

The room surface needs a small Go Matrix client (sync with a filter, send a notice, read an
account's status); that is new code either way.

### 7.3 Seat reservation and a concurrent mint

The broker serialises its own mints per team (one at a time, `ctl mint` included) and
reserves a `new` seat's number in memory for the request's life, so two requests and a
`ctl mint --seat new` never take the same number. A `sudo agent-bus add-member` run outside
the broker can still take a reserved number; then the collect-time `add-member --seat=<n>`
fails, the broker audits `mint-failed`, closes the request and the client exits 78 with the
reason. It never re-numbers at collect: the owner approved the handle shown.

## 8. Where it listens, how a client finds it, and how it knows it is the broker

- **Addresses.** The host address of every bridge whose subnet a present team admits
  (`lxcbr0`'s, read from the live interface as `bridge.py` does), plus every present team's
  `listen` addresses (the bus address, a WireGuard address). Never `0.0.0.0` or `::`.
- **Port.** One fixed port for the host, `agent_bus_broker_port` (decision 6), opened by
  firewalld rich rules for the same sources, recorded and removed like the homeserver's.
- **Per request**, the source must be inside the named team's effective `allow_from`;
  otherwise `404`, the same answer as for a team that does not exist.
- **No TLS.** The pairing code authenticates the request to the owner; the broker's
  signature authenticates the broker to the client; sealing protects the handover.
- **Finding it.** `--broker URL`, else `AGENT_BUS_BROKER` in the environment, else the
  default gateway's address on the fixed port (in an LXC container the gateway is the
  host's bridge address). Whatever answers must sign with the kit's key.

### 8.1 The broker's signing key

`agent-bus-install software` makes an Ed25519 key once, `/var/lib/agent-bus/broker/signing.key`
(0600, `agent-bus`), never regenerating an existing one, and writes its public half as
`broker.pub` into the kit directory. The kit reaches a container as today: pushed in by root
(`members.py` `_sync_kit`, `ccy-teams lxc-add`) or copied from the host by the provisioning
repository (decision 14). `agent-bus-seat` refuses to start without `broker.pub`. A ccy
session in an LXC container needs no image change: its launcher runs in the LXC container,
outside podman, and uses that container's kit. Replacing the key means re-pushing every
kit; the installer's `verify` reports the key's fingerprint so a mismatch is visible.

## 9. The API

JSON over HTTP/1.1. Errors are `{"error": "<word>", "reason": "<text>"}`, never holding a
secret.

- **Requests are signed** with the request key: `Authorization: AgentBusSig key=<fingerprint>, ts=<ms>, nonce=<b64>, sig=<b64>`,
  an Ed25519 signature over `"agent-bus-req/1"`, method, path, ts, nonce and the SHA-256 of
  the body. The body hash is mandatory, an empty body's included. For `POST /v1/requests`
  the team is in the body, so the hash covers it.
- **Every response is signed** with the broker key, errors included: `X-AgentBus-Sig: <b64>`
  over `"agent-bus-resp/1"`, the request's own nonce, method, path, the HTTP status and the
  SHA-256 of the response body (which holds the request ID and, for collect, `ephemeral_pub`
  and the ciphertext). The client refuses a response with no signature, a bad one, or one
  over another nonce (exit 76).

| Call                               | Who             | Does                                                                   |
| ---------------------------------- | --------------- | ---------------------------------------------------------------------- |
| `POST /v1/requests`                | anyone admitted | a request; `201` with ID, handle, expiry, poll interval                |
| `GET /v1/requests/{id}?wait=30`    | the requester   | `pending`, `approved`, `refused`, `expired`                            |
| `POST /v1/requests/{id}/collect`   | the requester   | the sealed bundle; `409` before approval, `410` after collect or lapse |
| `POST /v1/lookup`                  | anyone admitted | the admitted teams that cover a repo (for a bare `ccy --teams`)        |
| `ctl mint <team> ... --out=-`      | root (socket)   | mints and prints the tar (section 5.1)                                 |
| `ctl list <team>`                  | root            | pending requests and seats                                             |
| `ctl approve <team> --code <code>` | root            | approves the pending request with that code                            |
| `ctl refuse <team> <request-id>`   | root            | refuses it                                                             |
| `ctl park` / `remove` / `set-role` | root            | section 4.7; roles by the zipapp's `set-role` (D2)                     |

## 10. The clients

**`agent-bus-seat`**, a static Go binary (`CGO_ENABLED=0`) in the member kit, so it runs in
any container with the kit and needs no further runtime. It is Go because the client needs
Ed25519, X25519 and AES-GCM, which Python's standard library lacks and the helpers may not
import.

`agent-bus-seat join --team <team> --type podman|lxc --checkout <dir> --into <dir> [--env <file>] [--broker URL] [--wait <s>]`:
derives the handle (section 3); does nothing when `<into>` holds a bundle whose token works
(`/whoami`); otherwise requests, prints the code and waits. Places the bundle; with `--env`,
adds the team to `PINGBUS_TEAMS` and sets `HOOKS_DAEMON_HOSTNAME`. Exit 0 placed or already
valid, 64 usage, 69 broker unreachable, 75 still pending at `--wait` (a rerun resumes), 76
the broker's signature failed, 77 refused or expired, 78 anything else.

**The desktop.** `agent-bus seat take` keeps its placement code and swaps one call:
`sudo agent-bus add-member --out=-` becomes `sudo [-n] agent-bus-broker ctl mint ... --out=-`.
`seat remove` calls `ctl park`. `ccy-teams` gains the pending rows and uses `ctl` for mint,
role, park and remove; hot-add mints through `ctl mint`, then writes its join marker as
designed. Then `seat check|choose|take|list|remove` are ported to Go (spec first), the
launcher calls the Go command, and `checkout.py` goes (CCY version bump).

**`ccy --teams` in an LXC container** runs `agent-bus-seat join --type podman --checkout <dir> --into <checkout>/.claude/ccy/pingbus/seats/<team>/<seat>`
from the launcher (outside podman, with the LXC container's kit); with no `ctl` on the
machine it prints the code and the handle and waits for the owner.

**An `agent-bus-claude` member in an LXC container**: the launcher the other repository
installs must, as the agent user, in the checkout, run
`agent-bus-seat join --team <team> --type lxc --checkout <dir> --into ~/.config/pingbus/<team> --env ~/.config/pingbus/env`,
stop on a non-zero exit with its code, then run `pingbus config check`, then start Claude
Code as docs/agent-bus-lxc-members.md "Starting sessions" says. Its provisioner installs the
kit, which now includes `agent-bus-seat` and `broker.pub`.

## 11. State, the audit log, and retiring the LXC play

**State.** The registry stays `/var/lib/agent-bus/<team>/registry.json`, changed only by
the zipapp. The broker keeps no seat bindings: its only persistent state per team is
`broker-state.json` (the room sync position, when room approval is on), written atomically.
Pending requests are in memory. The broker never writes `localhost.yml`.

**Audit log.** `/var/lib/agent-bus/<team>/audit.jsonl`, one JSON object per line: time,
event (`request`, `approve`, `refuse`, `expire`, `lapse`, `collect`, `mint-failed`, `park`,
`remove`, `flood`, `wrong-code`, `ambiguous`), request ID, handle, claims, source address
and interface, and the approver (`root`, or the human's user ID and event ID). Opened
`O_APPEND`; the installer sets `chattr +a`, so the broker can add but not rewrite. Flood
lines are capped per team (section 4.6). It is in the team's backup archive and holds no
secret.

**The interim: `ccy-teams lxc-add`** is merged now (decision 12), so the owner adds an LXC
member from ccy-teams with no hand edit, with sudo as the approval, until Phase 3.

**Retiring `agent_bus_lxc_members` and `lxc-add`** (Phase 3). The play's two member tasks,
`helpers/agent_bus_lxc/members.py` and its tests, the `localhost.yml.dist` example,
`ccy-teams lxc-add` and the docs go. Once gone, the play stops with a message when
`agent_bus_lxc_members` is still set, naming the migration below; it never ignores it.

**Migration of existing LXC members.** None is needed for their seats: their handles
(`<repo>.<container>+<host>.lxc`) stay valid, and their bundles keep working until their
tokens do not, after which `agent-bus-seat join` asks the broker. The owner deletes the
entries from `localhost.yml`, and the provisioner takes over the kit, including
`broker.pub`.

## 12. Room for automatic approval by range (D1), not built

An approval is a record `{kind, by}`; the kinds built are `root` and `human`. A later kind
`range` would come from a team file key (say `auto_approve_from: [<cidr>]`), checked when the
request is made and audited like the others. It must then account for what section 2 says:
a container's root can send from another address on the bridge, so a range is only as
strong as the bridge's anti-spoofing filter, which that later work would have to add.

## 13. Decisions for the owner

01. **The code is shown only where the request was made**; the notice and ccy-teams show the
    facts and ask for it to be typed. Recommended: yes.
02. **Who may approve in the room**: every human in the team's `humans`, when room approval
    is on. Recommended: yes.
03. **Room approval is off by default**; `room_approval: true` turns it on per team, and a
    request from the host itself is never room-approved. Recommended: off, because any
    process of the desktop user can read the Element session.
04. **The broker runs as `agent-bus`.** Recommended (section 7.1).
05. **Reuse the zipapp, its contract pinned; port the admin code later.** Recommended
    (section 7.2).
06. **One broker per host on one fixed port**, `agent_bus_broker_port`. Recommended; the
    value is set in the SPEC.
07. **The broker is installed on the desktop only**, by `play-agent-bus.yml`, which must
    gain a step that builds `agent-bus-broker` and `agent-bus-seat` from `src-go` (today only
    `play-claude-yolo.yml` builds Go); `agent-bus-install software`'s kit list gains
    `agent-bus-seat` and `broker.pub`, and docs/agent-bus-lxc-members.md's "Python is the only
    language runtime" becomes "Python and one static binary". Other homeserver hosts keep
    `sudo agent-bus add-member`. Recommended: yes, until another project asks.
08. **Per-request keys only; no lasting member key.** Rejoining reuses the bundle and token;
    a failed token means a new request and approval. Recommended: yes, so `park-member` and
    `rotate-token` stay authoritative.
09. **The broker signs every response with a key whose public half is in the kit**; the
    client refuses unsigned or badly signed answers. Recommended: yes.
10. **Root mints through the socket** (`ctl mint`) on the desktop; no device flow against
    itself. Recommended: yes.
11. **`seat remove` parks**, so the next launch asks again. Recommended: yes.
12. **Merge `feat/ccy-teams-lxc-member` now as the interim**; Phase 3 retires `lxc-add` with
    `agent_bus_lxc_members`. Recommended: yes.
13. **No revocation from the room**; ccy-teams only. Recommended: a hijacked human session
    should not cut members off silently.
14. **The provisioner installs the kit in LXC containers** after the retirement, including
    `agent-bus-seat` and `broker.pub`. Recommended (the plan's non-goal).
15. **`/v1/lookup`** for bare `ccy --teams` in a container. Recommended: yes, admitted
    sources only (it confirms team names for a guessed repo).

## 14. Tasks (to replace the plan's Phases 2 and 3)

### Phase 2: Build (spec first, tests first)

- **2.1 SPEC-seat-broker.md**: numbered expectations for every call, limit, state change,
  exit code, audit line, the sealing and signing byte layouts, the lease file path and the
  locked-account check.
- **2.2 The broker core** (`internal/seatbroker`), through fakes for the clock, the zipapp
  and the network. Expectations include: a request never approved causes zero zipapp runs;
  a wrong code is refused, counted and refuses nothing else; two pending requests with one
  code make `approve` answer `ambiguous`; the code for swapped keys differs; collect twice
  gives `410`; an expired or lapsed request leaves the registry unchanged; a replayed nonce
  is refused; a request without a body hash is refused; each limit in 4.6 answers as stated,
  per interface; root's mint succeeds with the bridge pool full; the source check answers
  `404` for an unknown and an unadmitted team alike; a `human` approval is refused when the
  team's room approval is off, and for a request from the host; a lost reservation gives
  `mint-failed` and no re-number; the arrival interface maps to the stated `--address`.
  **The zipapp contract**: golden argv, exit-code and stdout tables shared with
  `tests/helpers/agent_bus/`, checked on both sides.
- **2.3 Installed**: the unit (with its device dependencies), the firewall rules, the listen
  computation, the signing key and `agent_bus_broker_port`; `play-agent-bus.yml` builds the
  Go binaries; `agent-bus-install team` restarts the broker; the read-only sudoers rule
  gains `ctl list`. Expectations as the installer's **`verify` facts**, run on every deploy:
  the allow and bind properties, nothing listening on a wildcard, the key present and its
  fingerprint equal to the kit's `broker.pub`.
- **2.4 `agent-bus-seat`** in the kit. Expectations: the handle matches `checkout.py`'s for
  `podman`/`host` and `members.py`'s for `lxc` on the Python suites' cases; a bundle is
  placed 0700/0600 with no symlink followed; a valid bundle causes no request; **a response
  with a missing, wrong or other-nonce signature is refused (exit 76)**; each exit code.
- **2.5 The desktop path**: `seat take` uses `ctl mint`, `seat remove` uses `ctl park`;
  `ccy-teams` pending rows with approve and refuse; hot-add through `ctl mint`. Expectation:
  `ccy --teams <team>` on the desktop asks sudo once and starts, as today.
- **2.6 The room surface** (opt-in): notice, `approve <code>`, `refuse`. Expectations: off by
  default; a non-human sender, a locked human, an edit, a reaction, a wrong code and a
  request from the host approve nothing.
- **2.7 Fable review** of the build; findings fixed.

### Phase 3: Cut-over

- **3.1 The seat commands in Go**; the launcher calls them; `checkout.py` deleted (CCY
  version bump).
- **3.2 Retirement** (section 11): `agent_bus_lxc_members`, `members.py` and `ccy-teams lxc-add`
  go. Expectation: with `agent_bus_lxc_members` set, the play stops naming the migration.
- **3.3 Host acceptance** through `meta-deploy.bash`: a desktop seat; an LXC `ccy --teams`
  approved from ccy-teams (and from Element on a team with room approval on); a refused and
  an expired request leave the registry unchanged; seats and the audit log survive a reboot.
  **The hostile acceptance, scripted**: from a scratch container, a request never approved,
  a replayed collect, a collect with a forged signature, a fake broker answering with a
  wrong key, and a flood to the cap; afterwards `agent-bus list` shows no new account and
  the owner's own request still succeeds. qa-reviewer over the plan.

## 15. Open risks

- **No TLS on the bus.** The handover is sealed and signed, but members' Matrix tokens still
  cross bridges in clear; a container root that can spoof ARP can take them.
- **Code grinding by a man in the middle.** An attacker must find keys whose 50-bit code
  equals the victim's within the 600 s expiry. A longer code costs the owner more typing.
- **The desktop user's Element session**, where room approval is turned on (section 5.2).
- **Broker compromise is the team's root of trust**: a network-facing Go service holding
  the admin token, the registration secret and the signing key. Mitigated by a small
  surface, body caps, the unit sandbox and the standard library plus `x/crypto` only.
- **Claims are unverifiable** (section 3): the repo, user and checkout are what the
  requester says.
- **Source spoofing on a bridge** defeats per-address limits, lets one container keep that
  bridge's pool full, and would weaken any later range policy.
- **A replaced signing key** strands every kit until it is re-pushed.
- **Two languages on one task** until the admin code is ported: the zipapp contract test is
  the guard.
- **The other repository must change**: it installs the kit with `agent-bus-seat` and
  `broker.pub`, and calls `join` before starting a session.
