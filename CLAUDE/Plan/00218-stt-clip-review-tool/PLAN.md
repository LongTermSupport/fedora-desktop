# Plan 00218: stt clip review tool

**Status**: Not Started
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

Addresses #112.

## Overview

Speech-to-text engines disagree on some stretches of long recordings, and when one skips
words there is no ground truth to settle it. Plan 00179's method
([RESEARCH-clip-review-method.md](../00179-stt-recording-batch-and-engine-research/RESEARCH-clip-review-method.md))
has two engines transcribe the same segments, triages their differences, and cuts each
doubtful segment (and some unmarked controls) into a short clip. A person then listens to
each clip and types what it says. That typed text settles the stretch, and the clips
together become the hand-checked reference for scoring engines.

This plan builds the tool the person uses: `stt-review`, a queue over a folder of clips that
plays the current clip and offers an empty text box, and nothing else. It shows no engine
text, so the person is not led. Each answer goes into a sidecar file beside its clip, and that
sidecar is the only state: a clip with one is done, and deleting it queues the clip again. It
runs on the owner's desktop, from the speech-to-text panel's menu, in the same house style as
the panel's other tools. The clips are the owner's voice: they stay in a local folder and are
never committed, attached to an issue or sent anywhere.

Which segments become clips, the clip cutter and the merge of sidecar text into a transcript
are not part of this plan (they belong to Plan 00179 and the experiment session). This plan
owns the tool, its file contract and its install.

## Goals

- `stt-review [FOLDER]`: play, replay, pause, 3 s back and forward, slower; type and save;
  mark inaudible; skip; previous clip; progress and the clip's name on screen.
- The file contract (clip name, sidecar suffix, inaudible marker) written down once, in
  `docs/features/speech-to-text.md`, so the cutter and the scorer are built independently.
- A crash, a quit or a closed window never leaves a half-written sidecar or a player still
  playing.
- Installed by `play-speech-to-text.yml`; opened from the panel menu.

## Non-Goals

- Choosing segments, cutting clips, triaging differences, or merging sidecars into a
  transcript (Plan 00179 Tasks 1.5 and 1.6).
- Showing any engine's text, a waveform or a spectrogram.
- Any network use, sync or sharing of clips. Copying a folder to another machine is the
  person's business; the tool needs nothing else to resume there.
- Audio formats other than the contract's Ogg Opus (decision D8).

## Design

### Platform: a Go terminal tool, opened by the panel (decision D1)

The existing panel is the `speech-to-text@fedora-desktop` GNOME Shell extension. Its menu
already opens two tools in a terminal window: `Manage Whisper Models...` and `Server Manager...` run in `foot` at a fixed size, falling back to `xdg-terminal-exec`
(`_openModelManager`, `_openServerManager` in `extension.js`). `stt-review` follows that
pattern: a Go command built on the house TUI (`internal/ui`, the frame's breadcrumb and footer
of visible actions, Plan 00204), opened by a new `Review Clips...` menu item.

- **Not inside GNOME Shell.** A text-entry dialog in the extension would run in the
  compositor: a fault there takes the session down on Wayland, it is testable only in a nested
  shell, and every change needs a logout. The extension's part stays one menu item.
- **Not a GTK window.** The one GTK window the panel opens (`wsi-article-window`) is Python;
  new tooling is Go (CLAUDE.md), and Go has no GTK binding in this module's allowlist. A
  terminal screen needs nothing beyond the vendored Charm libraries, and is unit tested
  through fakes.
- **Go, in `src-go`.** The flow (folder, queue, sidecars, player) is `internal/sttreview`; the
  screen is a new `internal/ui` component, since only `internal/ui` imports the TUI libraries.
  The text line reuses `pathinput`'s editing (control and bidirectional characters refused
  whole), factored into a shared piece, so no new package is vendored (decision D12).

### Playback (decision D2)

The player is an external program run through `execx`, behind a small `Player` seam (play a
file from an offset at a tempo; stop; an event when it ends), so every behaviour is tested
with a fake. The default is sox's `play`, which `play-speech-to-text.yml` already installs:
`play -q -t opus <clip> trim <offset> tempo -s <t>` covers replay, pause (stop, keep the
position), seeking and slower playback with the pitch kept, each a restart at an offset (sox
has no pause or seek of a running player). `-t opus` is needed because sox picks its Vorbis
handler for a `.ogg` name. The length is read in Go from the Ogg stream (last granule
position minus pre-skip; R18), so no tool's flags matter for it. Whether
Fedora's sox decodes Opus at all (through libopusfile) is not known yet: Task 1.1 establishes
it on the host. If it cannot, the fallback is `ffplay` from Fedora's own `ffmpeg-free`
(`-nodisp -autoexit -ss <offset> -af atempo=<t>`), which keeps the same restart design; `mpv`
is not the fallback because it comes from RPM Fusion, which this play does not depend on. The
position is counted from an injected clock, and the end of playback is the process's exit
(no timed guess; the `project_realtime_bound` rule). The player runs under
`execx.Cmd.NewSession` and `execx.SignalContext`, so no player outlives the tool.

