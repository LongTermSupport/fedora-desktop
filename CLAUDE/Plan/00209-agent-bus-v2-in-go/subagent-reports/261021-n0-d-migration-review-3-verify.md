Verdict: FIX-FIRST

# Plan 00209 N0 area D: verification of the round-3 fixes (commit 63131e3b)

Reviewed: `DESIGN-v2-D-migration.md` at `63131e3b`, against the round-3 review
(`261021-n0-d-migration-review-3.md`) and the v1 source. `D:n` is a line of the design at that
commit. All 14 findings are addressed. One fix (the stopped archive) adds a blocker in M8. Two
fixes (the receipt cut and the rollback check) add majors.

## Round 3 status

| #   | Finding                                      | Status | Where in the design                                                                                                                                                                                                                                                                                                                             |
| --- | -------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | New v1 fence verb grows a ratchet bash file  | FIXED  | D17 (D:45); 3.5 fence objects (D:197-202); 4.1 part 2 (D:345-348); 5.3 idempotency (D:638-639); M2 (d) (D:668); 5.4 options (D:705-738); 5.5 (D:778-788); M8 (5) (D:674); section 8 (D:1054); RK1 (D:1081); section 12 (D:1135-1138). The only `agent-bus-install fence` text left is the refused option (D:708). No `rooms-read-only` remains. |
| 2   | R checks state after lifting the fence       | FIXED  | 5.7 step 1 runs before the lift, with `--accept-state-diff` (D:852-858). P1 asserts the comparison before the lift and deliveries after it (D:1019). See new finding 3 for the re-run case.                                                                                                                                                     |
| 3   | Rolled-back team reads as v2-native          | FIXED  | `"origin"` field (D:262-267); R step 5 keeps the pointer (D:870-874); the decision function's unit test covers every origin and `rolled-back` (D:272-275); section 12 C (D:1139-1140).                                                                                                                                                          |
| 4   | `rooms-read-only` is undone by v1's play     | FIXED  | Option (ii) is taken: the archive is stopped at M8 (D:674 (4)), stays writable, and is opened on demand with an archive check (D:796-833). OD-D6 default (b) (D:1074). See new findings 1 and 6.                                                                                                                                                |
| 5   | Seat changes go round the fence via loopback | FIXED  | 3.9 (D:277-282); section 12 ccy (D:1161-1164, version bump); P1 seed (D:1018). See new finding 7.                                                                                                                                                                                                                                               |
| 6   | `last_send` cut under-carries                | FIXED  | Row 1 uses the `m.read` receipt, with a join fallback marked `possible_duplicate` (D:473); `ReadReceipts` (D:756); P1 probe (D:1018). See new finding 2.                                                                                                                                                                                        |
| 7   | TIMEOUT citation and missing seed            | FIXED  | D:462-468 cite `_candidates` and `_send_delivery` and the empty-inbox condition; P1 seed (D:1018). (`_deliver` is at `cli.py:1378`, not 1381; trivial.)                                                                                                                                                                                         |
| 8   | Fence proof covers one route                 | FIXED  | M2 (f) probes the bus address, a ccy container over pasta and an LXC guest over the bridge, and compares the table rule for rule (D:668). M0 refuses a loopback `base_url` (D:666).                                                                                                                                                             |
| 9   | Two message fates unstated                   | FIXED  | 5.2 rows for a send whose response the fence cut, and for unsent Element messages (D:482-483).                                                                                                                                                                                                                                                  |
| 10  | `backup-now` rotates and repeats             | FIXED  | Skipped when `fence.json` records it done (D:640-641, D:668 (b)); cost named in section 8 (D:1054).                                                                                                                                                                                                                                             |
| 11  | Declared move stays queued after R           | FIXED  | R step 5 marks the declaration `rolled-back` (D:873-874); `--declared` skips it (D:926-927).                                                                                                                                                                                                                                                    |
| 12  | No adapter read for LXC and host inject      | FIXED  | `ReadGuestInject` (D:755), cited to `config.py:123-138` and `inject.py:32` (D:476). Verified: `resolve_home` falls back to `$XDG_CONFIG_HOME/pingbus` before `~/.config/pingbus`, so "by default" is right.                                                                                                                                     |
| 13  | `teamchat0` next to libteam                  | FIXED  | Accepted with its reason in 3.5 (D:190-195), plus P2 triage assertions for packages, users and groups.                                                                                                                                                                                                                                          |
| 14  | Section 8 omits the fence cost               | FIXED  | D:1054: the table, the oneshot unit, the backup rotation, and the fallback's two restarts.                                                                                                                                                                                                                                                      |

## Checks of the fixes

- **The fence is consistent.** The same fence appears in D17, 3.5, 4.1, 5.3, 5.4 M2 (d) and
  M8 (5), 5.5, 5.7, 7.1 P1, section 8, RK1 and section 12: the nft table
  `inet teamchat_fence_<team>`, `teamchat-fence@<team>.service`, and the drop-in fallback.
  `fence-status` is used in the same way everywhere. No new v1 verb is left.
