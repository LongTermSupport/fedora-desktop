Verdict: FIX-FIRST

# Plan 00209 N0 area A (protocol and rooms): adversarial review, round 1

Reviewed: `DESIGN-v2-A-protocol.md` (1559 lines) against `DESIGN-v2-FRAME.md`, the N0 common rules,
DESIGN.md section 2 (R15-R30, R37-R44), and the scratch digests (spec-proto, matrix, 00211,
inject-hooks, v1-code). Line numbers are those of the reviewed file.

Overall: the document is strong. The allow-list power levels, the invariants-versus-exact split, the
versioning rule (ignore unknown, refuse unknown features), the typed notices, the server-time
deadlines and the reply-relation answers are real improvements, and each comes with its reason. The
findings below are mostly internal contradictions and a few trust and Matrix-fact gaps. None of them
needs the design rethought. Four of them would make the build implement the wrong thing, so they
are fixed first.

Counts: blocker 0, major 6, minor 14.

---

## Major

**1. major. Question-index handover contradicts "judge against the current roster" (10.2 l.1045, 10.5 l.1099, 7.6 rules 03/05 l.816-823, 6.5 l.591-593).**
What is wrong: 10.2 numbers questions as "1 plus the highest `n` among the coordinators' questions in
the room's history". 10.5 says a replacement coordinator gets reminders "from the room's history". But
6.5 judges every event against the roster as it stands now, and 7.6 rule 04/05 drops any questions-room
agent post whose sender is not the current coordinator (`drop sender`). A demoted or removed
coordinator is a worker or class none, so all of its questions and `<ns>.thread` closes are dropped.
The new coordinator then restarts at Q1, which duplicates the Q numbers humans already see in Element.
It also sees closed questions as open (their closes are dropped) and loses every open question's
reminder. That defeats 00211 D13, the reason `history_visibility` is `shared`. There is also an
ordering bug: rule 03 ignores `self` before rule 05, so "the coordinator's own posts feed the
question index" can never be reached.
Evidence: 00211 digest l.37-40 (handover relies on reading history); 7.6 rule 05 "Any other agent: drop `sender`".
Fix: give the question index its own rule. In the questions room, an `<ns>.question` or `<ns>.thread`
from any agent account counts for the index. This is sound because questions is invite-only and only the
admin invites, and only coordinators. Server-enforced membership is the authorisation, and that history
is never delivered. Keep the current-roster rule for delivery only. In rule 03, exempt questions-room
self posts so they reach the index.

**2. major. Presence as liveness and capability channel is under-specified and conflicts with R27 (6.6 l.604-626, 3.2 l.264, OD-A1).**
What is wrong:

- R27 makes the seat lock the presence fact ("Presence is the seat lock alone"). 6.6 adds a second
  liveness truth (Matrix presence) that the board and `who` read, without citing R27 or saying which
  one wins when they disagree.
- Matrix `/sync` sets presence implicitly. Its `set_presence` parameter defaults to `online`. So a
  syncing worker, or any one-shot command that syncs, re-marks the member online after the keeper set
  `offline`. It may also drop `status_msg`: in Conduit-family servers the sync-driven presence update
  rewrites the record. The doc does not say which process syncs with which `set_presence`.
- The capability report (`wire=`/`features=`) is a moving part the system does not need. C installs
  every member's kit and records its version (R8 applied stamp, R49 one kit per session). The majors
  and features a member accepts are a pure function of that version, which the host already knows. A
  member that is offline, or whose status was clobbered, blocks a `proto.send` raise indefinitely.
  Fix: keep presence for the Element dot only, and state it is display. Name the seat lock as the
  liveness truth for `who` and the board (R27). Require every member sync to pass `set_presence=offline`
  (or `unavailable`), with the keeper alone setting `online`, or drop the keeper PUT and let the sync set
  it. Add this to HC-A1. Have C's raise operation derive accepted majors from the kit version table, not
  from `status_msg`. Then OD-A1's fallback (the state event) is no longer needed for version reporting.

