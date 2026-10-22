Verdict: FIX-FIRST

# Adversarial review: DESIGN-v2-B-member.md (Plan 00209 N0, area B)

Judged against DESIGN-v2-FRAME.md and the N0 common rules. Sources sampled: DESIGN.md section 2
(R21-R50), RESEARCH-pingbus.md section 5, Plan 00194 SPEC-inject E14-E18, Plan 00203
SPEC-hot-control H10-H35, Plan 00211 SPEC sections 4.4-4.5 and 6.

The design is strong in shape. One owner of state, typed records, exit codes with one meaning
each, pidfd, and the honest note on sibling containers are all real improvements. The findings
below are what stops it merging. Three BLOCKERs break its own central claims: a single writer
for the store, delivery at most once, and an install with no teams being inert.

## Findings

01. **BLOCKER. 2.5, 2.4.5 and 2.10: the single-writer rule has three holes.**
    (a) *Keeper death.* The kernel releases the seat locks when the keeper dies, and only then
    does the worker "persist, set leases expired and exit". In that window another session can
    take the seat, and its worker loads and writes the same store while the old one is still
    writing. (b) *Remove.* The keeper waits "at most 10 s" for the worker's `remove` answer, and
    the design never says what happens after those 10 s. If the keeper releases the lock anyway,
    two writers are live. If it does not, the seat is stuck with no report. (c) *Unlinking a lock
    file.* `guest remove` ("removes the team's bundle and seat state") and any move-aside of the
    seat directory can delete `seat.lock` while it is held. A second keeper then locks a fresh
    inode, and two sessions hold "the" seat lock.
    **Fix:** let the worker inherit the seat-lock file descriptions (flock follows the open file
    description), so a lock is released only once both processes have closed it. Or have the
    worker take its own per-store `LOCK_EX` before its first read. On the remove timeout, the
    keeper kills the worker and waits on its pidfd before it releases. State as a rule that lock
    files are never unlinked. `guest remove` and every deletion of seat state take the seat lock
    first and delete only the other files. Add a crash-point test for each of the three.

02. **BLOCKER. 2.7.3 and 2.7.4: the proof state machine is incomplete. A false `missing` now
    causes a double delivery.**
    (a) `inflight` is cleared at the full write. From then on, "proof: waiting" exists only as a
    ledger flag with no transcript offset, so a worker that restarts cannot resume the scan. It
    never sends those receipts and never declares those items missing. `inflight` is also a
    single object, while several deliveries 2 s apart can await proof at once.
    (b) The 180 s "quiet" rule comes from v1 (E16), where a missing delivery stayed consumed, so
    a false `missing` cost nothing. v2 returns the items to `recv` (BD8). A session busy in a
    long tool call can write nothing to its transcript for more than 180 s while Claude Code
    holds the queued message. The items are then marked missing, the hooks tell the agent to run
    `recv`, the queued delivery lands as well, and the agent sees every item twice. That breaks
    B20 ("recv and the delivery loop never show one item twice").
    (c) Nothing says what happens when the needle appears after `missing` has been declared.
    (d) On restart, "not found → pending with repeat" contradicts the normal-path rule "missing →
    held from the socket". It also labels a delivery that was never written (a crash before the
    write) as a repeat.
    **Fix:** persist a list of awaiting deliveries, each `{nonce, ids, transcript path, offset, at_ms}`, and clear each one only on `proven` or `missing`. Start the quiet clock only after a
    Stop hook has fired since the write, so the session was idle and did not take the message.
    The 1 h cap stays. A late find removes the items still pending and logs any that `recv` has
    already shown. Restart resolution uses the same rule as the normal path, and `repeat:` is
    used only when a write may have happened. Add property tests for "busy for 10 min, then the
    message is recorded" and "found after missing".

03. **BLOCKER. B13, 2.4.1 and C3 against the frame: an install with no teams is not inert.**
    The frame says "a v2 install with no teams is inert". C3 has every launcher end in
    `<v2-bin> run`, and `run` always starts a keeper and a worker before READY. So every ccy
    session pays for two resident processes, inotify watches and a status-line exec, even with
    no team. v1 deliberately ran one keeper and nothing else on no team (H11, H33a), and gave
    `--no-bus` sessions no bus process at all (H33b). The cost section (2.14) does not mention
    this case.
    **Fix:** on no team, run nothing (the launcher does not wrap) or only the keeper, which
    starts the worker on the first add. Specify the `--no-bus` equivalent. Report the no-team
    cost in 2.14. Add the case to B40's acceptance.

04. **MAJOR. B38 and 2.4.4: an upgrade breaks every running session.** `hooks.json` and the
    status line call `<v2-bin>` by name. If that name resolves through the `current` link, which
    an install repoints, every hook and verb in every running session fails the `kit` digest
    check (exit 78) after the next install. The bus of every live session then stops at once.
    The claim that "sessions resolve current once" is not backed by any mechanism.
    **Fix:** `run` resolves the versioned kit directory once and passes it on: as an absolute
    path in `PATH`, or in `<V2>_KIT`, with the plugin's commands calling it through that path.
    Plugins must be versioned the same way, or the design must say why they need not be. Test:
    install a new kit under a running session, then hooks and verbs still work.

