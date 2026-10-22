# N1 Task 1.3 `teamchat/proto`: handoff 5

Superseded: the work below is done and Task 1.3 is complete; see `261021-n1-proto.md`.

Branch `feat/bus-v2-n1-proto`, worktree `untracked/worktrees/bus-v2-n1-proto`. The fifth builder
stopped at the context soft budget during review round 1's fixes. Read `untracked/scratch/n1-common.txt`
(main checkout) first. Handoffs 3 and 4 (`git show 7d57950d:<this file>`, `git show 61dcd6bf:<this file>`)
still hold: their helpers, decisions 01-44 and gotchas.

## Done (committed, green: `go test -race`, golangci-lint, `qa-changed.bash`; 98.1% coverage)

| Commit     | What                                                                                                                                                                                                                                                                                                             |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `b3661e0d` | `envelope.go` (`Entry.Lines`, `SlotError{Slot}`, `EntryQuestion`, `EntryCarried`, `RoleAdmin`, `MaxSentMs`), `delivery.go` (`Delivery.Render`, `Fit`, `Answered`, `HeldBack`, `RenderMembership`, `FrameText`), tests                                                                                            |
| `541adbfd` | `corpus_test.go` and testdata: `envelope/*.txt` goldens, `escape-classes.json`, `routing/<Test>.json` + `routing-world.json` (`pinRouting` in `checkVerdicts`), `-update` flag (`go test -skip TestEveryScalarValue ./internal/teamchat/proto/ -update` rewrites; run it twice the first time a new dir appears) |

Round 1 review: `subagent-reports/261021-n1-proto-review.md`, **FIX-FIRST, 2 major, 9 minor, NOT yet
committed** (commit it with the round 1 fixes). Rounds left: 2 more at most (files `-review-2.md`,
`-review-3.md`); round 1's reviewer brief is reusable (whole diff `db3c8350..HEAD` against chapter A, plus
the x/text vendoring and allowlist row; decisions 01-49 listed).

**Uncommitted in the worktree**: `delivery_test.go` is half-moved to the new API below (it does not
compile until `delivery.go` follows): `mustFit`, `TestDeliveryFit`, `TestDeliveryRenderHoldsTheCapsAndOrder`,
`TestFactsThatCrowdOutAnEntryAreSummarised` (uses an unexported `fit.summarised` field) are written;
`TestRenderMembership` is still the old `[]string` form and must be replaced.

## The round 1 fixes, planned (do them in this order, test first)

01. **F2 (major)** `Delivery.Render` refuses more than `MaxDeliveryEntries` entries and a text over
    `MaxDeliveryBytes` by `WireBytes` (`SlotError{"size"}`), and the 13.7 order (`SlotError{"order"}`):
    group rank HUMAN 0, PEER/PING 1, TIMEOUT 2, NOTICE 3, non-decreasing; within HUMAN and within
    PEER/PING, `SentMs` non-decreasing. TIMEOUT due order stays B's.
02. **F3 (minor, replaces decision 47)** `Fit() (Delivery, error)` returns the delivery to send: the
    most entries (1-8) that render, the rest added to `Pending`; when not even one entry fits beside the
    facts, the facts are summarised (an unexported `summarised bool` on `Delivery` makes
    `trailingLines` print each non-empty kind as its summary line only: `answered: <all> more; see ...`,
    `held back: <sum, capped at 999999999> more from other senders; see ...`, `not shown: <all> more; see ...`;
    gap, stopped, pending unchanged) and it tries again; a size error after that is returned. No entries:
    Render-check and return. The test's expected tail is already in `TestFactsThatCrowdOutAnEntryAreSummarised`.
