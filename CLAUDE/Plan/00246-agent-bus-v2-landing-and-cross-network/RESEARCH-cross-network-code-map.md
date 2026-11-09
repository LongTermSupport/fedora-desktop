# Research: cross-network membership, what the code and specs already do (Task 8.8)

Read only, at the head of `spike/v2-merge-up`. Citations are repo-relative `path:line`. Direction 7
is `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md:47-59`. Examples use RFC 5737 / RFC 3849 values and
`<team>`, `<cidr>`, `<homeserver-host>`. `teamchat/` is `src-go/internal/teamchat/`. A, B, C are
`CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-{A-protocol,B-member,C-host}.md`; `SPEC-matrix.md`
and `SPEC-n3.md` and `research-remote-seat.md` are in the same folder.

## Facts that shape every item

1. **No TLS exists in v2 code.** No non-test file under `teamchat/` imports `crypto/tls` or
   `crypto/x509`; `matrix/client.go:149-161` builds the transport with no `TLSClientConfig`, so an
   `https://` URL would be verified against the system roots. The specs say "No TLS, as in v1 (R3)"
   (C:423-426, DESIGN-v2.md:481) and Task 0.6 left an "optional TLS field reserved in bundle v1"
   open (PLAN.md:202-206). No team CA, certificate or key store exists.
