# N3 slices: what was built, and the reviews it passed

Moved out of `PLAN.md` (Tasks 3.2 and 3.3) so the plan stays within its size limit; the text is
unchanged. The tick of each task, and what is still missing, stay in `PLAN.md`.

## Task 3.2: the `teamchat` binary

- **n3a built** on `feat/bus-v2-n3a` (SPEC-n3 sections 1, 2, 9 and 10): `cmd/teamchat`, the
  CLI, the member files' codecs and store writer, the seat and run locks, the kit's layout
  and the guest operations. Coverage: bundle 93.1, cli 99.1, exits 98.9, guest 93.8, homefs
  93.5, kit 93.3, lock 93.3, records 100, state 99.7, reexec 92.3, paths 100, seat 91.0,
  health 91.9; `cmd/teamchat` 50.0 (its `main` only). Three Opus review rounds
  (`subagent-reports/261022-n3a-review*.md`): FIX-FIRST, FIX-FIRST, MERGE. Report and the
  design-text fixes it proposes: `subagent-reports/261022-n3a.md`.

- **n3c built** on `feat/bus-v2-n3c` and merged into `feat/bus-v2-n3` (SPEC-n3 section 4,
  decisions n3c-D1 to n3c-D24, W-D1 to W-D3): the keeper (`run`), the link, receiving, the
  client verbs (`wait`, `status`, `ledger`), the ends (EN1-EN4), the failpoints and the
  busfailpoints gate stage. Coverage: bundle 93.5, cli 99.1, client 91.5, ends 97.3, engine
  94.3, exits 98.9, guest 93.8, homefs 93.3, keeper 90.7, kit 93.3, link 91.1, lock 93.0,
  records 100, state 99.7, team 100, teamsync 94.5, wire 94.1, wiring 100, proto 98.3, matrix
  98.9, fakehs 93.3. Two Opus review rounds and a narrow check
  (`subagent-reports/261022-n3c-review*.md`, `261022-n3c-check.md`), then the qa-reviewer
  (`261022-n3c-qa-review.md`, FIX-BEFORE-MERGE). Its blocker, an e2e test failing under load,
  was a product race (a `back` posted after the run's `disconnected`), fixed with one
  lifecycle turn per team and rule `project_lifecycle_turn` (`261022-n3c-race.md`); its docs
  findings are fixed (`261022-n3c-docs.md`). A keeper test seen failing under load at the n3b
  merge read a log line before its turn ended: fixed with a turn barrier and rule
  `project_keeper_log_barrier` (`261023-n3c-owed-flake.md`). Guest `TestRegister` could not
  run twice: the op registry is its own type, and the failpoints test stage runs every test
  twice (`261023-n3-guestreg.md`). n3d and n3e are in review or being built; n3f is left.

- **n3d reviewed** on `feat/bus-v2-n3d` (the sends, the outbox and its recovery, TIMEOUTs,
  the forge check; rows n3d-D1 to D34): Opus reviews A and B (`261022-n3d-review-A.md`,
  `-B.md`), both fixed (`261022-n3d-fixa.md`, `-fixb.md`) and checked
  (`261023-n3d-narrow.md`); the qa-reviewer's findings (`261023-n3d-qa-review.md`) fixed
  (`261023-n3d-fixc.md`) and checked READY TO MERGE (`261023-n3d-fixc-narrow.md`). The store's `Version` is not raised for n3d's new keys: a store written by n3c or an
  earlier n3d part is refused whole rather than migrated, because no such store exists outside
  tests (review A n3; GATE A means none is deployed). Found here and fixed on
  `feat/bus-v2-n3-guestreg`: `guest`'s `TestRegister` could not run twice (`-count=2`).

- **n3e built in part**: n3e-1 to n3e-11, the agent seam (SPEC-n3 6.1) and the goroutine review
  are merged (`subagent-reports/261022-n3e-*.md`, `261023-n3e-*.md`). n3e-12's first half is
  built: `hook` and `status-line` are in `wiring.Verbs`, off `cli`'s `unbuilt` table, and the CL2
  test (`go test -tags teamchatrelease ./internal/teamchat/member/cli/`) now fails only on n3f's
  four verbs (`ask`, `thread`, `questions`, `report`). n3e-12's second half is built in
  part (`subagent-reports/261105-n3e-e2e-sonnet.md`): the wiring world runs a `--claude` session
  whose CMD is a fake Claude Code (`e2e_claudecode_test.go`: the plugin's hook commands and the
  settings' status line command run through `sh -c`, as Claude Code runs them, with a fakeinbox
  and a faketranscript), and four rows pass over the real keeper, link and verbs: delivery
  (DV1, DV3-DV5, HK1, PK1), a silent link (HK1's own recovery line), the status line through the
  kit's command (SL1, PK1) and the deterministic Stop block (HK2). Not covered by them: Busy
  mode, a rotated transcript, `missing` (180 s of quiet, the engine's fake-clock tests own it),
  membership notices, the 300 ms bound itself, the 10-minute repeats. Not built: the OF1 row
  with one team's server down (needs a second team and about 15 s of real time).

  n3e-13, PT1, is built in part (`subagent-reports/261105-n3e-pt1-sonnet.md`), in package `wiring`
  under `busfailpoints` (`pt1_model_test.go`, `pt1_run_test.go`, `pt1_model_unit_test.go`): a seed
  draws a schedule of posts, recv, wait, sends, a held, released or refusing inbox, homeserver
  restarts and a link SIGKILL, run over the real keeper and link with the fake Claude Code as CMD,
  drained, and judged by one pure function that has P1, P2, P4, P5, P6 and P11; the gate's tagged
  stage runs 22 fixed seeds, the 8 point rows and the redelivery row (about 60 s a count), and
  `-pt1.random=<n>` is the manual run; a failing seed prints its replay (`-pt1.seed=N`).
  `TestPT1Points` kills the link at the 8 points a one-team session reaches (judge-before-persist,
  auth, awaiting, user-line, hand-out, while-handed, charge, send) and checks the same. Not
  reached: P3 directly (a lost item is the observable), the removal and end points (their rows
  are `e2e_failpoints_test.go`'s), P7, P8-P10 and `link-after-missing` (180 s of quiet: no clock
  seam in the link), `link-between-store-writes` (needs two teams).

  n3e-13 slices 3 and 4 (`subagent-reports/261105-n3e-pt1b-sonnet.md`, `-pt1c-sonnet.md`) built the
  four more operations (`remove`, `end`, `die`, `recv-fail`), P4, P5 and P2 for returned items, and
  the runner that runs them (`pt1_end_test.go`, `pt1_redeliver_test.go`), so the manual run is
  usable on every seed. Each property was shown red on a planted defect in production code, put in,
  run and reverted: P11 (`Ack` leaves items pending), P5 (a second goodbye post with another
  transaction id, in `ends.Leave` and in `ends.send`), P4 (`Engine.Remove` returning at once, the team
  still running) and P2 (the `repeat:` line never written in `closingLines`; a `Return` that does
  not mark repeat). `-pt1.random=30` passed. One finding, open: seed 57 failed once in four
  concurrent full runs (P2, the returned item's first transcript copy carries no repeat line), and
  not in 25 unloaded replays; whether that is a production race or the harness sampling the
  transcript before a delivery lands is not decided (the slice 4 report; issue 156, where the
  reviewer's 180 isolated runs and 12 full-suite copies, none failing, are recorded, and the
  decision to keep the seed in the gate is stated with its one-line alternative).

- **n3f-a built in part** on `feat/bus-v2-n3f-a` (SPEC-n3f section 10's row n3f-a;
  `subagent-reports/261105-n3f-a-sonnet.md`). Built: G7 (exit 20's set gains `not-ready`, exit
  10's gains `open-limit`, row 20's meaning and next step as SPEC-n3f 12.5 words them; the
  skill's four class lines are unchanged because the classes are) and G6 (`proto.SeedWindow`,
  `Window.Oldest`, `Window.Within`, and `proto.DetailReportPerHour`, the detail by which a caller
  tells the report window's `rate` refusal, 14 `report-rate`, from the bucket's, 14 `rate`).
  Coverage of the touched packages: see the report. Not built: G1 (the question index), G2 (the
  store's keys and `MaxStore` at 4 MiB), G4 (records), G5 (the filter's argument), section 6's
  codecs. Nothing of n3f-b to n3f-e is built.

- **n3f-a, G4 and G5 built** on `feat/bus-v2-n3f-a-records` (from `feat/bus-v2-n3f-a`;
  `subagent-reports/261105-n3f-a-records-sonnet.md`).

  - G5: `proto.SyncFilter(r, a, questionsReady)` and `SyncFilterReAdd(r, a, questionsReady)`; the
    questions room is in the filter only when the roster names this member the coordinator and
    the argument is true. A coordinator with `false` is the worker's filter byte for byte (tested
    for both forms). `teamsync` passes `true` (nothing there yet knows whether the room is ready,
    QS3, which is n3f-b's).
  - G4 (`member/records`), each record's bytes pinned with its key order: `Question` gains
    `headline`, `re`, `asked_ms`, `remind_after_s`, `unanswered_more`, `redacted` (last), `id` and
    `asked_ms` nullable; the new kind `unattached`; `Human.Question` a nullable string (`Q<n>`,
    `none`, null) and `Human.Headline` after it; `Room.Check` and `Room.Uncounted`;
    `Timeout.Question` after `silent`, `Timeout.Verb` nullable. The encoders refuse a redacted
    question that holds anything but its number, a question label that is not null, `none` (a
    human's only) or A 2.2's `Q<n>`, and a headline with no question holding the post.
    `engine/render.go`'s timeout gives a verb only when the outbox holds the ping. The skill names
    `unattached` (a minimal line; n3f-e writes the section).
  - Coverage: records 100.0, proto 98.3, teamsync 95.3, engine 92.7, kit 93.3, matrix 98.9, cli 98.9.
  - Not done here: nothing sets `Human.Question`, `Human.Headline`, `Timeout.Question`,
    `Room.Check` or `Room.Uncounted` yet (the index, the reminders and the status rooms are
    n3f-b to n3f-d), so a questions-room human is still rendered with a null `question`; the
    encoder does not tie `question` to `room`, to leave that to the slice that builds the
    questions-room delivery. Findings against the spec's section 6 are in the report.

- **n3f-a, G1 in part** on `feat/bus-v2-n3f-a-index` (`subagent-reports/261105-n3f-a-index-sonnet.md`):
  `proto.QuestionIndex` now folds idempotently by event id (`Feed`, `Attach(event, human)`);
  attaches a human post whatever its delivery verdict and answers `AttachmentOf` read-only;
  tracks unattached posts and the agent plain replies that clear them (`Unattached`); keeps
  `asked_by`, `re`, `remind_after_s`, `asked_ms` and the `Question:` section as its SHA-256
  (`OpenDuplicate` hashes what it is given); `Unanswered` as ids (16 kept, a count past them,
  reset by an agent thread post); the caps of QX6 in the fold (64 open with the evicted counted,
  128 closed, 1024 posts, 64 unattached); redacted slots (`Slot`, `Next()` past them, held
  numbers as closed redacted entries); provisional own entries (`PlaceQuestion`, `PlaceThread`,
  `TakeDropped`); `State()` and `Copy()`; `Reminder.DueMs`. Not built yet: the strict codec of
  the state with the build record, `RedactedSlot` over the raw event.

- **n3f-a, the rest of G1** on `feat/bus-v2-n3f-a-codec` (from `feat/bus-v2-n3f-a-int`;
  `subagent-reports/261105-n3f-a-codec-sonnet.md`): `proto.QuestionsState` (the store's
  `questions` key: `index`, `token`, `last`, `build`) with `Encode(server)`,
  `DecodeQuestionsState(data, server)` (strict as `member/state`'s files: repeated keys and
  integers past 2^53 refused, unknown keys refused, a round trip that must equal the parsed
  value, then `Check(server)`; at most `MaxQuestionsStateBytes` 1 101 056), `IndexState.Check`
  and `LoadQuestionIndex(server, floor, state)`; the build record (`BuildRecord`: no record with
  an index, a build owed with none of the three and `after` set, a build under way with the
  three and `start` set; the engine's use of it is n3f-c's); `RedactedSlot(raw, server)`; and
  the two property tests (live equals restarted over 3 seeds of 2600 events, every cap fired;
  QX6's worst case, 1 001 559 bytes). `proto` 98.3% of statements. The fold's `State()` and
  `IndexedQuestion.clone()` now give `[]`, never `null`, for an empty list. Not built: the
  store (`member/state`, G2) that embeds `QuestionsState`; the engine's use of the build record.

- **n3f-a, G2 (the store's new keys)** on `feat/bus-v2-n3f-a-store` (from `feat/bus-v2-n3f-a-int`;
  `subagent-reports/261105-n3f-a-store-sonnet.md`): with this part n3f-a is built whole (G1, G2,
  G4, G5, G6, G7), pending its review. The seven keys of SPEC-n3f 12.6, strictly decoded and
  always present in an encoded store: in `outbox` after `last_sent`, `reports` (at most 60
  times), `questions_unechoed` (an event id or null), `questions_uncounted`
  (`{"count","said"}`, `said` at most `count`), `questions_handover`; at the top level after
  `outbox`, `questions` (`*proto.QuestionsState`, null for no index; the store runs the codec's
  `Check` with the team's server, `paths.ServerName`), `reminded_ms` and `remind_late` (at
  most 64 distinct numbers from 1). `MaxStore` is 16 x 256 KiB, 4 MiB; `TestStoreFullFitsTheFileCap`
  holds a store with every cap full at its widest (3 428 163 bytes, its `questions` key
  1 001 559 of at most 1 101 056), the caps' sum counted as written (4 110 165 bytes: every
  fixed field a row of `fixedWritten`, the questions room's lists and key among them) and the
  round-up to 4 MiB; the compact-form sums the test once pinned (3 001 136 and 4 104 600) went
  with n3d's store sizing (#164), which counts as written.
  `Version` stays 1: a store written before this change has none of the keys and is refused by
  the strict decoder (the engine's refused-store path), never read with them defaulted; a new
  store (`NewStore`) writes all seven. The item id grammar takes `~humans`, the reminder's key
  (`proto.ReminderKey`, A 10.5), which it refused: only on a TIMEOUT in the questions room, in
  pending and in the ledger. Coverage: `member/state` 99.8% of statements (`engine` 92.7,
  `teamsync` 95.2, `keeper` 91.2, `link` 92.3 pass unchanged under `-race`). Not built: what
  writes the keys (n3f-b, n3f-c), their drop at a demotion (n3f-c), the verbs, the skill.
  Open for the owner (the report's finding 1): `outbox.sending[].content` is sized in A's
  compact form but written HTML-escaped, so an entry can be written at up to about 98 KiB
  against its 20 KiB size.

- **n3d's caps measured as written** on `fix/store-size-as-written`
  (`261105-fix-storesize-opus-report.md`): the store's entry sizes, `MaxRosterCache` and
  `MaxSession` were derived from A's compact form while encoding/json writes `<`, `>`, `&`,
  U+2028 and U+2029 longer; the store now measures every entry through its one encoder, a send
  whose entry does not fit as written is refused 10 `size` at the verb, and the two file caps
  hold their largest values escaped. Not yet reviewed.

## Task 3.3: LXC members

- The guest side of B 2.13 (GU1-GU5, SA1) is built in n3a; the host side (C 3.9) is n3b's.
- **n3b built** on `feat/bus-v2-n3b` and merged into `feat/bus-v2-n3` (SPEC-n3 section 3,
  decisions n3b-D1 to n3b-D4 and n3-D21 to D30), as inert code: `teamchat-admin lxc apply`
  and `lxc restart` (C 3.9), the session unit as code (`host/lxc/unit`), the guest's facts,
  root and bus (`host/lxc/guest*`), `lxccmd` and the LXC record (`lxcstate`), the L3 probe in
  the health tick (n3b-D4), and the keeper's `--kind lxc --key`. An offline member never
  fails anything (n3b-D1, the owner's rule); a container holds any number of members, each
  (team, container, checkout, seat) (n3b-D2). Coverage: execx 94.8, admin 96.2, admin/wire
  91.5, apply 91.8, declare 96.0, health 93.8, host 93.3, host/lxc 91.9, guestbus 97.8,
  guestfacts 99.6, guestroot 95.9, lxccmd 97.8, lxcstate 96.5, unit 100, cli 98.9, guest
  93.7, keeper 90.8, paths 99.7, reexec 90.5. Reviews A, B and 2, the parts' review rounds
  and a check (`subagent-reports/261022-n3b-*.md`), then the qa-reviewer
  (`261022-n3b-qa-review.md`, FIX-BEFORE-MERGE), fixed in fix5 and fix6. No play task or unit
  is installed (gate A).
- The install build must run `go test -tags teamchatrelease ./internal/teamchat/member/cli/`,
  the CL2 gate, before it installs the binary.
- Tagged analyzer stages (n3-tagvet): `project-rules-tagged` runs the project rules over the
  packages built with each of the four tags; before them a finding in a tagged-only file passed the gate (`subagent-reports/261023-n3-tagvet.md`).
