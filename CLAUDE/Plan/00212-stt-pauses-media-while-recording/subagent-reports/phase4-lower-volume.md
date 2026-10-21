# Plan 00212 Phase 4: lower the media volume instead of pausing

Branch `agent-a9a981acd628713d2-1188eaed`, two commits on F44:

- `5c455b39`: the Phase 4 spec (V1-V10) and decisions in PLAN.md, before the code.
- `55fd4262`: the code, tests, play, extension, docs, acceptance and meta-deploy.

## Decisions

- The volume lowered is each playing app's own stream (a pactl sink input), not MPRIS `Volume`
  (Firefox and Chromium do not implement it) and not the output volume (it would quieten
  system sounds and other apps). `pactl --format=json` (`pulseaudio-utils`, added to the play
  with `pipewire-pulseaudio`).
- Player to stream: uncorked sink inputs whose process is the player's or one of its
  descendants, else those named as the player. No stream found: paused as before. pactl
  unreadable: every playing player is paused and the exit is 1, so the panel shows an error.
- Restore: only a stream with the same index and process that is still within 1% of the value
  set. A volume the owner changed is left, and a stream that ended is skipped.
- Renamed `wsi-lower-media-volume` and the key `lower-media-volume-while-recording`. The play
  removes `~/.local/bin/wsi-pause-media`. The menu label is "Lower media volume while recording".

## QA

- go test `internal/wsimedia`: red (23 failing plus one hang) then green, `-race`, 100% coverage.
  golangci-lint clean.
- Node: 128 pass. ESLint clean. `ansible-playbook --syntax-check` passes.
- `qa-changed.bash` passes for both commits.
- Not run: `qa-all`, the push, and the qa-reviewer pass (Task 4.6).

## What the owner checks

1. Run `./CLAUDE/Plan/meta-deploy.bash`. It runs the speech-to-text play, then this plan's
   acceptance.
2. Log out and back in.
3. Do the four hand checks in PLAN.md. The container cannot run the acceptance (no
   python3-gobject) and has no real sound server.
