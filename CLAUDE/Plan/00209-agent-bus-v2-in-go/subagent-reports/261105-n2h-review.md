# Review of SPEC-n2h.md (N2 slice S1, review to MERGE)

Read-only review. Sources read: the spec in full; DESIGN-v2-C 3.2.5, 3.3, 3.4, 3.5.1 to 3.5.4,
3.6.1 to 3.6.5, 3.8, 3.11.3, 3.12.2; DESIGN-v2-A 4.2 and 12.4; DESIGN-v2-D 4.1 and 5.6; SPEC-n2a,
n2b, n2e, n2g (RM7, RC1, RG1-RG3); and the code in admin (verbs, run, class, exit, child, serve,
answerops, execchild), apply (files, apply, member), status, declare (gate, declare), matrix
(client, member, admin, fakehs), hostnet/network, health (refresh, files), backup (list, meta),
restore/merge, paths, teamlock.

Not re-checked (the author's marks stand unverified by me): DESIGN-v2-C 3.14 rows, CO4
`ChildViewer`, SD4-SD9 of the units spec, SPEC-matrix M49/M51 wording, SPEC-n2d/n2f/n2g other
than RM7/RC1/RG, `host.TeamSandbox`'s lines against the CK4 table, the D-Bus property types on a
real systemd, DESIGN-v2-D RK10 beyond its quoted sentence.

## Findings

### F1. BLOCKER. DC5 refuses the retry of an interrupted first apply (section 2, DC5, n2h-D7)

Evidence: C 3.5.2 step 7 starts the homeserver and step 11 writes the stamp last ("a stamp always
means fully applied"). A first apply that fails at step 8 to 10 (readiness, bound-address assert,
bootstrap) leaves the team's own `teamchat-server@<team>` listening on its port with no stamp. DC5
refuses exactly that state ("port <n> is already listening and the team is not applied", 70).
C 3.2.5 says the refusal is for a port "listening now and is not this team's own
`teamchat-server@<team>`"; the spec drops the "not its own" half because an unprivileged caller
cannot attribute the listener (hostnet `holder` fails on another account's fd directory: TRUE),
and justifies it by "a team with a stamp is not judged". It never considers the stamp-less team
whose own unit holds the port. The play's re-run after any such failure would be stopped at step 2
for ever, before `team apply` (which can judge it) runs.
Correction: either take the spec's own alternative (no listener check in `declare check`; the
collision shows at `team apply` step 2, as root), or refuse only when the team's own unit is not
active, reading the unit's `ActiveState` over the system bus as the caller (a read any user may
make; `units.Manager.State`). Add a red test: no stamp, port listening, own unit active: exit 0.

### F2. BLOCKER. Restore's merge, with a `locked` state, would let an old password return (n2h-D12, HU6, HU1)

Evidence: `restore/merge.go` `mergeHuman` is one exclusive switch: `!inCur`, then
`cur.State == HumanRemoved` (goes in `Lock`, nothing else), then `!inBak`, then
`cur.RevokedSeq > b` (`needs: password`, `RevokeHumans`). The spec says only that RM7 "must carry a
locked human to `lock`". If `locked` is added beside `removed` in that case, a locked human whose
password was reset after backup N (HU1 allows `human password` of a `locked` human) skips the
`revoked_seq > B` branch: the restored database keeps the pre-reset password and `human unlock`
afterwards brings it back, with no `needs: password` on the board. `locked` must be locked AND
judged by the remaining cases. `MergeFromBackup` (RM11) has no `Lock` list at all (it never locks a
`removed` or `locked` human of its base), so a base `locked` human's account may be unlocked in the
restored database.
Correction: state in the spec that `locked` falls through to the `!inBak` and `revoked_seq > B`
cases and goes in `Lock` as well; say what RM11 does for `locked` and `removed` base humans (lock
them); add merge tests for locked + `revoked_seq > B` and locked + only-in-current.

### F3. MAJOR. n2h-D12 lists the places `locked` touches too thinly, and the design is not silent but inconsistent (section 5 HU6, section 8 D12)

