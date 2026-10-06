# Research: Element Desktop profiles and terminal Matrix clients (Plan 00161, Task 1.1)

Scope: issue #59 section 9 (desktop viewing) and the privacy check "no outbound traffic from
the Element team profile". Sources are listed at the end; every claim below is sourced, and
every claim not yet proven on a real machine is marked **VERIFY**.

## 1. Element Desktop: profiles and where config.json is read

### 1.1 The `--profile` flag

- `element-desktop --profile <name>` runs a separate instance with its own data
  (README, "Profiles"). `--profile-dir <path>` or `ELEMENT_PROFILE_DIR` set a custom data
  location instead.
- Code (`src/electron-main.ts`): `newUserDataPath = process.env.ELEMENT_PROFILE_DIR ?? app.getPath("userData"); if (argv["profile"]) newUserDataPath += "-" + argv["profile"];`
  So the profile directory is the default userData directory with `-<name>` appended.
- The user `config.json` is read from that userData directory
  (`loadLocalConfigFile`: `configDir = app.getPath("userData")`), unless `--config <file>` or
  `ELEMENT_DESKTOP_CONFIG_JSON` names a file.
- README: on Linux the file is `$XDG_CONFIG_HOME/$NAME/config.json`, where `$NAME` is
  `Element`, or `Element-$PROFILE` with `--profile $PROFILE`.
- Electron's single-instance lock is per userData directory, so one window per team runs
  side by side with the user's normal Element. **VERIFY** on the host (two profiles at once).

### 1.2 Inside the Flathub Flatpak (`im.riot.Riot`)

- The Flathub repo's README says the config goes in
  `~/.var/app/im.riot.Riot/config/$NAME/config.json`, because the sandbox sets
  `XDG_CONFIG_HOME` to the app's own `~/.var/app/<id>/config`.
