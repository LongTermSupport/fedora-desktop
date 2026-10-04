# Plan 00155: Headset button toggles speech-to-text

**Status**: Not Started (decision gate: the Phase 2 owner decisions D1 to D4; Phase 1
triage runs first)
**Created**: 2026-10-04
**Owner**: joseph
**Priority**: Medium

## Overview

Dictation is started and stopped today with the speech-to-text extension's keyboard
shortcut (Insert by default). Away from the keyboard, the natural control is the button
already on the user's ear: the multifunction button of a Bluetooth headset (for example
the OpenRun Pro by Shokz). This plan makes a press of that button start a recording and
the next one stop it, exactly as the shortcut does. The owner's stated preference, and
the leading candidate for the gesture decision (D3), is that a double press toggles
speech-to-text while a single press keeps doing media play/pause.

Triage on the owner's host has shown the button is reachable: while the headset is
connected in A2DP, bluez exposes its AVRCP controls as an evdev input device, and the
button arrives there as a play/pause media key. What stands in the way is access (the
node is readable by root and the `input` group only), hotplug (the device comes and goes
with the headset), and a set of behavioural choices only the owner can make: whether the
listener takes the key away from the media player, which headsets it listens to, and
which gesture means "toggle".

The work is a small, TDD'd stdlib-only Python listener run as a systemd user service,
plus a udev rule granting the desktop user access to the headset's AVRCP node, all
deployed by `play-speech-to-text.yml`.

## Goals

- One press of a connected Bluetooth headset's multifunction button performs the same
  toggle as the `toggle-recording` shortcut: it starts a recording when idle, and stops
  (or, during the stop grace, stops at once) when a recording is active.
- The listener survives the headset connecting, disconnecting and reconnecting, with no
  restart and no playbook run.
- The desktop user can read the headset's AVRCP node through a udev rule deployed by
  Ansible, without joining the `input` group.
- The behaviour, its limits and its interaction with media playback are documented in
  `docs/features/speech-to-text.md`.

## Non-Goals

- **The headset button while the headset is in HFP.** If the headset microphone became
  the recording source, the headset would switch from A2DP to HFP, and button presses
  would then arrive as HFP AT commands, not AVRCP key events. This plan does not handle
  that path: it is a documented limitation (F5).
- Recording from the headset microphone. wsi keeps recording from the default source.
- Changing wsi or wsi-stream's recording behaviour, or the Insert shortcut.
- Wired headsets, USB HID headsets, and other media keys (next/previous/volume) as
  speech-to-text controls.
- Any MPRIS-level integration (pausing a player when dictation starts, and so on).

## Context & Background

### Established facts (owner's host triage, before this plan was filed)

Phase 1's `triage.bash` re-captures each of these so they rest on a re-runnable probe.

| ID  | Fact                                                                                                                                                                                                      | Source                      |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| F1  | While a headset is connected in A2DP, bluez creates an evdev input device named `<headset name> (AVRCP)` at `/dev/input/eventN`. It is a virtual device, `Bus=0005` (Bluetooth).                          | owner's host triage         |
| F2  | That node is `root:input`, mode `0660`, with no `uaccess` ACL, so the desktop user cannot read it. A udev rule is needed, added through Ansible.                                                          | owner's host triage         |
| F3  | The button arrives as a play/pause media key. GNOME also routes that key to MPRIS players; an MPRIS player was registered at triage time.                                                                 | owner's host triage         |
| F4  | wsi records with `pw-record` from the default source. The default configured source is the laptop microphone, so the headset stays in A2DP and its AVRCP events keep arriving.                            | owner's host triage, `wsi`  |
| F5  | If the headset microphone became the source, the headset would switch to HFP, and presses would arrive as HFP AT commands instead of AVRCP key events.                                                    | owner's host triage         |
| F6  | The device appears and disappears as the headset connects and disconnects, so the listener must handle hotplug.                                                                                           | owner's host triage         |
| F7  | `play-speech-to-text.yml` already deploys one systemd user service, `wsi-stream-server-at-login.service`, from `files/home/.config/systemd/user/`; it is the pattern to follow (see the research doc).    | repository                  |
| F8  | wsi has **no toggle of its own**. A second `wsi` started while one records refuses with exit 1 (the EXT-03 PID-file guard); it does not stop the first.                                                   | `files/home/.local/bin/wsi` |
| F9  | The toggle is the extension's `_launchWSI()` in `extensions/speech-to-text@fedora-desktop/extension.js`, the callback of the `toggle-recording` keybinding. It is not reachable from outside GNOME Shell. | `extension.js`              |

