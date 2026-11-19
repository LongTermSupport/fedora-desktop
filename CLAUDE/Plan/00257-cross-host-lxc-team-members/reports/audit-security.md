# Audit: security (credentials, TLS and the wire)

Scope. Read in full (production files): `src-go/internal/teamchat/pki`, `tlsterm`,
`member/tlsverb`, `seat`, `member/placeissued`, `member/bundle`, `member/homefs`, `member/kit`,
`member/proof`, `member/forge`, `matrix` (`transport.go`, `client.go`, `seat.go`, `reach.go`,
`admin.go`, `sync.go`; `member.go` in part), `rooms` (first 330 lines), and of `proto`:
`rooms.go`, `verify.go`, `secret.go`, `refs.go`, and the limits of `strictjson.go` and
`content.go`. `matrix/fakehs` was read only for where it stops modelling Tuwunel;
`matrix/tuwunelhs`, `richtext` and the rest of `proto` were not read. Outside the area, read to
confirm findings: `internal/teams/teamchatlaunch/{adapters,launch}.go`,
`files/var/local/claude-yolo/lib/agent-bus-membership.bash`, `host/render.go:144-190`. Tests
run and green: `pki`, `tlsterm`, `seat`, `member/{placeissued,bundle,homefs,forge,proof}`.
Paths below are relative to `src-go/internal/teamchat/` unless they start with `src-go/` or
`files/`.

What holds up (checked, no finding): the member TLS client trusts exactly one CA, TLS 1.3 at
least, the URL host as the verified name, no redirect, no proxy, never the system roots
(`matrix/transport.go:80-106`, `:155-174`); plain http only for a loopback literal
(`matrix/client.go:673-694`); PEM parsing is strict (`pki/pki.go:249-265`); the seat store and
the member's files are opened by directory fd with `RESOLVE_BENEATH|NO_SYMLINKS`
(`seat/fs.go:23`, `member/homefs/homefs.go:54`); no error or log line carries a token, a
password, a PEM or server text; forge references are held to a grammar that cannot shape an API
path (`proto/refs.go:22-39`), and the forge client refuses a redirect off its origin
(`member/forge/forge.go:139-147`); room power levels close everything but `m.room.message`
(`proto/rooms.go:190-202`) and members verify every anchor by admin sender
(`proto/verify.go:80-98`); Tuwunel is rendered with registration, display-name changes, room
creation, encryption, federation and token logins off (`host/render.go:155-178`).

## Findings

### SEC-1 HIGH: a take that fails at the homeserver leaves a holding the next launch treats as held

- `seat/take.go:89-98`, `seat/take.go:315-323`;
  `src-go/internal/teams/teamchatlaunch/adapters.go:99-106`, `:121-124`;
  `files/var/local/claude-yolo/lib/agent-bus-membership.bash:67-74`, `:103`.
- What is wrong. `Take` writes `held/held.json` (step 2) before it mints (step 3). Only a
  refused login removes `held/` again; an unreachable homeserver, a timeout, a 429 or any other
  mint failure leaves a complete `held.json` naming the session, with no token and no session
  seat directory. `TakeAll` does not add the failing take to `taken`, so it is not released.
  The launcher returns on a failed `launch-args` before it sets `CCY_BUS_DISCONNECT`, so its
  exit hook releases nothing either. On the next launch `heldBy` sees a complete holding of the
  same seat by the same session and skips the take: "kept as it is and not taken again".
- Failure scenario (suspend). The bus VM is suspended. A session is launched or relaunched: the
  take fails `unreachable`. The bus resumes. Every later launch under the same session name
  succeeds without taking: no `sessions/<session>/<team>/` exists, the member finds no bundle
  and never links. A different session asking for that seat gets `ErrHeld` for as long as the
  first session's registry record exists (`seat/take.go:224-239`). A launch killed between
  step 2 and step 7 for any other reason ends the same way.
- Why tests miss it. `seat/reach_test.go:93-114` retakes through `Store.Take` with a session
  the fake does not mark restorable; nothing drives `TakeAll` twice over a failed mint.
- Fix. Make `Take` undo its own step 2 on every mint or copy failure (unlink `held.json`,
  rmdir `held/`, as it already does for `ErrReissue`); no token is kept at that point, and the
  next mint's logout ends any the server made. Make `heldBy` require the session seat directory
  and `held/token`, not just `held.json`. Add a `TakeAll` test: fail the mint, take again.
- Known weakness: none (new). Bears on the suspend requirement directly.

### SEC-2 MEDIUM: an empty or partial `held/token` blocks release and every later take

