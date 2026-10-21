# Review: Plan 00212 Phase 4 (lower media volume), branch agent-a9a981acd628713d2-1188eaed

Reviewer: Opus, standing in for Fable (qa-reviewer checklist, Task 4.6). Read-only.

## VERDICT: FIX-FIRST

One defect breaks the owner's requirement ("restore to 70%") in an ordinary case. Everything
else holds up.

## What I ran

- `go test ./internal/wsimedia/ ./cmd/wsi-lower-media-volume/`: ok.
- `node --test tests/extensions/test-stt-media-hold.mjs`: 6 pass, 0 fail.
- `git merge-tree F44 <branch>`: merges clean. The branch is based before 32c95ba6, but its
  meta-deploy.list hunk does not conflict with that commit.
- ESLint: not run. Bare `npx eslint` finds no config here. The JS change is renames only.

## Blocking finding

**F1. WirePlumber keeps the lowered volume when a stream is re-created or ends during the hold.**
I have not checked this on the host yet. WirePlumber's stream restore (`restore-stream`, on by
default on Fedora) saves each stream's volume per application. `pactl set-sink-input-volume`
to 23.3% is therefore saved as that app's volume. Two cases follow:

- **The stream ends during the hold** (next track, a new video, the app reopening its output).
  V7 says "has ended; nothing to restore" and exits 0. The app's next stream then starts at
  23.3% and stays there for good.
- **The stream is re-created during the hold.** The new stream has a new index, so it is never
  matched and is left at 23.3%.

The unit tests and acceptance.bash cannot see this: the fake pactl has no restore database.

Suggested fix: at restore, also put back to `before` any stream that is there now, was not
lowered, belongs to the same process tree or app name as a lowered stream, and sits within
tolerance of `after`. For an ended stream with no successor, either make it a reported
problem (exit 1, so the panel shows it) or prove on the host that WirePlumber does not keep
the volume. Add a manual host check of exactly this case to PLAN.md.

## Non-blocking notes

- **N1.** Every uncorked stream of a browser's process tree is lowered, not only the tab that
  is playing (a call in another tab is lowered too). A muted tab's stream is lowered and
  restored, which does no harm. Worth listing under known limitations.
- **N2.** An MPRIS proxy player (playerctld, GSConnect) has no stream of its own, so it is
  paused. That pause reaches the real player, which then ends up both lowered and paused.
  Neither tool is installed by this repo, so this is a known-limitations line only.
- **N3.** The GSettings key is renamed with no migration. That is fine only if the 00212 play
  never ran. The queue still holds it, and the last host run stopped at 00183's acceptance
  failure, which is before 00212 in the list. That is consistent with "never deployed", but
  confirm it with the owner.
- **N4.** Flatpak players: the bus process ID is xdg-dbus-proxy's, and the stream's process ID
  is the sandbox's own numbering, so the match falls back to the app name. That is correct
  for Spotify and Firefox, and acceptable.

## Checks that pass

- Every channel is snapshotted and set on its own, in `channel_map` order, so balance is kept.
  Raw integers work above 100%. `(v+1)/3` turns 45875 into 15292 (23.3%), and the restore
  sets the exact old integers.
- The ±655 tolerance is enough for the server's rounding.
- A stream moved to another sink keeps its index, so it is still found and restored.
- pactl that cannot be read or does not answer JSON falls back to pausing every playing
  player, with exit 1. Corked streams are excluded.
- The `pactl --format=json` field names match PulseAudio 16 and later (Fedora 44 ships 17).
- dnf `state: present` without allowerasing: on a host that already has pipewire-pulseaudio it
  does nothing. On a host running PulseAudio it fails on the conflict and never swaps the
  sound stack.
- The old binary is removed with `state: absent`.
- No live references to `wsi-pause-media` remain outside plan history, the removal task and
  its meta-deploy description. go-binary-suites.tsv, the extension and the docs are renamed.
- acceptance.bash uses dbus-run-session and a fake pactl first on PATH, so it touches no real
  player or volume.
- The meta-deploy.list descriptions are accurate.
- No install-specific names found.
