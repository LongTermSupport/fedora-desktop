# Plan 00209 N0, round-2 re-check (tip 63c00f98, diff 14e8e4d4..63c00f98)

Verifier, not author. Narrow scope: the ten round-1 verifier majors, new defects in the round-2
text only, and a sample of the round-2 minors. Read: the two verdict files, the two round-2
dispositions, the merge report's Round 2 section, and the cited sections at the tip (B 2.3.2, 2.4.1,
2.4.2, 2.4.4, 2.6, 2.7.3, 2.7.5, 2.11, 2.13, B 6 C3/C10/C13; C 3.3, 3.5.1, 3.5.3, 3.7.1-3.7.5,
3.8, 3.9.2-3.9.4, 3.11, 3.12.2, H-C15; A 2.3, 6.7, 6.10, 7.4, 7.6, 8.1; D 6.1 A4/A7; DESIGN-v2
rows 27, 44, 53, 56-59). The ccy supervisor claims were checked against the real
`.claude/ccy/claude-supervise.py`: it has no `SIGTERM` or `SIGHUP` handler, runs `claude` once
under `pty.fork`, and exits 75 for a relaunch. B's description of it is accurate.

## Verdict: FIX-FIRST

Eight of the ten majors are fixed in the text. Two are PARTIAL (cd 3 and cd 5). Round 2 adds
**0 blockers, 5 new majors, 9 minors**:

- the ccy launch line now written in B 6 C3 and row 53 is one that B 2.4.1 refuses with exit 64;
- the owner's new minute sweep posts a false `disconnected` while a ccy container starts, and that
  post then suppresses the real one;
- an LXC unit whose Claude Code exits at once loops for ever and posts `disconnected` and `back`
  each time;
- `team approve --only <field>` approves entries an agent planted inside the same list field;
- the Element profile's `base_url` is not a gated field.

## The ten round-1 majors

| Finding      | State          | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------ | -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| verify-ab N1 | VERIFIED-FIXED | B 2.3.2 "The resume marker" (floor below every owed item, `pending`, held and `handed` items included; lag costs only redelivery); 2.6 step 01 (b) (earlier of marker and `released_ms`; `dir=b`, so the bound gives up the oldest); 2.7.5 (receipt is display only); B49 reworded; 2.15 resume-bound row. A restore can only move the marker back: it is room account data, the floor never decreases, and step 5 writes none. Residual: minors 2 and 6 |
| verify-ab N2 | VERIFIED-FIXED | B 2.4.2 "When CMD is a wrapper" (pid from the socket facts, pidfd signalling past the supervisor, the end waits for both, one Claude Code per run, end reason cleared at every SessionStart); 2.11 ccy stop trace (12 s of 20); 2.15 VM-lab row with the real supervisor. The launch line it is paired with is refused by `run`: new major 1                                                                                                             |
| verify-ab N3 | VERIFIED-FIXED | A 2.3 (reserved `probe` seat), 5.1 (role `probe`), 6.7 (sender class), 7.4 (no notices), 7.6 rule 08, 8.1 (`check`: no ref, `ack`, probe-only sender; probe addressable for its own answer), 13.3 template; C 3.3; D 6.1 A4/A7; row 44. Residual: minors 4 and 5                                                                                                                                                                                         |
| verify-ab N4 | VERIFIED-FIXED | C 3.9.2 absent phase 1-5 (park first, then rewrite, unit stop, wait, remove); a held seat is `DEFERRED` with a dead token; 3.11.2 step 6 posts only for a handle still `active`, so the parked seat draws no tick notice; 3.14 LXC row                                                                                                                                                                                                                   |
| verify-cd 1  | VERIFIED-FIXED | C 3.12.2 merged roster (every `active` `cred: password` handle gets `needs: reissue`), step 5 revokes it in the staged copy, the "dead token" paragraph, 3.7.3 hot reissue; B 2.6 step 08 resumes on the `token` rename; row 59. Residual: minor 3 (LXC)                                                                                                                                                                                                 |
| verify-cd 2  | VERIFIED-FIXED | C 3.7.3 "What (c') costs", 3.11.2 step 3 (devices and deactivated flag), K12, DC16, DC19, FRAME. Residual: minor 8                                                                                                                                                                                                                                                                                                                                       |
| verify-cd 3  | PARTIAL        | C 3.5.1: the roster is rendered only from `root/<team>/team.json`, no member, role or human verb takes a declaration, LXC entries are gated, and `lxc apply` checks them. But `--only <field>` approves the whole field, planted entries included (new major 4). The Element `base_url` stays outside the gate (new major 5)                                                                                                                             |
| verify-cd 4  | VERIFIED-FIXED | C 3.9.4 `ExecStart` = B 2.4.1's grammar, with `--claude --kind lxc --desired <home>/.config/teamchat/membership --resume-recorded -- claude`; B 2.4.1 step 5 defines `--resume-recorded`; the parse test in 3.14. What the unit does when CMD fails at once: new major 3                                                                                                                                                                                 |
| verify-cd 5  | PARTIAL        | DC21: `teamchat-seats.{service,timer}` and `seats sweep` (C 3.7.5, 3.5.1 step 7, D 3.5, B 2.11 "Who calls it", row 27). Its "seen held" test misfires during every launch window (new major 2), and ten B/A sentences still name the health tick for ccy and host seats (minor 1)                                                                                                                                                                        |
| verify-cd 6  | VERIFIED-FIXED | C 3.12.2 steps 4-5 (loopback-only listen in `config.restore.toml`, the staging unit's own `SocketBindAllow=`/`IPAddressAllow=`, the child's loopback allow); H-C5/H-C15 paragraph: no fallback into the live path, DC8 returns to the owner                                                                                                                                                                                                              |

