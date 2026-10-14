# Plan 00197 Task 1.2: adversarial review of the seat broker design

Reviewer: Fable 5.1. Subject: `DESIGN.md` at `61354190` on `design/seat-broker`, read against
`PLAN.md` (D1, D2), `helpers/agent_bus/{admin,registry,checkout,bridge,teamfile,render}.py`,
`helpers/agent_bus_lxc/members.py`, `files/usr/local/bin/agent-bus`,
`files/usr/local/sbin/agent-bus-install`, `files/etc/systemd/system/agent-bus-hs@.service`,
`files/etc/sudoers.d/agent-bus-read-only.j2`, `playbooks/imports/play-agent-bus.yml`,
`docs/agent-bus-protocol.md` §7 and §9, `docs/agent-bus.md`, `docs/agent-bus-lxc-members.md`,
and the unmerged `feat/ccy-teams-lxc-member`.

## Verdict: REVISE

The core is sound: mint only at collect, a pairing code computed from the requester's keys
and typed by the owner, the bundle sealed to a key only the requester holds, root through a
Unix socket as the first approval surface, one writer of `registry.json`. Nothing I found
lets a hostile container obtain a seat or a token without an approval. What needs revision
is around that core: a long-lived member key that quietly turns `park` into a non-revocation,
a room approval surface that lets any process running as the desktop user approve its own
request (bypassing the sudo gate the design calls its strongest proof), no authentication of
the broker to the requester, and a desktop path that routes a local root operation through a
network device flow. The fixes make the design smaller, not larger.

## Ranked findings

### F1 (High): the member key defeats revocation, and buys nothing the token does not already give

Section 6.1: a requester holding its member key gets a fresh token without asking; "the
broker returns a parked seat (`add-member --seat`) or rotates an active one". Section 11: a
binding is dropped on read only "whose handle the registry no longer has".

