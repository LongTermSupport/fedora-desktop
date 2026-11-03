# Fact check, Plan 00209, round 3 (diff of 2026-11-03)

49 claims: 39 verified, 7 refuted (in 6 findings: R1 covers two claims), 3 unverifiable-here.

## Scope, and a mismatch in the dispatch

The diff file named in the dispatch holds, in 1124 lines: three small hunks (PLAN.md, SPEC-n2b.md HV3,
SPEC-n3.md section 7) and the whole of SPEC-n3f.md (1054 added lines). It holds none of the N2 backup and
restore text. That text is in the staged merge: PLAN.md +9 lines (the n2g paragraph) and SPEC-n2g.md (817
lines). Both were checked: the diff as given in full, and the staged n2g paragraph plus the code seams
SPEC-n2g.md names (SM1, SM3, SM4, RG1, RS16, the RC ops, the unit names, the BK members). SPEC-n2g.md was not
checked line by line.

Method: current tree only (index and working tree of the staged merge), read-only. No Go, QA or test command
was run, so every "passes" claim is UNVERIFIABLE-HERE. SPEC-n3f.md says it was checked against the tree
before n3d and n3e; the tree now has both merged, so base-tree statements that moved are reported as drift,
and only the ones that would mislead a builder are REFUTED.

## REFUTED (most consequential first)

### R1. SPEC-n3f.md section 12.5: the "Old" text of B 2.2.4 row 20 is not the current row, and G7 lists exit sets that have grown (two claims)