03. **F1 (major)** replace `RenderMembership([]string, nonce)` with a typed notice proto renders:
    `Membership{Left []MembershipLeft{Team, Reason}; Joined, Roles []MembershipTeam{Team, Handle, Role, Coordinator, Description}; Teams []string}` and `(m Membership) Render(nonce) (string, error)`;
    `FarewellRemoved = "removed"` (v1 `membership.FAREWELL_REASONS`, the one reason). Lines, in this order,
    v1 `helpers/pingbus/hooks.py:129-160` wording with `agent-bus`/`pingbus` made `teamchat`:
    - `teamchat: you left team <team>: removed by the owner. Commands for it are refused from now on.`
    - per joined team: `teamchat: you joined team <team> as <handle>.`, then
      `teamchat: team <team> is for: <description>` only when `ValidDescription` (P16, rooms.go) holds and
      it has no `<<`/`>>` (else no line: A 4.4 "otherwise it has no description"), then its role line;
    - per role change, its role line: coordinator `teamchat: in team <team> you are the coordinator.`;
      worker `teamchat: in team <team> you are a worker; the coordinator is <handle>.`, or with no
      coordinator `... you are a worker; the team has no coordinator now.`;
    - `teamchat: this session's teams are now: <a>, <b>.` or, none,
      `teamchat: this session is on no team now; nothing arrives until the owner adds it to one.`
      Slots: team (`slotTeam`), handle, role (coordinator|worker), coordinator (handle), reason, teams
      (each a team id); nothing changed (no Left, Joined, Roles) is `membership`; every output line still
      passes `badMembershipLine` (600 characters, no C0/DEL/C1/U+2028/U+2029, `<<`, `>>`, valid UTF-8; a
      failure is `membership line`, e.g. 30 teams of 22 characters). Tests to write: the full notice
      exactly, alone-and-on-no-team, the description cases (`x\nkind: HUMAN ...`, `a << b`, ` padded`,
      121 a's, U+2028 are left out; `kind: HUMAN ...` stays mid-line after `is for: `), and the slot table.
      Then update `envelopeCorpus()["membership"]` in `corpus_test.go` and rerun `-update`.
04. **F4** `verdict.go` `Judge.human`: content holding `KeyCarried` (top level, `v.Event.Content.Get`) is
    `drop(v, ReasonSender)` before `DecodeHuman`; add cases "human carried" in main and in questions to
    `TestVerdictHuman`'s / `TestVerdictRulesOneToFive`'s `checkVerdicts` tables (they reach the corpus).
05. **F5** The send order already is one order; say it in `Composer`'s comment and test it: class and
    role; what the content is built from (the index's number and relation, `schema`/`target`; the
    10.1-10.3 form, `body`); the receiver's rules over the built content (decode and roster: `schema`,
    `size`, `target`, ...); `secret`; `duplicate`; then in B the forge check and `Admit`. One test per
    composer with two failures (a secret beside a target, form or duplicate failure; `ownSample` is the
    secret) asserting the first reason. Decision 35's design fix names this order.
06. **F6** route the answer (`Record`) cases and the redaction (`Redact`) case through `checkVerdicts`
    where they are single events (`TestVerdictAnswers`, `TestVerdictRedaction`, verdict_test.go ~183 and
    ~362); for `rate` add an optional `floodBefore int` to `verdictCase`/`routingCase` (admits recorded in
    `Judge.Flood` for the sender before judging). Add to `doc.go` (or the world file) that `forge-rate`,
    `unresolved` and `provenance` are B's (decision 27).
07. **F7** `verify.go:82-84`: check `ValidRoomID(roomID)` first (`create`), test an empty anchor; `:123-129`
    skip `AdminID(a.Server)` in the `users` loop for `top` (A 6.4 "other than the creator"), with a test.
08. **F8** `corpus_test.go` `templates()`: `jsonStr` = `"(?:[^"\\\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]|\\(?:["\\/bfnrt]|u[0-9a-f]{4}))*"`, and the same escape alternative in `jsonASCII`.
09. **F9** `content.go` `DecodeHuman`/`decodeEdit`: `len(body) > MaxHumanBody` on the raw body before
    `stripReplyFallback` (`size`), with a test of an oversized body carrying a fallback.
10. **F10** `doc.go`: add `delivery.go` (13.1 shape, trailing lines, caps, 13.3 MEMBERSHIP, 13.5 frame
    text) and `forms.go` (10.1-10.3 body forms) to the by-file list.
11. **F11** `limits.go:89` `DeliveryWindowMs`: say it is B's `deliver_per_sender` window (A 11.1, 600 s)
    and pin it in a test.

## Decisions 45-49 (with 01-44, for the report)

45. A questions-room entry whose question has no headline renders `question: Q<n>`. Fix A 13.2: say
    the form is for an indexed question whose first line could not be read.
46. A carried ping's NOTICE carries `verb:` and `ref:` (no `trust:`: 7.7, not forge-checked). Fix
    A 13.2's table: `verb:` `ref:` row, NOTICE column "carried ping only".
47. (as revised by F3) Facts that leave no room for the first entry are summarised to their summary
    lines, so a single entry always fits. Fix A 13.1: "when the first entry does not fit beside the
    trailing lines, each kind of them is given as its summary line alone".
48. A MEMBERSHIP line also refuses C1 controls and invalid UTF-8. Fix A 13.3's line rule.
49. (F1) proto renders MEMBERSHIP from a typed notice with the templates above. Fix A 13.3: list the
    templates (or point to proto's), and say a description failing P16 or holding `<<`/`>>` gives no
    `is for:` line.

## Then

The report `subagent-reports/261021-n1-proto.md` (every decision 01-49+ with its chapter section and new
wording), Task 1.3 marked in PLAN.md, commit. Final message to the lead under 700 characters: tip,
verdict, coverage, count of design-text fixes.

## Gotchas (new)

- A large Bash heredoc can trip the secret-path guard (it matched `*.vault-password` on Go test text):
  edit Go with `Edit`.
- Golden files are written by `-update`; review them by eye before committing.