How `_launchWSI()` toggles, why the extension exports no D-Bus method (so D4 is
needed), and the exact user-unit and helper-deploy steps to copy are in
[RESEARCH-toggle-and-service.md](RESEARCH-toggle-and-service.md).

### Hypotheses (to confirm or refute in Phase 1)

- **H1**: bluez maps the AVRCP PLAY and PAUSE passthrough commands to two different key
  codes (`KEY_PLAYCD` 200, `KEY_PAUSECD` 201), and the headset alternates between them
  according to its own idea of play state, so "one press" may not be one key code.
  Confirmed by a `--capture` run showing alternating codes; refuted by a single code
  (for example `KEY_PLAYPAUSE` 164) on every press.
- **H2**: the headset handles a double press itself and sends a different command (most
  likely `KEY_NEXTSONG` 163, next track) rather than two play/pause events; a long press
  may likewise be a voice-assistant or other key. This is the hypothesis that matters
  most, because it decides which branch of D3 applies. Settled by the same capture.
- **H3**: GNOME receives the key through libinput on the same node, so a non-grabbing
  listener and the media player both act on each press, and `EVIOCGRAB` stops GNOME
  from seeing it. Settled in Phase 6 on the real headset.

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: Write `CLAUDE/Plan/00155-headset-button-toggles-speech-to-text/triage.bash`
  on `_planlib.inc.bash`, per [PlanTriage.md](../../PlanTriage.md) and
  [PlanScriptStandards.md](../../PlanScriptStandards.md). Read-only, `probe()` for
  every section, `plan_start_log auto`, `--help` before any environment resolution.
  - [ ] ⬜ Passive report: the `(AVRCP)` entries of `/proc/bus/input/devices`; the
    node's owner, mode and ACL (`getfacl`); `udevadm info --attribute-walk` for the node
    (the attributes a rule can match, and any existing tags); the default PipeWire
    source and the headset card's active profile (A2DP or HFP); the MPRIS names on the
    session bus; the current `toggle-recording` binding.
  - [ ] ⬜ `--capture` leg: under `sudo`, read the AVRCP node without grabbing it for a
    bounded window while the owner performs, in turn, a single press, a double press
    and a long press; record each event's type, code (with its `KEY_*` name) and value
    with timestamps, and for the double press the inter-press interval (release to
    press and press to press) over several repetitions, so the double-press window
    default rests on measured timing. It must show whether a double press arrives as two
    play/pause events or as one `KEY_NEXTSONG` (H2). Uses stdlib `python3` and `struct`, so no new tool; if `evtest` is
    wanted instead it is declared in the play first, never installed by hand. Fails,
    rather than writing an empty section, when no `(AVRCP)` device is present.
  - [ ] ⬜ A `READ THIS FOR:` pointer at the capture section; run QA on the script.
- [ ] ⬜ **Task 1.2**: Add the plan to `CLAUDE/Plan/meta-deploy.bash`'s `PLANS` list
  for the triage run, and tell the owner what it will do (it prompts for the presses).
- [ ] ⬜ **Task 1.3**: Read the report; journal the findings; settle H1 and H2 and
  record the key codes and measured double-press intervals per gesture in a
  `RESEARCH-key-codes.md` supporting document.

### Phase 2: Owner decisions (decision gate)

Nothing in Phases 3 to 6 starts until D1 to D4 are answered. D3 is asked only after
Task 1.3 has recorded what the headset actually sends.

- [ ] ⬜ **Task 2.1 — D1: Grab versus no-grab.** `EVIOCGRAB` is exclusive: with a grab,
  the headset's play/pause would no longer control media, and its other keys would have
  to be re-emitted through uinput (which needs its own access rule for `/dev/uinput`).
  A non-grabbing listener means a press both toggles wsi and plays/pauses media. D3's
  leading candidate (branch A) requires the grab, so answering D3 with it answers D1.
- [ ] ⬜ **Task 2.2 — D2: Which headsets.** Any `(AVRCP)` device, or only a configured
  device-name match (a variable in `host_vars`, defaulting to empty or to all).
