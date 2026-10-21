# Plan 00212: stt pauses media while recording

**Status**: In Progress
**Created**: 2026-10-21
**Owner**: joseph
**Priority**: Medium

## Overview

The owner asked: when a speech-to-text recording starts, music or other media that is playing
should pause for the recording and play again when the recording is over. It is a switch in the
speech-to-text panel icon's menu, on by default. Knowing whether headphones are in use is left
out for now.

Every recorder the panel starts (`wsi`, `wsi-stream` in its three startup modes with continuous
dictation, `wsi-article-window`) ends in one of several ways: a normal stop, a cancelled start,
Escape, an error, or a kill. Escape in batch mode SIGKILLs the recorder's whole process group,
so a recorder cannot be trusted to resume what it paused. The one process that sees every
recording start and end is the panel (`panelSession.js`), so the panel starts and ends the
pause. The pausing itself is a separate Go command, `wsi-pause-media` (the extension stays
thin, `extensions/CLAUDE.md`), which pauses, waits to be told the recording is over, and
resumes.

## Goals

- A recording pauses every MPRIS player that is Playing when it starts, and nothing else.
- When the microphone closes, each of those players that is still Paused plays again; one the
  owner paused, stopped or closed during the recording is left alone.
- Resume happens on every way a recording ends that leaves the panel or the pause command
  alive; what a hard kill does is written down (below).
- A panel menu switch, "Pause media while recording", on by default.

## Non-Goals

