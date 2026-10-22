# Plan 00209 research: LXC members, multi-team sessions, hot control, robustness

Read-only digest for the agent-bus v2 (Go) rebuild. Placeholders only: `<team>`, `<container>`,
`<agent-user>`, `<repo>`, `<session>`, `<checkout>`. "INFERENCE" marks what is reasoned, not read.
Paths are repo-relative to the worktree. Abbreviations: `M` = `helpers/agent_bus_lxc/members.py`,
`D` = `helpers/agent_bus_lxc/derive.py`, `AR` = `helpers/agent_bus_lxc/applied_record.py`,
`P` = `playbooks/imports/play-agent-bus.yml`, `DOC` = `docs/agent-bus-lxc-members.md`,
`HC` = `CLAUDE/Plan/00203-teams-hot-control-and-robustness/SPEC-hot-control.md`,
`LS` = `.../00203.../SPEC-lxc-stopped-members.md`, `TR` = `.../00203.../SPEC-team-add-and-restart.md`,
`MT` = `CLAUDE/Plan/00201-.../SPEC-many-teams.md`, `SD` = `.../00201.../SPEC-shutdown.md`,
`J16`/`J19` = 00203 JOURNAL 26-10-16 / 26-10-19.

---

## 1. LXC member lifecycle

### 1.1 Declaration (the input)

- Source of truth is a host_vars list `agent_bus_lxc_members` (untracked localhost.yml), one entry
  per (container, user, team). Required `team, container, user`; optional `host, checkout, seat, role, human_text, repo, state, service` (M:106-109). Unknown keys refused (M:410-414).
- Validation (M:406-462): team grammar from `protocol.is_team_name`; container
  `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}`; user `[a-z_][a-z0-9_-]{0,31}` (M:115-116); checkout absolute;
  a present entry needs `checkout`; an absent one needs `checkout` or `repo` (M:444-447); role
  default `worker` (M:81); `human_text` default true; `service` default false.
- Cross-entry rules (`_check_between`, M:476-514): one entry per (container,user,team); one
  `host` per (container,user) because they share one env file; seat clash only within the same
  repo+host (fixed by `fix/lxc-seat-clash`, review `subagent-reports/261016-lxc-seat-clash-review-fable.md`);
  one coordinator per team among present entries; all entries of a (container,user) must agree on
  `service`, and with `service: true` on one checkout (one supervised session per user).
- Written by hand or by `ccy-teams lxc-add` (Go, `src-go/internal/teams/lxcadd/`), which edits
  localhost.yml (backup `localhost.yml.backup.<time>`) then applies (DOC:86-128).

### 1.2 Handle derivation (no install-specific hostname ever)

- Handle `<repo>.<seat>+<host>.lxc` (D:4-17, DOC:167-177). `<repo>` = last component of
  checkout's `origin` minus `.git`, else directory name; `<host>` = `HOOKS_DAEMON_HOSTNAME` from
  `<checkout>/.claude/ccy/ccy.env.local`, else literal `local` (never the machine hostname:
  handles appear in public forge text, DOC:172-174); `<seat>` = lowest number no ACTIVE member of
  the same `<repo>+<host>.lxc` holds; a parked number is reused (D:579-607).
- Seat precedence (`derive_seat`, D:579-607): (a) the seat the container's existing bundle names
  (idempotence: a re-run never renames); (b) legacy "container name as seat" (`default_seat`,
  D:572-576) if that handle is already in the roster; (c) refuse if an active numbered member
  exists that no entry holds (possible lost bundle, or joined by hand) unless `unheld_ok`;
  (d) lowest free number.
- The checkout is read FROM THE HOST AS ROOT, never by running anything in the container
  (D:19-27): `ROOT_READ_PY` (D:68-413) parses `<lxcpath>/<container>/config`, finds rootfs and
  bind mounts, walks every path component with `O_PATH|O_NOFOLLOW` refusing links, caps files at
  64 KiB (D:43), reads `.git/config` and `ccy.env.local` as plain text, never runs git (repo
  config can name programs). Refuses `lxc.idmap` (unprivileged) (D:215-216). Store handling
  (D:75-76, 217-232): `dir`/`btrfs`/bare path read as host dirs; `overlay|overlayfs|zfs|lvm|loop| rbd|nbd` only via `/proc/<init pid>/root` of a running container; others refused by name.
  `insteadOf` rewrites refused (DOC:193-196). Optional bind with missing source skipped as LXC
  does (D:237-241). `ccy.env.local` must be owned by the agent user's uid (D:515-516).
- The same script has modes `probe`, `checkouts` (bounded search: home then `/var/www`, `/srv`,
  `/opt`, depth 4, max 50, 8 s, skip `node_modules|vendor|venv|__pycache__|site-packages`,
  D:46-53, 312-371), `dirs` (Tab completion listing, D:372-383) and `sessions` (Claude processes
  of the user via the container's `/proc`, start time from the HOST's btime, D:384-410). Shared by
  the play and by `ccy-teams lxc-add` via `sudo -n python3 -I -c` (D:26-27), so "the rule exists
  once".
