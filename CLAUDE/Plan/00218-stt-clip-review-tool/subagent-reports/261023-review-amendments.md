# Plan 00218: review amendments (book repository agent's review against #112)

Plan only; nothing built.

## Changes

1. **Loop.** R13 rewritten: the clip plays when shown and repeats, a 0.6 s gap on the injected
   clock after each end-of-pass event, until the person leaves it. R14: loop on by default,
   `ctrl+l` toggles it for the run (no stored state), four toggle cases tested. Replay moved to
   new R32; R33 typing never touches playback and the text box takes keys from the first frame
   (a refused save keeps the loop); R34 each of save, inaudible, skip, previous, quit, ctrl+c
   ends the loop, tested in a pass and in the gap. New D13: the loop is the tool's, not mpv's
   `--loop-file` (no gap, not assertable through a fake). Key table, R10, R12, R20, Goals,
   Keyboard flow and task R-ranges (R1-R34) updated.
2. **R7** kept; note in R7 and D7 that clip sets are cut outside any checkout.
3. **Player (D2).** mpv preferred: one `--keep-open` process per clip over JSON IPC on an
   inherited socket pair (`execx.Cmd.ExtraFiles`, `--input-ipc-client=fd://3`; no socket file),
   pause/seek/`speed` with pitch correction, `eof-reached` as the end of a pass; sox then ffplay
   as restart-design fallbacks. Flow tests see only seam calls; each adapter has its own tests
   (mpv against a fake IPC peer). Task 1.1 probes mpv (package, plays to null output, has
   `--input-ipc-client`). Task 3.1: `play-speech-to-text.yml` installs the chosen player itself.
4. **Stopgap sidecars.** Confirmed: `fsx.WriteAtomic` names its temporary `<path>.tmp.<pid>`
   (src-go/internal/fsx/fsx.go), equal to the stopgap's SIDECAR.tmp.PID, and R21 trims leading
   and trailing white space. The contract and R21 now say so; R6/R21 treat its sidecars as the
   tool's own.

## Review

Fable re-check: READY, no majors (subagent-reports/261023-fable-recheck.md). Its minors were
fixed in a follow-up commit, except the "experiment session is changing" note, kept as asked.