Does the design already provide a lock state? Partly, and inconsistently. C 3.6.1: "A human's `state`
is `enrolled` (not removed) or `removed`" (no `locked`); n2b-D7/AP1 and SR1 pin the same two.
C 3.12.2 step 5 ("every human `locked` is locked") and the merge bullet ("A human `locked` in the
current roster (`human lock`, or removed from the declaration, DC9)") use "locked" as an attribute,
possibly meaning "removed or locked by the verb"; SPEC-n2g RM7 says "when `human lock` is built, its
state must reach `lock` too" without defining it. So D12 is a grammar change to C 3.6.1, 3.11.3,
n2b AP1/AP4/AP7/D7, n2e SR1 and n2g RM7, and should be marked as an owner-visible grammar decision,
not only a default.
Places the spec does not list, all verified in code:

- `apply/files.go:322` human decode `oneOf(HumanEnrolled, HumanRemoved)`, and the `HumanEnrolled`
  comment "a human that is not removed";
- `apply/apply.go:231` `humans()` marks `HumanEnrolled && !declared` as `removed`: a locked human
  taken out of the declaration would stay `locked`, and DC9's "removing and re-adding the name ends
  a lock" (HU6) could not work. It must match `locked` too;
- `apply/apply.go:255` `human()`: `case acct.Locked || (known && removed)` unlocks every locked
  account today; it needs a `locked`-roster case that keeps the lock and one that locks an unlocked
  account, and `acct == nil` for a locked human;
- `apply/apply.go:297` `lockRemoved` (removed only) and a new convergence for `locked`;
- `apply/apply.go:431` `DeriveRoster` (`h.State == HumanEnrolled`);
- `health/view/view.go:223` passes the roster state into `status.Human`, and
  `status/status.go:61` `humanState = [enrolled, removed]` rejects `locked` (so does every reader of
  the record, ccy-teams' board included: the record's strict decode is a compatibility break);
- `restore/merge.go:142` and `MergeFromBackup` (F2); `restore.secure`'s `lock` list is fine.
  Tests that pin today's behaviour (will need a change, with a reason in the same commit):
  `apply/apply_test.go:317` (`TestAHumanRemovedIsKickedThenLockedAndAddedBackUnlocked`, and the
  `unlocked` line at 255-263), `apply/files_test.go:37,144-157`, `apply/apply_test.go:242,407,475, 500-525`, `health/view/view_test.go:259,351`, `restore/merge_test.go:124-129,290`, `status` tests of
  `humanState`. The spec should list these so S5 is not surprised.

### F4. MAJOR. HU9 (idempotence) contradicts HU6/HU7 crash completion

HU6 says a crash after the roster write "is completed by running the verb again"; HU7 leaves
account unlocked and roster `locked`. HU9 says a human "already in the state asked for" makes no
homeserver write and no roster write. If "already in the state" is judged by the roster alone, the
rerun of `lock` after the HU6 crash is a no-op and the account stays unlocked; judged by the account
alone, the rerun of `unlock` after the HU7 crash is a no-op and the roster stays `locked`.
Correction: HU9 is a no-op only when roster and account agree; otherwise the verb writes only the
side that differs. Add both crash reruns as tests (they are untestable as worded).

### F5. MAJOR. RD7 triggers its read-back on the wrong error set (section 6, RD7, n2h-D3)

Evidence: `matrix/client.go` `StatusError`: a 5xx is `ErrRefused` with `transient`; a 429 is
`ErrRateLimited`; `answerLost` is `ErrUnreachable && transient`; a 200 with an unusable body is
`ErrUnreachable` non-transient (`malformed()`), and a cancelled context is `ErrUnreachable` with a
cause. RD7 reads again only on "fails transiently (`ErrUnreachable`)". A 500/502 on the PUT may well
have landed and gets no read-back, ending as exit 1 "failed" instead of the "answer may be lost"
path; a malformed-200 (the redaction certainly landed) is lumped with others; after a cancelled
context the read-back needs a fresh bounded context, which the spec does not say. A 429 after the
client's own tries is not processed and is safe to report as 69 (the exit table already maps it).
Correction: read back after any `matrix.Error` with `Transient()` or the malformed-answer shape,
under its own bounded context; a 429 and a 4xx are not read back; give the class for each. Tests per
row.

### F6. MAJOR. RD10 under-reports where the text still is (RD10, RD11, n2h-D5, section 6 intro)

(a) `paths.RestoreLostFile` (`ctl/<team>/restore-<N>-lost.jsonl`, 0600, RC3 in SPEC-n2g) holds every
event after backup N in full, kept for `restore --lost`. A redaction after any past restore leaves
the secret there, and the verb's answer ("never names fewer than hold the text") does not name it.
The design (C 3.6.5, A 12.4) names only backups and migration exports, so this is a design gap the
spec inherits; it should be raised and `exports` (or a new key) should list it. The restore engine
refuses while `restore.json` exists (RG2), so the staged and `db.previous` copies are not a case.
(b) "naming by `sent_ms` is exact" is false: `backup.List` times are the meta file's whole seconds
times 1000 (`backup/meta.go` `metaTime`, `unixTimeRe`), and the stamp is the backup's own start
time, so a backup stamped just before the event may hold it. Under-report is possible within the
second and within the copy duration. Correction: compare with a slack (round `sent_ms` down to the
second and subtract a stated margin, or list every backup within it) and say so.
(c) The v1 export (RD11) holds v1's history, so it can hold a secret only if it was carried
(D 5.6); naming it always is the safe direction and matches the design, but "exact" should not be
claimed. (d) A redaction removes one event; Element edits and replies are separate events whose
`body` and `m.new_content` or quoted fallback carry the same text. RD12's stderr line should say the
edits and replies of that message must be redacted separately. (e) RocksDB keeps old SST versions
until compaction, so RD12's wording is right ("redacted in the live database") but section 6's
"leaves the live database" is stronger than the code can promise.

### F7. MAJOR. 78 for an undecodable file is not what the built verbs do; and DC and ST disagree (HV4, DC2, CK1, CK5, DC6, ST2/ST7)

Evidence: `ExitConfig` (78) is produced only through `configError` (owner-group file, pin, ctl
account, os-release), seam wiring and the backup seams (`grep configError` in admin). A
`team.json` that fails `DecodeTeamFile` (`readTeamFile`), an `approved.json` that fails
`FileApprovals.Load` (`bootstrap`, `approve`) and a roster that fails `DecodeRosterFile` all return
plain errors, which `ExitCodeOf` maps to 1. C 3.6.1 says "exit 78 naming the file", which the built
code does not do. So `declare check` (78 for `approved.json`) and `team apply` (1 for the same
file) differ, and the spec records only D6/G3 (the declaration), not this.
Same file, different verbs: DC6 makes an unreadable stamp 78, ST2 makes it a team problem (exit 1).
CK2 says "only a refusal before the first line is another code", but CK5 makes an undecodable
`units.json` 78 after CK3/CK4 have printed lines, contradicting CK2 and CK9 ("every other fact
still printed").
Correction: add a decision that file-decode failures in the new verbs are 78 through a typed config
error and list the departure beside G3 (or take 1 to match the built verbs); make CK5's
`units.json` a `FAIL` line; give DC6 and ST2 one rule for an unreadable stamp.

### F8. MAJOR. Human verbs omit what C 3.6.2/3.6.3 say they do (section 5, HU11, HV5)

C 3.6.2 says the rooms step runs "at the end of every apply and of every member, role and human
verb"; C 3.6.3 roll forward: "any later verb on that team first completes the pending operation".
HU11 says a pending roster is "completed first by the verb's lock rule"; the lock guard only refuses
a pending restore (RG2), roll-forward is `apply`'s `load`/`rollForward`. The spec neither includes
the rooms convergence nor records a departure; and says nothing on what a roll-forward adds to
`changed` or to the answer of `human password` (HV7 allows no notes).
Correction: name `RunHuman` as using `load`'s roll-forward and say its `changed` lines appear in
the answer; state whether the rooms step runs (it changes nothing when locked is treated as
enrolled, but it repairs drift and costs room reads) or record the departure as a decision.

### F9. MAJOR. ST4 shapes are not testable for the missing, unreadable and expired record (section 3)

`status.Shown` returns a zero `Record` when the read failed or the record is missing, so there is
no `name` or `origin`; ST4 gives the `team <team> "<name>" origin <origin> <health>` line no form for
this case (empty string? omitted?). It also drops the read error, so ST3's "its error on stderr"
needs the verb to keep it (the verb does, but say so). An expired record is printed with all rows
as if current, against SR5's "never as current": say whether rows carry a stale marker. A stamp that
cannot be read (ST2) is "a team problem" with no `what` text. Correction: give the exact line for
each of the four cases and the exact problem text for an unreadable stamp.

### F10. MAJOR. HU5: `Status.Refresh` is not what the spec says, and its failure ends the poll (section 5, HU5, HU12)

Evidence: `health.Tick.Refresh` runs the tick's whole `gather` (unit state, probes, the `team.view`
child over every room and human, backups, disk) and writes the record, with errors for "no stamp",
"another writer's record" (n2f-H2) and a dropped write. It does not "re-read that human's device
count". C 3.11.3's login flow polls every 2 s for 5 minutes: 150 full gathers, each starting the
child. And HV8 turns a refresh failure into exit 1 after the devices were read, which the poll may
read as `sudo -n` failure and end the flow.
Correction: state the cost and accept it, or add a narrower record rewrite for one human; say what
the poll must do on exit 1 with a valid document.

### F11. MAJOR. HU6 "a lock stops sign-in" is unproven for sessions already signed in; fakehs cannot test it (HU6, HU15)

`matrix/fakehs/admin_test.go:114` `TestALockedAccountsRequestIsUnmodelled`: a locked account's
whoami answers 500 and `CheckModelled` fails. So the fakehs row of HU15 can pin the admin flag only;
sign-in denial is the `tuwunel` row alone (RK10: unproven). The spec's tuwunel test checks only
login, not an existing device token, which is the lost-phone case; on Synapse a lock denies existing
tokens, on Tuwunel it is not recorded. Correction: add the existing-token probe to the tuwunel
test, say that S5's `lock` is gated on its result (RK10/P1), and decide whether `human lock`
also signs devices out (a password reset) if it does not.

### F12. MINOR. Password handling: the paths are sound but four gaps

Checked: argv has none; the roster and status record hold none (`status.Human` has a device count
only); `matrix.Error` never carries bodies; `execx`/`broke` echoes the child's stderr and the child
is the only author; `validCredential`'s "No error quotes the secret" precedent. Gaps:
(a) HU3 does not say what happens when the second roster write (clearing `needs`) fails after the
reset succeeded: the child answers a failure, the password is never printed, the old one is dead.
Harmless but undocumented; say it (HV8's "a password printed once is never lost" covers only the
status record); (b) HV8's guarantee does not hold if the stdout write fails (`answer()` returns 1
after the reset); say the rerun is the recovery; (c) HU13/HV7's "a lie" refusal must say its error
text never quotes the password (as `validCredential` does), and a test over the lie path's stderr;
(d) HU15's scan lists "every file of the fake tree, the status record and stderr" but not the
child's stderr capture (`ErrChild` text) or the recorded process transcript of `execx.Fake`.

