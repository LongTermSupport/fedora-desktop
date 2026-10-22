# Plan 00209 N0 fix pass, theme host: handoff

The second host fixer took this up and finished every item below; the disposition of every finding
is `261021-n0-fix-host.md`. This file is kept as the record of the decisions the first fixer took.

Worktree `/workspace/untracked/worktrees/bus-v2-n0-f3-host`, branch `feat/bus-v2-n0-f3-host`, tip
503c05bf. The binding rules are `/workspace/untracked/scratch/n0-fixer-common.txt` and
`n0-designer-common.txt`. Owned: `DESIGN-v2-C-host.md` (all), B 2.13 (LXC guest operations), and the
DESIGN-v2.md seam and decision rows these fixes change. Run `./scripts/qa-changed.bash` before each
commit; Bash commit commands need `&&` chaining (the safe-mode hook).

## Done (committed)

- 428952e0: C 3.9.1-3.9.4 and B 2.13. One mechanism: root writes guest system paths (kit, unit)
  host-side with `RESOLVE_NO_XDEV` and a root-owned, non-group/other-writable check of every
  directory; the agent user writes its home by `guest check|place-bundle|membership|remove`
  (`install-self` and `unit` ops deleted). Desired membership is the agent's own file
  (`~/.config/teamchat/membership/desired.json`, by `guest membership` as the agent; reasoning:
  it is not a boundary, the token is), host copy `root/lxc/<container>/<user>.json` holds
  `generation`. Present phase gains step 3 v1-still-active check (O8) and step 8 membership.
  Absent phase: farewell `parked` -> wait seat free (60 s, L3 probe) -> `guest remove` -> `member park` -> unit removed if no team. Kit GC: host reads guests' `session.pid` (B's rule) through the
  root fd. Unit text in C 3.9.4 with `KillMode=mixed`, `TimeoutStopSec=30`, `RuntimeDirectory=`,
  runs `teamchat-claude` in tmux. uid 0 and the desktop user's uid refused (K15, not yet written).
- 503c05bf: C 3.6.1-3.6.4. roster.json gains `seq` (raised every write, never lowered) and `rev`;
  members `cred_seq`/`cred_ms` (replacing `token_ms`), humans `revoked_seq` (replacing
  `revoked_ms`). Roster event = `proto.Roster(roster.json, declaration, rooms)`, validated by A's
  decoder, written by the rooms step, compared without `rev`, `rev` = max(event, file) + 1. Every
  member/role/human verb ends: converge rooms, post the admin notice, refresh status. Admin posts
  exactly `added parked removed role` + D's carried + welcome. `Desired(team, roster, rooms)` asked
  again after each create (bootstrap order, coherence 20). `--credential password|token` (DC16,
  below); guest tokens copied to `ctl/<team>/guest/<handle>.token`. `--human-text` on add and a
  `member set` verb; human_text read from the roster only. Role apply posts `role`, names the
  questions open/closed switch and the re-invite.

## Decisions taken (write them where listed below)

