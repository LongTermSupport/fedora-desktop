# Independent search: Class A (child exit code becomes tool exit status) and Class B (kind word of a member row)

Scope: first-party Go in src-go/ and src-go/qa, excluding vendor, qa/rules, qa/driver, qa/go-qa.toml, and tests. The dispatch did not contain the rule or its pattern; I searched from the class description alone. I did not open the detector's directories or run the detector.

Paths below are relative to src-go/internal/ unless stated.

Forms expected beforehand (A): `term.ExitCode(x.Code)` conversions; `return exit.Code` from int helpers; `stop{}` / `failure{code}` / `exitStatusError{code}` carrier structs; `128 + signal`; `os.Exit(int(...))`; `Leave(code)`. (B): constant `Kind: KindCcy`; `Kind(m.Kind())`; handle suffix parsing (LastIndex ".", Cut, regexp `\.podman$`, HasSuffix `.lxc`, "podman"/"host" literals); `Kind == KindCcy` used as "a seat of this checkout".

## Class A: a child's/helper's code reaches the shell as the tool's exit status

Documentation: teams/usage.txt line 294 documents only "64 usage, 130 cancelled, 1 refusal or failure", plus 78 for choose-teams with no terminal/consent (~208-210) and 75/78 for `session` changes (~266-269). It does not say declare.py / agent-bus / play codes pass through. sessions/usage.txt lines 77 and 111 document 0/1/64 only. vmtest/usage.txt 46-49 documents member-ssh (remote status) and freshness-status. SPEC-ccy-teams.md (CLAUDE/Plan/00191-go-cli-tooling) line 622 (backup: status is the exit status) and 713 (declare.py codes) document some passthrough. SPEC-vmtest.md line 425 says verify_response's exit is the command's exit.

Carries the hazard:
1. teams/choose/choose.go:224 (returned :227) `term.ExitCode(cmd.Code)`, agentbus.CommandError from `agent-bus seat choose`: the reported 78 case, also 69/70/75/77. Not documented for --teams / choose-seat. Confident. Found by text then reading. (:222,:256 return local const exitConfig=78, choose.go:28.)
2. teams/chooser/chooser.go:254-255 `failed()` returns `term.ExitCode(de.Code)` (declare.Error). Callers chooser.go:218,222,266, list.go:150, widen.go:109,118. Confident.
3. teams/ops/ops.go:229-233 `playExit` `term.ExitCode(ee.Code)` (ansible play status). Uses at ops.go:369,390 only compare with playlock.ExitBusy; other callers need checking. Unsure.
4. teams/ops/ops.go:245-259 `childExit` converts declare.Error, agentbus.CommandError and execx.ExitError codes; its comment says "a code the child chose (64, 77, 78) reaches the caller". Returned at ops/takedown.go:251,297,327,335,378 and via takeDownDeclared (:235-246); dropped at takedown.go:182,223 and ops.go:419. Not in usage.txt. Confident.
5. teams/create/wizard.go:172-174 `term.ExitCode(de.Code)`; :749 and :755 `&stop{term.ExitCode(de.Code)}` (ErrUsage/ErrInterrupted are handled separately at :743-746; refusal 78 and crash codes pass through). Confident.
6. teams/lxcadd/adder.go:213-215 `term.ExitCode(de.Code)`; :648, :654, :671 `&stop{term.ExitCode(de.Code)}`. Confident.
7. sessions/reboot.go:207-208 `return term.ExitCode(exit.Code)` (systemctl reboot status). sessions/usage.txt says 64/1 only. Confident.
8. open/launch.go:146 `term.ExitCode(128 + int(exit.Signal))` for a handler killed by a signal (failure.code, used open/app.go:128). Comment at :134 cites E25/E27/E29/E30 of SPEC-open.md; I did not read the spec. Unsure.
9. vmtest/member.go:272 `exitStatusError{code: term.ExitCode(code)}` from ssh status (guest.go:104-117 statusOf; returned vmtest/app.go:207-208). Documented in vmtest/usage.txt:48 as the remote command's status; undocumented: 124 (guest.go:119 timeout) and ssh's own 255. Unsure.
10. vmtest/verify.go:39-40 `exitStatusError{term.ExitCode(code)}` from the verify_response helper. Not in vmtest/usage.txt exit paragraph; SPEC-vmtest.md:425 documents it. Spec-documented only.

