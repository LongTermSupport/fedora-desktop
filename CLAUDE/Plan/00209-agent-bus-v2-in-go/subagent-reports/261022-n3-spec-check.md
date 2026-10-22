# SPEC-n3 narrow check: round 3's MAJOR 1 fix (gap 14)

Scope: commit f6e4ad62's fix to round 3's MAJOR 1 (`261022-n3-spec-review-3.md`), checked against
`proto/verdict.go:98-118` (`Related`, `Lookup`) and `proto/send.go:51, 323` (the sender-side
`Lookup`). This is not a fourth review.

**Verdict: MERGE**, after four small text fixes that this check applied to `SPEC-n3.md` (findings
5-8). The mechanism the reviewer proposed is present and coherent.

## The reviewer's bullets

1. **Resolve before judging: present** (SY4a, "Resolved before judging"). Before `Judge` runs, the
   relation ids of the main-room human events are collected. Each id is resolved through the
   ledger, the outbox, the own sends and the cache, and then by one `GetEvent` under the team's
   request deadline. The limit to main-room human events is kept, so agent pings
   (`probeCheckOf`) never fetch.

2. **Transient failure: present.** The whole answer is held before `Judge`, SY6 and SY5 run, and
   is re-fetched through SY8's path. n3-D20 still says "holds the batch". That is the same thing,
   so no change was made.

3. **404 and 403: present.** Each is cached as an unknown sender for the link's life.

4. **`Judge` over a memory-only `Lookup` with no error: present.** `proto` stays network-free and
   `Lookup` keeps its signature (gap 14).

5. **`Related` gains the roster-agent field, with a fill rule: was not met, now fixed.** Gap 14
   said the field is filled "by the rule SY4a states", but SY4a stated no rule. SY4a also called
   the field "addressed-to-a-roster-agent" while gap 14 called it "the addressees". SY4a now
   names the field `AddressedToRosterAgent` and states its fill rule:

   - a ledger entry is an item delivered to this member: both flags are true, and `Sender` is
     its `from`;
   - an outbox entry or an own send: `Sender` is this member, with its verb, and
     `AddressedToSelf` false. `AddressedToRosterAgent` is true when its `to` or mentions name a
     roster agent;
   - a fetched event fills both flags from its own mentions, `@room` and relations;
   - `Verb` is empty where the record holds none.

   Gap 14 now uses the same field name.

6. **Sender-side `Lookup` stays memory-only: present** (SY4a). This matches `send.go:323`, where
   `probeCheckOf` is called through the composer's `Lookup`.

7. **Test: present** (gap 14). A fetch error injected for one relation id leaves the answer
   unpersisted, and `Judge` is never called for that batch. SY4a's own test list also has "the
   fetch failing transiently persists nothing".

## Contradictions elsewhere (grep for Lookup, Related, GetEvent, AddressedTo)

08. **Gap 11 still put the fetch inside `Lookup`: fixed.** Its last sentence listed the production
    form of `proto.Lookup` as "ledger, own sends, cache and fetch". That is the shape round 3
    rejected. It now says the `Lookup` is memory only, and that the fetch is done before `Judge`
    runs (gap 14).
09. **Section 9's lead line did not place gap 14: fixed.** n3c's gaps were "5-7, 11 and 12". They
    are now "5-7, 11, 12 and 14".
10. **UH1 is consistent** ("fed by SY4a's Lookup"). It and its production-Lookup test need no
    change. No other `GetEvent` or `Related` use conflicts.

## Round 3 minors a-f

11. Before this check, minors a-f were only in the review-3 report and the fix report
    (`261022-n3-spec.md` line 112). PLAN.md and SPEC-n3.md did not mention them, so the n3c
    builder could miss them. Gap 14 now ends with a sentence that names all six and points to
    the review-3 report.