## New findings

### 1. major: the ccy launch line that round 2 wrote is refused by `run` (B 6 C3, DESIGN-v2 row 53, against B 2.4.1 step 5)

Round 2 spells out the ccy launch as
`teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <supervisor> ... -- claude`
(B 6 C3, row 53). The supervisor needs that inner `--`: today's `CCY_CLAUDE_WRAPPER` is
`... claude-supervise.py --arm --`. B 2.4.1 step 5 is unchanged: "a CMD whose arguments already
contain `--settings`, `--plugin-dir` or a bare `--` is exit 64". So every ccy launch, the main
member kind, exits 64 before the link starts. The VM-lab row "the real hooks-daemon supervisor as
CMD" would catch this, but only after the code is written to one of the two texts.

Fix: say in 2.4.1 step 5 which `--` is refused. The refusal guards against appended flags being
read as a prompt by Claude Code, so it should apply only to a `--` that comes after the argument
that starts Claude Code. Either the launcher names that argument (for example `run --claude --wrapper-args N`), or `run` allows exactly one `--` directly before an argument whose base name
is `claude`. Then add row 53's exact line to the parser test that C 3.14 already has for the LXC
`ExecStart`.

### 2. major: the owner's sweep posts a false `disconnected` while a ccy container starts, and that post then hides the real one (C 3.7.5 DC21 bullet, C 3.7.1 `held.json`, B 2.11 "Who calls it")

The sweep acts "for a restorable session whose seat lock is free (L3) and whose
`held/notice.json` does not record this claim". It judges "seen held" by the pool seat's `held/`
(B 2.11). C 3.7.1 defines a session as restorable when its session record exists. During every
launch, the launcher writes the record first and then takes the seat (`held/` exists from take
step 2), and only then runs `podman run`. `entrypoint.sh` runs its whole setup before it execs
`run`, and the keeper takes the seat lock only at `run` step 6. For those seconds, every condition
holds.

Scenario:

1. A sweep fires inside the start window. With a 60 s period and a start of several seconds,
   this happens on a fair share of launches.
2. The sweep posts `disconnected` as the member and records `held/notice.json` for the claim.
3. The new link then posts `back`, because the member's latest notice is `disconnected`.
4. The humans and the coordinator get two false notices, and each costs the coordinator a turn.
5. Later in the same claim the container is killed (OOM, `podman kill`, power loss). Both the
   launcher and the sweep now skip the seat, because `held/notice.json` already records this
   claim (C 3.7.3 release step 3, 3.7.5). The real disconnect is never announced, and the last
   thing the coordinator was told is `back`.

The board's background pass had the same window, but it ran only while the board was open. Now
the window is open every minute.

