Verdict: FIX-FIRST

# N2 slice n2a: adversarial review

Scope: `git diff feat/bus-v2-n1..HEAD` in `feat/bus-v2-n2a` (tip 169b65f5), judged against
SPEC-n2a.md, C 3.3, C 3.6.1 and 3.6.2 step 4, A 4 to 5.3 and 6.4, seams 13, 14 and 17, the gates
and the Go rules.

What I ran (from `src-go`):

- `go test -race -cover` over admin, rooms and cmd/teamchat-admin: all pass (admin 96.8%,
  rooms 98.4%).
- `go vet` with and without `-tags tuwunel`, and `golangci-lint run --build-tags tuwunel`: clean.
- With `FAKEHS_TUWUNEL_BIN` set to the pinned 1.9.4 binary and `-tags tuwunel`:
  `TestTuwunelConverge`, `TestTuwunel` (M44) and `TestStartAnswersVersions` all pass.

Gates: clean. Nothing in the diff is a play, task, unit or deployed file. The
`scripts/go-binary-suites.tsv` row (`teamchat-admin qa-go`) is only a QA routing row. No pool
password is handled or stored: the only passwords are random throwaway keys in the tagged test
harness. There is no `//nolint`, no discarded error and no wall clock in untagged tests.
`tuwunelhs` starts the server through `execx.OS`, and its one `os/exec` use is `LookPath`, which
GoProjectRules allows.

The admin-room fix (15180469) is right and complete. `candidate` checks the `m.room.create`
sender first, for every purpose, so it covers side rooms too. `read` checks the create sender
again for recorded and adopted rooms. The real-server round trip proves the fix: with the
server's admin room joined, the first run would otherwise adopt that room as main and then be
refused at `read`. What the fix leaves is the modelling (finding 4).

## Findings

01. **major**: `src-go/internal/teamchat/admin/child.go:272-274` (`validLine`), and the same
    test in `oneLine` at `child.go:123-131`.

    - **What is wrong.** The answer codec's "one line" check rejects only C0 and DEL. It lets
      through C1 controls (U+0080 to U+009F: U+0085 NEL, U+009B CSI) and U+2028 and U+2029.
    - **Why it matters.** CC6 says root decodes the answer strictly because it is "the one thing
      root parses that a less-privileged process wrote" (C 3.3, I-B3). It requires every
      `changed` line and the error message to be one line, and root prints these as
      `CHANGED<TAB><what>` lines that the play reads. Python's `str.splitlines()` splits on
      U+0085, U+2028, U+2029 and U+001C to U+001E. So a `teamchat-ctl` child can put a forged
      second line, or a C1 escape sequence, into root's output. The project already has the
      right rule in `proto/membership.go:154`, which refuses C0, DEL, C1, U+2028 and U+2029 so
      that no line can be split, and the boundary here is weaker than that rule.
    - **Fix.** Use the same predicate as `proto/membership.go:154`, ideally one shared helper,
      plus `Cf`, matching `paths.ParseDisplayName`. Apply it in `validLine` and `oneLine`. Add
      cases to `TestDecodeAnswerIsStrict` and to the `EncodeAnswer` refusals: `\u0085`,
      ` `, ` ` and `\u009b` in both a changed line and a message.

02. **minor**: `src-go/internal/teamchat/admin/child.go:104-121` and `child.go:199`.

    - **What is wrong.** Some bad requests get no answer at all. The error messages quote
      request text without a bound: `unknown key %s` quotes a key that can be up to 64 KiB.
      When that makes the message longer than `maxLine` (4096), `EncodeAnswer` refuses it and
      `childFailed` falls back to stderr and exit 1, with nothing on stdout.
    - **Why it matters.** CC3 says "A bad request is answered as a `failed` error (CC5)", and
      here it is not answered.
    - **Fix.** Bound the message in `oneLine` (cut at a rune boundary under `maxLine`, with a
      marker), or bound what `exactKeys` quotes. Add a test whose request has a 10 KiB unknown
      key and assert that a decodable `failed` answer comes back.