- The container's own `pingbus suggest-handle` is only HELD to the host's answer (M:966-976,
  1038-1040); a mismatch refuses. The host mints from what it read.

### 1.3 Join (present phase), order and pre-checks

`run_present` (M:1179-1253). Everything checkable without the kit is checked for every running
container BEFORE any change (DOC:24-27):

01. team declared present and its homeserver unit active (M:989-996);
02. per (container,user) group: container state (`lxc-ls -1`, `lxc-info -sH`, M:1430-1439); a
    missing container fails (M:934-940); a non-RUNNING one is held back (1.5);
03. `getent passwd` home (M:943-948, 1407-1416; exit 2 = no user);
04. gh logged in for the agent user, after sourcing `~/.config/pingbus/env` (M:363-364, 999-1005)
    — the token never leaves the container;
05. route: container's source address towards the bus address (UDP connect, no packet, M:129-130)
    must be on a host interface that is a bridge (`/sys/class/net/<dev>/bridge`, M:1540) and
    inside the team's `allow_from` (M:1018-1032). The play adds `lxcbr0`'s live subnet to every
    team's allow_from (P:279-299, `helpers/agent_bus/bridge.py`); other bridges need explicit
    `allow_from`;
06. `service: true`: `lxc.start.auto = 1` (M:1442-1450), systemd init, tmux (M:355-356, 1069-1079);
07. then host/seat derivation for all reachable entries (M:1143-1165, 517-578);
08. kit sync once per container (M:1008-1016, `KIT_SYNC_PY` M:135-228): tar of
    `/usr/local/share/agent-bus/kit` (root-owned, mtime 0, symlinks refused, M:1488-1507) piped to
    `python3 -I` as root; only differing files written (atomic rename), extras removed, links
    `/usr/local/bin/{pingbus,agent-bus-claude}`; Python >= 3.11 required;
09. join each member, ALL DEMOTIONS BEFORE ANY PROMOTION across containers (M:1232-1238), so one
    coordinator per team holds at every instant;
10. per (container,user): merge env file, then `pingbus config check` as the agent user
    (M:1239-1248), then ensure or drop the service unit (M:1249-1252).

`_join` (M:1035-1066) + `present_plan` (M:610-637):

- bundle names a different handle -> refuse with a hint (M:613-623);
- active numbered handle and no bundle here -> refuse (lost bundle / joined by hand, M:624-630);
- "current" = bundle present AND token non-empty AND active AND `base_url` matches AND
  `human_text` matches -> nothing but a possible `set-role` (no new token per run);
- else: remove stale bundle, park the active handle, `agent-bus add-member ... --seat --out=-`,
  validate the bundle, place it as the agent (`PLACE_PY`, M:261-282: dir 0700, files `token`,
  `README`, `member.json` 0600, `O_EXCL|O_NOFOLLOW`).
- Commands into the guest: `lxc-attach -n <c> --clear-env -- env -i PATH=... HOME=/root ...`
  (M:1403-1405); as agent `runuser -u <user> -- env -i -C <cwd> PATH=... HOME=<home>` (M:1424-1427).
  Requires util-linux `runuser` and coreutils >= 8.28 `env -C` (DOC:52).

### 1.4 Env file merge

`merge_env` (M:763-801): only `PINGBUS_TEAMS=<csv>` and `HOOKS_DAEMON_HOSTNAME=<role>` lines are
managed, every other line kept; a form it cannot read is refused; set twice refused; a different
host while other teams stay listed refused ("one role per user"); no team left -> both lines go,
and an otherwise empty file is deleted (`WRITE_ENV_PY`, M:295-312).

### 1.5 Stopped containers: SKIPPED / DEFERRED / LXC-SUMMARY

Owner requirement (LS:3-6): the play "can't be so brittle that it has to have LXC agents running".
Previous behaviour: `_guest_running` raised exit 78 for any non-RUNNING container, blocking every
other team change, `lxc-add` included (LS:8-14; J19 11:03).

- Reachable = exists and RUNNING (LS:18-19). Any other state (STOPPED, FROZEN, ...) is never
  entered and never fails (M:1199-1209). Missing container still fails (LS L5).
- Known handle without entering: entry gives host+seat+repo, else the member record item whose
  checkout matches and agrees with given fields (`known_handle`, M:689-704; LS:20-23).
- Present + not running + known handle active -> `SKIPPED` (status `stopped`); else `DEFERRED`
  (M:1168-1176, why texts M:713-723).
- Seat safety (LS L4): a skipped member's known handle is claimed (`with_handle`, M:707-710,
  1206); a deferred member with no known handle turns on `unheld_ok` for its team only
  (M:1208, 529-530, 572-573) so a new member gets the next number instead of a refusal.
- Absent + not running (LS L6, `_absent_held_back` M:1256-1272): known handle parked from the host
  side at once; bundle, env file and unit left until the first run with it up (DEFERRED). A
  service unit is dropped only when the container is RUNNING (M:1366-1369).
