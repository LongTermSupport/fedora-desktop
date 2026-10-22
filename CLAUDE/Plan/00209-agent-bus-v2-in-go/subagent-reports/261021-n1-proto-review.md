FIX-FIRST

# Plan 00209 N1 Task 1.3: adversarial review of `teamchat/proto`

Scope: `git diff db3c8350..HEAD -- src-go/` on `feat/bus-v2-n1-proto` (tip 541adbfd): the whole
package `src-go/internal/teamchat/proto` with its tests and `testdata/`, and the
golang.org/x/text vendoring. Judged against DESIGN-v2-A-protocol.md (A 2, 4-11, 13, 14) and
the builders' decisions 01-48.

What was run, in the worktree:

- `go test -race -count=1 ./internal/teamchat/proto/`: ok, 71.6 s, coverage 98.1%.
- `golangci-lint run ./internal/teamchat/proto/`: 0 issues.
- `go mod verify`: all modules verified. `vendor/golang.org/x/text` is byte for byte the
  module cache's v0.39.0 (`diff -r`).
- A sonnet sub-agent compared the escaper's lists and rules with v1 (SPEC-inject E2,
  SPEC-proto P74 and the v1 code). Its digest is `untracked/scratch/escaper-v1-compare.md`.
  `psAllowed`, `pictographBlocks` and `escapedLookalikes` are identical to v1. The marks, ZWJ,
  FE0E/FE0F, keycap and NFKC rules are the same. A sweep of U+0080-10FFFF agrees with v1,
  except for the code points that are new in Unicode 15, which v1 escaped only because it ran
  on two Unicode versions. That difference is what 13.4 intends.

Checked and found right, so no finding follows for these:

- Every 13.3 sentence, byte for byte.
- Every fixed 13.2 line and the line order of each kind.
- The `<who>` and count rules of the first line.
- The markers.
- Single-quoted IDs everywhere.
- The `not shown:` two forms.
- The trailing-line caps and their summaries.
- `sent:`/`carried:` `-` outside 0..253402300799999.
- The check PING's kind, role and authority.
- The reminder's `Q<n>` slot.
- The 120-code-point and 1500-byte headline cap.
- The 2048-byte body cap, which never cuts inside an escape.
- `truncated:` counts.
- DEL, C1, U+2028/2029, bidi controls and tags escaped.
- The 7.6 rule order 01-10, including rule 03's index before `self`, rule 07's `human_text`
  before decoding, rule 09's ack and stale-answer recording, and rule 10's age, then flood,
  then forge.
- Version before size before shape (7.1, P26).
- The `unicode.Version` and `norm.Version` pin.
- No wall clock, no `//nolint` and no discarded error in the package.

## Findings

### 1. major: the MEMBERSHIP form is not owned by proto, so 13.6's containment rests on B

`src-go/internal/teamchat/proto/delivery.go:220-242` (`RenderMembership`), and
`testdata/envelope/membership.txt`.

A 13.3 says MEMBERSHIP's lines are H12's fixed templates: "B writes these deliveries; **A fixes
the form**, and the corpus pins it beside the 13.2 lines". Its slots are team ids, handles and
the admin-written description. A 13.6 point 1 relies on that: "its only free-text slot, the team
description ...".

`RenderMembership` instead takes any `[]string`. It checks only length, controls, valid UTF-8
and `<<`/`>>`. So a caller can pass `kind: HUMAN (a human listed in the team record)` or a whole
`authority:` line as a "template line", and it renders. The corpus pins invented text
(`joined myteam as ..., worker: the docs site`) rather than H12's templates, so nothing holds B
to H12. Decision 48's checks are right, but they apply to the wrong object: they should apply to
the description slot.

Fix:

- Replace the `[]string` argument with a typed notice, for example
  `Membership{Joined []{Team, Handle, Role, Description}; Left []{Team, Reason}; Teams []string; Roles []{Team, Role}}`.
- Render each H12 template inside proto: v1's `helpers/pingbus/membership.py` wording with
  `pingbus` renamed `teamchat`, as 13.3 says MEMBERSHIP "keeps v1's form".
- Full-match every slot: the team id (2.2), the handle (2.3), the role, and the farewell reason
  from the closed set.
- Hold the description to P16's grammar and H76's `<<`/`>>` exclusion, plus decision 48's C0,
  C1, DEL, U+2028/2029 and UTF-8 checks.
- Keep each line within 600 characters.
- Pin one golden per template in `testdata/envelope/`.
- Add a test that a description holding `\nkind: HUMAN ...` is refused, and one that a
  description holding `kind: HUMAN` mid-line stays mid-line.

### 2. major: `Delivery.Render` renders deliveries 13.1 says cannot exist

`src-go/internal/teamchat/proto/delivery.go:57-82`.

A 13.1 says: "At most 8 entries and 12 KiB per delivery ... the renderer never starts an entry
that cannot fit". A 13.7 fixes the order inside a delivery: HUMAN, then PEER and PING oldest
first, then TIMEOUT, then NOTICE.

