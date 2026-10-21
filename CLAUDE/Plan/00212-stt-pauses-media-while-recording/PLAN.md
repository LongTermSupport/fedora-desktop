# Plan 00212: stt pauses media while recording

**Status**: In Progress
**Created**: 2026-10-21
**Owner**: joseph
**Priority**: Medium

## Overview

The owner asked: when a speech-to-text recording starts, music or other media that is playing
should get out of the way for the recording and come back when the recording is over. It is a
switch in the speech-to-text panel icon's menu, on by default. Knowing whether headphones are
in use is left out for now.

Phases 1-3 paused the players and played them again. The owner then asked (Phase 4) for the
volume to be lowered instead: snapshot the playing music's volume, lower it to one third of
what it was (70% to 23.3%), and restore exactly that volume when the recording is done. A
player whose sound cannot be found is still paused, as before.

Every recorder the panel starts (`wsi`, `wsi-stream` in its three startup modes with continuous
dictation, `wsi-article-window`) ends in one of several ways: a normal stop, a cancelled start,
Escape, an error, or a kill. Escape in batch mode SIGKILLs the recorder's whole process group,
so a recorder cannot be trusted to restore what it changed. The one process that sees every
recording start and end is the panel (`panelSession.js`), so the panel starts and ends the
hold. The work itself is a separate Go command, `wsi-lower-media-volume` (the extension stays
thin, `extensions/CLAUDE.md`), which lowers, waits to be told the recording is over, and
restores.

## Goals

- A recording lowers to one third the volume of the audio streams of every MPRIS player that
  is Playing when it starts, and nothing else; the system (output) volume is never touched.
- A Playing player none of whose streams can be found is paused instead, and played again
  after, as in Phases 1-3.
- When the microphone closes, each lowered stream that is still there and still at the lowered
  volume gets exactly its old volume back; one the owner changed, or that ended, is left alone.
- Restore happens on every way a recording ends that leaves the panel or the command alive;
  what a hard kill does is written down (below).
- A panel menu switch, "Lower media volume while recording", on by default.

## Non-Goals

- Headphone or output-device awareness (the owner's call: out of scope for now).
- Lowering or pausing players that start playing during a recording.
- A Settings (prefs.js) row: the switch is in the panel menu, as asked.
- A configurable fraction: one third, as asked.

## Decisions (Phase 4)

- **Which volume: the playing application's own stream (a PulseAudio sink input on PipeWire's
  pulse server), not the MPRIS `Volume` property and not the output (sink) volume.** The MPRIS
  `Volume` property is optional and browsers (Firefox, Chromium) do not implement it, and the
  owner listens in browsers. The output volume would also quieten the system's own sounds and
  everything else, and a restore of it would undo any volume key pressed meanwhile. A stream's
  volume is what the per-application slider in GNOME Settings shows, so "70%" is the number
  the owner sees there.
- **Tool: `pactl --format=json`** (`pulseaudio-utils`), now named in the speech-to-text play
  with `pipewire-pulseaudio` (its server; Fedora Workstation's default, so both are a no-op on
  a normal host). It prints JSON, which is parsed; `wpctl` (WirePlumber) prints only text for
  a person, and `pw-dump`'s JSON is the whole graph, with volumes on a cubic scale that would
  need converting back. Volumes are pactl's raw integers (65536 is 100%), set channel by
  channel, so a balance is kept and the restore is exact.
- **Which streams are a player's.** A player is playing when its MPRIS `PlaybackStatus` is
  `Playing` (as before). Its streams are the sink inputs that are not corked (producing sound)
  whose `application.process.id` is the player's process (`GetConnectionUnixProcessID` of its
  unique name) or a descendant of it (a browser plays from a child process). When that finds
  none (a sandboxed player, whose bus connection is a proxy's), the streams whose
  `application.process.binary` or `application.name` is the player's name (the first word
  after `org.mpris.MediaPlayer2.`, ignoring case: `spotify`, `firefox`) are its streams. A
  stream is lowered once, for the first player in name order that claims it.