- Output (M:50-53, 1590-1593): stdout `CHANGED\t<what>` lines, then
  `SKIPPED|DEFERRED\t<team>\t<user>@<container>\t<why>` per member, then one `LXC-SUMMARY\t<text>`
  (M:672-677). Diagnostics on stderr prefixed `agent-bus-lxc-members:`. Exit 0 / 64 / 78.
  Play shows held-back lines in separate `debug` tasks so they show without `-v` (P:250-258,
  383-389; regex P:69).

### 1.6 Removal (absent phase)

Runs BEFORE team removal so the team still runs while its member is parked (P:28-30, 241-248).
`_absent_one` (M:1275-1322): homeserver must be active if the team is present; missing
container needs `repo:` to know the handle (M:1285-1288); handle from the bundle, else from
host+seat(+repo or suggest-handle); park if active (`agent-bus park-member`); remove bundle;
remove the team from `PINGBUS_TEAMS`. A user left in no team gets its unit stopped/disabled/
removed (M:1117-1133, 1363-1369). The kit and the container are never removed (DOC:228).
To change host or seat: `state: absent`, run, re-declare (DOC:210-211). Handle stays fixed;
a lost bundle is replaced with a new token for the same handle only via absent-then-present
(DOC:200-220).

### 1.7 Supervised session (`service: true`, audit gap 9)

`render_member_unit` (M:859-895): system unit `agent-bus-claude-<user>.service` in the container,
`Type=forking`, `User=`, `WorkingDirectory=<checkout>`, `ExecStart=tmux -L agent-bus-claude new-session -d -s agent-bus-claude /usr/local/bin/agent-bus-claude`, `Restart=always`,
`RestartSec=10s`, `RestartSteps=6`, `RestartMaxDelaySec=5min`, `StartLimitIntervalSec=0`, values
regex-guarded against systemd specifiers (M:114). Enabled in multi-user.target; restarted only
when the unit text changed and it was active (M:1095-1114). Attach:
`lxc-attach -n <c> -- runuser -u <user> -- tmux -L agent-bus-claude attach` (DOC:150-151).

### 1.8 Applied records (member record, L8)

- Path `/var/lib/agent-bus-applied/lxc/<team>.json` (`RECORD_DIR = checkout.APPLIED_DIR/"lxc"`,
  M:99), dir 0750, file 0640, owner root, group = the "stamps' group" read from
  `/etc/agent-bus/stamp-group` (M:101, 1515-1527; AR:15-16, 41-68). Atomic `.<name>.new` +
  `os.replace`, rewritten only on change; a rewrite emits `CHANGED`. No secret.
- Schema v1 (M:100, 726-744): `{"version":1,"team":T,"members":[{"container","user","state", "status":"joined|stopped|deferred|left","handle"|null,"checkout","why","since":<epoch s>}]}`;
  `since` kept while status and handle stay; one item per declared entry; an undeclared item is
  dropped; written only after the phase succeeds; a team not declared present, or with no entry
  left, has its record removed; the present phase runs on every play run (even with no entries)
  to sweep (M:1338-1346, 1580-1586; P:369-381); a scoped run (`--only-teams`) sweeps only those
  teams (P:79-81).
- Readers: Go `src-go/internal/teams/lxcrecord`; `ccy-teams apply` prints held-back members and
  "Applied: <team>, with N LXC members held back (above)." (DOC:262-273; J19 11:03); the board
  (deferred row kind `lxc`, `container stopped at the last apply` on WHERE); `lxc-add` warns of
  stopped declared members and says a new member is deferred, not joined (J19 11:14). An
  unreadable record FAILS apply and lxc-add and is said on the board's team row.
- `applied_record.py` deliberately does NOT share the health record writer: `helpers/agent_bus` is
  baked into the ccy image and sharing would force an image bump (AR:6-7; J19 11:14 correction).

### 1.9 After a join: what is left to the human

lxc-add ends by naming `gh auth login` if needed and `agent-bus-claude` as the agent user in the
checkout; looks (read-only via `/proc`) for a Claude session of that user started before the add
and tells the owner to restart it; prints the live check
`set -a; . ~/.config/pingbus/env; set +a; pingbus config check && pingbus history --limit 1`
(DOC:118-127). pingbus never reads the env file itself; only `agent-bus-claude` does (J16 10:06).
Session start: `agent-bus-claude` refuses an Element profile in the home (exit 78), sources the
env file, puts the kit first on PATH, runs `pingbus config check`, execs
`claude --plugin-dir <kit>/plugin/pingbus --settings <kit>/settings.json` whose one setting is
`crossSessionInbound: accept` (DOC:277-306). LXC members claim no seat; their busy rule is the
team sync lock (robustness audit 3.5). INFERENCE: hence no `seat disconnected` notice exists for
them (it keys on seat-lock claims, section 4).

---

## 2. Multi-team sessions