03. **minor**: `src-go/internal/teamchat/rooms/rooms.go:80-100`.

    - **What is wrong.** On any failure `Converge` returns `Result{}`. Changes that already
      happened are lost from the result: rooms created and recorded, kicks, and a roster event
      the server accepted.
    - **Why it matters.** C 3.6.2 says "Every change appends a `CHANGED` line". C 3.6.1 says the
      file's `rev` "is updated after the server accepted it". After an invite failure that
      follows a roster write, the caller cannot print what changed or record the rev. The
      roster-rev half is self-healing, because `NextRev` takes the max next time. The
      `CHANGED` half is not: the next run sees nothing to report.
    - **Fix.** Return the partial `Result` (`Changed` so far, `RosterRev` once its `PutState`
      succeeded) together with the error. Say so in RE9. Add a test with a fault on `/invite`
      that asserts the `Changed` prefix and the `RosterRev`.

04. **minor**: `src-go/internal/teamchat/rooms/rooms_test.go:336-361`,
    `matrix/fakehs/fakehs.go:96` (`TuwunelQuirks`), and `rooms_test.go:257` and `267`.

    - **What is wrong.** fakehs does not model the server's own admin room. The only unit proof
      of the fix uses a room that alice created. Two other tests assume the opposite of the
      real server: `TestConvergeAdoptsAfterACrash` asserts that the admin's joined rooms are
      exactly the unrecorded room plus the recorded ones, and that there are 3 after adoption.
    - **Why it matters.** RE2 and n2a-D7 rest on this server behaviour, and fakehs's purpose is
      to stand in for Tuwunel (SPEC M37, `TuwunelQuirks`). Every later fakehs test that counts
      or walks the admin's joined rooms (adoption, health, the C 3.6.2 step 2 check) will
      repeat the same wrong assumption. The real server caught it this time only because
      someone ran the tagged suite by hand, and no gate runs it.
    - **Fix.** Add a quirk under `TuwunelQuirks()`. With it, `AddUser(..., admin=true)` (and
      shared-secret registration with `admin: true`) joins the user to a server-user-created,
      unlinked admin room. Turn the alice-built test into one that runs on that quirk. Make the
      crash test count only rooms the admin created. Record the behaviour as a server answer in
      SPEC-matrix (M37) or in Task 2.5's "Tuwunel's answers recorded into fakehs".

05. **minor**: `src-go/internal/teamchat/rooms/rooms.go:216` and `rooms_test.go:313`.

    - **What is wrong.** Three conditions in RE2 have no test that fails when the condition is
      removed:

      - the side-room link's `team` must be this team;
      - the link must be admin-sent;
      - the room must be admin-created, for a side purpose.

      `TestConvergeIgnoresOtherTeamsAndPurposes` builds only an other-purpose room of the same
      team, so its name claims more than it proves.

    - **Why it matters.** Spec section 0 says "every test actually proves the expectation it
      claims".

    - **Fix.** Use the `editing` client to add three cases:

      - a reports-purpose link naming `otherteam` with this main;
      - a correct link whose `sender` is alice;
      - a correct admin link in a room whose create `sender` is alice.

      Each must not be adopted, so the engine creates the room instead.

06. **minor**: `src-go/internal/teamchat/rooms/tuwunel_test.go:59-66`.

    - **What is wrong.** The real-server first run checks only the rev and the recorder. It does
      not assert `first.Changed`.
    - **Why it matters.** n2a-D12 and RE3 claim that a fresh room already holds A's content
      (name, topic and power levels as the override). The fakehs test proves that only against
      fakehs's own model of `createRoom`, which does a shallow merge of the override and stores
      the topic in `m.topic` form. If Tuwunel merged the defaults differently, its first run
      would quietly add `state m.room.power_levels` or `state m.room.topic` lines. The
      second-run check would still pass, so D12's claim is unproven on the real server.
    - **Fix.** Assert `first.Changed` against the same list as `TestConvergeNewTeam`: three
      creates, `state teamchat.roster`, then the nine invites.

