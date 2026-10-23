# Plan 00218 recheck (Fable), commit 5ed2e3339

Scope: the four amendments (loop, R7 note, D2 mpv, stopgap temp name and trim) in PLAN.md,
SPEC-stt-review.md and the 10:00 journal entry. Read-only review.

## Verdict: READY

No major finding. The minor findings below are wording and one unspecified edge; none
contradicts the design or blocks the build.

## Verified

- **R references.** R1-R34 are numbered without a gap; every cross-reference (R12→R33,
  R13→R34, R20→R34, R21↔contract, R24→R6, R33→R21/R24, R34→R20/R27/R13) resolves to a
  behaviour that says what the referrer claims. PLAN's "R1-R34" (spec link, Task 2.5) match
  the spec's last number. Task R-ranges: 2.3 (R4, R15-R20 + R13, R14, R32-R34 as flow tests),
  2.4 (R1, R2, R9-R14, R23, R25, R27, R28, R30, R32-R34) cover every new R; the overlap of
  R13/R14/R32-R34 between 2.3 and 2.4 is deliberate (seam vs screen) and harmless.
- **D references.** D2, D4, D5, D7, D13 cited in the spec exist in the table and say the same
  thing as the text (gap 0.6 s, `ctrl+l`, tool-owned loop, mpv first then sox then ffplay).
- **Key table vs text.** R30's table has `ctrl+l` = Loop on/off; R14, D13, the PLAN keyboard
  flow and the journal all use `ctrl+l`. No other key changed; no printable key added.
- **PLAN vs SPEC.** Goals, Design (Playback, Keyboard flow), D2/D4/D13, Tasks 1.1, 2.3, 2.4,
  3.1, 4.2 agree with R13-R18, R32-R34. Task 1.1's mpv probe (`--ao=null` to the end, exit 0;
  `--list-options` for `--input-ipc-client`; `rpm -qf`) matches what the adapter needs.
- **Testability.** Every new behaviour is stated as seam calls on a fake player plus an
  injected clock: the gap (no play before 0.6 s fake time, one play at 0.6 s), toggle (four
  cases plus default), pause-in-gap hold, replay-in-gap cancellation, typing leaves the call
  log unchanged, leaving in a pass and in the gap with the clock advanced. The mpv adapter's
  own behaviour (JSON sent, `eof-reached` turned into the end event, failed load) is against a
  fake IPC peer. Nothing needs a real player or wall time.
- **mpv claims.** `--keep-open=yes` pauses at end of file instead of exiting and is the
  documented condition under which the `eof-reached` property becomes true; it resets on a
  seek, so "seek 0 absolute, then unpause" for the next pass is right. `--input-ipc-client=fd://N`
  is documented: the FD (a `socketpair()` end or a pipe) is treated as an accepted IPC
  connection and no socket file is created. `speed` is a settable property; with
  `--audio-pitch-correction=yes` (mpv's default) it inserts scaletempo, pitch kept.
  `pause` (property) and `seek <s> absolute` are the right commands. `--no-config`
  `--no-terminal` `--no-video` are valid.
- **execx.** `Cmd.ExtraFiles []*os.File` exists (`src-go/internal/execx/execx.go:42-44`,
  "the child inherits from descriptor 3 on") and is passed through in `Run` (execx.go:134),
  the foreground path (foreground.go:56) and the `NewSession` path (session.go:51), so
  `fd://3` with `NewSession` is sound.
- **fsx.** `WriteAtomic` names its temporary `fmt.Sprintf("%s.tmp.%d", path, os.Getpid())`
  (`src-go/internal/fsx/fsx.go:16`): `<sidecar>.tmp.<pid>`, as the contract and R6/R24 say.
- **mpv provenance.** `playbooks/imports/optional/common/play-cli-tools.yml:175-187` installs
  `mpv` from Fedora's repos, desktop only, as PLAN says.
- **Names.** No personal name, host, path or container name appears in the three files. The
  only name is the `Owner` header field, which is the convention of every PLAN.md in the tree
  (117 plans), not an amendment.

## Minor findings

1. **R10 not updated for the loop.** R10 lists the playback line's states as "playing, paused
   or stopped"; R13 adds `waiting` (the gap) and R14 adds a `loop` indicator. R10 should name
   both so the screen test of R10 and the loop tests agree on one line.
2. **Seek and tempo in the gap or when stopped are unspecified.** R16 says seek "keeps playing
   if it was playing"; nothing says what `ctrl+left/right` or `ctrl+t` do during the gap or
   after a loop-off pass has stopped (hold the position for the next pass? start a pass?).
   One sentence in R16/R17 fixes it; the fake-player test is then obvious.
3. **R32 "the loop carries on from that pass"** should read "when the loop is on"; with the
   loop off (R14) a replay is a single pass. Wording only.
4. **Trim parity is asserted, not provable here.** R21 says the stopgap script "also applies"
   `strings.TrimSpace`; TrimSpace is Unicode white space, a bash/sed trim is usually ASCII
   `[[:space:]]`. The difference only matters for exotic spaces in typed text and the sidecar
   is read as a whole line anyway, so it is a note for the contract, not a defect.
5. **Narrative in the spec and D7.** "the experiment session is changing where it cuts them"
   is a point-in-time remark in a contract document; it belongs in the journal (where it
   already is). Keep "clip sets are cut outside any checkout; R7 is a guard" in the spec.
6. **Two plays install mpv.** D2 says `play-speech-to-text.yml` installs the chosen player
   itself so the tool does not depend on `play-cli-tools.yml`, while the Design says mpv is
   already there through `play-cli-tools.yml`. Both are true and dnf is idempotent, so this is
   not a contradiction, but Task 3.1 should name the duplicate on purpose (one line) so a
   reviewer does not read it as drift.
7. **Adapter detail for later, not a plan change.** With `--keep-open=yes` mpv sets its own
   `pause` property at end of file; the adapter must treat that as "ended", not as the person's
   pause, so the flow's pause state stays the tool's. The fake-peer test of R18/R15 should
   cover it. Likewise `eof-reached` needs `observe_property`, which the adapter test implies.

## Journal entry

The 10:00 entry matches the committed changes (R13, R14, R32-R34, D13, D2, Task 1.1, R7, the
temp name and trim). No name or path. Fine.
