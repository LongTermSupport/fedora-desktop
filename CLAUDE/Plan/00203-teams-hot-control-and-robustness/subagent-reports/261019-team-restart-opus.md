# Team restart and resume by id: interim report (Opus)

Branch `feat/teams-restart`, from local `work/add-session-and-restart-fixes` (F44 97537444
already merged into it). Tip 97d30e43. Not pushed. Stopped at a natural break because the
context budget ran out; the handoff for the next developer is
`untracked/scratch/261019-a70c26e520cdeb67d-handoff.md` in this worktree.

## Renumbered

F44's lxc-checkout task holds 3.8, so this branch's tasks moved up: add a running session is
3.9, the recorded conversation 3.10, restore and team restart by id 3.11, the agent view off
3.12. PLAN.md, the spec header and a journal correction entry (`--ref 10:09`) say so. The
earlier journal entries were left as written.

## Task 3.10: each session's conversation recorded (done)

- The pingbus SessionStart hook writes `conversation.json` (v, session, session_id, source,
  written_ms; mode 0600, atomic) into the session's report directory. It does this on every
  SessionStart: startup, resume, clear and compact.
- `internal/conversation.For` reads the file only when the record's launch carried the bus
  (Fable's M6 B1-class point). It resolves the transcript by the recorded id, never by taking
  the newest one in the folder.
- Tests, failing first: `ConversationRecordTest` (7 tests) and the plugin contract
  (`HooksJsonTest`) in Python; `TestSessionDirHoldsTheReport` and the `conversation` package
  tests in Go. All now pass, and the full helper suite passes (4851 tests).
- CCY 3.99.12.13 and container 2.77.9 are placeholders. The image carries the kit.

## Task 3.11, part 1: restore resumes by id (done)

`registry.ResumeArgs` replaces `RestoreArgs`. `ccy-sessions restore` resumes each session with
`--resume <recorded id>`. A record with no recorded id is reported as a failure, keeps its
record, and nothing is started.

Still to do: delete the uncalled bash `ccy_registry_restore` and `ccy_registry_restore_args`
(see the handoff), `ccy-teams restart` itself, the lifecycle plugin's restart reason, Task
3.12, and meta-deploy.

## Owner decisions

- D8, D9 and D11 are as in PLAN.md.
- D10 changes `ccy-sessions restore` for existing records. Before: every record was started
  with `--continue`. After: each is started with `--resume <id>`. A `--no-bus` record, a
  plain-`cc` record, or one whose hook never ran is not started, and is named with
  `ccy --resume` as the way back by hand.
- New D12: the plugin's hooks run only while a seat is held, so a ccy session on no team
  would never record its conversation. Under D10 it would then not be restored after a
  reboot. Its SessionStart now runs `pingbus hook conversation`, which only writes the record;
  every other event still runs nothing. This is the one exception to the inert contract.