- Grammar: `--teams <seat>@<team>[,...]`; one seat per team per session (D48), team twice refused
  (`seat.py` one_per_team; RESEARCH-current-design.md §3). A team named alone is resolved by the
  attended chooser; unattended launches use `seat check --resolved`, which refuses a bare team
  (`files/var/local/claude-yolo/lib/agent-bus-seats.bash:37-44, 132-157`).
- The bus was already multi-team end to end (RESEARCH §9): one watcher, one thread + lock per
  team, summed notice counts, every output line names its team. The one-team assumptions were
  only the chooser and picker (RESEARCH §1-2).
- Chooser (`ccy-teams choose-teams --type=podman|host [LIST]`, Go; MT S-B1-S-B15): fzf `--multi`;
  rows covering / "would add" (widen scope) / excludes / `(down)`; widening asks Add / Join without
  adding (references then external) / Leave out; all Adds applied in ONE `Ops.ApplyTeams`; an
  apply failure launches nothing; Esc prints nothing on stdout, exit 0. Per team then seat and
  role questions, headed "Team 2 of 3: <team>" (MT S-C1). Exactly one present team covering the
  checkout is taken without asking (S-B2). Bounded re-asks (3).
- Trust nuance: membership is NOT scope-checked; a seat in a team that does not cover the checkout
  works but its own repo's refs are `external` (RESEARCH §6). Hence widening.
- Descriptions (MT S-A1-S-A16): 1-120 chars, no Cc/Cf; optional in the team file, required by
  `create`. Reaches agents via the admin-sent `m.room.topic`, NOT a team-record key, because an
  unknown record key makes older members treat the room as untrusted (exit 10) (S-A8). Changing a
  description restarts no homeserver (S-A4). Shown as `ABOUT` in `pingbus status`, a SessionStart
  line, the board cell, Quick Launch `Team:` line.
- After `seat take`, the launcher prints the launch block via `ccy-teams describe-seats --block`
  on stderr (agent-bus-seats.bash:164-183). Canonical list replaces the given one in the argv so
  restart/restore replay it (agent-bus-seats.bash:186-206).
- `send`/`say` with several teams infer the team from `--re` (send only) or `--to`, else exit 64
  listing teams (MT S-C8, S-C9). `--to` resolves full ID, handle, human localpart, then short name
  `<repo>.<seat>` if unique (S-D2). `recv` prints a `REPLY` line after HUMAN/PEER (S-D1).
- Status line `👥 alpha (coordinator), beta (worker)` via the kit's `pingbus status-line`, read on
  each refresh; `(role?)` when unreadable; `(down)` for a fresh down mark (J19 09:52; HC H26).
- MT S-C2 ("teams change only by a new launch") was SUPERSEDED by 00203 hot control.
- cc (host) uses the same chooser with `--type=host`; differences that stay: member type `host`,
  Element consent (`~/.config/pingbus/element-accepted`), Element deny settings, temp home, cc
  creates `.claude/ccy/` (MT table after S-E6).

---

## 3. Hot control

### 3.1 Model

- Frozen-at-launch inventory F1-F11 (HC:27-44); only the role (team record) and the bus route are
  live (HC:46-53).
- D1: source of truth is a HOST-written desired membership in a per-session directory mounted
  read-only into the container (`$XDG_RUNTIME_DIR/ccy/membership/<session>/` -> `/run/ccy/membership:ro,Z`).
  Directory mount, not file mount, because a file mount pins the inode and hides atomic replaces
  (HC:57-68; `lib/agent-bus-membership.bash:5-16, 62-67`).
- D2: container can never write its membership (EROFS proved). D3: role stays only in the
  admin-sent team record (`sudo agent-bus set-role`); no membership change ever comes from the bus
  or a container-writable file (HC H6, H7).