### F13. MINOR. Claims about the code or the design that are false or imprecise

- DC4 says the stderr line is "the text of `declare.RefusedError`". `RefusedError.Error()` is
  multi-line (it prints each SECURITY line again and a "the owner approves with:" line); the
  quoted single line is not its text. Pin the line the verb builds.
- DC1 "exactly the keys": all four keys are optional (`object(nil, ...)` accepts `{}`); it is an
  allow-list, and DR1 says so.
- DC2 writes `FileApprovals{Path: Host.Root + ...}`; `Path` is a `func(TeamID) string`.
- CK7 "`boundAs`, exported": `boundAs(d Deps, unit, port, listen) error` returns an error only; the
  `<got> not <want>` text and the `info bound.addr` lines need the addresses, so it is a refactor.
- HV4 "decided before any seam is touched": built verbs call `e.system()` before the flags
  (`memberVerb`, `teamRemove`), so a wiring failure (78) wins over a usage error (64).
- HV2 lists `Systemd`, `Firewall` and the lock directory as `System` fields; they are in
  `System.Host` (`host.Deps`).
- HV6's holder-line verb `human`/`redact` departs from `memberVerb`, which passes the dotted op;
  `verbRe` allows dots, so `human.lock` is more informative.
- "anyone" (S2, S3) is `ClassOther`: root and `teamchat-ctl` get 77 for `declare check` and
  `status` (CA3, `run_test.go`). Say so, since the HV2 test asks for non-root callers.
