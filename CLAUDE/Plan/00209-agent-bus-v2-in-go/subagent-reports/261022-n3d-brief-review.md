# n3d brief review (Plan 00209 N3, slice n3d)

Reviewed: `subagent-reports/261022-n3d-handoff.md` at 30025ee9f, against SPEC-n3.md section 5 and
the n3d halves of SY4, SY4a, LN5, SY7 case 4 and PT1; B 2.2.2, 2.2.4, 2.3.2, 2.4.4-2.4.5, 2.6
steps 05-08 and 11; A 7.2, 7.6, 7.7, 8.1-8.5, 9.2-9.3, 11.1-11.3; SPEC-matrix M22, MB10; the n3c
part reports; and, where the brief defers to `go doc`, the built code (`proto`, `matrix`,
`member/state`, `member/engine`, `member/records`, `member/exits`, `member/link`, `member/cli`).

**Verdict: FIX-FIRST.** No blockers, 7 majors, 15 minors, 5 nits. The brief covers SD1-SD5 and
the carried halves, and its wave order is right. The majors are three places where the owner's
offline rule is not fully carried (M1-M3), two correctness gaps that the built code shows (M4,
M6), one conflict with A's send order and proto's built gate (M5), and D6's size (M7).

## Majors

### M1. A link started while its homeserver is down refuses every send (section 1 last bullet, D6 step 03, n3d-D3)

The owner's rule says: "A send to a team whose server is down is held in the outbox and reported,
never lost". n3d-D3 refuses with 20 when there is no roster in memory, and describes this as "one
narrow case (a link started while its homeserver was down)". But that case is the commonest way a
member is offline: a laptop that starts its session with no network, or a keeper that restarts
the link during an outage. In both, every `say`, `ping` and `reply` exits 20 for the whole outage.
"Nothing is lost because nothing was accepted" is true, but the agent cannot queue anything, and
the rule asks for the send to be held.

Fix (replace n3d-D3, and add to D1 and D6):

> n3d-D3. The link keeps the last verified roster of each team in a file next to its store,
> `<v2-state>/<team>/roster.json`, holding the roster state event's content and the `rev` it was
> verified at. The team goroutine writes it, through `StoreWriter`'s no-follow replace, only when
> a verification finds a `rev` it does not hold. It is read once at `Add`, so `Notices().Roster()`
> (or an engine-held copy) answers before the first verification. A send composed from it is
> charged and queued as any send to a down team. Its addressing is checked again by every
> receiver, and the first verification replaces it. With no file (a seat never verified), the
> verb exits 20 `unreachable` as now. The file is removed with the store (removal, set-aside).
> Its size is bounded by A's roster caps.

If the lead prefers not to add the file, take D3 to the owner as an explicit exception to the
rule. Do not let it stand as a silent one.

### M2. A permanent failure at the PUT deletes queued sends (D6 "the sender" bullet 4, n3d-D2)

The brief says: "permanent (401, 403, a refused trust): the entry is removed with one log line ...
a queued one is said in that log line and in `status`". There are two problems:

- A 401 is not always final. SY8 and LN9 retry a stopped team "on a bundle change". C 3.7.3 and
  3.12.2 replace a restore's revoked pool-seat tokens by a hot reissue. A restore during an outage
  therefore deletes every send the agent was told was `queued` (exit 0), which is exactly what the
  rule forbids ("never lost").
- Once the entry is removed, `status` has nothing to print. `queued` records come from `sending`
  entries (n3d-D21), so the claim "said ... in `status`" cannot hold.

Fix (replace the bullet and the n3d-D2 sentence):

> permanent (401, 403, lost trust): the sender stops for that team. If an op is waiting on the
> entry, that entry is removed and the op answers 21 or 22, because the agent sees the failure
> and B 2.4.4 forbids posting it behind its back. Entries already answered `queued` stay in
> `sending`: `status` shows them with the team's stop, and the sender posts them, by their txns,
> when the team runs again (a bundle change, a desired-generation change, the next start, as
> SY8). They are dropped only by a removal or a set-aside (n3d-D23). The sender always uses the
> team's current `team.Team` (a retried team is a new `NewTeam`), never one captured at `Add`.

