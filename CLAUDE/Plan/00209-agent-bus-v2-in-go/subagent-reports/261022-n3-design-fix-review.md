# N3 design-text fixes and the 2.0e decision: adversarial review

Diff reviewed: `46fd12d8..HEAD` (e4e42a5c, ab127186) on `feat/bus-v2-n3`.

Verdict: FIX-FIRST

Counts: major 0, minor 11, nit 5.

All 15 listed fixes are present and each says what its n3-D decision says. The n3-D20
departure note is in place. Nothing was applied that should have been held for Gate A or
Gate B: the unit path in C 3.9.4 is n3-D3's and changes no owner-visible name, and no seat
password reaches a process. No install-specific names were added. The findings below are
stale text next to the fixes, an RS2 sentence that disagrees with B 2.11, and the 2.0e
pointers. Several of these leave design and spec disagreeing, so the verdict is FIX-FIRST.

## The 2.0e change

1. **minor.** `SPEC-n3.md:667`. "Any other status, or a 75 from an LXC or host member, posts
   `left exit`" is wrong when no SessionEnd reason is recorded. Per B 2.11's table, an `exit`
   end with no reason (a crash) posts `disconnected`. The old RS2 sentence covered only 75
   cases, so this generalisation is new. **Fix:** "Any other status, or a 75 from an LXC or
   host member, is an ordinary `exit` end and is decided by B 2.11's table (`left exit` after
   an `/exit`)."
2. **minor.** `SPEC-n3.md:664-669`, `DESIGN-v2-B-member.md:1009` and `:1856`. The text never
   says where the "off" switch lives or how it is tested. B 2.4.2 says the keeper always
   reports `restart`, and B 2.11 says the row "is turned on only with the host half". So a
   reader cannot tell whether the keeper or the link holds the switch, or what a `restart`
   end does while the switch is off. RS1's "a test pins it" and RS2's table test also need
   both positions to be testable. **Fix:** in RS2 add: "The switch is one constant in the
   link. While it is off, a `restart` end is decided as `exit` (so `left exit` after the
   supervisor's `/exit`). The table test runs both positions through a test seam. RS1's test
   pins the shipped value off, and RS3's commit turns it on." Add the same sentence, in short,
   to B 2.11's `restart` row.
3. **minor.** `00194-agent-bus-live-delivery-supervisor-plugin/PLAN.md:106-107`. "Folded into
   Plan 00209 Task 3.1 (the Go SessionEnd hook), built there" is wrong: 00209 Task 3.1 is the
   N3 spec (done). The code is built in Task 3.2 (n3c) and turned on with Task 4.2, and the
   line does not mention the Task 4.2 dependency. **Fix (minimal):** "Specified in Plan 00209
   Task 3.1 (SPEC-n3 RS1-RS3), built in Task 3.2 and turned on with Task 4.2; the switch is
   CMD's exit 75 alone, so hooks-daemon #127 no longer gates it."
4. **minor.** `00209 PLAN.md:386-387` (Task 4.2). RS3 is the host half that the switch waits
   on, but Task 4.2's text does not list it. Someone building Task 4.2 from PLAN.md would not
   do RS3, and nothing would turn the switch on. **Fix:** append to Task 4.2: "the launcher
   skips its `seat disconnected` for the run it relaunches and records it in
   `held/notice.json` (SPEC-n3 RS3), and the same change turns RS2's switch on."
5. **minor.** `DESIGN-v2-B-member.md:1937` (B 2.11 kinds table, the ccy `/exit` row). It still
   says "...or 75 for a relaunch request it acted on); `end exit`" and posts "`left <reason>`
   by the link". That contradicts B 2.4.2 (`restart` for a 75) and the new decision row.
   **Fix:** "What reaches the keeper": "...; `end exit`, or `end restart` for a 75 (2.4.2)".
   "Posted": "`left <reason>` by the link, nothing for `restart` once its switch is on (the
   decision table); then ...".
6. **minor.** `DESIGN-v2-B-member.md:2212` (B 2.15 "every member kind's ends"). The row now says
   a relaunch posts nothing, but it still ends "Each asserts ... that exactly one is posted".
   **Fix:** "that exactly one is posted (none for a `restart` with its switch on)".

## The 15 fixes: stale text next to them

