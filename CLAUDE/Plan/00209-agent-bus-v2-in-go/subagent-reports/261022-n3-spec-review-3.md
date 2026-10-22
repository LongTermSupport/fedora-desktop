# Adversarial review, round 3 (final): SPEC-n3.md (commit 139108a5)

Plan 00209, Task 3.1. Checked against round 2 (`261022-n3-spec-review-2.md`), its fix log
(`261022-n3-spec-fixes-2.md`), the diff c4c62453..139108a5, B 2.1-2.15 (2.3.2, 2.5, 2.7.3, 2.10,
C6), A 7.5, C 3.7.5, 3.9, 3.11, and the code under `src-go/internal/teamchat/`
(`proto/verdict.go`, `matrix/client.go`, `matrix/member.go`, `matrix/client_test.go`,
`matrix/fakehs/sync.go`, `health/online.go`, `seat/lockprobe.go`).

**Verdict: FIX-FIRST.** One major, in round 2's MAJOR 1 fix: the rule is now right, but it is
written against a `proto.Lookup` that cannot carry it, and section 9 does not name the API change.
The fix is a short gap entry and one sentence in SY4a. The other round-2 findings are closed.

Counts: BLOCKER 0, MAJOR 1, minor (accepted for build) 6.

## Round 2, finding by finding

| R2      | Closed?          | Evidence in 139108a5                                                                                                                                                                                      |
| ------- | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MAJOR 1 | partly           | SY4a, UH1, gap 11 and n3-D20 state the source order, the cache, the fetch and the 404/403 and transient rules. The mechanism does not fit the built interface: **MAJOR 1**                                |
| MAJOR 2 | yes              | LK1 tries every acquire, the seat lock included, three times 50 ms apart. This matches B 2.5 "Acquisition" (lines 1201-1202). LK3, GU3, GU5 and SA1 cite it. Has the second-process `LOCK_SH` tests       |
| MINOR 1 | yes              | SY7 says the deadline is per try, and that a 429 retry is a new try bounded by `maxTries` and `maxRateWait` (`client.go:46-47, 357-387`)                                                                  |
| MINOR 2 | yes, with a note | Gap 12 now covers `once`'s deadline. The seam it names cannot drive a deadline as written: see minor a                                                                                                    |
| MINOR 3 | yes              | TB2's sentence is reworded, and the five hostile rows are added                                                                                                                                           |
| MINOR 4 | yes              | LP1 names its inputs and takes n2c-D14's rule (a refused open is free, with no record). `health.moveOn` then posts the member's own `disconnected` for a run it saw held, which is TB2.1's bounded effect |
| MINOR 5 | yes, with a note | LX7 leaves a deferred seat out of that apply's desired seats. The farewell and re-add consequences are not stated: see minor b                                                                            |
| MINOR 6 | yes              | Section 9 opens with a line giving each gap its slice. GU1's VM half moved to n3b. n3c's row now says its VM run follows n3b's merge                                                                      |
| MINOR 7 | yes              | LP2 owns the announcer adapter, and the PLAN line moves in the commit that lands LP2. godbus is decided once, in n3b                                                                                      |
| MINOR 8 | yes              | LX4's per-entry `ELOOP` and non-regular handling, with the EL9-shaped fixture                                                                                                                             |
| MINOR 9 | yes              | The v1 holdback seam gets TB1's opener. A FIFO is planted at the v1 env-file path                                                                                                                         |
| NIT 1-5 | yes              | all applied                                                                                                                                                                                               |

## MAJOR

**MAJOR 1. SY4a's fetch and its transient rule cannot be expressed through the built
`proto.Lookup`, and section 9 does not name the change (SY4a, gap 11, n3-D20;
`proto/verdict.go:98-118, 343-364, 474-480`).**

The built interface is:

```go
type Related struct { Sender, RoomID, Verb string; AddressedToSelf bool }
type Lookup interface { Seen(id string) bool; Event(id string) (Related, bool); OwnPing(id string) (OwnPing, bool) }
```

`Judge.Judge(raw) Verdict` is pure and has no error path. Here is what it does with a `false`
from `Event`:

- `sentBySelf` treats it as "not mine", so the event is ignored with `not-addressed` (line 362).
- `original` treats a missing edit original as `drop(ReasonEdit)` (lines 343-348).
- `probeCheckOf` treats it as "not a check answer" (line 478).

SY4a puts the fetch inside `Event(id)` ("On a miss ... one `matrix.GetEvent`"), and says a
transient failure "is SY8's transient path: the batch is held unpersisted and re-judged, never
guessed as `not-addressed`". Through this interface that cannot happen:

1. **The error cannot get out.** `Event` has no `context.Context` and no error. A fetch failure
   can only come back as `(Related{}, false)`, and `Judge` turns that into exactly the
   `not-addressed` ignore, or the edit drop, that SY4a forbids. A side-channel flag read after
   `Judge` would be an unspecified hack. SY4a's own test ("the fetch failing transiently
   persists nothing") cannot be made green without an API decision the spec does not take.
2. **`Event(id)` does not know which event is being judged.** It cannot apply SY4a's limit
   ("only for a main-room human event carrying a reply, thread or edit relation"). The same
   `Lookup` is called for agent pings (`probeCheckOf`), and `proto/send.go:51, 323` uses it on
   the sender side in the CLI, where no fetch may run.
