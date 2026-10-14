# ccy-sessions performance: CPU sample and transcript polling (Fable review finding 4)

Scope: `src-go/internal/sessions`, `sessprobe`, `transcript`. Measured on one workstation
(22 threads); the figures are for comparison, not absolute.

## Before and after

| Measure                                         | Before                                                                      | After                                                                            |
| ----------------------------------------------- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Picker redraw, CPU column                       | 1.000 s (a hard sleep of `CCY_CPU_SAMPLE_SECONDS`, default 1 s, per redraw) | 11 to 55 microseconds (one /proc read, no sleep)                                 |
| Picker first draw                               | 1.000 s                                                                     | same cost as any redraw, CPU column reads `-`                                    |
| `--list` (one-off)                              | 1.000 s                                                                     | unchanged: still two samples SampleFor apart, still `N%`                         |
| Transcript poll, 50 MB file, per session        | 401 ms, 100 MB allocated (full `ReadFile`, split, decode every line)        | unchanged file: 6 microseconds, 640 B; one line appended: 39 microseconds, 22 KB |
| Full read (first look, or file replaced/shrunk) | 401 ms, 100 MB                                                              | 355 ms, 26 MB (streamed in 1 MiB chunks)                                         |

Benchmarks: `BenchmarkPollFullRead`, `BenchmarkPollCached`, `BenchmarkPollCachedUnchanged` in
`src-go/internal/transcript` (`bench_test.go`, `cache_test.go`). They generate about 50 MB of
4 KiB lines in `b.TempDir()`. Run: `go test ./internal/transcript -run xxx -bench Poll -benchmem`.
The "before" figure is the pre-change code measured with the same benchmark.

## 1. CPU sample (sessprobe)

`Probe.KeepSamples` (set by the picker, not by `--list`). With it, `Detail` takes one `/proc`
sample per call and measures against the previous call's sample (utime+stime and the
injectable clock's wall time), so a redraw never sleeps.

- First call, and the first call to see a session: nothing to measure against, so `-`.
- A call under `minRedrawGap` (250 ms) after the baseline repeats the last reading; CPU time
  counts in 10 ms ticks, so a shorter span is rounding.
- Pid reuse: `Sample.Starts` now carries field 22 (starttime). A pid whose start time differs
  from the baseline is another process and is left out. A pid the baseline did not have
  (born since, or in a session first seen now) is also left out: counting its lifetime ticks
  would claim CPU that was not used in the interval. `statTimes` requires field 22, so a
  malformed line is an error as before.
- The clock is `Probe.Now`; tests never read the wall clock.

`--list` keeps the old two-sample path, because `test-ccy-sessions-reboot.bash` requires a
`N%` in the CPU column of `--list`. The picker header line changed from "over the last second"
to "since the list last redrew (`-` until it has)"; the Go test pinning it and the Plan 00191
spec line are updated. No bash suite pins that text.

## 2. Transcripts (transcript)

`transcript.Cache` (new `cache.go`; `App.Transcripts`, wired in `wire()`, used by `set-going`
and `verify-restore --wait`). The package-level `ContextTokens`, `TookInputSince` and
`CompactedSince` are now one-shot wrappers over a fresh Cache, so there is one implementation
and the existing tests (including the bash-library comparison) cover both.

Per file it keeps: device+inode, size read so far (offset), the first 256 bytes, the
unterminated last line, and a fixed-size summary (last context size, the latest main-thread
user-input timestamp, the latest compaction timestamp, and the first error of each kind).
"Any entry at or after `since`" is "the latest entry is at or after `since`", so the summary
answers any `since`.

- Append: only the bytes after the offset are read.
- Shrunk, replaced (device/inode change) or rewritten in place (first 256 bytes changed): read
  again from byte 0.
- Partial trailing line: held in the state, not committed. As in the old full read, a final
  unterminated line that is whole JSON counts for the answer without being committed, so it is
  never counted twice.
- Memory: 1 MiB read chunks, a line over 64 MiB is an error, at most 64 files remembered
  (least recently asked about is dropped).
- Not detected: a rewrite that keeps the inode, the size or more, and the first 256 bytes.
  Claude only appends, so this is a documented limit rather than a case.
- Tests: append reads only appended bytes; unchanged file reads nothing; partial line then
  completion; whole-JSON unterminated line counted once; truncate; replace by rename with the
  same prefix; rewrite in place; sticky error cleared by a rewrite; vanished file forgotten;
  over-long line; bound on files; and agreement with a fresh full read at every cut of a
  sample for seven chunk sizes.

## 3. Repeated subprocess calls

A redraw (`rows`) runs `tmux list-panes`, `ps`, one `<engine> ps` and one `<engine> inspect`
per engine in use, and `tmux list-sessions`. These are all different commands; no identical
command runs twice. The poll paths (`hostView`, `liveSessions`) likewise. There was nothing to
dedupe, so the change is a pin: `TestOneDrawRunsNoCommandTwice` fails if a draw ever runs the
same argv twice.

## Verification

- `scripts/qa-go.bash`: GO-OK (coverage 94.5%, lint clean, project rules clean).
- `qa-changed.bash` green, including the parity suites `test-ccy-sessions-reboot.bash` (284),
  `test-ccy-sessions-take-over.bash` (74), `test-ccy-tmux-targets.bash` (20) and
  `test-ccy-restore-askpass.bash` (90). No suite assertion needed changing.