Fix: judge "seen held" for ccy seats as the LXC tick does. The sweep keeps an owner-side record of
the seat locks it observed held, with the claim. It posts only when a lock it saw held at an
earlier sweep is now free. This still covers a failed link (B48 frees the locks) and a crash, and
it never fires before the keeper first takes the lock. Also scope `held/notice.json` to the event
it recorded, so that a later real end in the same claim is still announced. One way: after a
`back`, the link or the sweep clears it.

### 3. major: an LXC session whose Claude Code exits at once restarts every 10 s for ever, and posts `disconnected` and `back` each time (C 3.9.4, B 2.4.1 steps 6-7, 2.11 end table, 2.13 `check`)

The unit has `Restart=always`, `RestartSec=10` and no start limit (with a 10 s delay, systemd's
default burst never trips). `run` takes the seats and starts the link (step 6) before it starts
CMD (step 7). Nothing in 2.4.1 says what happens when CMD cannot start or ends at once. By 2.11's
table, an end of `exit` with no SessionEnd reason posts `disconnected`, and the next run posts
`back`. So any CMD that fails at once gives two notices to the humans and the coordinator about
every 10 s, unattended, for as long as the guest runs. Each notice costs the coordinator a model
turn. Three ways to get there:

- The command is the bare word `claude`, found on the unit's `PATH`. systemd's default `PATH` has
  no `~/.local/bin`, where the native installer puts Claude Code, and `guest check` (B 2.13) never
  checks that `claude` resolves on the unit's `PATH`. v1's launcher failed fast with
  "claude is not on PATH".
- Claude Code refuses its configuration.
- `--resume-recorded` names a transcript that makes Claude Code exit.

Fix:

- `run` treats a CMD that fails to exec, or ends before any SessionStart, as a launch failure. It
  posts nothing (the run never reached Claude Code), and `keeper.log`, `report.json` and the
  board say so.
- The unit sets `StartLimitIntervalSec`/`StartLimitBurst` so that a crash loop stops, and the
  tick reports the unit as failed.
- The unit sets an explicit `Environment=PATH=...`, and `guest check` resolves `claude` on
  exactly that `PATH`, failing the phase otherwise.

### 4. major: `team approve --only <field>` approves an agent's planted entries inside the same field (C 3.5.1 "ccy-teams' own flows", DC18)

`--only <field>` compares every *other* gated field with `approved.json` and then "writes the one
field and answers its difference, which ccy-teams shows". It shows the difference after it has
written it. `humans` and the LXC member entries are lists. Scenario:

1. An agent adds an LXC entry to host_vars that places a guest user it controls into team T. That
   entry creates an account and places a token (C 3.5.1's own reason for gating it).
2. Later the owner adds a different LXC member to T from the board.
3. The flow runs `--only lxc` with the stdin declaration, which holds both entries. The other
   fields match, so the whole list is approved.
4. The next `lxc apply` creates the planted member.

The same holds for a planted human. The text's claim, "a field an agent planted earlier is never
approved behind a change the owner made from the board", holds across fields but not within one.

Fix: the flow passes the owner's intended change itself, for example
`--only humans --add <name>` or `--only lxc --add <team,container,user>`. Root refuses with 70
unless the new field equals the approved value with exactly that change. As an alternative, root
returns the field's difference in a dry run, ccy-teams shows it, and the owner confirms before the
write.

### 5. major: the Element profile's `base_url` is outside the DC18 gate (C 3.8 `teamchat_element_teams`, 3.10, 3.5.1 DC18)

DC18 gates the team's `url`. But `teamchat_element_teams[].base_url` (C 3.8, "optional; default
the team's url") is a free URL in the same agent-writable file. It is handed to `element apply`,
an "anyone" verb that the owner's Element play runs. `element apply` writes the human's Element
profile from it. Scenario:

1. An agent sets `base_url` to a server it can reach and serve from.
2. The owner's next Element play rewrites the profile. The consent gate is a one-time record, not
   a per-change check.
3. The human signs in at that server with their team password.
4. The agent's server now holds a credential with which it can act as a human in the team.

DC18 exists to stop exactly this: an edit that changes who can act in a team.

Fix: `element apply` takes no URL from its input. It uses the approved `url` from `approved.json`
(0640 root:owner group, so the owner can read it). Or `base_url` becomes a gated field, compared by
`declare check` and refused by `element apply` on a difference.

### Minors

1. **Stale "health tick" for ccy and host seats** (B 1.5 line 210, 2.4.2 ×2, 2.4.5 ×2, 2.11 rule
   text "for every kind the first health tick" and "the launcher's call and the health tick", the
   end table's ccy-killed and host-power-loss rows; A 7.4 line 2084). DC21 moved these seats to the
   owner's sweep and its after-login gap. Fix: name the sweep and its gap in each, as "Who calls
   it" already does.
2. **H-C15 asks for two incompatible facts** (C 8, H-C15). "Every seat's `teamchat.floor` equals
   the backup's", and also "a held ccy session works again after ccy-teams' hot reissue". A held
   session that works again PUTs its store's floor at once (B 2.3.2), so the first fact fails as
   soon as the second holds. Fix: check the markers before the reissues, or for seats no session
   holds.
3. **After a restore, an LXC seat reissued since `N` stays dead until the next play** (C 3.12.2
   "dead token" paragraph). The paragraph says "LXC guest seats need nothing more ... `lxc apply`
   reissues a token that is refused". But `lxc apply` runs only from the play. A guest token whose
   `cred_seq` is above `N` is unknown to the restored database, so that member's link stops at the
   401 (B 2.6 step 08) until the owner runs the play. Fix: ccy-teams' post-restore pass also runs
   `lxc apply --only-teams T` in the same sudo session, or the answer names the play.
4. **The probe's `run --seat T/S` and its verb route are unspecified** (B 2.2.1 row, 2.4.1 step
   6; C 3.3).
   - `--seat` is in the grammar, but 2.4.1 takes seats only from the desired membership. Nothing
     says where a `--seat` bundle is read (`<v2-bundle>` under `--state`) or that its lock is
     taken.
   - CMD is `sleep infinity`, and `run` sets `TEAMCHAT_RUNTIME` only in CMD's environment, under
     a random run id. So the `migrate` verb that then runs `teamchat ping check` and `status`
     outside the session has no stated way to reach the probe's `link.sock`.
     Fix: define `--seat` in 2.4.1. Make the probe's CMD the check itself (`ping check` then
     `status`), so it inherits the runtime.
5. **A 6.10 defines filters for worker and coordinator only.** The probe's link syncs "with A's
   filter for the member's role" (B 2.6 step 02), and role `probe` has none. Fix: a probe column
   (main only, the same types).