- `seat/fs.go:128-142`, `seat/take.go:325`, `seat/release.go:416-427`, `:199-217`.
- What is wrong. `createAt` leaves the file in place when its write or sync fails (homefs
  unlinks: `member/homefs/homefs.go:199-201`). `retire` then reads `held/token`, and
  `ParseSecret` refuses an empty or cut value, so the release errors. `clearHeld` returns that
  error to every take. The "incomplete holding" path only judges `held.json`.
- Scenario. Disk full or power lost while the token is written: the seat can be neither
  released nor taken until someone removes the file by hand.
- Fix. Unlink on a failed create; in `retire`, treat a token that does not parse as nothing to
  retire (the next mint's logout ends the device's tokens) and go on to quarantine.

### SEC-3 MEDIUM: homeserver calls run under the per-checkout allocation lock

- `seat/take.go:63-99` (whole take under the lock), `:215` (a take releases an ended holding
  with `alreadyLocked`: notice post and logout), `seat/lock.go:20`, `seat/seat.go:82`,
  `matrix/client.go:49` (15 s a try).
- What is wrong. `Release` is careful to call the homeserver outside the lock
  (`seat/release.go:57-59`), but a take logs in, logs out and logs in again, and may post a
  disconnected notice and retire a token, all with the lock held. The lock is per checkout, not
  per team, and waits 30 s.
- Scenario (suspend). Team A's bus VM is suspended. A launch naming a seat of team A holds the
  lock through its timeouts; a launch or `place-issued` for team B in the same checkout gets
  `ErrBusy` ("another run holds the allocation lock"). One sleeping homeserver stalls seat work
  for every team of the checkout.
- Fix. Do the mint outside the lock (claim under the lock, mint, then re-check the claim and
  write under the lock, as `releaseHeld` already does with `finish`), or key the lock by team.

### SEC-4 MEDIUM: the terminator can be filled by one unauthenticated client, silently

- `tlsterm/tlsterm.go:48-52`, `:234-241`, `:288-301`.
- What is wrong. 256 slots, shared by every source address; a client that completes a TLS
  handshake (no credential needed, and the CA is not secret) and sends one byte every two
  minutes keeps a slot for ever. When the slots are full the next connection is closed with no
  log line (`logf` is called only for upstream and close failures).
- Scenario. Any host in an admitted network, or any container on the bridge (K16), opens 256
  connections: every member's sync and send is refused, and the journal shows nothing.
- Fix. Cap connections per source address; log the first refusal of each minute; give a relay
  that has sent no complete request a shorter bound than `IdleLimit`.

### SEC-5 MEDIUM: the terminator exposes the whole homeserver, admin API included

- `tlsterm/tlsterm.go:252-264`, `matrix/admin.go:15-19`, `:73-92`; `host/render.go:158`.
- What is wrong. The terminator relays bytes, so every path Tuwunel serves on loopback is
  served to every admitted network: `/_synapse/admin/v1/register` (guarded only by the
  shared-secret MAC) and the rest of `/_synapse/admin` (guarded only by the admin token). The
  design text says the admin client "reaches only http://127.0.0.1"; the server side does not
  hold the other end of that. Tuwunel also sees every client as `127.0.0.1`, so any limit it
  keys by address is one bucket for the whole team (known, weaknesses.md section 8).
- Scenario. A leaked registration key or admin token (a backup, a log) is usable from any
  member machine, not only by root on the bus host.
- Fix. Give the terminator a path rule (refuse anything outside `/_matrix/client/`), which
  needs it to read the request line, or front Tuwunel's admin routes with a second loopback-only
  listener. Either is also where a join-code endpoint would live (see below).
- Known weakness: extends W-section 8 ("Tuwunel cannot tell members apart by address").

### SEC-6 MEDIUM: the fake homeserver stops where parked and retired seats begin

- `matrix/fakehs/fakehs.go:630-633`, `:757-760`, `matrix/fakehs/admin.go:265-267`;
  `seat/credential.go:53-56`, `:69-77`, `:80-86`.
- What is wrong. A request by a locked account and a sign-in to a deactivated one are
  "unmodelled" (answered 500). So the branches that decide a parked or retired seat (`Mint`'s
  refused login to `ErrReissue`; `Retire` counting only `M_UNKNOWN_TOKEN` as done) are proven
  only where a real Tuwunel binary is present.
- Scenario. If Tuwunel answers a locked account's logout with another 401 errcode, `Retire`
  errors on every try, the release never finishes, and `checkState(StateRetired)` can never
  quarantine the pool record (`seat/take.go:128-136`).
- Fix. Record those exchanges against the pinned Tuwunel (as probe H4 did), model them, and
  test park and retire while a seat is held. Decide in `Retire` what a locked or deactivated
  account's refusal means.