- D4: the seat keeper (`pingbus seat keep`) is the only holder of every seat lock, close-on-exec,
  released with explicit `flock(LOCK_UN)` (old truncate-and-close did not release a dup'd lock).
  Dies with its parent via `PR_SET_PDEATHSIG` + getppid poll (m1 report). Polls desired file
  (0.5 s tick, inode/size/mtime) and reconciles within 2 s (HC H9). A refusal (`held-elsewhere`,
  `no-bundle`, `bundle-invalid`, `not-desired`) does not stop other seats.
- D5/D6: only sessions launched with the kit can be hot-controlled; remove does NOT revoke the
  token (control, not isolation, within a checkout).

### 3.2 Files

- Desired (HC 3.1): `{"v":1,"session","kind":"ccy|cc","checkout","generation","written_ms", "seats":[{team,seat}],"farewell":[{team,seat,reason:"removed"}]}`, 0700 dir / 0600 file,
  atomic. Generation compared for EQUALITY only (relaunch writes 1 again). Seat identity is
  (checkout, team, seat). Farewells accumulate until each removal is complete.
- Applied (HC 3.2): `$PINGBUS_HOME/applied.json` with `seats[...state]`, `refused[...why,holder]`,
  `told{generation,roles}`. Active teams = held seats ∩ desired (H4, H8), on every call.
- Report (HC 3.3): `<checkout>/.claude/ccy/pingbus/sessions/<session>/applied.json`, never
  authority; lock first, report second. Also `conversation.json` there (TR C1).
- `relaunch.json` in the membership dir requests a supervisor restart (TR F2, B4).

### 3.3 Commands and exit codes (HC H17-H29)

`ccy-teams session list | add <s> <team>[/<seat>] [--role] | remove <s> <team> [--coordinator=demote|hand-to=<seat>] [--park] | move <s> <from> <to>[/<seat>] | role <s>|<handle> <team> worker|coordinator [--demote-current] | repair <s>`; exits 0/1/64/75/78
(HC:349-357). One stderr line per step `✓/✗/-` (H18). Idempotent re-runs; partial failures resume
(H19).

- Add steps (H22): resolve session by `ccy-session` label; team declared + homeserver up + roster
  < 64 agents; choose/validate seat; `agent-bus seat take` (sudo) if new; route probe from inside
  the container (`pingbus reach`, versions GET 3 s); role; write REGISTRY RECORD then desired file
  (H5); wait 10 s for lock held and report generation. Failure before 7 changes nothing; at 8
  restores record and desired. A session launched on no team was launched with
  `crossSessionInbound: refuse`, which Claude Code reads only at start (M2 spike), so its FIRST
  join relaunches it once via the supervisor restart path (H22, H34).
- Remove (H23): coordinator needs `--coordinator`; record, then desired with farewell; wait lock
  free; re-baseline from the host (`pingbus seat retire`: clear `sync_token`, publish expired
  `listening`); post goodbye from the host as the seat's member "`<handle> left the team: removed by the owner`" (`helpers/pingbus/cli.py:2583-2595`); drop farewell. Nothing posted while out is
  delivered on re-add (H23a).
- Move = add then remove (H24). Role (H25): two coordinator paths ("has none" plain offer vs
  "takeover", default No; non-interactive takeover needs `--demote-current`), worded once in
  `src-go/internal/teams/coordinator`; one role-change path for every member kind
  (`src-go/internal/teams/roles`); a declared LXC member's ENTRY is set too (`declare lxc-role`)
  then applied, all inside one play-lock hold, else the next apply would revert the role or refuse a
  second coordinator (HC:527-543; J16 13:47).
- Locks (H20, as built): change lock `$XDG_RUNTIME_DIR/ccy/change/<session>.lock`; allocation
  lock `$XDG_RUNTIME_DIR/ccy/alloc/<first 32 hex sha256(resolved checkout)>.lock`; never in the
  mount or checkout because flock needs no write permission, so a container could hold them
  forever (HC:369-392; J16 06:14 B1). Allocation authority (H21): a seat is free only if its lock
  is free AND no live desired file names (checkout,team,seat); ccy-teams also honours a stopped
  restorable record (N4).
- Edge cases: homeserver down (H26: add/role refuse; remove releases locally, goodbye owed, exit 1);
  roster at 64 (H27); session exits mid-change (H28); `ccy-teams remove <team>` first takes its
  sessions off (H29).
- Who: only sessions with a registry name on ccy's tmux (H35); the name is taken only when the
  pane's first process is the launcher or its trampoline (J16 02:57 incident). cc sessions are
  weaker: the session can write its own file (H31). `cc --bus` refuses inbound until restarted on
  its first team (Decision 7).

### 3.4 Coordination with the play lock

- One lock for every play run: flock on `$XDG_RUNTIME_DIR/fedora-desktop-plays.lock`, holder
  line `pid=<pid> what=<what> since=<UTC>`, delegated to children by inherited fd 3 named by
  `FEDORA_DESKTOP_PLAY_LOCK_FD`; run.bash proves it with `lock.py held` or exits 75
  (`src-go/internal/playlock/playlock.go:1-14, 29-48, 188-195, 210-248`). Refuses a runtime dir not
  the user's or writable by others, a symlinked/non-regular lock (playlock.go:262-313).
- ccy-teams `ops.Locked` (`src-go/internal/teams/ops/playlock.go:54-70`) holds the lock across the
  host_vars write AND every play, so a wait that is stopped or times out (30 min,
  playlock.go:15) writes nothing (MT S-G1-S-G11). Waits on screen (fzf, Stop waiting row) or on
  stderr; exit 130 on stop, 1 on timeout; a play exiting 75 anyway is reported as refused.
- Hot session changes and `ccy-teams restart` apply no play, so take no play lock (TR B10);
  only declared-LXC role changes do.
- run.bash itself still exits 75 rather than waiting (00201 Task 2.19, pending owner).

### 3.5 Shutdown and restart semantics

- `ccy-teams shutdown <team> [--yes]` (SD): local only, never the bus. Types `/exit` into each
  session pane via `tmux -L ccy send-keys -l` then Enter after 200 ms; Escape first when busy;
  never types over unsent input text (fails it); hard stop `kill-session` + registry record removal
  after 60 s (exit) / 30 s (prompt); then waits until each seat's lock is released (30 s). LXC and
  other-machine members are listed "not shut down" (no clean route; no reach behind the owner's
  back) (SD SD5, Limits).
- `ccy-teams restart <team>` (TR B1-B11): running ccy member restarted in place by its supervisor at
  next idle moment via `relaunch.json {"why":"restart"}`, resumed by its recorded id on a fresh
  container (Decision 8); cc left running (Decision 9); stopped records started with
  `--resume <recorded id>`, never `--continue` (F4/F5: shared transcript dir, stale background
  records make `--continue` pick wrong or refuse forever) (Decision 10). LXC left out by request.
  300 s / 180 s bounds; a timed-out restart withdraws its request.

---

## 4. Membership notices, goodbye, disconnected (host view)

- MEMBERSHIP notice (HC H12-H13): on each new applied generation the watcher writes one envelope of
  kind `MEMBERSHIP` through Claude Code's inbox socket: per team joined (seat, handle, role,
  description), per team left (farewell reason, default "removed by the owner"), current teams,
  role changes. Fallback: next `UserPromptSubmit` adds the same lines; `told` in applied.json makes
  it once per generation. Non-membership sessions (agent-bus-claude: LXC/host/docker/vm) are told
  role changes via `told-roles.json` (J16 13:47).