- **No stream found: pause it**, if it can pause (the Phases 1-3 behaviour); else it is left
  playing. pactl that cannot run or answers what is not its JSON is reported (exit 1, the
  panel's error) and every playing player is paused instead, so the music still gets out of
  the way.
- **The command is renamed** `wsi-pause-media` to `wsi-lower-media-volume`, and the GSettings
  key `pause-media-while-recording` to `lower-media-volume-while-recording`. The play removes
  the old binary (`state: absent`).
- **The old key is migrated, in the extension.** The pause version was deployed on the host,
  so an owner may have turned the old switch off. The repo has no earlier GSettings key
  rename to follow, so: the old key stays in the schema (marked replaced; GSettings cannot
  read a key its schema does not name), and at each enable `migratePauseMediaKey`
  (`mediaHold.js`) copies an owner-set old value to the new key unless the new one is set
  already, then resets the old key, so it runs once. In the extension rather than the play
  because it is a pure function tested under Node, and the play cannot be run here.
- **New streams of a lowered application (F1 of the Phase 4 review).** WirePlumber's
  `restore-stream` (on by default) saves each application's stream volume and gives the
  application's next stream that volume. So the lowered 23.3% is very likely saved, and a
  stream that is re-created during the hold (the next track, a new video) or a second stream
  opened then would start at 23.3%. At restore, every stream that was not there when the
  volumes were lowered, of a lowered stream's application (same process, or same
  `application.name`), at that stream's lowered volume, is set to its volume before; that
  set is saved by WirePlumber in turn. A lowered stream that has ended with no stream of its
  application left cannot be fixed: nothing is left to set, and WirePlumber's saved state has
  no command to write it. That is reported (exit 1, the panel's error notification), so the
  owner knows to check the app's slider. Whether WirePlumber really keeps the lowered volume
  is settled by the owner's manual check 5.
- **Proxy players are not handled** (V1): `playerctld` mirrors the most recent player, and
  GSConnect exports a phone's players as `org.mpris.MediaPlayer2.GSConnect.*`. Pausing
  playerctld would pause the real player through it, which is lowered already.

## Spec (`wsi-lower-media-volume`, numbered as its tests are)

D-Bus is reached through `busctl --user --json=short --timeout=5`, the streams through
`pactl --format=json`. The package doc of `src-go/internal/wsimedia` is the full statement.

- **V1.** The players are the session-bus names beginning `org.mpris.MediaPlayer2.`
  (`ListNames`), in name order. Each is reached through its unique name (`GetNameOwner`), so a
  player that quits and is replaced under the same name is a different player; two names owned
  by one connection are one player. A proxy (`playerctld`, `GSConnect.*`) is not handled, with
  a line on stderr.
- **V2.** A player whose `PlaybackStatus` is `Playing` is playing; any other is not touched.
  Its streams are found as the decision above says (`pactl --format=json list sink-inputs`,
  read once before any change).
- **V3.** Each of its streams is lowered to one third of its volume, each channel rounded to
  the nearest unit (`pactl set-sink-input-volume <index> <v>...`, in `channel_map` order), and
  the volume before and after is remembered with the stream's index and process.
- **V4.** A playing player with no stream found, or every playing player when pactl cannot be
  read, is paused (`Pause`) if its `CanPause` is true, and remembered; else not touched.
- **V5.** Nothing lowered and nothing paused: the command exits at once.
- **V6.** Otherwise it waits until it is told the recording is over: SIGTERM, SIGINT or SIGHUP,
  or its parent process ending (polled each second; GNOME Shell gone). A signal that arrives
  while it is still lowering is acted on once that is done.
- **V7.** Then the sink inputs are listed again. Each lowered stream still there (same index,
  same process) whose every channel is within 1% (655 of 65536) of the volume it was set to is
  set back to exactly the volume before. One whose volume is otherwise (the owner changed it
  during the recording) is left as it is, and one that has gone is skipped, each with a line on
  stderr. A stream not there at the lowering, of a lowered stream's application (same process,
  or same `application.name` ignoring case), within 1% of that stream's lowered volume, is set
  to its volume before too. A lowered stream that has gone with no stream of its application
  left is a problem (V9, exit 1): the sound server may give the next stream the lowered volume.