**3. major. The envelope header grammar contradicts its own lines, and 13.6 overstates containment (13.2 l.1266-1268, l.1277, 13.6 l.1363-1370).**
What is wrong: 13.2 says header values are `[\x21-\x7e]{1,1200}` "no space", except the fixed
sentences and the reply command. But `to:` ("this session, as <handle>"), `copy:`, `truncated:`,
`answered:`, `held back:` and `question:` all hold spaces. `question:` also holds the question headline
"as a JSON string", and the 13.4 renderer shows non-ASCII letters as themselves, which breaks the
printable-ASCII rule. A builder who follows 13.2 literally refuses every entry. More important, 13.6
point 1 ("every header line ... passed a full-match ASCII grammar") and point 3 ("the sender's text
appears only inside one JSON string on a line that begins `body: "`") are false. The coordinator's
headline is sender-written text on the `question:` header line, and it rides on a HUMAN entry, so a
model can attribute coordinator words to the human.
Evidence: P77 and E3 in the spec-proto and inject digests (v1 had the same gap).
Fix: give each line its own grammar in a table: fixed template text plus typed slots. Render the
headline with the 13.4 escaper in ASCII-only mode (every non-ASCII as `\u`), or drop it and show `Q<n>`
alone. Restate 13.6 point 3 as "sender text appears only in JSON strings rendered by the one escaper,
on `body:` and `question:`". Mark the headline in the frame text as the coordinator's words.

**4. major. The `msgtype` rule contradicts the examples and AR26 (7.1 l.678 against 7.2 l.696, 7.4 l.744-752, 12.1 l.1184-1188, AR26 l.127-130).**
What is wrong: 7.1 says `m.text` whenever the event addresses a human. But 7.4's notice addresses
`@alice` and is `m.notice`. 12.1 and AR26 say pings, answers and lifecycle notices are quiet notices.
And `ack`/`nack`/`done`/`blocked` may address a human (8.1 l.863). Under 7.1, every done-to-human and
every lifecycle notice becomes a notifying `m.text`, which is the noise AR26 removes.
Fix: one table of `msgtype` by object. `<ns>.text` to a human is `m.text`. `<ns>.question` and an
asking `<ns>.thread` are `m.text`. Everything else (`<ns>.ping` of any verb, `<ns>.notice`,
`<ns>.report`, a non-asking thread post) is `m.notice`. If owners want a `done` sent to a human to
notify, make that an owner decision, not a side effect of 7.1.

**5. major. Questions and thread posts skip the secret and own-token refusal (10.2 l.1042-1051, 10.3 l.1073-1079).**
What is wrong: 7.3 (agent text) and 10.1 (reports) refuse secret shapes and the member's own token,
which R43 and P51 require. 10.2 and 10.3 do not. These are free text from an agent, posted into a room
whose history is `shared` and is mirrored in every human's Element. A pasted token cannot be redacted:
redaction is closed (4.2).
Fix: state that every free-text send (`text`, report, question, thread post, and any `nack`/`blocked`
free text if added later) runs the same secret and own-token check. Add `secret` refusal cases for
`ask-human` and `thread` to the corpus.

**6. major. The power-level invariants are said to imply more than they do, and two Matrix facts are wrong (4.2 l.326-329, l.376-378; 6.4 l.560-562; 12.2 l.1215-1218).**
What is wrong:

- "no one but the admin can change state, membership or history" (6.4) is false for membership.
  `m.room.member` is not gated by `state_default`. Any joined user can leave. Any main member, human or
  agent, can self-join reports by the restricted rule with no invite (4.3 says humans come in by admin
  invite). Any joined user can rewrite its own displayname and avatar (12.3 admits this one). None of
  these is an exploit today, but the threat table (6.9) does not list "an agent leaves main" (main is
  invite-only, so it cannot come back, a silent self-removal). The text also does not say that the
  restricted join lets humans in.
- Location shares are not closed by the allow-list (4.2 l.326, 12.2 l.1217). Element sends a static
  location as `m.room.message` with `msgtype` `m.location`. The same holds for files, voice and
  `m.emote`. 7.5 handles these as ignored, but 4.2 and 12.2 say they are refused.
- 12.2 attributes "redaction closed" to I2. Redacting one's own event is gated by the
  `m.room.redaction` event level, so I4 (or I3 if listed) closes it, not `redact` (I2).
  Fix: in 6.4, list exactly what I1-I4 close and what stays open (self leave, restricted self-join to
  reports, own profile, receipts, typing, presence). Add "agent leaves main" to 6.9: the next sync sees
  its own leave, so it is reported as untrusted and C re-invites. Drop "location" from the closed list
  and say any `m.room.message` msgtype is open and ignored by 7.5. Cite I4 in 12.2.

---

## Minor

**7. minor. A new human can post in reports in the window before its power levels are rewritten (4.2 l.380-382, 5.2 l.486-491).**
Reports lists humans at 0 and `users_default` is 10. A human invited to main can join reports by the
restricted rule before C adds them to reports' `users`, and then posts at 10. That breaks 00211 D2
(reports read-only for humans, enforced). 5.2 orders removals only.
Fix: add a 5.2 rule: when a human is added, write reports' power levels before the main-room invite.

**8. minor. The current-roster rule fails open in two directions (6.5 l.591-593).**
"Fails closed" holds for removal. It does not hold for promotion: a worker's `fetch`/`halt`, sent
before promotion and still unjudged within its timeout, is accepted. Nor for a `human_text` false-to-true
flip: older human messages up to `human_max_age_s` (up to a week) become deliverable. Fix: for pings
and human messages, also require the sender's class to have held when the event was sent: an event
older than the snapshot's roster event (by stream order) whose class was narrower is dropped `role`.
Or state the fail-open as accepted, with the staleness bound named.

**9. minor. Two values for `trust:` (13.2 l.1283 against 9.3 l.980).**
13.2 says `trusted` or `external`; 9.3 says "deliver, with the class as `trust`", and the classes are
listed/org/external. Pick one. `trusted`/`external` matches the 13.3 authority split.

**10. minor. Broken cross-references.**
2.2 l.176: verbs are "section 7" (they are in 8.1). l.177: references are "section 8" (they are in
section 9). 4.1 l.297: addressing is "section 6" (it is 7.5/7.6). 7.4 l.760-764 and AR18 cite R30 for
admin room notices, but R30 is the session's local MEMBERSHIP notices (L4), so cite R30 only for
MEMBERSHIP. 12.2 cites I2 for redaction (see 6).

**11. minor. AR6 calls relations "Matrix-authenticated" (l.49-50).**
`m.relates_to` is content the sender writes. Only the related event's own `sender` is authenticated,
once fetched. Fix: move relations to "sender claims, checked by fetch in the same room". Also state
that 7.5 rule 3 and 8.2 fetch the replied-to event with `GET /rooms/{this room}/event/{id}`, so a
relation to an event in another room fails (v1 defect 08 checked by grammar only).

**12. minor. Multiple-coordinator wording is left over after R10 was adopted (AR23 l.114, 7.3 l.731, 10.4 l.1083, 5.1 l.460).**
"every coordinator" appears in several places, but 5.1 allows at most one non-parked coordinator.
It is also undefined whether a parked coordinator keeps questions open, and whether it stays joined
in questions (4.3 says parked members stay joined). Fix: write "the coordinator" throughout. State
that a parked coordinator counts as none for questions (closed), and that it is kicked from questions
when parked.

**13. minor. The duplicate-window key refuses distinct texts (11.2 l.1152).**
`[verb or kind, ref, re, sorted to]` has no text component. A second, different `say` to the same
addressee within 60 s is refused `duplicate`. Fix: add a SHA-256 of the typed object's text (or of
the report or question body) to the key for text-bearing kinds.