- Session-end goodbye: SessionEnd hook posts `{handle} left the team: session ended ({reason})`
  (`helpers/pingbus/hooks.py:128`), covering only seats still applied (H16); a goodbye record per
  claim (`seat.record_goodbye/goodbye_said`, `helpers/pingbus/seat.py:262-267`).
- Host disconnect (Plan 00194 Task 2.0b): after the container has gone, the launcher's cleanup
  runs `pingbus seat disconnected --root <checkout>/.claude/ccy/pingbus/seats --session <name> --since <launch ms>` under `timeout 60`, SIGINT ignored meanwhile
  (`lib/agent-bus-membership.bash:71-94`). For each seat in the session's report: wait up to the
  disconnect wait for the lock to free, require the lock's claim stamp >= launch start, re-take the
  lock and re-check the claim (a new session may have taken it), skip if a goodbye was said, else
  post "`{handle} disconnected: session ended without a goodbye`" to humans and coordinators
  (`helpers/pingbus/cli.py:2598-2688`). Writes to a vanished terminal are swallowed only for
  EIO/EBADF/EPIPE (cli.py:2640-2653). Failure is reported, never blocks a restart.
- Host remove goodbye: `pingbus seat farewell` (cli.py:2583-2595), seat held throughout.
- LXC: no host-side disconnect or farewell (INFERENCE: they hold no seat lock; their goodbye is
  only the in-container SessionEnd hook; a stopped container says nothing to the team until peers
  see `listening` expire, 600 s TTL per HC H23 step 5).

---

## 5. On-disk files touched