### SEC-7 MEDIUM: quarantine is written and never emptied

- `seat/take.go:399-422`, `seat/release.go:439-463`, `seat/place.go:170-180`.
- What is wrong. Every release moves `held/` and the session's seat directory into
  `released/`, every reissue moves the old pool record there, and nothing under
  `internal/teamchat` reads or removes them (the only users of `ReleasedPool` and
  `ReleasedSession` are these writers and `paths`). Only the named credential file is unlinked;
  the session copy keeps its token file (dead once `retire` ran, live if `retire` failed) and
  the member's store.
- Scenario. A long-lived checkout grows one pair of directories per session end, each holding
  message state and a token file, mode 0700 but readable by any process of the owner.
- Fix. A bounded sweep (by age, under the allocation lock) and unlink of `token` in the session
  copy as part of `finish`. Not searched: whether `ccy-teams` sweeps it elsewhere.

### SEC-8 LOW: the seat store trusts the umask for secret files

- `seat/fs.go:129` against `member/homefs/homefs.go:187-192`; `member/bundle/bundle.go:154`.
- The store creates with mode 0600 and no `fchmod`; the member refuses a token that is not
  exactly 0600. A restrictive umask (for example 0277) gives 0400 and a seat the member rejects
  with "not mode 0600". Fix: `fchmod` as homefs does, or share homefs (see debloat).

### SEC-9 LOW: `place-issued` reports misleadingly after a late failure

- `member/placeissued/place.go:122-126`, `seat/place.go:86-103`, `:158-166`.
- `Fill` ends the claim before it writes, so `Abandon` after a failed write returns "the claim
  is already filled or abandoned", which is joined to the real cause. And a failure of the
  checkout record write (`p.write()`, after the swap) reports the placement failed although the
  seat is in place. Fix: abandon only when `Fill` failed before `end`; write the pin before the
  swap, or report "placed, record not written".

### SEC-10 LOW: the CA is unconstrained and has no overlap at rotation

- `pki/pki.go:133-143`, `pki/trust.go:22-30`.
- The CA carries no name constraints and lives ten years; a member holds exactly one CA, so
  `rotate-ca` is a hard cut (known, W6). For the join-code design, let a bundle carry two CA
  certificates during a rotation window. Name constraints would limit a stolen CA key to the
  team's own addresses.

### SEC-11 LOW: the transcript watcher goes quiet when its directory goes

- `member/proof/watch.go:161-170`.
- An `IN_IGNORED` for the directory watch (the directory removed or its filesystem unmounted)
  matches no case; the watcher keeps reading an inotify fd that will never fire, and `Close`
  reports no failure. Fix: treat `IN_IGNORED` on the directory as the watch's end.

## Suspend and resume, in this area

- SEC-1 and SEC-3 are the two places a suspended bus host leaves lasting damage or stalls
  unrelated teams. Both are in seat taking, not in the link.
- The terminator's timers (`HandshakeLimit`, `IdleLimit`) are Go timers, which do not count
  time asleep: after a resume the bus host keeps each pre-suspend relay up to two more minutes,
  then frees its slot (`tlsterm/tlsterm.go:336-346`). Members have long since redialled; with
  256 slots this is harmless unless SEC-4 is in play.
- The forge checker judges "down", rate holds, the token's life and its caches on the boot
  clock (`member/forge/forge.go:181-205`, `member/forge/token.go:87-92`), which counts time
  asleep: correct across a suspend.
- Certificate validity is judged on the wall clock (`matrix/transport.go:101`); see U1.
- The seat store's own waits (`allocWait`, `releaseWait`, `noticeBound`) compare wall-clock
  readings (`seat/lock.go:40-55`, `seat/release.go:255-266`, `:362-377`): a suspend mid-wait
  ends the wait on wake, which is the safe direction; a wall clock stepped backwards on resume
  lengthens `noticeBound` by the step.

## The join-code design: what to reuse and what must change

Reuse as it is:

- `pki.Fingerprint` and `pki.Pool` (`pki/trust.go`) for the pin the code carries.
- `seat.DecodeIssued`, `Store.Claim`, `Claim.Fill`, the checkout pin (`ErrHomeserverClash`) and
  `QuarantineLeftovers`: `seat join` should end in exactly `placeissued.placeIn`, fed from the
  network instead of stdin.
- `matrix.NewMember` and its pin for every call after the CA is known.

Must change or be added:

1. A fingerprint-pinned first contact. `newPin` takes a CA PEM, not a fingerprint, and the
   terminator presents the leaf alone (`tlsterm/tlsterm.go:201-205`). Either the code carries
   the whole CA certificate (about 550 base64 characters: paste, not type), or the terminator
   sends leaf plus CA and the client checks `sha256(CA DER)` against the code before building
   the pool. The second needs `ParseLeaf`'s one-block rule kept for the leaf file and the chain
   assembled at serve time. This is a third place that builds a `tls.Config`; X19 says only
   `matrix` and `tlsterm` may, so put it in `matrix`.
2. Something to redeem against. Tuwunel has no such endpoint and the terminator is a byte
   relay. Options: (a) no new server: `member invite` creates or resets the account with a
   one-time password, the code is url + fingerprint + user id + that password, and `seat join`
   logs in, then replaces the password with one it generates (needs password change by the
   member, or an admin-side step); (b) an HTTP-aware listener on the bus host holding the
   issued document under the code's hash. (b) is cleaner (single use and expiry are enforced
   in one place; the pool password never crosses as a reusable secret) and shares the path
   rule SEC-5 asks for.
3. Expiry judged on the wall clock at redemption, not by a timer: a Go timer does not run
   while the bus host sleeps, so a code minted before a suspend must still be dead after it.
   Store `expires_ms` and compare on redeem; sweep on the health tick.
4. Single use that survives a crash: mark the code spent (fsync) before the answer is sent,
   and make the answer re-issuable by `member reissue`, since a spent code whose answer was
   lost in transit cannot be replayed.
5. Store only a hash of the code's secret on the bus host, compare in constant time, and keep
   the code out of argv on the joining side (read it from stdin or a prompt, as
   `stdinread` already does for `place-issued`).
6. Rate-limit redemption per source at the listener, because Tuwunel's own limits see one
   address (SEC-5).
7. SEC-1 to SEC-3 first: a join that places a seat and then cannot take it reproduces today's
   wedge on every new machine.
8. The bundle's `base_url` per seat kind (REPORT.md section 2) belongs in the issued document
   the code redeems, so the code itself need only carry the address the joiner dials.

## Unconfirmed

- U1. A TLS failure of class "expired or not yet valid" is reported non-transient
  (`matrix/client.go:463-467`), and a leaf is valid from one hour before it was made
  (`pki/pki.go:70`). A member whose wall clock is more than an hour behind after a resume,
  meeting a leaf renewed meanwhile, gets a non-transient error; `member/engine/sender.go:373`
  and `member/teamsync/lookup.go:212` branch on `Transient()`. Not traced: whether either
  gives a send or a link up for good on it.
- U2. Forge existence oracle. A receiver checks a peer's reference with its own `gh` token
  (`member/forge/forge.go:524-528`) and tells "provenance" (a private repository exists,
  `:311-312`) from "unresolved" (not found). If that outcome is posted back to the room, a
  member can probe repositories only another member's login can see. Not traced: whether the
  verdict text reaches the sender.
- U3. Login limits. `host/render.go:186-187` renders failed-login and per-account login
  limits. If Tuwunel keys the failed-login limit by address, every member shares
  `127.0.0.1`, and three bad logins from anyone block every take's `Mint` for about five
  minutes. Needs a probe against the pinned Tuwunel.
- U4. Transaction ids across a re-take. A take logs the device out and in again under the same
  device id (`seat/credential.go:52-66`). The fake dedupes a transaction id per user and device
  id (`matrix/fakehs/rooms.go:505-517`); if Tuwunel forgets them when the device is logged out,
  an outbox entry retried after a release and re-take is stored twice. Needs the same probe.

## Bloat and debloat candidates

- Four root-fd file layers do one job: `seat/fs.go` (333 lines), `member/homefs/homefs.go`
  (527), `member/kit/kit.go:180-268`, `member/proof/scan.go:172-273`. homefs is the most
  careful (fchmod, unlink on failed create). Port the seat store and the kit to it and delete
  `seat/fs.go`; that also closes SEC-2's first half and SEC-8.
- Three DNS-name grammars: `pki/names.go:17`, `matrix/client.go:655`, `seat/homeserver.go:23`.
  One in `proto` or `paths`.
- `seat.Credential` is a seam with one implementation (`Password`); `KindToken`, `(d1)` and
  `(d2)` are speculative (`seat/credential.go:19-28`, `seat/take.go:385-397`). If join codes
  land, decide the one credential and drop the seam.
- `seat/records.go:242-272` parses each record twice (proto's strict parser, then
  `encoding/json`) and needs `exactEntryKeys` (`seat/homeserver.go:90-108`) to cover the gap
  between them. Decode from `proto.Value` once.
- `member/placeissued/read.go:93-154` accepts two document shapes (the issued document, or the
  admin verb's answer around it). With join codes the stdin path and both shapes can go.