07. **minor.** `DESIGN-v2-C-host.md:1187-1191` (C 3.7.x reissue, the LXC bullet). It still says
    place-bundle writes into `<team>.new-<claim>/` "and swapped the same way, the old bundle
    removed (3.9)". This is fix 1's old wording, which n3-D1 rejects because it deletes the
    store. **Fix:** "...reading the same JSON on stdin, placed by 3.9.2 step 07's rule (an
    existing seat directory keeps `store.json` and `seat.lock`; `token` then `seat.json` are
    replaced inside it)."
08. **minor.** `DESIGN-v2-C-host.md:1721-1723` (3.9.2 step 05). The step does not say the host
    sends `{"v":1,"team","base_url","plain_http_hosts"}` on stdin, which B 2.13's `check` row
    (fix 10) and GU2 require. **Fix:** after "as the agent user" add "with
    `{"v":1,"team","base_url","plain_http_hosts"}` from the team's declaration on stdin (B 2.13,
    for `reach`)".
09. **minor.** `DESIGN-v2-C-host.md:1736-1744` (3.9.2 step 08). Step 07 now produces
    `DEFERRED` for an identity change on a held seat, but step 08 does not apply LX7's
    consequence. As written, step 08 would still list the deferred seat in `desired.json` and
    the host copy. **Fix:** add "A seat whose `place-bundle` was `DEFERRED` in step 07 is left
    out of `seats` in both, so the running keeper releases the old identity's seat and the next
    apply places the new one."
10. **minor.** `DESIGN-v2-C-host.md:109` (C32) and `:1667` (step 02). Both still say a missing
    container "fails the apply", while 3.9.1 (line 1599) and the absent phase now treat it as a
    stopped container (n3-D12). **Fix:** C32: "only a missing container in the present phase,
    or a failure in a running one, fails the apply (the absent phase parks a missing
    container's member, 3.9.2)". Step 02: "Missing fails the present phase."
11. **minor.** `DESIGN-v2-B-member.md:1741-1746` (B 2.10 Reconcile). B 2.3.3 now names `code`
    `missing` "for a desired file deleted under a live keeper, 2.10", but 2.10 does not say that
    a deletion is a refusal. **Fix:** after "a file it cannot decode changes nothing ..." add "A
    file deleted under a live keeper (`IN_DELETE`, or `ENOENT` on the re-read) is the same
    refusal with `code` `missing`; a file written again clears it (n3-D19)."
12. **minor.** `DESIGN-v2-B-member.md:2205` (B 2.15 "guest operations"). The row tests only
    `remove`'s 75. It has none of GU3's place-bundle cases, although B 2.13 now gives 75 to
    `place-bundle`. **Fix:** append "`place-bundle` into an existing seat directory: same
    identity keeps `store.json` and the `seat.lock` inode; a different `user_id` or seat sets
    the store aside; a different identity under a held lock exits 75 and changes nothing".

## Nits

13. **nit.** `DESIGN-v2-B-member.md:1856`. The `restart` row is last but must match first
    ("ahead of the first row"). Move it to the top of the table so the table reads in match
    order, and drop "ahead of the first row".
14. **nit.** `DESIGN-v2-A-protocol.md:1032-1033`. The bound of B's cache (1024, oldest evicted)
    is stated in A, and B 2.7.3 points back to A for it, but the cache belongs to B (n3-D20:
    "unspecified in B"). State the numbers in B (2.6, where SY4a resolves before judging) and
    have A say "which B caches (bounded, B 2.6)". Also, "lost at a link restart, when a miss
    costs one more fetch" reads oddly. Say "lost at a link restart; a miss costs one more
    fetch".
15. **nit.** `DESIGN-v2-C-host.md:1734-1737` (step 07). GU3 treats an undecodable `seat.json` as
    a different identity, and the design does not say so. Add "(an undecodable `seat.json`
    counts as a different identity)".
16. **nit.** `DESIGN-v2-B-member.md:1208-1209`. The seat-lock probe list cites "C 3.9.2 and
    3.11.2" but not C 3.7.3 release step 1, which takes the same `LOCK_SH`. Add 3.7.3. Also,
    `DESIGN-v2-B-member.md:681`, `:759`, 00194 `PLAN.md:107` and 00209 `PLAN.md:365` add
    unwrapped long prose lines. Rewrap them to the files' width.