Add the test SD3-8b: queued, then 401, then a bundle change re-issues the token, then the entry is
posted once and `status` shows it until then.

### M3. Receive-side forge checks run one after another on the team goroutine, 30 s each (D4 "The receive forge check")

The brief says: "under a budget of 30 s per ping ... Every check runs on the team goroutine: a slow
forge holds this team's answer only". But "this team's answer" is the team's whole sync. While
GitHub is unreachable, a batch with n referenced pings holds that team's long-poll for n × 30 s.
The receive limit admits 60 events per sender per minute, so one sender (faulty or compromised)
can stop a team's human delivery for many minutes. Section 1 says the opposite: "a forge that does
not answer fails only the one check, under a bounded deadline". The owner's rule counts the forge
as a server too.

Fix (replace the sentence in D4, and add a test):

> The checks of one batch run concurrently, at most 8 at once, under one budget of 30 s for the
> whole batch on the clock seam. A check still running at the end is `unresolved`. After a check
> finds the forge unreachable (no answer, or a timeout), later checks within 60 s on the link are
> `unresolved` at once without a request. This is not a cached result about a reference: it is
> the forge's health, held in `forge.Checker`. So a forge outage costs a team's sync at most 30 s
> per batch.

Add SD5-r6: a batch of 10 referenced pings against a forge that never answers is persisted within
30 s of fake time, and the next batch makes no forge request within 60 s.

### M4. Pruning the outbox breaks a TIMEOUT that is still owed (D3 "Pruning", n3d-D6)

`engine/render.go`'s `timeoutRecord` builds the TIMEOUT record's `verb` and `ref` from
`s.Outbox.Pings`, by the key's event id. The brief prunes a ping "whose every addressee is in
`answered` or `reported`, and whose `due_ms` plus its own timeout has passed". A TIMEOUT item
queued at `due + 30 s` that has not been delivered by `due + timeout` (a busy session, a missed
delivery, `held`, or an agent that reads with `recv` an hour later) then renders with an empty
`verb` and `ref`.

Fix (append to "Pruning"):

> A ping is kept while any item keyed on it (`proto.TimeoutKey(ping id, …)`) is still in
> `pending` or `awaiting`, and is pruned at the first outbox write after the item leaves.

Add SD4-9b: a TIMEOUT left pending past `due + timeout` still renders its `verb` and `ref`.

### M5. The duplicate window runs before the forge check, against A 9.3's order and proto's built gate (D6 steps 07-09, section 3 "The send order")

A 9.3: "the secret check (`secret`); an open duplicate question (`duplicate`); then, in B, the
forge check, then the flood budget (`Admit`, 11.1-11.2)". The duplicate window is part of 11.2's
flood budget. The brief's section 3 reads the `duplicate` in that list as the window (it is the
open-question duplicate, which is n3f's), and puts the window at step 07, before the forge at
step 08. A send that is both a duplicate and fails provenance then exits 15 where A gives 13, and
SD3-5 ("refused by the first in A's order") would test the wrong order.

The brief also names `proto.Bucket.Admit`, which does not exist: `Bucket` has `Take`. Proto has
already built the whole gate: `SendGate{Bucket, Recent DupWindow}`,
`SendGate.Admit(l, key, dedupe, nowMs)` ("records one send, or returns its refusal (duplicate,
rate) having recorded nothing"), `Composer.Admit(out)` (the report window, then the gate), and
`DuplicateKey(verbOrKind, ref, re, to, text)`.

Fix (replace steps 07 and 09, and the section 3 bullet):