- **"An accept in another table does not undo a reject in this one": CORRECT.** In nftables,
  every base chain on a hook is run in priority order. An `accept` ends only the chain it is in,
  and the packet goes on to the next base chain. A `drop` or `reject` in any chain is final. So
  firewalld's `iif lo accept` in `inet firewalld` cannot open the fence. The same holds for
  Docker's iptables-nft tables. The wording of the rule itself is not correct: see new finding 4.
- **v1 needs only loopback while fenced: VERIFIED.** v1's readiness probe uses
  `http://127.0.0.1:<port>` (`agent-bus-install:654-674`). The admin tool accepts only a loopback
  `base_url` (`admin.py:247-252`). So M6's v1 play (`converge_units`, `bootstrap`) still works
  under the fence. It writes only `network.conf` in the drop-in directory and never removes
  other files (`agent-bus-install:816-819`), so the fallback `zz-` drop-in survives it.
- **The rollback is coherent and finishes on a first run.** Step 1 runs while the fence holds,
  and `--accept-state-diff` lets R continue past a difference. A re-run after a crash inside R
  gives a false alarm (new finding 3).
- **D9 and D10 hold, apart from the M8 ordering.** OD-D6 (D:1074) agrees with 5.6 (D:820-833),
  section 8 (D:1055), RK5, 7.2 step 1 and the 5.2 archive row (D:484). The archive stays
  writable, and the archive check carries human posts, so D9 holds. D10 holds only once the
  humans can log in to the archive. As M8 is written they cannot (new finding 1), and A9 asks
  for the v1 view too early (new finding 6).
- **The receipt cut (checked against `cli.py:1905-1922`, `2027-2071`) is sound for under-carry,
  but not for flagging.**
  - Within a delivery, candidates are sorted by `origin_server_ts` (`:1918, 1922`), and
    `envelope.fit` keeps a prefix (`:1961-1963`). So an item older than a receipted human
    message was delivered, held back, or dropped.
  - The exceptions are v1's own existing semantics, not new losses: items held back by the
    sender budget are consumed without delivery (`:1978-1980`), and a delivery whose nonce is
    missing from the transcript gets no receipt, but its items are already consumed
    (`:2072-2075`).
  - Receipts are posted only for `HUMAN` entries (`:2037-2039`), and only on the watcher's
    injection path; `recv` (`_deliver`) posts none. That makes the cut too early, never too
    late, so it never under-carries. The flag is the gap (new finding 2).
- **`"origin"` and the seat-change refusal are coherent.** The refusal range `preflighted` to
  `carried` is wider than the review asked for, which is safe. It does collide with 5.9's leave
  alternative (new finding 7).

## New findings

### 1. Blocker: M8 unlocks the humans and sets their passwords after stopping the v1 server, so M8 cannot finish

- **Location:** D:674, M8 acts (4) and (6); D:1074 (OD-D6 default).
- **Problem.**
  - Act (4) runs `systemctl disable --now` on `agent-bus-hs@<team>`. Act (6) then runs
    `agent-bus human unlock` and `agent-bus human password`.
  - Both verbs are server calls: `_human` opens an admin session and reads the account, then
    `set_user_flag` or `reset_password` is called over loopback (`helpers/agent_bus/admin.py:873-901`).
    With the server stopped they fail, and M8 stops ("a v1 verb fails").
  - A re-run cannot get past it either. Act (1), the event check, also needs the server, and it
    is now stopped. Nothing says M8 resumes after its last completed act.
  - So every migration ends with M8 failed and the humans locked out of the archive. That also
    breaks D10.
- **Fix.**
  - Move act (6) before act (4): unlock and issue passwords while the server still runs. The
    fence still holds at that point, and agents are already parked by act (2).
  - Or move the unlock and the password to `ccy-teams archive open`. That is better for humans,
    because a password shown once at M8 may be needed months later. Then M8 only records that
    the humans are owed an unlock.
  - Record each M8 act in `log.jsonl` (as act (4) already is), and resume after the last done
    act. Act (1) is skipped once act (3) has recorded the marker.
  - In P1, kill the run between each pair of M8 acts.

### 2. Major: items carried after a receipt cut are not flagged `possible_duplicate`, so delivered agent pings come back as new

- **Location:** D:473 (row 1, Fate column); RK2 (D:1082).
- **Problem.**
  - v1 posts a receipt only for a delivered `HUMAN` entry (`cli.py:2037-2039, 2046-2062`).
    Pings, replies and questions from agents never move it.
  - So every agent item delivered after the member's last receipted human message lies after
    the cut, and is carried again. Row 1 flags only items from a join cut.
  - The member gets a plain carried `review` or `do` ping it has already acted on, with nothing
    to warn it. That breaks RK2's own rule: "over-carrying is preferred, and the item is flagged
    `possible_duplicate`".
  - Also, there is no client-server endpoint that lists other users' receipts. The admin reads
    them as `m.receipt` ephemeral events in its own `/sync` of each room, so it must be joined to
    that room. P1's probe covers this, but `ReadReceipts` (D:756) should name the route.