- [ ] ⬜ **Task 2.3 — D3: Which key gesture.** A single play/pause press, or something
  else such as a double-press or a long-press. Needs the Task 1.3 probe of which key
  codes the real headset sends (`KEY_PLAYCD`, `KEY_PAUSECD`, `KEY_PLAYPAUSE`, or
  others). **Leading candidate (the owner's stated requirement): a double press toggles
  speech-to-text and a single press keeps doing media play/pause.** Which form it takes
  depends on H2, so the gate branches on the Task 1.3 capture:
  - **Branch A — the double press arrives as two play/pause events.** The listener tells
    single from double by timing. Consequences: it needs `EVIOCGRAB` plus a re-emitted
    play/pause through uinput for a single press (so D1 is grab); every single press is
    delayed by the double-press window before media reacts; the window is configurable
    (a `host_vars` variable, in the 300 to 400 ms range) with its default settled on the
    real headset from the Task 1.3 timings and Task 6.2; the helper needs a press-timing
    state machine, unit-tested with injected timestamps.
  - **Branch B — the headset handles the double press itself and sends `KEY_NEXTSONG`.**
    The listener keys off that code instead of timing presses: no window, no delay to a
    single press, and play/pause can pass through untouched. Open sub-question for the
    owner: whether giving up next-track on that headset is acceptable, and whether to
    grab (so next-track does not also skip the track) or not.
  - **Neither** (for example a double press is swallowed, or arrives as something else):
    the capture is reported and D3 is asked again with what it showed.
- [ ] ⬜ **Task 2.4 — D4: How the listener reaches the toggle (F8, F9).** Options:
  (a) the extension exports a `Toggle` method on `org.fedoradesktop.SpeechToText` that
  calls `_launchWSI()`, and the listener calls it with `gdbus call`: the same code path
  as Insert, every setting honoured, but an extension change that needs a logout to
  load; (b) the listener re-implements the toggle (SIGTERM the live PID, or spawn
  `wsi`): no extension change, but it duplicates the state logic and ignores the
  extension's settings and debounce; (c) the listener injects the bound key with
  `ydotool`: no extension change, but it breaks if the binding changes and is key
  injection. Agent's recommendation, for the owner to confirm or overrule: (a).
- [ ] ⬜ **Task 2.5**: Record each answer under Technical Decisions, and the reasoning
  in the journal.

### Phase 3: Listener helper (TDD)

Tests first: the hooks daemon blocks a source file whose test does not yet exist.
Stdlib only: `struct` for `input_event`, `fcntl` for `EVIOCGRAB`; `python3-evdev` only if
a play installs it.

- [ ] ⬜ **Task 3.1**: Pure core (`helpers/headset_button/core.py`, tests in
  `tests/helpers/headset_button/test_core.py`): decode `input_event` records
  (`struct` format `llHHi` on 64-bit); select AVRCP devices from
  `/proc/bus/input/devices` text per D2; a gesture recogniser per D3 that turns key
  events into "toggle" or "pass through", covering H1's alternating codes and key
  repeat. Under D3 branch A this is a press-timing state machine with the window as a
  parameter and the clock injected, tested with injected timestamps: a lone press emits
  play/pause once the window expires, two presses inside the window emit one toggle and
  no play/pause, a third press and presses straddling the window boundary are covered.
  Under branch B it maps `KEY_NEXTSONG` to the toggle.
- [ ] ⬜ **Task 3.2**: Thin executor (`helpers/headset_button/cli.py`): open each
  matching node, apply `EVIOCGRAB` per D1 (and the uinput re-emission if D1 is grab,
  including the delayed play/pause of a single press under D3 branch A), read the
  double-press window from its configuration,
  call the toggle per D4, and handle hotplug: a node that vanishes is closed without
  error and the device is picked up again on reconnect. Choose and record the hotplug
  mechanism (a rescan of `/sys/class/input`, or a udev `SYSTEMD_USER_WANTS` template
  unit per device) with the evidence from Task 1.1's attribute walk. Diagnostics to
  stderr per [StderrHygiene.md](../../StderrHygiene.md).
- [ ] ⬜ **Task 3.3**: Run `./scripts/qa-helper-tests.bash` and `./scripts/qa-all.bash`;
  fix all findings.

### Phase 4: udev rule and user service in the play

- [ ] ⬜ **Task 4.1**: A udev rule under `files/etc/udev/rules.d/` that tags the
  headset's AVRCP event node `uaccess` (matching on `Bus=0005` and the name per D2), so
  the active seat user gets an ACL; numbered before `73-seat-late.rules` so the tag is
  applied. Not the `input` group, which would expose every keyboard.
- [ ] ⬜ **Task 4.2**: In `play-speech-to-text.yml`: deploy the rule, reload udev rules
  and re-trigger the input subsystem, deploy the helper modules (explicit list) and the
  `~/.local/bin` wrapper, and the user unit under `files/home/.config/systemd/user/`,
  enabled, reloaded and read back from the live manager as F7 describes.
- [ ] ⬜ **Task 4.3**: If D4 is (a): the extension's `Toggle` D-Bus method, ESLint run
  and the extension version gate satisfied, and a note that a logout is needed.
- [ ] ⬜ **Task 4.4**: `deploy.bash` and `acceptance.bash` in the plan folder (R1 to
  R14); acceptance prints `COVERAGE: n of m` and names a real button press as NOT
  ESTABLISHABLE for a script. Add the plan to `meta-deploy.bash`.
- [ ] ⬜ **Task 4.5**: Run `./scripts/qa-all.bash`; fix all findings.

### Phase 5: Documentation

- [ ] ⬜ **Task 5.1**: A "Headset button" section in `docs/features/speech-to-text.md`:
  what one press does, which headsets and gesture (D2, D3), the media-control
  consequence (D1), the HFP limitation (F5), and troubleshooting (no `(AVRCP)` device,
  node not readable, service status). Add the button to the Keyboard Shortcuts and
  Architecture / File Locations sections.

### Phase 6: Verification on the real headset

- [ ] ⬜ **Task 6.1**: Owner runs `meta-deploy.bash` (deploy, then acceptance), logging
  out first if Task 4.3 changed the extension.
- [ ] ⬜ **Task 6.2**: On the real headset: the D3 gesture starts a recording, the next
  one stops it and the text arrives as with Insert; the media player behaves as D1 chose
  (settles H3); under D3 branch A, a single press still plays/pauses after the window,
  and the window default is settled here and recorded; disconnect and reconnect, then
  press again, with no service restart; the user service is active after a fresh login.
- [ ] ⬜ **Task 6.3**: Run the `qa-reviewer` agent over the plan's full diff and resolve
  every BLOCK and FIX-BEFORE-MERGE finding.

## Dependencies

- Related: Plan 00148 (the general speech-to-text improvements plan, In Progress). This
  plan touches the same play and extension; the two must not edit
  `play-speech-to-text.yml` or `extension.js` on diverging branches at once.

## Technical Decisions

D1 to D4 are open; see Phase 2. Each answer is recorded here as it is given.

## Success Criteria

- [ ] With the headset connected in A2DP, the D3 gesture (leading candidate: a double
  press) starts a recording and the next one stops it, through the same toggle as
  Insert, while a single press still plays/pauses media.
- [ ] Reconnecting the headset needs no restart and no playbook run.
- [ ] The desktop user reads the AVRCP node through the deployed udev rule, not group
  membership.
- [ ] Media keys behave as D1 decided, and the docs say so.
- [ ] Helper tests pass in `./scripts/qa-helper-tests.bash`; `./scripts/qa-all.bash`
  passes; `acceptance.bash` passes with full coverage.
- [ ] The `qa-reviewer` agent reports no unresolved BLOCK or FIX-BEFORE-MERGE finding.

## Risks & Mitigations

| Risk                                                                         | Impact | Probability | Mitigation                                                                  |
| ---------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------- |
| The headset alternates PLAY and PAUSE codes (H1), so one code misses presses | M      | M           | Capture first (Task 1.1); the recogniser treats both codes as one press     |
| A non-grabbing listener also plays/pauses media on every toggle              | M      | H           | D1 is the owner's call; documented either way                               |
| A grab breaks the headset's other keys if uinput re-emission is wrong        | M      | M           | Re-emission tested in the helper; verified on the real headset in Task 6.2  |
| A udev rule matching too widely grants access to other input devices         | H      | L           | Match on `Bus=0005` and the `(AVRCP)` name; acceptance checks no other node |
| Under D3 branch A, every single press reaches media late by the window       | M      | H           | Window configurable (300 to 400 ms), default settled on the real headset    |
| The headset turns a double press into `KEY_NEXTSONG` itself (H2)             | M      | M           | Triage first; D3 branch B keys off that code instead of timing              |
| Selecting the headset microphone switches to HFP and the button goes silent  | L      | M           | Documented limitation (F5); out of scope                                    |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00155-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed; awaiting Phase 1 triage and the Phase 2 decisions.