07. **minor**: `src-go/internal/teamchat/rooms/rooms.go:257-283` (`read`).

    - **What is wrong.** A recorded `RoomIDs` that names one room for two purposes, for example
      `Main == Reports` from a corrupt or hand-edited `rooms.json`, is not refused. Both specs'
      power levels and join rules are then written into the same room on every run, so the
      team never converges and nothing reports drift.
    - **Why it matters.** RE8 (idempotence) cannot hold for such input, and RE4's purpose is to
      refuse what the engine must not repair.
    - **Fix.** Before reading, refuse (as drift, step `read`) any non-empty ID that two
      purposes share, and add a test. Alternatively, record in the spec that the `rooms.json`
      reader of the later slice guarantees this.

08. **minor**: plan process (src-go/CLAUDE.md, "How a port is built").

    - **What is wrong.** The JOURNAL has no n2a entries, so the red and green of CA, CC and RE
      are not recorded. SPEC-n2a says each test was "written first and seen to fail", and
      src-go/CLAUDE.md requires that "(red and green recorded in the plan journal)".
    - **Fix.** Add a journal entry through `mkplan.bash --journal` with the red and green
      evidence per group, including the admin-room red seen against Tuwunel.

09. **nit**: `src-go/internal/teamchat/rooms/rooms.go:160-169`.

    - **What is wrong.** `set` ignores a purpose it does not know. If `proto.Desired` ever
      returns a fourth purpose, `find` loops for ever, creating a room on each pass.
    - **Fix.** Return an error from `set` for an unknown purpose, and fail the step on it.

10. **nit**: SPEC-n2a.md, RE3 (lines 111-114) and the `Converge` paragraph (lines 94-97).

    - **RE3.** It says "every other state event of the spec as initial state". The code also,
      correctly, leaves out the roster (A 5.2: "main is created with no roster"). Say "every
      other state event but the roster".
    - **Converge paragraph.** It says the engine answers "the roster `rev` now in the room",
      but `Result.RosterRev` is 0 when nothing was written, as the code's doc says. Make the
      spec say "the rev written, 0 when none".

11. **nit**: `src-go/internal/teamchat/admin/run.go:75-87`.

    - **What is wrong.** The usage text already ends in `\n`, and `fail` adds a second one, so
      stderr ends with a blank line.
    - **Fix.** Trim the last newline before calling `fail`.

12. **nit**: `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md`, Task 2.1.

    - **What is wrong.** Task 2.1 cites the report `subagent-reports/261021-n2a.md`, which does
      not exist yet. Only the handoff exists.
    - **Fix.** Write the report before merging. When you do, add the n2a-D7 admin-room fact to
      its list of design-text fixes for C 3.6.2 step 4.1. The handoff's list predates the fix.

## Checked and found right

- **CA1 to CA7.** The verb table matches C 3.3 exactly, checked both ways by
  `TestVerbTableIsCA1`. Two-word verbs are matched before one-word verbs, and flags belong to
  the verb. The caller class comes from the uid pair, and a mixed pair counts as anyone else.
  A missing ctl user gives no ctl class, and a lookup error is exit 78. A wrong class is exit
  77 with the exact text, for every verb and every class. An unbuilt verb is exit 1, after the
  class check. `version` refuses to answer without a valid digest. Stdout carries only the
  payload.
- **CC1 to CC6.** `ChildSpec` refuses a uid or gid of 0. The child refuses arguments, any extra
  or changed environment variable, a working directory other than `/`, and supplementary
  groups. The request is read strictly: at most 64 KiB, no repeated or unknown key, `v` 1, the
  op grammar, the team grammar and object args. `bundle` is refused. The child's exit code is
  never passed through: it is mapped by class.
- **RE1, RE2, RE4 to RE10, and D7, D8, D11, D13.** Specs are taken in A 5.2's order, with
  `Desired` asked again after each new room. Drift is refused before any write. Every kick of
  every room comes before any state write, and every state write before any invite. The roster
  is compared without `rev`, its rev is `NextRev(event, file)`, and `DecodeRoster` runs before
  anything is written. A converged team makes no write call. `ExitCodeOf` gives 70 for drift,
  69 for unreachable or rate-limited, and 1 otherwise. VerifyMain and VerifySide pass on fakehs
  and on Tuwunel 1.9.4. The role ordering of A 5.2 (demote, close, promote) is left to C
  3.6.4's two passes, which is right for this engine.
