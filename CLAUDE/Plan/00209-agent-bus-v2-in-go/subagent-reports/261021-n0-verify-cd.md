# Plan 00209 N0 fix pass, verification: scope cd (C host, D migration, DC19, their seam and decision rows)

Verifier, not author. Read at worktree tip 31a1d598 (fix pass cf5c57d9..31a1d598): DESIGN-v2.md, the
FRAME, the four review files, the host and migration dispositions, the merge report, C sections
1-8 in full, D 3.5-3.9 and 5.1-8, and the parts of B that C and D depend on (header, 2.6 steps 01
and 08, 2.10, 2.11, 2.13).

## Verdict: FIX-FIRST

0 blockers, 6 new majors, 7 new minors. The coherence blocker is fixed and the LXC path is one
mechanism. Most host and migration majors are fixed in the text. But the DC16 (c') seat rework
does not hold across a restore. It also gives every owner-uid process power to change seat
accounts, which the C10 and DC19 boundary says only the password has. The DC18 gate can be
bypassed. The LXC unit's `ExecStart` cannot start B's `run`. The "health tick for every kind"
guarantee for `disconnected` does not exist for ccy seats.

## Blockers and majors in scope

| Finding                                             | State          | Evidence                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| coherence 1 (blocker): LXC member never active      | VERIFIED-FIXED | C 3.9.2 step 8 runs `guest membership` with the full `desired.json` after placement; the absent phase writes a `parked` farewell, waits 60 s by L3, then `guest remove`, then `member park` (C 3.9.2 absent 1-5); B 2.10 (last paragraph) and B 2.13 agree; B 2.10 handles a `parked` farewell |
| coherence 2: two LXC kit and unit mechanisms        | VERIFIED-FIXED | C 3.9.1: root writes only system paths host-side, the agent runs `guest check\|place-bundle\|membership\|remove`; B 2.13 has no `install-self` or `unit` op, and one GC rule (`session.pid` plus start time). Path nit: new minor 2                                                            |
| coherence 5: admin posts only parked/removed        | VERIFIED-FIXED | C 3.6.3 table and "What the admin's account posts": `added parked removed role`, carried items, the `moved` welcome                                                                                                                                                                            |
| coherence 6: welcome has no wire form               | VERIFIED-FIXED | merge took A 7.4 `moved`; C 3.6.3, D 5.6 "Link (M4)" and D 6.2 agree (`to` every non-parked agent member, pinned, `body` the welcome)                                                                                                                                                          |
| coherence 7: parked coordinator; roster write       | VERIFIED-FIXED | C 3.6.1 (`proto.Roster(...)`, validated, `rev` = max+1, file rev after acceptance); C 3.6.3 every verb ends with the rooms step; park kicks a coordinator from questions                                                                                                                       |
| coherence 8 (C part): `bus/` packages               | VERIFIED-FIXED | C 3.6.1 `internal/teamchat/state`, C 3.7.3 `internal/teamchat/seat`                                                                                                                                                                                                                            |
| coherence 10: `teamchat-claude` unspecified         | PARTIAL        | removed (C 3.9.4, D 3.6); but C's unit `ExecStart` does not match B's `run` grammar (new major 4)                                                                                                                                                                                              |
| coherence 11: coexist scenario order                | VERIFIED-FIXED | D 4.3 staged N2/N3/N4                                                                                                                                                                                                                                                                          |
| coherence 12: A7 reads an outbox `status` lacks     | VERIFIED-FIXED | D 6.1 A7 + C 3.3 acceptance probe (own runtime, own `status` `outbox`); see new minor 7                                                                                                                                                                                                        |
| security 1: released token skips messages           | PARTIAL        | closed for a release (C 3.7.3 take step 3 and release step 4, `released_ms` in C 3.7.2, B 2.6 step 01 b); reopened by any restore (new major 1)                                                                                                                                                |
| security 2: security policy in agent-writable file  | PARTIAL        | DC18 in C 3.5.1; bypassable (new major 3)                                                                                                                                                                                                                                                      |
| security 3: password does not guard seat placement  | PARTIAL        | C10, DC19, K12 and the FRAME are narrowed; but (c') widens what an owner-uid process can do beyond what they state (new major 2)                                                                                                                                                               |
| security 4: tick cannot open `/proc/<pid>/root`     | VERIFIED-FIXED | C 3.11.1 `CAP_SYS_PTRACE` in the health unit only, `ptrace`/`process_vm_*` filtered, K14, H-C10 inside the sandbox. Write path: new minor 1                                                                                                                                                    |
| security 5: container files choose reposts          | VERIFIED-FIXED | D 5.2 rows 2 and 4 (candidates, grammar, cap, admin fetch plus addressing rule, `unverified_candidate`), row 6b (questions items reported, never carried), 5.2.1                                                                                                                               |
| operations 1 (C part): goodbye killed, LXC unit     | VERIFIED-FIXED | C 3.9.4 default `KillMode`, `TimeoutStopSec=30`; consistent with B 2.11 rules 2-3 (the link ignores `SIGTERM`/`SIGHUP`, keeper end about 6.5 s) and C 7 B item 09                                                                                                                              |
| operations 2: never-resumed session never announced | PARTIAL        | LXC: C 3.11.2 step 6. ccy: launcher at `podman` exit (C 3.7.5). The "tick for every kind" claim is false for ccy (new major 5)                                                                                                                                                                 |
| operations 3: restore and members' sync             | VERIFIED-FIXED | C 3.12.2 step 5 `restore_epoch`, B 2.6 step 08 direct state read after every outage, step 7 export, `--accept-carry-loss` against `carried_at`                                                                                                                                                 |
| operations 4: staged copy blocks later restores     | VERIFIED-FIXED | C 3.12.2 record first (step 3); failure in 3-5 discards; from step 6 on, kept as `staged` with `--resume` and `--discard`                                                                                                                                                                      |
| operations 5: crash serves revoked credentials      | PARTIAL        | ordering fixed (secured in staging, step 5, before the swap; block covers only the renames); the staging instance as specified cannot serve (new major 6); pool logins not secured (new major 1)                                                                                               |
| operations 6: LXC B/C contradiction, no desired     | VERIFIED-FIXED | with coherence 1 and 2                                                                                                                                                                                                                                                                         |
| operations 7: linked set ignores LXC                | VERIFIED-FIXED | D 5.9, M0                                                                                                                                                                                                                                                                                      |
| operations 8: stopped guest runs v1 and v2          | VERIFIED-FIXED | C 3.9.2 step 3 (`ReadGuestV1Session`, `DEFERRED (v1 not yet absent)`), D 6.3, D 5.5                                                                                                                                                                                                            |
| operations 9: A8/M8 count M6's own events           | VERIFIED-FIXED | D 5.2 "A post after the fence", used by M3, commit precondition 3, A8 and M8 (1)                                                                                                                                                                                                               |
| operations 10: lost fence before commit             | VERIFIED-FIXED | D 5.4 M3/M4 (snapshot void, only R), M6 re-fence, D 5.7 step 1 `fence-lost`                                                                                                                                                                                                                    |
| operations 11: M8 `systemctl` drift                 | VERIFIED-FIXED | D 5.4 M8 (4), 5.5 `SetV1TeamState`, 5.6, D17 sentence in 5.5                                                                                                                                                                                                                                   |
| operations 12: M0 ignores ccy versions              | VERIFIED-FIXED | D 5.4 M0                                                                                                                                                                                                                                                                                       |
| operations 13: unreadable outboxes                  | VERIFIED-FIXED | D 5.2 row 5, `unreadable_outbox`                                                                                                                                                                                                                                                               |
| quality 6: no redaction                             | VERIFIED-FIXED | C 3.6.5, DC20                                                                                                                                                                                                                                                                                  |
| earlier C major 43: restore backup deletes N        | VERIFIED-FIXED | C 3.12.2 step 4 stages before step 6 takes `M`; failure after `M` keeps the staged copy                                                                                                                                                                                                        |
| earlier C major 44: crash leaves empty database     | VERIFIED-FIXED | `AssertPathExists=!...restore.block` (C 3.2.3), block written before the stop, record steps `swap`/`start`, resume rules                                                                                                                                                                       |
| INT3 and the migration fixer's four fixes           | PARTIAL        | D 3.5, 3.9, 5.1, 5.10 and 7.2 step 3 hold; C 3.8 still has the old acceptance wording, and C's `team remove` deletes the record D's rolled-back resolution needs (new minors 3 and 4)                                                                                                          |
| D's last seven round-3 fixes                        | VERIFIED-FIXED | re-read: M8 order (now by declaration), `possible_duplicate` on receipt and join cuts, R's `state_check` (with `fence-lost`), literal per-family reset rules, RK1 and `After=`, archive check (no unit start), the 5.9 leave before any set record                                             |

## New findings

### 1. major: a restore brings released sessions' tokens back and kills every live ccy session's token (C 3.12.2 step 5, DC16, K13)

Under (c') a pool seat's logins are not roster writes. Only the password set is recorded
(`cred_seq`). Step 5 revokes `retired`, `parked` and `needs: reissue` handles and humans whose
`revoked_seq` is after `N`. An `active` pool seat whose password predates `N` is not touched.

- The restored database holds the `TCSEAT` token that was live at backup `N`. If that session was
  released after `N`, its logout is undone. A hostile released session's token works again until
  the next take logs in on that device. K13 reopens on every restore, and the secured-staging
  claim ("no crash serves a revoked credential") is false for pool seats.
- Every session holding a seat at restore time logged in after `N`, so its token is unknown to the
  restored database. Each live ccy member gets 401 (B 2.6 step 08: permanent for that team, exit
  21). Nothing in C or B re-takes the seat. The design says nothing of what a restore does to live
  sessions.

Fix:

- In step 5, delete or log out every pool seat's device in the staged copy. Use the admin device
  API, or give every pool seat `needs: reissue`.
- State the recovery for held ccy seats: ccy-teams re-logs each held seat from the pool password
  after the restore and replaces the session's `token` (B watches `token`, 2.6 step 08).
- Add both to H-C15 and to the 3.14 restore tests.

### 2. major: the pool password lets any owner-uid process change seat accounts, which C10, DC19 and the FRAME say needs the owner's password (C 3.7.1-3.7.3, DC16, DC19, K12, FRAME line 44)

Under (a), a host process reading the pool got a token. A token cannot pass UIA. Under (c') it
gets the account password, which can:

- change the password through `/account/password` with UIA, locking the pool out until
  `member reissue`;
- deactivate the account through `/account/deactivate`. The handle is then permanently unusable:
  a coordinator's handle can be destroyed;
- log in with any other device id. That gives a second valid credential, breaking C23 ("at most
  one credential is valid per member"). The release's logout of `TCSEAT` never kills it.

K12 says such a process "can read every seat (the pool's passwords included)". But C10 and DC19
state that the password guards every change to accounts, and DC16 says "a hostile holder cannot
keep the seat". Both are true only of containers. DC16 is flagged VISIBLE to the coordinator, so
the trade it describes must be the real one.

Fix:

- State these powers in K12, DC16 and DC19, and in the FRAME sentence.
- The health tick lists each pool seat's devices (it already reads device counts for humans). It
  reports any device other than `TCSEAT`, and an account found deactivated, as a problem with its
  fix.
- If Tuwunel can turn off password change for non-admin users, set it in the config.

### 3. major: the DC18 approval gate is bypassable (C 3.5.1, 3.6.1, 3.6.4, C46)

Only `declare check` (step 2) and `team apply` compare against `approved.json`. Other paths get
past it:

- `role apply` is run directly by ccy-teams "with the team's declaration on stdin". Every member,
  role and human verb then rewrites the roster event as `proto.Roster(roster.json, declaration, rooms)`, and that declaration carries `trust`, `humans` and `coordinator`. None of these verbs
  checks `approved.json`.
- "ccy-teams' own flows that change a security field run `team approve` inside the password-sudo
  session they already open, so the owner is not asked twice". `team approve` takes the whole
  declaration on stdin.

Scenario: an agent adds `example-org/attacker-repo` to `trust.repos`. The owner later adds a human
from the board. The flow approves the whole team entry and never shows the `trust` diff, and the
next roster write publishes the planted trust.

Also, `teamchat_lxc_members` is not a gated field. A declaration edit can put an existing guest
user into a team (an account, a token, `human_text`), and the next unrelated play creates it.

Fix:

- Every verb that renders the roster event uses `root/<team>/team.json` (the applied, approved
  declaration), never stdin. Or it compares stdin with `approved.json` and refuses with 70.
- An in-flow `team approve` shows the full security diff of the team entry, and refuses if any
  field other than the one the flow changed differs, naming `ccy-teams approve`.
- Add LXC membership entries (team, container, user, `human_text`) to the gated fields.

### 4. major: the LXC unit's `ExecStart` does not match B's `run` grammar (C 3.9.4 against B 2.2.1, 2.4.1, D 6.3)

C's unit text, which is "C's alone", runs `/usr/local/bin/teamchat run --kind lxc`. B's verb is
`run --claude|--plain --kind ... [--desired DIR] -- CMD` (B 2.2.1 row, 2.4.1). For LXC it is
`teamchat run --claude --kind lxc --desired <v2-desired> -- claude`, which D 6.3 prints as the
hand-run form.

As written, the supervised session has no mode, no command and no desired directory. It exits 64,
and `Restart=always` loops every 10 s. Every `service: true` member, the N3-first member kind,
never starts.

Fix: `ExecStart=/usr/bin/tmux -L teamchat new-session -d -s teamchat /usr/local/bin/teamchat run --claude --kind lxc --desired <home>/.config/teamchat/membership -- claude`. Add a test that
renders C's unit and parses its command line with B's argument parser.

### 5. major: "the health tick for every kind" posts `disconnected` only for LXC; ccy seats have no tick (B 2.11, BD9, seam row 27, C43 against C 3.11.2)

B 2.11 says the tick runs "for every seat it saw held and now finds free ... for every member
kind", and that "the tick is the guarantee". BD9 (VISIBLE) and row 27 say the same. C's tick (C
3.11.2 step 6) handles LXC handles only. It cannot do more:

- it runs as root with `ProtectHome=yes`;
- a ccy seat's lock and its `held/` bundle are in the owner's `~/.local/state/teamchat/seats`;
- root using an owner file as a credential is what C24 forbids.

For ccy, C 3.7.5 relies on the launcher at `podman` exit, and otherwise on "the board in the
background" and "the board posts on its first open after" a reboot. Scenario: the host loses
power with ccy sessions running, and the owner does not open the board for a day. No one is told
for a day, against BD9's "announced within about one tick" and C43's "as soon as the host sees it
gone".

Fix: choose and state one of these:

- correct B 2.11, BD9, row 27 and C43 to "launcher, else the next board open" for ccy;
- add an owner-side user timer (`teamchat-seats.timer`, ccy-teams as the owner) that runs the
  same release and notice pass each minute.

### 6. major: the restore's staging instance cannot serve as specified (C 3.12.2 steps 4-5, DC8)

The staging unit has "the base unit's whole sandbox", and the base unit has
`IPAddressDeny=any` and `SocketBindDeny=any`. The allow lines are in the per-team drop-in, which
the transient unit does not get. Its config is "the team's current config with one key changed
(`database_path`)", so it still lists the team's `listen` addresses. Those addresses do not exist
in a `PrivateNetwork=yes` namespace.

So Tuwunel cannot bind (EADDRNOTAVAIL, or a `SocketBindDeny` refusal). Even on loopback, the BPF
address filter (cgroup-wide, loopback included) drops the secure child's traffic. Every restore
fails at step 4 or 5. H-C5 would find this, but its stated fallback ("restore into the live path
after the stop") is option (b), the window operations 5 closed.

Fix:

- `config.restore.toml` also sets the listen address to loopback only.
- The staging unit adds `SocketBindAllow=tcp:<port>` and `IPAddressAllow=127.0.0.1/32 ::1/128`.
- The secure child's unit allows loopback the same way.
- Name all three in H-C5 and H-C15.

### Minors

1. C 3.11.1: the health unit's `ReadWritePaths=/var/lib/teamchat-status /run/teamchat-admin`
   (with `ProtectSystem=strict`) does not cover `root/<team>/lxc-online.json`, which step 6 writes.
   Every tick for an LXC team would get EROFS. Fix: add the file's directory, or move the file
   under `/var/lib/teamchat-status` or `/run/teamchat-admin`.
2. The kit binary path disagrees between sections. C 3.9.1's `lxc-attach` runs
   `/usr/local/lib/teamchat/kit/<digest>/teamchat`, while C 3.9.2 step 4, D 3.5 and B 2.13 lay the
   kit out as `<digest>/bin/teamchat`. Fix: `.../<digest>/bin/teamchat` in C 3.9.1.
3. INT3 fix 1 reached D only. C 3.8 still says an id that is also a v1 team "is accepted only
   while that team's migration set is at `preflighted` or later". That is the ambiguous wording the
   fixer found: a new v1 team named after a migrated short id is accepted through the v2 team's
   own set. The fixer's request 5 ("pairs that very v1 name with that id") is not in C. Also,
   `declare check`'s stdin (C 3.5.1 step 2) carries only `agent_bus_teams` ports, not the names
   and set records that check needs. Fix: copy D 5.1's rule into C 3.8, and name its inputs in
   step 2.
4. C 3.5.3 `team remove` deletes the status record. D 5.7 step 5 runs `team remove --purge`, then
   says the status record "keeps `"origin": "migration"` and its pointer to the set". D 3.9 (INT3
   fix 3, a rolled-back short id "refused, naming that v1 name") reads it. Fix: `team remove` keeps
   the status record for a team whose `origin` is `migration`, or R rewrites a minimal one.
5. B 2.10's first paragraph still says `<v2-desired>` is "host-written ... a directory the member
   reads but cannot write". For LXC that is false, by B 2.10's last paragraph and C 3.9.1. Fix: say
   "ccy" there.
6. C 3.12.2: the "every root verb ... refuses while `restore.json` exists" list omits `lxc apply`
   (which runs member add and park), `redact` and the `migrate` verbs. Fix: "every root verb that
   takes the team lock".
7. C 3.3's acceptance probe runs `teamchat run --plain --kind host` "in a fresh 0700 runtime
   directory ... not the seat store". But B's header fixes `<v2-state>` for host kind at
   `~/.config/teamchat/seats` (the owner's own home), and `run` has no flag that moves it. Fix:
   give `run` a state-root flag for the probe, or name the probe's paths in B.

## Checked and found sound

- **Coherence blocker:** the present phase runs `guest membership` after placement, once per user
  under the play lock. The host copy at `/var/lib/teamchat-admin/lxc/...` keeps `generation`. The
  absent phase waits for the seat before it removes and parks, and the parking holds the team lock,
  so the tick draws no false notice. There is one write mechanism per path class, and there are no
  guest programs as root.
- **DC16 (c') against B, for releases:** the take logs in with `TCSEAT` (`M_FORBIDDEN` stops it,
  naming `reissue`). The release posts the notice, then logs out, then writes `released.json`. A
  transient failure keeps `held/`. B starts from the earlier of the receipt and `released_ms`, and
  marks items before it `restored`. B's test row 1999 matches. Apart from new majors 1 and 2, the
  release side of security 1 is closed together with B.
- **Disconnected mechanics:**
  - `tc-disc-<claim>` (ccy) and `tc-disc-<run id>` (LXC) are each scoped to the one token in use.
  - The run id is in the seat lock with a checked 16-hex grammar.
  - The `since` rule is the same in B 2.11 and C 3.11.2.
  - `back` is posted by the link only after a `disconnected`, and the tick reads it.
  - The launcher's `podman`-exit path keeps seats held.
  - The unit text (`TimeoutStopSec=30`, default `KillMode`, `Before=lxc.service` on the
    homeservers) is consistent with B 2.11's three rules.
- **Restore ordering:** the record comes first. The order is stage, secure, `M`, export, then the
  block and the swap. Failures before `M` discard the staging, and failures after it keep it. The
  resume rules are idempotent. The merged roster compares by `seq`, not by clock. (This holds
  apart from new majors 1 and 6.)
- **Health and `CAP_SYS_PTRACE`:** the capability is held by this one unit, with the filter. The
  tick opens only through the guest root fd, and H-C10 runs inside the sandbox. Yama does not
  affect `PTRACE_MODE_READ`.
- **Seat moves without a password:** C10, DC19 and K12 now state them (apart from new major 2).
  The launcher's mount refusal covers ancestors, equals and insides, on real paths.
- **v1-inbox re-post risk:** closed. Names are candidates only, the admin fetches each event and
  applies the addressing rule, and questions-room content is never carried into main.
- **v2 touches nothing of v1:**
  - The fence is a v2 nftables table and the v2 `teamchat-fence@` unit. Its only v1 reference is
    the `Before=` ordering, which is allowlisted.
  - The play never touches fence instances.
  - M8 stops the archive through v1's own declaration and play, so no `systemctl` acts on a v1
    unit.
  - The adapter's writes are v1 verbs only, and the drop-in fallback is the one named exception.
  - M0's `V1SoftwareCurrent` stops a v1 play from redeploying other teams.
- **D's remaining text:**
  - The sudo refresher is consistent with C 3.6.6.
  - The play-lock window is stated.
  - R takes the set lock without waiting, and lifts the fence even on a full disk.
  - M0's free-space bound is stated.
  - The export is out of the backup, with the reason given.
  - The welcome is A 7.4 `moved` in both C and D.
  - A7's probe is consistent between C 3.3 and D 6.1 (apart from minor 7).
- **Sampled minors:** security 6, 7, 8, 9, 11, 13, 14 and 15; operations 14, 17, 18, 19, 22 and 24;
  coherence 15, 16, 17 and 20. Each is in the cited section as the disposition says.

## Outside scope (brief)

- B 2.6 step 08 treats a 401 as permanent for the team. With new major 1, this is what turns a
  restore into a bus-less team for every live ccy seat. The member side needs a stated path, such
  as re-reading `token` on the inotify watch, which already exists.