> 7. (none: the duplicate window is in the charge.)
> 8. **The charge**, one store write on the goroutine (SD3): build a `proto.SendGate` from the
>    store's `gate` (`credit`/`at_ms` to `Bucket`; `recent` to `DupWindow.Sent`, dropping keys
>    older than the window), and run `Composer.Admit` with that gate and the current
>    `Limits` (15 `duplicate`, 14 `rate`, nothing recorded on a refusal). Write the gate back to
>    the store and append the `sending` entry in the same write. Because it is on the goroutine,
>    two identical sends racing are ordered, and one gets 15.

In section 3, replace "`secret`; `duplicate`; then, in B, the forge check and `Admit`" with
"`secret`; the open-question `duplicate` (n3f's); then, in B, the forge check, then `Admit` (the
duplicate window, then the bucket)". In 2c, replace "the duplicate key of A 11.2 if proto has one"
with "`DuplicateKey`, `SendGate`, `DupWindow`, `Composer.Admit`".

### M6. `reply --verb ack` to the probe's `check` is refused when decided from the ledger alone (D6 resolving, n3d-D13)

`proto` composes by the receiver's rules. `probeCheckOf` (`proto/verdict.go:476`) makes the probe
addressable only when `Lookup.Event(root)` returns `Verb == "check"` and a probe sender. The
ledger entry holds no verb (B 2.3.2: room, item, thread, from), and `engine/seam.go` builds ledger
`Related` with `Verb` empty. So a worker's answer to the acceptance probe's `check` (A 8.1,
D 6.1 A4 and A7) is refused `target`, and the probe's round trip fails. The brief also never says
which `proto.Lookup` the send-side `Composer` is given.

