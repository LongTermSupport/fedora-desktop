# Plan 00209: review of the design text batch (6a17bd4c8..8c18dee2c)

Verdict: **FIX-FIRST**. 3 medium, 7 low.

Scope: `git diff 6a17bd4c8..HEAD` on `feat/bus-v2-n3` (DESIGN-v2-A/B/C/D, FRAME, SPEC-n3.md). The
binding rule used is the FRAME's "Offline members never block". The sources were the offline audit
rows B3-B5 and B9-B13, n3a's "Decisions" section, n2g-a's restore.check rooms decision, and
n2g-spec-check item 1.

## What holds

- **B5 vocabulary.** No record status `stopped` or `missing` is left in A, B, C, D, FRAME or
  SPEC-n3. C 3.9.3, SPEC-n3 LX2/LX6/LX9 and FRAME 121-122 all use joined/offline/deferred/left, and
  a missing container's line says `container missing`. The B `missing` hits are delivery and
  desired-file codes, which are a different thing. The only remaining `stopped` record status is
  `RESEARCH-lxc-robustness.md:119`, which is historical research and outside this scope.
- **C 3.9.1, 3.9.2 step 02, 03 and 05, and the new mid-phase paragraph** match LX2, LX3 and LX6:
  missing, stopped, frozen or stopped-mid-phase is DEFERRED or SKIPPED, never a phase failure.
- **Audit rows.**
  - B3: C 3.11.3's exit rule is fixed (but see 3).
  - B4: C 3.6.1 and 3.6.4 are fixed (but see 2).
  - B10: M0's clause was moved from "fails" to `server-only`.
  - B11: A7 targets a live member, else `not run`.
  - B12: D 5.7 step 4 waits only on members whose process is up.
  - B13: M6 restarts only sessions running at M6; its "fails" column was updated. D 5.3:717 is
    about status-record disagreement, not liveness, so it is fine.
- **n3a's B 2.x and B11 proposals** were all applied faithfully: error key order (both examples),
  the 78 row, all-teams-fail, exit 64 with no team, session/keeper/report/status/desired.json text,
  gate.credit, 13-digit times, B11 mode 0600, and B 2.13's 78 codes. No SPEC-rule proposal leaked
  into the chapters.
- `--kind other` was added in B 2.1 and B 2.14.
- **The n2g step 08 start-block gap** is fixed in the resume list (C:2310-2313), with the same
  reasoning as the spec-check.
- **Cross-references are real**: R27 (A 6.6), P61 (A:1542, `proto.Bucket`), B17, C32, 3.6.1,
  3.9.3, 5.2 row 5, and 6.2's "moves when it is next started".
- Nothing install-specific was found.

## Findings

01. **Medium: `DESIGN-v2-A-protocol.md:304-306`, A 3.2 item 3.**

    - What: it invents a NOTICE in an old kit ("kit too old: apply its container"). An old kit
      cannot know a future major's `proto` key: by 3.1, unknown keys are ignored within a known
      major. It refuses its team only if the roster change is listed under `features`. FRAME 127-129
      rests on exactly that, but item 1 never says `proto` goes under `features`. "apply its
      container" is also the LXC remedy only, and wrong for a ccy or host member.
    - Fix: in item 1 (or 3), say the raise lists `proto` under the roster's `features`, so by 3.1 a
      returning old kit treats its team as untrusted, naming the feature to upgrade for. Drop the
      quoted NOTICE, or make it kind-neutral ("upgrade its kit").

02. **Medium: `DESIGN-v2-C-host.md:973` (3.6.4) and `:798` (3.6.1).**

    - What: these say the status record shows `coordinator_wanted` "as team state, not as a
      problem". But the 3.11.3 status record schema (`:2062-2086`) has no such field, and its only
      channel for a fix is `problems`, so the board has nothing to render. Nor can it be a member
      problem: the handle is not yet a member entry, and B2's decode refuses that.
    - Fix: add `"coordinator_wanted": null | {"handle": "<h>", "fix": "<what to run>"}` to the
      3.11.3 schema, with a bullet saying it never counts as a team problem. Point 3.6.4 at it.

03. **Medium: `DESIGN-v2-C-host.md:2114-2117` (3.11.3), with `:2084`, `:2096-2099` and 3.11.2 step 3
    (`:2023-2026`).**

    - What: the exit rule depends on "a team problem (one that names no member)". But the `problems`
      schema has no `member` key. The `launch-failed` and unposted-`disconnected` bullets, and the
      only-invited coordinator (VW5), are never said to carry `member`. The FRAME lists C 3.11.2 as
      a place that must honour the rule, and the diff did not touch it.
    - Fix:
      - schema: `{"what", "fix", "member": "<handle>"}`, with `member` optional and naming one of
        the record's member entries (SR1);
      - the 2096-2099 bullet says those problems carry `member`;
      - 3.11.2 step 3 says an invited-not-joined coordinator is a problem naming it.

04. **Low: `DESIGN-v2-C-host.md:109`, C32.**

    - What: "its member held back and recorded `offline`" is not quite right. Per 3.9.3, an entry
      with no known handle is recorded `deferred`.
    - Fix: "recorded `offline`, or `deferred` when it has no handle yet".

05. **Low: `DESIGN-v2-C-host.md:1696`, 3.9.2 step 03.**

    - What: it uses SPEC's elision `DEFERRED<TAB>..<TAB>`, but C spells the line out elsewhere
      (`:1802`, `:1839`).
    - Fix: write `DEFERRED<TAB><team><TAB><user>@<container><TAB>container stopped during the apply`.

06. **Low: `DESIGN-v2-C-host.md:1770-1773`, the mid-phase paragraph (and SPEC-n3.md:285-289, LX6,
    identically).**

    - What: one entry is both "that entry's `DEFERRED`" and "recorded by 3.9.3's split (`SKIPPED`,
      status `offline`)". It is unclear which output line prints.
    - Fix: say the output line is `DEFERRED ... container stopped during the apply`, and the record
      status is `offline` when a handle exists (no second `add`), else `deferred`.

07. **Low: `DESIGN-v2-C-host.md:2234-2235` and `:2276`, 3.12.2 steps 05 and 09.**

    - What, step 05: its first sentence still lists `restore.secure`'s stdin as the token and the
      roster only, while the added `:2250` gives it `rooms.json` too.
    - What, step 09: "today's rooms (the swapped-in ones)". After step 08 those are the tar's rooms,
      which step 05 had just contrasted with "today's".
    - Fix: add `rooms.json` to step 05's first sentence. In step 09, write "with the rooms now in the
      control state (the tar's, swapped in at step 8)".

08. **Low: `DESIGN-v2-B-member.md:2268`, the BD2 decision row.**

    - What: it still shows `teamchat run --plain -- bash` with no `--kind`, against n3a decision 07
      (`run` requires `--kind`).
    - Fix: write `--plain --kind other`.

09. **Low: `DESIGN-v2-B-member.md:2125-2129`, B 2.13.**

    - What: it says "exits 0, 64, 75 ... or 78" and then adds exit 1 `internal` for an I/O failure.
      B 2.2.4's row 1 (`:502`) says exit 1 is "a bug; never returned on purpose".
    - Fix: put 1 in the exit list, and reword row 1 as "internal error (a bug, or an I/O failure
      the verb cannot recover)".

10. **Low (nit): `DESIGN-v2-B-member.md:889-891`, B 2.4.1.**

    - What: the clause on a `--team` that differs from the ledger's team for the event named is a
      verb rule. It sits inside `run`'s no-team paragraph.
    - Fix: move it to 2.2.1, beside the verbs that take an event id, or to the 2.2.4 row for 64.
