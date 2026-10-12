# Plan 00172: Bluetooth headset microphone for dictation

**Status**: Not Started
**Created**: 2026-10-12
**Owner**: joseph
**Priority**: Medium

## Overview

The owner dictated through speech-to-text wearing a Shokz OpenRun Pro (bone-conduction
Bluetooth headset) and found the input level very low. GNOME Settings → Sound showed the
headset ("Hands-Free OpenRun Pro by Shokz") as the input device. The level was low because
the headset was never recording: dictation was recording from the laptop's built-in
microphone.

Speech-to-text does not choose a microphone. `wsi-stream-server` runs `pw-record` with no
target (`CAPTURE_CMD`), so it records from PipeWire's default source. **The owner's rule:
the microphone dictation uses must be the one GNOME is set to. There must be no disconnect
between the two.** On the host, GNOME showed the headset while PipeWire's default was the
laptop microphone. This plan finds out why the two disagree and fixes it in IaC. It then
measures the headset microphone's real level and adds gain only if the measurement calls
for it.

The OpenRun Pro is the test device. The fix must hold for any Bluetooth headset with a
microphone, because the mechanism (WirePlumber's headset-profile auto-switch) is generic.

## Context & Background

Facts read on the host before this plan was filed (no install-specific identifiers):

- The card's active profile was `a2dp-sink` (high-quality playback, **no microphone**).
  The headset also offers `headset-head-unit` (mSBC, 16 kHz) and `headset-head-unit-cvsd`.
  The two A2DP profiles (SBC and SBC-XQ) are the two "Play HiFi quality music" entries
  GNOME offered under Configuration. Neither of them has a microphone.
- `wpctl status` listed the headset's microphone only under **Filters**, as
  `bluez_input.<address>` (`bluez5.loopback = true`). This is WirePlumber 0.5's
  always-present virtual microphone: recording from it switches the card to a headset
  profile (`bluetooth.autoswitch-to-headset-profile = true`).
- PipeWire's default source (`wpctl` `*`, `pactl get-default-source`, and
  `default.configured.audio.source` in WirePlumber's `default-nodes` state) was the
  laptop's digital microphone, not the headset.
- A `pw-record` with no target, run the way dictation runs it, linked to the laptop
  microphone. The same was seen for a live dictation.
- `play-hd-audio.yml` owns the Bluetooth configuration
  (`51-bluetooth-quality.conf`: mSBC on, SBC-XQ on, `bluez5.auto-connect`, a rule on
  `bluez_input.*`/`bluez_output.*`). Any fix goes there, not in the speech-to-text play.

Open question, not yet established: what GNOME's input selector was actually showing.
Was it the selected default, or only the device listed first? And if it was a
selection, why did it not become PipeWire's default?

## Goals

- When GNOME's input is set to a Bluetooth headset, dictation records from that
  headset's microphone, with no other step.
- The headset switches to a headset profile for the dictation and returns to A2DP
  afterwards (auto-switch), with no manual profile change.
- The headset microphone's speech level is measured. If it is too low for Whisper, a
  gain is applied in IaC (`play-hd-audio.yml`) and measured again.

## Non-Goals

- Speech-to-text choosing its own microphone or keeping a second microphone setting.
  It keeps following the system default. That is the owner's rule.
- Improving wideband/LC3 support beyond what the installed BlueZ/PipeWire offer.
- The Insert/icon state fault seen in the same session: that is Plan 00171.

## Tasks

### Phase 1: Triage (why GNOME and PipeWire disagree)

- [ ] ⬜ **Task 1.1**: `triage.bash` (read-only, on `_planlib.inc.bash`). With the headset
  connected and GNOME's input set to it, record: the card's profiles and active
  profile; `wpctl status`; the default source by `wpctl`, `pactl` and WirePlumber's
  `default-nodes` state; each source's `media.class`, `node.virtual`, `priority.session`
  and `bluez5.loopback`; the headset-related settings from `wpctl settings`; and which
  node a no-target `pw-record` links to over two seconds (the audio is thrown away).
  GNOME's own view is **NOT ESTABLISHABLE** from a script: the script asks the owner to
  state what Settings → Sound → Input shows, and records the answer.
- [ ] ⬜ **Task 1.2**: In the same run: select the headset in GNOME (an owner step the
  script prompts for), then re-read the default source. This answers whether GNOME's
  choice of the loopback node reaches `default.configured.audio.source`, is refused,
  or is replaced.
- [ ] ⬜ **Task 1.3**: Add the plan to `meta-deploy.bash`'s `PLANS` list. Read the report,
  then journal the cause with the evidence.

### Phase 2: Make GNOME's input be dictation's input (IaC)

- [ ] ⬜ **Task 2.1**: The fix the triage points to, in `play-hd-audio.yml` (a WirePlumber
  rule or setting in `wireplumber.conf.d/`). The fix must not make dictation choose its
  own source. If the cause is upstream (GNOME or WirePlumber), record it with the
  upstream reference and the narrowest IaC change that restores "GNOME's input is the
  default source".
- [ ] ⬜ **Task 2.2**: `deploy.bash` (runs `play-hd-audio.yml`) and `acceptance.bash`. With the
  headset selected in GNOME, a no-target `pw-record` must link to the headset's
  microphone; the card must switch to a headset profile while recording and back to
  A2DP afterwards. Any part a script cannot see is reported as NOT ESTABLISHABLE.

### Phase 3: Level

- [ ] ⬜ **Task 3.1**: Measure: a short spoken test through the headset in mSBC. Record peak
  and RMS dBFS in the acceptance log, and the laptop microphone with the same test
  for comparison. Journal the numbers.
- [ ] ⬜ **Task 3.2** (only if Task 3.1 shows the level is too low for Whisper): a gain on the
  headset's capture in `play-hd-audio.yml`, scoped to Bluetooth inputs, then measure
  again. Owner decision first on the target level.

### Phase 4: Close

- [ ] ⬜ **Task 4.1**: Docs: a Bluetooth headset note in `docs/features/speech-to-text.md`
  (dictation follows the system input; the headset switches profile while recording,
  and playback drops to call quality for that time).
- [ ] ⬜ **Task 4.2**: `./scripts/qa-all.bash`; the `qa-reviewer` agent over the plan's
  diff.

## Success Criteria

- [ ] With the headset chosen in GNOME, a dictation records from the headset (seen in
  `acceptance.bash`, not assumed).
- [ ] The headset is back in A2DP after the dictation ends.
- [ ] The headset's speech level is measured and recorded; any gain added is in IaC.
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` reports nothing unresolved.

## Risks & Mitigations

| Risk                                                                           | Impact | Probability | Mitigation                                                               |
| ------------------------------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------------ |
| Headset profile drops playback to call quality while dictating                 | M      | H           | Inherent to Bluetooth HFP; documented; auto-switch returns to A2DP       |
| Profile switch at the start of a dictation delays capture, so first words lost | M      | M           | Acceptance measures the delay; Plan 00148's warm path is checked with it |
| A gain clips loud speech                                                       | L      | M           | Measured before and after; owner sets the target                         |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00172-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed
