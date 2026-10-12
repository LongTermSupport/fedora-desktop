# Research: the headset button while the headset is in HFP

Question: is there a supported way for a user-space listener to learn "the headset button
was pressed" while a dictation holds the headset in HFP, when the press arrives only as
`AT+CHUP` on RFCOMM? The answer must also be scoped to the configured dictation devices
only. Every other headset keeps the default behaviour.

Versions on the host (`rpm -q`): pipewire 1.6.9-1.fc44, wireplumber 0.5.18-1.fc44,
bluez 5.87 (git snapshot build), ofono not installed (2.19 is in the Fedora 44 repo).
Upstream source was read at tag `1.6.9` from the PipeWire GitHub mirror (gitlab.freedesktop.org
blocks automated fetches), under `spa/plugins/bluez5/`. The working copies are in
`untracked/scratch/pw-src-00155/`.

## Root cause: PipeWire sends the headset a fake "call active"

In `backend-native.c`, `sco_acquire_cb()` runs whenever the SCO audio link opens, i.e. when
a recording starts in HFP. It calls `rfcomm_hfp_ag_set_cind(rfcomm, true)`, which sends
`+CIEV: 2,1`, the HFP "call = active" indicator. `sco_release_cb()` sends `+CIEV: 2,0`.
Both calls are guarded by `if (!td->rfcomm->device->disable_dummy_call)`.

So while a dictation runs, the headset believes a phone call is in progress, and its
button does what a button does during a call: it sends `AT+CHUP` (hang up). That explains
the journal finding. In A2DP there is no call, so the same button sends AVRCP `PLAY`.

What happens to `AT+CHUP` in `rfcomm_hfp_ag()` (verbatim logic, 1.6.9):

```c
} else if (!mm_is_available(backend->modemmanager)) {
        spa_log_info(backend->log, "RFCOMM receive command but modem not available: %s", buf);
        rfcomm_send_error(rfcomm, CMEE_NO_CONNECTION_TO_PHONE);
        return true;
```

The Fedora build has no ModemManager support. `strings` on `libspa-bluez5.so` finds neither
`spa.bluez5.modemmanager` nor `bluez5.hfphsp-backend-native-modem`, so `modemmanager.c`
was not compiled and the stub `mm_is_available()` always returns `false`. Every call-control
command (`ATA`, `ATD`, `AT+CHUP`, `AT+VTS`) is therefore logged at **info** and answered with
`ERROR`. Nothing else is emitted: no D-Bus signal, no SPA event, no WirePlumber event.

## 1. PipeWire telephony (`org.pipewire.Telephony`): does not apply

- **Present in 1.6.9, and enabled by default.** The configuration keys are
  `bluez5.telephony-dbus-service`, `bluez5.telephony.use-system-bus`,
  `bluez5.telephony.provide-ofono` and `bluez5.telephony.default-reject-sco`
  (`telephony.c` around line 424; all four strings are in the installed `.so`). The name
  `org.pipewire.Telephony` is registered on the session bus here (`busctl --user list`).
- **It covers the opposite role.** Its objects are `org.pipewire.Telephony.AudioGateway1`,
  `AudioGatewayTransport1` and `Call1`. They let a desktop dialler control a **remote phone**
  while the PC is the hands-free unit. `telephony_ag_new()` is called only from
  `rfcomm_hfp_hf()`, the HF-role path for a connected phone. Our case is the reverse: the PC is
  the audio gateway and the headset is the HF, so the code runs `rfcomm_hfp_ag()`, which
  never touches telephony.
- **Verified locally:** `GetManagedObjects` on `/org/pipewire/Telephony` returns
  `a{oa{sa{sv}}} 0` with the headset connected. There are no AG or call objects.
- **A fake call does not help.** `Call1` objects exist only for calls on a remote phone.
  There is no API to create a local call that the headset could hang up.

## 2. Other hooks