- **V8.** Each paused player is played again (`Play`) only if its unique name is still on the
  bus and its `PlaybackStatus` is `Paused`. Gone, `Stopped` or `Playing` is left as it is,
  with a line on stderr.
- **V9.** A player or stream that cannot be read (no owner, no object, no `CanPause`, no
  process, a stream with a channel and no volume for it) is skipped with one stderr line
  (`wsi-lower-media-volume: <player>: skipped: <words>`) and the others carry on; the exit
  stays 0, so a broken player does not raise an error notification on every recording. A
  `Pause`, `Play` or volume change that fails, pactl that cannot be read, or a state unreadable
  at the end, is reported without "skipped", the others still go on, and the exit is 1. A bus
  that cannot be listed (busctl missing, no session bus) is exit 1 with nothing changed.
- **V10.** Any argument but `-h`/`--help` is a usage error, exit 64, nothing run. stdout is
  never written; every message goes to stderr (the GNOME Shell journal, as the recorders').

## The panel's side

- `panelSession.js` asks for the hold (`holdMedia(launch)`) as it starts a recorder it owns and
  releases it when the microphone has closed: the first of a state outside the starting and
  microphone-open states (TRANSCRIBING, CLAUDE_PROCESSING, SUCCESS, ERROR, IDLE), Escape, the
  recorder's exit, a spawn failure, the session's end, and the panel's destroy (extension
  disabled, e.g. at a screen lock: the volume comes back even though the recorder may run on,
  since nothing would release it later).
- `mediaHold.js` (tested under Node): the switch off is no hold; on, it spawns
  `~/.local/bin/wsi-lower-media-volume` and release sends it TERM, once, and never after it has
  exited (its PID may be reused). A non-zero exit, or a signal it was not sent, is reported with
  the panel's error notification; the dictation carries on, since it does not depend on the
  media.
- Continuous dictation and its interim pastes keep the hold: the state stays RECORDING. A
  recorder the panel did not start (one that ran across a screen lock) gets no hold.

## What a hard kill does

| What dies                                | Result                                                                                                                                                                     |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The recorder (Escape, KILL, crash)       | The panel sees the exit and releases the hold: restored.                                                                                                                   |
| GNOME Shell (crash, logout)              | `wsi-lower-media-volume` sees its parent gone within a second and restores.                                                                                                |
| `wsi-lower-media-volume` itself, SIGKILL | Nothing can restore: the streams stay at one third (and a paused player paused) until changed by hand. The panel reports the unexpected exit, with its error notification. |

## Known limitations (accepted)

- **Overlapping holds on back-to-back recordings.** If hold 2 starts while hold 1 has not yet
  restored, hold 2 takes the lowered volume as the one to restore; hold 1 then sees a volume
  it did not set and leaves it, and hold 2 restores the lowered one: the music stays at one
  third. Unlikely, since Insert is refused until recording 1's session ends, well after the
  TERM at TRANSCRIBING. Not tested.
- **A stale status after a very short recording (paused players only).** Some players update
  `PlaybackStatus` after `Pause` has returned; a TERM within milliseconds can read a stale
  `Playing` and leave the player paused.
- **A stream that starts during the recording** is not lowered by this command; if
  WirePlumber starts it at the saved lowered volume, it is put back at restore (V7).
- **A lowered stream that ends with nothing of its app left at restore** (the playlist ended,
  the tab closed) cannot be put back, and WirePlumber may start the app's next stream at
  23.3%. It is reported with the panel's error; the owner sets the app's slider back.
- **Every playing stream of a browser is lowered**, not only the tab whose player is Playing:
  the streams are found by the browser's process tree, and all its tabs share it, so a call
  in another tab is lowered too (and restored with the rest).