- HU3 maps only `ErrUnreachable` to 69; `ExitCodeOf` also maps `ErrRateLimited` to 69 (HU14 does).

### F14. MINOR. DC1/D6: which code a malformed declaration should have

C 3.3's table has 70 "a rule said no" and 78 "configuration". A malformed declaration is the
owner's host_vars, closer to 78 than to 70, and 70 for both decode and SECURITY means the play can
only tell them apart by the SECURITY lines (DC4 accepts that). Both fail the play. If the
coordinator keeps 70, G3's alignment of `team apply`/`team approve` to 70 should be scheduled with
S2, since the spec creates the inconsistency itself.

### F15. MINOR. RD5 depends on an unverified field

Already flagged by the author. Add the fallback: if the `tuwunel` test shows a redacted event with
no `unsigned.redacted_because`, what the op treats as "already redacted" (for example `content` of
`{}` on a message), and that a second redact of a redacted event is harmless (a second redaction
event), which keeps RD7's rerun safe either way.

## Sections judged sound (one line each)

HV1 (rootRuns holds anyone verbs too), HV3 (stdout contract), HV5/HV8 order of work, HU2's
generation rule (uniform, unbiased, 190 bits; `rand.Text` is 26 base-32 characters, so the
departure is real), HU3's write-first order (the team lock spans the roster write and the reset, so
a backup cannot fall between them), HU4 (matches v1's logout, D13), HU13/CH1-CH3 (Answer has no
`Human`/`Redact` yet, `argKeys`/`validateFailure`/`validateOps` are the right seams), RD1-RD4, RD6,
RD8, RD9, RD13 (no status refresh), CK1, CK3, CK6, CK8, CK9, DC3, DC7, DC8, ST1, ST5, ST6, ST7 and G1,
G2, G3, G4 as stated.

