# Phase 13: a record marked fresh-if-no-conversation starts fresh

The owner's decision of 2026-11-11 revises Phase 12 ask (1). This report covers how it was built.

## The mark

- Record line `fresh-if-no-conversation=yes`, optional, written only when set. Go field
  `registry.Record.FreshIfNoConversation`; bash `REC_FRESH` and `ccy_registry_write --fresh yes`.
  Any value other than `yes` (empty included) makes the record unreadable on both sides.
- **How provisioning sets it.** No play in this repository writes session records. A record
  for a session that has never run exists only because provisioning outside the launcher wrote
  it (issue 108's second case). So the interface is the record line itself, and docs/ccy.md
  documents it. There is no host_vars variable: none would have a record to act on, and the
  machine-wide switch the owner ruled out is the only shape a variable could take. If a play is
  ever to write records, it should call `ccy_registry_write --fresh yes`, or `Store.Write` with
  the field set.
- Every bash rewrite keeps the mark: the restore mark, the compose outcome, the SSH key and
  the network removal. The first interactive start of a session under the same name writes a
  new record without it. That is correct, because the session then records its conversation.

## Restore behaviour

- `planRestore`: the mark applies only when `conversation.For` fails with the new sentinel
  `conversation.ErrNotRecorded`, which means no `conversation.json`. Every other failure is
  still refused under the mark: a launch without the bus, a file that is not the hook's, or a
  recorded id with no transcript. A no-bus record never records a conversation, so it would
  start fresh on every boot and its conversation would be lost each time without anyone being
  told.
- The fresh start comes from `registry.FreshArgs`. `ResumeArgs` is now `FreshArgs` plus
  `--resume <id>`, so the two cannot drift apart. The command is the same
  `env CCY_SESSION_RESTORE=1 [CCY_RESTORE_SSH_PASSPHRASE_FILE=<path>] <launcher> ...` that a
  resume gets, so the askpass unlock works the same way.
- Manifest: `going=none detail=fresh resume=` (empty). set-going skips `none`, so nothing is
  typed into an empty conversation. verify-restore judges the entry by its screen alone. Using
  an existing going word means the bash manifest reader needed no change.
- `restore --check`: prints `RESTORABLE` on stdout, so the cycle's parser is unchanged, and
  says on stderr that the session will start fresh.
- An unmarked record with nothing recorded is refused as before (`not-started no-conversation`).
  A marked record with a recorded conversation resumes by its id.

## The bash ratchet

`lib/session-registry.bash` is on the bash-limits allowlist. The change kept it within its
record, and the record was tightened (`--update`) to 849 code lines and 870 statements. Option
parsing is now one small loop over `--compose` and `--fresh` (`printf -v`). Two `case` checks
became `if` lines. `ccy_registry_set_restore` no longer checks yes or no itself, because
`ccy_registry_write` refuses any other value and nothing has been written at that point. The
"is unchanged" wording of that message is gone, and no test pinned it.

## Tests (red first, then green)

The tests failed first, then passed after the change:

- sessions: `TestAMarkedRecordWithNoConversationIsStartedFreshUnderTheAskpass`,
  `TestADryRunSaysAMarkedRecordWouldStartFresh`,
  `TestRestoreCheckJudgesAMarkedNeverRunRecordRestorable`
- registry: `TestTheFreshMarkRoundTripsAndIsWrittenOnlyWhenSet`,
  `TestTheFreshMarkSurvivesEveryBashRewrite` (runs the real bash library),
  `TestFreshArgsAreTheResumeArgsWithoutTheResume`, and two new cases in
  `TestReadRejectsMalformedRecords`
- conversation: `TestOnlyAnAbsentHookRecordIsNotRecorded`

Before the change, the Go tests failed to compile (the field and the functions were missing).
The bash parity test failed on "unknown key 'fresh-if-no-conversation'".

## Not done

- Defence Before Fix does not apply: this is a decision change, not a defect.
- Task 13.4 (HOST) is the owner's test on a headless box.
