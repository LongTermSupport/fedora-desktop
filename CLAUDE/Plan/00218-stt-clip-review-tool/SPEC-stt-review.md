# SPEC: `stt-review`, the clip review queue (Plan 00218, #112)

Each behaviour below is numbered (R1, R2, ...) and is one or more tests. The numbers move into
the package doc of `internal/sttreview` when it is built, as `stt`'s S1-S19 did. The key names
are the defaults of decision D3; a test names the action, and the binding table is one test
of its own (R30), so changing a key changes one row.

## The file contract (documented for the clip cutter and the scorer)

- **Clip**: `NNN_<recording>_<HH-MM-SS>.ogg`. `NNN` is the queue order, three or more digits.
  `<recording>` is the recording the clip was cut from, and may itself hold `_`. `HH-MM-SS`
  is where the clip starts in that recording. The audio is Opus in Ogg, 48 kHz mono, one
  speech segment of at most 20 s plus 0.25 s either side.
- **Sidecar**: the clip's name without `.ogg`, plus `.human.txt`, beside the clip. UTF-8, one
  line: what the person typed, with leading and trailing white space removed, then `\n`. It
  is written through a temporary named `<sidecar>.tmp.<pid>` (the sidecar's whole name, then
  `.tmp.` and the writer's process id) and moved into place. The stopgap script in the book
  repository writes the same temporary name and trims the same way, so R6 and R21 treat its
  sidecars exactly as the tool's own: a finished sidecar is done whichever wrote it, and a
  leftover temporary of either is a crash temporary.
- **Inaudible marker**: the sidecar text `[inaudible]` exactly. A sidecar holding it is done.
- **The sidecar is the only state.** A clip with a sidecar is done; deleting the sidecar
  queues the clip again. There is no other file, cache or database.

## Command line

- **R1.** `stt-review FOLDER` reviews FOLDER. `stt-review` with no FOLDER asks for one on a
  path line (`internal/ui/pathinput`), so the panel can launch it with no argument. A folder
  typed on the path line that R3, R6, R7 or R8 refuses is said on that screen with the reason,
  and the line asks again (InteractiveScripts.md, friendly recovery), so the panel's terminal
  window does not close before the message is read. Only a FOLDER given on the command line
  exits with the codes below.
- **R2.** An unknown flag, or more than one FOLDER, is exit 64 and runs nothing; `-h`/`--help`
  prints the usage and the file contract, exit 0.
- **R3.** A FOLDER that is missing or not a directory is exit 1, naming it.
- **R4.** A player program (decision D2: `mpv`, or a fallback's: `play` and `sox`, or
  `ffplay`) not found on PATH
  is exit 1 before any screen, with the line
  `stt-review: <program> is not installed; run play-speech-to-text.yml`.

## The folder

- **R5.** A clip is a regular file whose name matches the clip pattern. Clips are ordered by
  `NNN` as a number, then by name.
- **R6.** The folder holds only clips and their sidecars, as the contract says. Any other
  entry is a failure before any screen, exit 1, every such entry named: a subdirectory, a file
  that is neither a clip nor a sidecar, and a sidecar with no clip beside it. Nothing is
  silently ignored (decision D6). The one exception is the tool's own: a leftover sidecar
  temporary (`<sidecar>.tmp.<pid>`, from a write cut short by a crash) is removed at start and
  the removal is said on stderr, since it never held a finished answer and refusing on it
  would lock the person out. One run per folder at a time is assumed: a second run's live
  temporary removed under it makes that run's save fail loudly, never write a half file.
- **R7.** A FOLDER inside a git work tree (it, or a directory above it, holds `.git`) is exit 1:
  `clips are private speech and must not sit in a repository: <folder>` (decision D7). It is
  found by walking up the directories, not by running git. Clip sets are to be cut outside
  any checkout; the experiment session is changing where it cuts them, so this refusal stays
  as a guard, not a workflow step.
- **R8.** The queue is every clip without a sidecar, in clip order. A folder whose clips all
  have sidecars says `nothing to review: N clips, all done` on stderr and exits 0. A folder
  with no clips says `no clips in <folder>` and exits 1.

## The screen

- **R9.** The heading is the frame's breadcrumb, `Speech to text › Clip review › <folder name>`. The context line holds: the clip's place in this session's queue (`3 of 12`), how
  many clips in the folder still have no sidecar, and the clip's file name.
- **R10.** Under the heading: the playback line (playing, paused, waiting in the loop's gap or
  stopped; `loop` while the loop is on; position and length to a tenth of a second; the tempo
  when it is not normal), then the text box, then
  the footer of every action with its key.
- **R11.** **No transcript is shown.** The screen draws nothing from any file except the
  folder's entry names and, for a clip this session already saved, the text the person
  typed (R21). A test fills the folder's sidecars of other clips with a sentinel word and
  asserts it never appears in any rendered frame.
- **R12.** The text box starts empty for a clip with no sidecar and has the focus from the
  start, so the person can type from the moment the clip starts (R33). It is one line,
  wrapped to the width, never truncated. Typed or pasted text holding a
  control character or a bidirectional control is refused whole and the refusal is said (as
  `pathinput` does).

## Playback

- **R13.** A clip plays from its start when it is shown (decision D5), and repeats (decision
  D13): when a pass reaches the end (the seam's end event), the tool waits the gap, 0.6 s on
  the injected clock, then plays the clip again from its start at the current tempo, and so
  on until the person leaves the clip (R34). Tests: after the fake player's end event, no
  play is asked for before 0.6 s of fake time and one play from offset 0 is asked for at
  0.6 s; three end events give three further passes; the playback line says `waiting` in the
  gap. The tool owns the loop for every player, so it is the same under mpv, sox and ffplay.
- **R14.** The loop is on when the tool starts. Loop (`ctrl+l`) toggles it, and the setting
  carries to the next clip until toggled back, for this run only (nothing is stored; the
  sidecar is the only state). The playback line shows `loop` while it is on. Turned off
  during a pass, the pass plays to its end and playback stops there; turned off in the gap,
  the pending pass is cancelled. Turned on while stopped at the end, the next pass starts
  after the gap. Tests for each of these four, plus the default.
- **R15.** Pause and resume (`ctrl+space`) toggles, keeping the position. Paused in the gap,
  the pending pass is held; resume starts it at once. Through the `Player` seam, pause,
  resume, seek and the tempo change are each one call: the mpv adapter sends the command to
  the running player over its IPC (`pause`, `seek <s> absolute`, `speed`); the sox and
  ffplay adapters, whose players cannot pause or seek, stop the player and start it again
  at the position, at the tempo. The flow tests see only the seam calls, so they are the
  same for every adapter; each adapter has its own tests (mpv: the JSON commands sent to a
  fake IPC peer; sox and ffplay: the argument lists).
- **R16.** Back and forward (`ctrl+left`, `ctrl+right`) move 3 s (decision D4), clamped to the
  start and the end, and keep playing if it was playing.
- **R17.** Slower (`ctrl+t`) toggles the tempo between 1.0 and 0.75, pitch kept (mpv `speed`
  with `--audio-pitch-correction=yes`; sox `tempo -s 0.75`, its speech setting; ffplay
  `atempo`), from the current position; the tempo carries to the
  next clip until toggled back.
- **R18.** The position is counted from an injected clock (start offset plus elapsed wall time
  times tempo, in clip seconds) and is clamped at the clip's length (a test). It is an
  estimate: the player's start-up and `tempo` buffering are not counted, which a tenth of a
  second display tolerates. The length is read once per clip in Go, from the Ogg stream
  itself (the last page's granule position minus the Opus header's pre-skip, at 48 kHz), so
  it needs no external tool; a clip whose stream is not Ogg Opus is R19's unplayable clip.
  The end of a pass is an event, never a timed guess: mpv's `eof-reached` property, observed
  over the IPC (the player is run with `--keep-open=yes`, so it stays for the next pass), or
  the sox or ffplay process's exit. The mpv adapter's IPC is a socket pair whose one end the
  child inherits (`execx.Cmd.ExtraFiles`, `--input-ipc-client=fd://3`), so no socket file is
  made, in FOLDER or anywhere (R31). The sox adapter names the type before the file (`-t opus`),
  since sox picks its Vorbis handler for a `.ogg` name.
- **R19.** A clip the player cannot play (the player exits non-zero, mpv reports the file
  failed to load, or its length cannot be
  read from the stream, R18) is said on the screen
  with the player's stderr. The person may still type, mark inaudible or skip it; the end
  screen lists it (R25). This is an interactive recoverable state, not a skipped error. R11's
  sentinel test covers this frame too.
- **R20.** Leaving a clip (save, inaudible, skip, previous, quit) stops its playback first,
  a pending pass included (R34).
  No player process outlives the tool: the player is run with `execx.Cmd.NewSession` (its own
  session, the whole group killed when its context ends), under `execx.SignalContext`, so
  every exit path ends it, SIGTERM and SIGHUP (the terminal window closed) included.

## Saving

- **R21.** Save (`enter`) writes the sidecar, then shows the next clip. The text is trimmed of
  leading and trailing white space (Go's `strings.TrimSpace`, the contract's rule; the book
  repository's stopgap script trims too, and its exact white-space set is not proven equal,
  which does not matter to R6 or R8 since only a sidecar's existence is read);
  empty or white-space-only text is refused with `type what you hear, or mark inaudible (ctrl+x), or skip (pgdown)`, and nothing is written.
- **R22.** Inaudible (`ctrl+x`) writes the sidecar `[inaudible]\n`, then shows the next clip,
  whatever the text box holds.
- **R23.** Skip (`pgdown`) writes nothing and shows the next clip. The clip stays queued for
  the next run.
- **R24.** Every sidecar write is atomic: a temporary beside it, then moved into place, mode
  0600\. A new sidecar is put in place without replacing, by a new shared primitive
  `fsx.WriteNew` (a hard link of the temporary, then the temporary removed; `os.ErrExist` when
  the name is taken), beside `fsx.WriteAtomic`, which replaces. Both name their temporary
  `<path>.tmp.<pid>`, so R6's crash-temporary clean-up covers either: if a sidecar appeared since the queue was read (another run, or a hand
  edit), the save is refused, the screen says so, and the person moves on with skip. Only a
  re-save of a clip this session already saved (R26) replaces its sidecar.
- **R25.** After the last clip, the end screen counts this session's saved, inaudible and
  skipped clips, the clips that would not play (named), and the clips the folder still has
  without a sidecar (read again). `r` goes round again over a fresh queue (the folder read
  again, so skipped clips come back); `q` quits, exit 0. Both are `key.Binding`s drawn in that
  screen's footer.

## Moving back and quitting

- **R26.** Previous (`pgup`) shows the clip before this one in this session. If this session
  saved it, the text box holds the saved text (the person's own, not a transcript), and save
  or inaudible replaces that sidecar atomically. On the first clip it says `no previous clip`.
- **R27.** Quit (`esc`) stops playback and exits 0, writing nothing for the clip on screen. If
  the text box holds unsaved text, it first asks `discard what you typed? (y/n)` on the
  screen (the text box is not on that screen, so `y` and `n` are its footer's `key.Binding`s);
  any answer but `y` returns to the clip.
- **R28.** `ctrl+c` exits at once with exit 130 (`term.ExitCancelled`), writing nothing for the
  clip on screen, playback stopped (R20).
- **R29.** Quitting part way and starting again queues exactly the clips with no sidecar
  (R8): a saved or inaudible clip never returns; a skipped one does.

## Keys and privacy

- **R30.** The bindings, each drawn in the footer with its help (InteractiveScripts.md rule 14,
  the Bubble Tea `key.Binding` rule of GoProjectRules.md):

  | Action                 | Key                        |
  | ---------------------- | -------------------------- |
  | Save and next          | `enter`                    |
  | Replay from the start  | `ctrl+r`                   |
  | Loop on / off          | `ctrl+l`                   |
  | Pause / resume         | `ctrl+space`               |
  | Back 3 s / forward 3 s | `ctrl+left` / `ctrl+right` |
  | Slower / normal        | `ctrl+t`                   |
  | Inaudible              | `ctrl+x`                   |
  | Skip                   | `pgdown`                   |
  | Previous clip          | `pgup`                     |
  | Quit                   | `esc`                      |

  On the review screen no action is a printable key, because the text box has the focus
  throughout. `ctrl+space` is also a common input-method toggle; if it does not reach the tool
  on the owner's desktop (Task 4.2), `ctrl+p` replaces it, one row here.

- **R31.** The tool opens no network connection, copies no clip, and writes nothing but
  sidecars in FOLDER. Its stderr names clip files and errors only; typed text never reaches
  stderr, a log or the journal.

## The loop and typing over it

- **R32.** Replay (`ctrl+r`) plays the clip from its start at once, whatever the state: in a
  pass, paused, in the gap (the pending pass cancelled, so no second play follows it) or
  stopped. When the loop is on, it carries on from that pass. Back, forward and the tempo
  change in the gap or when stopped set the offset and tempo of the next pass and start
  nothing by themselves. The mpv adapter tells the end of a pass (`eof-reached`, whose
  keep-open stop also sets mpv's `pause`) apart from the person's pause, which only the
  seam's pause call sets (a test on the fake IPC peer).
- **R33.** Typing never touches playback. From the first frame of a clip every printable key
  goes to the text box, with no key or focus change first; typing, deleting or pasting during
  a pass or in the gap leaves the fake player's call log unchanged and the next pass on time.
  A refused save (R21's empty text, R24's sidecar that appeared) keeps the clip on screen and
  its loop running.
- **R34.** Leaving a clip ends its loop. For each of save, inaudible, skip, previous, quit
  (`esc`, after its question if it asks one) and `ctrl+c`, a test leaves the clip once in a
  pass and once in the gap, then advances the fake clock well past the gap and asserts no
  further play of that clip was asked for and its player was stopped (R20). The question of
  R27 does not stop the loop; a `y` does. The next clip's first pass starts as R13 says.