2. **The v2 launcher side is not built.** `cmd/ccy-teams` imports no `teamchat` package (only
   `cmd/teamchat` and `cmd/teamchat-admin` do) and 00209 Tasks 4.1 to 4.4 are unchecked
   (PLAN.md:497-506). The seat store, the member packages and `teamchat-admin` exist; there is no
   v2 play (`playbooks/imports/play-agent-bus.yml` is v1's). A second-machine member can be proven
   at the `seat` + `teamchat` level, not through ccy.
3. **Every non-loopback member is plain HTTP today, by design.** Local ccy containers reach the
   bus address (a `/32` on `teamchat0`, C:404-406) and LXC guests connect from the bridge subnet;
   neither is loopback. "Plain HTTP only on loopback" therefore moves every ccy and LXC seat to
   TLS, not only remote ones. This is the largest scope fact (item 3).
4. **`allow_from`, `listen` and `url` are three independent declared fields,** tied together only
   by the owner's approval (`declare/gate.go:19`), never by code.

---

## Item 1: a team declares its member networks as CIDRs, the one source

### Exists

- `declare.Team` has `Listen` (addresses, required, 1 to 8), `AllowFrom` (CIDRs, 0 to 32) and `URL`
  (`declare/declare.go:53-70`; bounds `declare/values.go:15-18`; required keys
  `declare/declare.go:209`). Readers: `validAddress` `values.go:29`, `validPrefix` `:35`,
  `listen()` `:126`, `allowFrom()` `:130`.
- All three are approval-gated and compared as sets: `gatedFields` `declare/gate.go:19`,
  `listField` `:129`, `changeSet` `:346`, `applyChange` `:297-331` (`keyAllowFrom` `:315`,
  `keyListen` `:317`, `keyURL` `:323`), `approved.json` `:435-451`. `team.json` is
  `Team.members()` (`declare/input.go:98-114`), strict keys `teamFileKeys` `:121`.
- Consumers, each derived at apply time from the declared lists:
  - Tuwunel `address = ["127.0.0.1", <listen>...]`: `host/render.go:136-145`.
  - Unit IP filter: `IPAddressAllow=` = `127.0.0.1/32 ::1/128` (`render.go:32`) + each listen as a
    `/32` or `/128` + each effective `allow_from` (`render.go:218-234`); base unit
    `IPAddressDeny=any`, `SocketBindDeny=any` (C:288-289); drop-in `SocketBindAllow=tcp:<port>`
    (`render.go:236`). The listen `/32`s are there because local container traffic is sourced from
    the host's own bus address.
  - Firewall: one rich rule per effective `allow_from` source in the zone of the interface that
    routes it (`host/apply.go:556-570`, `host/records.go:96-102`, `hostnet/firewall.go:57-76`,
    `route()` `:165` reads `/proc/net/route`), recorded first in `firewall.json`
    (`apply.go:508-540`).
  - Effective `allow_from` = declared + the live LXC bridge subnet when the team has a present LXC
    member (`host/apply.go:459-472`, `hostnet/network.go:74`).
  - Each listen address must be on exactly one interface, no wildcard (`host/apply.go:302-330`);
    after start the bound set must equal loopback + listen (`apply.go:791`).
  - LXC guest source check (R48): `guestfacts.Source(addrs, m.team.AllowFrom, link, bridge)`
    (`host/lxc/guestfacts/guestfacts.go:268-291`, called `host/lxc/apply.go:776`).

### Gap

- No `networks` field. `listen` is hand-declared and unrelated to the CIDRs.
- **Latent mismatch, confirm before reuse.** `guestfacts.Source` reads the DECLARED `AllowFrom`
  (`host/lxc/apply.go:776`; `m.team = in.Team` at `:327`), while the unit and firewall use declared
  PLUS the bridge (`host/apply.go:459-472`). The only test world declares an `allow_from` that
  already holds the guest address (`host/lxc/world_test.go:38,564`). So an LXC member of a team
  that does not list the bridge looks refused by the check but admitted by the firewall.
  UNVERIFIED which is intended. With one source, `Source` must read the effective set.
- `url`'s host is checked against nothing (`validURLHost`, `values.go:66-71`).
- The `Network` seam has `Interfaces`, `Listeners`, `Bridge` only (`host/apply.go:124-128`); route
  lookup exists only inside the firewall adapter, for zones.

### Decisions the spec must make

- Shape. `networks: [<cidr>...]` (matches the owner's words) versus `networks: [{cidr, site}]`
  (also gives a handle `site` source, item 5, but a larger gated structure). Plain list is smaller.
- Fate of `allow_from` and `listen`. (a) Remove both, derive only (one source; `team.json` and
  `approved.json` go to `v` 2; the tree is unreleased and the owner said break things,
  PLAN.md:35-39). (b) Keep `allow_from` as a derived value in `team.json` (smaller diff, two
  spellings of one fact). (c) Keep `listen` as an optional override. Recommend (a).
- How `listen` is derived, the hard part. (1) Host addresses INSIDE a declared CIDR: pure and
  simple, but a routed range such as `203.0.113.0/24` behind a gateway has no host address inside
  it, so yields nothing. (2) Addresses of the interface that ROUTES each CIDR, from
  `/proc/net/route` as `Firewall.Zone` does: handles routed ranges, but a CIDR that falls to the
  default route would bind the default (possibly public) interface, still filtered by IP filter
  and firewall. (3) (1) plus an explicit per-network `via` address: explicit, more fields.
  Recommend (2), refusing a CIDR that routes only via the default route unless the owner approves,
  and the host-local bus address kept as a `/32` entry in `networks`.
- Whether the bus address and the LXC bridge are `networks` entries or stay special (the bridge is
  auto-added only for teams with an LXC member, C:414-416); and whether the bridge counts as a
  declared network for the seat URL check. The listen `/32`s stay in the IP filter for host-local
  traffic; say so.
- `networks` is a gated list field, `--add/--remove`, as `allow_from` is.
- Defence Before Fix: the class is "an address list consumed by render, firewall or filter that
  is not the one declared list". No analyzer fits well; say so (CLAUDE.md allows it) and prove it
  with one table test that renders config, drop-in and firewall rules from a declaration and
  asserts each is a function of `networks` alone.

---

## Item 2: a seat is keyed on the homeserver URL

### Exists

- C 3.7.6 item 1: the take "is keyed on the team's approved url (3.10), not on a local
  `teamchat-server@<team>`"; pool layout, `held.json` and C41 unchanged (C:1492-1496).
- In code the take is already not tied to a local unit: `seat.Store.Take` (`seat/take.go:61`) works
  from the pool record, `Take.State` and the bundle; `paths.ServerUnit` is used only by `health`,
  `host/apply.go:241` and `host/remove.go:30`. The "unit must be active here" refusal is v1 Python
  (`helpers/agent_bus/checkout.py:701-716`, per `research-remote-seat.md:90-92`).
- The bundle carries the URL: `seat.Bundle.BaseURL` (`seat/records.go:46`), set from `team.URL`
  (`admin/bundles.go:42`). Element profiles use the approved url (`element/element.go:196-201`);
  `reach --team T` uses the bundle alone (`member/client/reach.go:42-59`).
- The store key is the team id: pool `pool/<checkout>/<team>/<seat>` (`paths/paths.go:648-661`),
  session seat dir `<state>/<team>` (`paths/paths.go:467`), and the member asserts seat.json's team
  equals the directory name (`member/bundle/bundle.go:80,165`). `server_name` defaults to
  `<team>.teamchat.internal` (`paths/paths.go:137`), so it is no second key.

### Gap

- **Nothing makes a team id unique across homeserver hosts.** Two homeservers on two hosts that
  both declare `<team>` give bundles that pass `Bundle.check` (`seat/records.go:154-185`) and
  collide in one pool and seat directory (Task 0.6 change 1, PLAN.md:202-203).
- `Take.State` (`seat/take.go:49,101-119`: active, parked, lost, needs-reissue, retired) comes from
  the status record's roster (C 3.7.3), a file on the homeserver host (`paths/paths.go:87`). A
  machine without the homeserver has none. `teamchat-admin status` is non-root (`admin/run.go:143`)
  and could be read over ssh; UNKNOWN whether its output carries per-handle state.
- Creating a pool seat for a remote homeserver (item 4) and C 3.7.6 item 3 (fail fast at creation,
  naming the URL, never at launch) have no code. Nothing checks that the machine's own address is
  inside a declared CIDR before creating a seat (one reading of the owner's "seat URL check").

### Decisions the spec must make

- The key. (a) Keep the team id; the pool checkout record (`paths/paths.go:625`) stores
  `server_name` + canonical URL at first use and a later bundle with the same id and another URL
  is refused naming both (smallest, fail fast, no path change). (b) Key on a digest of the
  canonical URL (`<team>-<8 hex>` or a `url_key` segment): literal "keyed on the URL", but changes
  the `TeamID` grammar (`proto/ids.go:43`), every path and the seat directory name. (c) Key on
  `server_name` and require it unique per homeserver (moves the duty to the owner, C:1541).
  Recommend (a). The canonical URL is `matrix.memberBase`'s normal form (`matrix/client.go:632-649`).
- Take-state source on a remote machine: (a) `status` over ssh at take time; (b) none, let the
  homeserver refuse (401 is `ErrAuthRefused`, exit 8, `matrix/client.go:219`) and reissue; (c) the
  issue answer carries it. Recommend (b), with (a) as the fast-fail at creation only.
- What "approved url" means off the homeserver host: the approval is a root file there
  (`paths/paths.go:285`); the remote machine knows the URL from the bundle it was issued, so a
  changed `url` re-issues bundles. State it.

---

## Item 3: TLS with the team's CA pinned in the member bundle; plain HTTP only on loopback

### Exists

- Rule M14: `http://` only to an IP literal listed in `plain_http_hosts`; `https://` to an IP
  literal or lower-cased DNS name (`SPEC-matrix.md:73-78`; `matrix/client.go:632-653`). The admin
  client takes only `http://127.0.0.1:<port>` (`SPEC-matrix.md:79-80`; `matrix/client.go:614-628`).
- `plain_http_hosts` is "the URL host when it is an IP literal" (`admin/bundles.go:64-75`,
  duplicated in `admin/lxc.go:247-254`), a required key of seat.json (`seat/records.go:47,53-56`),
  re-checked on every decode via `NewMember` (`:170`), and passed by every member client build
  (`seat/credential.go:89`, `member/client/seat.go:126`, `reach.go:48`, `member/guest/check.go:195`,
  `member/teamsync/teamsync.go:337`).
- Plain-HTTP loopback clients: `ctl/ctl.go:81,93,124` and `hostnet/probe.go:80` (readiness,
  health). gosec is on (`default: all`, `src-go/.golangci.yml:11`), so G402 covers new TLS code.
- Tuwunel: the pinned key list `host/testdata/tuwunel-1.9.4-keys.txt` holds leaf names `certs`
  (`:73`), `key` (`:182`), `dual_protocol` (`:126`), `config_reload_signal` (`:90`); the config is
  rendered with `error_on_unknown_config_opts = true` (`host/render.go:168`) and tested against that
  list (C:388-389). The unit receives only `config.toml` as a credential
  (`LoadCredential=` C:261, `TUWUNEL_CONFIG=%d/config.toml` C:262).

### Tuwunel TLS: what the repo states

UNKNOWN beyond those leaf names. The repo does not give the table the keys sit in (the list has
leaf names only), whether `certs` and `key` are file paths or inline PEM, the accepted key
encodings, chain support, what `dual_protocol` does or defaults to, whether TLS covers every
`address` entry including `127.0.0.1`, or whether `config_reload_signal` reloads certificates. The
spec must pin these with a recorded host check on the pinned binary through `matrix/tuwunelhs`
(`tuwunelhs.go:182-216` renders its config), as HC-A1 to A7 and H-C13 were recorded (PLAN.md:223-245).

### Gap

- No CA, leaf, key storage, bundle trust field or client pool. `matrix.Config` has no CA field
  (`matrix/client.go:86-100`).
- seat.json is strict with every key required (`decodeStrict` `seat/records.go:194-224`,
  `bundleKeys` `:53-56`, `v != 1` refused `:155`). The "optional TLS field reserved in bundle v1"
  has no mechanism: a new key is a bundle version change or a new decoder parameter. Size is no
  issue (`maxRecord` 16 KiB, `:23`; one PEM CA is under 1 KiB).
- `guest check` stdin and `reach` carry `base_url` + `plain_http_hosts` only (`SPEC-n3.md:193`,
  n3-D6 `:1155`; `member/guest/check.go:209-223`); they need the CA too.
- The Element profile writes the approved url into `config.json` (`element/element.go:196-205`)
  with no way to trust a team CA; UNKNOWN how Element Desktop is told to trust a private CA.
- `ctl`, `probe` and readiness break if the homeserver serves TLS on `127.0.0.1` too.

### Decisions the spec must make

- Where TLS terminates. (a) Tuwunel serves TLS on all `address` entries: one process, but loopback
  becomes TLS (the key name `dual_protocol` suggests both protocols on one listener; UNKNOWN, and
  it would risk plain HTTP for remote members), so `ctl`, `probe` and the admin client (M15) move
  to HTTPS-with-CA, contradicting "plain HTTP on loopback". (b) A small Go TLS terminator unit per
  team listens on the `listen` addresses and forwards to Tuwunel bound to loopback only, plain
  HTTP: loopback stays plain, Tuwunel's `address` and unit IP filter shrink to loopback, the CA
  key never reaches the homeserver user; cost is a second unit, its hardening and a new binary.
  (c) TLS in Tuwunel only on non-loopback addresses, if the check shows it can: UNKNOWN. Decide
  after the Tuwunel check; (b) needs nothing from Tuwunel.
- Where plain HTTP stays. Strict (the owner's words): only `127.0.0.1` / `::1`; local ccy and LXC
  seats use TLS, their bundle carries the CA. Loose: loopback + `teamchat0` + the bridge. Strict
  deletes `plain_http_hosts` and its two derivations; recommend strict.
- Bundle shape. (a) `seat.json` `v` 2: add `tls_ca` (PEM, empty only for a loopback URL), drop
  `plain_http_hosts`; `Issued`, `HeldBundle`, `MemberBundle` follow; no compatibility needed
  (PLAN.md:25-39). (b) `v` 1 with an optional key (smaller, two shapes for ever). Recommend (a).
- Pin form. (a) An exclusive trust pool of exactly the team CA, system roots not consulted (the
  meaning of "pinned"). (b) A leaf SPKI hash (breaks at every leaf renewal). Recommend (a), TLS 1.3
  minimum, verified against the URL host (IP SAN or DNS SAN). `matrix.Config` gains `CAPEM`;
  `memberBase` refuses `https` without one and `http` to a non-loopback host.
- The CA. ECDSA P-256 from stdlib `crypto/x509` (no new module, `go-deps.allowlist` untouched), in
  a new package (suggest `teamchat/pki`) with an injected clock. Key at rest in `root/<team>/`
  (0600, C:211-220). Whether the backup tar (C:218) includes it: in keeps issued bundles valid
  across a restore; out reissues every bundle. Leaf SANs = every `listen` address + the `url` host,
  and `url` must be one of them (a new `declare` rule). Leaf renewal trigger and CA lifetime. CA
  rotation = reissue every bundle (the `needs: reissue` path, C:2710).
- How the homeserver gets the cert and key (under (a)): `LoadCredential=` beside `config.toml`,
  so the config holds `/run/credentials/<unit>/<file>` (path from systemd, not shown in the repo,
  UNVERIFIED), versus a state-dir file (readable by the homeserver user). Credentials match C's
  "the homeserver cannot rewrite its own config" (C:340-341).
- Element: `NODE_EXTRA_CA_CERTS` in the launcher it writes (UNKNOWN whether the renderer honours
  it) or a manual trust-store install (not IaC). Needs a check first.
- Defence Before Fix: a rule that `http.Transport` / `tls.Config` under `teamchat/` is built in
  `matrix` only, the shape of `seamexec.go` and `rawconnprobe.go` in `src-go/qa/rules`.

---

## Item 4: a remote seat's token is issued over ssh

### Exists

- Issuing is `teamchat-admin member add|return|reissue`, root class (`admin/run.go:127`); the caller
  names the credential KIND (`--credential password|token`, `admin/verbs.go:103-111`, checked at
  `:339`). The answer is one JSON document on stdout whose `bundle` is the pool `seat.json` + one
  credential (`admin/verbs.go:301-309`; `seat.Issued` `seat/records.go:346-365`, `DecodeIssued`
  `:368-396`; built by `admin.SeatBundles.Bundle` `admin/bundles.go:26-52`).
- A push precedent: the host runs `guest place-bundle` with the issued JSON on stdin inside an LXC
  guest (`member/guest/places.go`, `OpPlaceBundle`; `host/lxc/lxc.go:104-108`).
- `teamchat-admin status` is non-root (`admin/run.go:143`).
- C 3.7.6 item 2: the remote pool gets the credential "over ssh (Plan 00205) or through the broker
  (Plan 00197)", `password` staying in the remote pool only (C:1497-1501). Plan 00205 is about the
  HOST reaching into members (`CLAUDE/Plan/00205-ssh-team-members/PLAN.md:10-16`), open decisions,
  no code. `research-remote-seat.md:159-163` lists what must never be on a less-trusted remote
  machine, the owner's credentials included.

### Gap

- No ssh use under `teamchat/` (non-test). No member-side entry point that writes a ccy pool seat
  from a remote `Issued`; `guest place-bundle` is the closest but writes a guest layout.
- `member add` needs root and the owner's password sudo (no sudoers file, 00209 PLAN.md Goals).
  Over ssh, sudo needs a tty or a password on stdin, and a tty mixes echo into stdout.
- If the REMOTE machine pulls, it holds an ssh credential for the homeserver host, which
  contradicts the less-trusted-machine posture.

### Decisions the spec must make

- Direction. (a) Push: the owner runs `sudo teamchat-admin member add` on the homeserver host and
  pipes the `Issued` document to `ssh <member-host> teamchat seat place-issued` (a new member verb);
  the remote machine holds no credential for the homeserver host. (b) Pull by the remote launcher
  over a restricted key with a forced command: needs a root path from a restricted key or a new
  non-root issue verb, which breaks "every account change needs the owner's password". Recommend (a).
- The member verb: reuse `seat.DecodeIssued`; write the pool seat through the store's fd writers
  (`seat/fs.go`) under the allocation lock; refuse a bundle whose canonical URL or `server_name`
  differs from the checkout record (item 2).
- ssh handling: `execx` with fixed argv, the secret on stdin only (`src-go/CLAUDE.md` "Secrets never
  in argv"), strict host-key checking, the target a member-machine alias from the owner's own ssh
  config, never a hostname in a declaration or in this repo. Whether a declaration names member
  machines at all (YAGNI: the target is a command-line argument).
- Credential kind for a remote seat: the pool password (a remote pool logs in per take, DC16 (c'))
  or token only (no password on the less-trusted machine, DESIGN-v2.md 7.1); the latter reissues
  over ssh at each session start. An owner choice.
- Revocation: `member park|retire` already kill the token; dropping a CIDR from `networks` and
  applying cuts reach; a revoked remote seat is found at its next failed login.

---

## Item 5: a seat handle never carries a real hostname

### Exists

- Grammar `<repo>.<seat>+<site>.<type>`, `site = [a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
  (`proto/ids.go:33,50`). The rule is stated: "never a hostname" (A:211), AR8 (A:66-67), C18
  (C:82). `NormaliseSite` folds case and turns every other character into `-`
  (`proto/ids.go:200-218`), so a dotted hostname becomes a hyphenated site, not a refusal.
- A site comes from only an LXC entry's optional `site` (`declare/declare.go:78,378-383`;
  `declare/values.go:101-104`).
- `guestfacts.Prefix` (`host/lxc/guestfacts/guestfacts.go:327-359`): the entry's `site`, else
  `HOOKS_DAEMON_HOSTNAME` read from the checkout's `ccy.env.local` (`:339-352`, `:503-533`),
  normalised, else `DefaultSite = "local"` (`:33`). The rule is stated three times and enforced in
  no code.

### Gap

- **A real hostname can reach a handle today** through `HOOKS_DAEMON_HOSTNAME`
  (`guestfacts.go:346-351`); normalisation hides nothing (`my-host` stays `my-host`). That the
  variable holds a machine hostname is inferred from its name.
- A ccy or host `cc` member has no site source (A defers to C, A:240-241; C 3.8 gives only LXC
  entries a `site`; Task 0.6 change 2 is open, PLAN.md:203-204). Two machines defaulting to
  `local` get indistinguishable handles (`research-remote-seat.md:162` wants a distinct label).
- Nothing refuses a site equal to a hostname the code can see: the machine's own (`os.Hostname` is
  never read under `teamchat/`), the homeserver host's, or the `url` host's labels.

### Decisions the spec must make

- Site source. (a) Declared only, a per-machine `teamchat_site` (as 0.6 suggested), default
  `local`, the env-file read removed. (b) From the `networks` entry that contains the machine's
  address (the object form of item 1): no extra setting, but a machine on two networks needs a
  tie-break. (c) Keep the fallbacks and add only a refusal. Recommend (a).
- A refusal rule: a site equal to the normalised machine hostname, any dot-label of it, or a DNS
  label of the `url` host is refused with a fixed message, in one place called from the
  declaration reader and `guestfacts.Prefix`, tested with a fake hostname seam. It is best effort;
  the real guarantee is that no code path reads a hostname into a handle.
- Defence Before Fix: the class is "a handle part derived from anything but the declaration".
  Candidate analyzer: `os.Hostname` and `net.LookupAddr` banned under `teamchat/` outside one
  reviewed file, with a red fixture; plus a test that `guestfacts` no longer names
  `HOOKS_DAEMON_HOSTNAME`.
- Handles already issued with a hostname site are permanent (C18): retire and reissue, or accept.
- Forge text is the stated reason (A:240-241); name the code that builds it from a handle and
  assert it in a test.

---

## Ordered build list (smallest independently testable slices)

Each slice is test first. Order: S1 before S2 and S3; S4 before S6 and S7; S5 before S6; S6 before
S8 and S9; S10 any time after S1.

01. **S1 `declare`: `networks`.** The field in `Team`, `values.go`, `declare.go`, `input.go`
    (`team.json` v 2), `gate.go` (gated, set compare), one function for the effective `AllowFrom`.
    Pure; unit tests.
02. **S2 `host` + `hostnet`: derive and consume.** The `Network` seam gains route-to-interface
    addresses (reusing `hostnet/firewall.go:165`); `listenInterfaces`, `renderConfig`, `ipAllow` and
    `wantedRules` read the one derived set; a golden render proves they are functions of `networks`.
03. **S3 LXC source check** reads the effective set (declared + bridge); a failing test first.
04. **S4 `teamchat/pki`.** CA and leaf generation, SANs from listen + url, validity and renewal,
    PEM round trip, injected clock. No network.
05. **S5 `matrix` TLS.** `Config.CAPEM`, exclusive roots, TLS 1.3, `memberBase` rules; tests on a TLS
    variant of `fakehs` (`matrix/fakehs/http.go:54` listens with `net.Listen`); a wrong-CA and a
    system-root-only certificate must fail.
06. **S6 `seat` bundle v2.** `tls_ca` replaces `plain_http_hosts` (decoder, `Issued`, `HeldBundle`,
    `MemberBundle`, `check`); `admin.SeatBundles` fills it; `guest check` input and `reach` carry
    it; the five member client builds pass it.
07. **S7 serve TLS.** The recorded Tuwunel TLS check first; then the chosen termination, the
    loopback clients adapted, the vendored key-list test extended; real-Tuwunel test under the
    `tuwunel` tag.
08. **S8 seat key and remote take.** Checkout record holds `server_name` + URL; a clash is refused
    naming both; fast-fail on an unreachable URL at creation; take-state per the decision. Tests at
    `seat.Store` level.
09. **S9 issuing over ssh.** `seat place-issued` on the member side; the push command through
    `execx` with the document on stdin; fake-runner tests plus a two-process or VM-lab acceptance.
    Blocked on the direction decision. Acceptance uses `teamchat` + `seat`, not ccy (Tasks 4.2, 4.3).
10. **S10 handle and site.** Site source per decision, env-file read removed, refusal rule and its
    Defence-Before-Fix detector with a red proof, policy for existing handles.
11. **S11 Element, docs, acceptance.** Element CA trust after its check; the declaration's doc page;
    the success criterion of PLAN.md:92-93 (a member on a second machine joins over TLS, token
    issued over ssh, an undeclared network refused; the refusal is provable on one machine with a
    second address, a firewalld source rule and the unit IP filter).

## Owner questions this surfaced

- Strict "plain HTTP only on loopback" puts every local ccy and LXC seat on TLS: intended?
- Push or pull for ssh issuing, given the less-trusted remote machine.
- Does the team backup include the CA key?
- May a routed CIDR bind the default-route interface?