Fix (D1 and D6, and replace n3d-D13's second sentence):

> D1: `state.Pending` and `state.Ledger` gain `verb` (`null`, or the ping's verb for an item
> `ping`), written by `Persist` from the verdict and carried into the ledger at `Ack` and proof.
> The size fits the 320-byte entry. D6: the send `Composer.Lookup` is a memory-only lookup over
> the store (ledger with its `verb`, outbox), filled on the goroutine as `engine.view` is. A
> ledger `ping` item takes `--verb` only when its `verb` expects an answer (A 8.1); any other
> item is 10 `re`. This closes D13's "an ack to an unthreaded done". A ledger entry written
> before the field existed (`verb` absent) is fetched once (`Refetch`).

Add SD1-5: `reply <check id> --verb ack` from a worker to the probe is sent, from the ledger alone.

### M7. D6 is too large for one builder (section 0, section 9)

D6 is three new files and hunks in `Serve`, `Add`, `Remove` and `status` of the package that P4
already filled. It covers eleven resolving rules (carried items and unknown ids among them), the
charge, the sender goroutine with its health waits, the restart search, and 21 named tests. The
brief itself expects a handoff. Earlier builders ran out of context on less.

Fix (section 0 and section 9):

> D6a (wave 3, after D3): `send.go` and `sender.go` with `say` and `ping`: resolving by `--to` and
> `--to-coordinator`, steps 01-10, the sender, the queue, the `status` `queued` record, and the
> restart re-send (SD2-*, SD3-*, SD4-n3d, SY4a-n3d, n3d-D3).
> D6b (wave 4, after D6a, beside D5): `resolve.go` with `reply`: the ledger cases, carried items,
> unknown ids, `--verb` and `--to-coordinator` (SD1-\*), on D6a's charge and sender. W moves to
> wave 5.

## Minors

01. **New exit codes have no owner** (n3d-D11, D1). `member/exits` has `forge` only under 13 and
    no `forge-rate`. `exits.New` takes its exit from the code's set, so 14 `forge-rate` and
    20 `forge` cannot be built until the set changes. Fix: add to D1's package list "`member/exits`:
    14 gains `forge-rate`, 20 gains `forge`, with the table's rows and `help exit-codes`".

02. **`Item.Trust` is redundant, and `Refetch` cannot carry it** (D1 `Item.Trust`, D4 Refetch,
    n3d-D12). `Refetch` returns `(proto.Verdict, error)`, so a restored ping cannot "carry
    `Trust` again". It does not need to: a proven check's trust is the class's trust, which
    `proto.ForgeCheck.Trust()` already gives, and `render.go` already uses it. Fix: drop
    `Item.Trust` and n3d-D12's memory table. "A failed check, live or in `Refetch`, turns the
    verdict into `Outcome: Drop` with `Reason` `provenance`, `unresolved` or `forge-rate`
    (proto's reasons), so the engine's re-judge drops it as any drop".

03. **The outbox can fill between the charge and the post** (D6 step 09, sender). The cap is
    checked at the charge, but the ping enters the outbox only after its PUT, so other pings can
    fill it in between. The ping is then posted with no outbox entry: no TIMEOUT and no answers.
    Fix: "at the charge, `len(pings)` plus the `sending` entries with `ping` set must be under 256".

04. **A restart can record a ping after its answers have arrived** (D6 restart search, n3d-D9).
    After `link-after-send-before-record`, the ping enters the outbox only when the search finds
    it. The first sync can deliver its answer before that, so proto sees no `OwnPing`, the answer
    is lost, and a false TIMEOUT follows. Fix: "the search for `tried` entries runs before the
    team's first long-poll (teamsync calls the engine's resolver after its start verification and
    before SY2), and a found ping's `sent_ms` is the found event's `origin_server_ts`".

05. **The TIMEOUT timer can sleep through a suspend** (D3, n3d-D7). `AfterFunc` falls due on
    monotonic time, which stops during a suspend, while `due_ms` is wall time. A TIMEOUT due
    during an 8-hour suspend would fire 8 hours late. Fix: "re-armed also on every `Persist` and
    on SY12's freeze or suspend detection".

06. **`gate.recent`'s 64-key cap is below the send rate the limits allow** (D6 step 09). With
    `send_per_minute` 60 and `send_burst` 30, there can be 90 sends in one 60 s window, so a
    legitimate send meets `ErrFull` and exits 78 `store`. Neither the spec nor the brief decides
    this. Fix (new decision): "the key cap is the larger of 64 and `send_burst + send_per_minute`, bounded by 90; the file cap is recomputed with it". Or take B 2.3.2's 64 to
    the owner.

07. **Two sources for "on time"** (D1 `team.Answer`, D4). `proto.Answer.OnTime` is already judged
    against `OwnPing.OriginTs` plus the current roster's timeout. The engine's `due_ms` uses the
    timeout at send time. Fix: "the engine judges by `due_ms` alone; teamsync passes `OriginMS` and
    does not use `Answer.OnTime`". Also note in D4 that proto makes no `Answer` for a sender the
    ping did not address, so SD4-4 tests the engine's own guard only.

08. **GitHub rate limits also come as 403** (D2 HTTP). A primary or secondary limit can come as
    403 with `x-ratelimit-remaining: 0` or a `Retry-After` header. Read as `unresolved`, it gives
    exit 13 with the advice "push first". Fix: "a 403 or 429 with `Retry-After`, or with
    `x-ratelimit-remaining: 0`, is the rate path of A 11.3; any other 403 is `unresolved`. Test it
    in SD5-7".

09. **An answer whose forge check fails: does it count?** (D3, D4). Neither the spec nor the brief
    says. Fix (new decision): "an answer (`nack`, `done`, `blocked`) whose reference fails its
    check is dropped and does not count, so its ping gets its TIMEOUT. A `Record` answer (`ack`,
    or a stale answer, A 8.5) is not forge-checked, because it is not delivered, and counts".

10. **The end of a send's deadline must not go through `teamDone`** (D6 step 10, the fetch
    cases). `engine/serve.go`'s `teamDone` maps a context end to 4 `timed-out`. The brief wants
    `sent queued` at the deadline, and 20 for a `Refetch` or forge check with no answer. Fix: "the
    send ops map their own ends: deadline with the entry charged, `queued`; `Refetch` with no
    answer, 20 `unreachable`; the forge with no answer, 20 `forge`. Never `teamDone`".

