MERGE

# Plan 00209 N1 Task 1.3: adversarial review round 2 of `teamchat/proto`

Scope: `git diff db3c8350..HEAD -- src-go/` on `feat/bus-v2-n1-proto` (tip 341f19c3): the whole
package `src-go/internal/teamchat/proto`, its tests and `testdata/`, and the golang.org/x/text
v0.39.0 vendoring with its `go-deps.allowlist` row. Judged against DESIGN-v2-A-protocol.md (A 2,
4-11, 13, 14), 00203 SPEC-hot-control H12 (which A 13.3 makes binding for MEMBERSHIP), src-go/CLAUDE.md
and GoProjectRules.md, and decisions 01-49.

## What was run

- `go test -race -count=1 -cover ./internal/teamchat/proto/`: ok, 74.3 s, coverage 98.2%.
- `golangci-lint run ./internal/teamchat/proto/`: 0 issues.
- `go mod verify`: all modules verified.
- A grep of the package for `time.Now`, `//nolint`, `_ =` and `_ :=`: none.
- `RenderMembership` is gone and nothing refers to it.
- Read against v1: `helpers/pingbus/hooks.py` (`TEMPLATES`, `role_lines`, `membership_notice`),
  `helpers/pingbus/membership.py` (`FAREWELL_REASONS`), `helpers/pingbus/envelope.py`
  (`MEMBERSHIP_FIRST_LINE`).

## Round 1 findings

01. **F1 (major), MEMBERSHIP owned by proto: fixed.** `Membership` is typed (membership.go). Every
    slot is checked: team, handle, role, coordinator, reason from the closed set, and teams. The
    description passes `ValidDescription` (P16: no Cc, no Cf, trimmed, 1-120 runes) and the line
    rule, or it gets no line. The `x\nkind: HUMAN` description is left out, and `kind: HUMAN`
    mid-line stays mid-line (TestMembershipDescription). The golden is regenerated. Residual issues
    are findings 1 and 2.
02. **F2 (major), Render holds the caps and the order: fixed.** It refuses more than 8 entries and
    more than 12 KiB of WireBytes (`size`), and a lower group or an older HUMAN/PEER/PING (`order`).
    Each case is tested in TestDeliveryRenderHoldsTheCapsAndOrder. A gap in those tests is finding 4.
03. **F3 (minor), facts summarised instead of deferring the entry: fixed.** Fit returns the
    delivery. When the first entry does not fit, it sets `summarised` and fits again. A summarised
    delivery shows each kind as its summary line alone. TestFactsThatCrowdOutAnEntryAreSummarised
    uses the largest entry beside every fact. A single entry now always fits, so the starvation
    case is gone. The flag being unexported is finding 3.
04. **F4 (minor), a human's `teamchat.carried`: fixed.** `Judge.human` drops it `sender` before
    decoding, in main and in questions. Both cases are corpus rows ("human carried",
    "questions, human carried").
05. **F5 (minor), send refusal order: fixed.** The Composer doc states one order. `Question` now
    checks `asked_by` after decode, so it fits that order. TestSendRefusalOrder pins one two-failure
    case per composer, ten cases in all. I read every composer against the stated order and they
    agree.
06. **F6 (minor), routing corpus rows: fixed.** `admin redaction` (Redact), `ack` and `stale done`
    (Record), and `rate` are now rows. `rate` uses `floodBefore`, which TestRoutingCorpus replays
    from the file. doc.go names forge-rate, unresolved and provenance as B's.
07. **F7 (minor), verify panic and the creator in top: fixed.** `ValidRoomID` is checked first
    (`create`), and there is an empty-anchor test. `AdminID(server)` is skipped in `top`, with a
    test. Room version 12 refuses a creator in `users` server-side anyway, so skipping it opens
    nothing.
08. **F8 (minor), corpus `jsonStr`: fixed.** As proposed. A residual looseness is listed as nit 8.
09. **F9 (minor), raw human body size: fixed.** The size is checked before the fallback is
    stripped. `decodeEdit` measures `m.new_content.body` raw, since nothing strips it. The corpus
    row is "too big quoted".