05. **MAJOR. 2.4.4 and 2.8: any process of the same uid can rewrite this session's facts.**
    `SO_PEERCRED` checks only the uid. A `hook` request sets the inbox socket path, the token, the
    transcript path and SessionEnd. Any process of the same uid can send one: the agent's own
    Bash tool, or another `cc` session of the same user on a host. It can redirect deliveries to
    another socket, point the proof scan at a file of its choice, or fake a SessionEnd so a
    goodbye is posted while the session lives on.
    **Fix:** accept only a socket path equal to the one derived from the session pid
    (`.../cc-socks/<pid>.sock`, H34), and only a transcript path under the session's projects
    directory whose name matches `session_id`. Check that the peer pid descends from the session
    pid. Record the SessionEnd reason only, and post the goodbye only when the session pidfd
    fires. That is also simpler. Name the hook-only ops and test each one with a forged request.

06. **MAJOR. 2.4.5 `repair` cannot work as written.** (a) Under `podman exec` or `lxc-attach`
    it has no `<V2>_RUNTIME`, and `<v2-run>` carries a random run id, so `repair` cannot find the
    runtime. (b) `session.pid` is trusted with no check against pid reuse. After a keeper crash
    and then the session's end, `<v2-run>` is left behind, and `repair` could attach a keeper to
    an unrelated process and hold its seats. (c) The keeper "refuses to start unless its parent
    is the `run` it was forked by", which a keeper started by `repair` never meets.
    **Fix:** `repair --runtime DIR`, with discovery by listing `<v2-run>`'s parent for live
    runtimes. Record the session's start time (`/proc/<pid>/stat` field 22) next to its pid and
    check it. Give the keeper an explicit adopt mode, with its own refusal rules. Define who
    removes runtimes left behind by a keeper crash.

07. **MAJOR. Section 5 drops R46 silently.** v1's SessionStart writes `conversation.json`, and
    `ccy-teams restart` and the first-join relaunch (H22, "comes back ... resumed") resume by it
    with `claude --resume <id>`. In v2 the session id lives only in `<v2-run>/session.json` on
    tmpfs, which dies with the session. So area C's relaunch on first join (B35, C2) has nothing
    to resume from.
    **Fix:** carry R46. Write a persistent, worker-owned conversation record with session id and
    source, as a report and never as authority. State it in C's assumptions.

08. **MAJOR. 2.2.1 `recv` can lose items silently.** `recv` "prints every pending item and
    marks it delivered", with up to 4096 items and full texts. Claude Code truncates long Bash
    output in the middle, so items marked delivered can go unseen by the model. The same happens
    if the CLI is killed after the worker has marked items. B20 and B21 claim loss is closed, but
    this path is open.
    **Fix:** cap `recv` like a delivery (8 entries, 12 KiB) and end with a `more` record. Mark
    items delivered only after the CLI acknowledges it has written stdout (one extra line on the
    socket). Add a test with a pending list larger than the cap.

09. **MAJOR. 2.4.1 step 1: B decides a matter that belongs to D.** "`run` refuses an environment
    that carries v1's bus variables" means one session can never be on a v1 team and a v2 team
    at once. The frame forbids mixing within one team, not within one session. Take a member of
    two teams of which only one has moved: under this rule it must leave one. This is D's call
    (how a running session moves, coexistence). And if v1's ccy sets `PINGBUS_HOME` for every
    container, every v2 launch fails.
    **Fix:** move this to "Assumptions on other areas" (D) as a proposed rule, with the
    two-teams case. Add an owner decision row with options and a default.

10. **MAJOR. 2.11, B11 and 2.5: `seat close-ended` contradicts B11 and needs R39 checks.** It
    runs on the host, reads the seat's token, and so breaks B11 "the token is read by one
    process only (the worker)". It also reads and rewrites a store in the container-writable
    checkout as a host process. The lifecycle table says the "next keeper" runs it, but B14
    says the keeper makes no network calls. Nothing is said about the uid an atomic replace
    from the host leaves on the store, which the worker then refuses with exit 78.
    **Fix:** restate B11 as "worker, and `close-ended` on the host". Fix the table (the next
    *worker* runs it). Apply R39: open every path component `O_NOFOLLOW`, decode strictly,
    render only fixed templates. Keep the original owner on replace (`fchown`), or have the
    worker accept the host uid as named by C. Carry the RP 5.03 tolerance of a dead terminal
    (EIO, EPIPE).

11. **MAJOR. 2.3.2: the store caps contradict each other.** Take 2048 ledger entries of about
    230 bytes and 4096 pending entries of about 160 bytes: together that is over 1 MiB, so the
    file cap fails the team before the list caps are reached. The whole document, up to 1 MiB,
    is also re-fsynced twice per delivery, which 2.14's cost section does not account for. A
    multi-team delivery writes `inflight` and the ledger into several stores, so the cross-store
    order must be stated (crash between store A and store B).
    **Fix:** size the caps from the schema (or shrink the ledger and keep only the ids `reply`
    needs). State the per-delivery write cost. Define the order across stores and the
    resolution on restart for each store.