3. **`Related` has no field for "addressed to a roster agent".** Gap 11's predicate and SY4a's
   tuple both need one, and only `AddressedToSelf` exists. The ledger (B 2.3.2, lines 609-611 and
   662-664) holds room, thread, from and item kind. It holds no verb and no addressing, so the
   spec should also say how a ledger entry fills `Related`. For example: a delivered item was
   addressed to self, and so to a roster agent.

The header promises that "section 9 names every gap in their APIs". This one changes a built N1
package, and it decides whether 2.0g's count and A 7.5 rules 3 and 4 are safe.

**Fix (small):** add a gap 14 (n3c) and make SY4a say this:

- **Resolve before judging.** Before `Judge` runs on a sync answer, the link collects the relation
  ids of the answer's main-room human events (reply, thread root, edit original). It resolves each
  one through the ledger, the own sends, the cache and then one `GetEvent`, under the team's
  request deadline.
- **Transient failure.** Any transient failure holds the whole answer, before `Judge` runs and
  before SY6 and SY5, through SY8's path.
- **404 and 403.** These are cached as "unknown sender" for this link's life.
- **`Judge` itself.** `Judge` then runs over a `Lookup` that answers from memory only. `proto`
  stays network-free and its interface keeps no error.
- **`Related` gains `AddressedToRosterAgent`** (or the addressees). Gap 11's predicate reads it.
  Ledger and outbox entries fill it by a stated rule.
- **Sender side.** The sender-side `Lookup` (`proto/send.go`) stays memory-only.
- **Test:** a fetch error injected for one relation id leaves the answer unpersisted, and
  `Judge` is never called for that batch.

## Minor, accepted for build (no change required before MERGE; fix while building)

a. **Gap 12's deadline seam.** `matrix.Clock` has only `Sleep(ctx, d)` (`client.go:66-78`). The
existing test clock returns at once (`client_test.go:28-44`). A deadline built as
`Sleep(limit)` then `cancel` would time out every request in the current matrix suite. Give
the per-try deadline its own seam: a timer or `AfterFunc` method, or a separate field. Do not
reuse the 429 clock's `Sleep`.

b. **LX7's deferred seat.** Taking the seat out of the desired file needs a `farewell` entry by
B C6, "for every seat it takes out of a session". Name the reason: `removed` for a retired old
identity, since the admin's notice was posted at step 06. Also, at the next apply the keeper
remembers the seat as removed, so RC1 makes the re-add SY1 (c), with timeline limit 0. Items
addressed to the new identity between its join and the next apply are then skipped. Either
key the "removed" memory by `user_id`, so that a new identity resumes by SY1 (b), or state the
loss. Add a test.

c. **n3-D20 against B 2.7.3.** B line 1510 says "That is the only re-fetch" (restored items). SY4a
adds a second fetch. Record it as a departure in n3-D20.

d. **A poison relation stalls a team.** A related event that keeps answering 5xx holds every
later answer of that team for good: `down` after 15 s, and nothing more. State it as accepted,
or bound it. For example, after N transient failures on the same id, treat it as unknown
sender and log one line.

e. **UH1 "once per event" across a discarded answer.** An answer that SY6 discards is re-fetched
and judged again, so an in-memory counter counts its events twice. Dedupe the `unheard` count
by event id, held as long as the cache.

f. **Own sends.** SY4a records own sends "at its send" but does not say whether the record
survives a link restart. Memory-only is fine, because the fetch backs it. Say so in n3-D20.

## Whole-document pass: checked and sound

- **Test-first.** Apart from MAJOR 1, every expectation names its fakes and its failing case.
  Gaps 3-13 each name the seam a test needs. Gap 12, with minor a, removes the last real-time
  wait.
- **Trust boundary.**
  - TB1: every host-side guest read, directory listing and socket connect goes through
    `O_PATH`, a regular-file or `S_ISSOCK` check, and the `/proc/self/fd` reopen. This covers
    LX3-LX5, LP1, LX8, SU2 and D's seam. No host step opens a FIFO.
  - TB2: its four bounded effects are unchanged, and the reworded path sentence now matches LX5.
    LP1's refused-open rule leaves the host, at most, posting the member's own `disconnected`,
    which is TB2.1.
  - TB3 and TB4 are unchanged.
- **SY7 and H-C14.**
  - The per-try deadline wording now matches `once` and `answered`.
  - Gap 6's atomic epoch raise and SY6's read before SY5 still make every restored-server answer
    discarded.
  - Case 3's late drop is a transient timeout followed by SY6's read.
  - Case 8 is a new try, and is checked like any other answer.
  - The Tuwunel case runs under `-tags tuwunel` in Task 3.4.
- **2.0e.** RS1-RS3 are unchanged and correct. The PLAN row stays open until RS3.
- **2.0g.** UH1 is right once MAJOR 1's resolver feeds it.
- **Gate marks.** Every host run is **[A]**. The one **[B]** is LN9's ccy hot reissue, in
  Task 4.4. The LXC path uses token seats only.
- **Slice order.** n3a, then n3b and n3c in parallel, then n3d and n3e. Every gap is placed in
  its slice by section 9's lead line. n3f is deferred to `SPEC-n3f.md`.
- **B 2.15 completeness.** P1-P11, F1-F6, T1-T25, E1-E3, B23, B24 and B30 are each mapped.
  PT1 names all 14 failpoints. T23's LXC half maps to LP1 and LP2.