10. **F10 (minor), doc.go: fixed.** It lists delivery.go, membership.go and forms.go.
11. **F11 (minor), `DeliveryWindowMs`: fixed.** Its comment names B's `deliver_per_sender` window,
    and TestTheFixedWindowsAreA11 pins it. Nit 7 is about that test.

## Findings

### 1. minor: the MEMBERSHIP role lines are not H12's `role_*` templates, though decision 49 says they are v1's wording

`src-go/internal/teamchat/proto/membership.go:106-121` (`roleLine`), and `:89` (the joined line).
Design: A 13.3 ("MEMBERSHIP keeps v1's form, and 00203 H12 is binding for it"); H12 ("for a role
change ...: the new role, using the existing `role_*` templates"; "for each team joined: the
seat, the handle, ...").

v1's `role_coordinator` is "in team {team} you are the coordinator. Workers report to you and copy
you on what they tell humans; when a human asks you to get something done, work out how and
dispatch it to the workers, then track it." `role_worker` goes on: "Report to it
(`--to-coordinator` ...); it is copied on what you tell humans. Ask the coordinator, never a human,
when you need a decision." proto keeps only the first clause of each. So a session hot-added as a
coordinator or worker is never told how the role works. `role_worker_alone` also gains "now"
here. v1's `joined` line names the seat, and proto's does not, though the handle holds the seat,
so nothing is lost there. Dropping the command-bearing text may be deliberate: B 2.12's test would
fail on a v1 flag that B lacks. But no decision records it, and decision 49 says the opposite.

Fix: choose one of these.

- Render the full v1 role sentences, with only commands B's CLI has (B 2.2.1, for example
  `teamchat send --to-coordinator` if B keeps it). Pin each in TestMembershipNotice and in the
  corpus.
- Record decision 50: the MEMBERSHIP role lines are the first clause only, and the role guidance
  lives in B's SessionStart context. Then reword decision 49 and A 13.3's fix to list the
  shortened templates, and say that the seat is carried in the handle.

### 2. minor: the MEMBERSHIP form is not pinned beside the 13.2 lines in the corpus

`src-go/internal/teamchat/proto/corpus_test.go:215` (membership is skipped) and `:119-121` (one
golden). Design: A 13.3, "the corpus pins it beside the 13.2 lines"; A 14's envelope row, "every
line full-matches its template".

`templates()` holds no MEMBERSHIP template, and `TestEveryCorpusLineFullMatchesItsTemplate` skips
`membership.txt`. Two template lines appear in no golden: the worker with no coordinator, and the
no-team line. They are pinned only by the Go unit test `TestMembershipAloneOnNoTeam`. A change to
a template's wording would update the golden through `-update` and pass every check that is
written from the design.

Fix:

- Add `cases["membership-alone"]` (the `Membership` of TestMembershipAloneOnNoTeam) to
  `envelopeCorpus`.
- Add the MEMBERSHIP templates to `templates()`, written from H12 and v1: the first line, `from: teamchat`, `to: this session`, the MEMBERSHIP kind and authority sentences, and the
  left/joined/is-for/role/teams lines. The `is for:` description slot is
  `[^\x00-\x1f\x7f-\x9f\x{2028}\x{2029}<>]` with no `<<`/`>>`, 1-120 runes.
- Drop `membership` from the skip at :215. The ASCII check must then exempt only the `is for:`
  line.

### 3. minor: `Delivery.summarised` is unexported state that changes the rendered text

`src-go/internal/teamchat/proto/delivery.go:53-55`, `:127-138`. Design: A 13.1 (decision 47 as
revised).

Fit's result renders differently from the same exported fields without the flag. B cannot read
the flag, and cannot set it. Two cases follow:

- B persists a Delivery by its fields for a later repeat (B 2.7.3's store transitions) and renders
  it again. That render fails `size`, which is the very case Fit summarised.
- B cannot tell that the answered, held-back and not-shown details went out as summaries only.

Nothing in proto is wrong today. It is a trap in the API that B is about to build on.

Fix: choose one of these.

- Export it as `Summarised bool`, documented as set by Fit and kept with the delivery, and add a
  test that a Fit result copied field by field renders the same text.
- State on `Fit` that B stores and resends the rendered text and never renders a stored Delivery
  again.

### 4. nit: two order cases are untested

`src-go/internal/teamchat/proto/delivery_test.go:180-190`. Design: A 13.7.

The time order inside the HUMAN group (`n == p && n <= 1` with `n == 0`) has no case: "newer
first" is PEER/PING only. Fit on out-of-order entries (it returns `order` at count 2) is also
untested.

Fix: add `"newer human first": {[]Entry{newerHuman, olderHuman}, "order"}`, and a Fit case
asserting `order`.

### 5. nit: Fit can fail `pending`, which its doc rules out

`src-go/internal/teamchat/proto/delivery.go:145`. Design: A 13.1/13.2 (`<k>` is `[0-9]{1,9}`).

`d.Pending+len(d.Entries)-count` can pass `maxCount` when B's own pending count is near it. Fit
then returns `pending` for a delivery it created, while its doc says "an error is a slot that
failed, never the size of the entries".

Fix: `min(d.Pending+len(d.Entries)-count, maxCount)`, or document that the caller keeps `Pending`
plus the entries within `maxCount`.

### 6. nit: Membership does not check its parts against each other

`src-go/internal/teamchat/proto/membership.go:35-58`. Design: A 13.3, H12.

These all render without an error:

- A team that is both left and listed in `Teams`.
- A joined team missing from `Teams`.
- A team repeated in `Teams`.
- A `Roles` entry's `Handle` and `Description` are ignored without a word.

Fix: refuse `teams` for a duplicate, a left team in `Teams`, or a joined team not in it. Or say in
the `Membership` doc that B guarantees these.

### 7. nit: TestTheFixedWindowsAreA11 hard-codes the budget

`src-go/internal/teamchat/proto/limits_test.go:184-193`.

The budget is pinned as 10 rather than read from the `deliver_per_sender` row's default
(limits.go:38). The test's message says "10 per 600 s", so a default change would leave that
message wrong.

Fix: take the limit from the limits table's default.

### 8. nit: the corpus `jsonStr` still admits raw format and private-use characters

`src-go/internal/teamchat/proto/corpus_test.go:157`. Design: A 14's envelope row, 13.4.

A raw U+202E or U+200B inside `body:` would full-match the template. The exhaustive property test
covers the escaper itself, so this is belt and braces.

Fix: exclude `\p{Cf}\p{Co}\p{Cs}` too. If ZWJ between pictographs is shown raw (decision 42),
exclude `\p{Cf}` except U+200D.

## Decisions 45-49

- **45**, `question: Q<n>` with no headline: sound. It is 13.2's existing alternative. The design
  fix should say it is used when the index holds no readable first line.
- **46**, `verb:`/`ref:` on a carried ping's NOTICE, with no `trust:`: sound. Otherwise
  `--verb <VERB>` would answer a ping whose verb the session cannot see, and 7.7 says the ref is
  not forge-checked.
- **47** (revised): sound. "A single entry always fits" now holds, and nothing starves. The API
  point is finding 3.
- **48**, C1 and invalid UTF-8 refused in MEMBERSHIP lines: sound, since 13.6 names U+0085. With
  P16 (no Cc, no Cf) on the description, bidi controls are excluded too.
- **49**, a typed notice rendered by proto: sound in structure. It closes round 1's F1. Its claim of
  "v1 wording made teamchat" is not true for the role lines (finding 1), so its text needs
  correcting or decision 50 needs adding.

Decisions 01-44: I found none unsound. Decision 35's stated order is now the Composer doc's order
and is pinned. Decision 44's runtime of about 70 s under `-race` was confirmed at 74 s.
GO-2026-5970 could not be checked offline. `qa-go-vuln` in `qa-all` is the check.