- **WirePlumber Lua or event hooks:** none. WirePlumber never sees AT commands, which are
  parsed entirely inside the SPA plugin. `grep` over `/usr/share/wireplumber` and
  `libwireplumber` finds no bluez AT, call or dummy handling.
- **Log lines:** the `AT+CHUP` line is logged at **info** on topic `spa.bluez5.native`, as
  `RFCOMM receive command but modem not available: AT+CHUP`. `RFCOMM << ...` and
  `RFCOMM event: ...` are logged at debug. The documented static way to raise only that
  topic is
  `context.properties = { log.level = "<global>,spa.bluez5.native:I" }`; WirePlumber 0.5's
  logging docs say topic globs apply to `spa.*`. `wpctl set-log-level` documents only a
  global level, set through the `settings` metadata.
  - Why the earlier global-D test produced no `spa.bluez5` lines: **unverified.** One
    possibility is that the runtime level does not reach SPA topics that were initialised
    when the plugin loaded.
  - The log line names no device. The listener could tell which headset pressed only by
    knowing that a single HFP device is active. Log text is not an API either.
- **`bluez5.hfphsp-backend = ofono | hsphfpd | native | none`:** this is a monitor
  property, so it changes every headset.
  - `ofono` makes oFono the audio gateway. oFono forwards call-control commands to a real
    modem, and there is none here.
  - `hsphfpd` is an unreleased daemon; `dnf repoquery` returns no package.
  - `none` turns HFP off for every headset, which removes the headset microphone.
  - None of these exposes a "button pressed" event. All are rejected.

## 3. AT+BVRA (voice recognition)

PipeWire's AG never advertises the voice-recognition feature. `+BRSF` is built from
`SPA_BT_HFP_AG_FEATURE_HF_INDICATORS | ESCO_S4 | CODEC_NEGOTIATION | mm_supported_features()`,
and the stub returns 0. `defs.h` defines no AG voice-recognition bit. The HFP spec says an HF
sends `AT+BVRA` only when the AG supports it, so a long press is unlikely to send it. If one
did arrive, it is not in `rfcomm_hfp_ag()`'s table and would get debug
`RFCOMM received unsupported event` and `ERROR`. Not surfaced.

Whether the OpenRun Pro long press sends anything over HFP: **unverified, not captured yet.**

`AT+CKPD=200`, the HSP button, is handled in `rfcomm_hsp_ag()` and triggers
`spa_bt_device_emit_switch_profile` internally. That covers HSP only, not HFP.

## Recommended: per-device `bluez5.disable-dummy-call = true`