**14. minor. `forge.api` is a configurable credential destination (5.1 l.441, l.464; 9.3 l.958-959).**
Each member sends its own GitHub credential to whatever origin the roster names. Anyone who can write
the roster (root or a backup holder, R38) can then harvest every member's forge token, not only
instruct agents. That widens what the R38 set gets. `forge.kind` is `github` only. Fix: drop
`forge.api` and fix the origin per kind in `<v2>/proto` (`https://api.github.com`). Adding GitHub
Enterprise later is a feature.

**15. minor. Reply-fallback stripping can eat a human's own quote (7.5 l.782).**
"leading `>` quote lines ... removed when present" applies to every message. A human message that
opens with a markdown quote is altered, or ignored as `empty`. Matrix 1.13 (MSC2781) removed reply
fallbacks, and current Element no longer sends them. Fix: strip only when `m.in_reply_to` is present
and the quote matches the fallback form `> <@user:server> ...`. Record the observed shape in HC-A3.

**16. minor. Thread fallback target (10.3 l.1063-1068).**
The specification's thread fallback sets `m.in_reply_to` to the latest event in the thread, not the
root. Root works, but non-thread-aware clients then show every post as a reply to the question. Fix:
use the latest thread event the coordinator knows of, or note the deviation and the reason.

**17. minor. A local-clock TIMEOUT can precede an on-time answer (8.2 l.880-886, 8.3 l.893).**
The deadline is in server time, but the TIMEOUT fires when "the member's clock passes" it. If the
member's clock is ahead, or the answer arrives slowly, a TIMEOUT is delivered for a ping whose answer
then counts as on time. 8.4 has no state for "TIMEOUT already delivered, answer on time". Fix: fire
only when local now is past the deadline plus a fixed grace (one long-poll interval), and say that an
on-time answer arriving after its TIMEOUT is delivered with `answered:` marked "after timeout".