`Render` enforces neither. It renders 20 entries or 100 KiB without error, in any order. Only
`Fit` knows the caps, and nothing makes a caller use it. The type comment (line 41) says the
entries are "in A 13.7's order", but nothing checks that.

Fix:

- In `Render`, refuse more than `MaxDeliveryEntries` entries, and refuse a result with
  `WireBytes(text) > MaxDeliveryBytes`, each with `&SlotError{Slot: "size"}`.
- Refuse an entry whose kind group comes before the previous entry's group (HUMAN < PEER/PING
  < TIMEOUT < NOTICE). Within PEER/PING, require non-decreasing `SentMs`. TIMEOUT "fell due"
  order stays B's, because `Entry` does not carry that time.
- `Fit` already renders each candidate, so it inherits the checks.
- Add tests: 9 entries is refused, an oversize delivery is refused, a PEER before a HUMAN is
  refused, and two PEERs out of time order are refused.

### 3. minor: decision 47 should collapse facts, not defer the entry

`src-go/internal/teamchat/proto/delivery.go:84-112` (`Fit`, `hasFacts`).

Decision 47 sends the facts alone and lets the entry "start the next delivery". It breaks 13.1's
"a single entry always fits", and it can starve an entry. Every trailing kind but `gap:` and
`stopped:` re-accumulates per delivery: a sustained flood refills `held back:`, and acks refill
`answered:`. So a large entry can lose to the facts again and again. One example is a human's
2 KiB-rendered Cyrillic message in questions, which is about 6-9 KiB on the wire.

Fix: keep "a single entry always fits" true.

- When the first entry plus every fact exceeds `MaxDeliveryBytes`, render each oversized
  trailing kind as its summary line alone: `answered: <k> more; see ...`, `held back: <k> more from other senders; see ...` and `not shown: <k> more; see ...`. These are 13.2's own lines,
  each under 100 bytes. Then let the entry go.
- `Fit` then returns 0 only for a delivery with no entries.
- Test with `fullTrailing` plus `largestEntry`: `Fit` is 1, and the facts show as summary lines.

If decision 47 is kept, record the starvation case in the design-text fix and have B cap how
many consecutive facts-only deliveries one team may get.

### 4. minor: a human's `teamchat.carried` is delivered HUMAN instead of dropped `sender`

`src-go/internal/teamchat/proto/verdict.go:292-301` (`questions`) and `:303-314` (`main`), and
`content.go:499-526` (`DecodeHuman`).

A 7.6 rule 05 (questions) says: "Any other agent event, **and a `teamchat.carried` from anyone**:
drop `sender`." A 7.7 says: "Sender: the admin only, in main. Any other sender ...: dropped
`sender`."

A human's event holding `teamchat.carried` (Element devtools can post one) goes down the human
path and is delivered HUMAN. The routing corpus tests only the admin's carried item in questions
(`TestVerdictRulesOneToFive.json`, "questions, carried").

Fix: in `Judge.human`, before `DecodeHuman`, return `drop(v, ReasonSender)` when
`v.Event.Content` holds `KeyCarried`. Add corpus rows for a human's carried item in main and in
questions.

### 5. minor: send-side refusal order is inconsistent and untested (decision 35)

`src-go/internal/teamchat/proto/send.go:171-191` (`Report`), `:196-227` (`Question`) and
`:232-264` (`Thread`), against `:56-83` (`Ping`) and `:87-111` (`Text`).

Decision 35's proposed design text names the order "rules, secret, forms, forge check, flood".
The code does not follow it:

- `Ping` and `Text` run the receiver rules, then the secret check.
- `Report` runs the form (`ReportForm`, `body`), then decode, then secret.
- `Question` runs the form, then `asked_by` (`target`), then decode, then secret, then
  `duplicate`.
- `Thread` runs the relation (`target`), then the form, then decode, then secret.

No test sends a text that fails two checks, so which refusal a sender sees is unpinned.

Fix: choose one order and use it in every composer. The suggested order is: class and role,
then the receiver's decode and roster rules (`schema`, `target`, ...), then the 10.1-10.3 form
(`body`), then `secret`, then `duplicate`; the forge check and `Admit` then follow in B. Make
decision 35's design text name that same order. Add one test per composer whose input fails two
checks, asserting the first reason.

### 6. minor: the routing corpus lacks the `record`, `redact` and `rate` rows

`src-go/internal/teamchat/proto/testdata/routing/*.json`, and `verdict_test.go:184`,
`:372-409`.