Not a hazard (own signal, constant, or classification only):
- vmtest/app.go:204 `128+in.Signal` (own signal, documented usage.txt:46); vmtest/freshness.go:299,350-365 (own codes); ghaccount/app.go:171 `term.ExitCode(se.ExitCode())` (SignalError, own signal, E27); cmd/pingbus-status-line/main.go:41,49 and pingbus/statusline/statusline.go:95,132 (own signal, S12; the inner child's code at statusline.go:262 does not become the status).
- teams/ops/playlock.go:41 `LockExit` (maps to 130/1), used wizard.go:907, adder.go:801.
- teams/flow/kit.go:36-49 `LeaveError`/`Leave`/`ExitCodeOf` is a carrier; every `flow.Leave` site passes constants or `p.Create.Run` (picker/offers.go:38,414, which is item 5's code).
- Classification only: execx/* (origin), agentbus/agentbus.go:76, declare/declare.go:59, ui/ui.go:53, wsi/app.go:295 (messages and exit 3 check prepare.go:269), wsipost/app.go:222 (message only :181), wsitoken/app.go:263, engine/engine.go:318, convert/app.go:208, ftpcamera/teardown.go:108, gitx/gitx.go:213, gitx/promptfetch.go:20, rclonerc/mounts.go:31, sudo/sudo.go:135, ghaccount/app.go:668,724, vmtest guest.go:239 / freshness.go:166-176 / run.go:399-404.
- ccylaunch/front.go:476 handOver execs bash (no code read). sessions/picker.go `leave` carries constants.
- every cmd/*/main.go `os.Exit(int(run(...)))` only forwards term.ExitCode; cmd/convert/main.go:21 likewise. qa/** : every `res.Code` is judged and mapped to the gate's own 0/1/2 (qa/all/gate.go:176-185, qa/changed/run.go:264-266, checks.go:123,425,428); qa/cmd/*/main.go os.Exit(run()) are own codes.

## Class B: kind word of a row / record

Handle-suffix parsers (places that read the type word from a handle):
- agentbus/parse.go:49-59 `Member.Kind()`: LastIndex ".", "podman" shown as "ccy". The reader named in the dispatch. Used at teams/board/snapshot.go:292.
- teams/roles/roles.go:286-300 `parseHandle`/`IsHandle`: second parser with its own list podman/host/lxc/docker/vm. Duplicate.
- teams/seats/seats.go:118 regexp `...\.podman$` (nextSeat), matches `.podman` by literal; host seats not counted. Duplicate.
- teams/lxcadd/adder.go:782 `HasSuffix(m.Handle, "+"+d.Host+".lxc")`. Duplicate.
- Related type words: teams/chooser/chooser.go:38-39,150 `typePodman`/`typeHost`; teams/hot/hot.go:50 `memberType="podman"`, hot/cc.go:24 `ccMemberType="host"`; pingbus/statusline/roles.go:30 `hostType`.

Assignment of a kind word to a row/record:
- teams/board/collect.go:516 `seatRow`: `Kind: KindCcy` constant for every seat row from `agent-bus seat list` in a checkout, whatever the handle's type. The defect site (host seat shown as ccy). Confident.
- teams/board/snapshot.go:292 `Kind(m.Kind())` (derived, correct); :303 `Kind: "lxc"` literal for a deferred LXC member with no handle; :268 KindHuman; collect.go:455 KindTeam; table.go:25-33 constants, table.go:105 prints `string(r.Kind)`.
- `kindLxc = board.Kind("lxc")` restated in teams/restart/restart.go:54, shutdown/shutdown.go:64, winddown/winddown.go:65.
- Session-record axis (ccy vs cc launcher), a different but related source of the words: teams/membership/membership.go:64-65, membership/desired.go:111, hot/hot.go:51-52,426, hot/cc.go:47, hot/list.go:377-383 `kindOf(container)` (empty container means cc), discover/discover.go:44,156,189,202, winddown/winddown.go:69, registry/registry.go:279, registry/restore.go:17, restore/verdict.go:87-88, sessions/restore.go:237, transcript/transcript.go:31-33, declare/lxc.go:222. Unsure whether in scope.

`Kind == KindCcy` used to mean "a seat of this checkout" (breaks if host seats get their own kind word), all under teams/:
- board/table.go:95; picker/addrunning.go:29 (no Dir test); picker/menu.go:132 (empty case), :169, :187; picker/sessions.go:429; seats/seats.go:209 (no Dir test), :241, :243; seats/role.go:113; restart/jobs.go:120; shutdown/shutdown.go:229; winddown/winddown.go:247.
Not this form: login/login.go:60,141,284 (KindHuman); KindTeam checks.

## What text search found that reading could not have checked
The complete set of `.Code`, `term.ExitCode(`, `os.Exit`, `KindCcy` and `kindLxc` sites, and the literal `.podman` / `.lxc` suffix compares, across both modules.

## What reading found that text search could not have
- Carrier types (Leave, stop, failure, exitStatusError) and which call sites feed them a child's code; picker/offers.go forwarding the wizard's code.
- That playExit/childExit results are sometimes dropped and sometimes returned.
- That pingbus, ghaccount, vmtest app.go:204 convert the tool's own signal, and ui/wsi/wsipost/engine only classify.
- That seatRow assigns KindCcy by constant instead of Member.Kind(); that roles.parseHandle is a second suffix parser unrelated by name to the board; that seats.go:209 and addrunning.go:29 use Kind alone as "a seat of this checkout".
- Comparing each site against usage.txt exit paragraphs.

## Searches run
`os.Exit(`, `term.ExitCode`, `ExitCode(`, `\.Code\b`, `ExitError|CommandError|declare.Error`, `128 ?\+`, `WaitStatus|Signaled|ProcessState`, `res.ExitCode`, `Exec(`, `code term.ExitCode` fields, `flow.Leave(`, `stop{`, `exitConfig`, `childExit(|playExit(|failed(`; `Kind(Ccy|...)`, `\.Kind\b`, `Kind(`, quoted podman/host/lxc/docker/vm words, `HasSuffix|LastIndex` on handles, `"(cc|ccy)"`, `typePodman|typeHost|memberType`.
Limits: tests/testdata excluded; SPEC files grepped, not read in full (item 8 undecided); bash/Python callers out of scope.
