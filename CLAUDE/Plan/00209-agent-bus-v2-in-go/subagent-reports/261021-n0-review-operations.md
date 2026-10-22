# Plan 00209 N0 integrated review: operations, failure modes and migration

Verdict: **FIX-FIRST**

Reviewer concern: operations, failure modes and migration. Read: DESIGN-v2-FRAME.md, DESIGN-v2.md
(whole), DESIGN-v2-D-migration.md (whole), the D round-3 verification and handoff, B 2.1-2.5,
2.9-2.14, C 3.2.5, 3.7.5, 3.9, 3.12, 3.15 and C 7. Checked against v1 source where a claim
depended on it: `files/var/local/claude-yolo/entrypoint.sh`, `claude-yolo` (the image entrypoint),
`files/usr/local/sbin/agent-bus-install` (`cmd_remove`), `helpers/agent_bus/admin.py`
(`_sync_team`, `park_member`), `playbooks/imports/play-agent-bus.yml` (absent handling).

Counts: 0 blockers, 13 majors, 11 minors.

## Findings

### 1. Major: the goodbye "after Claude Code exits" is killed before it is sent, in ccy and in LXC

- **Where:** B 2.4.1 step 8 (`run` execs `claude`), B 2.4.2 "When the session ends", B 2.11
  Goodbye, BD11 (SessionEnd timeout lift dropped), DESIGN-v2 section 7.
- **What is wrong.** The keeper's 5 s window only exists if something keeps the keeper and worker
  alive after `claude` exits. Nothing in the design does.
  - **ccy.** The image's entrypoint is `ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]`
    (`claude-yolo:1985`), and `entrypoint.sh:755,763` execs into the seat wrapper and `claude`. B
    makes `run` exec `claude` too, so `claude` is tini's only child. When `claude` exits, tini
    exits with its status. When PID 1 of a pid namespace exits, the kernel SIGKILLs every other
    process in it. The keeper and worker die at the same instant as `claude`: no goodbye is ever
    posted on a ccy seat. Then the host's release finds no `left` since the claim and posts
    `disconnected`. So **every clean ccy exit is announced to humans and the coordinator as a
    disconnection**. v1's SessionEnd hook ran inside `claude`'s lifetime and did post a goodbye,
    so this is a regression of R28/B28 on the most common member kind.
  - **LXC.** `teamchat-session-<user>.service` runs tmux. When `claude` exits, the pane and the
    tmux server end, the unit's main process is gone, and systemd stops the unit with the default
    `KillMode=control-group`: SIGTERM to every process in the cgroup, the worker included. The
    keeper treats SIGTERM as a session end, but the worker's own signal handling is not stated, and
    Go's default for SIGTERM is to exit. The worker can die mid-goodbye, leaving `goodbye: "attempted"`, which is re-sent only when the seat is next taken (possibly days later).
  - **Dying terminal.** No rule says where the keeper's and worker's stdout and stderr go. If they
    inherit the session's terminal, writes after a hangup fail (`EIO`), and a Go process writing to
    a closed pipe on fd 1 or 2 is killed by SIGPIPE. `seat disconnected` has an explicit dead-terminal
    rule (B 2.11); the keeper and worker do not.
- **Fix.**
  - `run` does not exec `claude`: it forks it, records the child's pid in `session.pid` (the
    pidfd, the inbox socket form `cc-socks/<pid>.sock` and the peer walk all key on that pid, not on
    `run`'s), waits for it, then waits for the keeper (bounded, 6 s) and exits with `claude`'s
    status. Or the ccy entrypoint and the LXC unit do this wait; one place, stated.
  - The worker ignores SIGTERM, SIGINT and SIGHUP; only the keeper's `end` ends it.
  - The LXC unit uses `KillMode=mixed` (SIGTERM to the main process only) and a `TimeoutStopSec`
    above the keeper's bound.
  - The keeper and worker get `/dev/null` (or the journal) for stdio, never the terminal.
  - Add a per-kind test to the VM lab and to P1: clean exit, `/exit`, a killed terminal, and a
    container stop, each asserting which of `left` or `disconnected` humans see.

### 2. Major: a session that dies and is never resumed is never announced; the ccy crash path regresses v1