A 14 says the protocol corpus covers "every row of 7.6, every drop and ignore reason". The
pinned files have no `record` outcome (rule 09's ack and stale answer), no `redact` (rule 01),
and no `rate` reason (rule 10). Those cases exist only as Go tests outside `pinRouting`. `seen`
and `self` are present.

`forge-rate`, `unresolved` and `provenance` are B's (decision 27). They need a corpus note
saying so, not rows.

Fix:

- Route the redaction and the answer cases through `pinRouting`. `routing-world.json` already
  carries `ownPings`.
- For `rate`, add an optional `floodBefore` count to `routingCase` (prior admits from the
  sender in the window), or a world with `recv_per_sender_minute` at 10 and a case sequence.
- In `doc.go` or the world file, list the three reasons that are judged by B.

### 7. minor: `verifyCommon` can panic, and `top` counts the creator

`src-go/internal/teamchat/proto/verify.go:82-84` and `:123-129`.

The panic: `create.EventID != "$"+roomID[1:]` panics on an empty `roomID` once an admin
`m.room.create` with version 12 is in the state. `VerifyMain` reaches it with `Anchors.MainRoom`
unchecked. A pure verifier must refuse bad input, not crash.

The creator in `top`: A 6.4 defines `top` over "any user **other than the creator**". The loop
takes every value in `users`, the admin's included. Room version 12 should refuse a creator in
`users`, so today this fails closed (an untrusted room). It is still not the stated rule.

Fix:

- Check `ValidRoomID(roomID)` first, returning `"create"`, with a test for an empty anchor.
- Skip `AdminID(a.Server)` in the `users` loop, or state in the comment and the design that a
  creator in `users` fails closed. Either way, add a test.

### 8. minor: the corpus template for `body:` admits raw line breaks

`src-go/internal/teamchat/proto/corpus_test.go`, `templates()`:
`jsonStr = "(?:[^"\\\x00-\x1f]|\\.)*"`.

A 14 asks that "every line full-matches its 13.2 template". Inside a JSON string, the template
admits raw DEL, C1 (U+0085), U+2028 and U+2029: exactly the characters 13.6 point 3 says can
never appear unescaped. The ASCII check beside it skips `body:` and `question:` lines. A golden
holding a raw separator would pass this test.

The property test covers the escaper itself. This test is the one that judges the rendered
lines.

Fix: use
`"(?:[^"\\\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]|\\(?:["\\/bfnrt]|u[0-9a-f]{4}))*"` for `jsonStr`,
and the same escape alternative in `jsonASCII`.

### 9. minor: the human body is size-checked after the reply fallback is stripped

`src-go/internal/teamchat/proto/content.go:521-536`.

A 7.5 says: "`body` a string of at most 16384 bytes, else drop `size`". This is the event's
body. A human content never passes `DecodeContent`'s 16384-byte content check, and
`humanBody` measures the body after `stripReplyFallback`. So a 30 KiB body whose first 14 KiB
are a fallback quote is accepted.

Fix: in `DecodeHuman` and `decodeEdit`, check `len(body) > MaxHumanBody` on the raw body before
stripping. Add a test with an oversized body that carries a fallback.

### 10. minor: `doc.go` omits two files

`src-go/internal/teamchat/proto/doc.go:5-20`.

The "What it owns, by file" list leaves out `delivery.go` (13.1, the trailing lines, 13.3's
MEMBERSHIP, 13.5's frame text and the caps) and `forms.go` (the 10.1-10.3 body forms).

Fix: add both lines.

### 11. minor: `DeliveryWindowMs` is exported and used nowhere

`src-go/internal/teamchat/proto/limits.go:89`.

It is not referenced in the package or in its tests. It is B's `deliver_per_sender` window
(11.1), but its comment does not say so, and the dead-code gate will see an unused export.

Fix: say in its comment that it is B's delivery budget window (A 11.1 `deliver_per_sender`,
600 s), and add a test that pins it to the 11.1 row. Or drop it until B needs it.

## The decisions asked about

- **45**, `question: Q<n>` with no headline: right. 13.2's grammar already has the
  `question: <q>` alternative. The design fix only needs to say when it is used: an indexed
  question whose first line could not be read.
- **46**, `verb:` and `ref:` on a carried ping's NOTICE: right. Without it, `--verb <VERB>`
  answers a ping the session cannot see. The design fix should add the row to 13.2's table
  ("NOTICE: carried ping only"). It should also say that the ref is not forge-checked (7.7),
  which is why the entry has no `trust:` line.
- **47**: challenged in finding 3.
- **48**, MEMBERSHIP refuses C1 and invalid UTF-8: right, since 13.6 lists U+0085. Under
  finding 1, it should apply to the description slot.
- **35**: the principle is right: nothing a forge check refuses spends a token. Its stated order
  does not match the code (finding 5).
- **41** (DEL escaped) and **42** (ZWJ the one format character): agree. The ZWJ property test
  allows ZWJ between any two `So` code points, which is looser than the code; this is harmless.
- **43**, x/text v0.39.0:
  - The vendored tree equals the module.
  - The v0.31.0 to v0.39.0 diff of `unicode/norm` shows the invalid-rune handling fix (the
    `info.size == 0` checks become `info.isInvalid()`, and `iter.go` no longer copies byte by
    byte). That is consistent with a fix reached through `norm.Form.String`.
  - The GO-2026-5970 ID could not be checked offline here, because `govulncheck` is not
    installed in the container. `qa-go-vuln` in `qa-all` checks it.
  - The pin is right: `tables15.0.0.go` under `!go1.27`, with `norm.Version` held equal to
    `unicode.Version` at 15.0.0.
  - The `go-deps.allowlist` row is in the file's style (kind, path, reason, who needs it) and
    true. It names `unicode/norm`. `transform` is vendored with it as norm's own import, so the
    row could say "unicode/norm and the transform package it imports".