11. **`reply` to a TIMEOUT item** (D6 resolving). A TIMEOUT's ledger id is `<event>~<localpart>`,
    which is not an event, and the brief is silent. Fix: "an id holding `~` is 64 `usage` ('a
    TIMEOUT is not a message; ping the member again')".

12. **The restart search compares bytes** (D6 restart, n3d-D4). The homeserver re-serialises
    content, so key order and escapes may differ. A compact-bytes match then misses the posted
    event, and the search, which is the fallback for a server that forgot its txns, double-posts.
    Fix: "matched by `proto.Value` equality of the decoded content (strict parse), not by bytes".

13. **`tried` is never set before a queued entry's first PUT** (D1 `Tried`, D6 sender). An entry
    charged while the team was down has `tried: false`. If the link crashes during the sender's
    first PUT, the restart skips the search. Fix: "before the first PUT of an entry with `tried`
    false, the sender sets it true in a store write (on the goroutine)".

14. **A queued send is posted later against a roster that has changed** (D6 sender). The
    addressee may have been parked or removed, or this member's role demoted, while the send
    waited. Receivers then drop it, and the sender believes it was sent. Fix (new decision):
    "before each post, the sender checks the content again with the current verified roster
    (`DecodeContent` and the roster checks the composer runs). An entry the roster now refuses is
    removed with one log line naming the reason, and `status` counts it".

15. **Trailing newline on stdin** (D5, n3d-D15). `echo text | teamchat say` sends `text\n`.
    Fix: "one trailing `\n` is removed before the size and empty checks; the text must be UTF-8
    (10 `schema`)".

## Nits

1. D2 `Config.Origin` is an exported field marked "tests only". Make it unexported and set it from
   `export_test.go`, so production code cannot point the token at another origin.
2. D6 `RecordOwnSend`: the built outbox `Related` uses `slices.ContainsFunc(p.To, r.HumanReader)` (`engine/seam.go`). Name the same predicate, not "names a roster agent".
3. D6: say that the sender's post-PUT write runs through `do` on the state goroutine. Also say
   that the engine passes `SetHealth` to a team's sender by a non-blocking send, so the
   goroutine never waits on it (n3d-D20).
4. D6 "at `Add` ... looked for in main": at `Add` the team has not run. Write "before the
   sender's first post after `Add`, once the team is up" (and see minor 4).
5. 2a quotes "64 sends of 128 B" and D1 sets 32. Say in n3d-D1 that this halves B 2.3.2's 64,
   and give the proposed B text with "32 sends in flight (20 KiB)".

## Answers to the five questions

1. **SD1-SD5 and the carried halves.** All are present. The contradictions are M5 (send order)
   and M1/M2 (the owner's rule over SD3's queue). Missing decisions are minors 6, 9, 11 and 14.
   SY7 case 4, the LN5 `outbox` records, SY4a's own sends and PT1's failpoint are carried.
2. **Sizing and order.** D1-D5 and W are each the right size. D1 touches five packages, but each
   change is trivial. D6 is not the right size (M7). The order is right: D3 before D6 in
   `member/engine`, D4 parallel through `team.Batch`, D2 used only through `team.Forge`.
3. **API consistency.** The gaps are: `Bucket.Admit` and the built `SendGate` (M5); the send
   Composer's `Lookup` (M6); `Refetch` and `Item.Trust` (minor 2); `team.Answer` against
   `proto.Answer` (minor 7); `teamDone` (minor 10); the exits sets (minor 1). D1's `team.Forge`
   placeholders resolve to `proto.ForgeCheck` and `proto.Forge` (both built).
4. **The owner's offline rule.** It is carried for down teams and transient failures. It is not
   carried for a link started offline (M1), for a token reissue after a restore (M2), or for a
   slow forge on the receive side (M3).
5. **The decisions.** D4, D5, D7, D8, D10, D11 and D15-D23 are sound. D3 should be replaced (M1).
   D12 should be dropped (minor 2). D13 needs the ledger verb (M6). D2 needs M2's change. D6
   needs M4's guard.
