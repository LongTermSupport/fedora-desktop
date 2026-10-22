Verdict: FIX-FIRST

# Plan 00209 N1: adversarial review of the design sync (`ad0405ca..567ab3c4`)

Scope: the diff to DESIGN-v2-A/B/C/D, DESIGN-v2.md and PLAN.md, read against
`src-go/internal/teamchat/{proto,matrix,paths}`, `src-go/qa/teamchatseparate` and SPEC-matrix.md.
Check 2 (every proposal applied or listed): passes. All 51 proto fixes, the 4 matrix proposals (3
applied, item 4 listed with a reason), the paths proposals, the paths-check Task 6.1 minor and the
two hc-design-check minors are in the diff. Check 4 (gates): no applied fix changes an answered
gate-A or gate-B decision. The `_lxc`, presence, Redact, LoginDevice/Logout and marker-read text
matches the code. Findings 9 and 5 below are about where the report's gate notes landed.

Counts: 2 major, 9 minor, 4 nit.

## Findings

01. **major**: `DESIGN-v2-A-protocol.md:1916`, "The one farewell reason is `removed`."
    This contradicts B and C. B 2.10 (`DESIGN-v2-B-member.md:1753-1757`, 2194), B's C6
    (`B:2396`) and C 3.7.5 give `desired.json` `farewell` three reasons: `released`, `removed` and
    `parked`. B 1780-1783 makes every change of the active set one MEMBERSHIP notice, "left with
    its reason". The code agrees with A, not B: `proto/membership.go:11-13` has only
    `FarewellRemoved`, and `changeLines` refuses any other reason with `SlotError{"reason"}`
    (`membership.go:72-77`). So a `released` or `parked` farewell cannot be rendered, and B's
    reconcile would fail on the commonest leave. The text was copied from v1's
    `helpers/pingbus/membership.py:48`, which had one reason. **Fix**: in A 13.3, replace the
    sentence with "B's farewell reasons are `released`, `removed` and `parked` (B 2.10). proto
    renders `removed` only (`FarewellRemoved`), so the left-line texts for `released` and `parked`
    are owed to N3 with B." Add the same item to the design-sync report's "Not applied, for the
    build session" list.

02. **major**: `DESIGN-v2-A-protocol.md:1363-1366` (10.1, reports), "one that starts with a section
    name is refused `body`, as v1". The code does not refuse this for a report. `ReportForm`
    (`proto/forms.go:54-59`) checks only the 1500-byte cap and `Headline` (`forms.go:44-50`). The
    section-name check, `sectionOf(headline)`, exists only in `QuestionForm` (`forms.go:77-79`), and
    `TestReportForm` (`forms_test.go:52-61`) has no such case. The design-sync report row 40 put the
    question rule under both 10.1 and 10.2. **Fix**: delete "; one that starts with a section name
    is refused `body`, as v1" from 10.1. Keep it in 10.2 (`A:1399-1400`), which is correct.

03. **minor**: `DESIGN-v2-C-host.md:1467-1480` and `C:1486`, `C:1529-1531`; `DESIGN-v2.md:87-89`.
    A 2.2 (`A:197-199`) and D 3.5 (`D:218-222`) now say "C's validator refuses such an id at
    declaration (C 3.8)". C 3.8 says nothing about reserved ids. Its `team` comment (`C:1486`) gives
    only the grammar. C:1530 and DESIGN-v2.md:87-89 still say a migrated team keeps its v1 name as
    its id "when the name fits the grammar", which contradicts D 3.5's new "is not reserved"
    (`D:213`). Evidence: `paths/ids.go:82-91` refuses the ten ids in `ParseTeamID`.
    **Fix**: in C 3.8's `team` bullet, add "and is not one of A 2.2's reserved ids (`ParseTeamID`)".
    In C:1530 and DESIGN-v2.md:87-89, write "when the name is free as a v2 id (D 3.5: the grammar,
    not reserved, no v2 team has it)".

04. **minor**: `DESIGN-v2-D-migration.md:785` (5.4 M0 row). D 5.10 (`D:1108-1111`) says "M0
    checks it again (5.4)". M0's check only asks that the id "fits v2's team id grammar and is
    neither another v1 team's name nor an existing v2 id". Its refusal column says "the id fails
    the grammar or collides (3.5)". A reserved id passes M0 as written. **Fix**: add "is not
    reserved (3.5)" to both the check and the refusal cell.

05. **minor**: INT3 rows. `PLAN.md:75`, `DESIGN-v2.md:206-209` and `:359`, and `D:1373-1375`
    say only "a v1 name over 22 characters migrates under a short id". D 3.5 and 5.10 now also
    send a v1 team with a reserved name (`status`, `server`, `health`, ...) to a short id. The
    report's gate check relies on INT3 to cover this ("INT3's short id covers a v1 team with such a
    name"), but INT3's own text does not. INT3 is VISIBLE and goes to the coordinator, so this is
    not a gate breach. The coordinator should still hear of the wider case. **Fix**: widen INT3 in
    these four places to "a v1 name over 22 characters, or one of A 2.2's reserved ids".

06. **minor**: `DESIGN-v2-C-host.md:1222-1225`, "`DeleteDevice` ... is the fallback to the logout
    ... The take does not use it." Other texts present `DeleteDevice` as part of the take:

    - SPEC-matrix M45 (`SPEC-matrix.md:221-222`): "a take ends the previous holder's token by
      `Logout` or `DeleteDevice` (M52)".
    - M52 (`:260-262`): "C 3.7.3's take, with the H-C13 fallback".
    - The code's own docs say the same: `matrix/seat.go:26-27` and `:90-91` ("how a take ends a
      previous holder's token").

    **Fix**: make SPEC-matrix M45 and M52 say "the take ends it by `Logout` (C 3.7.3); `DeleteDevice`
    is the client's alternative, not used by the take". Record the two `seat.go` doc comments as
    owed to N2. Alternatively, change C if the take should use it.