**18. minor. The drop and ignore closed sets are incomplete (7.6 l.839-841).**
A human edit whose original is missing, in another room, or from another sender (7.5 l.799-801) has no
named reason. A human message in reports or a closed questions room has none either. Fix: add `edit`
for edit-check failures (it is already a drop reason) and say so in 7.5. Run a corpus case per row.

**19. minor. 12.3's display-name check must follow R39 (12.3 l.1230-1231).**
C's health check reads member-controlled `m.room.member` display names. R39 requires every such field
to be grammar-checked before it is passed on. Add "names compared and reported by user ID; the
display name itself is shown only when it passes the human or handle grammar".

**20. minor. The envelope is ported, not weighed (13, l.1237-1239).**
The frame and common rules ask for at least two designs per part. Section 13 keeps v1's key-value
grammar because it "held up under review" and weighs no alternative. One obvious candidate: each entry
as one compact JSON object on one line, with fixed key order and ASCII-only escaping. It removes the
whole per-line header grammar (finding 3) and makes the containment argument a one-liner. It costs
some readability for the model. Fix: weigh it and keep the reason. The present choice may well stand.

---

## Checked and sound

- I1-I4 do close own-event redaction (`m.room.redaction` falls to `events_default` 100), reactions,
  stickers, polls, power-level and tombstone writes. Creators being unlisted in `users` and holding
  unlimited power matches room version 12. The create event ID equalling `$` plus the room ID's hash
  matches v12 room IDs. `GET /state` returns `m.room.create` with its `sender` in the specification;
  HC-A7 is cheap insurance, not a gap.
- `.m.rule.suppress_notices` sits in the override rules ahead of `.m.rule.is_user_mention`, so a
  mention on `m.notice` never notifies (12.1 is right). Element adds the replied-to sender to
  `m.mentions` for a plain reply and adds nothing for a reply in thread (7.5 rules 3-4 are right and
  needed). `+` in localparts is valid since Matrix 1.8 (MSC4009).
- Stripped invite state carries `sender`, so the 6.3 invite check is implementable.
- The handle grammar keeps humans and agents disjoint. The reserved-name list and the AR7/AR9 rules
  are sound.
- Requirements carried from R15-R23, R28-R30, R37-R44: each has a home in A, or is B's or C's with an
  assumption in section 20. Nothing silently vanished, apart from R27 (finding 2) and the secret checks
  (finding 5).
- No install-specific names or time estimates were found.