Checked against the code: `admin.park_member` revokes the token and marks the handle parked;
the handle stays in `registry.json` (`Registry.park` adds to `parked`, the member entry stays).
So after `sudo agent-bus park-member <team> <handle>` (still a supported root command: the
wrapper, D7's "other homeserver hosts", `ccy-teams` today, `members.py` `state: absent`), the
broker's binding survives and the parked holder returns itself with no approval. Park is the
project's one revocation primitive; the design keeps it working only for parks that go through
`ctl revoke`.

The threat: the key sits beside the token in the same directory (`<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/member.key`, or `~/.config/pingbus/<team>/`). Whoever can read the
token (the agent process itself; a prompt-injected session; anyone who copies a rebuilt or
compromised container's disk) can read the key. Today a stolen token is ended by `park`. With
the key, a stolen key outlives every token rotation and every park, and its holder can cut the
legitimate seat off at will (a rejoin rotates an active seat's token). The design's own
sentence, "a seat token is not the identity", is the problem: it creates a second, stronger
bearer secret where one already existed.

What the key was meant to buy: surviving a container rebuild. It does not: a rebuilt
container has lost the directory, key and token together (section 6.2 says so). For a ccy
seat the checkout persists the token already. Tuwunel tokens do not expire. The rejoin case
that remains is "the token was rotated or the seat parked by an admin", which is precisely the
case that must require approval.

Recommendation: drop the member key, `broker.json` bindings, `.../token`, `.../enroll`,
section 6 and decision 8. Keys are per request and ephemeral (one Ed25519 for signing the
request's own calls, one X25519 for sealing), discarded after collect. A client with a bundle
whose token still works needs nothing; one whose token fails makes a new request (D1: a
rebuilt container is a new container), and the notice says "replaces the token of `<handle>`
(active, last seen …)" exactly as 6.2 already describes for a lost key. The migration of
existing LXC members (section 11) then needs no enroll step: their bundles keep working until
they do not. This removes one state file, three endpoints, a decision and the risk.

### F2 (High): the room surface lets any process running as the desktop user approve its own seat

Section 5.2 authenticates "a human of the team" by the sender's user ID. `helpers/agent_bus/element.py`'s own docstring: "A human's Element session token lives in that profile, and any
process of the same user can read it." The repository guards against this only on the
member side (`agent-bus-claude` and `pingbus config check` refuse a `host` member in a home
that holds an Element profile). Nothing stops any other process of the desktop user (a plain
`cc` session with bypass permissions, a script, a browser extension) from reading the profile
token, posting `approve <code>` for a request it made itself, and collecting a seat. The code
is no obstacle: the requester computes it.

Section 15 names this as "a `cc --teams` agent beside the owner's Element could approve its
own request" and leaves it. But decision 3 turns room approval on for every team by default,
and section 5 calls sudo "the strongest proof of a human there is on the desktop". The two do
not fit: with room approval on, the desktop's effective approval strength is "can read a file
in the desktop user's home", which is below sudo and below what D1 intends. A hostile
container cannot do this (it does not see the desktop home); a careless owner's desktop can.

Recommendation: decision 3 defaults off; a team file key turns it on, and the key's
documentation states the exposure in one sentence. When it is on, the broker should refuse an
approval whose sender's user ID is also the requester's declared `user` on the desktop
(cheap, partial) and post every approval in the room naming the approver (already designed),
so a self-approval is at least visible. Decision 11 (no revocation from the room) is right
for the same reason and should stay.

### F3 (High): the requester authenticates nothing; a bridge MITM can be the broker

Section 8: "Whatever answers is not trusted: the code check holds even if the gateway is
someone else." The code check protects the owner from approving a swapped request. It does
not protect the requester: a container that answers ARP for the gateway (section 2 grants
this) can play the broker end to end: accept the request, report `approved` at once, and seal
to the requester's public key (which is public) a bundle whose `member.json` names the
attacker's own homeserver and a team record in which the attacker is the human. The client
places it, `pingbus config check` passes against that server, and the agent takes the
attacker's instructions as a human's. The design says the sealing "protects the handover";
it protects confidentiality only, and X25519 ephemeral-static has no sender authentication.

This is the same class as the existing no-TLS limit (a MITM can impersonate the homeserver
to a member today), so the broker does not make the bus weaker than it is; but the design
presents the handover as sound, and it has a trust anchor it does not use: the member kit is
copied into the container by root (`members.py` `_sync_kit`, the provisioner per decision 12).

Recommendation: the broker has a long-term Ed25519 signing key under `/var/lib/agent-bus/`;
its public half ships in the kit (and in the ccy image for podman seats). Every broker response
is signed over the request ID, the ciphertext and the nonce; the client refuses unsigned or
wrongly signed answers. Add the public key to the HKDF `info` with both X25519 keys and the
request ID. This is HPKE's auth mode in spirit; using the Go `x/crypto` HPKE implementation
is acceptable, or the stated primitives with the `info` and signature pinned in the SPEC. The
same anchor later lets the bundle carry a pinned homeserver key when TLS is added, so it is
not throwaway.

### F4 (Medium): the desktop path is a network device flow for a local root operation

Section 10: `seat take` becomes `obtain` (request, `sudo ctl approve` with the code, collect),
and `ccy-teams`'s role, park and remove go through `ctl`. On the desktop, root already reaches
the admin socket; routing its own seat through `POST /v1/requests`, a pending slot, a code the
launcher computes and types to itself, sealing and collect adds four round trips and the
whole crypto path to an operation that is `sudo agent-bus add-member --out=-` today, and it
puts the owner's request in the same pending pool a bridge flood fills (F5).

Recommendation: `sudo agent-bus-broker ctl mint <team> --repo … --seat … --out=-` over the
admin socket: root asks, the broker runs the zipapp and prints the tar, exactly the shape the
wrapper gives today, audited as `approve {kind: root}` plus `collect`. No code, no sealing, no
pool slot. The "one code path" goal is met where it matters: one process runs the zipapp and
writes the audit log. `seat take` swaps `sudo agent-bus add-member` for `sudo agent-bus-broker ctl mint`; the hot-add flow does the same. The device flow is for requesters that are not root
on the host.

### F5 (Medium): one hostile container can hold every team's seat flow closed indefinitely

Section 4.6: pending requests capped at 8 per team and 32 per host; "a source spoofing others'
addresses can take their slots". A container root sends eight requests from eight bridge
addresses, repeating every 600 s. Every honest request answers `429` for as long as it
likes, at no cost. The design accepts "a flood can delay the owner's own request"; with the
desktop also in this pool (F4) it delays every seat on the host.

Separately, the audit log grows with the flood: "one audit line per source per 60 s" with a
spoofable source is one line per address per minute, on a file with `chattr +a` that nothing
rotates.

Recommendation: pools per arrival interface, so lxcbr0 cannot exhaust the bus address or a
WireGuard interface; the sudo path outside the pools (F4); a team cap that counts requests by
`(interface, handle prefix)` rather than only by address, since the prefix is what the owner
recognises; flood audit lines capped per team per interval, not per source. The design's one
room notice per team per hour is right.

### F6 (Medium): section 3 conflates a ccy session inside an LXC container with an LXC member

Section 3 says `<type>` is "`lxc` for an agent in an LXC container", and section 10 says
`ccy --teams` in an LXC container "runs the same launcher code" and the in-container launcher
runs `agent-bus-seat join --type lxc … --into ~/.config/pingbus/<team>`. These are two
different members. `ccy` inside an LXC container is podman inside LXC: `pingbus suggest-handle`
(`helpers/pingbus/cli.py`, `systemd-detect-virt`) reports `podman`; its bundle lives under the
checkout's seats directory and its `<host>` comes from `ccy.env.local`, by `checkout.py`'s
rule. An `agent-bus-claude` member of the container is type `lxc` with `~/.config/pingbus/`.
The PLAN's goal is the first; sections 3 and 10 describe the second. The spec must say which
`join` serves which, and that the handle rule the broker re-derives is `checkout.py`'s for
`podman`/`host` and `members.py`'s named-seat rule for `lxc` (the migration in section 11
relies on `<repo>.<container>+<host>.lxc` staying valid, so both rules live on).

### F7 (Medium): `--address` for the mint is unspecified, and the obvious value is wrong

The broker runs `add-member … --address=?`. `admin._member_address` refuses any address that
is not one of the team's `listen` addresses (loopback plus `listen`), and the LXC path today
passes the bus address (`members.py` `_join`, `--address={bus}`). The broker listens on the
lxcbr0 host address (section 8), which is not a `listen` address; passing the arrival address
fails the mint. The spec must map arrival interface to the bundle's `base_url`: the bus
address for a bridge or the desktop, the WireGuard listen address for a WireGuard source.

### F8 (Medium): five wrong codes cancel the owner's own requests, and only the owner can type codes

Section 4.3: five wrong codes within 600 s refuse every pending request in the team. Codes
reach the broker only from root (`ctl`) and from team humans in the room; a hostile container
has no way to submit one. The rule therefore defends against nothing and gives a careless
owner a way to cancel their own request by typo. Recommendation: count and audit wrong codes,
post a notice after five, refuse nothing. Also: two pending requests whose codes collide
(2^-50 per pair, but the broker must define it) make `approve <code>` ambiguous; refuse the
approval and say so, never pick one.

### F9 (Low): sealing details the SPEC must pin

- HKDF `info` must bind the ephemeral public key, the recipient public key, the request ID and
  the handle, so a ciphertext cannot be re-bound (the AAD alone covers only the GCM step).
- One message per derived key, so a fixed all-zero GCM nonce is correct; say so, rather than
  leave an implementer to pick a random nonce and a reused key.
- The signature over "method, path, ts, nonce and the body's SHA-256" lacks the team and the
  host:port; the path carries the team for seat calls but not for `POST /v1/requests` (the team
  is in the body, which is hashed, so this is fine once the body hash is mandatory; make it
  mandatory, including for an empty body).
- The nonce window (120 s) must stay at least twice the clock skew (60 s); state the
  dependency so one is not tuned without the other.
- Crockford base32 of 50 bits is 10 characters; the alphabet maps `O`/`0` and `I`/`L`/`1`
  together on input, which is why it was chosen: say the decoder normalises them.

### F10 (Low): the broker's unit and listen set have ordering gaps

Binding the lxcbr0 host address needs the interface up: the homeserver's drop-in carries
"the listen interfaces' device dependencies" (`render.py` `device_unit`); the broker's unit in
section 7.1 does not. `lxc-net` recreates the bridge on restart; use the same device
dependencies, or `IP_FREEBIND`. A new team's `allow_from` changes the broker's
`IPAddressAllow` and firewall rules; "restarted by the play" covers it, but
`agent-bus-install team` is also run by hand (docs), so the installer must restart the broker
too.

### F11 (Low): seat reservation and a concurrent sudo mint

Section 3 reserves a new seat's number "for the request's life" in broker memory; a
concurrent `sudo agent-bus add-member` (or the zipapp's own counter) can take it, and the
collect-time `add-member --seat=<n>` fails. `update_registry`'s `flock` makes the two zipapp
runs safe; the broker must still serialise its own zipapp runs per team and define the
outcome of a lost reservation: `mint-failed` audited, request closed, client exit 78 with the
reason. Do not re-number at collect: the owner approved the handle shown.

### F12 (Low): decision 7 misstates the build, and the kit becomes two-language

"play-agent-bus.yml, which builds Go": it does not today (`play-claude-yolo.yml` builds the
`src-go` binaries). The kit is documented as Python-only (`docs/agent-bus-lxc-members.md`:
"Python is the only language runtime the member kit needs"), and `agent-bus-install software`
installs a fixed `KIT_FILES` list. A static binary needs no runtime, so the claim survives,
but the installer, the kit list, the docs and the build live in Task 2.3/2.4 and should be
named there.

## Claims checked against the code

| Design claim                                                               | Code                                                                                                                                             | Holds                              |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------- |
| `add_member` is the only minting path, as `agent-bus`, loopback only       | `admin.add_member`, `check_base_url` ("only `http://127.0.0.1:<port>`"), wrapper `runuser -u agent-bus`                                          | yes                                |
| `registry.json` changes only under an `fcntl` lock                         | `registry.update_registry`, `flock(LOCK_EX)` on `registry.lock`; `save_registry` atomic                                                          | yes                                |
| Root's only part is the wrapper; `--out=-` prints a tar                    | `files/usr/local/bin/agent-bus` `print_bundle`                                                                                                   | yes                                |
| sudo's password is today's approval; `sudo -n` when unattended             | `checkout._sudo_agent_bus`; `agent-bus-seats.bash` adds `--no-prompt`                                                                            | yes                                |
| ccy's seat rule: lowest number no seat or active member of the prefix uses | `checkout.seat_choices`                                                                                                                          | yes (for `podman`/`host` only, F6) |
| `<host>` from `HOOKS_DAEMON_HOSTNAME` parsed, else `local`                 | `checkout.role_from_env_local`, `seat_host`                                                                                                      | yes                                |
| `allow_from` is declared list plus the live bridge subnet                  | `bridge.effective`, play task "Add the LXC bridge's subnet"                                                                                      | yes                                |
| No TLS anywhere                                                            | `render.py` toml, `docs/agent-bus.md` Limits                                                                                                     | yes                                |
| pingbus drops everything `admin` sends (protocol §9 step 4)                | §9: "anyone else (a non-team account, `admin`, …): drop (`sender`)"                                                                              | yes                                |
| Edits ignored, reply fallback stripped, as protocol §7                     | §7 and §9 06h, 09h                                                                                                                               | yes                                |
| Federation off, registration closed                                        | `render.py`: `allow_registration: False`, `allow_federation: False`                                                                              | yes                                |
| A locked account can be told apart (admin API)                             | `admin.human_lock` sets `locked` via `PUT /_synapse/admin/v2/users`; `account()` reads the same record                                           | plausible; pin in SPEC             |
| Fixed argv to the zipapp is injection-safe                                 | team `[a-z][a-z0-9-]*`, repo `[a-z0-9]…`, host `[a-z0-9]…`, seat `[1-9][0-9]{0,5}`: no value can begin with `-`; `execx` seam exists in `src-go` | yes                                |
| Read-only sudoers rule can gain `ctl list`                                 | `agent-bus-read-only.j2` pins two anchored regexes; a third fits                                                                                 | yes                                |
| The homeserver is Synapse-compatible for the admin calls                   | It is Tuwunel (`agent-bus-hs@.service`, `/_tuwunel/server_version`); the design never names it                                                   | name it                            |
| `play-agent-bus.yml` builds Go                                             | It does not; `play-claude-yolo.yml` does                                                                                                         | no (F12)                           |
| The lease file gives a container name for a source address                 | lxc-net's dnsmasq; the play sets `LXC_DHCP_CONFILE` only; default leases path is distribution-defined and root-owned 0644                        | plausible; pin the path in SPEC    |

## Is there an existing mechanism?

- **Matrix registration tokens.** Tuwunel takes one static `registration_token` for all
  self-registration. That is "anyone with the secret may register", not "the owner approves
  this request", and the account name, the registry entry, the role record and the room invite
  would still have to be made by the admin tool. Synapse's per-token admin API
  (`/_synapse/admin/v1/registration_tokens`, with use counts and expiry) is not implemented by
  Tuwunel. Not a fit.
- **Knocking.** Needs an account first; the account is the thing being approved. Not a fit.
- **The Synapse admin API.** It is what `add_member` already uses, behind loopback. The broker
  is a front door to it; it does not reinvent it.
- **`lxc-attach` push** (`members.py`, and `ccy-teams lxc-add` on the unmerged branch). For a
  container on this host, the host already delivers a bundle with no network, no crypto and
  the owner's sudo as approval. The broker does reinvent this for the on-host LXC case; its
  new value is the requester the host cannot attach to (a podman session inside an LXC
  container, a WireGuard peer, a container the desktop's `lxc-*` tools do not see) and the
  "ask from inside" shape. That is the PLAN's goal, so the reinvention is deliberate; the
  design should say so in one line rather than present the network path as the only way.

Honest answer: the device flow with a human-typed binding code is not available off the
shelf in a Conduit-lineage homeserver; building it is justified. The *size* of the build is
not: F1 and F4 remove roughly a third of it.

## The 13 decisions, and the `feat/ccy-teams-lxc-member` recommendation

| #   | Design                                             | This review                                                                                                                             |
| --- | -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| 01  | code shown only where the request was made         | Keep.                                                                                                                                   |
| 02  | every team human may approve                       | Keep; the set is at most 16 and the server mints every ID.                                                                              |
| 03  | room approval on by default                        | **Change: off by default, opt-in per team** (F2).                                                                                       |
| 04  | runs as `agent-bus`                                | Keep; the argument in 7.1 is right.                                                                                                     |
| 05  | reuse the zipapp                                   | Keep; pin its argv, exit codes and stdout as a contract test (Task 2.2).                                                                |
| 06  | one broker per host, one port                      | Keep.                                                                                                                                   |
| 07  | desktop only                                       | Keep; fix the build claim (F12).                                                                                                        |
| 08  | enroll a key with a valid token                    | **Drop with the member key** (F1).                                                                                                      |
| 09  | `seat remove` revokes                              | Keep; without keys it is `park`, as today, and the next launch asks sudo (desktop) or the owner (container).                            |
| 10  | do not merge `feat/ccy-teams-lxc-member`           | **Change: merge it.** See below.                                                                                                        |
| 11  | no revocation from the room                        | Keep, for the reason F2 gives.                                                                                                          |
| 12  | the provisioner installs the kit                   | Keep.                                                                                                                                   |
| 13  | `/v1/lookup`                                       | Keep; add to the threat table that it confirms team names for a guessed repo to any admitted source (it is already there in substance). |
| new | a member key, or per-request keys only             | Per-request keys only (F1).                                                                                                             |
| new | the broker signs its answers with a kit-pinned key | Yes (F3).                                                                                                                               |
| new | root mints through the socket, no device flow      | Yes (F4).                                                                                                                               |

**Decision 10.** The owner has said the ccy-teams approach is OK and that a daemon is the
goal; the branch received its adversarial review and fixes (`7590b1ad`, after this plan was
opened) and is behind `F44` only by the merge. The design's reason not to merge, "a stopgap
that Phase 3 deletes", is a cost of one deletion commit against a benefit the owner has asked
for: adding an LXC member from `ccy-teams` with no hand edit, today, with the owner's sudo as
approval, which is D1 exactly. Phase 3 of this plan already deletes `members.py` and the
`agent_bus_lxc_members` key; deleting `lxc-add` beside them is the same task. Merge it, and
list `lxc-add` in Task 3.2's retirement. The one thing to check at merge is that `lxc-add`
and this plan agree on handle derivation for `lxc` members (named seat by default, F6), which
they do today because both call `members.py`'s rule.

**D1 and D2** are respected throughout: nothing mints without an approval of kind `root` or
`human`; teams are created by `ccy-teams create` only; the `range` kind is left as a record
shape and not built. Section 12's warning that a range is only as strong as the bridge's
anti-spoofing is correct and should stay.

## Testability of the task list

The expectations in section 14 are mostly concrete and falsifiable through fakes (clock,
zipapp, network), which is the right shape. Gaps:

- **2.2** lists the right negatives. Add: "a request whose approval kind is `human` is refused
  when the team file has room approval off"; "two pending requests with one code make
  `approve` answer `ambiguous`"; "a response with a wrong broker signature is refused by the
  client" (F3); "`park` by the zipapp outside the broker leaves nothing the broker will honour"
  (moot once F1 is taken, otherwise mandatory).
- **2.2** needs a contract test against the real zipapp: a Go test that runs
  `python3 -I agent-bus.pyz` with the Python suite's fake homeserver is not available, so pin
  the contract as golden argv and exit-code tables shared with `tests/helpers/agent_bus/`
  (the `registry` and `agentbus` Go packages already parse the wrapper's marker lines; reuse
  that discipline).
- **2.3**'s expectation, "`systemctl show` lists the allow and bind properties; nothing listens
  on a wildcard", should be the installer's `verify` facts, as the homeserver's are
  (`assert_listening`, the property loop in `agent-bus-install`), so it runs on every deploy.
- **2.4**: "the handle matches `checkout.py`'s for the same checkout" is testable only once F6
  decides which rule applies to which type.
- **3.3** has the positive acceptance and the refused/expired negatives; add the hostile one,
  scripted: from a scratch container, a request that is never approved, a replayed collect,
  a collect with a forged signature, and a flood to the cap, after which `agent-bus list`
  shows no new account and the owner's own request still succeeds (F5).

## What holds, for the record

- A hostile container reaching every endpoint gains a pending request and one notice, and
  nothing else, provided F3 is taken (otherwise it gains the ability to feed a requester a
  bundle for its own server).
- Replay: timestamp plus nonce windows are consistent; collect is one-shot; room events are
  acted on once by sync position.
- Expiry and lapse leave no account, because nothing is minted before collect; checked
  against `add_member`'s rollback (`remove_member` or `park` on any failure after the claim).
- The pairing code binds the approval to the requester's keys; a swapped key changes it; a
  second-preimage on 50 bits inside 600 s is out of reach of anything on a bridge.
- Argument injection into the zipapp is prevented by the handle and team grammars.
- The audit log's `O_APPEND` plus `chattr +a` is right, with F5's cap.