- **Where:** C 3.7.5 (last bullet: "A crashed or rebooted session whose record remains ... gets no
  notice"), B 2.11 Disconnected, B 2.4.5 ("the container or guest is stopped").
- **What is wrong.**
  - ccy: a container that crashes (an OOM kill, a `claude` crash, a podman failure) keeps its
    session record, so no release runs and no `disconnected` is posted until the session is
    restored or released, which may be never. v1's launcher ran `seat disconnected` when the old
    container exited (D 6.2), so humans heard of it at once. Presence going offline is a display
    only (OD-A1), and agents never see presence, so a coordinator waiting on that member learns
    nothing.
  - LXC and host: `disconnected` is posted "when the seat is next taken". A guest that is stopped
    and not restarted, or a member removed while its guest is down, is never announced. The host
    has no copy of an LXC seat's bundle, so it cannot post as the subject.
  - The question the lead asked ("a session that never comes back may never be announced") is
    answered yes: on every member kind.
- **Fix.**
  - ccy: the launcher posts `disconnected` as soon as `podman` returns with no goodbye seen, using
    the pool bundle as now, and keeps the seat held for a restore; a restore then posts A's
    `joined` (or a new `back` kind). The claim-derived transaction id already prevents a double.
  - LXC: keep a host-held copy of each LXC seat's bundle (as the pool is for ccy), and have the
    health tick post `disconnected` when the container is stopped or the seat lock is free while
    the store's last run has `ended: null` (both are host-side reads through
    `/proc/<init>/root` or the record).

### 3. Major: a restore leaves every member's sync position ahead of the restored database, and loses events without a report

- **Where:** C 3.12.2 (whole), B 2.6 and 2.3.2 (`next_batch`, room floors); nothing in A or B
  mentions a restore.
- **What is wrong.**
  - After `restore --backup N`, the restored database's stream counter is behind every token a
    member stored. A member's next `/sync` sends a `since` from the future. What Tuwunel does with
    that is not checked (it may answer from its current position, wait until the counter passes
    the token, or refuse). The second means **silent loss** of every event posted after the restore
    until the counter catches up. No host check covers it.
  - Every event posted between backup `N`'s time and the swap disappears from the server. Humans
    see their messages vanish in Element; agents' outboxes hold pings whose events no longer exist,
    and replies to them fail `not-found`. The restore's answer lists handles that need `reissue`,
    but not the lost events.
  - A migrated team restored to a backup taken before M4 loses its carried items and welcome notice
    with no report: the frame's "nothing lost silently" fails after the migration is accepted.