- **Fix.**
  - Mark every item carried from a `read_receipt` cut `possible_duplicate`. At least mark every
    non-human item and every item after the receipt. Give the per-member count in the M3 report,
    as for join cuts.
  - Reword "every item before it was delivered" to "every item before it was delivered,
    consumed as held back, or lost to a missing delivery, as v1 already treats them".
  - Name the `/sync` route in `ReadReceipts`.

### 3. Major: a re-run of R after its own unfence fails its state check and asks for `--accept-state-diff`

- **Location:** D:852-858 (step 1), D:877-878 ("A second run of R finds every step done").
- **Problem.**
  - Step 1 compares member state with the snapshot whenever a snapshot exists. Suppose R
    crashes, or is interrupted, after step 2 has lifted the fence. The watchers have then synced
    and delivered.
  - A re-run's step 1 sees `sync.json`, `inbox/` and `consumed/` changed. It prints the carry
    plan as items "to tell members by hand", and refuses to continue without the flag.
  - That is a false alarm, and it teaches the owner to pass `--accept-state-diff` routinely.
    That defeats the check. It also contradicts "a second run changes nothing", which P1
    asserts (D:1019).
- **Fix.**
  - Record step 1's result in `fence.json` (`state_check: passed | accepted`, with its time)
    before step 2.
  - A re-run skips step 1 when that record exists and the fence status shows the fence lifted.
  - A lifted fence with no record means the fence vanished without R. That is a real anomaly:
    stop and name it.
  - Add the case to P1's fault injection: kill R between steps 2 and 5.

### 4. Minor: the rule as described does not reset established connections, and misses IPv6

- **Location:** D:722-728 (option 4); M2 (d) (D:668); P1 rollback (D:1019).
- **Problem.**
  - A bare `reject` in an `inet` table sends ICMP (`icmpx port-unreachable`), not a TCP reset.
    A connect fails at once either way. But Linux treats ICMP port-unreachable on an
    established TCP socket as a soft error, so "takes effect at once, on established long-polls
    too" holds only with `reject with tcp reset`.
  - The rule matches only packets towards the v1 port, so a server response already on its way
    still reaches the client. M2's 60 s wait is what covers this, and the text should say so.
  - In an `inet` table, `ip saddr != 127.0.0.0/8` implies IPv4. The `::1` case needs its own rule
    (`meta nfproto ipv6 ip6 saddr != ::1`), or IPv6 traffic is not fenced.
- **Fix.** Give `fence.nft`'s rules literally in 5.4: `meta l4proto tcp` with
  `reject with tcp reset`, and one rule per family. State that the 60 s wait covers in-flight
  responses. Keep P1's assertion on established connections, which will catch a bare `reject`.

### 5. Minor: a ruleset flush is missing from the fence's durability risks

- **Location:** D:735-738; RK1 (D:1081); 3.5 (D:201).
- **Problem.**
  - On Fedora, `nftables.service` start, reload and stop, and any tool that runs
    `nft flush ruleset`, delete every table, this one included.
  - The generic rule (D:643-644) stops the next step. But a flush during M3 or M4 lets watchers
    deliver items that the carry plan also carries.
  - The unit's ordering against `nftables.service` and `firewalld.service` at boot is also
    unstated.
- **Fix.**
  - Add "`nft flush ruleset` and an `nftables.service` restart" to the P1 list and to RK1.
  - Order the unit `After=nftables.service firewalld.service`.
  - Have M3 and M4 check `fence-status` at their start and end. A lift between the snapshot and
    the check then stops the run before the carry.

### 6. Minor: the archive check has no stated behaviour on a stopped archive, no event filter and no report sink; A9 checks the v1 view while it is fenced

- **Location:** D:828-833 (archive check); A9 (D:955); 7.2 step 1 (D:1033).
- **Problem.**
  - Under OD-D6 (b) the archive is usually stopped. The check reads over loopback and so needs
    the server running. `migrate --status` would fail, or would have to start the archive.
  - "Every v1 event after the marker" includes state events: Element display name and avatar
    changes, and anything `bootstrap` writes when a v1 play restarts the archive. Those are not
    posts.
  - Reported items have no place to go after M7, because the migration report is then closed.
  - A9 runs at M7, while the v1 server is fenced and the humans are locked (and possibly
    logged out). So "opens the v1 window and sees the old history" cannot pass then.
- **Fix.**
  - If the archive is inactive and has not been active since the marker's time (systemd
    `ActiveEnterTimestamp`), skip the check with nothing to do. Otherwise check it, and leave the
    unit in the state it was found in.
  - Consider only `m.room.message` events from humans.
  - Write each check's outcome to an `archive-check` report, and show its count on the board.
  - Move A9's v1 half to after M8, through `archive open`.

### 7. Minor: 5.9's leave alternative is a hot remove, which 3.9 now refuses

- **Location:** D:908-912 against D:277-282.
- **Problem.** The owner "chooses it at M0" to have shared sessions leave with
  `ccy-teams session remove`. That is a hot remove. If M0 has already written the set record at
  `preflighted`, the new refusal blocks it.
- **Fix.** State that the leave runs before the set record exists. M0 stops with the leave as its
  named precondition, the owner does it, and M0 is re-run.
