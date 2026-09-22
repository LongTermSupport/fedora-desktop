# Research — what the startup logs said after the kernel/firmware update reboot

Evidence gathered on the host from `journalctl -b` (system + user), `coredumpctl`,
`abrt-cli list`, `systemctl --failed`, `gnome-extensions info`, and the repo. All
identifying detail (hostname, users, project and container names, paths outside the
repo) has been removed. Raw captures stay under `untracked/scratch/boot-triage/`.

## Headline: nothing crashed during this boot

- `coredumpctl list` since boot: none. `systemctl --failed` (system and user): none.
- `gnome-shell` JS ERROR count this boot: 0. Every enabled extension reports `ACTIVE`.
- `journalctl -b -p 3` holds seven lines, none of them a crash (see the noise table).

So the "random crash error" notifications and the panel icon are **reports about
earlier state**, delivered at login by two independent mechanisms.

## F1 — the blue diamond-with-question-mark panel icon is the fedora-desktop status panel

`extensions/fedora-desktop@fedora-desktop/extension.js` maps
`StatusDocument.UNAVAILABLE` to `dialog-question-symbolic` tinted `#8ab4f8` (blue).
The menu body is the host-health findings document, and the long text is the
`play-freshness` diagnostic, joined with `; ` into a single line by
`helpers/host_health/login_report.py` (`freshness_findings`):

```
play-freshness could not give an answer, so no play was judged: play-freshness: the
ledger is marked BROKEN and cannot be trusted, so no play was judged.;   reason: <ts>
ValueError: the play carries no source position, so its file cannot be named; the
ledger will not record a play it cannot identify;   clear it deliberately once the
cause is fixed; it never clears itself:;     cd
```

Notice the remedy is truncated at `cd` — the multi-line stderr hint was flattened and
then cut, so the one actionable part (the clear command) never reaches the reader. The
same text is what `host-health.service` sends through `notify-send` at graphical login,
which is the "big long error message" notification. Neither the notification body nor
the panel menu item can be selected or copied (`St.Label` in a PopupMenu). The
`container-watch` extension already solves this with an explicit "copy" menu action
using `St.Clipboard`.

## F2 — ROOT CAUSE of the BROKEN ledger: any ad-hoc `ansible` run marks it broken

`ansible.cfg` sets `bin_ansible_callbacks = true` and enables the `play_ledger`
callback, so the callback also runs for `ansible <pattern> -m <module>` invocations.
An ad-hoc play is built by `ansible.cli.adhoc` from a plain dict
(`Play().load(play_ds, …)`), so it carries no `Origin` tag and, on ansible-core
≥ 2.19, no `_ds.ansible_pos` either. `plugin_support.play_source(None)` raises
`ValueError("the play carries no source position…")`, the callback catches it and
calls `store.mark_broken`, and the sentinel stays until an operator clears it.

Reproduced deterministically with a throwaway state dir, no change to the host:

```
$ XDG_STATE_HOME=$(mktemp -d) ansible localhost -m ping
LEDGER-WRITE-FAILED: ValueError: the play carries no source position, … — recorded in
  <tmp>/fedora-desktop/play-ledger/BROKEN. …
PLAY [Ansible Ad-Hoc] ***
ok: [localhost]
```

The sentinel on this host carries the same reason, timestamped the day before the
reboot; `runs.jsonl` has no record around that time, which is exactly what an ad-hoc
run leaves behind. The panel had been showing this since that moment — the reboot
merely re-delivered it as a fresh notification.

Ad-hoc plays are not something the ledger can or should track: the ledger's unit is
*a play file at a commit*, and an ad-hoc play has no file. Treating "no origin" as a
recorded hole is correct for a *playbook* play and wrong for an ad-hoc one.

## F3 — the crash notifications are ABRT re-announcing an unreported backlog

`abrt-cli list` holds 79 problem directories, 64 never reported, going back about
five months (browser/webkit renderers, container runtimes, a scripting runtime,
editors). Seven new ones landed the afternoon before the reboot: one CLI interpreter
binary crashing repeatedly under a user project's console command. `abrt-applet`
also logs `g_app_info_should_show: assertion 'G_IS_APP_INFO (appinfo)' failed` eight
times at login — one per record whose executable has no desktop entry, which is why
those notifications show a generic icon and no app name. Nothing in the repo manages
ABRT (no play mentions it): there is no retention policy, no auto-reporting decision,
and the applet is the Fedora default.

## F4 — WirePlumber Lua configuration written by `play-hd-audio.yml` is ignored

```
wireplumber: Lua configuration files are NOT supported in WirePlumber 0.5. You need to
port them to the new format if you want to use them.
```

`playbooks/imports/optional/common/play-hd-audio.yml` writes
`~/.config/wireplumber/main.lua.d/99-hd-audio.lua` (ALSA rate switching, period size,
suspend timeout) and `bluetooth.lua.d/99-bluetooth-quality.lua` (codecs, mSBC, LDAC
quality, auto-connect). WirePlumber 0.5 loads only SPA-JSON `*.conf` under
`wireplumber.conf.d/`, so every one of those settings has silently done nothing since
the 0.5 upgrade. The play still restarts WirePlumber on change, so it *looks* applied.