6. **A crash right after a hot re-add can deliver what was posted while the member was out** (B
   2.6 step 01 (c), 2.3.2). (c) persists `fresh: false` and only then PUTs the marker, at most every
   10 s and with failures retried. If the session dies before the PUT lands, the next store-less
   start resumes from the marker written at the removal. Rooms are `history_visibility` `shared`
   (A 4), so everything posted while the member was out is fetched. Items after `released_ms`
   (the hot leave's release) are delivered as new, against B33. Fix: in (c), keep `fresh: true`
   until the marker PUT has been answered.
7. **The DC18 rows disagree on the LXC entries.** C 4's DC18 row and C46 list `humans`,
   `coordinator`, `allow_from`, `listen`, `url`, `trust` and existence. C 3.5.1 and DESIGN-v2's
   DC18 add the LXC members. Fix: add them to C 4 and C46.
8. **The account watch misses a plain password change** (C 3.11.2 step 3, FRAME, DC16). An owner
   process can change a pool seat's password with `logout_devices: false`. That adds no device and
   deactivates nothing, so the tick reports nothing, and the change shows only at the next take's
   `M_FORBIDDEN`. Meanwhile the FRAME says "which the health tick reports". A device logged in and
   out between two ticks is not seen either. Fix: say "devices and deactivation, within a tick; a
   password change at the next take", or have the tick try a login with the pool's password,
   which it cannot hold (C24). The wording fix is the honest one.
9. **The stop before the first hook op** (B 2.11, the paragraph after the ccy stop trace). It says
   "step 6's bound still ends it". Before the socket facts arrive, the keeper has no pidfd on
   Claude Code, so it can neither signal it nor see it end. In ccy the container's end kills it,
   which is harmless. For another wrapper kind it is not, by 2.4.2's own rule. Fix: say that the
   container or job end covers this case, and that a wrapper launch without socket facts (for
   example one whose Claude Code version creates no `cc-socks` socket under `inbound: refuse`)
   ends on CMD alone. Name the per-version socket host check for `refuse`.

## Round-2 minors sampled

Each of these is in the cited section as the disposition says:

- ab N5 (`back` at every start, B 2.11);
- ab N6 (claim seed and `run.id`, B 2.11; C 3.11.2 step 6 uses the recorded run id);
- ab N7 (B 2.6 step 01 (a));
- ab N8 (A 6.10);
- ab N9 (A 13.3 reminder `thread Q<n> --ask`);
- ab N11 (B 2.12 item 1);
- ab N13 (B48 paragraph);
- cd minors 1 (`lxc-online.json` under `/var/lib/teamchat-status`), 2 (C 3.9.1 `.../bin/teamchat`),
  3 (C 3.8 "pairs that very v1 name"), 4 (C 3.5.3 minimal record) and 6 (C 3.12.2 "every root verb
  that takes that team's lock").

## Checked and found sound

- **The resume marker and restore.** The floor moves only forward. Missing deliveries return to
  `pending` already below the floor, and held-back flood items never enter it. The marker is
  restored with the database and step 5 writes no account data, so a restore moves the start
  back, never forward. A PUT retried into the restored database before step 08's epoch read writes
  the store's floor. That floor is still below every owed item and only lies past events the
  restore lost, so it is harmless. A hostile writer can only make the marker undecodable (start
  from the join, which costs redelivery) or move it forward, which `released_ms` bounds; under
  (c') its token is dead at release anyway.
- **The ccy stop chain against the real supervisor.** tini forwards only to its one child, the
  keeper. The keeper signals Claude Code by pidfd, and the supervisor follows its child out with
  its status, or 75. The timings fit: worst case 7 s plus 5 s, against `--stop-timeout 20` (B 2.11,
  D 12). The peer check walks `ppid` across the supervisor's pty session, and pidfd works on a
  non-child in the same pid namespace.
- **The absent phase.** Park first means nothing the guest does delays the revocation. The tick
  skips parked handles. `DEFERRED` is retried at every apply.
- **The probe.** It cannot ask for real work: `check` is probe-only, has no ref, and its answer is
  `ack`. The probe is addressable only for its own answer. No admin or lifecycle notices are
  posted for it, it is outside the pool and the tick's lists, and `--state` keeps the owner's
  seats untouched.
- **The staging restore.** The loopback-only listen, the transient unit's own allow lines, the
  child's `JoinsNamespaceOf=` (the staging unit is active when the child starts), and no silent
  fallback.
- **The hot reissue.** Every write is fd-relative with `O_EXCL` and rename, only `token` changes in
  the session directory, and B's watch acts on `token`. A failure stays `needs: reissue`, and the
  next take reissues it.
- **Names across chapters.** `teamchat.probe+<site>.host`, `check`, `--state`,
  `teamchat-seats.timer`/`seats sweep`, `teamchat.floor` and the LXC `ExecStart` line are the same
  wherever they appear. The exception is row 53 and B 6 C3 against B 2.4.1 (major 1).

## Owner decisions (not findings; the default the design took)

- **DC21 without lingering.** ccy seats are announced after a reboot only from about a minute after
  the owner's first login. Meanwhile an autostarted LXC coordinator learns of dead ccy members only
  from TIMEOUTs. Default: the play does not enable linger.
- **DC16 (c') kept.** An owner process holding a pool password has power over the seat's account,
  and the watch is best-effort (minor 8 is about its wording). Default: kept, narrowed by Tuwunel
  config if H-C13 finds a key; DC19 (b) not built.
- **DC8 with no fallback.** If H-C5 or H-C15 fails, `restore` is not shipped in this form and DC8
  returns to the owner. Default: no restore into the live path.
- **The probe's footprint.** Each check writes the roster twice (return, park), and the handle's
  join and leave show in Element's room state and timeline, though no teamchat notice is posted.
  Default: accepted.
- **`--resume-recorded` on every unit start.** An LXC supervised session always resumes its last
  recorded conversation: after a deliberate `/exit` (10 s later), and when a user re-added to a
  team after a gap gets its unit back. Default: always resume.
- **The FRAME was edited by a fixer.** The DC19 sentence now grants owner processes power over pool
  seat accounts. The FRAME holds the owner's binding directions, so this widening needs the owner's
  own confirmation. Default: the fixer's wording.