### Keyboard flow

The text box has the focus throughout, so every action is on a non-printing key, drawn in the
footer (decision D3; the table is R30 of the spec). A clip plays as soon as it is shown. The
person listens (replaying, pausing, stepping back, slowing down as needed), types, and presses
Enter to save and move on. `ctrl+x` marks it inaudible, PgDown skips it, PgUp goes back to the
previous clip (its saved text editable), Esc quits. After the last clip an end screen counts
the session and offers to go round again over what is still unsaved.

### Specification

[SPEC-stt-review.md](SPEC-stt-review.md): the file contract and behaviours R1-R31, each a test.

## Decisions the coordinator may default

| #   | Decision                                      | Default                                                                                                                                                                                                                                                     |
| --- | --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Platform                                      | Go terminal tool on the house TUI, opened from the panel menu in `foot` (above)                                                                                                                                                                             |
| D2  | Player                                        | sox `play -t opus` if Task 1.1 shows it decodes Ogg Opus; else `ffplay` from Fedora's `ffmpeg-free`, installed by the play                                                                                                                                  |
| D3  | Keys                                          | As in R30; every action on the review screen non-printing; `ctrl+p` replaces `ctrl+space` if an input method takes it                                                                                                                                       |
| D4  | Seek step and slow tempo                      | 3 s; 0.75 with pitch kept (`tempo -s`)                                                                                                                                                                                                                      |
| D5  | Play a clip as soon as it is shown            | Yes                                                                                                                                                                                                                                                         |
| D6  | A folder holding anything but clips, sidecars | Refused before any screen, every stray entry named (R6), as the contract says the folder holds only clips; the tool's own crash temporaries are removed at start, said on stderr. The clip cutter (Plan 00179 Task 1.5) is told to write nothing else there |
| D7  | A folder inside a git work tree               | Refused (R7): clips must not sit in a repository                                                                                                                                                                                                            |
| D8  | Formats                                       | `.ogg` only, as the contract says; widen when a cutter writes another                                                                                                                                                                                       |
| D9  | Previous clip                                 | Allowed, its saved text editable and re-saved atomically (R26)                                                                                                                                                                                              |
| D10 | Panel menu item                               | Yes, `Review Clips...`, opening the tool with no folder, so it asks (R1)                                                                                                                                                                                    |
| D11 | Command name                                  | `stt-review`, a binary of its own (`stt` takes paths as arguments, so a `review` subcommand would be ambiguous)                                                                                                                                             |
| D12 | Text line                                     | `pathinput`'s editing factored out and shared; no new vendored package                                                                                                                                                                                      |

## Tasks

### Phase 1: Establish the player (read-only on the host)

- [ ] ⬜ **Task 1.1**: `triage.bash` in this folder, on `_planlib.inc.bash`
  (PlanScriptStandards.md R1-R14), read-only: which encoder can make a synthetic 48 kHz mono
  Ogg Opus clip (`opusenc`, `gst-launch-1.0` with `opusenc ! oggmux`, `ffmpeg`), one made in a
  temporary folder outside any checkout; whether the installed sox decodes it, probed both
  without and with the type (`sox <clip>.ogg -n stat` and `sox -t opus <clip>.ogg -n stat`, the
  decoded length in `stat`'s output, which sox writes to stderr), and `play -q -t opus` of it to a null output; whether
  `ffplay` is installed, which package owns it (`rpm -qf`), and whether it plays the clip. Queued in `meta-deploy.list`, with
  what it does, in the commit that adds it. The result settles D2 and the acceptance's encoder,
  and is recorded in this plan's journal.

### Phase 2: The tool (tests first)

- [ ] ⬜ **Task 2.1**: Tests, then the folder and queue: the clip pattern, ordering, the stray
  entry, git work tree and empty folder failures, the queue as clips without a sidecar
  (R3, R5-R8, R29).
- [ ] ⬜ **Task 2.2**: Tests, then the sidecar writes: trimmed text, the marker, atomic
  temporary and move, mode 0600, re-save after previous through `fsx.WriteAtomic`, and a new
  `fsx.WriteNew` (link-then-unlink, `os.ErrExist` when the name is taken) with its own tests,
  so no sidecar that appeared is replaced, sharing `WriteAtomic`'s `.tmp.<pid>` naming; the
  crash temporary removed at start; the clip length read from the Ogg stream
  (R6, R18, R21, R22, R24, R26).
- [ ] ⬜ **Task 2.3**: Tests, then the `Player` seam and its sox (or `ffplay`) adapter through
  `execx` (`Cmd.NewSession`, `SignalContext`): `-t opus`, offset, tempo, stop, the end as an
  event, no player left on any exit path (R4, R13-R20). A fake player and clock for the flow
  tests.
- [ ] ⬜ **Task 2.4**: Tests, then the screen in `internal/ui` (the frame's heading, playback
  line, text line, footer) and the flow: every action, the end screen, quit with unsaved text,
  the no-transcript check (R1, R2, R9-R12, R23, R25, R27, R28, R30). The shared text line
  factored out of `pathinput` with its tests kept green.
- [ ] ⬜ **Task 2.5**: `src-go/cmd/stt-review`, the package doc carrying R1-R31, its
  `stt-review<TAB>qa-go` row in `scripts/go-binary-suites.tsv` (a command with no row fails
  QA), `stt-review` added to the tool list of `src-go/CLAUDE.md`, and a `GoProjectRules.md`
  rule first for any defect class found on the way.

### Phase 3: Install, panel, docs

- [ ] ⬜ **Task 3.1**: `play-speech-to-text.yml` builds and installs `stt-review` to
  `~/.local/bin` through `tasks/build-go-command.yml`, and installs the player (if D2 needs
  `ffmpeg-free`) and the acceptance's encoder (as it installs `dbus-daemon` for Plan 00212's
  acceptance).
- [ ] ⬜ **Task 3.2**: The `Review Clips...` panel menu item, opening `stt-review` in `foot`
  at `--window-size-chars=100x20` with `xdg-terminal-exec` as the fallback, as the server
  manager does; ESLint clean. A menu item is a change to `extension.js`, so the owner logs out
  and in after the play (Wayland); the meta-deploy description says so.
- [ ] ⬜ **Task 3.3**: `docs/features/speech-to-text.md`: a clip review section with the file
  contract (clip name, sidecar, marker, the sidecar as the only state), the keys, and that the
  clips are private and stay local; the new item in its Extension Menu list.

### Phase 4: Acceptance

- [ ] ⬜ **Task 4.1**: `deploy.bash` (runs `play-speech-to-text.yml`) and `acceptance.bash`, the
  host agent's check, on synthetic clips only: tone clips of about 10 s encoded to Ogg Opus
  in a temporary folder outside any checkout, named by the contract. It drives `stt-review`
  in a detached tmux session of a fixed size: types a line and saves, marks one inaudible,
  skips one, quits; each step waits, bounded, on the screen text it expects (`capture-pane`),
  never a bare sleep. It then checks each sidecar's bytes, that the skipped clip alone is
  queued on a second start, that a playback stream exists while a clip is playing (`pactl list sink-inputs`, probed inside the 10 s), that no
  player process is left after quit and after the tmux session is killed, and that a folder
  inside a git checkout is refused. Exit 2 (COULD NOT ESTABLISH) when the host has no audio
  sink. Queued in `meta-deploy.list`.
- [ ] ⬜ **Task 4.2**: The owner's hand check, comfort only (everything else is the
  acceptance's): opened from the panel on a real clip folder, the keys feel right (and
  `ctrl+space` reaches the tool rather than an input method; else D3's `ctrl+p`), the slow
  tempo is useful, the text is legible at the window size.
- [ ] ⬜ **Task 4.3**: `qa-all.bash`, then the `qa-reviewer` agent over the plan's diff.

## Success Criteria

- [ ] Pointed at a folder of clips, a person can play each, type what they hear, quit part
  way, reopen and find only the clips without a sidecar queued (#112's acceptance).
- [ ] Every sidecar is plain UTF-8 a script can read; the contract is documented.
- [ ] No engine text is ever drawn; no clip leaves the folder; no player outlives the tool.
- [ ] The host acceptance passes and the owner finds it comfortable.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00218-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #112 and the experiment session's contract.