07. **minor**: `DESIGN-v2-A-protocol.md:858` (7.1) and `:1022-1023` (7.5), "a human's malformed
    relation or mention is none, never a drop". A malformed edit relation is a drop. The 7.5 table's
    edit row (`A:1013`) and `DecodeHuman`/`decodeEdit` (`proto/content.go:511-512`, `:542-550`)
    drop an `m.replace` with a bad original ID or no `m.new_content` as `edit`. **Fix**: in both
    places write "a human's malformed reply, thread relation or mention is none, never a drop (an
    edit's is `edit`, 7.5)".

08. **minor**: `DESIGN-v2-A-protocol.md:1412` (10.2), "`asked_by` 0-16 user IDs of roster members
    (else `target`)". Only the sender checks this: `Composer.Question` (`proto/send.go:218-221`).
    The receiver's decode checks only the count and distinctness, with every string valid
    (`content.go:429`), and rule 03 indexes the question with no roster check (`verdict.go:266-280`).
    **Fix**: "`asked_by` 0-16 distinct user IDs; the sender refuses one that is not a roster member
    (`target`), and a receiver does not check membership".

09. **minor**: the ASCII-only fold vs D 5.1 (`A:214-217`; `D:514`). The report's gate note says
    v2's fold can give a handle different from v1's for a non-ASCII repository name. v1's
    `normalise_repo` uses Python `str.lower()` (`helpers/agent_bus/registry.py:70-75`); v2's
    `normalise` folds `A`-`Z` only (`proto/ids.go:198-211`). That note lives only in the subagent
    report. D 5.1 still says "The mapping is the identity". That holds for the existing handles, but
    a v2 seat in the same checkout derives a different `<repo>` prefix when the name holds a
    letter that `str.lower()` folds to ASCII, such as U+212A KELVIN SIGN or U+0130. The A 2.3 table
    row (`A:209`) also still says "normalised as P17", which is v1's rule. **Fix**: in D 5.1's
    agent-accounts row, add "a checkout whose repository name holds a non-ASCII letter derives a
    different `<repo>` in v2 (A 2.3); M0 names it". In A:209, write "normalised as P17 but
    folding ASCII case only (below)".

10. **minor**: `DESIGN-v2-A-protocol.md:247-249`, "(whichever homeserver HC-A6 chooses; ...)".
    HC-A6 chooses no homeserver. It recorded Tuwunel 1.9.4's service account as `@conduit`
    (`A:2089`). The code comment says `tuwunel` is held "until host check HC-A6 records Tuwunel's
    name" (`proto/ids.go:11-13`). `PLAN.md:206` (Task 2.3) still names only `conduit`. **Fix**:
    write "`conduit` (Tuwunel 1.9.4's service account, HC-A6, and v1's reservation) and `tuwunel`
    (held against a later pin that renames it) are reserved; the list is fixed in
    `teamchat/proto`". In PLAN.md:206, write "`conduit` and `tuwunel`".

11. **minor**: four prose lines added by the diff are over 100 characters:

    - `DESIGN-v2-A-protocol.md:454` (160): 4.3, "kicked from every room ... (00211 section 3; R10".
    - `A:529` (140): 5.1, "member's events drop `sender` ... (C kicks".
    - `DESIGN-v2-D-migration.md:292` (101): "The other derived names cannot collide ...".
    - `D:1111` (105): "team's own rolled-back move left is allowed). M0 checks it again ...".

    **Fix**: rewrap each paragraph to 100 characters.

12. **nit**: `DESIGN-v2-A-protocol.md:1866`, "`question: Q<n>` (no headline) is for an indexed question
    whose first line could not be read". proto's index never holds such a question. `count`
    refuses any body that fails `QuestionForm` (`proto/questions.go:164-168`), and `QuestionForm`
    needs a valid headline (`forms.go:73-79`). The bare form is only `Entry.questionLines`'
    branch for an empty `Headline` (`envelope.go:221-223`). **Fix**: "`question: Q<n>` (no
    headline) is the form for an entry given no headline; proto's index always has one".

13. **nit**: `DESIGN-v2-A-protocol.md:978-979`, "An admin notice's subject is a roster member,
    except for `removed`". `checkAdminNotice` also exempts `moved` (`proto/verdict.go:612`), which
    has no subject (`content.go:392-395`). **Fix**: "except for `removed` (and `moved`, which has
    no subject)".

14. **nit**: `DESIGN-v2-A-protocol.md:214-217`. The normalisation paragraph leaves out the two other
    steps of `NormaliseRepo` (`ids.go:180-189`): leading `-` and `_` are removed, and the result is
    cut to 48 characters. **Fix**: append "; a repository name then loses leading `-` and `_` and is
    cut to 48 characters".

15. **nit**: `DESIGN-v2-C-host.md:2379` (test row, "the roster event's `restore_epoch` raised") and
    `C:2653` (H-C15, "the raised `restore_epoch`") still say "raised". Both describe the
    plain-restore case, so this is harmless, but they are the remaining instances of the N1 class.
    **Fix (optional)**: "changed".

## Gate check (the report's section)

I agree with the report's judgement. OD-A7's grammar regexes are unchanged (`proto/ids.go:31-55`).
TID's stable, 22-character, fixed-at-creation id is untouched: the reserved list only refuses ten
values. DC16 is unchanged, and the take still logs out (`C:1206-1212`). DC1, DC5, DC15, OD-D1 and
OD-D5 are not touched by any change. Two consequences of the gate notes still need to reach the
tracked text: the INT3 widening (finding 5) and the handle-fold difference (finding 9).