- Spec: Old = `| 20 | the homeserver did not answer | unreachable | wait | tell a human if it persists; the link keeps retrying on its own |`; G7 says "14 {rate, server-rate, report-rate}, 20 {unreachable}".
- Tree: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-B-member.md:518` reads `the homeserver or the forge did not answer | unreachable, forge-down | wait | unreachable: queued, the link retries it; forge-down: not sent, send it again later; tell a human if it persists`. Row 14 (`:516`) holds `rate, server-rate, report-rate, forge-rate`. `src-go/internal/teamchat/member/exits/exits.go:96-102` has `CodeForgeRate` and `CodeForgeDown`; `exits_test.go:36,38` pin them in the sets of 14 and 20.
- Effect on the plan text: applying section 12.5 as written would delete `forge-down` from row 20 and its "Old" quote cannot match. Rebase the row on the current text (add `not-ready`, keep `forge-down`), and describe the sets of 14 and 20 with the forge codes.

### R2. SPEC-n3f.md sections 6.1 and 12.1: "the rendered role line equals the NOTICE text, byte for byte"

- Spec: the coordinator's role line becomes `in team <t> you are the coordinator. Human posts made ...`, "proto's `membership.go` renders it and the corpus pins it", and the n3f-e test compares the rendered line to that text byte for byte. The ready line and the `uncounted` line are given WITH the `teamchat: ` prefix.
- Tree: `src-go/internal/teamchat/proto/membership.go:142-155` (`roleLine`) renders `"teamchat: in team " + t.Team + " you are the coordinator."`; the corpus line is `membership_test.go:51`: `teamchat: in team otherteam you are the coordinator.` The NOTICE text in 6.1 and in the "New" of 12.1 has no `teamchat: ` prefix (A 13.3 also gives the clause without it).
- Effect: the byte-for-byte test as worded cannot pass. Either the NOTICE text carries the prefix like the other two lines, or the test must say it compares after the fixed prefix.

### R3. SPEC-n3f.md section 6 and QL1: the `more` and `timeout` records are used in ways G4 does not list

- Spec: "`more` (B 2.2.2's) closes a `questions` listing cut at 256 records", "the count left out"; a reminder is a `timeout` record with `"question":"Q<n>","verb":null,"ref":null`, and a ping's TIMEOUT has `question` null. G4 lists changes to `Question`, `Human`, `Room` and a new `unattached` kind only.
- Tree: B 2.2.2 (`DESIGN-v2-B-member.md:454-456`) defines `more` as ending a `recv` or `wait` that stopped at the caps, with the count still pending and `team` null because it counts across teams; `records.More` is `{team:null, pending}` (`member/records/records.go:262-272`). `records.Timeout` has `Verb string` (never null), `Ref *string` and no `question` field (`records.go:109-116`).
- Effect: G4 is incomplete. It needs `Timeout` gaining `question` and a nullable `verb`, and a decision on whether a per-team `questions` listing can end in the cross-team `more` (which names no team) or needs its own record.

### R4. SPEC-n3f.md G4 `records.go:193`, and section 0 "the file is `ln6.go`"

- Tree: `Question.ID` is `records.go:221` (struct at `:217`), not `:193`, which is now inside `Sent`'s marshaller. There is no `ln6.go`; `verbErr` is in `member/teamsync/verbs.go:36-52`.
- Effect: stale base-tree references; a builder on the merged tree should read `records.go:221` and `teamsync/verbs.go`. Low severity.

### R5. SPEC-n3f.md QX6: "64 of them alone would pass 3 MiB"

- 8000 bytes x 6 (escaping) = 48 000 per section; x 64 = 3 072 000 bytes = 2.93 MiB, under 3 MiB (3 145 728); it passes 3 MB decimal only. 192 entries x 48 000 = 9 216 000 would pass it. Every other sum in QX6 recomputes (V35).
- Effect: say "192 of them" or "about 3 MB". Low severity (rationale for the hash).

### R6. PLAN.md, the n2g paragraph: "`restore` stays refused in production (`restore.ErrUnproven`, n2g-D8, until H-C15)"

- Tree: `src-go/internal/teamchat/restore/unproven.go:3-5` says the refusal holds "until host checks H-C5 and H-C15 pass", message "host checks H-C5 and H-C15 have not passed (DC8)"; SPEC-n2g.md DC8 (line 41) and n2g-D8 (line 804) say "until H-C5 and H-C15 have passed"; `admin/wire/wire.go:130-133` gates on `m.restoreProven`. PLAN.md `:107` and `:166` also name both.
- Effect: change "until H-C15" to "until H-C5 and H-C15".

## UNVERIFIABLE-HERE

- **U1.** SPEC-n2b.md HV3: "its gosec G304 is the owner's to decide when it is built (decision 3 on pull request 124)". `src-go/internal/teamchat/declare/hostvars/hostvars.go:25-30` cites the same decision; the decision itself is not in the repository. Settled by the pull request's comments.
- **U2.** PLAN.md n2g paragraph: "RT1-RT4 pass against the pinned Tuwunel 1.9.4". The four tests exist (V19) but were not run. `subagent-reports/261022-n2g.md:56` says they pass.
- **U3.** SPEC-n3f.md E1 row and QX7: that a real Tuwunel returns an admin-redacted event with `unsigned.redacted_because` on both `/messages` directions, and that `GET /joined_rooms` and the restricted join behave as RP1 and RP4 say. Needs the `-tags tuwunel` run (spec Task 3.4).

## VERIFIED

### Diff hunks outside SPEC-n3f.md

- **V1.** PLAN.md status "landing on F44 is Plan 00245": `CLAUDE/Plan/00245-agent-bus-v2-landing-and-cross-network/PLAN.md:3`.
- **V2.** Task 6.3: "decision 5 on pull request 124, Plan 00245 Task 8.1": 00245 `PLAN.md:62-66`.
- **V3.** `lxccmd` beside the v1 owner of `lxc-ls`, `lxc-info`, `lxc-attach`: `src-go/qa/rules/table.go:89-93` (`lxcfreezePkg`, `lxccmdPkg`).
- **V4.** `teamchat/units` beside the v1 owner of `systemctl`: `table.go:83`.
- **V5.** A `dynamicNames` row for a run-time program inside a wrapper: `table.go:244-248` (`LXC.Guest`).
- **V6.** Phase 8 "Moved to Plan 00245 ... Tasks 8.1 to 8.7, pull request 124 ... Task 8.8": true, with one nit: 00245 `PLAN.md:14` says the pull request carries Tasks 8.1 to 8.7 and 8.9.
- **V7.** SPEC-n2b HV3: `hostvars.Read(data)` takes bytes, at most 1 MiB: `declare/hostvars/hostvars.go:30,60-63`.
- **V8.** HV3: no file reader is built: no `ReadFile` anywhere under `src-go/internal/teamchat/declare`; the package comment (`hostvars.go:25-29`) says the same.
- **V9.** SPEC-n3.md section 7: `SPEC-n3f.md` exists and answers each open point listed in SR1 (headline/body split n3f-D1, `report-rate` RP3, `--ask`/`--close` TH1 and n3f-D3, `--remind-after` AK1, `--all` QL1, the `question` fields section 6, `history --room questions` QR2, 22 on verify RP4). Note: SR2's "invite left pending while `human_text` is false" is still in SPEC-n3.md; SPEC-n3f section 12.7 says to delete it (an amendment still to be applied, not an error).

### The staged n2g paragraph in PLAN.md (`:367-375`)

- **V10.** Names: `SPEC-n2g.md` and `subagent-reports/261022-n2g.md` exist; `feat/bus-v2-n2g` is the branch the reports name (`261022-n2g.md:3`); it is gone, merged into `feat/bus-v2-n2` (merge commit 4ba6feb4a).
- **V11.** "C 3.12": `DESIGN-v2-C-host.md:2347` is "3.12 Backups and restore".
- **V12.** Package `backup` holds BT, BK, HB1 (`backup/take.go`, `tar.go`, `list.go:19-20` `States`). Nit: BV (the `backup` and `backups` verbs) is in `admin/backupverbs.go`, not `backup`.
- **V13.** Package `restore` holds RG (`guard.go`), RM (`merge.go`), RS and RO (`engine.go`), RE (`epoch.go`), the renames (`renames.go`, `fs.go:140` `RENAME_NOREPLACE`).
- **V14.** `admin` holds RC and the secure unit's child: `admin/restorechild.go`; the six ops `restore.secure`, `.swap`, `.export`, `.finish`, `.revert`, `.check` exist; the units are `paths/restore.go:58,62`.
- **V15.** Verbs `backup`, `backups`, `restore`: `admin/backupverbs.go`, `admin/restoreverb.go`.
- **V16.** Wiring: `admin/wire/wire.go`.
- **V17.** Opus review FIX-FIRST then MERGE: `261022-n2g-review.md:1` ("Verdict: FIX-FIRST"), `261022-n2g-review-2.md:1` ("Verdict: MERGE").
- **V18.** `backup --all` goes on past a down team: `admin/backupverbs.go:73-75`.
- **V19.** RT1-RT4 tests exist under `-tags tuwunel`: `backup/tuwunel_test.go:102` (RT1), `admin/tuwunel_test.go:353,375,397` (RT2-RT4); the pin is v1.9.4 (`files/usr/local/share/agent-bus/tuwunel.pin:9`).
- **V20.** `restore.ErrUnproven` is the production wiring's answer, n2g-D8: `restore/unproven.go`, `admin/wire/wire.go:130-133`, SPEC-n2g.md:804.
- **V21.** Units, timers and install wait for gate A, Task 2.5: PLAN.md `:283`, `:382`, `:391`, `:393`.
- **V22.** Nits 5-11: `261022-n2g-review-2.md` lists nits 9, 10, 11 and says nits 5-8 of round 1 stay.

### SPEC-n3f.md, against the merged tree

- **V23.** Seams that exist as named: `QuestionIndex` with `Feed`, `Attach`, `Attachment`, `OpenDuplicate`, `Next`, `Reminders`, `ReminderKey`; `IndexedQuestion.Unanswered` is an `int` (`proto/questions.go:30,55,82-148`); `Composer.Report`, `Question`, `Thread`, `Admit`; `ReportForm`, `QuestionForm`, `ThreadBody` (`forms.go:54,65,97`); `Outgoing.DuplicateKey` (`send.go:21`); `ThreadPost{N, Answers, Close, Ask}`; `Window{Times}` with `Count`, `Admit`, `Prune` (`limits.go:210-233`); `Admit` refuses the report window as `ReasonRate` with `report_per_hour` (`send.go:286-288`); `Entry.Questions`, `Question`, `Revises` (`envelope.go:49-60`); `VerifySide` and `Invite` with accept, reject, pending and a roster argument (`verify.go:49,164-205`); `SyncFilter(r, a)` takes the coordinator form from the roster alone (`filter.go:26-39`).
- **V24.** `questions.go:173` (the collapsed `Question:` section) and `questions.go:185` (`threadPost` ignores a post with no counted `n`): both exactly as cited.
- **V25.** `verdict.go:235` (the admin's redaction) and `verdict.go:260` (`unsigned.redacted_because` drops the event): as cited.
- **V26.** `member/teamsync/session.go`: `handle` judges the answer's invites before main's trust (`:482-489`); `take`'s `proto.Index, proto.Fault` case is empty (`:761-762`); every pending item is `proto.PurposeMain` (`:743`); `verbErr` maps `matrix.ErrRateLimited` to `CodeServerRate` (`verbs.go:45-46`).
- **V27.** `member/state/store.go`: `MaxStore = 12 * 256 << 10` (`:18`), `MaxSending = 32`, `capGateKeys = 96`, `maxNextBatch = 1024`; `TestStoreFullFitsTheFileCap` (`store_test.go:292`) uses `4096 + maxNextBatch + sum(count x (size+1))`. Recomputed from the caps in `store.go:23-81`: 2 996 016 + 5 120 = 3 001 136, as claimed; that rounds up to 12 x 256 KiB.
- **V28.** The store has none of n3f's keys yet (`outbox.reports`, `questions`, `reminded_ms`, `remind_late`, `outbox.questions_*`); `Outbox.LastSent` exists (`store.go:204`); `storeRooms` already lists the three purposes (`:105`).
- **V29.** `member/records`: `KindQuestion`, `KindRoom`, `KindMore`, `KindQueued`, `Sent{ID *string, Queued}`; `Question{team,n,state,id,asked_by,unanswered}` with `ID string`; `Human.Question *int64`; `Room{team,purpose,state}`.
- **V30.** `member/cli/run.go:53-56`: `unbuilt` now holds n3e's `hook`, `status-line` and n3f's `report`, `ask`, `thread`, `questions`; n3d's `reply`, `say`, `ping` are no longer in it.
- **V31.** `member/engine` has `send.go`, `sender.go`, `outbox.go`, `reply.go`, `timeout.go`, and `member/state/rostercache.go` exists.
- **V32.** The 12 secret shapes (`proto/secret.go`, 12 patterns, "v1's twelve secret shapes"); an event id is `$` + 43 characters = 46 bytes quoted (`ids.go:45`); `report_per_hour` upper bound 60 (`limits.go:37`); window 3 600 000 ms (`limits.go:88`); `maxUserID = 128` (`store.go`).
- **V33.** A 5.1: a coordinator with `human_text: false` is invalid, parked or not: `DESIGN-v2-A-protocol.md:530`.
- **V34.** A 4.2 and 12.4: the admin's redaction carries reason `secret`; 7.6 rule 02 drops an event with `unsigned.redacted_because`: `DESIGN-v2-A-protocol.md:428,1070,1683-1688`.
- **V35.** Every sum in QX6 and 12.6 recomputes: the question (4729), 192 x 5121 = 983 232, 1024 x 97, 64 x 257, the key total 1 101 056, `outbox.reports` 856, `remind_late` 1296, the four scalars 108, n3f's 1 103 464, the new sum 4 104 600, 16 x 256 KiB = 4 194 304; 2 x ceil(100 000/100) + 1 = 2001; an attachment (77) and an unattached entry (224).
- **V36.** Section 12 "Old" texts that match the design files word for word: A 13.3 role lines, A 6.5 numbering, A 10.2 `n` bullet, B 2.2.1 `ask` row, B 2.2.4 row 10, the B 2.3.2 cap sentence, B 2.14 "2 MiB at worst", B 2.6 step 10 `human_text`. (Row 20 is R1.) The B 2.3.2 form is still the pre-n3d 2 MiB text, as the spec allows.
- **V37.** IDs referenced resolve: SD1, SD3, SD4, LN2, LN5, LN6, LN8, CL2, CL3, CL6, DV2, DV4, DV6, PK2, PT1, SY1, SY3, SY5 to SY8, SY10 to SY12, SY14, OF1, TB4, CK1, UH1, SR1, SR2, n3b-D1, n3c-D8, n3-D2, n3-D5 are in `SPEC-n3.md`; OW1 to OW4 are in SPEC-n3f section 9; n3d-D1 to D34 are rows of `subagent-reports/261022-n3d-handoff.md:885+` (not of a SPEC file); `M5` is defined only by SPEC-n3f itself (G7).
- **V38.** SY3 fills "100 per page, at most 20" pages (`SPEC-n3.md:586`), consistent with QS7's test gap of 2100 events.
- **V39.** C 3.11.3's status record has a per-member `rooms` map with `reports`: `DESIGN-v2-C-host.md:2260-2262`.