## Exit-code table audit (item 8)

Every code used (0, 1, 64, 69, 70, 75, 77, 78) is in C 3.3's table. Uses that disagree across
verbs or with built behaviour: F7 (78 for an undecodable file vs 1 in built verbs; unreadable stamp
78 in DC6, team problem in ST2; CK5 78 mid-report vs CK2), F14 (declaration decode 70 vs 1 in
`team apply`/`team approve`), F13 (`ErrRateLimited` 69 not named in HU3; HV4 ordering of 64 and 78).

## Claims re-verified and found TRUE

- `rootRuns()` holds `element check|apply`; `buildTable` takes each verb's run from it; `unbuilt()`
  in `run_test.go` names a verb built by its presence in `rootRuns()`.
- `ExitCodeOf`: `*declare.RefusedError` is 70, `teamlock.ErrBusy` 75, `restore.ErrPending`/
  `ErrRefused` 70; `team apply`/`team approve` return an `DecodeInput` error through `failWith` and
  `ExitCodeOf` has no row for `declare.FieldError`, so 1 (D6's departure note is accurate).
- `parseFlags` gives 64 for a bad, missing-value, repeated, unknown flag and a stray argument.
- `Stamps.Stamped` (health) reads `team.json`; `hostnet.Network.Listeners` fails on the first
  process whose fd directory it may not read (DC5/D7 premise); `units.Manager` has `State`, `Kill`,
  `Start`, `Stop` only (D9).
- `status.Record` has no `coordinator_wanted` (G1); `status.Human` holds a device count, no device
  IDs; `status.Shown` yields the missing/unreadable/expired problems of ST3.
- `apply.human` unlocks every locked account of a declared human (D12's premise); `DeriveRoster`
  includes only `enrolled` declared humans; the roster decode allows `enrolled|removed` only.
- `declare.Decode` refuses unknown keys (`agent_bus_teams`, DC8); `FileApprovals.Load` caps at 256 KiB
  and answers nil for no file.
- `Client.Redact` makes one try and `GetEvent` exists; `fakehs` models `unsigned.redacted_because`;
  `apply.Admin` has no `Devices` yet (CH1); `ResetPassword` sends `logout_devices: true`.
- `paths.MigrationDir` exists; `v1-export` is a D 5.6 name and is not in `v1-names.txt`;
  `backup.List` returns `Entry{ID, TimeMS, Usable}` newest first, time 0 for unknown.
- `teamlock.verbRe` admits `human` and `redact`; `Take` takes the guard; RG2 refuses on
  `restore.json` with 70; `SeatWait` is 60 s.
- `Answer` has no `Human`/`Redact`; `validateFailure`, `validateOps`, `answerFits` are the places
  CH2 names; `childLie` exists.
