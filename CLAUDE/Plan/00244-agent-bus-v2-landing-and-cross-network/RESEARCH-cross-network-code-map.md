# Research: cross-network membership, what the code and specs already do (Task 8.8)

Read only, at the head of `spike/v2-merge-up`. Citations are repo-relative `path:line`. Direction 7
is `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md:47-59`. Examples use RFC 5737 / RFC 3849 values
and `<team>`, `<cidr>`, `<homeserver-host>`. `teamchat/` below is `src-go/internal/teamchat/`.
Design chapters are `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-{A-protocol,B-member,C-host}.md`
(A, B, C), specs `SPEC-matrix.md`, `SPEC-n3.md`.

## Facts that shape every item

1. **No TLS exists anywhere in v2 code.** No non-test file under `teamchat/` imports `crypto/tls` or
   `crypto/x509`; `matrix/client.go:149-161` builds the transport with no `TLSClientConfig`, so an
   `https://` base URL would be verified against the system roots. The only TLS words in the specs
   are "No TLS, as in v1 (R3)" (C:423-426, DESIGN-v2.md:481) and Plan 00209 Task 0.6's open item
   "optional TLS field reserved in bundle v1" (PLAN.md:202-206). No team CA, no certificate, no
   key store exists.
2. **The v2 launcher side is not built.** `cmd/ccy-teams` imports no `teamchat` package (only
   `cmd/teamchat`, `cmd/teamchat-admin` do), and 00209 Tasks 4.1 to 4.4 (kit, launcher, ccy-teams
   on both systems) are unchecked (PLAN.md:497-506). The seat store (`teamchat/seat`), the member
   packages (`teamchat/member/...`) and `teamchat-admin` exist. There is no v2 play yet
   (`playbooks/imports/play-agent-bus.yml` is v1's). So "a member on a second machine" can be
   proven at the `seat` + `teamchat` level; ccy-teams and the launcher cannot carry it.
3. **Every non-loopback member today is plain HTTP by design.** Local ccy containers reach the bus
   address (a `/32` on `teamchat0`, C:404-406) and LXC guests reach it over the bridge, neither
   is loopback. Direction 7's "plain HTTP only on loopback" therefore moves every ccy and LXC
   seat to TLS as well, not only remote ones. That is the biggest scope fact for item 3.
4. **`allow_from`, `listen` and `url` are three independent declared fields** today, tied to each
   other only by the owner's approval (`declare/gate.go:19`), never by code.

---

## Item 1: a team declares its member networks as CIDRs, the one source

### Exists

- `declare.Team` carries `Listen []string` (addresses, required, 1 to 8), `AllowFrom []string`
  (CIDRs, 0 to 32) and `URL` (`declare/declare.go:53-70`; bounds `declare/values.go:15-18`;
  required keys `declare/declare.go:209`). Readers: `validAddress` (`values.go:29`, canonical,
  not unspecified), `validPrefix` (`values.go:35`, canonical masked), `listen()` `values.go:126`,
  `allowFrom()` `values.go:130`.
- The three are in the approval gate, compared as sets: `gatedFields` `declare/gate.go:19`,
  `listField` `:129`, `--add/--remove` through `changeSet` `:346`, `applyChange` `:297-331`
  (`keyListen` `:317`, `keyAllowFrom` `:315`, `keyURL` `:323`); `approved.json` shape `:435-451`.
  `team.json` is `Team.members()` (`declare/input.go:98-114`), strict key list
  `teamFileKeys` `:121`.
- Consumers of `AllowFrom`/`Listen` (all derived at apply time, none declared again):
  - Tuwunel `address = ["127.0.0.1", <listen>...]`: `host/render.go:136-145`; `loopbackListen`
    `:29`.
  - Unit IP filter `IPAddressAllow=` = loopback `127.0.0.1/32 ::1/128` (`:32`) + each listen as a
    `/32` or `/128` + each effective `allow_from` (`host/render.go:218-234`); the base unit has
    `IPAddressDeny=any`, `SocketBindDeny=any` (C:288-289), the drop-in `SocketBindAllow=tcp:<port>`
    (`render.go:236`). The listen `/32`s are in the allow list because local container traffic is
    sourced from the host's own bus address.
  - Firewall: one firewalld rich rule per effective `allow_from` source, in the zone of the
    interface that routes it (`host/apply.go:556-570`, `host/records.go:96-102`;
    `hostnet/firewall.go:57-76`, `route()` `:165`, reads `/proc/net/route`), recorded in
    `firewall.json` before any change (`apply.go:508-540`).
  - Effective `allow_from` = declared + the live LXC bridge subnet when the team has a present
    LXC member (`host/apply.go:459-472`; `hostnet/network.go:74`).
  - Listen check: each listen address must be on exactly one interface, no wildcard
    (`host/apply.go:302-330`); after start the bound set must equal loopback + listen
    (`apply.go:791`).
  - LXC guest source check (R48): `guestfacts.Source(addrs, m.team.AllowFrom, link, bridge)`,
    `host/lxc/guestfacts/guestfacts.go:268-291`, called at `host/lxc/apply.go:776`.
- Design statement: A `listen` + `allow_from` + `url` in C 3.8 (C:1533-1545); "allow_from is not a
  boundary between teams on one host" (C:417-422, K16 at C:2736).

### Gap

- No `networks` field. `listen` is hand-declared and unrelated to the CIDRs; nothing derives it.
- **Latent mismatch to confirm before reuse:** `guestfacts.Source` reads the team's DECLARED
  `AllowFrom` (`host/lxc/apply.go:776`, `m.team = in.Team` at `:327`), while the unit and firewall
  use the declared list PLUS the bridge (`host/apply.go:459-472`). The only test world declares an
  `allow_from` that already contains the guest address (`host/lxc/world_test.go:38,564`). So an
  LXC member of a team that does not list the bridge looks refused by the check but admitted by
  the firewall. If this is real, the single-source change must make `Source` read the effective
  set. UNVERIFIED which side is intended; the spec should say.
- `url` host is checked against nothing: `validURLHost` (`values.go:66-71`) takes any IP or DNS name.
- Route lookup exists (`hostnet/firewall.go:165`) but only to pick a firewalld zone; the host's
  `Network` seam has `Interfaces`, `Listeners`, `Bridge` only (`host/apply.go:124-128`).

### Decisions the spec must make

- Field name and shape. `networks: [<cidr>...]` (plain list, matches the owner's words) versus
  `networks: [{cidr, site}]` (adds a per-network handle `site`, see item 5). Plain list is
  smaller; the object form solves the site source for free but adds a gated sub-structure.
- Fate of `allow_from` and `listen`. Options: (a) remove both, derived only (one source, as the
  owner asked; breaks `team.json` v1 and `approved.json` v1, so a version bump or a migration of
  `declare/input.go:121` and `gate.go:19`); (b) keep `allow_from` as a read-only derived value in
  `team.json` for the existing consumers (smaller diff, two spellings of one fact); (c) keep
  `listen` as an optional override. Recommend (a) with `team.json` `v` 2, since the tree is
  unreleased and the owner said break things (PLAN.md:35-39).
- How `listen` is derived from CIDRs, the hard part. Options: (1) host addresses INSIDE a
  declared CIDR (simple, pure, but a routed range like `203.0.113.0/24` that the host reaches
  through a gateway has no host address inside it, so it yields no listen address); (2) the
  addresses of the interface that ROUTES each CIDR, via `/proc/net/route` as `Firewall.Zone` does
  (handles routed ranges; a CIDR that falls to the default route would listen on the default
  interface, so a wide CIDR can bind a public-facing address, still filtered by IP filter and
  firewall); (3) (1) plus an explicit per-network `via` address for the routed case (explicit,
  more fields). Recommend (2) with a refusal when a CIDR routes only via the default route unless
  the owner approves it, and the host-local bus address kept as a `/32` entry in `networks`.
- Whether the local `teamchat_address` and the LXC bridge become `networks` entries (bridge stays
  auto-added today: C:414-416) or stay special. Keeping the bridge special preserves "bridge only
  for teams with an LXC member"; the spec must say whether it counts as a declared network for
  the seat URL check.
- Where the IP filter keeps the listen `/32`s (render.go:225-229): still needed for host-local
  traffic; say so.
- Gate: `networks` is a gated field (list set compare, `--add/--remove`), as `allow_from` is.
- A defect class for Defence Before Fix: "an address or prefix list consumed by render, firewall
  or filter that is not the one declared list". A go-analysis rule is awkward; a table-driven test
  that renders config, drop-in and firewall rules from one declaration and asserts each is a
  function of `networks` alone is the practical proof. Say so in the spec (CLAUDE.md allows "a
  class no detector can express is said so").

---

## Item 2: a seat is keyed on the homeserver URL

### Exists

- Spec statement: C 3.7.6 item 1, the take "is keyed on the team's approved url (3.10), not on a
  local `teamchat-server@<team>`"; pool layout, `held.json` and C41 unchanged (C:1492-1496).
- In code the take is already not tied to a local unit. `seat.Store.Take` (`seat/take.go:61`) works
  from a pool record, `Take.State` and `bundle.base_url`; nothing under `seat/` or `member/` calls
  `paths.ServerUnit`. Its only callers are `health`, `host/apply.go:241`, `host/remove.go:30`.
  The v1 "unit must be active here" refusal is Python (`helpers/agent_bus/checkout.py:701-716`,
  per `research-remote-seat.md:90-92`).
- The bundle carries the URL: `seat.Bundle.BaseURL` (`seat/records.go:46`), `admin.SeatBundles`
  sets it from `team.URL` (`admin/bundles.go:42`). Element profiles use the approved url
  (`element/element.go:196-201`). `reach --team T` uses the bundle alone
  (`member/client/reach.go:42-59`, n3c-D16).
- The store keys are the team id: pool `pool/<checkout>/<team>/<seat>`
  (`paths/paths.go:648-661`), session seat dir `<state>/<team>` (`paths.Layout.Seat`,
  `paths/paths.go:467`), and the member reads it back asserting seat.json's team equals the
  directory name (`member/bundle/bundle.go:80,165`). `server_name` defaults to
  `<team>.teamchat.internal` (`paths/paths.go:137`), so it is not a second key.

### Gap

- **Team id is the only key and nothing makes it unique across homeserver hosts.** Two homeservers
  on two hosts that both declare team `<team>` produce bundles that pass `Bundle.check`
  (`seat/records.go:154-185`) and collide in the same pool and seat directory. This is Task 0.6's
  open change 1 ("team ids unique across every homeserver host", PLAN.md:202-203).
- `Take.State` (`seat/take.go:49,101-119`: active, parked, lost, needs-reissue, retired) comes from
  "the status record's roster" (C 3.7.3), which is a file on the homeserver host
  (`paths.StatusRoot`, `paths/paths.go:87`). A machine that does not run the homeserver has no
  such file. `teamchat-admin status` is a non-root verb (`admin/run.go:143`) and could be read
  over ssh; UNKNOWN whether its output carries per-handle state, the spec must pin the field.
- Pool seat creation for a team whose homeserver is elsewhere does not exist (item 4).
- C 3.7.6 item 3 (fail fast at seat creation naming the URL, never at launch) has no code.
- Nothing checks that the machine's own source address lies in a declared CIDR before a seat is
  created. A connect timeout is the only symptom (this is the "seat URL check" of the owner's
  text, one reading of it).

### Decisions the spec must make

- The key. (a) Keep team id, and make uniqueness a declaration rule recorded where the member
  can check it: the pool checkout record (`paths.Store.PoolCheckoutRecord`, `paths/paths.go:625`)
  stores `server_name` + canonical `base_url` at first use, and a later bundle with the same
  team id and a different URL is refused with both URLs named (smallest, fail fast, no path
  change). (b) Key on a digest of the canonical URL, e.g. `<team>-<8 hex>` or a `url_key` path
  segment (true to "keyed on the URL", but changes `TeamID` grammar `proto/ids.go:43`, every
  path and the member's seat-dir name). (c) Key on `server_name` and require `server_name` to be
  unique per homeserver (declared per team, "never changes", C:1541; moves the uniqueness duty to
  the owner). Recommend (a) now and (b) only if two estates are expected to share a machine.
- What "canonical URL" is: scheme, lower-cased host, explicit port (`matrix.memberBase` already
  normalises, `matrix/client.go:632-649`), or the `(host, port)` pair alone.
- Take-state source on a remote machine: (a) ask `status` over ssh at take time (accurate,
  needs the ssh link at take time); (b) no state, let the homeserver refuse (token 401 maps to
  `ErrAuthRefused` exit 8, `matrix/client.go:219`) and reissue (fewer moving parts, a parked or
  retired seat is found by a failed login); (c) the issue answer carries the state. Recommend (b)
  with (a) as the fast-fail for creation.
- What "approved url" means off the homeserver host: the approval is a root file there
  (`status/<team>/approved.json`, `paths/paths.go:285`); the remote machine learns the URL from
  the bundle it was issued, which is the approved url at issue time. Say so, and say a changed
  `url` re-issues bundles (as any gated change).

---

## Item 3: TLS with the team's own CA pinned in the member bundle; plain HTTP only on loopback

### Exists

- The URL rule M14: `http://` only to an IP literal listed in `plain_http_hosts`; `https://` to an
  IP literal or a lower-cased DNS name (`SPEC-matrix.md:73-78`; `matrix/client.go:632-649`
  `memberBase`, `parseBase` `:653`). The admin client takes only `http://127.0.0.1:<port>`
  (`SPEC-matrix.md:79-80`; `matrix/client.go:614-628`, `admin.go:26`).
- `plain_http_hosts` is derived as "the URL host when it is an IP literal" (`admin/bundles.go:64-75`,
  duplicated in `admin/lxc.go:247-254`), stored in the bundle (`seat/records.go:47`), required in
  seat.json's strict key list (`:53-56`), re-checked on every decode through `NewMember`
  (`seat/records.go:170`), passed by every member client build (`seat/credential.go:89`,
  `member/client/seat.go:126`, `member/client/reach.go:48`, `member/guest/check.go:195`,
  `member/teamsync/teamsync.go:337`).
- Plain-HTTP loopback clients today: `ctl/ctl.go:81,93,124` (admin + member to `127.0.0.1:<port>`),
  `hostnet/probe.go:80` (readiness and health probe, `http://127.0.0.1:<port>`).
- Transport hygiene to keep: no proxy, no redirect, caps (`matrix/client.go:149-161`,
  `SPEC-matrix.md:26-47`); `TLSHandshakeTimeout` already set. gosec is enabled
  (`default: all`, `.golangci.yml:11`), so G402 (TLS min version, skip-verify) applies to new code.
- The Tuwunel pinned key list (`host/testdata/tuwunel-1.9.4-keys.txt`) holds leaf names `certs`
  (`:73`), `key` (`:182`), `dual_protocol` (`:126`), `config_reload_signal` (`:90`), `address`
  (`:3`), `port` (`:258`), `unix_socket_path` (`:410`, deliberately unused C:365-366), and
  `error_on_unknown_config_opts = true` makes an unknown key fatal (`host/render.go:168`). The
  renderer's keys are tested against that list (C:388-389).
- The unit gets only the config as a credential: `LoadCredential=config.toml:...` and
  `TUWUNEL_CONFIG=%d/config.toml` (C:261-262).

### Tuwunel TLS: what the repo states

UNKNOWN beyond the leaf key names above. The repo does not state: the table the keys sit in
(the list holds leaf names only, not table names), whether `certs` and `key` are file paths or
inline PEM, the accepted key encodings, whether a chain file is allowed, the default of
`dual_protocol`, whether TLS applies to every `address` entry including `127.0.0.1`, or whether
`config_reload_signal` reloads certificates. The spec must pin these by a recorded host check
(the way HC-A1 to A7 and H-C13 were recorded on Tuwunel 1.9.4, PLAN.md:223-245), run against
the pinned binary through `matrix/tuwunelhs` (`tuwunelhs.go:182-216` renders its config), before
any design depends on them.

### Gap

- No CA, no leaf certificate, no key storage, no trust field in the bundle, no client pool.
- `matrix.Config` has no CA field (`matrix/client.go:86-100`); `newHTTPClient` takes none.
- `seat.json` is strictly keyed and every key is required (`decodeStrict` `seat/records.go:194-224`,
  `bundleKeys` `:53-56`, `check` refuses `v != 1` `:155`). Task 0.6 wanted an "optional TLS field
  reserved in bundle v1"; the decoder has no optional-key mechanism, so any new key is a bundle
  version change or a new decoder parameter.
- Size: `maxRecord` is 16 KiB (`seat/records.go:23`); one PEM CA certificate (ECDSA) is about 0.6
  KiB, fine; `maxIssued` = bundle + secret (`member/guest/places.go`).
- LXC `guest check` stdin and `reach` carry `base_url`, `plain_http_hosts` only
  (`SPEC-n3.md:193`, n3-D6 `:1155`; `admin/lxc.go:247-254`; `member/guest/check.go:209-223`);
  they need the CA too.
- Element Desktop profile writes the approved url into `config.json` (`element/element.go:196-205`)
  with no way to trust a team CA. UNKNOWN how Element Desktop (Electron) is told to trust a private
  CA; the spec must decide (item 3 decision "Element").
- Loopback plain-HTTP clients (ctl, probe, readiness) break if the homeserver serves TLS on
  `127.0.0.1` too.

### Decisions the spec must make

- Where TLS terminates, driven by the Tuwunel facts above:
  (a) Tuwunel serves TLS itself on all `address` entries (one process; loopback becomes TLS
  unless `dual_protocol` is on, which would also accept plain HTTP from remote members, so not
  that). Then `ctl.go`, `probe.go` and the admin client move to HTTPS-with-CA on loopback, and the
  admin client's "http://127.0.0.1 only" rule (M15) changes. Contradicts "plain HTTP on
  loopback".
  (b) A small Go TLS terminator unit per team (`teamchat-tls@<team>`) listens on the `listen`
  addresses and forwards to Tuwunel bound to loopback only, plain HTTP. Loopback stays plain
  (honours the owner's text), Tuwunel's `address` becomes loopback alone, the Tuwunel unit's
  `IPAddressAllow` shrinks to loopback, and the CA key never reaches the homeserver user. Cost:
  a second unit, its hardening, a restart interplay, and a new binary.
  (c) Only if the Tuwunel check shows TLS can be confined to non-loopback addresses: TLS in
  Tuwunel for those, plain for `127.0.0.1`. UNKNOWN today.
  Recommend deciding after the Tuwunel check; (b) is the fallback that needs nothing from
  Tuwunel.
- Where plain HTTP stays. Strict reading (owner's words): only `127.0.0.1` / `::1`; local ccy
  and LXC seats use TLS too (their bundle already carries the CA). Looser reading: loopback plus
  `teamchat0` plus the bridge, which never leave the machine. Strict removes
  `plain_http_hosts` entirely (the rule becomes "http only if the host is a loopback literal");
  looser keeps a derived list. Recommend strict: it deletes the field and the two duplicated
  derivations (`admin/bundles.go:64`, `admin/lxc.go:247`).
- Bundle shape. (a) `seat.json` `v` 2: add `tls_ca` (PEM string, empty only for a loopback URL),
  drop `plain_http_hosts`; every decoder, `Issued`, `HeldBundle`, `MemberBundle` follow
  (`seat/records.go`); no compatibility needed (PLAN.md:25-39). (b) Keep `v` 1, add an optional
  key through a new `decodeStrict` optional list (smaller, but two shapes for ever). Recommend
  (a).
- Pin form. (a) Exclusive trust pool of exactly the team CA (system roots NOT consulted; the
  right reading of "pinned"). (b) SPKI hash pin of the leaf (survives CA changes less well, breaks
  on every leaf renewal). Recommend (a), TLS 1.3 minimum, verification by the URL host (IP SAN for
  an IP literal, DNS SAN for a name). `matrix.Config` gains a `CAPEM` (or `RootCAs`) field, and
  `memberBase` refuses `https` without one and refuses `http` to a non-loopback host.
- The CA itself. Algorithm (ECDSA P-256 via stdlib `crypto/x509`, no dependency; the repo
  needs no new module, `go-deps.allowlist` untouched). Where the CA key lives: `root/<team>/` (0600,
  root; C:211-220) and whether the team backup tar includes it (`backups/<n>.tar`, C:218): in
  means a restore keeps the same CA so issued bundles stay valid; out means a restore reissues
  every bundle. Leaf validity and renewal trigger (listen set changes, expiry) and CA lifetime;
  rotation of the CA means re-issuing every bundle, same as the `needs: reissue` path
  (C:2710, DC16). A new package (suggest `teamchat/pki`) with an injected `clock`.
- How the homeserver gets the cert and key: `LoadCredential=` entries beside `config.toml`
  (`C:261`), so the config holds `/run/credentials/teamchat-server@<team>.service/<file>`
  (stable path from the unit name; the repo does not show a config using that path, UNVERIFIED)
  versus a state-dir file (readable by the homeserver user, wider exposure). Credential is
  consistent with C's "homeserver cannot rewrite its own config" (C:340-341).
- Listen/URL consistency: certificate SANs = every `listen` address + the `url` host; `url` must
  be one of them (a new `declare` rule; today unchecked, `values.go:66`).
- Element: (a) `NODE_EXTRA_CA_CERTS` in the profile launcher (`element/element.go` writes the
  launcher; UNKNOWN whether Electron honours it for the renderer) or (b) the owner installs the CA
  in a trust store by hand (not IaC). Needs a check before choosing.
- `Admin` and loopback readiness: unchanged under option (b); under (a) they gain the CA.
- Test plan hooks: `fakehs` (`matrix/fakehs/http.go:54`, `net.Listen`) needs a TLS listener
  variant; the `tuwunel` build tag test (`src-go/CLAUDE.md` Build tags) renders the real config.
- Defence Before Fix: add a rule that a `http.Transport` or `tls.Config` under `teamchat/` is built
  only in `matrix` (one place), the same shape as `seamexec.go` / `rawconnprobe.go` in
  `src-go/qa/rules`; gosec G402 already covers skip-verify and min version.

---

## Item 4: a remote seat's token is issued over ssh

### Exists

- Issuing is `teamchat-admin member add|return|reissue` (root class, `admin/run.go:127`;
  flags `admin/verbs.go:103-111`, the caller names the credential KIND, not the secret, via
  `--credential password|token`). The answer is one JSON document on stdout carrying the
  `bundle` member: the pool `seat.json` + exactly one credential (`admin/verbs.go:301-309`,
  `seat.Issued` `seat/records.go:346-365`, strict `DecodeIssued` `:368-396`, built by
  `admin.SeatBundles.Bundle` `admin/bundles.go:26-52`). The credential is the root-created seat
  password (v2 default, DC16 (c')) or a token.
- A placement precedent: the host runs `guest place-bundle` with the issued JSON on stdin inside
  an LXC guest (`member/guest/places.go`, `OpPlaceBundle`; `host/lxc/lxc.go:104-108`). That is a
  push of an `Issued` document to a member-side program.
- `teamchat-admin status` is non-root (`admin/run.go:143`), so a read over ssh needs no sudo.
- Spec: C 3.7.6 item 2: remote pool gets the credential "over ssh (Plan 00205) or through the
  broker (Plan 00197)", `password` stays in the remote pool only (C:1497-1501). Plan 00205 is
  about the HOST reaching into members (`CLAUDE/Plan/00205-ssh-team-members/PLAN.md:10-16`) and has
  open owner decisions, no code. `research-remote-seat.md:159-163` lists what must never be on
  the remote VM, the owner's credentials included.

### Gap

- No ssh use anywhere under `teamchat/` (non-test). No pool-writing entry point on the member
  machine for a remote `Issued` (the guest op is the closest, `member/guest/places.go`, but it
  is a "guest" seat keyed for a guest layout, not a ccy pool seat).
- `member add` needs root and, per DC4, the owner's password sudo ("no sudoers file", PLAN.md
  Goals). Over ssh, sudo needs a tty or a password on stdin; stdout then also carries tty echo.
- The creating verb only prints the credential; nothing writes it to a remote pool, nothing records
  that a pool seat's account was made for a remote checkout, and nothing re-issues when the
  remote pool is lost.
- Direction conflict: if the REMOTE machine pulls (it ssh-es to the homeserver host), the remote
  machine must hold an ssh credential for the homeserver host, contradicting the
  less-trusted-machine posture (`research-remote-seat.md:159`).

### Decisions the spec must make

- Direction. (a) Push from the owner's machine: the owner (or the owner's `ccy-teams`) runs
  `sudo teamchat-admin member add` locally on the homeserver host, then pipes the `Issued`
  document to `ssh <member-host> teamchat seat place-issued` (a new member-side verb);
  the remote machine holds no credential for the homeserver host. Needs ssh from the owner to the
  member machine, which direction 7 already assumes ("issued over ssh"). (b) Pull by the remote
  launcher over ssh with a restricted key and forced command. Needs a root-capable path
  (sudo prompt) from a restricted key, or a new non-root issue verb, which would break "every
  account change needs the owner's password". Recommend (a).
- The remote placement verb: reuse `guest place-bundle`'s decoder (`seat.DecodeIssued`) in a new
  `seat place-issued` that writes a pool seat (password 0600 in the pool only, `seat.json`)
  through the `seat` store's directory-fd writers (`seat/fs.go`), takes the allocation lock, and
  refuses a bundle whose canonical URL or `server_name` differs from the checkout record (item 2).
- ssh invocation: through `execx` with a fixed argv; the stdin is the secret document (never argv,
  `src-go/CLAUDE.md` Rules "Secrets never in argv"); host key checking strict; the ssh target is a
  member-machine alias in the owner's own ssh config, never a hostname in a declaration or in this
  repo. Whether the declaration names a member machine at all (a `member_hosts` list) or the
  owner passes the target on the command line (YAGNI: command line).
- Credential kind for a remote seat: the pool password (a remote pool logs in per take, DC16
  (c')) versus a token only (no password stored on the less-trusted machine, DESIGN-v2.md 7.1
  alternative INT6). Recommend deciding with the owner; a token-only remote seat means a session
  restart reissues over ssh again.
- Revocation: `member park|retire` already revoke on the homeserver (token dies, C:2710 DC16);
  say that removing a CIDR from `networks` plus `team apply` cuts reach, and that a revoked
  remote pool seat is found at the next failed login.
- Who may start an issue to a remote: still the owner's password; no sudoers file.

---

## Item 5: a seat handle never carries a real hostname

### Exists

- Grammar: `<repo>.<seat>+<site>.<type>`, `site = [a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
  (`proto/ids.go:33,50`; A:205-212). A says "the install's site label ... never a hostname"
  (A:211), AR8 "derived from facts the host reads, never the machine's hostname" (A:66-67), C18
  "`site` never the machine hostname" (C:82). `NormaliseSite` folds case and turns every other
  character into `-` (`proto/ids.go:200-218`; A:214-217), so a dotted hostname becomes a
  hyphenated site, not a refusal.
- Where a site comes from today: only an LXC entry's optional `site` (`declare/declare.go:78, 378-383`, validated as already-normal `declare/values.go:101-104`).
- Derivation for LXC members: `guestfacts.Prefix` (`host/lxc/guestfacts/guestfacts.go:327-359`):
  the entry's `site`, else `HOOKS_DAEMON_HOSTNAME` read from the checkout's `ccy.env.local`
  (`:339-352`, `envHostname` `:503-533`) normalised, else `DefaultSite = "local"` (`:33`).
- The "never a hostname" rule is stated three times and enforced zero times in code.

### Gap

- **A real hostname can reach the handle today** through `HOOKS_DAEMON_HOSTNAME`
  (`guestfacts.go:346-351`), a file the container writes. Normalisation hides nothing: `my-host`
  stays `my-host`.
- Neither a ccy nor a host `cc` member has any site source in v2 (A defers to C, A:240-241; C 3.8
  gives only LXC entries a `site`; Task 0.6 change 2 is open, PLAN.md:203-204). Two machines both
  defaulting to `local` get legible-looking, indistinguishable handles (`research-remote-seat.md:162`
  asks for a distinct remote site label).
- No refusal of a site equal to a hostname the code can see: the member machine's own
  (`os.Hostname`, never read under `teamchat/`), the homeserver host's, or the `url` host's DNS
  labels.

### Decisions the spec must make

- Source of the site. (a) Declared only: a per-machine value the owner sets once in the
  member machine's own declaration (`teamchat_site`, as 0.6 suggested, `PLAN.md:204`), default
  `local`; the env-file source is removed. (b) Derived from the declared `networks` entry that
  contains the machine's own address (object form of item 1, `networks: [{cidr, site}]`): no extra
  per-machine setting, but a machine on two networks needs a tie-break. (c) Keep today's
  fallbacks and add only a refusal. Recommend (a), with `HOOKS_DAEMON_HOSTNAME` dropped.
- The refusal rule. A site equal to the normalised machine hostname, any dot-label of it, or any
  DNS label of the team `url` host is refused with the fixed message (cannot detect every real
  name; say so). Implemented once in `proto` or `declare` and called from the declaration reader
  and `guestfacts.Prefix`; tested with a fake hostname seam. The spec must say it is
  best-effort, and that the real guarantee is "no code path reads a hostname into a handle".
- Defence Before Fix (CLAUDE.md): the class is "a handle part derived from a source other than
  the declaration". Candidate go-analysis rule: `os.Hostname` and `net.LookupAddr` banned under
  `teamchat/` except one reviewed file; proof is a red fixture. A rule against the env-file read
  can be a test that `guestfacts` no longer references `HOOKS_DAEMON_HOSTNAME`.
- Existing handles that already embed a hostname (LXC members applied before this change): they
  are permanent identities (C18, handles never reused); the spec says retire/reissue or accept.
- Forge text: the existing stated reason (A:240-241) is that a hostname "must never reach forge
  text"; the spec should name which A/B code builds forge text from a handle so a test asserts it.

---

## Ordered build list (smallest independently testable slices)

Each slice is test first. S1 to S3 do not depend on S4 to S7.

01. **S1 `declare`: `networks`.** New field in `Team`, `values.go`, `declare.go`, `input.go`
    (`team.json` v 2), `gate.go` (gated, set compare, `--add/--remove`), effective `AllowFrom`
    derived in one function. Pure; unit tests only.
02. **S2 `host` + `hostnet`: derive and consume.** `Network` seam gains a route-to-interface-address
    lookup (reuse `hostnet/firewall.go:165` route reading); `listenInterfaces`, `renderConfig`,
    `ipAllow`, `wantedRules` read the one derived set; golden render proves config, drop-in and
    firewall rules are functions of `networks`. Fake `Network`.
03. **S3 LXC source check** reads the effective set (declared + bridge); closes the mismatch noted
    in item 1 with a failing test first.
04. **S4 `teamchat/pki`.** CA and leaf generation, SAN set from listen + url, validity, renewal
    test, PEM round trip, injected clock. No network.
05. **S5 `matrix` TLS.** `Config.CAPEM`/pool, exclusive roots, TLS 1.3, `memberBase`: `https`
    needs a CA, `http` only to a loopback literal. Tests against a TLS variant of `fakehs`
    (`matrix/fakehs/http.go:54`); a wrong-CA and a system-root-only cert must fail.
06. **S6 `seat` bundle v2.** Replace `plain_http_hosts` with `tls_ca` (decoder, `Issued`,
    `HeldBundle`, `MemberBundle`, `check`), `admin.SeatBundles` fills it from `root/<team>/` CA,
    `guest check` input and `reach` carry it, `seat/credential.go:89` and the other four member
    client builds pass it.
07. **S7 serve TLS.** First the recorded host check of Tuwunel's TLS keys through `tuwunelhs`
    (item 3 UNKNOWN); then the chosen termination (Tuwunel keys + `LoadCredential`, or the
    terminator unit), the loopback clients (`ctl/ctl.go`, `hostnet/probe.go`) adapted, the vendored
    key-list test extended. Real-Tuwunel test under the `tuwunel` tag.
08. **S8 seat key and take on a remote machine.** Pool checkout record stores `server_name` + URL;
    a clashing team id is refused naming both URLs; fast-fail on an unreachable URL at creation
    (C 3.7.6 item 3); take-state source per the decision. Tests at the `seat.Store` level.
09. **S9 issuing over ssh.** `seat place-issued` on the member side; the push command in the
    owner's tool using `execx` and stdin; test with a fake runner plus a VM-lab or two-process
    acceptance. Blocked on the direction decision; the ccy launcher path stays out (Tasks 4.2 and
    4.3 unbuilt), so acceptance uses `teamchat` + `seat`, not ccy.
10. **S10 handle and site.** Site source per decision; remove the env-file read; refusal rule and
    its Defence-Before-Fix detector with a red proof; existing-handle policy.
11. **S11 Element, docs, acceptance.** Element profile CA trust (after its check); the doc page
    for the declaration; the success criterion of PLAN.md:92-93: a member on a second machine
    joins over TLS, token issued over ssh, a connection from an undeclared network refused (the
    refusal is provable on one machine with a second address and a firewalld source rule, plus the
    unit IP filter).

Order constraint: S1 before S2, S3; S4 before S6 and S7; S5 before S6; S6 before S8 and S9.
S10 can run any time after S1.

## Owner questions this surfaced (for the spec's decision list)

- Strict "plain HTTP only on loopback" puts every local ccy and LXC seat on TLS; confirm that is
  intended (item 3).
- Push versus pull for ssh issuing, given the less-trusted remote machine (item 4).
- Whether the team backup includes the CA key (item 3).
- Whether a routed CIDR may bind the default-route interface (item 1).