- So a team profile's config is
  **`~/.var/app/im.riot.Riot/config/Element-<team>/config.json`**, and its data (store,
  `electron-config.json`, IndexedDB) lives in the same directory. That follows from 1.1 plus
  the README path. **VERIFY** that the directory appears after one
  `flatpak run im.riot.Riot --profile <team>` (the issue's open "to verify" point).
- The wrapper `element.sh` ends `... zypak-wrapper /app/Element/element-desktop $FLAGS "$@"`,
  so `flatpak run im.riot.Riot --profile <team>` passes the flag through.
- Manifest finish-args include `--share=network` (the host network namespace) and no
  home filesystem access. Two consequences: the sandbox reaches a homeserver bound to
  `127.0.0.1` or to a host bridge address. And a play can write the profile `config.json`
  from the host side into `~/.var/app/im.riot.Riot/config/Element-<team>/` with no
  `flatpak override`. `--config <host path>` would need a filesystem override, so use the
  profile directory instead.
- The Flatpak repackages the upstream tarball from `packages.element.io` (version 1.12.29 at
  the time of writing). That tarball bundles the **element.io release `config.json`**. This
  matters for 1.3.

### 1.3 How the configs merge: why omitting a key is NOT enough

There are two merge layers, and each one changes what "switch it off" has to mean:

1. **Desktop layer, shallow:** `global.vectorConfig = Object.assign(bundled, local)`.
   A top-level key in the profile `config.json` replaces the bundled one wholesale. **A key
   the profile omits keeps the bundled element.io value.** If the local config sets any of
   `default_is_url`, `default_hs_url`, `default_server_name` or `default_server_config`,
   all four bundled homeserver keys are removed first.
2. **Web layer, deep:** `SdkConfig.put` → `mergeConfig(DEFAULTS, cfg)` uses lodash
   `mergeWith`. The customiser returns the old value when the old value is an object and
   the new one is not: **an object default cannot be nulled.** String defaults can be set
   to `null`.

The bundled element.io config turns on, among others: `update_base_url`, a matrix.org
homeserver plus the `vector.im` identity server, Scalar integrations (`integrations_*`),
`bug_report_endpoint_url` (rageshakes.element.io), a `room_directory` listing matrix.org and
gitter.im, `posthog` (posthog.element.io), `map_style_url` (maptiler), `element_call.url`
(call.element.io) and the `features` flags for video rooms and group calls.
`terms_and_conditions_links` and `privacy_policy_url` are only links. The web `DEFAULTS` add
`integrations_ui_url` and `integrations_rest_url` (Scalar), `jitsi.preferred_domain: "meet.element.io"` (an object, so it cannot be nulled) and
`enable_client_well_known_lookups: true`.

**So the team profile must override every one of these explicitly.**

### 1.4 Proposed profile `config.json` (placeholders in `<>`)

```json
{
  "default_server_config": {
    "m.homeserver": { "base_url": "http://<host-local-address>:<port>", "server_name": "<server_name>" }
  },
  "disable_custom_urls": true,
  "disable_guests": true,
  "disable_3pid_login": true,
  "disable_login_language_selector": true,
  "enable_client_well_known_lookups": false,
  "update_base_url": null,
  "integrations_ui_url": null,
  "integrations_rest_url": null,
  "integrations_widgets_urls": [],
  "bug_report_endpoint_url": null,
  "posthog": null,
  "sentry": null,
  "privacy_policy_url": null,
  "terms_and_conditions_links": [],
  "map_style_url": null,
  "jitsi": { "preferred_domain": "<host-local-address>" },
  "element_call": { "disable": true },
  "features": {
    "feature_video_rooms": false,
    "feature_group_calls": false,
    "feature_element_call_video_rooms": false
  },
  "room_directory": { "servers": ["<server_name>"] },
  "show_labs_settings": false,
  "mobile_guide_toast": false,
  "setting_defaults": {
    "UIFeature.urlPreviews": false,
    "UIFeature.voip": false,
    "UIFeature.widgets": false,
    "UIFeature.locationSharing": false,
    "UIFeature.identityServer": false,
    "UIFeature.thirdPartyId": false,
    "UIFeature.registration": false,
    "UIFeature.passwordReset": false,
    "UIFeature.deactivate": false,
    "UIFeature.feedback": false,
    "UIFeature.shareSocial": false,
    "UIFeature.allowCreatingPublicRooms": false,
    "UIFeature.allowCreatingPublicSpaces": false,
    "fallbackICEServerAllowed": false
  }
}
```

What each key stops, and why it is written this way:

| Concern               | Key(s)                                                                                           | Note                                                                                                                                                                                                                 |
| --------------------- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Pin homeserver        | `default_server_config` and `disable_custom_urls`                                                | Setting `default_server_config` also strips the bundled matrix.org and vector.im keys (1.3). No `m.identity_server` is given, so there is no identity server.                                                        |
| `.well-known` lookups | `enable_client_well_known_lookups: false`                                                        | Without it, Element fetches `https://<server_name>/.well-known/...`: a DNS lookup of the team's name, which the issue forbids ("names are not published"). Added in Element Web v1.12.28, and Flathub ships 1.12.29. |
| Identity server       | no `m.identity_server`, and `UIFeature.identityServer` / `thirdPartyId` set false                |                                                                                                                                                                                                                      |
| Integration manager   | `integrations_ui_url` and `integrations_rest_url` set null, `integrations_widgets_urls` set `[]` | Strings can be nulled. Arrays are replaced, not merged.                                                                                                                                                              |
| Jitsi                 | `jitsi.preferred_domain` set to the host-local address, `UIFeature.voip`/`widgets` set false     | `jitsi` is an object default and **cannot be nulled** (1.3). Pointing it at the host means an accidental use stays on the machine.                                                                                   |
| Element Call          | `element_call: {"disable": true}` and the three `features` set false                             | This replaces the bundled `element_call.url`, because the desktop merge is shallow.                                                                                                                                  |
| TURN fallback         | `fallbackICEServerAllowed: false`                                                                | Otherwise a failed call offers the public fallback STUN server. This is the setting's name in Element's settings code. **VERIFY** it is still honoured.                                                              |
| Maps                  | `map_style_url: null`, `UIFeature.locationSharing: false`                                        | A homeserver `.well-known` `m.tile_server` would override it, and Tuwunel serves none.                                                                                                                               |
| Analytics             | `posthog: null`, `sentry: null`                                                                  | Desktop runs `Sentry.init` only with a configured DSN.                                                                                                                                                               |
| Rageshake             | `bug_report_endpoint_url: null`                                                                  |                                                                                                                                                                                                                      |
| URL previews          | `UIFeature.urlPreviews: false`                                                                   | Previews are fetched by the homeserver anyway, and Tuwunel denies them by default.                                                                                                                                   |
| Update check          | `update_base_url: null`                                                                          | On Linux `updater.start()` returns early anyway ("Squirrel / electron only supports auto-update on these two platforms"). Nulling it costs nothing.                                                                  |
| Room directory        | `room_directory.servers` set to the team only                                                    |                                                                                                                                                                                                                      |

**Spell-check dictionaries: a leak that config.json cannot stop.** Element Desktop
calls `session.setSpellCheckerEnabled(store.get("spellCheckerEnabled", true))`. On Linux,
Chromium's spellchecker downloads Hunspell dictionaries from Google's CDN
(`redirector.gvt1.com`), which has been reported about 150 ms after an Electron app starts.
The setting lives in the Electron store `electron-config.json` in the profile directory
(`src/store.ts`: `name: "electron-config"`, key `spellCheckerEnabled`), not in `config.json`.
Options: pre-seed `{"spellCheckerEnabled": false}` in a new profile's `electron-config.json`,
or turn it off once in Settings. **VERIFY** whether pre-seeding works (the store may be
encrypted or schema-checked). Either way the privacy acceptance check must capture DNS and
traffic for this case.

**Plain HTTP to a bridge address.** The desktop origin is `vector://vector`. A deployment
report says Element Desktop accepts a plain-HTTP LAN homeserver, although Element *Web*
needs a secure context. **VERIFY** with a bridge IP. If it fails, the fallback is
`http://127.0.0.1:<port>`, which is a potentially trustworthy origin and also host-local.

### 1.5 A launcher per profile

The Flatpak exports `im.riot.Riot.desktop`. Add one user entry per team, for example
`~/.local/share/applications/agent-team-<team>-element.desktop`:

```ini
[Desktop Entry]
Type=Application
Name=Element (<team> team bus)
Comment=Watch and command the <team> agent team
Exec=flatpak run im.riot.Riot --profile <team>
Icon=im.riot.Riot
Terminal=false
Categories=Network;InstantMessaging;
```

All profiles share the WM class, so GNOME groups their windows under one dock icon. That is
cosmetic. The profile name is used in a path, so restrict it to `[a-z0-9-]`.

## 2. How this repo installs Flatpaks and launchers

- **Flatpak:** `playbooks/imports/play-comms.yml` and `optional/common/play-videography.yml`
  install system-wide with `become: true`: `community.general.flatpak_remote` for flathub,
  then `community.general.flatpak` (`remote: flathub`, `state: present`). Overrides go
  through `flatpak override` with a read-then-guard pair, because there is no module for
  them (play-comms, Slack `home:ro`). Element needs no override.
- **Launchers:** `optional/common/play-unifi-controller.yml` writes a user launcher with
  `ansible.builtin.copy` and inline `content:` to
  `/home/{{ user_login }}/.local/share/applications/<name>.desktop`, owned by the user,
  mode 0644. `play-photography.yml` writes system ones to `/usr/share/applications/`.
- **Pinned GitHub release binaries:** `play-cli-tools.yml` (ouch: static musl tarball,
  `unarchive` to a versioned directory, symlink into `/usr/local/bin`, then assert
  `--version` matches the pin). `play-photography.yml` (RapidRAW: `get_url` with
  `checksum: "sha256:{{ pin }}"`, `dnf` with `disable_gpg_check`, and a newer-release
  check). `scripts/check-pinned-versions.bash` and the `update-versions` skill track
  these pins.
- No Matrix client is installed anywhere in the repo today.

**Design consequence:** team names, addresses and ports live only in the untracked team
registry, so a play cannot template them from tracked vars. Proposal: the desktop play
installs the Flatpak and the terminal client, and ships a profile template. The
`agent-team create` / `remove` command writes and removes each team's profile
`config.json` and launcher from the registry. **Decide in DESIGN.md.**

## 3. Terminal Matrix clients

| Client                                    | Kind / maturity                                                                                                                                                                                                                                                      | Fedora packaging                                                                                                                       | Single-binary install                                                                                                                                                                                                                            | Network behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **iamb** (Rust, matrix-rust-sdk 0.19)     | Full TUI with Vim keys. Threads, spaces, E2EE, receipts. v0.0.12, released 2026-09-20.                                                                                                                                                                               | Not in Fedora (packages.fedoraproject.org returns 404). Only unofficial COPRs. Also on Flathub (`chat.iamb.iamb`), Snap and crates.io. | Yes. Each release ships `x86_64-unknown-linux-musl` static binary, `.rpm` and `.deb`, zipped from v0.0.12 (`iamb-x86_64-unknown-linux-musl-binary.zip`; earlier releases shipped `.tgz`). **No checksum file**, so the play pins its own sha256. | All HTTP goes through matrix-sdk. Cargo.toml has no other HTTP client. With `profiles.<p>.url` set, `homeserver_url()` is used directly and there is **no `.well-known` discovery** (`src/worker.rs`). Without it, it resolves `server_name` and then tries `https://<server_name>/`, so the URL must always be set. reqwest honours `*_PROXY` environment variables unless the config `proxy` setting disables them. Image previews fetch media from the homeserver. Notifications go over local D-Bus. `:open` runs a local opener only on user action. No telemetry, update check or identity server in the documentation or dependencies. **VERIFY** with a capture. |
| **gomuks**, new architecture (Go)         | A backend plus frontends. The web frontend is "ready for daily use". The terminal frontend is "still experimental and doesn't have many features beyond basic chatting" and must be set up through the web frontend first. Monthly releases (v0.2609.0, 2026-09-16). | Not in Fedora. Unofficial COPRs only.                                                                                                  | Yes. Static `gomuks-terminal-amd64` with `sha256sums.txt`.                                                                                                                                                                                       | Backend plus web UI means more moving parts. The web frontend's GIF search goes through gomuks' own `gifproxy`, a media repo that redirects to the Giphy and Klipy CDNs: third-party, when the feature is used. Not audited further.                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| **legacy gomuks** (v0.3.1)                | A TUI that the upstream rewrite has replaced.                                                                                                                                                                                                                        | No                                                                                                                                     | Yes                                                                                                                                                                                                                                              | Unmaintained. Not suitable.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| **matrix-commander** (Python, matrix-nio) | A one-shot CLI, not a viewer.                                                                                                                                                                                                                                        | Not packaged (`python3-matrix-nio` is in Fedora).                                                                                      | No (pip)                                                                                                                                                                                                                                         | Scriptable, but not a way for a human to watch. Duplicates `pingbus tail`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| **weechat + weechat-matrix**              | A Python plugin, upstream inactive.                                                                                                                                                                                                                                  | weechat yes, plugin no                                                                                                                 | No                                                                                                                                                                                                                                               | Not considered.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |

### Recommendation: iamb

It is a mature, full TUI, a single static binary from upstream releases, and has the
smallest network surface: matrix-sdk to one pinned homeserver URL, with no discovery when
`url` is set.

Install it the way ouch is installed: pin the version and a repo-recorded sha256, download
the musl zip, unpack it under a versioned directory, symlink it into `/usr/local/bin`,
assert `iamb --version`, and add it to `check-pinned-versions.bash`. Avoid the COPRs and Snap.

Per team, write an iamb profile with `url` set to the host-local base URL, `user_id`, a
`password_file` with mode 0600, `settings.image_preview` off, and notifications as wanted.
This can go in the same `agent-team` step as the Element profile. **VERIFY** that iamb can
log in with a pre-provisioned account non-interactively (`password_file`), and where it
stores the session token (its data directory), so the token check covers it.

Revisit gomuks terminal once it no longer needs the web frontend to set it up.

## 4. Proposed checks for the plan

1. The profile directory resolves to `~/.var/app/im.riot.Riot/config/Element-<team>/` (1.2).
2. A capture of traffic and DNS during a scripted session of the Element team profile
   (start, log in, open a room, receive a ping) shows only the homeserver address. Watch for
   `redirector.gvt1.com` (spell-check), `*.element.io`, `vector.im`, `matrix.org`,
   `maptiler`, and any lookup of the team's `server_name`.
3. The same capture for iamb.
4. Element Desktop logs in to a plain-HTTP bridge address. If it does not, use
   `127.0.0.1` (1.4).
5. The token-in-logs check includes the Element profile directory and iamb's log directory.

## Sources

- Element Desktop README (profiles, config.json locations, `--config`):
  <https://github.com/element-hq/element-desktop/blob/develop/README.md>
- Element Desktop `src/electron-main.ts` (profile path, shallow merge, homeserver-key
  stripping, updater gate, spell-check, Sentry):
  <https://github.com/element-hq/element-desktop/blob/develop/src/electron-main.ts>
- Element Desktop `src/updater.ts` (no auto-update on Linux):
  <https://github.com/element-hq/element-desktop/blob/develop/src/updater.ts>
- Element Desktop `src/store.ts` (`electron-config`, `spellCheckerEnabled`):
  <https://github.com/element-hq/element-desktop/blob/develop/src/store.ts>
- Bundled element.io release config:
  <https://github.com/element-hq/element-desktop/blob/develop/element.io/release/config.json>
- Element Web config reference: <https://github.com/element-hq/element-web/blob/develop/docs/config.md>
- Element Web `SdkConfig.ts` (DEFAULTS, `mergeConfig`):
  <https://github.com/element-hq/element-web/blob/develop/apps/web/src/SdkConfig.ts>
- Element Web releases (`enable_client_well_known_lookups` in v1.12.28):
  <https://github.com/element-hq/element-web/releases>
- Flathub packaging: <https://github.com/flathub/im.riot.Riot> (README, `im.riot.Riot.yaml`, `element.sh`)
- Electron spellchecker (Google CDN dictionaries): <https://www.electronjs.org/docs/latest/tutorial/spellchecker>,
  <https://github.com/HilbertraumAI/HilbertRaum/issues/567>
- Plain-HTTP homeserver and Element Desktop vs Web: <https://github.com/famstack-dev/famstack/pull/161>
- iamb: <https://github.com/ulyssa/iamb> (README, `docs/iamb.5`, `Cargo.toml`, `src/worker.rs`, releases API)
- gomuks: <https://github.com/gomuks/gomuks>, releases API, <https://github.com/gomuks> (gifproxy)
- Fedora packages search: <https://packages.fedoraproject.org/> (404 for iamb, gomuks,
  matrix-commander, weechat-matrix). COPR search API.