## F5 — Vivaldi's repo id is defined twice

`dnf5daemon-server: ERROR Failed to create repo "vivaldi": Id is present more than once`.
`play-browsers.yml` adds `/etc/yum.repos.d/vivaldi-fedora.repo` (`creates:` guarded),
and the Vivaldi RPM's own post-install drops `/etc/yum.repos.d/vivaldi.repo` with the
same `[vivaldi]` id (both files unowned by any package; they differ only in
`baseurl` spelling). Every dnf5daemon/GNOME Software refresh fails to build that repo.

## F6 — `vmtest-bridge@.service` declares a cap that systemd ignores

```
vmtest-bridge@….service: RuntimeMaxSec= has no effect in combination with Type=oneshot. Ignoring.
```

`files/home/.config/systemd/user/vmtest-bridge@.service` sets `Type=oneshot` and
`RuntimeMaxSec=120` — the comment says it "caps the drain". It caps nothing. The two
host-health units in the same directory already document the correct spelling
(`TimeoutStartSec=`, "systemd disables the start timeout for Type=oneshot").

## F7 — SELinux denial flood from containers against bind-mounted checkouts

Over the two days before the reboot the audit log recorded ~2.7 million AVC denials
with `scontext=system_u:system_r:container_t` against `user_home_t` targets — the
hooks-daemon runtime dirs (`thread-registry`, `context-sidecar`), a status-line hook,
`.git/index`, a web framework's `var/log`, `http-cache`. None this boot yet (no
container has started). The CCY launcher relabels the workspace with `:z` when
SELinux is enforcing (`lib/common.bash: ccy_selinux_mode`), yet this checkout's tree is
labelled `user_home_t` throughout, so either the relabel is not reaching some sessions
(e.g. those launched with `--security-opt label=disable` do not deny — so these are
labelled sessions) or paths created on the host after the relabel are reverting. This
was not diagnosed here; it needs its own triage probe (which containers, which mounts,
what label the host sees on each denied path).

## F8 — small, real, repo-owned

| Log line                                                                                   | Owner                                                                                                                          | Note                                                                                                                                                               |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `~/.config/autostart/jetbrains-toolbox.desktop is marked executable. Please remove…`       | `play-toolbox-install.yml` (the app writes the file itself with the exec bit; the play never normalises it)                    | `systemd-xdg-autostart-generator` proceeds anyway; a mode 0644 task makes it quiet and idempotent                                                                  |
| firewalld: ~60 `COMMAND_FAILED: iptables … DOCKER…` + `NAME_CONFLICT: 'docker-forwarding'` | Docker 29 uses the nftables backend; firewalld still tears down legacy DOCKER chains; docker re-registers its firewalld policy | Noise per boot — BUT `lxc-docker-user-iptables-reconcile.bash` edits the iptables `DOCKER-USER` chain, which the nftables backend may not consult. Needs verifying |
| dbus-broker: duplicate `org.freedesktop.FileManager1` from a Thunar service file           | `Thunar` is installed and no package requires it; no play installs it                                                          | Orphan package on a GNOME host; every file-manager D-Bus activation logs the conflict                                                                              |

## Reviewed and judged not actionable (so the next triage does not redo this)

| Line                                                                            | Why it is left alone                                                       |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `irqbalance: Cannot change IRQ NNN affinity: Permission denied` ×22             | Kernel-managed MSI-X vectors refuse userspace affinity; upstream behaviour |
| `NVRM … failed to get target temp/platform power mode from SBIOS`               | Firmware/driver assertion on a hybrid laptop; no repo-side control         |
| `Couldn't find suitable cursor plane format … disabling HW cursor`              | Mutter + proprietary driver; cosmetic                                      |
| `gsd-usb-protection: Failed to fetch USBGuard parameters`                       | USBGuard is not installed; GNOME probes it unconditionally                 |
| `gsd-media-keys: Failed to grab accelerator … hibernate / playback-repeat`      | Standard GNOME on a host without hibernate                                 |
| `malcontent-timerd` assertions, `MalcontentTimer1 … Invalid or unknown user`    | Parental-controls daemon with no configured child user; Fedora default     |
| `pipewire: mod.raop-sink: sess.latency.msec … should be an integer multiple`    | Default RAOP module config; no AirPlay sink in use                         |
| `spi-nor: unrecognized JEDEC id`, `TDX not supported`, PCIe `retraining failed` | Hardware/firmware probes at early boot                                     |
| `dash-to-dock` / `blur-my-shell` `st_widget_get_theme_node … not in the stage`  | Third-party extension warnings on enable; no functional effect observed    |
| `dnf5daemon: Sync check failed for repo "updates", sha256 checksum mismatch`    | Transient mirror skew at refresh time                                      |
| `gdm-password: gkr-pam: unable to locate daemon control file`                   | Keyring not yet started at PAM time; keyring unlocked fine afterwards      |