| Path                                                                                                                                                | Owner / mode                                            | Written by                                           | Schema / version                                                        |
| --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------------------------- |
| host_vars `agent_bus_lxc_members` (localhost.yml)                                                                                                   | owner, untracked                                        | hand / `ccy-teams lxc-add` (backup `.backup.<time>`) | YAML list, keys M:106-109                                               |
| `/var/lib/agent-bus/<team>/team.json`                                                                                                               | root                                                    | installer                                            | teamfile (read M:1467-1471)                                             |
| `/var/lib/agent-bus-applied/lxc/<team>.json`                                                                                                        | root:<stamp group> 0640, dir 0750                       | members.py                                           | record v1 (M:100, 726-744)                                              |
| `/etc/agent-bus/stamp-group`                                                                                                                        | root 0644                                               | play (P:219-226)                                     | group name + newline                                                    |
| `/etc/sudoers.d/agent-bus-read-only`                                                                                                                | root 0440, visudo-validated                             | play (P:231-239)                                     | `agent-bus list <team>`, `human devices` without password               |
| container `/usr/local/share/agent-bus/kit/**`                                                                                                       | root, dirs 0755 (repaired from tighter), files tar mode | KIT_SYNC_PY                                          | mirror of host kit                                                      |
| container `/usr/local/bin/{pingbus,agent-bus-claude}`                                                                                               | symlinks to kit                                         | KIT_SYNC_PY                                          | refuses a non-link                                                      |
| container `~<agent>/.config/pingbus/<team>/{token,README,member.json}`                                                                              | agent, dir 0700, files 0600                             | PLACE_PY                                             | bundle (`member.json` has `user_id`, `base_url`, `human_text`)          |
| container `~<agent>/.config/pingbus/env`                                                                                                            | agent 0600                                              | WRITE_ENV_PY                                         | `PINGBUS_TEAMS=`, `HOOKS_DAEMON_HOSTNAME=` + foreign lines              |
| container `/etc/systemd/system/agent-bus-claude-<user>.service`                                                                                     | root 0644                                               | UNIT_WRITE_PY                                        | M:859-895                                                               |
| `$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json`                                                                                         | owner 0700/0600, tmpfs                                  | `ccy-teams internal membership-init`, session cmds   | desired v1 (HC 3.1)                                                     |
| `.../membership/<session>/relaunch.json`                                                                                                            | owner                                                   | hot add first join, restart                          | \`why: restart                                                          |
| `$XDG_RUNTIME_DIR/ccy/change/<session>.lock`, `.../alloc/<sha32>.lock`                                                                              | owner                                                   | ccy-teams, agent-bus seat take                       | flock only                                                              |
| `$XDG_RUNTIME_DIR/ccy/container-names/<name>`                                                                                                       | owner                                                   | `ccy-teams internal claim-container-name`            | `<pid> <start>` (J16 08:27)                                             |
| `$XDG_RUNTIME_DIR/fedora-desktop-plays.lock`                                                                                                        | owner 0600                                              | every play runner                                    | holder line                                                             |
| `$PINGBUS_HOME/{applied.json,active,settings.json,statusline-inner,socket.json,keeper.lock,keeper.log,down/<team>}` (container `/tmp/pingbus-home`) | container-private                                       | keeper / seat exec / watcher                         | applied v1 (HC 3.2); socket.json `{v,socket,token}` 0600                |
| `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/` (`seat.lock`, bundle, `sync.json`, inbox, goodbye record)                                     | checkout, container-writable                            | seat take / keeper / watcher                         | per seat                                                                |
| `<checkout>/.claude/ccy/pingbus/sessions/<session>/{applied.json,conversation.json}`                                                                | checkout                                                | keeper / SessionStart hook                           | report v1; conversation `{v,session,session_id,source,written_ms}` 0600 |
| `~/.local/state/ccy/sessions/<name>`                                                                                                                | owner                                                   | session registry                                     | launch args incl. canonical `--teams` (SD facts)                        |
| `$XDG_STATE_HOME/ccy-teams/declined/{description,path-limit}-<team>`                                                                                | owner                                                   | picker                                               | `never v2` (MT S-A15)                                                   |
| `~/.config/pingbus/element-accepted`                                                                                                                | owner, 0700 dir                                         | cc consent                                           | marker                                                                  |

---

## 6. Hard-won behaviours and incidents

01. **Stopped LXC container failed the whole play** (exit 78, blocked every team change). Fix:
    SKIPPED/DEFERRED, known handle from a member record, seat claims, record sweep (LS; J19 11:03).
    Review FIX-FIRST found a team's record was never removed once its last entry went, so the board
    kept telling the owner to start a container nobody declared; fixed by sweeping on every run,
    scoped by `--only-teams` (stopped-member review finding 2). First-deploy artefact: a joined
    member with no record yet shows DEFERRED plus a duplicate row until its container is up
    (finding 3). Stopped path not yet proven live on the host (J19 12:33).
02. **Inherited umask 077 through `lxc-attach`** made `/usr/local/share/agent-bus` 0700, so
    `pingbus` gave "Permission denied" (exit 126) for the agent. Fix: every guest script sets its
    own umask first (022 root, 077 agent), names every leaf mode, and repairs a root-owned too-tight
    kit dir to 0755 with `fchmod` on an `O_NOFOLLOW` fd, reporting it (umask report; M:120-126,
    143-161; J16 12:08/12:38).
03. **Seat clash across repos**: two containers of different repos were refused "seat 1 ... to two
    containers"; seats are numbered per `<repo>+<host>`. Fix keys the clash on repo too
    (seat-clash review).
04. **btrfs rootfs** (`btrfs:/var/lib/lxc/<c>/rootfs`) refused as "not a directory"; reader only
    stripped `dir:`. Fix: store table (J16 09:12).
05. **Checkouts outside home** (web roots) not found; directory picker untypeable. Fix: bounded
    multi-root search, typed path line with Tab completion, refuses control and Bidi_Control chars
    whole, paste is text (J16 13:18; J19 09:50).
06. **Live role changes for declared LXC members were reverted by the next apply.** Fix: one roles
    package writes the entry, sets live, applies, inside one play-lock hold (J16 13:47).
    Owner-accepted on the host: making a running LXC member coordinator works (J19 15:04).
07. **Launcher identity**: a ccy run from inside another session's pane took the outer session's
    name, label, membership dir and record (inherited `TMUX`). Fix: named only when the pane's first
    process is the launcher or its trampoline; Go side reads no `TMUX` (J16 02:57, 03:21).
08. **Reused tmux names** inherited a stale membership dir; fix: a membership counts only while its
    own launch's record carries the bus; `--no-bus` removes its name's dir
    (`lib/agent-bus-membership.bash:57-60`; HC:651-661).
09. **Change lock inside the read-only mount** could be held by the container forever (flock needs
    no write). Moved to `$XDG_RUNTIME_DIR/ccy/change/` (J16 06:14).
10. **Container name race**: two launches chose the same lowest free name (podman exit 125). Fix:
    claim under a short flock with liveness/pid-reuse check; rejected holding a lock until create
    (sudo prompts in between) and retry-on-conflict (name already in seats/membership) (J16 08:27).
11. **Claude Code does not re-read `--settings` mid-session** (M2 spike), so inbound is decided at
    launch: no team -> `crossSessionInbound: refuse`; first join relaunches once (J16 00:31; HC:721-741).
    The inbox socket is bound in EVERY session; its presence proves nothing (HC:701-713; J16 05:16).
12. **Watcher died on a homeserver restart** (refused connection treated as permanent) — dated to
    our own play restart, not a crash; triage found no homeserver crash on record (J16 08:58). The
    owner REJECTED the session-side fallback chain (watcher supervisor -> hook revive -> waiter) and
    asked for root cause (J16 08:58).
13. **Server-side**: Tuwunel does send sd_notify; unit is `Type=notify`, `WatchdogSec=30s`,
    `Restart=always` 5-60 s backoff; minutely health check restarts after 3 missed probes; health
    check initially queried timer-only properties on a .service (fake too permissive); Persistent
    backup timer stamp outlived a removed team; seat check/take wait up to 180 s for an
    `activating` homeserver because a user unit cannot be ordered after a system unit (J16 00:34,
    01:24-02:03).
14. **Test hermeticity**: helper tests inherited the agent-bus session env (`PINGBUS_*`), fixed by a
    scrub list in `helpers/qa_environment/session_env.py` (J16 09:46).
15. **Process grep counted claude itself** (its argv names the plugin dir): match by program, not
    argv substring (J16 07:09).
16. **QA pushes after a FAILED qa-all** twice because the push was gated on a grep of the result
    line's presence (J16 05:42; J19 13:03).
17. **No destructive action where a cursor starts; no action reachable by key only** (owner rule 14)
    (HC:549-553; J16 08:55, 10:12).

Owner acceptance: B1/B2 reboot checks and M3-M6 interactive steps still owed (ACCEPTANCE.md:30-41;
J16 12:08 "needs ccy launched interactively"). Confirmed: LXC coordinator role (J19 15:04); both
LXC members joined after the umask fix (J16 12:38). Open owner decisions 5-12 (PLAN.md:270-331).

---

## 7. What not to carry into v2 (judgements; reasons given)

01. **Three languages for one rule set.** Handle/seat derivation, env merge and the guest scripts
    are Python run on host and shipped as `python3 -I -c` strings into containers; lxc-add is Go
    calling them via `sudo -n python3`; the launcher is bash. v2: one Go binary that both the play
    and ccy-teams call; guest-side actions should not require Python 3.11 in every container
    (INFERENCE: a static Go helper copied in, or host-side file writes through the rootfs, would
    remove the Python prerequisite).
02. **Ansible as the LXC control loop.** Joining, stopped-container deferral, records and sweeps
    exist because membership is reconciled only when a play runs. A v2 reconciler (systemd timer or
    event on container start) would make DEFERRED self-healing instead of "run the play again".
03. **Legacy seat compatibility** (`default_seat` container-name seats, "entries written before host
    and seat were derived", D:572-576, DOC:205-211) is migration debt; v2 can drop it with a one-off
    migration.
04. **The member record duplicates the registry.** Known-handle via a JSON record exists only
    because the host cannot read a stopped container's bundle. v2 should keep the authoritative
    (container,user,team)->handle map on the host from the start and treat the container's bundle as
    a copy.
05. **Separate record writer kept only to avoid an image bump** (AR:6-7) — a packaging artefact.
06. **Two meanings of "live"** (ccy-teams: tmux session or restorable record; agent-bus: running
    container) (PLAN Decision 6) — one liveness definition in v2.
07. **Env-file line editing with foreign lines preserved** and the "env file only `agent-bus-claude`
    reads" contract (shell users must `set -a; .`) — v2 should give LXC members the same desired
    membership model as ccy (read-only host-written file) rather than `PINGBUS_TEAMS` in a writable
    file, unifying hot control for LXC (today lxc gets no hot add, TR A10).
08. **Version-number churn** (CCY/container renumbered on nearly every merge, J16 03:00, 10:04,
    10:45, 10:55) and acceptance scripts that failed mostly on test defects (J16 01:24-07:59) —
    process overhead, not requirements.
09. **Inert contract machinery** (flag file, shell guards in `hooks.json`, `statusline-inner`,
    relaunch on first join) exists because Claude Code fixes plugins/settings at launch. Keep the
    requirement (no Python, one resident process, refuse inbound when on no team), but it may
    belong to the planned `ccy` mod (HC:948-959).
10. **Keep** (these are requirements, not accidents): never execute container-controlled config;
    no-follow path walks with size caps; check-everything-before-changing-anything; demote before
    promote; handle stability (never re-mint a token silently); locks out of container reach;
    write record before desired file; owner-only control, never from the bus; remove does not
    revoke (until token rotation refuses a held seat); stopped is ordinary, missing is an error.