- **DC16 new default (c'):** a pool seat holds a **password** (`pool/.../<seat>/password`, 0600,
  never mounted), not a token. Each take logs in with the seat's one fixed device id (`TCSEAT`),
  which per the Matrix spec invalidates the previous session's token (host check H-C13 proves it on
  Tuwunel; fallback: per-claim device id plus UIA device delete; last fallback: (a) with the
  release-time bound). The session's token is kept in the pool as `held/` (a complete bundle:
  `held.json`, `token`, `seat.json`), the copy goes to the session. Release: disconnected notice
  with `held/`'s token, then `POST /logout` with it, then quarantine `held/`. A hostile released
  session's token is dead at release; it cannot change the password (UIA needs the old one).
  Old (a) and (b) become rejected options. K13 restated (silent skipping via a read receipt, and
  continued reading) and marked closed by (c') except for the fallback.
- **Release time per seat (security 1, member fixer's ask):** the take writes into the session's
  `seat.json` copy `"claim": "<claim id>"` and `"released_ms": <last release time or null>`
  (the pool records `released.json` at release step 4). B starts a store-less seat from the
  earlier of its own read receipt and `released_ms`. Pool `seat.json` drops `human_text`.
- **ccy crash disconnected (ops 2):** when the session is gone (seat lock free) but its record
  remains (restorable), the launcher at podman exit and the board in the background run `teamchat seat disconnected --bundle <pool>/held --claim <claim>`, record `held/notice.json`, keep the seat
  held. Txn id from the claim; B's restored worker must not double it (cross-fixer: B uses the same
  `tc-disc-<claim>` txn, the claim is in seat.json; A/B: a restored session posts `joined` back).
- **LXC disconnected from the health tick (ops 2):** tick keeps `root/<team>/lxc-online.json`
  (handle -> held, run id from the seat lock content grammar-checked, since_ms, notice). On a
  held->free or container-stopped transition with the member still active, at step 5 under the
  team lock (non-waiting; the LXC absent phase holds it, so no false notice), the child op
  `member.disconnected` checks main for the member's own `left`/`disconnected` since since_ms and
  otherwise posts A's `disconnected` with the guest token copy, txn `tc-disc-<run>`. Cross-fixer B:
  next worker checks the room the same way and uses the same txn id.
- **CAP_SYS_PTRACE (security 4):** add to the health unit's bounding set only, with
  `SystemCallFilter=~ptrace process_vm_readv process_vm_writev`; reason stated; new risk K14
  (/proc/pid/mem reach, fixed paths only); H-C10 run inside the health unit's sandbox.
- **Security policy approval (security 2), new DC18:** security fields (`humans`, `coordinator`,
  `allow_from`, `listen`, `url`, `trust`, and the team's first existence) are compared by
  `declare check` (step 2) with `/var/lib/teamchat-status/teams/<team>/approved.json` (root-written
  0640); a difference prints `SECURITY<TAB><team><TAB><field><TAB><old> -> <new>` in its own task
  and fails the play naming `ccy-teams approve <team>`, which shows the diff and runs `sudo teamchat-admin team approve --team T` (declaration on stdin). `team apply` re-checks as root.
  ccy-teams' own flows (create, humans, coordinator) approve inside their sudo session. K9 gains
  "the declaration is agent-writable; the gate stops a data edit, not a code edit". New K row.
- **Seat assignment (security 3), new DC19:** narrow C10 and section 1 (password guards accounts,
  roles, humans, backups, restores; seat-to-session placement is guarded by the owner uid and the
  mounts only); widen K12 ("can read, and can re-seat sessions and change their desired teams");
  option (b) pool and `control/` under a uid no owner process holds with password-sudo placement,
  default (a) not built. Frame sentence: cross-fixer to the build session.
- **Restore (ops 3, 4, 5, 18; security 7), DC8 rewritten:** order: (1) validate, refuse a
  `pending` roster, refuse a backup older than D's migration carry time unless
  `--accept-carry-loss`; (2) merged roster in memory, compared by `seq` (`cred_seq`/`revoked_seq`
  > backup roster's `seq`), not ms; (3) `restore.json` `{"step":"staging"}` FIRST; (4) stage in a
  > transient unit with the live identity and `PrivateNetwork=yes` (no TCP port reachable from the
  > host, no staging port); (5) secure the staged db: a ctl-child transient unit with
  > `JoinsNamespaceOf=` the staging unit and `PrivateNetwork=yes`, run by `systemd-run --pipe   --wait`, applies the merged roster's revocations (retired kicked/deactivated, parked revoked,
  > needs:reissue revoked, humans revoked to needs:password, locked locked) and writes the roster
  > event with `rev` above the live one and `restore_epoch` + 1; stop staging; record `staged`;
  > (6) pre-restore backup M (after staging, major-43 order kept); (7) export live events after
  > N's time to `ctl/<team>/restore-<N>-lost.jsonl` via the child, count them (window between export
  > and stop stated); (8) record `swap`, block, stop live, renames, child `restore.swap`, record
  > `start`, remove block (block covers only the rename window: the staged db is already secured, so
  > a crash after the block is removed serves no revoked credential); (9) start, ready, listen
  > assert, normal bootstrap; (10) cleanup, report M, lost-event count, handles needing reissue.
  > Failures: before M (3-5) discard the staging under the lock and remove the record (N still
  > exists); from M on, swap back and keep `db.restore-<N>` with record `staged`; `restore --resume`
  > retries from it, `restore --discard` removes it; status names it. Resume at `swap` goes
  > forward. Fault injection per step in 3.14. Host checks: H-C5 rewritten (private namespace
  > staging, JoinsNamespaceOf), new H-C14 (Tuwunel `/sync` with `since` ahead of the db).
  > Cross-fixer A: roster `restore_epoch`; B: a worker seeing it change drops `next_batch` and
  > resumes as a store-less seat.

## Left to write, in C unless named

01. C 3.7.1 (pool layout: `password`, `held/`, `released.json`; security 6: N4 acceptance, the
    launcher refuses any mount whose real source is an ancestor of, equal to, or inside the seat
    store root other than the two binds, VM-lab test), 3.7.2 (seat.json: drop `human_text`, add
    `claim`, `released_ms` on session copies), 3.7.3 (take with login, release with logout,
    DC16 paragraph), 3.7.5 (keeper wording: the worker posts `left released`, B 2.10, coherence
    16; ccy crash notice; LXC by the tick).
02. C 3.12.2 restore rewrite as above; 3.12.1 one sentence: D's migration files are not in the
    backup and stay on the same disk (ops 17; cross-fixer D 5.6).
03. C 3.11.1 capability; 3.11.2 LXC disconnected step, free space on `/var/lib` as a problem (ops
    19), itemised questions checks (no worker in questions, coordinator joined, form matches;
    coherence 19); 3.11.3 `online`, `disk`, restore states.
04. C 3.2.5: bridge subnet only for teams with a declared LXC member; `allow_from` and the login
    limits are the only boundary between teams on a host (security 14; A 6.9 cross-fixer); TLS
    sentence (coherence 18, R3) and a 6.1 dropped row.
05. C 3.3 verbs: `team approve`, `redact`, `member set`, `restore --resume|--discard`, migrate list
    = `preflight stage fence fence-status capture carry commit archive archive-check unfence rollback` + `footprint`, with "D 5.4 names which acts run as the user" (coherence 17;
    cross-fixer D to confirm).
06. C 3.5.1 step 2 security diff; step 6 pin printed in its own task when changed, and the
    update-versions flow cross-checks upstream's published checksum (security 15); a sentence that
    the play never touches `teamchat-fence@` instances (ops 22, partial).
07. C 3.6.5 or new 3.6.7: `teamchat-admin redact --team T --event ID --reason secret` (quality 6),
    password sudo via ccy-teams, the admin redacts (A 4.2 already lets only the admin); new DC20;
    cross-fixer A 12.2 "Delete a message".
08. C 3.6.6: ccy-teams runs `sudo -v` before each root call, not once per change (ops 14).
09. C 3.8 / DC17: go-deps rule that `src-go/cmd/teamchat-admin` does not depend on
    `go.yaml.in/yaml/v3`; godbus message sizes capped; it decodes answers from host systemd and a
    guest's PID 1 (security 13).
10. C 3.10 / C34: consent record required for every Element profile write (security 9).
11. C 3.15 the one statement of resident cost, timers "enabled only while a team is present";
    cross-fixer: DESIGN-v2 section 6 and D 8 cite it (ops 24).
12. DC1/DC2 text: one `teamchat-ctl` uid holds every team's admin token, so DC1's per-team scope
    holds at the homeserver, not the controller (security 11).
13. Section 2: C10 narrowed; C34; C43 widened; new C46 (security-field approval), C47 (restore
    keeps sync correct and reports lost events).
14. Section 5 risks: K9, K12, K13 rewritten; new K14 (CAP_SYS_PTRACE), K15 (same kernel uid in a
    privileged guest), K16 (`allow_from` not a team boundary).
15. Section 7: B items 1 (done), 4 (the worker posts the goodbye, coherence 16), new items:
    `released_ms`/`claim` in seat.json, disconnected room check and txn ids, `run` waits for the
    keeper (for 3.9.4), `restore_epoch`; A items: `added`/`role` content, `restore_epoch`, admin
    redact, 6.9 threat row.
16. Section 8: H-C5 rewritten, H-C10 inside the health sandbox, new H-C13 (device re-login), H-C14
    (sync since ahead), H-C15 (JoinsNamespaceOf + PrivateNetwork + `systemd-run --pipe`).
17. 3.14 tests: restore fault per step, absent-phase wait, approval gate, membership write.
18. Package names: `bus/seat` at C 3.7.3 -> `internal/teamchat/seat` (coherence 8; `bus/state`
    already done).
19. DESIGN-v2.md rows: seam 17, 20, 24, 25, 27, 28, 32, 46, 53 (Fixed, new interfaces); new rows
    for the tick's disconnected and `restore_epoch`; decisions DC8, DC16, DC17, BD9, BD13, new
    DC18-DC20; section 8 R3 (TLS), R10 (C30 single field), R14 (180 s wait is C 3.7.4).
20. The disposition file `subagent-reports/261021-n0-fix-host.md`: every finding below, then the
    cross-fixer requests.

## Findings in this theme and their dispositions

Coherence (`261021-n0-review-coherence.md`): 1 fixed (3.9.2, B 2.13); 2 fixed (3.9.1, 3.9.2 step 4,
3.9.4, B 2.13); 5 fixed (3.6.3, 3.6.4); 7 fixed (3.6.1, 3.6.3); 8 C part fixed (`bus/state`; seat
left), B part cross-fixer to member; 15 fixed in C (3.6.3, 3.7.2 left), B 2.6 step 10 cross-fixer;
16 left (3.7.5, 7); 17 left (3.3); 18 R3/R10/R14 left, R19/R31/R41/R43/R44 not host (member,
protocol); 19 health and questions part (3.6.4 done, 3.11.2 left), skill parts not host; 20 fixed
(3.6.2), A 5.2 sentence cross-fixer.

Security (`261021-n0-review-security.md`): 1 left (3.7, DC16 c'); 2 left (DC18); 3 left (DC19); 4
left (3.11.1); 6 left (3.7.1, row 53); 7 left (restore, private namespace); 8 fixed (3.9.1), K15
left; 9 left (3.10); 11 left (DC1/DC2); 12 cross-fixer to migration (D 3.5); 13 left; 14 left; 15
left. 5 and 10 are migration's.

Operations (`261021-n0-review-operations.md`): 1 LXC unit part fixed (3.9.4), `run` waiting is
member's; 2 left; 3, 4, 5, 18 left (restore); 6 fixed (with coherence 1, 2); 8 fixed (3.9.2 step
3), adapter function cross-fixer to migration; 14 left (3.6.6), D part migration's; 17 left; 19
health part left, M0/R parts migration's; 22 play sentence left, rest migration's; 24 left.

Quality (`261021-n0-review-quality.md`): 6 left (redact verb, DC20), A part cross-fixer.

## Cross-fixer requests so far

- member (B): 2.10 last paragraph: LXC desired written by `guest membership` as the agent user,
  not root-owned; 2.15 guest-ops test row (no sha256sum/install-self); B 6 items on install-self
  sweep; B 3 BD13 text; header `<v2-desired>` LXC value "(by `guest membership`, as the agent)";
  `run` waits for the keeper (bounded) before it exits, and the worker ignores SIGTERM (ops 1, the
  LXC unit relies on it); store-less start from min(read receipt, `released_ms`); disconnected
  dedupe by room check and shared txn ids (`tc-disc-<claim>` ccy, `tc-disc-<run>` LXC); restored
  ccy session after a host-posted disconnected; `restore_epoch` handling; `human_text` from the
  roster in 2.6 step 10; `bus/...` package names.
- protocol (A): `added` and `role` notice content; `restore_epoch` in the roster; A 5.2 bootstrap
  order sentence; A 12.2 admin redact; A 6.9 threat row (`allow_from` not a team boundary); a
  `joined` back notice after a restore of a crashed ccy session.
- migration (D): D 3.5 path row (security 12); `V1GuestHolds` in the adapter (ops 8); D 5.6 export
  not in C's backup (ops 17); D 8 cites C 3.15 (ops 24); confirm C 3.3's migrate verb list.
- build session: the frame's "nothing an agent in a cc session can run changes membership"
  sentence narrowed per DC19; DESIGN-v2 section 6 cites C 3.15.