Upstream added this for "headsets used by an application other than telephony"
(commit `25a6fdcdb1`, 2025-11-28, "spa: bluez: device: Add SPA_PROP_params to disable dummy
call state"; documented in commit `04cf29f7cd`). It is in 1.6.9. The installed
`pipewire-props(7)` documents it, and the device's `Props` already show
`bluez5.disable-dummy-call = false`.

With it set, PipeWire stops sending the fake call indicator. The headset keeps its idle
button mapping in HFP, which is expected to be AVRCP `PLAY`, i.e. `KEY_PLAYCD`, the same
event the listener already reads in A2DP.

- **Per-device scope works.** The key is read by `bluez5-device.c` `impl_init()` from the
  **device** properties, and also accepted at runtime as a device `Props` `params` entry. It
  is not read from the monitor properties. Despite the man page listing it under monitor
  properties, the right place is `monitor.bluez.rules` matching `bluez_card` devices.
  WirePlumber's `monitors/bluez/name-device.lua` applies those rules after
  `device.description` and `device.alias` are filled in, and `create-device.lua` passes the
  result to `SpaDevice`.
- **Matchable properties** (from `pw-dump`): `device.alias` and `device.description` carry
  the Bluetooth name, here `OpenRun Pro by Shokz`. `api.bluez5.address` would also match,
  but addresses must not go in the repo.
- **Other headsets are unaffected.** They do not match the rule, so they keep the dummy
  call, the default HFP behaviour, and their media buttons.

Drop-in: add one rule per dictation device to the `monitor.bluez.rules` array in
`51-bluetooth-quality.conf`, generated from the host_vars list. In SPA-JSON, keys inside one
`matches` object are combined with AND.

```
    {
      matches = [
        { device.name = "~bluez_card.*", device.alias = "OpenRun Pro by Shokz" }
      ]
      actions = {
        update-props = {
          bluez5.disable-dummy-call = true
        }
      }
    }
```

Caveats:

1. **The headset's HFP-idle press mapping is unverified.** A press could send AVRCP
   `PLAY`, `AT+BVRA`, or nothing. This must be measured before it is built: a plan-local
   triage script sets the property at runtime while the headset is in A2DP with no recording
   running, starts a recording, and the owner presses the button while `btmon` and the
   evdev reader run. The runtime command, with the syntax unverified, would be
   `pw-cli set-param <device-id> Props '{ params = [ "bluez5.disable-dummy-call", true ] }'`.
2. **Set it only while HFP is idle.** If the property flips while SCO is open, release
   skips `+CIEV: 2,0`, and the headset may keep a phantom call until it reconnects. A
   static rule applied at connect avoids this.
3. **Possible disconnects.** Upstream issue pipewire#1744 is why the dummy call exists:
   some headsets drop SCO after a timeout when no call is indicated. Test a long dictation
   of several minutes.
4. **GNOME also receives `KEY_PLAYCD`** and toggles the media player during a dictation, as
   it already does in A2DP. That is a separate design point.

**Listener contract:** PipeWire offers no D-Bus contract for this, and none is needed. The
listener stays evdev-only: `KEY_PLAYCD` on the BlueZ `(AVRCP)` uinput node, filtered to the
dictation devices by node name. Whether that node is named `<alias> (AVRCP)` is unverified.

## Fallbacks, in order of cost

| Route                                                                                                     | Cost                                                                                                                        | Per-device                                                                                  |
| --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| A. Scrape the `spa.bluez5.native` info line from the WirePlumber journal (keeps the dummy call)           | Log text is not an API and may change in any release; more WirePlumber logging; the line names no device; latency           | Behaviour is unchanged for all headsets. Attribution relies on one HFP device being active. |
| B. Root btmon-style service on the HCI monitor channel (`CAP_NET_RAW`/root), parsing RFCOMM for `AT+CHUP` | Root daemon; parses every Bluetooth packet, a privacy and attack surface; reassembles RFCOMM; feeds a user session over IPC | Yes, by filtering on the address at runtime, read from config rather than the repo.         |
| C. Avoid HFP during the stop                                                                              | Not viable, because the microphone must stay open                                                                           | —                                                                                           |
| D. Global backend change (`ofono`/`none`)                                                                 | Breaks or alters HFP for every headset, and no button event is exposed anyway                                               | No                                                                                          |

## Sources

- PipeWire 1.6.9, `spa/plugins/bluez5/`: `backend-native.c` (`rfcomm_hfp_ag`, `rfcomm_hsp_ag`,
  `sco_acquire_cb`, `sco_release_cb`, `rfcomm_process_events`), `telephony.c`,
  `modemmanager.h` (stubs), `bluez5-device.c` (`disable-dummy-call`), `meson.build`.
- Upstream commits `25a6fdcdb1` and `04cf29f7cd` (via `gh api repos/PipeWire/pipewire/commits/...`).
- `man pipewire-props` (installed), section `bluez5.disable-dummy-call`.
- WirePlumber logging docs: <https://pipewire.pages.freedesktop.org/wireplumber/daemon/logging.html>
- Local, read-only: `rpm -q`, `strings` on `libspa-bluez5.so`, `busctl --user list/tree/introspect/call`,
  `pw-dump`, `/usr/share/wireplumber/scripts/monitors/bluez/*.lua`, `dnf repoquery`.