- Headphone or output-device awareness (the owner's call: out of scope for now).
- Pausing players that start playing during a recording.
- A Settings (prefs.js) row: the switch is in the panel menu, as asked.

## Spec (`wsi-pause-media`, numbered as its tests are)

D-Bus is reached through `busctl --user --json=short --timeout=5` (systemd, on every host, and
its JSON is parsed rather than GVariant text). playerctl is not installed by any play and is not
needed. The package doc of `src-go/internal/wsimedia` is the full statement.

- **M1.** The players are the session-bus names beginning `org.mpris.MediaPlayer2.`
  (`ListNames`), in name order. Each is reached through its unique name (`GetNameOwner`), so a
  player that quits and is replaced under the same name is a different player; two names owned
  by one connection are one player.
- **M2.** A player whose `PlaybackStatus` is `Playing` and whose `CanPause` is true is paused
  (`Pause` on its unique name) and remembered. Any other player is not touched.
- **M3.** Nothing paused: the command exits at once (nothing to resume).
- **M4.** Otherwise it waits until it is told the recording is over: SIGTERM, SIGINT or SIGHUP,
  or its parent process ending (polled each second; GNOME Shell gone). A signal that arrives
  while it is still pausing is acted on once the pausing is done.
- **M5.** Then each remembered player is played again (`Play`) only if its unique name is still
  on the bus and its `PlaybackStatus` is `Paused`. Gone, `Stopped` or `Playing` (the owner
  changed it during the recording) is left as it is, with a line on stderr.
- **M6.** A player whose owner or state cannot be read (no object, no `CanPause`, a reply that is
  not busctl's JSON) is skipped with one stderr line (`wsi-pause-media: <player>: skipped: <words>`) and the others carry on; the exit stays 0, so a broken player does not raise an
  error notification on every recording. A `Pause` or `Play` that fails, or a state unreadable at
  the resume, is reported the same way without "skipped", the others still go on, and the exit
  is 1. A failure to list the bus (busctl missing, no session bus) is exit 1 with nothing paused.
- **M7.** Any argument but `-h`/`--help` is a usage error, exit 64, nothing run. stdout is
  never written; every message goes to stderr (the GNOME Shell journal, as the recorders').

## The panel's side

- `panelSession.js` asks for the hold (`holdMedia(launch)`) as it starts a recorder it owns and
  releases it when the microphone has closed: the first of a state outside the starting and
  microphone-open states (TRANSCRIBING, CLAUDE_PROCESSING, SUCCESS, ERROR, IDLE), Escape, the
  recorder's exit, a spawn failure, the session's end, and the panel's destroy (extension
  disabled, e.g. at a screen lock: the music resumes even though the recorder may run on, since
  nothing would release it later).
- `mediaHold.js` (tested under Node): the switch off is no hold; on, it spawns
  `~/.local/bin/wsi-pause-media` and release sends it TERM, once, and never after it has exited
  (its PID may be reused). A non-zero exit, or a signal it was not sent, is reported with the
  panel's error notification; the dictation carries on, since it does not depend on the media.
- Continuous dictation and its interim pastes keep the hold: the state stays RECORDING. A
  recorder the panel did not start (one that ran across a screen lock) gets no hold.

## What a hard kill does

| What dies                          | Result                                                                                                   |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------- |
| The recorder (Escape, KILL, crash) | The panel sees the exit and releases the hold: resumed.                                                  |
| GNOME Shell (crash, logout)        | `wsi-pause-media` sees its parent gone within a second and resumes.                                      |
| `wsi-pause-media` itself, SIGKILL  | Nothing can resume: the players stay paused until played by hand. The panel reports the unexpected exit. |

## Known limitations (accepted, from the Opus review)

- **A stale status after a very short recording.** Some players (Spotify among them) update
  `PlaybackStatus` after `Pause` has returned. If TERM arrives within milliseconds of the pause,
  M5 can read a stale `Playing`, leave the player alone, and the pause then takes effect: the
  music stays paused. It needs a sub-second tap; a short re-read before "left as it is" would
  close it.
- **Overlapping holds on back-to-back recordings.** Hold 1 may still be resuming when hold 2
  lists the players; hold 2 then sees `Paused`, does not remember the player, and hold 1 plays it
  during recording 2. Unlikely, since Insert is refused until recording 1's session ends, well
  after the TERM at TRANSCRIBING. Not tested.
- **A screen lock resumes the music** while the recorder may still run (the panel's `destroy()`
  releases the hold). A decision above, listed so the owner is not surprised.

## Tasks

### Phase 1: The pause command

- [x] ✅ **Task 1.1**: Tests first for M1-M7 against `execx.Fake` (busctl replies) and fake
  signals and parent PID; then `src-go/internal/wsimedia` and `src-go/cmd/wsi-pause-media`.
  `busctl` gains `internal/wsimedia` as a second owner in the project rules' table.
- [x] ✅ **Task 1.2**: Register it in `scripts/go-binary-suites.tsv` (qa-go) and build it in
  `play-speech-to-text.yml` with `tasks/build-go-command.yml`; name `python3-gobject` and
  `dbus-daemon` (the acceptance's fake player and private bus; busctl is systemd's, already on
  every host) in the play's dependencies. `dbus-daemon` is a test-only dependency of a
  production play, kept for `dbus-run-session`.

### Phase 2: The panel

- [x] ✅ **Task 2.1**: `pause-media-while-recording` GSettings key, default true.
- [x] ✅ **Task 2.2**: `mediaHold.js` and the `panelSession.js` hold and release, tests first in
  `tests/extensions/`; the menu switch in `extension.js`; the new file in the play's copy loop
  and the new suite in `scripts/test-panel-sections.bash`. ESLint clean. `metadata.json`'s
  version stays 1: no rule asks for a bump and no feature has bumped it.

### Phase 3: Deploy and accept

- [ ] 🔄 **Task 3.1**: `acceptance.bash` (written; to run on the host): the installed
  `wsi-pause-media` against a fake MPRIS player (`fake-mpris-player.py`) on a private session
  bus (`dbus-run-session`), so no real player is touched: a Playing player is paused and
  played again at TERM; one stopped by hand during the hold is not played again; nothing
  playing exits at once; its parent ending plays it again. The fixture could not run in the
  container (no python3-gobject).
- [ ] 🔄 **Task 3.2**: `meta-deploy.list` entries (the play, then this plan) are queued; the
  owner's manual check below remains.
- [ ] ⬜ **Task 3.3**: qa-reviewer pass.

## Owner's manual check (after logging out and in)

1. Play music; press Insert; the music pauses; speak; press Insert; it plays again once the
   microphone closes.
2. Play music; press Insert, then Escape: it plays again.
3. Play music; press Insert; close the player (or stop it) during the recording; stop the
   recording: nothing is started again.
4. Turn "Pause media while recording" off in the panel menu; press Insert: the music keeps
   playing.

## Success Criteria

- [x] M1-M7 each have a Go test seen red then green.
- [x] The panel's hold and release have Node tests for each ending.
- [ ] `acceptance.bash` passes on the host, and the owner's manual check passes.

## Delivery & Milestones

- Delivery commits are listed here when the plan completes.