12. **MAJOR. 2.3.1 and 2.7.3: replay by a sibling container is understated.** "A planted id can
    only name a real event" is true, but a sibling with the same uid can plant the id of an
    *old* real HUMAN request ("push", "delete") into `pending`. The re-fetch and A's verdict
    accept it, and it is delivered as new. The sibling cannot forge text, but it can replay an
    instruction.
    **Fix:** accept a restored id only if it is newer than the store's sync floor. Keep a
    high-water `origin_server_ts` per room, written with `next_batch`, and drop anything older
    that is not already in the ledger. Mark restored items as `restored` in the envelope. State
    the residual risk under C7.

13. **MINOR. 2.7.2 and 2.10: Plan 00211 behaviour is not fully carried.** There is no `revises:`
    line or record field for a questions-room edit (00211 D6). Nothing covers how invites are
    accepted or left pending, or stopping the questions-room sync at once on demotion (00211
    section 4.4). `human_text` is missing from C1. A role change does not say it rebuilds the
    sync filter.
    **Fix:** add these rows, citing A for the content.

14. **MINOR. Section 5 is missing smaller v1 rules.** R43 (two bundles sharing a user id or a
    token are refused). R48 (the source address inside `allow_from` as a `guest check`
    pre-check, or a note that it moved to C). R26 (`(role?)` for an unknown role, one row
    without `COLUMNS`). RP 5.16 (the send gate is charged before the network call; 2.3.2 has a
    `gate` but no rule). RP 5.14 (one TIMEOUT per silent target).
    **Fix:** carry each rule or drop it with a reason in section 5.

15. **MINOR. B36 and 2.13: tmux contradicts B36.** B36 says the guest needs only coreutils,
    util-linux and systemd, but `<v2-unit>` runs a tmux session. Also, `install-self`
    "checks its own digest", and a binary vouching for itself proves nothing.
    **Fix:** list tmux, or justify it as C's. Check the digest with `sha256sum` before running
    the new binary.

16. **MINOR. 2.9: the "not answering" mark is false when no team syncs.** The heartbeat in
    `status.json` rides on long-poll returns. With every team stopped on a permanent failure,
    nothing writes it, and after 90 s the status line falsely says `bus runtime not answering`.
    **Fix:** write the heartbeat from the state goroutine's own timer, which wakes once a
    minute.

17. **MINOR. 2.6 step 08 and step 04: no path back from a permanent failure.** A 401, a 403
    or lost trust "is retried only when [the worker] is next started". Remove plus re-add
    deletes the store and with it the owed TIMEOUTs. There is no verb or trigger to resume one
    team, so a rotated token (a new bundle from C) needs a full relaunch.
    **Fix:** retry a stopped team when its bundle changes (inotify) or when the desired
    generation changes. State it.

18. **MINOR. Fallbacks left unstated (fail fast).** Hook stdin that is not JSON is "treated as
    no payload", silently. A render failure drops an item and only `worker.log` knows; a lost
    HUMAN message should reach the agent as a NOTICE. A desired file that does not decode keeps
    the last good one with no named channel; name it in `report.json` and `status`. The gap
    policy of step 03 (the position moves on past more than 2000 events) is a skip and needs a
    decision row.
    **Fix:** count and report each one through a named channel. Add a BD row for the gap
    policy.

19. **MINOR. Internal contradictions.**
    (a) 2.3.1 says the store is "written by the worker only", but 2.4.2 has the keeper write
    `run.ended`. (b) The told generation is held in both `store.told` and `session.json`: two
    truths. (c) The `verify` verb "prints ... full text", but the `verified` record has no
    text field. (d) `runtime.LockOSThread` in `run` has no reason left once pidfd replaces
    `PR_SET_PDEATHSIG`. (e) `<v2-kit>`, `<v2-skill>` and `<v2-name>` are used but missing from
    the placeholder table. (f) The skill tells the agent "tell a human" on exit 69, but in
    `wait` mode a worker restart kills the agent's background `wait` for good. `wait` should
    reconnect within a bound, or the skill should say to rerun it.
    (g) `XDG_RUNTIME_DIR` is often unset under a system unit with `User=`, which is the LXC
    path that comes first.
    **Fix:** settle each one. For (g): fail fast with exit 78 and have C's unit set a runtime
    directory.

20. **MINOR. Form (frame).** Two designs are weighed for state ownership, output, the store
    and the death signal. They are not weighed for transcript proof against alternatives, for
    reconciling (inotify against v1's 2 s poll), for the worker socket protocol, for the hooks
    without the `sh` guard, or for the guest install. 2.15 lists fakes but gives no tests for
    `repair`, the hot-reconcile 2 s bound, peer and path checks, `guest` ops, or a kit upgrade
    under a live session.
    **Fix:** add one alternative line per part and a test row per piece.

No install-specific names were found. Placeholders are used throughout. The frame's
section order is followed.