- **A proxy player is never paused** (`playerctld`, GSConnect's phone players). A phone's
  music therefore plays on during a recording; a proxy not on the list (another name) is
  handled as a player, and if it has no stream it is paused, which reaches the real player.
- **A screen lock restores the volume** while the recorder may still run (the panel's
  `destroy()` releases the hold). A decision above, listed so the owner is not surprised.

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

### Phase 3: Deploy and accept (the pause version)

- [x] ✅ **Task 3.1**: superseded by Task 4.4 before it ran on the host.
- [x] ✅ **Task 3.2**: superseded by Task 4.5 (the queued entries now describe Phase 4).

### Phase 4: Lower the volume instead (the owner's second request)

- [x] ✅ **Task 4.1**: The spec above (V1-V10) and the decisions, before the code.
- [x] ✅ **Task 4.2**: Tests first for V1-V10 (fake busctl, pactl and `/proc` parents), then
  `internal/wsimedia`; the command renamed `src-go/cmd/wsi-lower-media-volume`, its row in
  `scripts/go-binary-suites.tsv`, `src-go/CLAUDE.md`'s row.
- [x] ✅ **Task 4.3**: The play builds the renamed command, removes `wsi-pause-media`
  (`state: absent`) and names `pulseaudio-utils` and `pipewire-pulseaudio`; the GSettings key
  renamed; `mediaHold.js`, its Node test and the menu label "Lower media volume while
  recording"; the docs.
- [ ] 🔄 **Task 4.4**: `acceptance.bash`: the installed command against the fake MPRIS player
  on a private bus and a fake `pactl` (`fake-pactl.py`, first on `PATH`), so no real volume or
  player is touched: lowered to a third and restored at TERM; a volume changed by hand during
  the hold is left; a stream that ended with nothing of its app left is reported (exit 1); a
  stream re-created at the lowered volume is put back; no stream found is paused and played again;
  stopped by hand during the hold is not played again; nothing playing exits at once; the
  parent ending restores. Written; it runs on the host (the container has no python3-gobject
  for the fake player; `fake-pactl.py` was exercised on its own).
- [x] ✅ **Task 4.5**: The two `meta-deploy.list` entries (the play, then this plan) describe
  Phase 4.
- [ ] 🔄 **Task 4.6**: qa-reviewer pass. The Opus stand-in's review
  (`subagent-reports/261021-review-00212-phase4-opus.md`): FIX-FIRST on F1; Task 4.7 fixes it.
- [x] ✅ **Task 4.7**: The review's fixes, tests first: new streams of a lowered application at
  the lowered volume restored, an ended stream with nothing of its app left reported (V7, the
  fake pactl models a re-created stream); the old key migrated (`migratePauseMediaKey`);
  proxy players skipped (V1); the browser-tab limitation recorded. `acceptance.bash` gains
  the `recreated` leg, and `stream-ended` now expects exit 1.
- [ ] ⬜ **Task 4.8**: A fresh qa-reviewer pass over Task 4.7.

## Owner's manual check (after logging out and in)

1. Play music in a browser at a per-app volume you can see (GNOME Settings, Sound, Volume
   Levels: say 70%); press Insert; the music drops to about 23%; speak; press Insert; it is
   back at 70% once the microphone closes.
2. Play music; press Insert, then Escape: the volume comes back.
3. Play music; press Insert; move that app's slider during the recording; stop: your setting
   stays.
4. Turn "Lower media volume while recording" off in the panel menu; press Insert: the music
   keeps its volume. (If you had turned "Pause media while recording" off before, the new
   switch is already off after the login.)
5. Does WirePlumber keep the lowered volume? Play a playlist in a music app at 70%; press
   Insert; while it records, skip to the next track (in a browser: start another video in the
   same tab); stop. The next track must be at 70% again in Settings, Sound, Volume Levels,
   and the journal (`journalctl --user -b --no-pager -g wsi-lower-media-volume`) shows either
   "a new stream of it at the lowered volume" (WirePlumber did keep 23.3%, and it was put
   back) or nothing about a new stream (the app kept its stream). Then play, press Insert,
   close the app during the recording, stop, and reopen it: the error notification appeared,
   and the app's volume is 23.3% if WirePlumber keeps it (set it back by hand), 70% if not.
   Record which it was in the journal; if 70%, the exit 1 for an ended stream is too loud and
   becomes a plain line.

## Success Criteria

- [x] M1-M7 each have a Go test seen red then green.
- [x] The panel's hold and release have Node tests for each ending.
- [x] V1-V10 each have a Go test seen red then green.
- [ ] `acceptance.bash` passes on the host, and the owner's manual check passes.

## Delivery & Milestones

- Delivery commits are listed here when the plan completes.