- **Fix.**
  - A host check (Tuwunel's `/sync` with a `since` ahead of the database) recorded before the build.
  - The roster carries a `restore_epoch` that step 6 raises; a worker that sees it change drops
    `next_batch` and resumes as for a seat with no store (B 2.6 step 01 b), with its ledger and floors
    deduplicating.
  - Before step 5, the live database's events after backup `N`'s time are exported to
    `root/<team>/restore-<N>-lost.jsonl` and counted in the answer and the status record. A restore
    to a backup older than the team's migration `carried` time is refused unless the owner passes
    a named flag.

### 4. Major: the restore's staged copy blocks every later restore, and backup N may then exist nowhere else

- **Where:** C 3.12.2 steps 1, 2, 3, 7 and the resume rules (the major-43 fix).
- **What is wrong.**
  - Step 1 refuses any leftover `db.restore-*` or `config.restore.toml` when no `restore.json`
    exists. Step 2 creates both before any record is written, and no step removes them if staging
    fails. Step 7 (failure in 5-6) and the `swap`/`revert` resume both roll back "to the state
    before step 5", which still contains `db.restore-<N>`, and then remove the record. So after any
    failed or interrupted restore, the next `restore` refuses, naming files the owner must delete by
    hand. That contradicts "no state needs a hand-made rename to recover".
  - Step 3 takes backup `M` after staging, which prunes the oldest backup. When `N` was the oldest,
    `db.restore-<N>` is now the only copy of `N`. Deleting the leftover loses `N`; keeping it blocks
    every restore.
- **Fix.** On a failure before step 5, remove the staging under the team lock before returning.
  After a failure or `revert` in 5-7, keep `db.restore-<N>` and a record with `"step": "staged"`,
  so that `restore --resume` retries the swap from the staged copy and `restore --discard` removes
  it. Either way, name it in the status record. A fault-injection test for each step of 2-8.

### 5. Major: a crash after the start block is removed lets the homeserver serve revoked credentials until a manual resume

- **Where:** C 3.12.2 step 5 (record set to `bootstrap`, then the block removed), step 6, and the
  resume rules (the major-44 fix).
- **What is wrong.** The block is removed before bootstrap re-revokes parked, retired and reissued
  tokens and old human passwords. A power loss in step 6 leaves `restore.json` at `bootstrap` and no
  block. At boot, `teamchat-server@<team>` starts (`Restart=always`, wanted by the target): only the
  root verbs and the health restart look for `restore.json`, not the unit. The team then serves the
  restored database, with every credential revoked since backup `N` valid again, until the owner
  runs `--resume`. That includes a released hostile session's token (risk K13) and a removed
  human's old password.
- **Fix.** Keep the block through bootstrap. Run step 6 in a transient instance like step 2's
  (loopback only, staging port, the restored `db`), and remove the block only after its revocations
  are done. The resume from `bootstrap` does the same.

### 6. Major: LXC guest operations contradict between B and C, and nothing writes an LXC member's desired membership

- **Where:** B 2.13 (operations table and "Installing the binary"), B 2.10 last paragraph, BD13;
  C 3.9.1 ("no guest program run as root"), C 3.9.2 steps 4-8, C 3.9.4, C 7 item 1;
  DESIGN-v2 section 3 row 28 ("Fixed").
- **What is wrong.**
  - B runs `install-self`, `check`, `membership` and `unit` as root through `lxc-attach`, and its
    kit garbage collection reads each runtime's `session.pid`. C forbids running any guest program
    as root (the CVE-2019-5736 class), writes the kit, the `current` link and the unit from the host
    side, and collects old digests by reading `/proc/<pid>/exe` of the guest's processes. C 7 says
    every guest op runs as the agent user except `install-self`. That is three statements of one
    mechanism. Row 28 marks it fixed, but only the verb names were reconciled.
  - **No step of C 3.9.2 writes the guest's `~/.config/teamchat/membership/desired.json`.** B's
    keeper takes no seat that is not desired (B31), so an LXC member placed by the apply joins no
    team at all. Its `session`, `generation` and `inbound` fields have no source for LXC, and BD13's
    LXC hot control has no writer. `guest membership` "as the agent user" (C 7) cannot write a
    root-owned directory, and if it could, the member could grant itself teams.
  - The two collection rules differ in safety: a session whose keeper has died (`keeper-lost`)
    still runs hooks from its digest, but no process has that digest's binary as its `exe`, so C's
    rule deletes a digest that B's rule keeps.
- **Fix.** One owner for the mechanism: C's host-side writes for the kit, unit and desired file
  (root-owned paths, `RESOLVE_IN_ROOT`), with B's `session.pid` rule for collection. Add a numbered
  step to C 3.9.2 that writes `desired.json` with an explicit `generation` and `inbound`, and say
  which flow raises it on a hot change. Delete the root `lxc-attach` operations from B 2.13, or
  state why each one is safe.

### 7. Major: the linked set ignores LXC and host members, so one guest user can end up needing both systems

- **Where:** D 5.9 ("M0 computes the set from the session registry"), D 5.4 M0, D19, D5.
- **What is wrong.** ccy's session registry lists ccy sessions only. An LXC user seated in v1 teams
  X and Y runs one supervised session for both. Migrating X alone makes M6 take X out of the guest's
  v1 env (the v1 service stays for Y) and start `teamchat-session-<user>` for X. The same user, in
  the same checkout, then runs two Claude Code sessions, one per system. That is what D5 and OD-D2
  forbid, and two sessions in one checkout share a project directory and its transcripts.
- **Fix.** M0 builds the set from the session registry, from `agent_bus_lxc_members` (same container
  and user in several teams) and from host members, and P1 seeds an LXC user in two teams.

### 8. Major: a stopped LXC container that starts after M6 can run v1 and v2 sessions side by side

- **Where:** D 6.3, D 5.4 M6, C 3.9.3, DC10.
- **What is wrong.** For a stopped guest, v1's absent phase only parks the handle from the host and
  leaves the guest's files ("until it next runs"). Its `agent-bus-claude-<user>.service` stays
  enabled, and its env still lists the moved team. When the container starts (`lxc.start.auto`),
  the v1 session comes up on a parked team. When v2's play next runs, it starts
  `teamchat-session-<user>` in the same guest. Healing depends on someone running v1's play and then
  v2's play, in that order, and nothing enforces either the run or the order.
- **Fix.** v2's present phase reads, through the `migrate/v1` adapter, whether the guest still has
  v1's unit enabled or v1's env naming the team, and holds the member back as `DEFERRED (v1 not yet absent)` until it does not. ccy-teams' heal for a deferred migrated member runs v1's play, then
  v2's. P4's stopped LXC member must assert this.

### 9. Major: A8 and M8 treat M6's own v1 changes as foreign events, so M7 cannot pass for a team with LXC members

- **Where:** D 6.1 A8 ("No v1 event exists after `v1_last_event`"), D 5.4 M8 act (1).
- **What is wrong.** M6 runs v1's play for the team while it is fenced. Its LXC absent phase parks
  handles (`park_member` updates the registry), and the play's `bootstrap` then runs `_sync_team`,
  which writes the `m.room.power_levels`, team record and topic state events whenever they differ
  (`helpers/agent_bus/admin.py:584-599`). A changed team record is a v1 event after `v1_last_event`,
  so A8 fails. It cannot be "fixed forward on v2", because the v1 event is the migration's own. M8
  act (1) would also "report" these as items with no recipient.
- **Fix.** A8 and M8 act (1) count only `m.room.message` events from non-admin senders, the same
  filter as the archive check (D 5.6). Admin and state events after `v1_last_event` are listed as
  evidence, matched against `log.jsonl`.

### 10. Major: a fence lost before commit leaves rollback unfinishable, and re-fencing reuses a stale snapshot

- **Where:** D 5.7 step 1 ("A lifted fence with no record means the fence was lost without R, and R
  stops and names it"), D 5.4 M3 and M4, D 5.4 "Where the network fence lives" (last paragraph:
  "re-run M2's fence act or roll back"), RK1.
- **What is wrong.**
  - An `nft flush ruleset` or an `nftables.service` restart during M3 or M4 is the case RK1 names.
    M3 or M4 then stops. The owner's two routes both fail:
    - R stops at step 1, because the fence is gone with no `state_check` record. Neither the
      `--accept-state-diff` path nor anything else is said to get past this, and "R is never
      left unfinishable" is then false.
    - Re-running M2 skips every act `fence.json` marks done, the snapshot included. The carry plan
      is then built from a snapshot taken before the watchers synced and delivered through the lost
      fence, so items v1 has already delivered are carried again without `possible_duplicate`.
  - After commit, nothing checks the fence. A flush between M5 and the M6 restarts reconnects the
    still-running v1 watchers, which deliver items that are also carried on v2.
- **Fix.**
  - A fence lost before commit invalidates the snapshot. The only route is R, and R's step 1 on a
    lost fence records `state_check: fence-lost` and goes on, because v1 has been live and its
    delivery is correct. The report names the window.
  - Remove "re-run M2's fence act".
  - M6, M7 and M8 check `fence-status` at their start and re-apply the fence if it is gone. After
    commit, re-fencing is always right.

### 11. Major: M8 stops v1 units outside Ansible, against v1's declaration, and any v1 play undoes it

- **Where:** D 5.4 M8 act (4), D 5.5 `StopArchive` and `StartArchive`, D 5.6 Archive, D 8 "After the
  move", DESIGN-v2 section 6 last row.
- **What is wrong.**
  - `systemctl disable --now` of `agent-bus-hs@<team>` and its timers is a lasting system change
    made outside any play, while `agent_bus_teams` still declares the team present. D says itself
    that any v1 play run then starts the server again. A routine full v1 run, or meta-deploy running
    v1's play for another reason, re-enables the archive and its health and backup timers. They stay
    resident until someone runs `archive close`. So "nothing resident" (D 8, DESIGN-v2 section 6) is
    true only until the next v1 play.
  - That is drift between the declaration and the host, which the IaC rule forbids.
- **Fix.** v1 already has the state: `state: absent` without `purge` stops and disables the server
  and both timers, removes the firewall rules and the drop-in, and keeps the data
  (`agent-bus-install` `cmd_remove`, `play-agent-bus.yml:55-60`). M8 act (4) sets the v1 entry to
  absent (no purge) through ccy-teams' editor and runs v1's play for the team. `archive open` and
  `archive close` flip the declaration and run the play, or are named as transient operational
  acts that the board reverts on its next tick. P1 must check that the absent entry keeps
  everything `archive open` needs to restore it (port, humans), or that ccy-teams holds those
  fields. Then the cost rows are true.

### 12. Major: M0 does not check that the deployed ccy launcher, ccy image and ccy-teams can run a v2 team

- **Where:** D 5.4 M0 ("checks that v2 software is installed at an accepted version"), D 3.9,
  section 12 "ccy (shared)", DESIGN-v2 section 3 rows 53-55 (Later N4, N6).
- **What is wrong.** The decision function, the refusals of 3.9, the v2 mount layout and the v2
  kit in the image are ccy changes, each a CCY version bump, deployed by plays other than
  `play-teamchat.yml`. A migration run with an older launcher commits, then the M6 relaunch, or any
  later `ccy --teams`, takes v1's path for a team that is now v2-live. The session comes up on a
  fenced v1 with a parked handle, and its v2 items wait unread. A launcher with the logic but an
  image without the v2 kit fails the relaunch after commit, and the team is bus-less until the
  image is rebuilt.
- **Fix.** M0 refuses unless the installed `claude-yolo` launcher, the built ccy image (its v2 kit
  digest) and ccy-teams report at least the versions that implement 3.9 and 6.2. It names the
  meta-deploy entry that brings each up to date.

### 13. Major: outboxes that cannot be read have no fate, so pending acknowledgements are dropped silently

- **Where:** D 5.2 row 5 ("From the snapshot"), D 5.8 remote members, carry plan
  `state_read_from: server-only`, D9.
- **What is wrong.** Row 5 settles pending pings from the snapshot's `outbox.json`. A remote member
  and a member whose state cannot be read (P1 seeds one) have no snapshot. Their pings that are
  awaiting acknowledgement get no `moved` notice and no `TIMEOUT`, and the member starts v2 with a
  fresh outbox. No `reported` kind names them. That is the silent fate D9 forbids.
- **Fix.** For every member with `state_read_from: server-only`, derive its open pings from the
  server: its own ping events with no reply from the target before the fence, with the deadline from
  the event content. Settle them as row 5 does, flagged `possible_duplicate`. Add an
  `unreadable_outbox` row per such member to `reported`.

### 14. Minor: "one confirmation, no other prompt" depends on a sudo timestamp that the run can outlive

- **Where:** D 5.4 (who runs a step; "The owner confirms once"), C 3.6.6 / C10 (sudo made ready once
  per change, `sudo -k` at the end).
- **What is wrong.** M2's 60 s wait, the export, the carry posts and M6's per-session bounds (300 s
  plus 180 s each) can outlast the default 5-minute sudo timestamp. A password prompt in M2-M5 then
  stretches the bus-less window while the owner is away, and sudo's own prompt timeout fails the
  step with the team fenced.
- **Fix.** ccy-teams runs `sudo -v` before each root step. Or run the root acts of M2-M5 in one
  `sudo teamchat-admin migrate run-fenced --set S` call. State which one.

### 15. Minor: a worker crash loop never ends, and the recovery for `worker-failed` and `keeper-lost` is not named per kind

- **Where:** B 2.4.2 Supervising the worker, B 2.4.5, B 2.2.4 exit 69, B 2.12 item 7.
- **The lead's question, answered.**
  - With the worker down, every agent verb exits 69 at once. The skill tells the agent to tell a
    human. A human attached to an LXC member's tmux sees the agent say so, and a red
    `bus runtime not running` (lock free) or `(failed)` in the status line.
  - The keeper restarts the worker after 1 s, doubling to 60 s, with the backoff reset after
    10 minutes of running. That is acceptable for a transient crash, and it is not a regression
    against v1's direct mode for one.
- **What is wrong.**
  - A worker that panics on the same input at every start, for example a store or event it cannot
    handle, loops for ever at 60 s. It is never marked `failed`, and agents are told "restarting"
    indefinitely.
  - A final 78 and a keeper crash leave the session without a bus for its life, and the design
    names no recovery for an LXC or ccy human. `repair` covers `keeper-lost` only, and it must be
    run by the owner through `lxc-attach` or `podman exec`.
- **Fix.** After 5 restarts with no 10-minute run, mark the worker `failed (crash loop)`. Make the
  one hook line and the status line name the recovery per kind: `ccy-teams restart <session>`,
  `systemctl restart teamchat-session-<user>` in the guest (through ccy-teams), or `repair`.

### 16. Minor: the keeper's "no wakeup of its own" bar ignores Go's runtime

- **Where:** B 2.14 (idle wakeups and the acceptance bar), DESIGN-v2 section 6.
- **What is wrong.** The keeper is a Go process. Go's forced GC wakes it every 2 minutes, and
  `sysmon` runs too. B lists these for the worker but not for the keeper. The bar "no wakeup of its
  own over the 10 minutes" is either unmeasurable as written or will fail.
- **Fix.** Say that the runtime's wakeups are counted separately, with their expected number. Or
  have the keeper set `debug.SetGCPercent(-1)`, which its tiny heap allows, and measure that.

### 17. Minor: the v1 export is said to be backed up, but C's backup does not include it

- **Where:** D 5.6 Export ("backed up by the host's `teamchat-backup` with the team's state"), C
  3.12.1 step 3 (the tar holds `ctl/<team>/`, the config and `team.json` only).
- **Fix.** Add `migration/v1-export/` (and `carry-plan.json`, `log.jsonl`) to the backup, or correct
  D. Either way, say that DC14 means it stays on the same disk.

### 18. Minor: the merged roster compares wall-clock times across a clock step

- **Where:** C 3.12.2 merged roster (`token_ms`, `revoked_ms` "after the backup's time").
- **What is wrong.** A backward clock step (an NTP correction after a suspend) can put a reissue
  that came after backup `N` before `N`'s timestamp. Its older token is then not revoked.
- **Fix.** Compare roster `rev` instead: record the `rev` of each token issue and revocation, and
  the backup's roster `rev`.

### 19. Minor: disk full can strand a fenced team

- **Where:** D 5.4 M0 and M3, D 5.7 step 1, C 3.12.1.
- **What is wrong.** M3's export and the M2 backup share one filesystem with every team's
  homeserver and backups. A full disk mid-M3 stops the run fenced. R's first act is an atomic write
  of `state_check`, which also fails, so R cannot reach the unfence. Every team on the host shares
  that filesystem, so its other homeservers suffer too.
- **Fix.** M0 requires free space above a stated multiple of the rooms' export estimate. R does the
  unfence even when it cannot write the `state_check` record, and names it. Health reports free
  space on `/var/lib`.

### 20. Minor: M6's v1 play can redeploy v1 in place for every other v1 team

- **Where:** D 5.4 M6 (`ApplyV1Play(team)` with `agent_bus_only_teams`), LESSON 10.
- **What is wrong.** A team-scoped v1 play still runs its global tasks (the kit, units, sudoers).
  If v1's tracked code has moved since v1 was last deployed, migrating one team replaces the v1 kit
  under every running v1 team. That is the in-place kit replacement LESSON 10 records, and it means
  another team's migration touches running teams.
- **Fix.** M0 runs v1's play in check mode (or compares the installed v1 kit with the checkout) and
  refuses while it would change anything outside the team.

### 21. Minor: the user play lock blocks other teams' flows for the whole of M2-M5

- **Where:** D 5.4 ("From M2 to M5 `ccy-teams migrate` holds the user play lock itself").
- **Fix.** State the expected length of the window and what a waiting flow of another team does
  (waits with a bound, or fails naming the migration). Make sure the ccy launcher's own flows never
  take that lock.

### 22. Minor: the fence lives outside IaC, and the rollback needs the set lock

- **Where:** D 3.5 fence objects, D 5.3 set lock, D 5.7.
- **What is wrong.** `teamchat-fence@<team>` is enabled by `teamchat-admin`, not by a play. Nothing
  says that `play-teamchat.yml` leaves fence instances alone; a play that converges "enabled v2 units"
  would lift a fence. R is not said to take the set lock, so it could run beside a live `migrate`.
- **Fix.** State that the fence is migration runtime state that the play never prunes, and test
  it. R takes the set lock non-blocking and refuses while a step holds it.

### 23. Minor: the fence proof must use each member's real `base_url`

- **Where:** D 5.4 M2 (f), M0's loopback `base_url` refusal.
- **What is wrong.** Under pasta, a connection to the gateway address mapped to the host's loopback
  (`--map-host-loopback` or `host.containers.internal`) reaches the host from `127.0.0.1`, which the
  fence accepts. M0 refuses only a literal loopback `base_url`.
- **Fix.** M2 (f) connects to the exact host and port in each running member kind's bundle. M0 also
  refuses a `base_url` whose address pasta maps to the host's loopback.

### 24. Minor: the resident-cost figures disagree between chapters

- **Where:** DESIGN-v2 section 6 and C 3.15 (Tuwunel 100-135 MB from the 00161 spike) against D 8
  ("v1 has no measured memory figure"). DESIGN-v2 section 6 says "timers disabled" with no team; D 8
  lists the two host timers under "no team".
- **Fix.** One statement of each fact, in C 3.15, cited by the others.

## Checked and found sound

- **Commit.** One rename of the set record under the set lock, with its four preconditions checked
  under the same lock. Copies are repaired from the set record, and a copy ahead of it stops the
  run. The decision function shared by the launcher and ccy-teams covers `origin` and
  `rolled-back`. Linked sets commit in one rename. This is a real atomic commit, and rollback
  before it is coherent for ccy members.
- **The verifier's seven fixes** (`261021-n0-d-migration-review-3-verify.md`), each read in the
  current text:
  1. M8 order: unlock and password are moved to `archive open` under the default, and before the
     stop under (a). Each act is logged and resumed. Act (1) is skipped after the marker. Sound,
     apart from finding 11.
  2. Every item carried from a receipt or join cut is `possible_duplicate`. The `ReadReceipts`
     route through the admin's `/sync` is named. Sound.
  3. R's `state_check` record and its skip on a re-run. Sound, apart from the lost-fence case
     (finding 10).
  4. The `fence.nft` rules are literal, one per family, with `reject with tcp reset` and the 60 s
     wait. Sound.
  5. Ruleset flush is in RK1, `After=nftables.service firewalld.service` is set, and M3 and M4
     check `fence-status` at both ends. Sound before commit; after commit, see finding 10.
  6. Archive check: a stopped-archive skip by `ActiveEnterTimestamp`, humans' `m.room.message`
     only, an `archive-check.jsonl` sink, and A9's v1 half moved after M8. Sound.
  7. 5.9's leave runs before any set record exists. Sound.
- **Fates of queued and in-flight items** for local and stopped members: unsynced, synced
  undelivered, consumed while out, unconfirmed injects, pending acknowledgements with a reply in the
  carry set, open questions with `question_floor`, cut responses, unsent Element messages, archive
  posts and post-fence events. Each has one fate, and the duplicates are flagged. (The exception is
  finding 13.)
- **The network fence.** It is a v2 object, changes no v1 file, works on established long-polls,
  is reboot-safe through the oneshot unit, and is proven on three routes. "An accept in another
  table does not undo a reject" is correct.
- **v1 untouched by a v2 install.** Disjoint names, the static gate, `InaccessiblePaths=`, the VM-lab
  footprint diff against a live v1, the separate `teamchat0` interface and Tuwunel binary, and
  firewall rules converged permanent and runtime without a reload. (The exceptions are findings 12
  and 20.)
- **Homeserver restart mid-sync.** It is transient, the team is marked down after 15 s, and other
  teams are not affected. Freeze and thaw reconnect by the `CLOCK_BOOTTIME` gap. Carried posts use
  transaction ids that Tuwunel stores across a restart.
- **Restore.** Staging before the pre-restore backup (major 43) does stop `M` pruning `N` before it
  is copied. The start block with `AssertPathExists=!` (major 44) does stop an empty database from
  being served, and `restore.json` gates every root verb. The fixes hold apart from findings 4, 5,
  3 and 18.
- **The worker and keeper.** A worker crash returns handed items as `repeat` and re-sends `sending`
  with the same transaction ids. A keeper crash keeps the seats until the worker has stopped
  writing. Deadlines on `CLOCK_BOOTTIME` survive clock steps. A full disk on the member stops only
  that team's sync advance and loses nothing.
- **Member cost.** The keeper and worker estimates, the measured acceptance with stated bars, live
  `status` reporting, no TCP port and one socket are honestly stated (apart from finding 16).
- **IaC and meta-deploy.** Host_vars edits go through ccy-teams' editor under the play lock, plays
  run through `run.bash`, `migrate-declared.bash` keeps team names out of `meta-deploy.list`, and v2
  software comes through the plan's `deploy.bash`. (The exceptions are findings 11 and 22.)
- **The "disconnected only at next take" default.** For LXC and host this is not a regression:
  v1 posted nothing for them (B29). For ccy it is one (finding 2).

## Outside this concern, briefly

- B 2.4.1 says pidfd is needed "in every kernel a member runs on: EL9's 5.14". An LXC guest runs on
  the host's kernel, so EL9's kernel matters only for VM members. The wording should say so.
- BD9 in DESIGN-v2 section 7 still reads "next worker (LXC, host) or the host's release (ccy)",
  which finding 2 would change.
